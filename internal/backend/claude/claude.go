package claude

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
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

// Options drives one invocation of the claude CLI.
type Options struct {
	Prompt       string
	CWD          string
	AllowedTools []string
	ResumeID     string // if set, passes --resume <id>
	Model        string // optional, e.g. "claude-sonnet-4-6"
	ExtraArgs    []string
}

// Run starts `claude -p` and streams parsed events into the returned channel.
// The channel closes when the child exits. ctx cancellation kills the child.
// The error channel reports spawn / decode / process-exit issues.
func Run(ctx context.Context, opts Options) (<-chan Event, <-chan error, error) {
	args := []string{
		"-p", opts.Prompt,
		"--output-format", "stream-json",
		"--include-partial-messages",
		"--verbose",
		"--append-system-prompt", nonInteractiveSystemPrompt,
	}
	if opts.CWD != "" {
		args = append(args, "--add-dir", opts.CWD)
	}
	if len(opts.AllowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(opts.AllowedTools, " "))
	}
	if opts.ResumeID != "" {
		args = append(args, "--resume", opts.ResumeID)
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	args = append(args, opts.ExtraArgs...)

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

	events := make(chan Event, 32)
	errs := make(chan error, 2)

	// Drain stderr in the background; surface only on non-zero exit.
	stderrBuf := &strings.Builder{}
	go func() {
		_, _ = io.Copy(stderrBuf, stderr)
	}()

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
			if ev.Kind == KindIgnored {
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
