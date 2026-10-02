package agent

import (
	"strings"
	"testing"
)

// Load rejects a run id that escapes the runs directory, and only that:
// the containment check (now pathutil.Within) keeps its lexical semantics.
func TestCheckpointLoadRunIDContainment(t *testing.T) {
	store, err := NewCheckpointStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		id      string
		escapes bool
	}{
		{"run1", false},
		{"..foo", false},
		{"sub/../run1", false},
		{"sub/", false},
		{"../run1", true},
		{"..", false}, // "...json": a file name, not the parent
		{"../", true},
		{"sub/../../run1", true},
		{"../../etc/passwd", true},
	}
	for _, c := range cases {
		_, err := store.Load(c.id)
		if err == nil {
			t.Fatalf("Load(%q) found a checkpoint in an empty store", c.id)
		}
		if got := strings.Contains(err.Error(), "invalid run id"); got != c.escapes {
			t.Errorf("Load(%q): rejected as invalid = %v, want %v (err %v)", c.id, got, c.escapes, err)
		}
	}
}
