package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/jev"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/loop"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// mailbox is an unbounded FIFO from a chat turn's goroutines to the Bubble
// Tea program. put never blocks, so the loop, its hooks and its tools never
// wait on the UI, and nothing blocks after the program quits and stops
// reading. One reader at a time: the command chain in reader.
type mailbox struct {
	mu     sync.Mutex
	queue  []tea.Msg
	closed bool
	ready  chan struct{} // capacity 1: a put or close since the last wait
}

func newMailbox() *mailbox { return &mailbox{ready: make(chan struct{}, 1)} }

func (b *mailbox) put(msg tea.Msg) {
	b.mu.Lock()
	if !b.closed {
		b.queue = append(b.queue, msg)
	}
	b.mu.Unlock()
	b.signal()
}

// close ends the mailbox: later puts are dropped; get drains what is left.
func (b *mailbox) close() {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()
	b.signal()
}

func (b *mailbox) signal() {
	select {
	case b.ready <- struct{}{}:
	default:
	}
}

// get returns the next message, or ok=false once the mailbox is closed and
// empty.
func (b *mailbox) get() (tea.Msg, bool) {
	for {
		b.mu.Lock()
		if len(b.queue) > 0 {
			msg := b.queue[0]
			b.queue[0] = nil
			b.queue = b.queue[1:]
			b.mu.Unlock()
			return msg, true
		}
		closed := b.closed
		b.mu.Unlock()
		if closed {
			return nil, false
		}
		<-b.ready
	}
}

// reader is the command that delivers the next message of run, wrapped with
// the command that reads the one after it (nil after TurnDoneMsg).
func (b *mailbox) reader(run uint64) tea.Cmd {
	return func() tea.Msg {
		msg, ok := b.get()
		if !ok {
			return nil
		}
		var next tea.Cmd
		if _, done := msg.(tui.TurnDoneMsg); !done {
			next = b.reader(run)
		}
		return tui.TurnEventMsg{Run: run, Msg: msg, Next: next}
	}
}

// chatTurn is one user turn on the loop (a tui.TurnHandle).
type chatTurn struct {
	loop      *loop.Loop
	ctx       context.Context
	cancel    context.CancelFunc
	box       *mailbox
	compactor *chatCompactor // nil without a context window
	model     string         // for costs and the log; set on the Update goroutine
	endpoint  string         // for the log
	tools     int            // tool definitions offered, for the log
	msgs      int            // pump goroutine only: the last snapshot's length, for the log
	compacted bool           // pump goroutine only: a prune since the last snapshot
	start     sync.Once
}

func (t *chatTurn) Steer(text string) { t.loop.Steer(text) }
func (t *chatTurn) Cancel()           { t.cancel() }

// Leftover takes the steers the loop has not joined. doneMsg already took
// the ones queued when the run ended; this catches an Enter that landed
// after that, before the app handled TurnDoneMsg (Loop.TakeSteers is safe
// from any goroutine).
func (t *chatTurn) Leftover() []string { return t.loop.TakeSteers() }

// RunTurn implements tui.TurnRunner. It only builds the turn; the returned
// command starts the run on its own goroutine and delivers the first event.
// The run's context is a child of the chat's life context, so quitting
// cancels it.
func (a *TUIClientAdapter) RunTurn(req tui.TurnRequest) (tui.TurnHandle, tea.Cmd) {
	ctx, cancel := context.WithCancel(a.lifeContext())
	// Its permission and ask requests name this turn (2.0 F2e).
	ctx = tui.WithRunOwner(ctx, tui.RunOwner{Kind: tui.OwnerTurn, Run: req.Run})
	cfg := a.client.GetConfig()
	t := &chatTurn{ctx: ctx, cancel: cancel, box: newMailbox(), model: cfg.Model, endpoint: cfg.BaseURL, msgs: len(req.History)}
	t.loop = a.newTurnLoop(req, t)
	t.tools = len(t.loop.Client.GetSkills())
	read := t.box.reader(req.Run)
	return t, func() tea.Msg {
		t.start.Do(func() {
			if !a.beginRun() {
				t.box.put(tui.TurnDoneMsg{Stop: string(loop.StopInterrupted)})
				t.box.close()
				return
			}
			// The loop starts from the history the chat sends, without
			// empty text-only replies: every snapshot drops them
			// (withoutEmptyReplies), so the input must too, or a legacy
			// one would shift SyncLLM's positions.
			go a.runTurn(t, withoutEmptyReplies(req.History))
		})
		return read()
	}
}

// newTurnLoop configures one turn's loop: the chat's limits (the turn cap
// is max_tool_iterations), the modal as Gate, UserPromptSubmit, the
// adapter's pruning (with Jev shadow scoring) when the window is known, and
// tool metadata for vision models.
func (a *TUIClientAdapter) newTurnLoop(req tui.TurnRequest, t *chatTurn) *loop.Loop {
	lim := loop.DefaultLimits()
	if a.baseConfig != nil && a.baseConfig.MaxToolIterations > 0 {
		lim.MaxTurns = a.baseConfig.MaxToolIterations
	}
	lim.KeepToolMetadata = true
	l := &loop.Loop{
		Client:       chatLLM{client: a.client, tools: req.Tools},
		Tools:        a.registry,
		Limits:       lim,
		Gate:         a.gate,
		SessionID:    fmt.Sprintf("tui-%d", os.Getpid()), // spill directory, as before
		SpillCounter: &a.spillSeq,
		CheckPrompt:  a.checkPrompt,
	}
	if req.Window > 0 {
		// Jev is resolved here, on the Update goroutine, once per turn.
		t.compactor = &chatCompactor{a: a, window: req.Window, used: req.Used, jev: a.jevShadow()}
		l.Compact = t.compactor
	}
	return l
}

// runTurn is the turn's goroutine. A Stop hook's deny continues the turn
// once, while turns remain; the continuation joins the history hidden, as
// the Stop hook's instruction, not the user's words.
func (a *TUIClientAdapter) runTurn(t *chatTurn, history []tui.ChatMessage) {
	defer a.endRun()
	defer t.box.close()
	defer t.cancel()
	maxTurns := t.loop.Limits.MaxTurns
	turnsLeft := maxTurns
	continued := false
	for {
		t.loop.Limits.MaxTurns = turnsLeft // >= 1: stopHook stops at 0
		msgs, res, err := a.runOnce(t, history)
		turnsLeft -= res.Turns
		if t.compacted {
			// The run pruned and ended before its next snapshot (an error,
			// an interrupt, a guard): the chat takes the pruned history.
			t.compacted = false
			t.box.put(tui.HistoryMsg{History: withoutEmptyReplies(msgs)})
		}
		if err == nil && res.StopReason == loop.StopDone {
			if next := a.stopHook(t, res.FinalText, continued, turnsLeft); next != "" {
				continued = true
				msg := tui.ChatMessage{Role: "user", Content: next, Timestamp: time.Now(),
					Metadata: map[string]any{"hidden": true, tui.MetaPromptHookDone: true}}
				t.box.put(tui.StopContinueMsg{Message: msg, Reason: next})
				history = append(withoutEmptyReplies(msgs), msg)
				continue
			}
		}
		res.Turns = maxTurns - turnsLeft // the whole user turn, Stop continuations included
		t.box.put(a.doneMsg(t, res, err))
		return
	}
}

// stopHook asks Stop hooks whether a finished turn may end, and returns the
// instruction to continue with, or "" to finish. A deny is honoured once per
// user turn and only while turns remain (the agent and MCP chat rule);
// later ones are reported in the chat and ignored. An interrupted turn ends
// as interrupted: Stop does not fire, and Esc while it runs cuts it short
// without a warning and ignores its answer.
func (a *TUIClientAdapter) stopHook(t *chatTurn, final string, continued bool, turnsLeft int) string {
	if !a.hooks.Has(hooks.EventStop) || t.ctx.Err() != nil {
		return ""
	}
	t.box.put(tui.StopHookStartMsg{})
	ctx := hooks.WithWarn(t.ctx, func(msg string) {
		if t.ctx.Err() == nil {
			tui.LogInfo(msg)
			t.box.put(tui.HookWarningMsg{Text: msg})
		}
	})
	out := a.hooks.Stop(ctx, final)
	if t.ctx.Err() != nil {
		return ""
	}
	instr, warning := hooks.StopContinuation(out, continued, turnsLeft,
		hooks.StopScope{Event: "Stop", Actor: "the chat", Unit: "turn"})
	if warning != "" {
		t.box.put(tui.HookWarningMsg{Text: warning})
	}
	return instr
}

// runOnce runs the loop once while a pump turns its events into the chat's
// messages. The pump reads until EventDone, so Run never waits on it for
// long, and <-pumped orders every message before what the caller puts next.
func (a *TUIClientAdapter) runOnce(t *chatTurn, history []tui.ChatMessage) ([]tui.ChatMessage, loop.Result, error) {
	events := t.loop.Events()
	pumped := make(chan struct{})
	go func() {
		defer close(pumped)
		first := true
		for ev := range events {
			for _, msg := range a.translate(t, ev, &first) {
				t.box.put(msg)
			}
			if ev.Kind == loop.EventDone {
				return
			}
		}
	}()
	msgs, res, err := t.loop.Run(t.ctx, history)
	<-pumped
	return msgs, res, err
}

// translate turns one loop event into the chat's messages, on the pump
// goroutine: it touches only the turn, the thread-safe cost tracker and the
// log file (requests and responses, as the chat logged them before the loop).
func (a *TUIClientAdapter) translate(t *chatTurn, ev loop.Event, first *bool) []tea.Msg {
	switch ev.Kind {
	case loop.EventTurnStart:
		*first = true
		tui.LogInfo(fmt.Sprintf("→ Sending request to: %s (model: %s)", t.endpoint, t.model))
		tui.LogLLMRequest(t.msgs, t.tools)
		return []tea.Msg{tui.TurnStartMsg{Turn: ev.Turn}}
	case loop.EventTextDelta:
		chunk := tui.StreamChunkMsg{Chunk: tui.StreamChunk{Content: ev.Text, IsFirst: *first}}
		*first = false
		return []tea.Msg{chunk}
	case loop.EventAssistant:
		tui.LogLLMResponse(len(ev.Text), len(ev.ToolNames) > 0)
		for _, name := range ev.ToolNames {
			tui.LogInfo("LLM requested tool call: " + name)
		}
		usage := a.recordUsage(t.model, ev.Usage)
		if len(ev.ToolNames) == 0 {
			return []tea.Msg{tui.StreamDoneMsg{FullContent: ev.Text, FinishReason: "stop", Usage: usage}}
		}
		return []tea.Msg{tui.ToolTurnMsg{Text: ev.Text, Usage: usage}}
	case loop.EventPromptsChecked, loop.EventCallsRecorded, loop.EventTurnEnd:
		t.msgs = len(ev.History)
		t.compacted = false
		return []tea.Msg{tui.HistoryMsg{History: withoutEmptyReplies(ev.History)}}
	case loop.EventToolStart:
		return []tea.Msg{tui.ToolStartMsg{ID: ev.Call.ID, Name: ev.Call.Name, Args: ev.Call.Input}}
	case loop.EventToolResult:
		return []tea.Msg{tui.ToolResultMsg{ID: ev.Call.ID, Name: ev.Call.Name, Content: ev.Text, IsError: ev.IsError, Metadata: ev.Metadata}}
	case loop.EventCompacted:
		t.compacted = true
		return []tea.Msg{tui.CompactedMsg{Line: ev.Text, Saved: int(t.compactor.saved.Load())}}
	case loop.EventSteered:
		return []tea.Msg{tui.SteeredMsg{Message: ev.Msg}}
	case loop.EventPromptBlocked, loop.EventSteerBlocked:
		return []tea.Msg{tui.PromptBlockedMsg{Reason: ev.Text, Content: ev.Msg.Content, Timestamp: ev.Msg.Timestamp, Steer: ev.Kind == loop.EventSteerBlocked}}
	case loop.EventNotice:
		return []tea.Msg{tui.HookWarningMsg{Text: ev.Text}}
	}
	return nil
}

// withoutEmptyReplies drops text-only assistant messages with no text. The
// loop records an empty reply; the chat never added one to its history (it
// shows a system line instead), and some providers reject an empty
// assistant message on the next send. Every snapshot is filtered the same
// way, so the chat's positions still line up for SyncLLM.
func withoutEmptyReplies(history []tui.ChatMessage) []tui.ChatMessage {
	out := make([]tui.ChatMessage, 0, len(history))
	for _, m := range history {
		if tui.IsEmptyReply(m) {
			continue
		}
		out = append(out, m)
	}
	return out
}

func (a *TUIClientAdapter) recordUsage(model string, u *llm.TokenUsage) *tui.TokenUsage {
	if u == nil {
		return nil
	}
	tui.LogInfo(fmt.Sprintf("  Usage: %d prompt + %d completion = %d tokens", u.PromptTokens, u.CompletionTokens, u.TotalTokens))
	if a.costTracker != nil {
		a.costTracker.RecordUsage(model, u.PromptTokens, u.CompletionTokens)
		if summary := a.costTracker.GetSummary(); summary.TotalCostUSD > 0 {
			tui.LogInfo(fmt.Sprintf("Session cost: $%.4f (%d turns)", summary.TotalCostUSD, summary.Turns))
		}
	}
	return &tui.TokenUsage{PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens, TotalTokens: u.TotalTokens}
}

// doneMsg ends the turn: why it stopped, the text for a cap or guard, the
// steers no run joined, and whether pruning left the history too big.
func (a *TUIClientAdapter) doneMsg(t *chatTurn, res loop.Result, err error) tui.TurnDoneMsg {
	done := tui.TurnDoneMsg{Stop: string(res.StopReason), Leftover: t.loop.TakeSteers()}
	if t.compactor != nil {
		done.Summarize = t.compactor.over.Load()
	}
	lim := t.loop.Limits
	switch res.StopReason {
	case loop.StopError:
		done.Err = err
		tui.LogInfo(fmt.Sprintf("LLM error: %v", err))
		tui.LogInfo("  Endpoint: " + t.endpoint)
		tui.LogInfo("  Model: " + t.model)
	case loop.StopCap:
		done.Notice = fmt.Sprintf("⚠️ Tool loop stopped after %d turn(s). Send another message to continue.", res.Turns)
	case loop.StopIdentical:
		done.Notice = fmt.Sprintf("⚠️ Stopped: the model made the identical tool call %d times in a row (stuck loop). Send another message (or rephrase the goal) to continue.", lim.IdenticalCalls)
	case loop.StopProgress:
		done.Notice = fmt.Sprintf("⚠️ Stopped: the tools returned the same results %d turns in a row. Send another message (or rephrase the goal) to continue.", lim.NoProgressTurns)
	case loop.StopInvalidArgs:
		done.Notice = "⚠️ Stopped: the model kept sending invalid tool arguments. Send another message to continue."
	}
	return done
}

// chatLLM is the chat's loop.LLM: the shared client, offering no tools when
// the chat has them off (NSFW mode, a provider without function calling).
type chatLLM struct {
	client *llm.Client
	tools  bool
}

func (c chatLLM) SendMessageStreamEvents(ctx context.Context, msgs []tui.ChatMessage, defs []tui.SkillDefinition, cb llm.StreamEventCallback) error {
	return c.client.SendMessageStreamEvents(ctx, msgs, defs, cb)
}

func (c chatLLM) GetSkills() []tui.SkillDefinition {
	if !c.tools {
		return nil
	}
	return c.client.GetSkills()
}

// chatCompactor is the chat's loop.Compactor: before every request it
// prunes old tool results through CompactContext (Jev shadow scoring
// included), and records when pruning was not enough, so the chat writes a
// summary when the turn ends.
type chatCompactor struct {
	a      *TUIClientAdapter
	jev    *jev.Client // shadow scorer, resolved when the turn started; nil: off
	window int
	used   int          // Run's goroutine only: the tracker's count, then the provider's
	saved  atomic.Int64 // the tokens the last prune freed, for the chat's count (read by the pump)
	over   atomic.Bool
}

func (c *chatCompactor) Compact(_ context.Context, history []tui.ChatMessage, last *llm.TokenUsage, force bool) ([]tui.ChatMessage, []string, bool) {
	if last != nil {
		if last.TotalTokens > 0 {
			c.used = last.TotalTokens
		} else {
			c.used = last.PromptTokens + last.CompletionTokens
		}
	}
	out := c.a.compactWith(history, c.window, c.used, force, c.jev)
	c.over.Store(out.StillOver)
	if len(out.Edits) == 0 {
		return history, nil, false
	}
	edited := tui.EditToolResults(history, out.Edits)
	if c.used > out.SavedTokens {
		c.used -= out.SavedTokens
	}
	c.saved.Store(int64(out.SavedTokens))
	return edited, []string{out.Summary}, true
}

// chatGate is the chat's loop.Gate: the prompt installed on the registry
// (runChatTUI's permission modal; tests install their own), read at ask
// time. PromptGate serializes asks and answers deny when the turn ends.
func chatGate(reg *tools.Registry) loop.Gate {
	return loop.PromptGate(func(req tools.PermissionRequest) tools.PermissionResponse {
		if fn := reg.Prompt(); fn != nil {
			return fn(req)
		}
		return tools.PermissionResponse{Decision: "deny"}
	})
}

// beginRun counts a starting turn; false once shutdown has begun.
func (a *TUIClientAdapter) beginRun() bool {
	a.runsMu.Lock()
	defer a.runsMu.Unlock()
	if a.closing {
		return false
	}
	a.running++
	return true
}

func (a *TUIClientAdapter) endRun() {
	a.runsMu.Lock()
	defer a.runsMu.Unlock()
	a.running--
	if a.running == 0 && a.idle != nil {
		close(a.idle)
		a.idle = nil
	}
}

// waitRuns stops new turns and waits up to d for running ones to end.
func (a *TUIClientAdapter) waitRuns(d time.Duration) bool {
	a.runsMu.Lock()
	a.closing = true
	if a.running == 0 {
		a.runsMu.Unlock()
		return true
	}
	if a.idle == nil {
		a.idle = make(chan struct{})
	}
	idle := a.idle
	a.runsMu.Unlock()
	select {
	case <-idle:
		return true
	case <-time.After(d):
		return false
	}
}

// shutdown cancels every running turn (the life context) and waits up to d
// for them to end, so the Env closes after its last user.
func (a *TUIClientAdapter) shutdown(d time.Duration) bool {
	if a.lifeCancel != nil {
		a.lifeCancel()
	}
	return a.waitRuns(d)
}
