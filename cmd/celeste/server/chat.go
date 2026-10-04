package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/agent"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/compact"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	ctxmgr "github.com/whykusanagi/celeste-cli/v2/cmd/celeste/context"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/jev"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/loop"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/rules"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/steer"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// chatMaxTurns is MCP chat's turn cap, unchanged from the pre-loop server.
const chatMaxTurns = 25

// chatLimits are MCP chat's loop limits: the spec defaults (25 turns,
// identical-call guard 3, progress guard 6, spill at 128 KiB, 45 s per tool)
// with no per-turn call cap, no invalid-argument cap and no <tool_call> text
// parsing, none of which MCP chat ever had.
func chatLimits() loop.Limits {
	lim := loop.DefaultLimits()
	lim.MaxTurns = chatMaxTurns
	return lim
}

// runChatMode runs MCP `celeste` mode:"chat": one prompt through the unified
// loop (2.0 F2b), on the workspace's cached ModeMCPChat Env. That Env runs
// Trust mode with the user's deny rules, hooks without repo-hook approval,
// and global MCP servers only. The call gets its own Loop and history. The
// result is one text block, as before, with any warnings after the reply.
func (s *Server) runChatMode(ctx context.Context, cfg *config.Config, prompt, workspace string) ([]ContentBlock, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("chat error: %w", err)
	}
	cfg = s.servedConfig(ctx, cfg)
	var warns warnSink
	ce, err := s.chatEnvs.acquire(cfg, workspace, &warns)
	if err != nil {
		return nil, fmt.Errorf("chat setup: %w%s", err, warns.section())
	}
	defer s.chatEnvs.release(ce)
	// This call's hook warnings come back on this call only: the Env and
	// its hook runner are shared with any overlapping call.
	ctx = hooks.WithWarn(ctx, warns.add)
	env := ce.env

	// Each MCP chat call is a session from the plugin's view: SessionStart
	// fires per call and its context goes into this call's system prompt
	// only, never into the shared Env. The loop then runs the prompt through
	// UserPromptSubmit before the first request (2.0 F2e), in the chat UI's
	// order. Both run under ctx, so a failed hook's warning lands on this
	// call.
	session := env.SessionStartContext(ctx, "startup")
	// The window is this call's served model's; a small one steps the
	// persona down. The guard's notice is only logged: MCP responses are
	// frozen (W5 ruling 7).
	window, _ := config.ResolveContextLimit(cfg.BaseURL, cfg.Model, cfg.ContextLimit)
	sp := env.SystemPrompt(loop.PromptOptions{Session: session, Window: window})
	if sp.Notice != "" {
		log.Printf("[persona] %s", sp.Notice)
	}
	system := sp.String()
	sessionID := "mcp-chat-" + config.UniqueNanoID()
	l := newChatLoop(cfg, newChatClient(cfg, env.Registry, system), env, system, sessionID)
	sess := chatSteering(ctx, cfg, env, prompt, workspace)
	// The call's ballot ends with it: one in flight is cancelled.
	defer sess.Close()
	l.Steering = sess.Steering()
	l.Advisor = steer.NewToolGate(cfg.JevGateMode(), workspace, func() string { return prompt }, func(line string) {
		log.Printf("celeste chat: %s", line)
	})
	record := func(u *llm.TokenUsage) { s.cost.record(cfg.Model, u) }
	text, err := runChat(ctx, l, env.Hooks, prompt, warns.add, record)
	var blocked *promptBlockedError
	if errors.As(err, &blocked) {
		// The pre-loop server's refusal, verbatim: no "chat error:" prefix.
		// A hook's refusal of the prompt is not a failed completion.
		return nil, fmt.Errorf("%w%s", err, warns.section())
	}
	s.health.record(err)
	if err != nil {
		return nil, fmt.Errorf("chat error: %w%s", err, warns.section())
	}
	return []ContentBlock{{Type: "text", Text: text + warns.section()}}, nil
}

// chatSteering is one MCP chat call's stream rules and watchdog (2.0 W3);
// the prompt is the watchdog's goal and ctx (the call's) cancels a ballot
// in flight. Lines go to the server log (stderr); the result carries none
// of them.
func chatSteering(ctx context.Context, cfg *config.Config, env *loop.Env, prompt, workspace string) *steer.Session {
	logf := func(line string) { log.Printf("celeste chat: %s", line) }
	return steer.New(steer.Options{
		Rules:     env.Rules,
		RulesMode: cfg.StreamRulesMode(),
		Watchdog:  cfg.WatchdogMode(),
		Oracle:    agent.WatchdogOracle(cfg, workspace, logf),
		Goal:      prompt,
		Context:   ctx,
		Logf:      logf,
	})
}

// newChatClient is the pre-loop server's client (same config fields) on the
// Env's registry. The tool mode stays tools.ModeChat.
func newChatClient(cfg *config.Config, reg *tools.Registry, system string) *llm.Client {
	client := llm.NewClient(llm.ConfigFrom(cfg), reg)
	client.SetSystemPrompt(system)
	return client
}

// newChatLoop builds one call's loop. There is no Gate: an Ask (only a
// hook-forced one, since Trust mode asks for nothing else) is denied
// headless, as before. Tool hooks already run in env.Registry. Old tool
// results are pruned near the window (spec §4 F2: MCP chat gains
// compaction).
func newChatLoop(cfg *config.Config, client *llm.Client, env *loop.Env, system, sessionID string) *loop.Loop {
	l := &loop.Loop{
		Client:    client,
		Tools:     env.Registry,
		Limits:    chatLimits(),
		SessionID: sessionID, // names this call's spill directory
		// UserPromptSubmit on the call's prompt, before the first request
		// (2.0 F2e); nil without such hooks.
		CheckPrompt: loop.HookPromptCheck(env.Hooks),
	}
	// Assigned only when non-nil: a nil *chatCompactor in the interface
	// would be a non-nil Compactor.
	if c := newChatCompactor(cfg, system, client.GetSkills(), env.Workspace); c != nil {
		l.Compact = c
	}
	return l
}

// chatCompactor prunes old tool results when a call's history nears the
// model's window: the first rung of the agent's ladder (#174). Pruned bodies
// go to the pruned-results store, where recall_tool_result restores them.
// MCP chat has no summary rung: a call is one prompt of at most 25 turns.
type chatCompactor struct {
	window  int            // the model's context window, in tokens
	meter   *compact.Meter // the next request's size and what is unseen (#234)
	store   *compact.Store
	jev     *jev.Client // jev_prune's scorer (2.0 W3); nil: off
	jevMode string
}

// newChatCompactor sizes the compactor for cfg's model (context_limit
// honoured, as in the TUI and agent). Without a store it returns nil:
// compact.Prune never prunes without one.
func newChatCompactor(cfg *config.Config, system string, skills []tui.SkillDefinition, workspace string) *chatCompactor {
	store, err := compact.DefaultStore()
	if err != nil {
		return nil
	}
	window, _ := config.ResolveContextLimit(cfg.BaseURL, cfg.Model, cfg.ContextLimit)
	c := &chatCompactor{
		window:  window,
		meter:   compact.NewMeter(ctxmgr.EstimateTokens(system) + compact.DefinitionTokens(skills)),
		store:   store,
		jevMode: cfg.JevPruneMode(),
	}
	if c.jevMode != config.ModeOff {
		if jc, err := jev.NewFromEnv(); err == nil {
			jc.Workspace = workspace // paths in excerpts are sent relative to it
			c.jev = jc
		} else {
			log.Printf("celeste chat: jev prune disabled: %v", err)
		}
	}
	return c
}

// Compact implements loop.Compactor. The loop calls it before every request,
// and once with force after a context-overflow error.
func (c *chatCompactor) Compact(ctx context.Context, history []loop.Message, usage *llm.TokenUsage, force bool) ([]loop.Message, []string, bool) {
	prompt := 0
	if usage != nil {
		prompt = usage.PromptTokens
	}
	c.meter.Observe(history, prompt)
	// Shadow reports inline: a call's log lines must not outlive it.
	used := c.meter.Used(history)
	opts, report := compact.WithJev(ctx, c.jev, c.jevMode, history, compact.Options{Window: c.window, Used: used, Overhead: used - compact.Estimate(history), Unseen: c.meter.Unseen(history), Force: force}, func(line string) {
		log.Printf("celeste chat: %s", line)
	}, false)
	out, res := compact.Prune(history, opts, c.store)
	report(res)
	c.meter.Sending(out)
	if !res.Pruned() {
		return history, nil, false
	}
	return out, []string{"context compacted: " + res.Summary()}, true
}

// runChat runs the loop for one prompt and returns the tool-result text.
// When a run finishes as done, Stop hooks may continue it once with turns
// left out of the 25 (chatStopHook). Guard and cap stops never reach Stop.
// The claim flags span the whole call, as the pre-loop session flags did.
// record gets each model reply's token usage (nil when the provider sent none).
func runChat(ctx context.Context, l *loop.Loop, h *hooks.Runner, prompt string, warn func(string), record func(*llm.TokenUsage)) (string, error) {
	lim := l.Limits
	history := []loop.Message{{Role: "user", Content: prompt, Timestamp: time.Now()}}
	var claims chatClaims
	turnsLeft := lim.MaxTurns
	continued := false
	for {
		l.Limits.MaxTurns = turnsLeft // >= 1: chatStopHook stops at 0
		var res loop.Result
		var err error
		history, res, err = runObserved(ctx, l, history, &claims, record)
		if err != nil {
			return "", err
		}
		if res.StopReason == loop.StopBlocked {
			return "", &promptBlockedError{reason: claims.blocked}
		}
		turnsLeft -= res.Turns
		if res.StopReason != loop.StopDone {
			return chatText(res, lim, &claims), nil
		}
		next := chatStopHook(ctx, h, strings.TrimSpace(res.FinalText), continued, turnsLeft, warn)
		if next == "" {
			return chatText(res, lim, &claims), nil
		}
		continued = true
		// The Stop hook's instruction, not the caller's prompt: it skips
		// UserPromptSubmit, as in the chat.
		history = append(history, loop.Message{Role: "user", Content: next, Timestamp: time.Now(),
			Metadata: map[string]any{tui.MetaPromptHookDone: true}})
	}
}

// runObserved runs l once and feeds its events to claims. The event channel
// is unbuffered, so the consumer reads until EventDone. Only the consumer
// touches claims while Run executes, and <-done orders that before return.
func runObserved(ctx context.Context, l *loop.Loop, history []loop.Message, claims *chatClaims, record func(*llm.TokenUsage)) ([]loop.Message, loop.Result, error) {
	events := l.Events()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range events {
			claims.observe(ev)
			// A reply a stream rule dropped was billed too.
			if (ev.Kind == loop.EventAssistant || ev.Kind == loop.EventRuleInterrupt) && record != nil {
				record(ev.Usage)
			}
			if ev.Kind == loop.EventDone {
				return
			}
		}
	}()
	msgs, res, err := l.Run(ctx, history)
	<-done
	return msgs, res, err
}

// chatText is the tool result for a run that ended without an error. The
// stop texts are the pre-loop server's, verbatim (the plugin and the F1
// tests match on them).
func chatText(res loop.Result, lim loop.Limits, claims *chatClaims) string {
	switch res.StopReason {
	case loop.StopIdentical:
		return fmt.Sprintf("Stopped: the model made the identical tool call %d times in a row (stuck loop).", lim.IdenticalCalls)
	case loop.StopProgress:
		return fmt.Sprintf("Stopped: the model called the same tool with no new result %d turns in a row (stuck loop).", lim.NoProgressTurns)
	case loop.StopCap:
		return "Tool loop limit reached"
	default:
		return claims.strip(strings.TrimSpace(res.FinalText))
	}
}

// chatClaims records whether generate_speech and spawn_agent really ran
// (and succeeded) in this call, so a fabricated "Audio saved:" or "subagent
// spawned (id: …)" claim in the final reply can be replaced. These are the
// pre-loop server's ttsRan/spawnRan flags, now derived from loop events.
// blocked is the reason a UserPromptSubmit hook gave for blocking the
// prompt.
type chatClaims struct {
	tts, spawn bool
	blocked    string
}

func (c *chatClaims) observe(ev loop.Event) {
	if ev.Kind == loop.EventPromptBlocked {
		c.blocked = ev.Text
		return
	}
	if ev.Kind != loop.EventToolResult || ev.IsError {
		return
	}
	switch ev.Call.Name {
	case "generate_speech":
		c.tts = true
	case "spawn_agent":
		c.spawn = true
	}
}

// strip is the claim backstop. The unbacked-audio-claim stream rule
// interrupts such a reply when stream_rules is "on"; this still runs on
// every reply, so the MCP result never carries an unbacked claim whatever
// the mode (2.0 W3).
func (c *chatClaims) strip(text string) string {
	text = rules.StripUnbackedAudioClaim(text, c.tts)
	return rules.StripUnbackedSpawnClaim(text, c.spawn)
}
