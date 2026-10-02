package config

import "testing"

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
