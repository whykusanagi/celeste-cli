package server

import (
	"context"
	"fmt"
	"time"
)

// maxTrackedRuns bounds the registry. Completed runs are retained so a caller
// polling after completion still gets its result, which means the map would
// otherwise grow for the life of the server. Fifty long runs in a single client
// session is far past normal use, and the alternative — dropping a result once
// read — loses it to a retried poll.
const maxTrackedRuns = 50

// BackgroundRun is the state of one MCP agent run that outlived the inline
// threshold. Held in memory only: the MCP server is a stdio child of the client,
// so a run dies when the client restarts. Checkpoints are still written to disk,
// so `celeste agent -resume <AgentRunID>` remains available from the CLI.
type BackgroundRun struct {
	ID        string
	Status    string // "running" | "completed" | "failed" | "cancelled"
	Result    string
	Error     string
	Turns     int
	ToolCalls int
	// AgentRunID is the agent's own run identifier, known only once RunGoal
	// returns. Recorded so the run can be resumed from the CLI afterwards.
	AgentRunID string
	StartedAt  time.Time
	EndedAt    time.Time

	cancel context.CancelFunc
}

// newRunID mints a handle identifier. Server-minted rather than the agent's
// RunID because RunGoal builds its state internally and only returns it on
// completion — the agent's ID does not exist when the handle must be returned.
func newRunID(now time.Time) string {
	return fmt.Sprintf("bg-%d", now.UnixNano())
}

// registerRun records a run as running and returns its state.
func (s *Server) registerRun(id string, cancel context.CancelFunc) *BackgroundRun {
	run := &BackgroundRun{
		ID:        id,
		Status:    "running",
		StartedAt: time.Now(),
		cancel:    cancel,
	}
	s.runMu.Lock()
	defer s.runMu.Unlock()
	s.runs[id] = run
	s.evictLocked()
	return run
}

// lookupRun returns a copy of a run's state. A copy, not the pointer: callers
// render it outside the lock, and the watcher goroutine may still be writing.
func (s *Server) lookupRun(id string) (BackgroundRun, bool) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	run, ok := s.runs[id]
	if !ok {
		return BackgroundRun{}, false
	}
	return *run, true
}

// completeRun records terminal state from a finished run.
func (s *Server) completeRun(id string, final *BackgroundRun) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	run, ok := s.runs[id]
	if !ok {
		return
	}
	run.Status = final.Status
	run.Result = final.Result
	run.Error = final.Error
	run.Turns = final.Turns
	run.ToolCalls = final.ToolCalls
	run.AgentRunID = final.AgentRunID
	run.EndedAt = time.Now()
	run.cancel = nil
}

// cancelRun cancels a live run. Returns false if the run is unknown or already
// terminal. Without this a runaway conductor run bills fan-out unstoppably.
func (s *Server) cancelRun(id string) bool {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	run, ok := s.runs[id]
	if !ok || run.cancel == nil {
		return false
	}
	run.cancel()
	run.cancel = nil
	run.Status = "cancelled"
	run.EndedAt = time.Now()
	return true
}

// runCount reports how many runs are tracked. Test seam for the bound.
func (s *Server) runCount() int {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	return len(s.runs)
}

// evictLocked drops the oldest COMPLETED run when over capacity. A running run
// is never evicted — losing a live run's handle would strand work in flight.
// Caller must hold runMu.
func (s *Server) evictLocked() {
	for len(s.runs) > maxTrackedRuns {
		var oldestID string
		var oldest time.Time
		for id, r := range s.runs {
			if r.Status == "running" {
				continue
			}
			if oldestID == "" || r.StartedAt.Before(oldest) {
				oldestID, oldest = id, r.StartedAt
			}
		}
		if oldestID == "" {
			return // everything tracked is still running; nothing may be evicted
		}
		delete(s.runs, oldestID)
	}
}
