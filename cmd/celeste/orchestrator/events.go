// Package orchestrator coordinates multiple agent runners and model routing.
package orchestrator

// EventKind identifies the type of an OrchestratorEvent.
type EventKind int

const (
	EventClassified   EventKind = iota // goal classified into a task lane
	EventAction                        // agent took a discrete action
	EventToolCall                      // agent called a tool
	EventFileDiff                      // agent wrote or modified a file
	EventReviewDraft                   // reviewer produced a critique
	EventDefense                       // primary agent responded to critique
	EventVerdict                       // reviewer issued a final verdict
	EventComplete                      // orchestrator run finished
	EventError                         // orchestrator run failed
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
	Kind       EventKind
	Lane       TaskLane
	Text       string  // human-readable description
	FilePath   string  // non-empty for EventFileDiff
	Diff       string  // unified diff content for EventFileDiff
	Turn       int
	MaxTurns   int
	Score      float64 // 0.0–1.0 for EventVerdict
	VerdictErr string  // non-empty if verdict is Contested or NeedsWork
}
