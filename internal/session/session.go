package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const idleTTL = 2 * time.Hour

// Store maps cwd -> claude session id, with idle expiry. Persisted as JSON.
type Store struct {
	path    string
	mu      sync.Mutex
	entries map[string]entry
}

type entry struct {
	SessionID string    `json:"session_id"`
	Updated   time.Time `json:"updated"`
}

func DefaultPath() string {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "llmterm", "sessions.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "llmterm", "sessions.json")
}

func Open(path string) *Store {
	s := &Store{path: path, entries: map[string]entry{}}
	b, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(b, &s.entries)
	}
	return s
}

// Resume returns a non-empty session id if one is fresh enough to reuse for cwd.
func (s *Store) Resume(cwd string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[cwd]
	if !ok {
		return ""
	}
	if time.Since(e.Updated) > idleTTL {
		return ""
	}
	return e.SessionID
}

// Save updates the entry for cwd and writes to disk. Best-effort; errors ignored.
func (s *Store) Save(cwd, sessionID string) {
	if cwd == "" || sessionID == "" {
		return
	}
	s.mu.Lock()
	s.entries[cwd] = entry{SessionID: sessionID, Updated: time.Now()}
	b, _ := json.MarshalIndent(s.entries, "", "  ")
	s.mu.Unlock()
	_ = os.MkdirAll(filepath.Dir(s.path), 0o755)
	_ = os.WriteFile(s.path, b, 0o600)
}
