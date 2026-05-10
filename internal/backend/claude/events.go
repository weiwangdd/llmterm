package claude

import (
	"encoding/json"
	"fmt"

	"github.com/wei/llmterm/internal/event"
)

// Parse decodes one NDJSON line from `claude -p --output-format=stream-json`.
// Lines we don't care about return KindIgnored.
func Parse(line []byte) (event.Event, error) {
	var ev event.Event
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
			ev.Kind = event.KindInit
			ev.Init = &event.Init{SessionID: x.SessionID, CWD: x.CWD, Model: x.Model}
			return ev, nil
		}
		return ev, nil

	case "stream_event":
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
			ev.Kind = event.KindTextDelta
			ev.Text = &event.TextDelta{Text: x.Event.Delta.Text}
		}
		return ev, nil

	case "assistant":
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
				ev.Kind = event.KindToolUse
				ev.Tool = &event.ToolUse{ID: c.ID, Name: c.Name, Input: c.Input}
				return ev, nil
			}
		}
		return ev, nil

	case "user":
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
				ev.Kind = event.KindToolResult
				ev.Result = &event.ToolResult{
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
		ev.Kind = event.KindFinal
		ev.Final = &event.Final{
			SessionID:        x.SessionID,
			IsError:          x.IsError,
			Result:           x.Result,
			DurationMS:       x.DurationMS,
			NumTurns:         x.NumTurns,
			TotalCostUSD:     x.TotalCostUSD,
			PermissionDenied: x.PermissionDenied,
		}
		return ev, nil
	}
	return ev, nil
}

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
