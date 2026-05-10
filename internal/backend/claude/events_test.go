package claude

import (
	"bufio"
	"os"
	"testing"
)

func TestParseFixture(t *testing.T) {
	f, err := os.Open("../../../testdata/claude_stream_with_bash.ndjson")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	counts := map[EventKind]int{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 8*1024*1024)
	var sawText, sawToolUse, sawToolResult, sawFinal bool
	var lastFinal *FinalResult

	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		ev, err := Parse(line)
		if err != nil {
			t.Fatalf("parse: %v\nline=%s", err, line)
		}
		counts[ev.Kind]++
		switch ev.Kind {
		case KindTextDelta:
			if ev.Text.Text != "" {
				sawText = true
			}
		case KindToolUse:
			if ev.Tool.Name == "Bash" && ev.Tool.Input["command"] == "ls -la" {
				sawToolUse = true
			}
		case KindToolResult:
			if ev.Result.Content != "" && !ev.Result.IsError {
				sawToolResult = true
			}
		case KindFinal:
			sawFinal = true
			lastFinal = ev.Final
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}

	if !sawText {
		t.Error("expected text deltas")
	}
	if !sawToolUse {
		t.Error("expected Bash tool_use with command=ls -la")
	}
	if !sawToolResult {
		t.Error("expected non-error tool_result")
	}
	if !sawFinal {
		t.Fatal("expected final result event")
	}
	if lastFinal.IsError {
		t.Errorf("expected success, got error final: %+v", lastFinal)
	}
	if lastFinal.Result == "" {
		t.Error("expected non-empty result text")
	}
}
