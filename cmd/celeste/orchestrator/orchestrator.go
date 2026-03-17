package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
	runnerFactory RunnerFactory
	onEvent       func(OrchestratorEvent)
	debateRounds  int
}

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
	if o.runnerFactory == nil {
		// Pass a closure that always calls the current o.onEvent, so that
		// OnEvent() calls made after New() are still honoured.
		o.runnerFactory = defaultRunnerFactory(cfg, func(e OrchestratorEvent) { o.onEvent(e) })
	}
	return o
}

// OnEvent registers a callback for all orchestrator events.
func (o *Orchestrator) OnEvent(fn func(OrchestratorEvent)) {
	o.onEvent = fn
}

// Run classifies the goal, routes to models, executes the primary agent,
// and optionally runs a reviewer debate. Returns the final Result.
func (o *Orchestrator) Run(ctx context.Context, goal string) (*Result, error) {
	// 1. Classify
	lane, confidence := ClassifyHeuristic(goal)
	o.onEvent(OrchestratorEvent{Kind: EventClassified, Lane: lane, Text: fmt.Sprintf("%.0f%% confidence", confidence*100)})

	// 2. Route
	assignment, err := o.router.Resolve(lane)
	if err != nil {
		o.onEvent(OrchestratorEvent{Kind: EventError, Text: err.Error()})
		return nil, err
	}

	// 3. Run primary agent
	o.onEvent(OrchestratorEvent{Kind: EventAction, Lane: lane, Model: assignment.Primary, Text: fmt.Sprintf("[%s] primary agent", assignment.Primary)})
	primary := o.makeRunner(assignment.Primary, assignment.PrimaryBaseURL, assignment.PrimaryAPIKey)
	primaryResponse, err := primary.RunGoal(ctx, goal)
	if err != nil {
		o.onEvent(OrchestratorEvent{Kind: EventError, Text: err.Error()})
		return nil, fmt.Errorf("primary agent failed: %w", err)
	}

	result := &Result{Lane: lane, Primary: primaryResponse}

	// 4. Debate (code/review lanes with a configured reviewer only)
	if assignment.HasReviewer() && (lane == LaneCode || lane == LaneReview) {
		verdict, debateErr := o.runDebate(ctx, goal, primaryResponse, assignment)
		if debateErr != nil {
			// Debate failure is non-fatal — emit warning and continue.
			o.onEvent(OrchestratorEvent{Kind: EventError, Text: fmt.Sprintf("debate skipped: %v", debateErr)})
		} else {
			result.Verdict = verdict
		}
	}

	o.onEvent(OrchestratorEvent{Kind: EventComplete, Lane: lane, Text: result.Primary})
	return result, nil
}

// makeRunner creates an AgentRunner for the given model, optionally overriding
// the base URL and API key for cross-provider orchestration.
func (o *Orchestrator) makeRunner(model, baseURL, apiKey string) AgentRunner {
	if baseURL != "" || apiKey != "" {
		cfg := *o.cfg
		if baseURL != "" {
			cfg.BaseURL = baseURL
		}
		if apiKey != "" {
			cfg.APIKey = apiKey
		}
		return defaultRunnerFactory(&cfg, func(e OrchestratorEvent) { o.onEvent(e) })(model)
	}
	return o.runnerFactory(model)
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
		o.onEvent(OrchestratorEvent{Kind: EventDebateStart, Model: assignment.Reviewer, Text: fmt.Sprintf("round %d", round)})

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
		o.onEvent(OrchestratorEvent{Kind: EventReviewDraft, Model: assignment.Reviewer, Text: reviewSummary, Response: reviewOutput, Duration: reviewElapsed, InputTokens: reviewIn, OutputTokens: reviewOut})
		verdict := dm.Verdict(lastIssues)

		if verdict.Kind == VerdictApproved {
			o.onEvent(OrchestratorEvent{Kind: EventVerdict, Model: assignment.Reviewer, Score: verdict.Score, Text: "approved", Duration: reviewElapsed, InputTokens: reviewIn, OutputTokens: reviewOut})
			return &verdict, nil
		}
		if verdict.Kind == VerdictContested {
			o.onEvent(OrchestratorEvent{Kind: EventVerdict, Model: assignment.Reviewer, Score: verdict.Score, Text: "contested", Duration: reviewElapsed, InputTokens: reviewIn, OutputTokens: reviewOut})
			return &verdict, nil
		}

		// Primary agent responds to critique
		defensePrompt := fmt.Sprintf("The reviewer found these issues:\n%s\n\nAddress each issue and provide the corrected output.", reviewOutput)
		defenseRunner := o.runnerFactory(assignment.Primary)
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
		o.onEvent(OrchestratorEvent{Kind: EventDefense, Model: assignment.Primary, Text: defensePreview, Response: defenseOutput, Duration: defenseElapsed, InputTokens: defenseIn, OutputTokens: defenseOut})
		reviewPrompt = fmt.Sprintf("Review the revised output:\n%s", defenseOutput)
	}

	// Use the last set of parsed issues (not empty) so VerdictContested is returned correctly.
	verdict := dm.Verdict(lastIssues)
	o.onEvent(OrchestratorEvent{Kind: EventVerdict, Score: verdict.Score, Text: "max rounds reached"})
	return &verdict, nil
}

// runGoalAccumStats runs runner.RunGoal while intercepting all emitted events to
// accumulate total token counts and elapsed time. All events are still forwarded
// to the original o.onEvent so the TUI receives per-turn progress in real time.
func (o *Orchestrator) runGoalAccumStats(ctx context.Context, runner AgentRunner, goal string) (output string, elapsed time.Duration, totalIn, totalOut int, err error) {
	saved := o.onEvent
	o.onEvent = func(e OrchestratorEvent) {
		totalIn += e.InputTokens
		totalOut += e.OutputTokens
		saved(e)
	}
	start := time.Now()
	output, err = runner.RunGoal(ctx, goal)
	elapsed = time.Since(start)
	o.onEvent = saved
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
