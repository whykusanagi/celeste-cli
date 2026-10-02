package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Windows' clock can return the same UnixNano for back-to-back calls; two
// sessions with one ID overwrite each other's file on save.
func TestNewSessionIDsAreUniqueAndIncreasing(t *testing.T) {
	m := &SessionManager{}
	prev := ""
	seen := map[string]bool{}
	for i := 0; i < 10000; i++ {
		id := m.NewSession().ID
		if seen[id] {
			t.Fatalf("duplicate session ID %s after %d sessions", id, i)
		}
		seen[id] = true
		if prev != "" && len(id) == len(prev) && id <= prev {
			t.Fatalf("session ID %s not after %s", id, prev)
		}
		prev = id
	}
	merged := m.MergeSessions(&Session{}, &Session{})
	if seen[merged.ID] {
		t.Fatalf("merged session reused ID %s", merged.ID)
	}
}

// Another process may already have saved a session under the next ID.
func TestNewSessionSkipsAnIDWhoseFileExists(t *testing.T) {
	dir := t.TempDir()
	m := &SessionManager{sessionsDir: dir}
	future := time.Now().UnixNano() + int64(time.Hour)
	lastSessionID.Store(future)
	taken := fmt.Sprintf("%d", future+1)
	if err := os.WriteFile(filepath.Join(dir, taken+".json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := m.NewSession().ID; got == taken {
		t.Fatalf("NewSession returned %s, whose file already exists", got)
	}
}
