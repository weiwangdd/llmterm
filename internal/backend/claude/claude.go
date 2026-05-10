// Package claude adapts the locally-installed `claude` CLI (Claude Code) to
// llmterm's backend.Backend interface.
package claude

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/weiwangdd/llmterm/internal/backend"
	"github.com/weiwangdd/llmterm/internal/event"
)

func init() { backend.Register(&Backend{}) }

const (
	readOnlyTools = "Read Glob Grep WebFetch WebSearch"
	writeTools    = "Read Glob Grep WebFetch WebSearch Bash Edit Write"
)

const nonInteractiveSystemPrompt = `You are running inside llmterm, a non-interactive single-shot session.
There is NO user input channel during this turn — you cannot ask the user any
questions and cannot wait for approval. Do not write text like "shall I…",
"do you allow…", "请授权…". Just act with the tools you have.

If a tool you would need is not currently allowed (e.g. Bash, Edit, Write are
typically only allowed when the user runs ` + "`llm!`" + ` instead of ` + "`llm`" + `),
do not ask. Instead, finish with a brief one-line note like:
  "(needs Bash; rerun: llm! <same prompt>)"
and stop. Be concise: this output goes straight to the user's terminal.`

type Backend struct{}

func (b *Backend) Name() string { return "claude" }

func (b *Backend) Available(ctx context.Context) error {
	if _, err := exec.LookPath("claude"); err != nil {
		return fmt.Errorf("claude CLI not found in PATH (install: https://docs.anthropic.com/claude/docs/claude-code)")
	}
	return nil
}

func (b *Backend) Run(ctx context.Context, opts backend.Options) (<-chan event.Event, <-chan error, error) {
	tools := readOnlyTools
	if opts.Unsafe {
		tools = writeTools
	}
	args := []string{
		"-p", opts.Prompt,
		"--output-format", "stream-json",
		"--include-partial-messages",
		"--verbose",
		"--append-system-prompt", nonInteractiveSystemPrompt,
		"--allowedTools", tools,
	}
	if opts.CWD != "" {
		args = append(args, "--add-dir", opts.CWD)
	}
	if opts.ResumeID != "" {
		args = append(args, "--resume", opts.ResumeID)
	}

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = opts.CWD

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("start claude: %w", err)
	}

	events := make(chan event.Event, 32)
	errs := make(chan error, 2)

	stderrBuf := &strings.Builder{}
	go func() { _, _ = io.Copy(stderrBuf, stderr) }()

	go func() {
		defer close(events)
		defer close(errs)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
		for sc.Scan() {
			line := append([]byte(nil), sc.Bytes()...)
			if len(line) == 0 {
				continue
			}
			ev, perr := Parse(line)
			if perr != nil {
				errs <- fmt.Errorf("parse: %w", perr)
				continue
			}
			if ev.Kind == event.KindIgnored {
				continue
			}
			select {
			case events <- ev:
			case <-ctx.Done():
				return
			}
		}
		if scErr := sc.Err(); scErr != nil {
			errs <- fmt.Errorf("scan stdout: %w", scErr)
		}
		if waitErr := cmd.Wait(); waitErr != nil {
			tail := stderrBuf.String()
			if len(tail) > 800 {
				tail = "..." + tail[len(tail)-800:]
			}
			errs <- fmt.Errorf("claude exited: %w; stderr: %s", waitErr, tail)
		}
	}()

	return events, errs, nil
}
