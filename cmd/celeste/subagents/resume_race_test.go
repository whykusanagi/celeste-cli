package subagents

import (
	"context"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
)

// A resumed run is registered before it finishes, so its final fields are
// written under the manager's lock, where ListRuns and the stagger scan
// read them (go test -race).
func TestResumeWritesTheRunUnderTheLock(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{Status: 400, Body: `{"error":{"message":"fake bad request","type":"invalid_request_error"}}`},
		fakeprovider.Turn{Text: "resumed and finished"},
	)
	m, _, ws, _ := fakeManager(t, srv)
	run, err := m.Spawn(context.Background(), "do it", ws)
	if err == nil || run.CheckpointID == "" {
		t.Fatalf("want a failed first run with a checkpoint: run=%+v err=%v", run, err)
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			m.mu.Lock()
			for _, r := range m.runs {
				_, _, _, _ = r.Status, r.Result, r.Summary, r.EndedAt
			}
			m.mu.Unlock()
		}
	}()
	resumed, err := m.Resume(context.Background(), run.CheckpointID, nil)
	close(stop)
	<-done
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	status := resumed.Status
	m.mu.Unlock()
	if status != "completed" {
		t.Fatalf("status = %q", status)
	}
}
