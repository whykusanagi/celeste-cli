package agent

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/loop"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// runIDSeq makes run IDs unique even when two runs are created within the
// clock's resolution. time.Now() is coarse on Windows, so a plain timestamp
// collides and one checkpoint file (RunID+".json") overwrites another.
var runIDSeq atomic.Uint64

const (
	StatusRunning           = "running"
	StatusCompleted         = "completed"
	StatusFailed            = "failed"
	StatusMaxTurnsReached   = "max_turns_reached"
	StatusNoProgressStopped = "no_progress_stopped"
	StatusCancelled         = "cancelled"
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
	Workspace                     string        `json:"workspace"`
	MaxTurns                      int           `json:"max_turns"`
	MaxToolCallsPerTurn           int           `json:"max_tool_calls_per_turn"`
	MaxConsecutiveNoToolTurns     int           `json:"max_consecutive_no_tool_turns"`
	MaxConsecutiveInvalidToolArgs int           `json:"max_consecutive_invalid_tool_args"`
	RequestTimeout                time.Duration `json:"request_timeout"`
	ToolTimeout                   time.Duration `json:"tool_timeout"`
	RequireCompletionMarker       bool          `json:"require_completion_marker"`
	CompletionMarker              string        `json:"completion_marker"`
	EnablePlanning                bool          `json:"enable_planning"`
	PlanMaxSteps                  int           `json:"plan_max_steps"`
	RequireVerification           bool          `json:"require_verification"`
	// RequestTimeoutExplicit records that RequestTimeout was set deliberately by
	// the caller, so NewRunner must not raise it for conductor models. Same
	// contract as PlanningExplicit below.
	RequestTimeoutExplicit bool `json:"-"`
	// PlanningExplicit records that EnablePlanning was set deliberately by the
	// caller, so NewRunner must not override it for conductor models. Not
	// serialised: it describes how this run was launched, not its state.
	PlanningExplicit bool `json:"-"`
	// VerificationExplicit is the same guarantee for RequireVerification.
	VerificationExplicit bool          `json:"-"`
	VerificationCommands []string      `json:"verification_commands,omitempty"`
	VerifyTimeout        time.Duration `json:"verify_timeout"`
	// Model overrides the LLM model for this run (the model-router seam). Empty
	// uses cfg.Model. Agent/orchestrate/subagent callers set this to
	// cfg.ResolveAgentModel() so agent work can use a tool-capable/reasoning model.
	Model string `json:"model,omitempty"`
	// AutoApproveTools runs the permission checker in Trust mode (allow all).
	// Set for subagents, which are headless and would otherwise deny every
	// write/exec tool ("Ask" with no prompt). Spawning the subagent is the
	// approval. Never set for the interactive main agent.
	AutoApproveTools bool `json:"auto_approve_tools"`
	// Sliders overrides slider.json for this run's voice modulation (a
	// subagent's persona override). Nil uses slider.json.
	Sliders *config.SliderConfig `json:"-"`
	// ExtraTools are registered on the run's own registry before ToolFilter
	// applies (a typed subagent's submit_result, 2.0 W4e).
	ExtraTools []tools.Tool `json:"-"`
	// ToolFilter, when set, keeps only the tools it accepts in the run's own
	// registry (Registry.Retain); a call to any other tool fails as unknown.
	// The parent's registry is never touched. Typed subagents set it (2.0 W4e).
	ToolFilter func(tools.Tool) bool `json:"-"`
	// PersonaLevel is the system prompt's persona level: empty is the full
	// persona; the explore and review subagent types and orchestrator lanes
	// set prompts.PersonaOff (identity, honesty rule and voice boundary). The
	// agent contract stays.
	PersonaLevel prompts.PersonaLevel `json:"-"`
	// PromptFunc asks the user to approve a tool the permission policy
	// resolves to Ask. The TUI's /agent sets it to its permission modal (#172);
	// without it, Ask means deny.
	PromptFunc tools.PromptFunc `json:"-"`
	// Nested marks a runner started by another run (a subagent, an
	// orchestrator lane, the TUI's /agent). It skips SessionStart and Stop
	// hooks, which belong to the top-level run; a subagent fires
	// SubagentStop instead (AgentID).
	Nested bool `json:"-"`
	// AgentID names a subagent for SubagentStop hooks: the ID spawn_agent
	// returned, kept across a resume. A Nested runner with an AgentID fires
	// SubagentStop when it finishes as completed; one without (an
	// orchestrator lane, /agent) fires neither.
	AgentID string `json:"-"`
	// CheckGoal runs the goal through UserPromptSubmit once, before any
	// model call (2.0 F2e): set where the goal is the user's own text
	// (celeste agent, MCP mode:"agent", /agent). A blocked goal ends RunGoal
	// with ErrGoalBlocked. Subagents and orchestrator lanes leave it false:
	// a model wrote their goal. Resume never re-checks.
	CheckGoal bool `json:"-"`
	// Warn receives setup and hook warnings. Nil writes them to errOut.
	Warn func(string) `json:"-"`
	// ParentEnv, when set, is the environment of the run that started this
	// one (the subagent manager's or an orchestrator run's loop.Parent, or a
	// Setup Env). NewRunner builds its Env with ParentEnv.Nested instead of
	// loop.Setup, sharing MCP clients, hooks and the code graph, and the run
	// is Nested. The runner never closes ParentEnv; its owner does.
	ParentEnv loop.Nester `json:"-"`
	// ResumeRunID is the run a Resume will continue: its ID, not a new
	// one, names the run's session (hooks' session_id and file
	// checkpoints, 2.0 F4). Ignored with ParentEnv (the parent's session).
	ResumeRunID string `json:"-"`
	// Client, when set, is used instead of building an llm.Client from the
	// config. Tests inject a client around a fake backend (2.0 F1).
	Client *llm.Client `json:"-"`
	// FailOnBlockedTools makes NewRunner refuse to start when the policy would
	// send a mutating tool to an approval prompt that does not exist. Set by the
	// `celeste agent` CLI, which can offer -auto-approve as the remedy. Other
	// callers (TUI /agent, orchestrator) leave it false so this does not change
	// their behaviour; they share the same underlying gap, tracked separately.
	FailOnBlockedTools bool   `json:"fail_on_blocked_tools"`
	EmitArtifacts      bool   `json:"emit_artifacts"`
	ArtifactDir        string `json:"artifact_dir,omitempty"`
	DisableCheckpoints bool   `json:"disable_checkpoints"`
	Verbose            bool   `json:"verbose"`
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
	// Dropped: a stream rule cut this reply short and the turn re-runs
	// (2.0 W3). The provider billed it; nothing else about it is kept.
	Dropped bool
}

func DefaultOptions() Options {
	return Options{
		MaxTurns:                      50,
		MaxToolCallsPerTurn:           8,
		MaxConsecutiveNoToolTurns:     3,
		MaxConsecutiveInvalidToolArgs: 3,
		RequestTimeout:                90 * time.Second,
		ToolTimeout:                   45 * time.Second,
		RequireCompletionMarker:       true,
		CompletionMarker:              "TASK_COMPLETE:",
		EnablePlanning:                true,
		PlanMaxSteps:                  8,
		RequireVerification:           false,
		VerificationCommands:          nil,
		VerifyTimeout:                 120 * time.Second,
		EmitArtifacts:                 true,
		ArtifactDir:                   "",
		DisableCheckpoints:            false,
		Verbose:                       true,
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
	RunID                      string              `json:"run_id"`
	Goal                       string              `json:"goal"`
	Status                     string              `json:"status"`
	CreatedAt                  time.Time           `json:"created_at"`
	UpdatedAt                  time.Time           `json:"updated_at"`
	CompletedAt                *time.Time          `json:"completed_at,omitempty"`
	Turn                       int                 `json:"turn"`
	ConsecutiveNoToolTurns     int                 `json:"consecutive_no_tool_turns"`
	ConsecutiveInvalidToolArgs int                 `json:"consecutive_invalid_tool_args"`
	ToolCallCount              int                 `json:"tool_call_count"`
	Messages                   []tui.ChatMessage   `json:"messages"`
	Steps                      []Step              `json:"steps"`
	Phase                      string              `json:"phase"`
	Plan                       []PlanStep          `json:"plan,omitempty"`
	ActivePlanStep             int                 `json:"active_plan_step,omitempty"`
	Verification               []VerificationCheck `json:"verification,omitempty"`
	LastAssistantResponse      string              `json:"last_assistant_response,omitempty"`
	ArtifactBundlePath         string              `json:"artifact_bundle_path,omitempty"`
	Error                      string              `json:"error,omitempty"`
	// StopReason is the loop's reason when a guard stopped the run
	// ("identical", "progress", "invalid_args"); empty otherwise.
	StopReason string `json:"stop_reason,omitempty"`
	// GateVetoes counts completions the gate rejected (2.0 W3; at most one).
	GateVetoes int     `json:"gate_vetoes,omitempty"`
	Options    Options `json:"options"`
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
	return fmt.Sprintf("%s-%d", t.Format("20060102-150405.000000000"), runIDSeq.Add(1))
}

// KeepTurnStats stores st as its turn's stats for a progress display,
// unless it is a reply a stream rule dropped (2.0 W3): that turn re-runs,
// and the re-run's stats are the ones to show.
func KeepTurnStats(m map[int]TurnStats, st TurnStats) {
	if st.Dropped {
		return
	}
	m[st.Turn] = st
}
