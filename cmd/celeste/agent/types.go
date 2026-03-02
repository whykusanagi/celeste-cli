package agent

import (
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

const (
	StatusRunning           = "running"
	StatusCompleted         = "completed"
	StatusFailed            = "failed"
	StatusMaxTurnsReached   = "max_turns_reached"
	StatusNoProgressStopped = "no_progress_stopped"
)

type Options struct {
	Workspace                 string        `json:"workspace"`
	MaxTurns                  int           `json:"max_turns"`
	MaxToolCallsPerTurn       int           `json:"max_tool_calls_per_turn"`
	MaxConsecutiveNoToolTurns int           `json:"max_consecutive_no_tool_turns"`
	RequestTimeout            time.Duration `json:"request_timeout"`
	ToolTimeout               time.Duration `json:"tool_timeout"`
	RequireCompletionMarker   bool          `json:"require_completion_marker"`
	CompletionMarker          string        `json:"completion_marker"`
	DisableCheckpoints        bool          `json:"disable_checkpoints"`
	Verbose                   bool          `json:"verbose"`
}

func DefaultOptions() Options {
	return Options{
		MaxTurns:                  12,
		MaxToolCallsPerTurn:       8,
		MaxConsecutiveNoToolTurns: 3,
		RequestTimeout:            90 * time.Second,
		ToolTimeout:               45 * time.Second,
		RequireCompletionMarker:   true,
		CompletionMarker:          "TASK_COMPLETE:",
		DisableCheckpoints:        false,
		Verbose:                   true,
	}
}

type Step struct {
	Turn      int       `json:"turn"`
	Type      string    `json:"type"`
	Name      string    `json:"name,omitempty"`
	Content   string    `json:"content,omitempty"`
	ToolCall  string    `json:"tool_call_id,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

type RunState struct {
	RunID                  string            `json:"run_id"`
	Goal                   string            `json:"goal"`
	Status                 string            `json:"status"`
	CreatedAt              time.Time         `json:"created_at"`
	UpdatedAt              time.Time         `json:"updated_at"`
	CompletedAt            *time.Time        `json:"completed_at,omitempty"`
	Turn                   int               `json:"turn"`
	ConsecutiveNoToolTurns int               `json:"consecutive_no_tool_turns"`
	ToolCallCount          int               `json:"tool_call_count"`
	Messages               []tui.ChatMessage `json:"messages"`
	Steps                  []Step            `json:"steps"`
	LastAssistantResponse  string            `json:"last_assistant_response,omitempty"`
	Error                  string            `json:"error,omitempty"`
	Options                Options           `json:"options"`
}

func NewRunState(goal string, options Options) *RunState {
	now := time.Now()
	return &RunState{
		RunID:     generateRunID(now),
		Goal:      goal,
		Status:    StatusRunning,
		CreatedAt: now,
		UpdatedAt: now,
		Messages:  []tui.ChatMessage{},
		Steps:     []Step{},
		Options:   options,
	}
}

func generateRunID(t time.Time) string {
	return t.Format("20060102-150405.000000000")
}
