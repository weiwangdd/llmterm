package render

import (
	"bufio"
	"bytes"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/wei/llmterm/internal/backend/claude"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestRenderFixture(t *testing.T) {
	f, err := os.Open("../../testdata/claude_stream_with_bash.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var buf bytes.Buffer
	r := New(&buf, false)
	var sid string
	var done bool

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		ev, err := claude.Parse(line)
		if err != nil {
			t.Fatal(err)
		}
		s, d := r.Handle(ev)
		if s != "" {
			sid = s
		}
		if d {
			done = true
		}
	}

	plain := ansiRE.ReplaceAllString(buf.String(), "")
	if !done {
		t.Fatal("renderer did not see final event")
	}
	if sid == "" {
		t.Error("expected session id from final event")
	}
	if !strings.Contains(plain, "▸ Bash") {
		t.Errorf("missing Bash tool line:\n%s", plain)
	}
	if !strings.Contains(plain, "ls -la") {
		t.Errorf("missing command echo:\n%s", plain)
	}
	if !strings.Contains(plain, "✓") {
		t.Errorf("missing success marker:\n%s", plain)
	}
	if !strings.Contains(plain, "Listed contents of the working directory.") {
		t.Errorf("missing assistant final text:\n%s", plain)
	}
}
