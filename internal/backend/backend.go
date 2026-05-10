// Package backend defines the contract every agent CLI adapter must implement
// (claude, codex, gemini). The concrete backends live in subpackages.
package backend

import (
	"context"
	"fmt"

	"github.com/weiwangdd/llmterm/internal/event"
)

type Options struct {
	Prompt   string
	CWD      string
	Unsafe   bool   // user invoked `llm!` — backend may grant write/exec tools.
	ResumeID string // best-effort session continuity if the backend supports it.
}

type Backend interface {
	Name() string
	// Run spawns the agent and streams parsed Events. The returned channels
	// close once the child exits. ctx cancellation kills the child.
	Run(ctx context.Context, opts Options) (<-chan event.Event, <-chan error, error)
	// Available returns a non-nil error with install/auth guidance if the
	// backend cannot be used. Called by `llmterm doctor` and `llmterm use`.
	Available(ctx context.Context) error
}

// Registry is populated by each backend package's init().
var registry = map[string]Backend{}

func Register(b Backend) { registry[b.Name()] = b }

func Get(name string) (Backend, error) {
	b, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown backend %q (have: %v)", name, Names())
	}
	return b, nil
}

func Names() []string {
	out := make([]string, 0, len(registry))
	for n := range registry {
		out = append(out, n)
	}
	return out
}
