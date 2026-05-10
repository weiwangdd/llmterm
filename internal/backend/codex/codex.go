// Package codex adapts the locally-installed `codex` CLI (OpenAI Codex CLI)
// to llmterm's backend.Backend interface. It uses `codex exec --json`.
package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/weiwangdd/llmterm/internal/backend"
	"github.com/weiwangdd/llmterm/internal/event"
)

func init() { backend.Register(&Backend{}) }

type Backend struct{}

func (b *Backend) Name() string { return "codex" }

func (b *Backend) Available(ctx context.Context) error {
	if _, err := exec.LookPath("codex"); err != nil {
		return fmt.Errorf("codex CLI not found in PATH (install: https://github.com/openai/codex)")
	}
	return nil
}

func (b *Backend) Run(ctx context.Context, opts backend.Options) (<-chan event.Event, <-chan error, error) {
	args := []string{
		"exec", "--json", "--skip-git-repo-check", "--color", "never",
	}
	// Codex's sandbox mode is the equivalent of our allowed-tool gating.
	// Read-only by default; --unsafe lets it write & execute commands.
	if opts.Unsafe {
		// "danger-full-access" disables the codex sandbox entirely. Equivalent
		// to claude's Bash/Edit/Write allowance. The user opted in via `llm!`.
		args = append(args, "-s", "danger-full-access", "--dangerously-bypass-approvals-and-sandbox")
	} else {
		args = append(args, "-s", "read-only")
	}
	if opts.CWD != "" {
		args = append(args, "-C", opts.CWD)
	}
	args = append(args, opts.Prompt)

	cmd := exec.CommandContext(ctx, "codex", args...)
	cmd.Dir = opts.CWD
	// Codex prints "Reading additional input from stdin..." if stdin is open.
	cmd.Stdin = devNull()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("start codex: %w", err)
	}

	events := make(chan event.Event, 32)
	errs := make(chan error, 2)

	stderrBuf := &strings.Builder{}
	go func() { _, _ = io.Copy(stderrBuf, stderr) }()

	go func() {
		defer close(events)
		defer close(errs)

		var st state
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
		for sc.Scan() {
			line := append([]byte(nil), sc.Bytes()...)
			if len(line) == 0 {
				continue
			}
			ev, ok := parse(line, &st)
			if !ok {
				continue
			}
			select {
			case events <- ev:
			case <-ctx.Done():
				return
			}
		}
		// Synthesize a final event from the last agent_message + thread id.
		// Codex's turn.completed has only usage; we keep our own bookkeeping.
		if st.sawTurnCompleted {
			fin := event.Event{
				Kind: event.KindFinal,
				Final: &event.Final{
					SessionID:  st.threadID,
					Result:     st.lastAgentMsg,
					DurationMS: 0, // codex doesn't report wall time; leave 0.
					NumTurns:   1,
				},
			}
			select {
			case events <- fin:
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
			errs <- fmt.Errorf("codex exited: %w; stderr: %s", waitErr, tail)
		}
	}()

	return events, errs, nil
}

// state tracks pieces we need to assemble per stream:
//   - thread id (only seen on thread.started)
//   - last agent_message text (used for the synthesized final result)
//   - whether turn.completed arrived (so we don't emit a half-baked final)
//   - in-flight command_execution items keyed by id (so the matching
//     item.completed becomes a tool_result for the right tool_use id)
type state struct {
	threadID         string
	lastAgentMsg     string
	sawTurnCompleted bool
	pendingCmds      map[string]bool
}

func parse(line []byte, st *state) (event.Event, bool) {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(line, &head); err != nil {
		return event.Event{}, false
	}

	switch head.Type {
	case "thread.started":
		var x struct {
			ThreadID string `json:"thread_id"`
		}
		_ = json.Unmarshal(line, &x)
		st.threadID = x.ThreadID
		return event.Event{
			Kind: event.KindInit,
			Init: &event.Init{SessionID: x.ThreadID},
		}, true

	case "item.started":
		var x struct {
			Item struct {
				ID      string `json:"id"`
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"item"`
		}
		_ = json.Unmarshal(line, &x)
		if x.Item.Type == "command_execution" {
			if st.pendingCmds == nil {
				st.pendingCmds = map[string]bool{}
			}
			st.pendingCmds[x.Item.ID] = true
			return event.Event{
				Kind: event.KindToolUse,
				Tool: &event.ToolUse{
					ID:    x.Item.ID,
					Name:  "Bash",
					Input: map[string]any{"command": x.Item.Command},
				},
			}, true
		}
		return event.Event{}, false

	case "item.completed":
		var x struct {
			Item struct {
				ID               string `json:"id"`
				Type             string `json:"type"`
				Command          string `json:"command"`
				AggregatedOutput string `json:"aggregated_output"`
				ExitCode         *int   `json:"exit_code"`
				Text             string `json:"text"`
			} `json:"item"`
		}
		_ = json.Unmarshal(line, &x)
		switch x.Item.Type {
		case "command_execution":
			if st.pendingCmds != nil {
				delete(st.pendingCmds, x.Item.ID)
			}
			isErr := x.Item.ExitCode != nil && *x.Item.ExitCode != 0
			return event.Event{
				Kind: event.KindToolResult,
				Result: &event.ToolResult{
					ToolUseID: x.Item.ID,
					Content:   x.Item.AggregatedOutput,
					IsError:   isErr,
				},
			}, true
		case "agent_message":
			st.lastAgentMsg = x.Item.Text
			// Stream the final assistant text as a single delta so the
			// renderer prints it inline like claude's text deltas.
			return event.Event{
				Kind: event.KindTextDelta,
				Text: &event.TextDelta{Text: x.Item.Text},
			}, true
		}
		return event.Event{}, false

	case "turn.completed":
		st.sawTurnCompleted = true
		return event.Event{}, false
	}
	return event.Event{}, false
}

func devNull() *os.File {
	f, _ := os.Open(os.DevNull)
	return f
}
