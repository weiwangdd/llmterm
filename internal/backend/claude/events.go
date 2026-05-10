package claude

import (
	"encoding/json"
	"fmt"
)

// Event is the parsed union of one NDJSON line from `claude -p --output-format=stream-json`.
// Only the fields llmterm needs are decoded; the raw line is kept for debugging.
type Event struct {
	Kind    EventKind
	Raw     []byte
	Init    *InitEvent
	Text    *TextDelta
	Tool    *ToolUse
	Result  *ToolResult
	Final   *FinalResult
	Err     *ErrorEvent
}

type EventKind int

const (
	KindIgnored EventKind = iota
	KindInit
	KindTextDelta
	KindToolUse
	KindToolResult
	KindFinal
	KindError
)

type InitEvent struct {
	SessionID  string
	CWD        string
	Model      string
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

type FinalResult struct {
	SessionID        string
	IsError          bool
	Result           string
	DurationMS       int
	NumTurns         int
	TotalCostUSD     float64
	PermissionDenied []map[string]any
}

type ErrorEvent struct {
	Message string
}

// Parse decodes one NDJSON line. Returns an Event whose Kind tells the consumer
// which payload field to read. Lines we don't care about return KindIgnored.
func Parse(line []byte) (Event, error) {
	ev := Event{Raw: line}
	var head struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
	}
	if err := json.Unmarshal(line, &head); err != nil {
		return ev, fmt.Errorf("decode head: %w", err)
	}

	switch head.Type {
	case "system":
		if head.Subtype == "init" {
			var x struct {
				SessionID string `json:"session_id"`
				CWD       string `json:"cwd"`
				Model     string `json:"model"`
			}
			if err := json.Unmarshal(line, &x); err != nil {
				return ev, err
			}
			ev.Kind = KindInit
			ev.Init = &InitEvent{SessionID: x.SessionID, CWD: x.CWD, Model: x.Model}
			return ev, nil
		}
		// hook_started, hook_response, status — noise
		return ev, nil

	case "stream_event":
		// Only consume text_delta partials; tool_use comes via the consolidated
		// `assistant` event so we don't have to reconstruct partial JSON.
		var x struct {
			Event struct {
				Type  string `json:"type"`
				Delta struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"delta"`
			} `json:"event"`
		}
		if err := json.Unmarshal(line, &x); err != nil {
			return ev, err
		}
		if x.Event.Type == "content_block_delta" && x.Event.Delta.Type == "text_delta" {
			ev.Kind = KindTextDelta
			ev.Text = &TextDelta{Text: x.Event.Delta.Text}
		}
		return ev, nil

	case "assistant":
		// Full assistant message after streaming. Use this to detect tool_use blocks.
		var x struct {
			Message struct {
				Content []struct {
					Type  string         `json:"type"`
					ID    string         `json:"id"`
					Name  string         `json:"name"`
					Input map[string]any `json:"input"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &x); err != nil {
			return ev, err
		}
		for _, c := range x.Message.Content {
			if c.Type == "tool_use" {
				ev.Kind = KindToolUse
				ev.Tool = &ToolUse{ID: c.ID, Name: c.Name, Input: c.Input}
				return ev, nil
			}
		}
		return ev, nil

	case "user":
		// Tool result comes back as a user-role message with tool_result content.
		var x struct {
			Message struct {
				Content []struct {
					Type      string `json:"type"`
					ToolUseID string `json:"tool_use_id"`
					Content   any    `json:"content"`
					IsError   bool   `json:"is_error"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &x); err != nil {
			return ev, err
		}
		for _, c := range x.Message.Content {
			if c.Type == "tool_result" {
				ev.Kind = KindToolResult
				ev.Result = &ToolResult{
					ToolUseID: c.ToolUseID,
					Content:   stringifyToolResult(c.Content),
					IsError:   c.IsError,
				}
				return ev, nil
			}
		}
		return ev, nil

	case "result":
		var x struct {
			Subtype          string           `json:"subtype"`
			IsError          bool             `json:"is_error"`
			Result           string           `json:"result"`
			DurationMS       int              `json:"duration_ms"`
			NumTurns         int              `json:"num_turns"`
			SessionID        string           `json:"session_id"`
			TotalCostUSD     float64          `json:"total_cost_usd"`
			PermissionDenied []map[string]any `json:"permission_denials"`
		}
		if err := json.Unmarshal(line, &x); err != nil {
			return ev, err
		}
		ev.Kind = KindFinal
		ev.Final = &FinalResult{
			SessionID:        x.SessionID,
			IsError:          x.IsError,
			Result:           x.Result,
			DurationMS:       x.DurationMS,
			NumTurns:         x.NumTurns,
			TotalCostUSD:     x.TotalCostUSD,
			PermissionDenied: x.PermissionDenied,
		}
		return ev, nil

	case "rate_limit_event":
		return ev, nil
	}

	return ev, nil
}

// Tool result content can be either a string or a list of content blocks.
// We flatten to plain text for display.
func stringifyToolResult(c any) string {
	switch v := c.(type) {
	case string:
		return v
	case []any:
		var out string
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if t, _ := m["text"].(string); t != "" {
					out += t
				}
			}
		}
		return out
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}
