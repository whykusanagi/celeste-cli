package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/textutil"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/loop"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
)

// AgentRunner is the interface the orchestrator uses to execute a goal.
// The real implementation wraps agent.Runner; tests supply fakes.
type AgentRunner interface {
	RunGoal(ctx context.Context, goal string) (string, error)
}

// RunnerFactory creates an AgentRunner for the given model name.
type RunnerFactory func(model string) AgentRunner

// Result is the final output of an orchestrator run.
type Result struct {
	Lane     TaskLane
	Primary  string        // final response from primary agent
	Verdict  *DebateResult // nil when no debate was run
	Approval Approval      // how the lanes answered tools that need approval
}

// Approval is how the lanes answer a tool call the permission policy
// resolves to Ask. Spec §4 F2: the orchestrator inherits its caller's Gate.
type Approval int

const (
	// ApprovalDeny is the headless default: no caller prompt, so Asks are
	// denied. Run says so in an event and records it in Result.Approval.
	ApprovalDeny Approval = iota
	// ApprovalPrompt asks the caller's prompt (the TUI's permission modal).
	ApprovalPrompt
)

func (a Approval) String() string {
	switch a {
	case ApprovalPrompt:
		return "prompt"
	default:
		return "deny"
	}
}

// headlessDenyNotice is emitted once per headless Run.
const headlessDenyNotice = "⚠ no approval prompt: tools that need approval will be denied"

// WithPrompt makes the lanes ask fn (the TUI's permission modal) for tools
// the policy resolves to Ask. A nil fn keeps the headless default.
func WithPrompt(fn tools.PromptFunc) Option {
	return func(o *Orchestrator) {
		if fn == nil {
			o.approval, o.prompt = ApprovalDeny, nil
			return
		}
		o.approval, o.prompt = ApprovalPrompt, fn
	}
}

// newLanes builds one Run's shared lane environment. A var so tests can
// observe it.
var newLanes = loop.NewParent

// laneInheritance is what a lane inherits from its orchestrator run: the
// caller's approval (spec §4 F2) and the run's shared environment.
type laneInheritance struct {
	Approval Approval
	Prompt   tools.PromptFunc
	Env      loop.Nester // nil: the lane builds its own (loop.Setup)
}

// laneInherit is what every real lane of a Run nesting under env inherits.
func (o *Orchestrator) laneInherit(env loop.Nester) laneInheritance {
	return laneInheritance{Approval: o.approval, Prompt: o.prompt, Env: env}
}

// Orchestrator manages multi-model agent execution.
type Orchestrator struct {
	cfg           *config.Config
	router        *Router
	runnerFactory RunnerFactory // WithRunnerFactory; nil = real agent lanes
	debateRounds  int
	approval      Approval
	prompt        tools.PromptFunc

	// mu serializes event delivery: emit holds it while it updates the
	// run's active token accumulators and calls onEvent, so lanes, tool
	// goroutines, concurrent Runs and OnEvent never race.
	mu      sync.Mutex
	onEvent func(OrchestratorEvent)
}

// orchRun is one Run: its events go to the Orchestrator's callback, and its
// token accumulators are its own, so concurrent Runs never count each
// other's tokens.
type orchRun struct {
	*Orchestrator
	accs []*tokenAcc // guarded by Orchestrator.mu
}

// tokenAcc sums the tokens of the events emitted while one runGoalAccumStats
// call is running.
type tokenAcc struct{ in, out int }

// Option configures an Orchestrator.
type Option func(*Orchestrator)

// WithRunnerFactory overrides the AgentRunner factory (useful in tests).
func WithRunnerFactory(f RunnerFactory) Option {
	return func(o *Orchestrator) { o.runnerFactory = f }
}

// New creates an Orchestrator backed by the given config.
func New(cfg *config.Config, opts ...Option) *Orchestrator {
	rounds := 3
	if cfg.Orchestrator != nil && cfg.Orchestrator.DebateRounds > 0 {
		rounds = cfg.Orchestrator.DebateRounds
	}
	o := &Orchestrator{
		cfg:          cfg,
		router:       NewRouter(cfg),
		debateRounds: rounds,
		onEvent:      func(OrchestratorEvent) {},
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// OnEvent registers a callback for all orchestrator events. Safe to call at
// any time. Events are delivered one at a time, under a lock: fn must not
// call back into the Orchestrator.
func (o *Orchestrator) OnEvent(fn func(OrchestratorEvent)) {
	if fn == nil {
		fn = func(OrchestratorEvent) {}
	}
	o.mu.Lock()
	o.onEvent = fn
	o.mu.Unlock()
}

// emit delivers e to the caller and adds its tokens to every running
// runGoalAccumStats call of this run. Every event goes through here, and every event it
// is given is delivered. Not reentrant: it holds o.mu while the callback runs.
func (o *orchRun) emit(e OrchestratorEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, a := range o.accs {
		a.in += e.InputTokens
		a.out += e.OutputTokens
	}
	o.onEvent(e)
}

// noLaneMatched is the EventClassified text for a goal no lane matched.
const noLaneMatched = "no lane matched · default model"

// Run classifies the goal, routes to models, executes the primary agent,
// and optionally runs a reviewer debate. Every real lane of the run nests
// under one environment (loop.Parent). That environment is local to this
// call, so concurrent Runs never close each other's lanes, and it is closed
// before the terminal event (EventComplete or EventError), so nothing from
// it reaches a caller that stops reading there.
func (orch *Orchestrator) Run(ctx context.Context, goal string) (*Result, error) {
	o := &orchRun{Orchestrator: orch}
	ws, err := os.Getwd()
	if err != nil {
		o.emit(OrchestratorEvent{Kind: EventError, Text: err.Error()})
		return nil, err
	}
	lanes := newLanes(o.cfg, ws, loop.SetupOptions{
		// One session_id for every lane's hooks.
		SessionID: "orchestrator-" + config.UniqueNanoID(),
		// Load-time warnings only (hooks skipped, MCP failures): a lane's
		// hook warnings follow its own runner (hooks.WithWarn).
		Warn: func(s string) { o.emit(OrchestratorEvent{Kind: EventAction, Text: "⚠ " + s}) },
	})
	result, end, err := o.run(ctx, goal, o.laneInherit(lanes))
	lanes.Close()
	o.emit(end)
	return result, err
}

// run is Run up to its terminal event, which it returns for Run to emit
// once the lanes' environment is closed.
func (o *orchRun) run(ctx context.Context, goal string, li laneInheritance) (*Result, OrchestratorEvent, error) {
	// 1. Classify
	lane, confidence, note := Classify(ctx, goal, o.cfg.JevRouteMode(), func(s string) {
		o.emit(OrchestratorEvent{Kind: EventAction, Text: "⚠ " + s})
	})
	classified := fmt.Sprintf("%.0f%% confidence", confidence*100)
	if lane == LaneUnknown {
		// No lane keyword matched: the heuristic's 0.1 is a placeholder,
		// not a confidence, and the run uses the default model.
		classified = noLaneMatched
	}
	o.emit(OrchestratorEvent{Kind: EventClassified, Lane: lane, Text: classified})
	if note != "" {
		o.emit(OrchestratorEvent{Kind: EventAction, Lane: lane, Text: note})
	}
	if o.approval == ApprovalDeny {
		o.emit(OrchestratorEvent{Kind: EventAction, Lane: lane, Text: headlessDenyNotice})
	}

	// 2. Route
	assignment, err := o.router.Resolve(ctx, lane)
	if err != nil {
		return nil, OrchestratorEvent{Kind: EventError, Text: err.Error()}, err
	}
	for _, n := range assignment.Notes {
		o.emit(OrchestratorEvent{Kind: EventAction, Lane: lane, Text: "⚠ " + n})
	}

	// 3. Run primary agent
	o.emit(OrchestratorEvent{Kind: EventAction, Lane: lane, Model: assignment.Primary, Text: "primary agent"}) // the TUI prefixes Model
	primary := o.makeRunner(li, assignment.Primary, assignment.PrimaryBaseURL, assignment.PrimaryAPIKey)
	primaryResponse, err := primary.RunGoal(ctx, goal)
	if err != nil {
		return nil, OrchestratorEvent{Kind: EventError, Text: err.Error()}, fmt.Errorf("primary agent failed: %w", err)
	}

	result := &Result{Lane: lane, Primary: primaryResponse, Approval: o.approval}

	// 4. Debate (code/review lanes with a configured reviewer only)
	if assignment.HasReviewer() && (lane == LaneCode || lane == LaneReview) {
		verdict, debateErr := o.runDebate(ctx, li, goal, primaryResponse, assignment)
		if debateErr != nil {
			// Debate failure is non-fatal: a notice, not EventError, which
			// callers (the TUI) treat as the end of the run.
			o.emit(OrchestratorEvent{Kind: EventAction, Lane: lane, Text: fmt.Sprintf("⚠ debate skipped: %v", debateErr)})
		} else {
			result.Verdict = verdict
		}
	}

	return result, OrchestratorEvent{Kind: EventComplete, Lane: lane, Text: result.Primary}, nil
}

// makeRunner creates an AgentRunner for model that inherits li. A base URL
// or API key override (cross-provider orchestration) always gets a real
// agent lane; otherwise a WithRunnerFactory factory, when set, makes it.
func (o *orchRun) makeRunner(li laneInheritance, model, baseURL, apiKey string) AgentRunner {
	cfg := o.cfg
	if baseURL != "" || apiKey != "" {
		c := *o.cfg
		if baseURL != "" {
			c.BaseURL = baseURL
		}
		if apiKey != "" {
			c.APIKey = apiKey
		}
		cfg = &c
	} else if o.runnerFactory != nil {
		return o.runnerFactory(model)
	}
	return defaultRunnerFactory(cfg, o.emit, li)(model)
}

func (o *orchRun) runDebate(ctx context.Context, li laneInheritance, goal, primaryOutput string, assignment ModelAssignment) (*DebateResult, error) {
	dm := NewDebateManager(DebateOptions{MaxRounds: o.debateRounds})
	reviewer := o.makeRunner(li, assignment.Reviewer, assignment.ReviewerBaseURL, assignment.ReviewerAPIKey)

	reviewPrompt := fmt.Sprintf(
		"You are reviewing code produced by another model. Evaluate purely on correctness, security, and clarity.\n\nOriginal goal: %s\n\nOutput to review:\n%s\n\nList any issues as JSON: [{\"file\":\"\",\"line\":0,\"severity\":\"low|medium|high\",\"description\":\"\"}]",
		goal, primaryOutput,
	)

	// Track last parsed issues so the max-rounds path uses real data, not an empty list.
	var lastIssues []Issue

	for round := 1; round <= o.debateRounds; round++ {
		o.emit(OrchestratorEvent{Kind: EventDebateStart, Model: assignment.Reviewer, Text: fmt.Sprintf("round %d", round)})

		reviewOutput, reviewElapsed, reviewIn, reviewOut, err := o.runGoalAccumStats(ctx, reviewer, reviewPrompt)
		if err != nil {
			return nil, fmt.Errorf("reviewer round %d failed: %w", round, err)
		}
		dm.AddTurn(DebateTurn{Round: round, Role: RoleReviewer, Input: reviewPrompt, Output: reviewOutput})

		// Parse issues before emitting so the action feed shows a readable summary.
		lastIssues = parseIssues(reviewOutput)
		var reviewSummary string
		if len(lastIssues) == 0 {
			reviewSummary = "no issues found"
		} else {
			reviewSummary = fmt.Sprintf("%d issue(s): %s", len(lastIssues), lastIssues[0].Description)
			reviewSummary = eventPreview(reviewSummary)
		}
		o.emit(OrchestratorEvent{Kind: EventReviewDraft, Model: assignment.Reviewer, Text: reviewSummary, Response: reviewOutput, Duration: reviewElapsed, InputTokens: reviewIn, OutputTokens: reviewOut})
		verdict := dm.Verdict(lastIssues)

		if verdict.Kind == VerdictApproved {
			o.emit(OrchestratorEvent{Kind: EventVerdict, Model: assignment.Reviewer, Score: verdict.Score, Text: "approved", Duration: reviewElapsed, InputTokens: reviewIn, OutputTokens: reviewOut})
			return &verdict, nil
		}
		if verdict.Kind == VerdictContested {
			o.emit(OrchestratorEvent{Kind: EventVerdict, Model: assignment.Reviewer, Score: verdict.Score, Text: "contested", Duration: reviewElapsed, InputTokens: reviewIn, OutputTokens: reviewOut})
			return &verdict, nil
		}

		// Primary agent responds to critique
		defensePrompt := fmt.Sprintf("The reviewer found these issues:\n%s\n\nAddress each issue and provide the corrected output.", reviewOutput)
		defenseRunner := o.makeRunner(li, assignment.Primary, assignment.PrimaryBaseURL, assignment.PrimaryAPIKey)
		defenseOutput, defenseElapsed, defenseIn, defenseOut, err := o.runGoalAccumStats(ctx, defenseRunner, defensePrompt)
		if err != nil {
			return nil, fmt.Errorf("primary defense round %d failed: %w", round, err)
		}
		dm.AddTurn(DebateTurn{Round: round, Role: RolePrimary, Input: defensePrompt, Output: defenseOutput})
		defensePreview := strings.TrimSpace(defenseOutput)
		if nl := strings.IndexByte(defensePreview, '\n'); nl > 0 {
			defensePreview = defensePreview[:nl]
		}
		defensePreview = eventPreview(defensePreview)
		o.emit(OrchestratorEvent{Kind: EventDefense, Model: assignment.Primary, Text: defensePreview, Response: defenseOutput, Duration: defenseElapsed, InputTokens: defenseIn, OutputTokens: defenseOut})
		reviewPrompt = fmt.Sprintf("Review the revised output:\n%s", defenseOutput)
	}

	// Use the last set of parsed issues (not empty) so VerdictContested is returned correctly.
	verdict := dm.Verdict(lastIssues)
	o.emit(OrchestratorEvent{Kind: EventVerdict, Score: verdict.Score, Text: "max rounds reached"})
	return &verdict, nil
}

// runGoalAccumStats runs runner.RunGoal and sums the tokens of the events
// emitted meanwhile; every event still reaches the caller in real time. It
// registers an accumulator instead of replacing onEvent, so an event from
// another goroutine never races a swap. A lane's runner is closed before
// RunGoal returns, so none of its callbacks arrive after the accumulator is
// removed.
func (o *orchRun) runGoalAccumStats(ctx context.Context, runner AgentRunner, goal string) (output string, elapsed time.Duration, totalIn, totalOut int, err error) {
	acc := &tokenAcc{}
	o.mu.Lock()
	o.accs = append(o.accs, acc)
	o.mu.Unlock()
	start := time.Now()
	output, err = runner.RunGoal(ctx, goal)
	elapsed = time.Since(start)
	o.mu.Lock()
	for i, a := range o.accs {
		if a == acc {
			o.accs = append(o.accs[:i], o.accs[i+1:]...)
			break
		}
	}
	totalIn, totalOut = acc.in, acc.out
	o.mu.Unlock()
	return
}

// parseIssues extracts Issue structs from a JSON array in the reviewer's response.
// Returns empty slice on parse failure (non-fatal).
func parseIssues(text string) []Issue {
	start := -1
	depth := 0
	for i, ch := range text {
		if ch == '[' {
			if depth == 0 {
				start = i
			}
			depth++
		} else if ch == ']' {
			depth--
			if depth == 0 && start >= 0 {
				var issues []Issue
				_ = json.Unmarshal([]byte(text[start:i+1]), &issues)
				return issues
			}
		}
	}
	return nil
}

// eventPreview caps a review or defense line for the action feed at 100
// bytes, marking the cut with "…" and never splitting a UTF-8 character.
func eventPreview(s string) string {
	if len(s) <= 100 {
		return s
	}
	return textutil.CutBytes(s, 100) + "…"
}
