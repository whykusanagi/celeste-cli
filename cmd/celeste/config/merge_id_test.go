package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A merged session never takes the ID of a session file already on disk
// (another process's session from the same clock tick), as NewSession.
func TestMergeSessionsSkipsAnIDInUse(t *testing.T) {
	stateHome(t)
	m := NewSessionManager()
	if err := os.MkdirAll(m.sessionsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// The next ID is predictable: one past a last ID set in the future.
	next := time.Now().Add(time.Hour).UnixNano()
	lastSessionID.Store(next - 1)
	taken := fmt.Sprintf("%d", next)
	if err := os.WriteFile(filepath.Join(m.sessionsDir, taken+".json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	merged := m.MergeSessions(&Session{Name: "a"}, &Session{Name: "b"})
	if merged.ID == taken {
		t.Fatalf("merged session took ID %s, whose file exists", taken)
	}
}
