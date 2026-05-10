// Package event defines the backend-agnostic event union that flows from a
// running agent (claude, codex, gemini) into the renderer. Each backend's
// adapter is responsible for translating its native NDJSON/JSONL events into
// these types.
package event

type Kind int

const (
	KindIgnored Kind = iota
	KindInit
	KindTextDelta
	KindToolUse
	KindToolResult
	KindFinal
	KindError
)

type Event struct {
	Kind   Kind
	Init   *Init
	Text   *TextDelta
	Tool   *ToolUse
	Result *ToolResult
	Final  *Final
	Err    *Error
}

type Init struct {
	SessionID string
	Model     string
	CWD       string
}

type TextDelta struct {
	Text string
}

type ToolUse struct {
	ID    string
	Name  string
	Input map[string]any
}

type ToolResult struct {
	ToolUseID string
	Content   string
	IsError   bool
}

type Final struct {
	SessionID    string
	IsError      bool
	Result       string
	DurationMS   int
	NumTurns     int
	TotalCostUSD float64
	// Tool denials surfaced by the backend (free-form per-backend).
	PermissionDenied []map[string]any
}

type Error struct {
	Message string
}
