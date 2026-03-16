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

const (
	PhasePlanning     = "planning"
	PhaseExecution    = "execution"
	PhaseVerification = "verification"
)

const (
	PlanStatusPending    = "pending"
	PlanStatusInProgress = "in_progress"
	PlanStatusCompleted  = "completed"
)

// ProgressKind identifies an agent progress event.
type ProgressKind int

const (
	ProgressTurnStart ProgressKind = iota
	ProgressToolCall
	ProgressStepDone
	ProgressResponse
	ProgressComplete
	ProgressError
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
	EnablePlanning            bool          `json:"enable_planning"`
	PlanMaxSteps              int           `json:"plan_max_steps"`
	RequireVerification       bool          `json:"require_verification"`
	VerificationCommands      []string      `json:"verification_commands,omitempty"`
	VerifyTimeout             time.Duration `json:"verify_timeout"`
	EmitArtifacts             bool          `json:"emit_artifacts"`
	ArtifactDir               string        `json:"artifact_dir,omitempty"`
	DisableCheckpoints        bool          `json:"disable_checkpoints"`
	Verbose                   bool          `json:"verbose"`
	// OnProgress is an optional callback invoked at key agent events.
	// text is a human-readable label. turn/maxTurns are 0 for non-turn events.
	// This field is not serialised to JSON (func types are not JSON-safe).
	OnProgress func(kind ProgressKind, text string, turn, maxTurns int) `json:"-"`
	// OnTurnStats is an optional callback fired after each LLM API call completes.
	// It carries timing and token usage data for that call.
	OnTurnStats func(TurnStats) `json:"-"`
}

// TurnStats carries per-turn performance data from a completed LLM call.
type TurnStats struct {
	Turn         int
	MaxTurns     int
	Elapsed      time.Duration
	InputTokens  int
	OutputTokens int
	Response     string   // full assistant content for this turn (may be empty for pure tool-call turns)
	ToolCalls    []string // names of tools called this turn
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
		EnablePlanning:            true,
		PlanMaxSteps:              8,
		RequireVerification:       false,
		VerificationCommands:      nil,
		VerifyTimeout:             120 * time.Second,
		EmitArtifacts:             true,
		ArtifactDir:               "",
		DisableCheckpoints:        false,
		Verbose:                   true,
	}
}

type PlanStep struct {
	Index  int    `json:"index"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

type VerificationCheck struct {
	Command   string    `json:"command"`
	Passed    bool      `json:"passed"`
	ExitCode  int       `json:"exit_code"`
	Output    string    `json:"output,omitempty"`
	TimedOut  bool      `json:"timed_out,omitempty"`
	Timestamp time.Time `json:"timestamp"`
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
	RunID                  string              `json:"run_id"`
	Goal                   string              `json:"goal"`
	Status                 string              `json:"status"`
	CreatedAt              time.Time           `json:"created_at"`
	UpdatedAt              time.Time           `json:"updated_at"`
	CompletedAt            *time.Time          `json:"completed_at,omitempty"`
	Turn                   int                 `json:"turn"`
	ConsecutiveNoToolTurns int                 `json:"consecutive_no_tool_turns"`
	ToolCallCount          int                 `json:"tool_call_count"`
	Messages               []tui.ChatMessage   `json:"messages"`
	Steps                  []Step              `json:"steps"`
	Phase                  string              `json:"phase"`
	Plan                   []PlanStep          `json:"plan,omitempty"`
	ActivePlanStep         int                 `json:"active_plan_step,omitempty"`
	Verification           []VerificationCheck `json:"verification,omitempty"`
	LastAssistantResponse  string              `json:"last_assistant_response,omitempty"`
	ArtifactBundlePath     string              `json:"artifact_bundle_path,omitempty"`
	Error                  string              `json:"error,omitempty"`
	Options                Options             `json:"options"`
}

func NewRunState(goal string, options Options) *RunState {
	now := time.Now()
	return &RunState{
		RunID:        generateRunID(now),
		Goal:         goal,
		Status:       StatusRunning,
		CreatedAt:    now,
		UpdatedAt:    now,
		Messages:     []tui.ChatMessage{},
		Steps:        []Step{},
		Phase:        PhasePlanning,
		Plan:         []PlanStep{},
		Verification: []VerificationCheck{},
		Options:      options,
	}
}

func generateRunID(t time.Time) string {
	return t.Format("20060102-150405.000000000")
}
