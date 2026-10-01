package loop

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// Run drives turns until the model answers without tools, a limit or guard
// stops it, the provider fails, or ctx is cancelled (an interrupt). It
// returns a copy of history with the run's messages appended. Every declared
// tool call in the returned history has a result.
func (l *Loop) Run(ctx context.Context, history []Message) (msgs []Message, res Result, err error) {
	lim := l.Limits.withDefaults()
	msgs = append([]Message(nil), history...)
	l.unsynced = false
	defer func() {
		res.HistoryEdited = l.unsynced
		l.emit(Event{Kind: EventDone, Result: res, Err: err})
	}()
	if l.CheckPrompt != nil {
		checked, allowed, blocked, cerr := l.checkPrompts(ctx, msgs)
		if cerr != nil {
			res.StopReason = StopInterrupted
			return msgs, res, cerr
		}
		msgs = checked
		if blocked > 0 && allowed == 0 {
			res.StopReason = StopBlocked
			return msgs, res, nil
		}
		if allowed > 0 {
			// The consumer marks its copy now: an interrupt during the
			// first request must not leave a checked prompt unmarked.
			l.emit(Event{Kind: EventPromptsChecked, History: cloneHistory(msgs)})
		}
	}

	msgs = l.joinReminders(msgs, BoundaryRun)

	turn := 0
	ident := guard{limit: lim.IdenticalCalls}
	prog := guard{limit: lim.NoProgressTurns}
	invalidTurns := 0
	overflowRetried := false
	interrupts := 0 // steering re-runs of the current turn
	for {
		if cerr := ctx.Err(); cerr != nil {
			res.StopReason = StopInterrupted
			return msgs, res, cerr
		}
		if turn >= lim.MaxTurns {
			res.StopReason = StopCap
			return msgs, res, nil
		}
		turn++
		res.Turns = turn
		msgs, _ = l.joinSteers(ctx, msgs)
		if l.CheckPrompt != nil && ctx.Err() != nil {
			// A steer check cut short: the steer whose check errored and
			// the ones after it went back in the queue (steers checked and
			// joined before it stay in the history), and no request is sent.
			// A CheckPrompt error that is not an interrupt requeues the
			// same way but the turn still sends; in the final-reply branch
			// below it ends the run StopDone, leaving them for TakeSteers.
			turn--
			res.Turns = turn
			res.StopReason = StopInterrupted
			return msgs, res, ctx.Err()
		}
		if turn > 1 {
			msgs = l.joinReminders(msgs, BoundaryTools)
		}
		l.emit(Event{Kind: EventTurnStart, Turn: turn})
		if l.Compact != nil {
			msgs, _ = l.compact(ctx, msgs, false)
		}

		allow := interrupts < lim.MaxRuleInterrupts
		rep, rerr := l.request(ctx, msgs, lim, turn, allow)
		if rerr != nil {
			if ctx.Err() != nil {
				res.StopReason = StopInterrupted
				return msgs, res, ctx.Err()
			}
			if errors.Is(rerr, ErrRuleInterrupt) {
				msgs = l.rerun(msgs, turn)
				interrupts++
				turn--
				res.Turns = turn
				continue
			}
			// The history overflowed anyway (an estimate was off, or the
			// window is smaller than configured): compact harder and retry
			// the same turn once (#174).
			if errors.Is(rerr, llm.ErrContextOverflow) && l.Compact != nil && !overflowRetried {
				overflowRetried = true
				if out, changed := l.compact(ctx, msgs, true); changed {
					msgs = out
					turn--
					continue
				}
			}
			res.StopReason = StopError
			return msgs, res, rerr
		}
		overflowRetried = false
		l.lastUsage = rep.usage
		if rep.blocksRejected {
			// The provider refused the blocks this request replayed: a
			// sanctioned history edit, so later requests (and the chat,
			// via the snapshots) send the neutral view (2.0 F3).
			msgs = tui.StripProviderBlocks(msgs)
			l.unsynced = true
		}

		calls := capCalls(rep.calls, lim.MaxCallsPerTurn)
		native := calls
		if len(calls) == 0 && lim.TextToolCalls {
			calls = capCalls(parseTextToolCalls(rep.text), lim.MaxCallsPerTurn)
		}
		// Rules on tool arguments see the calls before they are recorded
		// or run; past the turn's re-runs their interrupt is dropped.
		if l.Steering != nil && len(calls) > 0 && l.Steering.Calls(turn, steeringCalls(calls)) && allow {
			msgs = l.rerun(msgs, turn)
			interrupts++
			turn--
			res.Turns = turn
			continue
		}
		interrupts = 0
		l.emit(Event{Kind: EventAssistant, Turn: turn, Text: rep.text, ToolNames: callNames(calls), Usage: rep.usage, Elapsed: rep.elapsed})
		res.FinalText = rep.text

		if len(calls) == 0 {
			msgs = append(msgs, tui.AttachProviderBlocks(Message{Role: "assistant", Content: rep.text, Timestamp: time.Now()}, rep.blocks))
			res.ToolCallsLastTurn = 0
			res.NoToolTurns++
			l.emit(Event{Kind: EventTurnEnd, Turn: turn, History: cloneHistory(msgs)})
			if l.hasSteers() && l.CheckPrompt == nil {
				continue // a steer arrived during the final reply: answer it
			}
			// With CheckPrompt, a steer that arrived during the final reply
			// is answered unless every one of them was blocked or no turn
			// is left (TakeSteers hands those back).
			if l.hasSteers() && turn < lim.MaxTurns {
				var joined int
				msgs, joined = l.joinSteers(ctx, msgs)
				if ctx.Err() != nil {
					res.StopReason = StopInterrupted
					return msgs, res, ctx.Err()
				}
				if joined > 0 {
					continue
				}
			}
			res.StopReason = StopDone
			return msgs, res, nil
		}

		// Checked before the turn is recorded or run, so the stopped turn
		// leaves no unpaired tool_calls behind.
		if ident.observe(batchSig(calls)) {
			res.StopReason = StopIdentical
			return msgs, res, nil
		}
		turnMsg := Message{Role: "assistant", Content: rep.text, ToolCalls: toToolCallInfo(native), Timestamp: time.Now()}
		if len(native) == len(rep.calls) && len(calls) == len(native) {
			// The blocks hold every call the provider made: after
			// MaxCallsPerTurn dropped some, they would replay tool_use
			// blocks that never get a result; text-format calls have no
			// tool_use blocks at all (2.0 F3).
			turnMsg = tui.AttachProviderBlocks(turnMsg, rep.blocks)
		}
		msgs = append(msgs, turnMsg)
		l.emit(Event{Kind: EventCallsRecorded, Turn: turn, History: cloneHistory(msgs)})
		out := l.runCalls(ctx, calls, lim)
		msgs = append(msgs, out.messages...)
		res.ToolCallsLastTurn = len(calls)
		res.ToolCalls += len(calls)
		res.NoToolTurns = 0
		l.emit(Event{Kind: EventTurnEnd, Turn: turn, History: cloneHistory(msgs)})
		if ctx.Err() != nil {
			res.StopReason = StopInterrupted
			return msgs, res, ctx.Err()
		}
		if out.anyInvalid {
			invalidTurns++
		} else {
			invalidTurns = 0
		}
		if lim.MaxInvalidArgTurns > 0 && invalidTurns >= lim.MaxInvalidArgTurns {
			res.StopReason = StopInvalidArgs
			return msgs, res, nil
		}
		if prog.observe(out.resultSig) {
			res.StopReason = StopProgress
			return msgs, res, nil
		}
	}
}

// Steer queues a user message. It joins the conversation at the next tool
// boundary (before the next request). Safe from any goroutine.
func (l *Loop) Steer(msg string) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return
	}
	l.mu.Lock()
	l.steers = append(l.steers, msg)
	l.mu.Unlock()
}

func (l *Loop) hasSteers() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.steers) > 0
}

// joinSteers adds queued steers to msgs as user messages and returns how
// many joined. With CheckPrompt, each is checked first: a blocked one is
// dropped (EventSteerBlocked); an interrupted check puts it and the ones
// after it back in the queue.
func (l *Loop) joinSteers(ctx context.Context, msgs []Message) ([]Message, int) {
	l.mu.Lock()
	pending := l.steers
	l.steers = nil
	l.mu.Unlock()
	joined := 0
	for i, s := range pending {
		msg := Message{Role: "user", Content: s, Timestamp: time.Now()}
		if l.CheckPrompt != nil {
			out, v, err := l.CheckPrompt(ctx, msg)
			if err != nil {
				l.requeue(pending[i:])
				break
			}
			if v.Blocked {
				l.emit(Event{Kind: EventSteerBlocked, Msg: msg, Text: v.Reason})
				continue
			}
			msg = markChecked(out)
		}
		msgs = append(msgs, msg)
		joined++
		l.emit(Event{Kind: EventSteered, Text: s, Msg: msg})
	}
	return msgs, joined
}

// requeue puts steers back at the front of the queue.
func (l *Loop) requeue(steers []string) {
	l.mu.Lock()
	l.steers = append(append([]string(nil), steers...), l.steers...)
	l.mu.Unlock()
}

// TakeSteers removes and returns the steers no run has joined: typed after
// the last tool boundary of a run that then ended (an interrupt, a guard, a
// cap). The caller decides what to do with them; the chat sends them next.
func (l *Loop) TakeSteers() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.steers
	l.steers = nil
	return s
}

// compact runs the Compactor with the previous request's usage.
func (l *Loop) compact(ctx context.Context, msgs []Message, force bool) ([]Message, bool) {
	usage := l.lastUsage
	l.lastUsage = nil
	out, notes, changed := l.Compact.Compact(ctx, msgs, usage, force)
	for _, n := range notes {
		l.emit(Event{Kind: EventCompacted, Text: n})
	}
	if !changed {
		return msgs, false
	}
	l.unsynced = true
	return out, true
}

type reply struct {
	text    string
	calls   []llm.ToolCallResult
	usage   *llm.TokenUsage
	elapsed time.Duration
	blocks  *tui.ProviderBlocks // EventMessageDone's; nil when the backend keeps none
	// blocksRejected: the provider refused the replayed blocks (2.0 F3).
	blocksRejected bool
}

// rerun drops an interrupted turn's reply (EventRuleInterrupt) and joins
// the reminders for its re-run.
func (l *Loop) rerun(msgs []Message, turn int) []Message {
	l.emit(Event{Kind: EventRuleInterrupt, Turn: turn})
	return l.joinReminders(msgs, BoundaryRetry)
}

// steeringCalls is a turn's calls as Steering sees them.
func steeringCalls(calls []llm.ToolCallResult) []ToolCall {
	out := make([]ToolCall, len(calls))
	for i, c := range calls {
		input, _ := parseInput(c.Arguments)
		out[i] = ToolCall{ID: c.ID, Name: c.Name, Input: input}
	}
	return out
}

// request streams one turn. Text deltas are forwarded as they arrive. With
// Steering, allow lets it interrupt this request: the request then returns
// ErrRuleInterrupt, whatever the stream did.
func (l *Loop) request(ctx context.Context, msgs []Message, lim Limits, turn int, allow bool) (reply, error) {
	reqCtx, cancel := ctx, context.CancelFunc(func() {})
	if lim.RequestTimeout > 0 {
		reqCtx, cancel = context.WithTimeout(ctx, lim.RequestTimeout)
	}
	intr := &interruptor{cancel: func() {}}
	if l.Steering != nil {
		var cancelCause context.CancelCauseFunc
		reqCtx, cancelCause = context.WithCancelCause(reqCtx)
		defer cancelCause(nil)
		intr.cancel = func() { cancelCause(ErrRuleInterrupt) }
		if allow {
			l.Steering.Request(turn, intr.fire)
		} else {
			l.Steering.Request(turn, nil)
		}
	}
	var r reply
	var text strings.Builder
	acc := llm.NewToolUseAccumulator()
	start := time.Now()
	err := l.Client.SendMessageStreamEvents(reqCtx, withHookContext(msgs), l.Client.GetSkills(), func(ev llm.StreamEvent) {
		switch ev.Type {
		case llm.EventContentDelta:
			text.WriteString(ev.ContentDelta)
			if l.emit(Event{Kind: EventTextDelta, Text: ev.ContentDelta}) && allow {
				intr.fire()
			}
		case llm.EventToolUseStart, llm.EventToolUseInputDelta, llm.EventToolUseDone:
			acc.HandleEvent(ev)
		case llm.EventMessageDone:
			r.usage = ev.Usage
			r.blocks = ev.ProviderBlocks
			r.blocksRejected = ev.BlocksRejected
		}
	})
	// Read before cancel(): afterwards the context reports Canceled.
	timedOut := lim.RequestTimeout > 0 && errors.Is(reqCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil
	interrupted := intr.close()
	cancel()
	r.elapsed = time.Since(start)
	if interrupted && ctx.Err() == nil {
		return r, ErrRuleInterrupt
	}
	if err != nil {
		if timedOut {
			return r, &TurnTimeoutError{Timeout: lim.RequestTimeout, Err: err}
		}
		return r, err
	}
	r.text = text.String()
	r.calls = acc.CompletedCalls()
	return r, nil
}

func capCalls(calls []llm.ToolCallResult, max int) []llm.ToolCallResult {
	if max > 0 && len(calls) > max {
		return calls[:max]
	}
	return calls
}

func callNames(calls []llm.ToolCallResult) []string {
	if len(calls) == 0 {
		return nil
	}
	names := make([]string, len(calls))
	for i, c := range calls {
		names[i] = c.Name
	}
	return names
}

func toToolCallInfo(calls []llm.ToolCallResult) []tui.ToolCallInfo {
	if len(calls) == 0 {
		return nil
	}
	out := make([]tui.ToolCallInfo, len(calls))
	for i, c := range calls {
		out[i] = tui.ToolCallInfo{ID: c.ID, Name: c.Name, Arguments: c.Arguments, ThoughtSignature: c.ThoughtSignature}
	}
	return out
}

func cloneHistory(msgs []Message) []Message {
	return append([]Message(nil), msgs...)
}
