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

const readOnlySystemPrompt = `You are running inside llmterm, a non-interactive single-shot session.
There is NO user input channel during this turn — you cannot ask the user any
questions and cannot wait for approval. Do not write text like "shall I…",
"do you allow…", "请授权…". Just act with the tools you have.

The currently allowed tools are read-only (Read, Glob, Grep, WebFetch, WebSearch).
Bash, Edit, and Write are NOT allowed in this session. If the task genuinely
needs one of those, do not ask — finish with one short line like:
  "(needs Bash; rerun: llm! <same prompt>)"
and stop. Be concise: this output goes straight to the user's terminal.`

const unsafeSystemPrompt = `You are running inside llmterm, a non-interactive single-shot session.
There is NO user input channel during this turn — you cannot ask the user any
questions and cannot wait for approval. Do not write text like "shall I…",
"do you allow…", "请授权…".

The user has explicitly enabled write/exec tools by invoking llmterm in
` + "`llm!`" + ` mode. Bash, Edit, Write, Read, Glob, Grep, WebFetch, and
WebSearch are ALL allowed. Use them freely to complete the task. Do not
refuse to act, do not suggest the user "rerun with llm!" — they already
did. Only stop if the task is genuinely impossible (e.g. requires sudo
on a system you can't elevate to), in which case explain in one short
line what's missing.`

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
	systemPrompt := readOnlySystemPrompt
	if opts.Unsafe {
		tools = writeTools
		systemPrompt = unsafeSystemPrompt
	}
	args := []string{
		"-p", opts.Prompt,
		"--output-format", "stream-json",
		"--include-partial-messages",
		"--verbose",
		"--append-system-prompt", systemPrompt,
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
