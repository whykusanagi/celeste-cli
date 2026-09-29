package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
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
	Lane    TaskLane
	Primary string        // final response from primary agent
	Verdict *DebateResult // nil when no debate was run
}

// Orchestrator manages multi-model agent execution.
type Orchestrator struct {
	cfg           *config.Config
	router        *Router
	runnerFactory RunnerFactory // WithRunnerFactory; nil = real agent lanes
	debateRounds  int

	// mu serializes event delivery: emit holds it while it updates the
	// active token accumulators and calls onEvent, so lanes, tool
	// goroutines and OnEvent never race.
	mu      sync.Mutex
	onEvent func(OrchestratorEvent)
	accs    []*tokenAcc
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
// runGoalAccumStats call. Every event goes through here, and every event it
// is given is delivered. Not reentrant: it holds o.mu while the callback runs.
func (o *Orchestrator) emit(e OrchestratorEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, a := range o.accs {
		a.in += e.InputTokens
		a.out += e.OutputTokens
	}
	o.onEvent(e)
}

// Run classifies the goal, routes to models, executes the primary agent,
// and optionally runs a reviewer debate. Returns the final Result.
func (o *Orchestrator) Run(ctx context.Context, goal string) (*Result, error) {
	// 1. Classify
	lane, confidence := ClassifyHeuristic(goal)
	o.emit(OrchestratorEvent{Kind: EventClassified, Lane: lane, Text: fmt.Sprintf("%.0f%% confidence", confidence*100)})

	// 2. Route
	assignment, err := o.router.Resolve(lane)
	if err != nil {
		o.emit(OrchestratorEvent{Kind: EventError, Text: err.Error()})
		return nil, err
	}

	// 3. Run primary agent
	o.emit(OrchestratorEvent{Kind: EventAction, Lane: lane, Model: assignment.Primary, Text: fmt.Sprintf("[%s] primary agent", assignment.Primary)})
	primary := o.makeRunner(assignment.Primary, assignment.PrimaryBaseURL, assignment.PrimaryAPIKey)
	primaryResponse, err := primary.RunGoal(ctx, goal)
	if err != nil {
		o.emit(OrchestratorEvent{Kind: EventError, Text: err.Error()})
		return nil, fmt.Errorf("primary agent failed: %w", err)
	}

	result := &Result{Lane: lane, Primary: primaryResponse}

	// 4. Debate (code/review lanes with a configured reviewer only)
	if assignment.HasReviewer() && (lane == LaneCode || lane == LaneReview) {
		verdict, debateErr := o.runDebate(ctx, goal, primaryResponse, assignment)
		if debateErr != nil {
			// Debate failure is non-fatal — emit warning and continue.
			o.emit(OrchestratorEvent{Kind: EventError, Text: fmt.Sprintf("debate skipped: %v", debateErr)})
		} else {
			result.Verdict = verdict
		}
	}

	o.emit(OrchestratorEvent{Kind: EventComplete, Lane: lane, Text: result.Primary})
	return result, nil
}

// makeRunner creates an AgentRunner for model. A base URL or API key
// override (cross-provider orchestration) always gets a real agent lane;
// otherwise a WithRunnerFactory factory, when set, makes it.
func (o *Orchestrator) makeRunner(model, baseURL, apiKey string) AgentRunner {
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
	return defaultRunnerFactory(cfg, o.emit)(model)
}

func (o *Orchestrator) runDebate(ctx context.Context, goal, primaryOutput string, assignment ModelAssignment) (*DebateResult, error) {
	dm := NewDebateManager(DebateOptions{MaxRounds: o.debateRounds})
	reviewer := o.makeRunner(assignment.Reviewer, assignment.ReviewerBaseURL, assignment.ReviewerAPIKey)

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
			if len(reviewSummary) > 100 {
				reviewSummary = reviewSummary[:100] + "…"
			}
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
		defenseRunner := o.makeRunner(assignment.Primary, "", "")
		defenseOutput, defenseElapsed, defenseIn, defenseOut, err := o.runGoalAccumStats(ctx, defenseRunner, defensePrompt)
		if err != nil {
			return nil, fmt.Errorf("primary defense round %d failed: %w", round, err)
		}
		dm.AddTurn(DebateTurn{Round: round, Role: RolePrimary, Input: defensePrompt, Output: defenseOutput})
		defensePreview := strings.TrimSpace(defenseOutput)
		if nl := strings.IndexByte(defensePreview, '\n'); nl > 0 {
			defensePreview = defensePreview[:nl]
		}
		if len(defensePreview) > 100 {
			defensePreview = defensePreview[:100] + "…"
		}
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
func (o *Orchestrator) runGoalAccumStats(ctx context.Context, runner AgentRunner, goal string) (output string, elapsed time.Duration, totalIn, totalOut int, err error) {
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
