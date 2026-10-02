package builtin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// A reader (a summary rendering the todo list while a subagent on the same
// workspace rewrites it) never sees a torn tasks.json: save writes it
// atomically (#200).
func TestTodoStoreSaveIsAtomic(t *testing.T) {
	ws := t.TempDir()
	s := NewTodoStore(ws)
	s.Create("seed", "")
	path := filepath.Join(ws, ".celeste", "tasks.json")

	var done, stop atomic.Bool
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		defer done.Store(true)
		for i := 0; i < 300 && !stop.Load(); i++ {
			s.Create("a task with a title long enough to take more than one write", "and a description too")
		}
	}()
	var failure string
	for failure == "" && !done.Load() {
		data, err := os.ReadFile(path)
		var v map[string]any
		if err != nil {
			failure = "read: " + err.Error()
		} else if err := json.Unmarshal(data, &v); err != nil {
			failure = fmt.Sprintf("torn tasks.json (%d bytes): %v", len(data), err)
		}
	}
	stop.Store(true)
	<-finished
	if failure != "" {
		t.Fatal(failure)
	}
}
