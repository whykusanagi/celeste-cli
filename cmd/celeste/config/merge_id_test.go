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
	// The next ID is predictable with a fake clock.
	taken := fakeSessionClock(t, time.Now().Add(time.Hour))
	if err := os.WriteFile(filepath.Join(m.sessionsDir, taken+".json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	merged := m.MergeSessions(&Session{Name: "a"}, &Session{Name: "b"})
	if merged.ID == taken {
		t.Fatalf("merged session took ID %s, whose file exists", taken)
	}
}

// fakeSessionClock makes UniqueNanoID hand out base+1µs, base+2µs, ... and
// returns the first ID it will give.
func fakeSessionClock(t *testing.T, base time.Time) string {
	t.Helper()
	n := 0
	prev := sessionClock
	sessionClock = func() time.Time {
		n++
		return base.Add(time.Duration(n) * time.Microsecond)
	}
	t.Cleanup(func() { sessionClock = prev })
	return fmt.Sprintf("%d", base.Add(time.Microsecond).UnixNano())
}
