package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/weiwangdd/llmterm/internal/event"
)

// ANSI helpers. Kept tiny on purpose — no TUI lib for MVP.
const (
	dim    = "\x1b[2m"
	bold   = "\x1b[1m"
	reset  = "\x1b[0m"
	cyan   = "\x1b[36m"
	green  = "\x1b[32m"
	red    = "\x1b[31m"
	yellow = "\x1b[33m"
)

type Renderer struct {
	w        io.Writer
	color    bool
	textOpen bool // true while we're in the middle of streaming an assistant text block
}

func New(w io.Writer, color bool) *Renderer {
	return &Renderer{w: w, color: color}
}

func (r *Renderer) c(code, s string) string {
	if !r.color {
		return s
	}
	return code + s + reset
}

// Handle one event. Returns the final SessionID once the stream ends, or "".
func (r *Renderer) Handle(ev event.Event) (sessionID string, done bool) {
	switch ev.Kind {
	case event.KindInit:
		// Quiet: don't announce session start. The user just wants their answer.
		return "", false

	case event.KindTextDelta:
		r.textOpen = true
		fmt.Fprint(r.w, ev.Text.Text)
		return "", false

	case event.KindToolUse:
		if r.textOpen {
			fmt.Fprintln(r.w)
			r.textOpen = false
		}
		fmt.Fprintln(r.w, r.c(cyan, "▸ "+ev.Tool.Name)+r.c(dim, "  "+oneLineInput(ev.Tool.Input)))
		return "", false

	case event.KindToolResult:
		if ev.Result.IsError {
			fmt.Fprintln(r.w, r.c(red, "  ✗ ")+r.c(dim, truncate(ev.Result.Content, 200)))
		} else {
			fmt.Fprintln(r.w, r.c(green, "  ✓ ")+r.c(dim, truncate(firstLine(ev.Result.Content), 200)))
		}
		return "", false

	case event.KindFinal:
		if r.textOpen {
			fmt.Fprintln(r.w)
			r.textOpen = false
		}
		if ev.Final.IsError {
			fmt.Fprintln(r.w, r.c(red, "✗ ")+ev.Final.Result)
		} else if len(ev.Final.PermissionDenied) > 0 {
			fmt.Fprintln(r.w, r.c(yellow, "⚠ permission denied for: ")+formatDenied(ev.Final.PermissionDenied))
			fmt.Fprintln(r.w, r.c(dim, "   tip: rerun with `llm! ...` to allow Bash/Edit/Write"))
		}
		dur := time.Duration(ev.Final.DurationMS) * time.Millisecond
		fmt.Fprintln(r.w, r.c(dim, fmt.Sprintf("─ %d turn(s) · %s · $%.4f",
			ev.Final.NumTurns, dur.Round(time.Millisecond), ev.Final.TotalCostUSD)))
		return ev.Final.SessionID, true
	}
	return "", false
}

func (r *Renderer) Errorf(format string, a ...any) {
	if r.textOpen {
		fmt.Fprintln(r.w)
		r.textOpen = false
	}
	fmt.Fprintln(r.w, r.c(red, "✗ ")+fmt.Sprintf(format, a...))
}

func oneLineInput(in map[string]any) string {
	// Bash: just show the command. Other tools: best-effort short summary.
	if cmd, ok := in["command"].(string); ok {
		return truncate(cmd, 120)
	}
	if path, ok := in["file_path"].(string); ok {
		return truncate(path, 120)
	}
	if pat, ok := in["pattern"].(string); ok {
		return truncate(pat, 120)
	}
	b, _ := json.Marshal(in)
	return truncate(string(b), 120)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func formatDenied(denials []map[string]any) string {
	parts := make([]string, 0, len(denials))
	for _, d := range denials {
		name, _ := d["tool_name"].(string)
		if name == "" {
			name, _ = d["tool"].(string)
		}
		if name != "" {
			parts = append(parts, name)
		}
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%v", denials)
	}
	return strings.Join(parts, ", ")
}
