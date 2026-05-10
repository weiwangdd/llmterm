// Package gemini adapts the Google Gemini CLI to llmterm's backend.Backend.
// MVP: simple --prompt mode without streaming events; the entire response is
// emitted as one text delta plus a final.
package gemini

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/wei/llmterm/internal/backend"
	"github.com/wei/llmterm/internal/event"
)

func init() { backend.Register(&Backend{}) }

type Backend struct{}

func (b *Backend) Name() string { return "gemini" }

func (b *Backend) Available(ctx context.Context) error {
	if _, err := exec.LookPath("gemini"); err != nil {
		return fmt.Errorf("gemini CLI not found in PATH (install: https://github.com/google-gemini/gemini-cli)")
	}
	return nil
}

func (b *Backend) Run(ctx context.Context, opts backend.Options) (<-chan event.Event, <-chan error, error) {
	args := []string{"--prompt", opts.Prompt}
	if opts.Unsafe {
		// Gemini CLI uses --yolo to auto-accept tool calls in non-interactive mode.
		args = append(args, "--yolo")
	}

	cmd := exec.CommandContext(ctx, "gemini", args...)
	cmd.Dir = opts.CWD
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	events := make(chan event.Event, 4)
	errs := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errs)
		runErr := cmd.Run()
		out := strings.TrimRight(stdout.String(), "\n")
		if runErr != nil {
			tail := stderr.String()
			if len(tail) > 800 {
				tail = "..." + tail[len(tail)-800:]
			}
			errs <- fmt.Errorf("gemini exited: %w; stderr: %s", runErr, tail)
			// Still emit a final so the renderer prints a closing line.
			events <- event.Event{Kind: event.KindFinal, Final: &event.Final{IsError: true, Result: out}}
			return
		}
		if out != "" {
			events <- event.Event{Kind: event.KindTextDelta, Text: &event.TextDelta{Text: out}}
		}
		events <- event.Event{Kind: event.KindFinal, Final: &event.Final{Result: out, NumTurns: 1}}
	}()

	return events, errs, nil
}
