package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/codegraph"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/compact"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	ctxmgr "github.com/whykusanagi/celeste-cli/cmd/celeste/context"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/decide"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/shellrun"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/jev"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/loop"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/permissions"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/providers"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/rules"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/steer"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

const maxCommandOutput = 12_000

type Runner struct {
	client   *llm.Client
	registry *tools.Registry
	store    *CheckpointStore
	options  Options
	out      io.Writer
	errOut   io.Writer
	budget   *ctxmgr.TokenBudget
	indexer  *codegraph.Indexer // code graph indexer, may be nil
	// pruned holds tool results that compaction removed from the history,
	// for recall_tool_result (#174). Nil disables pruning.
	pruned *compact.Store
	// summarize writes a compaction summary with the small-model role, the
	// rung after pruning (#174). Nil disables summaries.
	summarize compact.SummarizeFunc
	// jev is set when jev_prune is "shadow" (pruning logs Jev's verdict next
	// to the rules', #175) or "on" (Jev orders the elisions, 2.0 W3).
	jev     *jev.Client
	jevMode string
	// env is the loop.Setup environment (registry, MCP, code graph, hooks).
	// Nil for runners built directly in tests.
	env *loop.Env
	// hooks is env.Hooks (nil = no hooks); warn is the warning sink.
	hooks *hooks.Runner
	warn  func(string)
	// gate serializes Warn, OnProgress and OnTurnStats; Close shuts it.
	gate *callbackGate
	// firstRunID is the hooks' session_id; the first RunGoal adopts it.
	firstRunID string
	// rulesMode is stream_rules (2.0 W3): off, shadow or on.
	rulesMode string
	// watchdog is the watchdog mode and oracle answers its ballot (2.0 W3).
	watchdog string
	oracle   decide.Oracle
	gateMode string // completion_gate (2.0 W3)
	jevGate  string // jev_gate (2.0 W3)
}

// compactMessages keeps the history inside the window (#174). It prunes old
// tool results when the history is over the compaction threshold, or
// unconditionally when force is set (after a context-overflow error); if
// that isn't enough it summarizes everything but the newest ~20k tokens. It
// returns the history, progress notes for the event stream, and whether it
// changed. It runs on the loop goroutine and must not write r.out.
func (r *Runner) compactMessages(ctx context.Context, msgs []tui.ChatMessage, meter *compact.Meter, force bool) ([]tui.ChatMessage, []string, bool) {
	// A nil prune store only disables pruning (Prune is a no-op without
	// one); the summary rung below must still run.
	if r.budget == nil || (r.pruned == nil && r.summarize == nil) {
		return msgs, nil, false
	}
	var notes []string
	meter.Overhead = r.budget.SystemPromptTokens + r.budget.ToolDefinitionTokens
	used := meter.Used(msgs)
	// What the provider counts beyond the history estimate (the system
	// prompt and tools, or more) still counts after a prune, as in the
	// chat's compactWith.
	overhead := used - compact.Estimate(msgs)
	// Shadow reports inline: errOut may be a caller's bytes.Buffer, and the
	// run must not outlive its output.
	opts, report := compact.WithJev(ctx, r.jev, r.jevMode, msgs, compact.Options{Window: r.budget.ModelLimit, Used: used, Unseen: meter.Unseen(msgs), Force: force}, func(line string) {
		fmt.Fprintf(r.errOut, "[agent] %s\n", line)
	}, false)
	pruned, res := compact.Prune(msgs, opts, r.pruned)
	report(res)
	changed := res.Pruned()
	if changed {
		msgs = pruned
		r.budget.RecordCompaction(compact.Estimate(msgs))
		notes = append(notes, "context compacted: "+res.Summary())
	}

	// Next rung: summarize when pruning wasn't enough, or when a forced
	// compaction (after an overflow) found nothing to prune.
	stillOver := compact.Estimate(msgs)+overhead > compact.Threshold(r.budget.ModelLimit)
	if r.summarize != nil && (stillOver || (force && !changed)) {
		summarize, blocked := r.hookedSummarize(r.summarize)
		sctx, cancel := context.WithTimeout(ctx, summaryTimeout)
		out, sres, err := compact.Summarize(sctx, msgs, compact.SummaryOptions{Window: r.budget.ModelLimit, State: r.renderState()}, summarize)
		cancel()
		if reason := blocked(); reason != "" {
			// Always reported, not only in verbose output (TUI parity).
			r.warning("compaction blocked by a PreCompact hook: " + reason)
			return msgs, notes, changed
		}
		if err != nil {
			if !errors.Is(err, compact.ErrNothingToSummarize) {
				notes = append(notes, "context summary failed: "+err.Error())
			}
			return msgs, notes, changed
		}
		msgs = out
		r.budget.RecordCompaction(sres.TokensAfter)
		notes = append(notes, "context compacted: "+sres.Line())
		r.postCompact(ctx, sres.Summary)
		changed = true
	}
	return msgs, notes, changed
}

// renderState is the run's authoritative state for summaries (#200).
func (r *Runner) renderState() string {
	if r.env == nil {
		return ""
	}
	return r.env.RenderState()
}

// SmallModelSummarizer returns a SummarizeFunc on its own client for the
// small-model role, so summaries don't disturb the agent client's system
// prompt or tools. The client is base.Plain(): a summary always sends its
// own system prompt, whatever the run's persona and xAI settings are.
func SmallModelSummarizer(base *llm.Config, model string) compact.SummarizeFunc {
	cfg := base.Plain()
	cfg.Model = model
	client := llm.NewClient(cfg, nil)
	return func(ctx context.Context, system, user string) (string, error) {
		client.SetSystemPrompt(system)
		res, err := client.SendMessageSync(ctx, []tui.ChatMessage{{Role: "user", Content: user, Timestamp: time.Now()}}, nil)
		if err != nil {
			return "", err
		}
		return res.Content, nil
	}
}

// summaryTimeout bounds a compaction summary request.
const summaryTimeout = 3 * time.Minute

func (r *Runner) reportCompaction(state *RunState, msg string) {
	if state.Options.Verbose {
		fmt.Fprintf(r.out, "[agent] %s\n", msg)
	}
	r.emitProgress(ProgressStepDone, msg, state.Turn, state.Options.MaxTurns)
}

// emitProgress calls r.options.OnProgress if it is set.
func (r *Runner) emitProgress(kind ProgressKind, text string, turn, maxTurns int) {
	if r.options.OnProgress != nil {
		r.options.OnProgress(kind, text, turn, maxTurns)
	}
}

// Close releases resources held by the runner (e.g. code graph DB). After it
// returns, no Warn, OnProgress or OnTurnStats call reaches the caller, even
// from a tool goroutine the run abandoned.
func (r *Runner) Close() {
	if r.env != nil {
		r.env.Close()
	} else if r.indexer != nil {
		r.indexer.Close()
	}
	if r.gate != nil {
		r.gate.close()
	}
}

// callbackGate runs the caller's callbacks one at a time and drops them once
// closed.
type callbackGate struct {
	mu     sync.Mutex
	closed bool
}

func (g *callbackGate) do(f func()) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.closed {
		f()
	}
}

func (g *callbackGate) close() {
	g.mu.Lock()
	g.closed = true
	g.mu.Unlock()
}

func (g *callbackGate) isClosed() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.closed
}

// conductorRequestTimeout is the per-turn deadline for a model that plans
// server-side. Deliberately generous: fan-out width is chosen per request, so a
// conductor's latency is nondeterministic by design and a tight deadline fights
// it rather than protecting anything. The architecture spec's guidance is
// "generous timeouts, idempotent tool execution, backoff only on genuine
// transport errors" — this is the first half.
//
// ponytail: one constant, not a per-model table. Raise it if a real workload
// exceeds it; a table is only worth it once the variants actually differ.
const conductorRequestTimeout = 300 * time.Second

// annotateTurnTimeout wraps err with actionable guidance when the agent's OWN
// per-turn deadline expired. Issue #113's second complaint was that a timeout
// "ends in the bare, uninformative error context deadline exceeded" — the retry
// fix in #122 could not address that on this path, because the agent sets its
// own deadline, so withRetry sees a cancelled parent and returns the raw error.
//
// ours must be true only when OUR deadline fired and the caller's context is
// still healthy, so a Ctrl+C or a genuine transport failure is never mislabelled
// as a timeout the user could fix by raising a flag.
func annotateTurnTimeout(err error, ours bool, timeout time.Duration) error {
	if err == nil || !ours {
		return err
	}
	return fmt.Errorf("agent turn exceeded the %s per-request timeout; raise it with -request-timeout <seconds> or reduce the work in a single turn: %w", timeout, err)
}

// maxDuration returns the larger of a and b. Used to lift a configured timeout
// to a floor without ever lowering one the user chose deliberately.
func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func NewRunner(cfg *config.Config, options Options, out io.Writer, errOut io.Writer) (*Runner, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if out == nil {
		out = os.Stdout
	}
	if errOut == nil {
		errOut = os.Stderr
	}
	// Hooks warn from tool goroutines while the run writes compaction lines.
	errOut = &syncWriter{w: errOut}

	if options.Workspace == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		options.Workspace = cwd
	}
	absWorkspace, err := filepath.Abs(options.Workspace)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace path: %w", err)
	}
	options.Workspace = filepath.Clean(absWorkspace)

	// Model-router seam: agent/orchestrate/subagent callers may override the model
	// (cfg.ResolveAgentModel()) so agent work uses a tool-capable/reasoning model
	// while chat keeps a cheaper one (task e8775b91). Resolved once, here: the
	// planner-ownership check right below and the tool-support guardrail further
	// down must agree on the model this run actually talks to, or we could decide
	// planner ownership for a model the run never calls.
	model := cfg.Model
	if options.Model != "" {
		model = options.Model
	}

	// clientTimeoutFloor raises the LLM client's per-attempt deadline for models
	// that plan server-side. Zero means "leave the configured value alone".
	var clientTimeoutFloor time.Duration

	// Planner ownership. Fugu is a conductor: it decomposes, delegates and
	// verifies server-side. Running our planning and verification phases on
	// top of that means two planners that cannot see each other, so we stand
	// ours down and act as a tool host. Every agent path reaches this
	// constructor, so deriving here covers the CLI, subagents and the server.
	if providers.OrchestratesServerSide(providers.DetectProvider(cfg.BaseURL), model) {
		// A conductor picks its fan-out width per request, so its latency is
		// variable BY DESIGN — the architecture spec calls this out as a price of
		// the offload. The 90s default is measurably too low: a substantial review
		// prompt through MCP agent mode hit that ceiling twice and died with a
		// bare deadline error. Give conductors headroom unless the caller named a
		// timeout themselves.
		//
		// BOTH deadlines have to move or neither matters. The agent's per-turn
		// context and the LLM client's per-attempt deadline (cfg.Timeout) are
		// independent, and the tighter one wins — raising only the agent's was
		// moot in practice: a real workload still died at the client's 90s and
		// reported the chat-path message. clientTimeoutFloor carries the same
		// floor into llm.Config below.
		if !options.RequestTimeoutExplicit && options.RequestTimeout < conductorRequestTimeout {
			options.RequestTimeout = conductorRequestTimeout
		}
		clientTimeoutFloor = conductorRequestTimeout
		if !options.PlanningExplicit {
			options.EnablePlanning = false
		}
		if !options.VerificationExplicit {
			options.RequireVerification = false
		}
	}

	normalizeOptions(&options)

	// One gate serializes every callback into the caller: hooks warn from
	// tool goroutines (an abandoned one can outlive the run) while the event
	// consumer reports progress, and adopters share state between these sinks
	// unsynchronized. Close shuts the gate, so nothing reaches the caller
	// once Close returns (an orchestrator restores its callbacks after).
	gate := &callbackGate{}
	warn := options.Warn
	if warn == nil {
		warn = func(s string) { fmt.Fprintf(errOut, "Warning: %s\n", s) }
	}
	userWarn := warn
	warn = func(s string) { gate.do(func() { userWarn(s) }) }
	options.Warn = warn
	if p := options.OnProgress; p != nil {
		options.OnProgress = func(kind ProgressKind, text string, turn, maxTurns int) {
			gate.do(func() { p(kind, text, turn, maxTurns) })
		}
	}
	if f := options.OnTurnStats; f != nil {
		options.OnTurnStats = func(st TurnStats) { gate.do(func() { f(st) }) }
	}
	firstRunID := generateRunID(time.Now())
	var env *loop.Env
	if options.ParentEnv != nil {
		// Part of the parent's run: no SessionStart or Stop, and the
		// parent's MCP clients, hooks and code graph instead of new ones.
		options.Nested = true
		env, err = options.ParentEnv.Nested(loop.NestedOptions{Workspace: options.Workspace, Warn: warn})
	} else {
		sessionID := firstRunID
		if options.ResumeRunID != "" {
			sessionID = options.ResumeRunID
		}
		env, err = loop.Setup(loop.ModeAgent, cfg, options.Workspace, loop.SetupOptions{SessionID: sessionID, Warn: warn})
	}
	if err != nil {
		return nil, err
	}
	// Subagents and MCP agent mode run headless (no approval modal): spawning
	// IS the approval, so run in trust mode. Deny rules still apply.
	if options.AutoApproveTools {
		env.Trust()
	} else if options.PromptFunc != nil {
		// The user's own prompt (the TUI modal behind /agent and
		// /orchestrate lanes): "always allow/deny" persists, as in the chat.
		env.PersistRules()
	}
	registry := env.Registry
	checker := env.Checker
	// A typed subagent (2.0 W4e): its extra tools join its own registry,
	// then the filter keeps only what its type allows.
	for _, t := range options.ExtraTools {
		registry.RegisterWithModes(t, tools.ModeAgent, tools.ModeChat)
	}
	registry.Retain(options.ToolFilter)

	// Fail fast rather than no-opping. `celeste agent` never wires an
	// interactive prompt, so every tool that resolves to Ask is denied — and the
	// run still reports success with exit 0 after burning the whole turn budget.
	// Under the DEFAULT policy that is every mutating tool, so an unattended
	// agent can only read. Check before turn 1 and say which flag fixes it.
	if options.FailOnBlockedTools && !options.AutoApproveTools && options.PromptFunc == nil {
		if blocked := blockedMutatingTools(registry, checker); len(blocked) > 0 {
			env.Close()
			return nil, fmt.Errorf(
				"agent mode cannot execute %s: these need interactive approval, and the agent runtime has no prompt.\n"+
					"Pass -auto-approve to run unattended (invoking the agent is the approval), "+
					"or grant them in ~/.celeste/permissions.json",
				strings.Join(blocked, ", "))
		}
	}
	// SessionStart belongs to the top-level run; nested runners skip it. It
	// fires only once the run can start, never for a refused one.
	if !options.Nested {
		env.StartSession(context.Background(), "startup")
	}

	// Guardrail: warn loudly if the chosen model doesn't support tool calling —
	// that model will flail/hallucinate in agent mode (observed with
	// non-reasoning grok failing to drive spawn_agent). model was resolved once,
	// above, so this reads the same value the planner-ownership check used.
	if provider := providers.DetectProvider(cfg.BaseURL); provider != "" {
		if !providers.NewModelDetection(provider).SupportsTools(model) {
			fmt.Fprintf(errOut, "⚠️  agent model %q (%s) may not support tool calling — agent/subagent work can fail or hallucinate; consider setting a tool-capable agent_model\n", model, provider)
		}
	}

	llmConfig := llm.ConfigFrom(cfg)
	llmConfig.Model = model
	llmConfig.Timeout = maxDuration(cfg.GetTimeout(), clientTimeoutFloor)
	var client *llm.Client
	if options.Client != nil {
		client = options.Client
	} else {
		client = llm.NewClient(llmConfig, registry)
	}
	client.SetToolMode(tools.ModeAgent)

	// Build the system prompt: persona (if enabled) with the voice boundary,
	// then the agent contract, then project context. Agent mode never carries
	// the chat task rules or confirm mode (#170).
	systemPrompt := env.SystemPromptOpts(buildAgentSystemPrompt(options, detectEnvContext()), options.Sliders, options.PersonaLevel)

	client.SetSystemPrompt(systemPrompt)

	store, err := NewCheckpointStore("")
	if err != nil {
		env.Close()
		return nil, err
	}

	// Pruned tool results are spilled here; recall_tool_result reads them.
	prunedStore, err := compact.DefaultStore()
	if err != nil {
		prunedStore = nil
	}

	// Create a token budget for context tracking.
	systemPromptTokens := ctxmgr.EstimateTokens(systemPrompt)
	// Honour the configured context_limit, as the TUI does: for local models it
	// is the only way to know the window (#169).
	contextLimit, known := config.ResolveContextLimit(cfg.BaseURL, model, cfg.ContextLimit)
	if !known {
		if notice := config.UnknownContextNotice(model, contextLimit); notice != "" {
			fmt.Fprintln(errOut, notice)
		}
	}
	// The tool schemas the run offers count too (#234 item 1).
	budget := ctxmgr.NewTokenBudget(contextLimit, systemPromptTokens, compact.DefinitionTokens(client.GetSkills()))

	return &Runner{
		client:     client,
		registry:   registry,
		store:      store,
		options:    options,
		out:        out,
		errOut:     errOut,
		budget:     budget,
		indexer:    env.Indexer,
		pruned:     prunedStore,
		summarize:  SmallModelSummarizer(llmConfig, cfg.ResolveSmallModel()),
		jev:        jevShadowClient(cfg.JevPruneMode(), options.Workspace, errOut),
		jevMode:    cfg.JevPruneMode(),
		env:        env,
		hooks:      env.Hooks,
		warn:       warn,
		gate:       gate,
		firstRunID: firstRunID,
		rulesMode:  cfg.StreamRulesMode(),
		watchdog:   cfg.WatchdogMode(),
		gateMode:   cfg.CompletionGateMode(),
		jevGate:    cfg.JevGateMode(),
		oracle: WatchdogOracle(cfg, options.Workspace, func(line string) {
			fmt.Fprintf(errOut, "[agent] %s\n", line)
		}),
	}, nil
}

// jevShadowClient returns a Jev client when jev_prune is "shadow" or "on",
// and says once that excerpts will leave the machine. Paths in them are
// sent relative to workspace, or as <path>.
func jevShadowClient(mode, workspace string, errOut io.Writer) *jev.Client {
	if mode == config.ModeOff {
		return nil
	}
	c, err := jev.NewFromEnv()
	if err != nil {
		fmt.Fprintf(errOut, "[agent] jev prune disabled: %v\n", err)
		return nil
	}
	c.Workspace = workspace
	fmt.Fprintf(errOut, "[agent] jev prune %s: redacted excerpts of old tool results are sent to TypeSafe\n", mode)
	return c
}

func (r *Runner) ListRuns(limit int) ([]RunSummary, error) {
	return r.store.List(limit)
}

func (r *Runner) Resume(ctx context.Context, runID string) (*RunState, error) {
	state, err := r.store.Load(runID)
	if err != nil {
		return nil, err
	}
	normalizeStateOptions(state, r.options)
	return r.runState(ctx, state)
}

func (r *Runner) RunGoal(ctx context.Context, goal string) (*RunState, error) {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return nil, fmt.Errorf("goal is required")
	}

	// UserPromptSubmit, once, before any model call (2.0 F2e). A blocked or
	// interrupted check ends the run before it has a state or a checkpoint.
	content, err := r.submitGoal(ctx, goal)
	if err != nil {
		r.emitProgress(ProgressError, err.Error(), 0, r.options.MaxTurns)
		return nil, err
	}

	state := NewRunState(goal, r.options)
	// The first run takes the ID the hooks were loaded with (session_id).
	if r.firstRunID != "" {
		state.RunID = r.firstRunID
		r.firstRunID = ""
	}
	normalizeStateOptions(state, r.options)
	if !state.Options.EnablePlanning {
		state.Phase = PhaseExecution
	}

	state.Messages = append(state.Messages, tui.ChatMessage{
		Role:      "user",
		Content:   content,
		Timestamp: time.Now(),
	})
	state.Steps = append(state.Steps, Step{
		Turn:      0,
		Type:      "goal",
		Content:   goal,
		Timestamp: time.Now(),
	})

	return r.runState(ctx, state)
}

func (r *Runner) runState(ctx context.Context, state *RunState) (*RunState, error) {
	if state == nil {
		return nil, fmt.Errorf("run state is nil")
	}
	// Hook warnings raised under this run (tool hooks, Stop, compaction) go
	// to this runner's gated sink, even from a hooks runner shared with
	// other runners (a ParentEnv's), so they stop at Close like every
	// callback.
	ctx = hooks.WithWarn(ctx, r.warn)
	normalizeStateOptions(state, r.options)
	defer r.persistArtifacts(state)

	if state.Phase == "" {
		if state.Options.EnablePlanning {
			state.Phase = PhasePlanning
		} else {
			state.Phase = PhaseExecution
		}
	}

	if state.Phase == PhasePlanning {
		if err := r.runPlanningPhase(ctx, state); err != nil {
			state.Status = StatusFailed
			state.Error = err.Error()
			state.UpdatedAt = time.Now()
			_ = r.store.Save(state)
			r.emitProgress(ProgressError, err.Error(), state.Turn, state.Options.MaxTurns)
			return state, err
		}
		if !state.Options.DisableCheckpoints {
			if err := r.store.Save(state); err != nil {
				fmt.Fprintf(r.errOut, "Warning: failed to save checkpoint: %v\n", err)
			}
		}
	}

	sess := r.newSteering(ctx, state)
	// The run's ballot ends with it: one in flight is cancelled, and none
	// logs to errOut after RunGoal returns (MCP agent mode reads a buffer).
	defer sess.Close()
	l := r.newLoop(state, sess)
	stopContinued := false
	for {
		if state.Turn >= state.Options.MaxTurns {
			return r.finishRun(state, StatusMaxTurnsReached), nil
		}
		if err := ctx.Err(); err != nil {
			return r.cancelRun(state, err)
		}
		state.Status = StatusRunning
		state.Phase = PhaseExecution
		l.Limits.MaxTurns = state.Options.MaxTurns - state.Turn

		msgs, res, err := r.step(ctx, l, state)
		state.Messages = msgs
		state.UpdatedAt = time.Now()
		if err != nil {
			if res.StopReason == loop.StopInterrupted {
				return r.cancelRun(state, err)
			}
			var tte *loop.TurnTimeoutError
			err = annotateTurnTimeout(err, errors.As(err, &tte), state.Options.RequestTimeout)
			state.Status = StatusFailed
			state.Error = err.Error()
			_ = r.store.Save(state)
			r.emitProgress(ProgressError, err.Error(), state.Turn, state.Options.MaxTurns)
			return state, err
		}

		switch res.StopReason {
		case loop.StopCap:
			continue // the MaxTurns check above ends the run
		case loop.StopIdentical, loop.StopProgress:
			state.StopReason = string(res.StopReason)
			return r.finishRun(state, StatusNoProgressStopped), nil
		case loop.StopInvalidArgs:
			state.StopReason = string(res.StopReason)
			state.ConsecutiveInvalidToolArgs = state.Options.MaxConsecutiveInvalidToolArgs
			state.Status = StatusFailed
			state.Error = fmt.Sprintf("tool-call arguments were invalid JSON %d turns in a row — aborting to avoid an unbounded retry loop (likely upstream stream corruption)", state.ConsecutiveInvalidToolArgs)
			now := time.Now()
			state.CompletedAt = &now
			if !state.Options.DisableCheckpoints {
				_ = r.store.Save(state)
			}
			r.emitProgress(ProgressError, state.Error, state.Turn, state.Options.MaxTurns)
			return state, errors.New(state.Error)
		}

		// StopDone: the model answered without calling a tool.
		if res.ToolCalls > 0 {
			state.ConsecutiveNoToolTurns = 0
		}
		state.ConsecutiveNoToolTurns++

		complete, vetoed := r.completion(ctx, state, res.FinalText, sess)
		if vetoed {
			// The gate appended its continue prompt: skip the generic one,
			// and the no-tool-turn count restarts (as after a failed check).
			state.ConsecutiveNoToolTurns = 0
			if !state.Options.DisableCheckpoints {
				_ = r.store.Save(state)
			}
			continue
		}
		if complete {
			completed, err := r.handleCompletionCandidate(ctx, state)
			if err != nil {
				state.Status = StatusFailed
				state.Error = err.Error()
				state.UpdatedAt = time.Now()
				_ = r.store.Save(state)
				r.emitProgress(ProgressError, err.Error(), state.Turn, state.Options.MaxTurns)
				return state, err
			}
			if completed {
				// The run is finishing: Stop hooks may send it back once.
				if next := r.stopHook(ctx, state, &stopContinued); next != "" {
					state.Status = StatusRunning
					state.Phase = PhaseExecution
					state.CompletedAt = nil
					state.ConsecutiveNoToolTurns = 0
					state.Messages = append(state.Messages, tui.ChatMessage{Role: "user", Content: next, Timestamp: time.Now()})
					if !state.Options.DisableCheckpoints {
						_ = r.store.Save(state)
					}
					continue
				}
				r.emitProgress(ProgressResponse, state.LastAssistantResponse, state.Turn, state.Options.MaxTurns)
				if !state.Options.DisableCheckpoints {
					_ = r.store.Save(state)
				}
				r.emitProgress(ProgressComplete, state.Status, state.Turn, state.Options.MaxTurns)
				return state, nil
			}
		}

		if state.ConsecutiveNoToolTurns >= state.Options.MaxConsecutiveNoToolTurns {
			return r.finishRun(state, StatusNoProgressStopped), nil
		}

		state.Messages = append(state.Messages, tui.ChatMessage{
			Role:      "user",
			Content:   buildContinuePrompt(state),
			Timestamp: time.Now(),
		})
		if !state.Options.DisableCheckpoints {
			_ = r.store.Save(state)
		}
	}
}

func (r *Runner) finishRun(state *RunState, status string) *RunState {
	state.Status = status
	now := time.Now()
	state.CompletedAt = &now
	if !state.Options.DisableCheckpoints {
		_ = r.store.Save(state)
	}
	r.emitProgress(ProgressComplete, state.Status, state.Turn, state.Options.MaxTurns)
	return state
}

// cancelRun records an interrupt: ctx was cancelled between or during turns.
func (r *Runner) cancelRun(state *RunState, err error) (*RunState, error) {
	state.Status = StatusCancelled
	state.Error = fmt.Sprintf("run cancelled: %v", err)
	now := time.Now()
	state.CompletedAt = &now
	state.UpdatedAt = now
	if !state.Options.DisableCheckpoints {
		_ = r.store.Save(state)
	}
	r.emitProgress(ProgressError, state.Error, state.Turn, state.Options.MaxTurns)
	return state, err
}

// newLoop builds the run's loop from the agent options. One Loop serves
// every step of a run. Steers are not cleared between steps: a steer left
// over when a step ends (it arrived after the loop's last check, or the step
// stopped on an error) is user input, so it joins the next step instead of
// being dropped. A new run gets a new Loop, so nothing carries across runs.
func (r *Runner) newLoop(state *RunState, sess *steer.Session) *loop.Loop {
	lim := loop.DefaultLimits()
	lim.MaxCallsPerTurn = state.Options.MaxToolCallsPerTurn
	lim.ToolTimeout = state.Options.ToolTimeout
	lim.RequestTimeout = state.Options.RequestTimeout
	lim.MaxInvalidArgTurns = state.Options.MaxConsecutiveInvalidToolArgs
	lim.TextToolCalls = true
	// Tool hooks already run in r.registry (Setup); Stop is fired by runState
	// when the run finishes, PreCompact/PostCompact by compactMessages.
	return &loop.Loop{
		Client:    r.client,
		Tools:     r.registry,
		Limits:    lim,
		Gate:      loop.PromptGate(r.options.PromptFunc),
		Compact:   &runCompactor{r: r, meter: compact.NewMeter(0)},
		SessionID: "agent-" + state.RunID,
		Steering:  sess.Steering(),
		Advisor: steer.NewToolGate(r.jevGate, r.options.Workspace, func() string { return state.Goal }, func(line string) {
			fmt.Fprintf(r.errOut, "[agent] %s\n", line)
		}),
	}
}

// newSteering is the run's stream rules and watchdog (2.0 W3): one session
// per run, so a rule's repeat policy and the ballot's cadence span the
// run's steps; ctx (the run's) cancels a ballot in flight. With verification commands
// the runtime checks the work after TASK_COMPLETE, so the
// task-complete-before-verify rule stands down.
func (r *Runner) newSteering(ctx context.Context, state *RunState) *steer.Session {
	var set *rules.Set
	if r.env != nil {
		set = r.env.Rules
	}
	return steer.New(steer.Options{
		Rules:           set,
		RulesMode:       r.rulesMode,
		RuntimeVerifies: state.Options.RequireVerification && len(state.Options.VerificationCommands) > 0,
		Watchdog:        r.watchdog,
		Oracle:          r.oracle,
		Goal:            state.Goal,
		Context:         ctx,
		// The completion gate asks its own ballot at a final reply.
		FinalRepliesToGate: true,
		Logf:               func(line string) { fmt.Fprintf(r.errOut, "[agent] %s\n", line) },
	})
}

// WatchdogOracle is the oracle the config names for the watchdog ballot,
// or nil (the heuristic) when the watchdog is off: no key is read and no
// notice printed for a feature that is not on. oracle "llm" asks the small
// model on a client of its own, one call at a time, so a background
// ballot never shares a client with a compaction summary. That client is
// a plain completion (no xAI collections or features). workspace makes
// paths in what the oracle sends workspace-relative (jev.RedactPaths). The
// chat, MCP chat and agent runs share it.
func WatchdogOracle(cfg *config.Config, workspace string, logf func(string)) decide.Oracle {
	if cfg == nil || cfg.WatchdogMode() == config.ModeOff {
		return nil
	}
	var complete decide.CompleteFunc
	if cfg.OracleMode() == "llm" {
		base := llm.ConfigFrom(cfg)
		base.Collections, base.XAIFeatures = nil, nil
		small := SmallModelSummarizer(base, cfg.ResolveSmallModel())
		var mu sync.Mutex
		complete = func(ctx context.Context, system, user string) (string, error) {
			mu.Lock()
			defer mu.Unlock()
			return small(ctx, system, user)
		}
	}
	return decide.New(cfg.OracleMode(), complete, "ballot", workspace, logf)
}

// step runs the loop once. Only the event consumer touches state (and r.out)
// while Run executes; the unbuffered event channel orders it before Run
// returns.
func (r *Runner) step(ctx context.Context, l *loop.Loop, state *RunState) ([]tui.ChatMessage, loop.Result, error) {
	events := l.Events()
	base := state.Turn
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range events {
			r.onEvent(state, base, ev)
			if ev.Kind == loop.EventDone {
				return
			}
		}
	}()
	msgs, res, err := l.Run(ctx, state.Messages)
	<-done
	return msgs, res, err
}

func (r *Runner) onEvent(state *RunState, base int, ev loop.Event) {
	switch ev.Kind {
	case loop.EventTurnStart:
		state.Turn = base + ev.Turn
		if state.Options.Verbose {
			fmt.Fprintf(r.out, "\n[agent] turn %d/%d\n", state.Turn, state.Options.MaxTurns)
		}
		r.emitProgress(ProgressTurnStart, fmt.Sprintf("turn %d/%d", state.Turn, state.Options.MaxTurns), state.Turn, state.Options.MaxTurns)
	case loop.EventCompacted:
		r.reportCompaction(state, ev.Text)
	case loop.EventRuleInterrupt:
		if r.options.OnTurnStats != nil && ev.Usage != nil {
			r.options.OnTurnStats(TurnStats{Turn: state.Turn, MaxTurns: state.Options.MaxTurns, Elapsed: ev.Elapsed,
				InputTokens: ev.Usage.PromptTokens, OutputTokens: ev.Usage.CompletionTokens, Dropped: true})
		}
	case loop.EventAssistant:
		text := strings.TrimSpace(ev.Text)
		if r.options.OnTurnStats != nil {
			stats := TurnStats{Turn: state.Turn, MaxTurns: state.Options.MaxTurns, Elapsed: ev.Elapsed, Response: text, ToolCalls: ev.ToolNames}
			if ev.Usage != nil {
				stats.InputTokens = ev.Usage.PromptTokens
				stats.OutputTokens = ev.Usage.CompletionTokens
			}
			r.options.OnTurnStats(stats)
		}
		state.LastAssistantResponse = text
		state.Steps = append(state.Steps, Step{Turn: state.Turn, Type: "assistant", Content: text, Timestamp: time.Now()})
		if state.Options.Verbose && text != "" {
			fmt.Fprintf(r.out, "[assistant]\n%s\n", text)
		}
		updatePlanProgressFromAssistant(state, text, len(ev.ToolNames) > 0)
	case loop.EventToolStart:
		r.emitProgress(ProgressToolCall, ev.Call.Name, state.Turn, state.Options.MaxTurns)
		if state.Options.Verbose {
			fmt.Fprintf(r.out, "[tool] %s\n", ev.Call.Name)
		}
	case loop.EventToolResult:
		state.ToolCallCount++
		state.Steps = append(state.Steps, Step{
			Turn: state.Turn, Type: "tool", Name: ev.Call.Name, Content: truncateForStep(ev.Text),
			ToolCall: ev.Call.ID, Timestamp: time.Now(),
		})
	case loop.EventNotice:
		if state.Options.Verbose {
			fmt.Fprintf(r.out, "[agent] warning: %s\n", ev.Text)
		}
	case loop.EventTurnEnd:
		// A consistent history (every call paired): checkpoint per turn, as
		// before the loop existed.
		state.Messages = ev.History
		state.UpdatedAt = time.Now()
		if !state.Options.DisableCheckpoints {
			_ = r.store.Save(state)
		}
	}
}

// runCompactor feeds the loop's usage into the token budget and runs the
// agent's compaction ladder (#174) on the loop goroutine. Its meter spans
// the run (#234).
type runCompactor struct {
	r     *Runner
	meter *compact.Meter
}

func (c *runCompactor) Compact(ctx context.Context, history []tui.ChatMessage, usage *llm.TokenUsage, force bool) ([]tui.ChatMessage, []string, bool) {
	prompt := 0
	if usage != nil {
		prompt = usage.PromptTokens
		if c.r.budget != nil {
			c.r.budget.AddTurn(usage.PromptTokens, usage.CompletionTokens)
		}
	}
	c.meter.Observe(history, prompt)
	out, notes, changed := c.r.compactMessages(ctx, history, c.meter, force)
	c.meter.Sending(out)
	return out, notes, changed
}

func (r *Runner) runPlanningPhase(ctx context.Context, state *RunState) error {
	if !state.Options.EnablePlanning {
		state.Phase = PhaseExecution
		return nil
	}
	if len(state.Plan) > 0 {
		state.Phase = PhaseExecution
		markPlanStepInProgress(state, state.ActivePlanStep)
		return nil
	}

	state.Phase = PhasePlanning
	prompt := buildPlanningPrompt(state)
	state.Messages = append(state.Messages, tui.ChatMessage{
		Role:      "user",
		Content:   prompt,
		Timestamp: time.Now(),
	})

	requestCtx, cancel := context.WithTimeout(ctx, state.Options.RequestTimeout)
	planTurnStart := time.Now()

	var result llm.ChatCompletionResult
	// The planning request offers tools but records only text: a reply that
	// called one keeps no blocks, which would replay a tool_use that never
	// gets a result (2.0 F3, ruling 6).
	sawCalls := false
	streamErr := r.client.SendMessageStreamEvents(requestCtx, state.Messages, r.client.GetSkills(), func(event llm.StreamEvent) {
		switch event.Type {
		case llm.EventToolUseStart, llm.EventToolUseInputDelta, llm.EventToolUseDone:
			sawCalls = true
		}
		switch event.Type {
		case llm.EventContentDelta:
			result.Content += event.ContentDelta
		case llm.EventMessageDone:
			result.Usage = event.Usage
			result.ProviderBlocks = event.ProviderBlocks
			result.BlocksRejected = event.BlocksRejected
			if event.FinishReason == "tool_use" || event.FinishReason == "tool_calls" {
				sawCalls = true
			}
		}
	})
	planTimedOut := errors.Is(requestCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil
	cancel()
	if streamErr != nil {
		if llm.BlocksRejectedIn(streamErr) {
			// Refused, and the resend failed too: still strip them (W8-1 M4).
			state.Messages = tui.StripProviderBlocks(state.Messages)
		}
		return annotateTurnTimeout(streamErr, planTimedOut, state.Options.RequestTimeout)
	}

	if r.options.OnTurnStats != nil {
		stats := TurnStats{Turn: state.Turn, MaxTurns: state.Options.MaxTurns, Elapsed: time.Since(planTurnStart)}
		if result.Usage != nil {
			stats.InputTokens = result.Usage.PromptTokens
			stats.OutputTokens = result.Usage.CompletionTokens
		}
		r.options.OnTurnStats(stats)
	}

	if result.BlocksRejected {
		// The provider refused the blocks this request replayed: strip them,
		// as the loop does (2.0 F3).
		state.Messages = tui.StripProviderBlocks(state.Messages)
	}
	planResponse := strings.TrimSpace(result.Content)
	planMsg := tui.ChatMessage{Role: "assistant", Content: planResponse, Timestamp: time.Now()}
	if !sawCalls && planResponse == result.Content {
		// Trimmed text is an edit: the blocks would no longer match it
		// (2.0 F3).
		planMsg = tui.AttachProviderBlocks(planMsg, result.ProviderBlocks)
	}
	state.Messages = append(state.Messages, planMsg)
	state.Steps = append(state.Steps, Step{
		Turn:      state.Turn,
		Type:      "plan",
		Content:   truncateForStep(planResponse),
		Timestamp: time.Now(),
	})

	state.Plan = parsePlanSteps(planResponse, state.Options.PlanMaxSteps)
	if len(state.Plan) == 0 {
		state.Plan = []PlanStep{{Index: 1, Title: "Complete the requested goal", Status: PlanStatusPending}}
	}
	state.ActivePlanStep = 0
	markPlanStepInProgress(state, state.ActivePlanStep)
	state.Phase = PhaseExecution

	state.Messages = append(state.Messages, tui.ChatMessage{
		Role:      "user",
		Content:   buildExecutionKickoffPrompt(state),
		Timestamp: time.Now(),
	})
	return nil
}

func (r *Runner) handleCompletionCandidate(ctx context.Context, state *RunState) (bool, error) {
	if !state.Options.RequireVerification || len(state.Options.VerificationCommands) == 0 {
		markAllPlanStepsCompleted(state)
		completeState(state)
		return true, nil
	}

	return r.runVerificationPhase(ctx, state)
}

func (r *Runner) runVerificationPhase(ctx context.Context, state *RunState) (bool, error) {
	state.Phase = PhaseVerification
	state.Steps = append(state.Steps, Step{
		Turn:      state.Turn,
		Type:      "verification_start",
		Timestamp: time.Now(),
	})

	allPassed := true
	checks := make([]VerificationCheck, 0, len(state.Options.VerificationCommands))
	for _, cmd := range state.Options.VerificationCommands {
		check := executeVerificationCommand(ctx, state.Options.Workspace, cmd, state.Options.VerifyTimeout)
		checks = append(checks, check)
		state.Steps = append(state.Steps, Step{
			Turn:      state.Turn,
			Type:      "verification_check",
			Name:      cmd,
			Content:   truncateForStep(check.Output),
			Timestamp: time.Now(),
		})
		if !check.Passed {
			allPassed = false
		}
	}
	state.Verification = append(state.Verification, checks...)

	if allPassed {
		markAllPlanStepsCompleted(state)
		completeState(state)
		state.Steps = append(state.Steps, Step{
			Turn:      state.Turn,
			Type:      "verification_passed",
			Timestamp: time.Now(),
		})
		return true, nil
	}

	failureSummary := buildVerificationFailurePrompt(checks, state.Options)
	state.Messages = append(state.Messages, tui.ChatMessage{
		Role:      "user",
		Content:   failureSummary,
		Timestamp: time.Now(),
	})
	state.Steps = append(state.Steps, Step{
		Turn:      state.Turn,
		Type:      "verification_failed",
		Content:   truncateForStep(failureSummary),
		Timestamp: time.Now(),
	})
	state.ConsecutiveNoToolTurns = 0
	state.Phase = PhaseExecution
	return false, nil
}

func executeVerificationCommand(parent context.Context, workspace, command string, timeout time.Duration) VerificationCheck {
	if timeout <= 0 {
		timeout = DefaultOptions().VerifyTimeout
	}

	// --verify-cmd is user-authored (trusted, so no denylist); the runner
	// still kills its whole process group on timeout, so a go test child
	// holding the pipe cannot keep the check open.
	res := shellrun.Run(parent, shellrun.Options{Dir: workspace, Command: command, Timeout: timeout, MaxOutput: maxCommandOutput})
	output := res.Output
	if res.Err != nil {
		output += "\n" + res.Err.Error()
		if len(output) > maxCommandOutput {
			output = output[:maxCommandOutput]
		}
	}

	return VerificationCheck{
		Command:   command,
		Passed:    res.Err == nil && !res.TimedOut && res.ExitCode == 0,
		ExitCode:  res.ExitCode,
		Output:    output,
		TimedOut:  res.TimedOut,
		Timestamp: time.Now(),
	}
}

func normalizeOptions(options *Options) {
	defaults := DefaultOptions()
	if options.MaxTurns <= 0 {
		options.MaxTurns = defaults.MaxTurns
	}
	if options.MaxToolCallsPerTurn <= 0 {
		options.MaxToolCallsPerTurn = defaults.MaxToolCallsPerTurn
	}
	if options.MaxConsecutiveNoToolTurns <= 0 {
		options.MaxConsecutiveNoToolTurns = defaults.MaxConsecutiveNoToolTurns
	}
	if options.MaxConsecutiveInvalidToolArgs <= 0 {
		options.MaxConsecutiveInvalidToolArgs = defaults.MaxConsecutiveInvalidToolArgs
	}
	if options.RequestTimeout <= 0 {
		options.RequestTimeout = defaults.RequestTimeout
	}
	if options.ToolTimeout <= 0 {
		options.ToolTimeout = defaults.ToolTimeout
	}
	if strings.TrimSpace(options.CompletionMarker) == "" {
		options.CompletionMarker = defaults.CompletionMarker
	}
	if options.PlanMaxSteps <= 0 {
		options.PlanMaxSteps = defaults.PlanMaxSteps
	}
	if options.VerifyTimeout <= 0 {
		options.VerifyTimeout = defaults.VerifyTimeout
	}
}

func normalizeStateOptions(state *RunState, fallback Options) {
	if state.Options.Workspace == "" {
		state.Options.Workspace = fallback.Workspace
	}
	if state.Options.ArtifactDir == "" && fallback.ArtifactDir != "" {
		state.Options.ArtifactDir = fallback.ArtifactDir
	}
	if state.Options.VerificationCommands == nil && fallback.VerificationCommands != nil {
		state.Options.VerificationCommands = fallback.VerificationCommands
	}
	if len(state.Options.VerificationCommands) == 0 && len(fallback.VerificationCommands) > 0 {
		state.Options.VerificationCommands = fallback.VerificationCommands
	}
	// EnablePlanning and RequireVerification are deliberately NOT restored from
	// the fallback. RunState.Options is persisted in full at run start, so a
	// false here is a decision, not a gap — restoring it would put the local
	// planner back on top of a server-side conductor mid-run, which is the one
	// state this design exists to prevent.
	if !state.Options.EmitArtifacts && fallback.EmitArtifacts {
		state.Options.EmitArtifacts = fallback.EmitArtifacts
	}
	normalizeOptions(&state.Options)
}

func completeState(state *RunState) {
	state.Status = StatusCompleted
	now := time.Now()
	state.CompletedAt = &now
	state.UpdatedAt = now
}

func isCompletionResponse(content string, options Options) bool {
	text := strings.TrimSpace(content)
	if text == "" {
		return false
	}
	if options.CompletionMarker != "" && strings.Contains(strings.ToUpper(text), strings.ToUpper(options.CompletionMarker)) {
		return true
	}
	return !options.RequireCompletionMarker
}

func buildPlanningPrompt(state *RunState) string {
	return fmt.Sprintf("Create a concise execution plan for this goal with 3-7 numbered steps. Include technical validation steps. Goal: %s", state.Goal)
}

func buildExecutionKickoffPrompt(state *RunState) string {
	planLines := make([]string, 0, len(state.Plan))
	for _, step := range state.Plan {
		planLines = append(planLines, fmt.Sprintf("%d. %s", step.Index, step.Title))
	}
	return fmt.Sprintf("Begin executing this plan now. Use tools as needed and emit STEP_DONE: <n> when a step is completed.\n\nPlan:\n%s", strings.Join(planLines, "\n"))
}

func buildContinuePrompt(state *RunState) string {
	marker := state.Options.CompletionMarker
	if marker == "" {
		marker = "TASK_COMPLETE:"
	}

	stepHint := ""
	if len(state.Plan) > 0 && state.ActivePlanStep >= 0 && state.ActivePlanStep < len(state.Plan) {
		stepHint = fmt.Sprintf(" Focus on plan step %d: %s.", state.Plan[state.ActivePlanStep].Index, state.Plan[state.ActivePlanStep].Title)
	}
	return fmt.Sprintf("Continue working toward the goal.%s Use tools when needed. If you are done, respond with '%s' followed by final deliverables and validation notes.", stepHint, marker)
}

func buildVerificationFailurePrompt(checks []VerificationCheck, options Options) string {
	failed := make([]string, 0)
	for _, c := range checks {
		if c.Passed {
			continue
		}
		failed = append(failed, fmt.Sprintf("- `%s` (exit=%d, timed_out=%v)\n%s", c.Command, c.ExitCode, c.TimedOut, truncateForStep(c.Output)))
	}
	return fmt.Sprintf("Verification failed. Fix the issues and continue execution. Re-run validations before completion.\n\nFailed checks:\n%s\n\nWhen complete, respond with '%s'.", strings.Join(failed, "\n"), options.CompletionMarker)
}

// detectEnvContext probes the runtime environment and returns a concise
// summary string for inclusion in the agent system prompt.
func detectEnvContext() string {
	osName := runtime.GOOS
	if osName == "darwin" {
		osName = "macOS"
	}
	arch := runtime.GOARCH

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "unknown"
	}

	pkgManager := "none"
	for _, candidate := range []string{"brew", "apt-get", "apt", "dnf", "yum", "pacman", "apk", "scoop", "choco"} {
		if path, err := exec.LookPath(candidate); err == nil {
			pkgManager = filepath.Base(path)
			break
		}
	}

	pythonExe := "none"
	for _, candidate := range []string{"python3", "python"} {
		if _, err := exec.LookPath(candidate); err == nil {
			pythonExe = candidate
			break
		}
	}

	return fmt.Sprintf("OS: %s (%s)\nShell: %s\nPackage manager: %s\nPython: %s",
		osName, arch, shell, pkgManager, pythonExe)
}

func buildAgentSystemPrompt(options Options, envContext string) string {
	marker := options.CompletionMarker
	if marker == "" {
		marker = "TASK_COMPLETE:"
	}

	verificationInstruction := ""
	if options.RequireVerification && len(options.VerificationCommands) > 0 {
		verificationInstruction = "Before final completion, run all verification commands using bash and confirm they pass."
	}

	return fmt.Sprintf(`You are Celeste Agent, an autonomous execution loop for software and content tasks.

## Tool Usage — Non-Negotiable Rules

You have file and shell tools. You MUST use them. There are no exceptions.

- To read a file: call read_file. Never ask the user to paste contents.
- To write a new file: call write_file. NEVER output file content as raw text in your response.
- To edit an existing file: call patch_file with old_string/new_string. Never rewrite the whole file unless it is new.
- To run a command (git status, go test, ls, grep, etc.): call bash.
- To find files: call list_files, or call bash with ls/find.
- To search code: call search, or call bash with grep.

## Tool Invocation Format

Invoke tools via the function calling API when available. If the API does not forward function calls, use this exact text format instead — one block per tool:

<tool_call>{"name": "write_file", "arguments": {"path": "hello.py", "content": "print('hello')"}}</tool_call>

Rules for text-format tool calls:
- Output ONLY the <tool_call> block(s) — do NOT narrate the action or simulate the output.
- Stop after the block(s). Wait for [Tool Result] messages before continuing.
- Do NOT write "I will call...", "Let me...", or any description before or after the block.

If you write code or file content in your response instead of calling a tool, you have failed. The content will appear in the chat and nothing will be written to disk.

## Execution Contract

1. Work iteratively — inspect, act, verify — until the objective is complete.
2. Emit STEP_DONE: <n> when you complete plan step n.
3. When complete, begin your final response with %q and include:
   - what files were created or modified
   - what commands ran and their results
   - any remaining risks or open items
4. If blocked, clearly describe the blocker and what the user needs to do.
5. %s

## Environment

%s

Use the package manager and Python executable listed above. Do not use sudo or assume alternatives are available.

Workspace root: %s`, marker, verificationInstruction, envContext, options.Workspace)
}

func parsePlanSteps(content string, maxSteps int) []PlanStep {
	if maxSteps <= 0 {
		maxSteps = DefaultOptions().PlanMaxSteps
	}

	lines := strings.Split(content, "\n")
	steps := make([]PlanStep, 0, maxSteps)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		title := ""
		if isNumberedStep(line) {
			title = stripStepPrefix(line)
		} else if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
			title = strings.TrimSpace(line[2:])
		}

		if title == "" {
			continue
		}
		steps = append(steps, PlanStep{
			Index:  len(steps) + 1,
			Title:  title,
			Status: PlanStatusPending,
		})
		if len(steps) >= maxSteps {
			break
		}
	}
	return steps
}

func isNumberedStep(line string) bool {
	if len(line) < 3 {
		return false
	}
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i == 0 || i >= len(line) {
		return false
	}
	if line[i] != '.' && line[i] != ')' && line[i] != ':' {
		return false
	}
	if i+1 >= len(line) {
		return false
	}
	return line[i+1] == ' '
}

func stripStepPrefix(line string) string {
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i >= len(line) {
		return strings.TrimSpace(line)
	}
	if (line[i] == '.' || line[i] == ')' || line[i] == ':') && i+1 < len(line) {
		return strings.TrimSpace(line[i+1:])
	}
	return strings.TrimSpace(line)
}

func extractStepDoneMarker(content string) int {
	upper := strings.ToUpper(content)
	idx := strings.Index(upper, "STEP_DONE:")
	if idx < 0 {
		return -1
	}
	rest := strings.TrimSpace(content[idx+len("STEP_DONE:"):])
	numBuf := strings.Builder{}
	for _, r := range rest {
		if r >= '0' && r <= '9' {
			numBuf.WriteRune(r)
		} else {
			break
		}
	}
	if numBuf.Len() == 0 {
		return -1
	}
	n, err := strconv.Atoi(numBuf.String())
	if err != nil || n <= 0 {
		return -1
	}
	return n
}

func updatePlanProgressFromAssistant(state *RunState, content string, hadTools bool) {
	if len(state.Plan) == 0 {
		return
	}

	if step := extractStepDoneMarker(content); step > 0 {
		markPlanStepsCompletedThrough(state, step-1)
		next := step
		if next >= len(state.Plan) {
			next = len(state.Plan) - 1
		}
		state.ActivePlanStep = next
		if state.ActivePlanStep >= 0 && state.ActivePlanStep < len(state.Plan) {
			if state.Plan[state.ActivePlanStep].Status != PlanStatusCompleted {
				state.Plan[state.ActivePlanStep].Status = PlanStatusInProgress
			}
		}
		return
	}

	if hadTools {
		markPlanStepInProgress(state, state.ActivePlanStep)
	}
}

func markPlanStepsCompletedThrough(state *RunState, idx int) {
	if idx < 0 {
		return
	}
	if idx >= len(state.Plan) {
		idx = len(state.Plan) - 1
	}
	for i := 0; i <= idx; i++ {
		state.Plan[i].Status = PlanStatusCompleted
	}
}

func markPlanStepInProgress(state *RunState, idx int) {
	if len(state.Plan) == 0 {
		return
	}
	if idx < 0 || idx >= len(state.Plan) {
		idx = 0
		state.ActivePlanStep = 0
	}
	if state.Plan[idx].Status == PlanStatusPending {
		state.Plan[idx].Status = PlanStatusInProgress
	}
}

func markAllPlanStepsCompleted(state *RunState) {
	for i := range state.Plan {
		state.Plan[i].Status = PlanStatusCompleted
	}
}

func truncateForStep(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}

// blockedMutatingTools returns the names of non-read-only tools the current
// policy would send to an interactive prompt. Read-only tools are excluded:
// an agent that can only read is degraded, but one that silently cannot write
// is broken, and that is the case worth refusing to start.
func blockedMutatingTools(registry *tools.Registry, checker *permissions.Checker) []string {
	var blocked []string
	for _, t := range registry.GetTools(tools.ModeAgent) {
		if t.IsReadOnly() {
			continue
		}
		if checker.Check(permToolInfo{name: t.Name(), readOnly: t.IsReadOnly()}, nil).Decision == permissions.Ask {
			blocked = append(blocked, t.Name())
		}
	}
	sort.Strings(blocked)
	return blocked
}

// permToolInfo adapts a tool's identity to permissions.ToolInfo. The tools
// package has its own adapter but does not export it, and permissions
// deliberately avoids importing tools to stay acyclic.
type permToolInfo struct {
	name     string
	readOnly bool
}

func (p permToolInfo) ToolName() string { return p.name }
func (p permToolInfo) IsReadOnly() bool { return p.readOnly }

// syncWriter serializes writes: hooks warn from tool goroutines while the
// loop goroutine writes compaction lines.
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}
