// Package orchestrator coordinates multiple agent runners and model routing.
package orchestrator

import "time"

// EventKind identifies the type of an OrchestratorEvent.
type EventKind int

const (
	EventClassified  EventKind = iota // goal classified into a task lane
	EventAction                       // agent took a discrete action
	EventToolCall                     // agent called a tool
	EventFileDiff                     // agent wrote or modified a file
	EventReviewDraft                  // reviewer produced a critique
	EventDefense                      // primary agent responded to critique
	EventVerdict                      // reviewer issued a final verdict
	EventComplete                     // orchestrator run finished
	EventError                        // orchestrator run failed
	EventDebateStart                  // debate round beginning (carries round number and reviewer model)
)

// TaskLane identifies the type of task being performed.
type TaskLane string

const (
	LaneCode     TaskLane = "code"
	LaneContent  TaskLane = "content"
	LaneMedia    TaskLane = "media"
	LaneReview   TaskLane = "review"
	LaneResearch TaskLane = "research"
	LaneUnknown  TaskLane = "unknown"
)

// OrchestratorEvent is emitted by the orchestrator state machine.
type OrchestratorEvent struct {
	Kind         EventKind
	Lane         TaskLane
	Text         string        // human-readable description
	Model        string        // model name — non-empty where role matters (primary/reviewer)
	Duration     time.Duration // per-turn API call elapsed time (EventTurnStats)
	InputTokens  int           // prompt tokens for this turn
	OutputTokens int           // completion tokens for this turn
	Response     string        // full assistant response text for this turn (live code output)
	FilePath     string        // non-empty for EventFileDiff
	Diff         string        // unified diff content for EventFileDiff
	Turn         int
	MaxTurns     int
	Score        float64 // 0.0–1.0 for EventVerdict
	VerdictErr   string  // non-empty if verdict is Contested or NeedsWork
}
