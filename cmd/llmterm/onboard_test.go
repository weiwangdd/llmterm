package main

import (
	"strings"
	"testing"
)

func TestZshrcSnippet(t *testing.T) {
	cases := []struct {
		name        string
		bin         string
		path        string
		home        string
		wantExport  bool
		wantExportL string // exact line we expect, without trailing newline
		wantEvalL   string
	}{
		{
			name:       "bin dir missing from PATH gets export line",
			bin:        "/home/alice/go/bin/llmterm",
			path:       "/usr/bin:/bin",
			home:       "/home/alice",
			wantExport: true,
			wantExportL: `export PATH="$HOME/go/bin:$PATH"`,
			wantEvalL:   `eval "$($HOME/go/bin/llmterm init zsh)"`,
		},
		{
			name:        "bin dir already in PATH — no export line",
			bin:         "/home/alice/go/bin/llmterm",
			path:        "/usr/bin:/home/alice/go/bin:/bin",
			home:        "/home/alice",
			wantExport:  false,
			wantEvalL:   `eval "$($HOME/go/bin/llmterm init zsh)"`,
		},
		{
			name:        "bin outside $HOME stays absolute",
			bin:         "/usr/local/bin/llmterm",
			path:        "/usr/bin:/bin",
			home:        "/home/alice",
			wantExport:  true,
			wantExportL: `export PATH="/usr/local/bin:$PATH"`,
			wantEvalL:   `eval "$(/usr/local/bin/llmterm init zsh)"`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := zshrcSnippet(c.bin, c.path, c.home)
			if !strings.Contains(got, c.wantEvalL) {
				t.Errorf("missing eval line\nwant: %s\ngot:\n%s", c.wantEvalL, got)
			}
			hasExport := strings.Contains(got, "export PATH=")
			if hasExport != c.wantExport {
				t.Errorf("export presence = %v, want %v\nsnippet:\n%s", hasExport, c.wantExport, got)
			}
			if c.wantExport && !strings.Contains(got, c.wantExportL) {
				t.Errorf("export line wrong\nwant: %s\ngot:\n%s", c.wantExportL, got)
			}
		})
	}
}
