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
	defer func() {
		l.emit(Event{Kind: EventDone, Result: res, Err: err})
	}()

	turn := 0
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
		l.emit(Event{Kind: EventTurnStart, Turn: turn})

		rep, rerr := l.request(ctx, msgs, lim)
		if rerr != nil {
			if ctx.Err() != nil {
				res.StopReason = StopInterrupted
				return msgs, res, ctx.Err()
			}
			res.StopReason = StopError
			return msgs, res, rerr
		}

		calls := capCalls(rep.calls, lim.MaxCallsPerTurn)
		native := calls
		l.emit(Event{Kind: EventAssistant, Turn: turn, Text: rep.text, ToolNames: callNames(calls), Usage: rep.usage, Elapsed: rep.elapsed})
		res.FinalText = rep.text

		if len(calls) == 0 {
			msgs = append(msgs, Message{Role: "assistant", Content: rep.text, Timestamp: time.Now()})
			res.ToolCallsLastTurn = 0
			res.NoToolTurns++
			l.emit(Event{Kind: EventTurnEnd, Turn: turn, History: cloneHistory(msgs)})
			res.StopReason = StopDone
			return msgs, res, nil
		}

		msgs = append(msgs, Message{Role: "assistant", Content: rep.text, ToolCalls: toToolCallInfo(native), Timestamp: time.Now()})
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
	}
}

type reply struct {
	text    string
	calls   []llm.ToolCallResult
	usage   *llm.TokenUsage
	elapsed time.Duration
}

// request streams one turn. Text deltas are forwarded as they arrive.
func (l *Loop) request(ctx context.Context, msgs []Message, lim Limits) (reply, error) {
	reqCtx, cancel := ctx, context.CancelFunc(func() {})
	if lim.RequestTimeout > 0 {
		reqCtx, cancel = context.WithTimeout(ctx, lim.RequestTimeout)
	}
	var r reply
	var text strings.Builder
	acc := llm.NewToolUseAccumulator()
	start := time.Now()
	err := l.Client.SendMessageStreamEvents(reqCtx, msgs, l.Client.GetSkills(), func(ev llm.StreamEvent) {
		switch ev.Type {
		case llm.EventContentDelta:
			text.WriteString(ev.ContentDelta)
			l.emit(Event{Kind: EventTextDelta, Text: ev.ContentDelta})
		case llm.EventToolUseStart, llm.EventToolUseInputDelta, llm.EventToolUseDone:
			acc.HandleEvent(ev)
		case llm.EventMessageDone:
			r.usage = ev.Usage
		}
	})
	// Read before cancel(): afterwards the context reports Canceled.
	timedOut := lim.RequestTimeout > 0 && errors.Is(reqCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil
	cancel()
	r.elapsed = time.Since(start)
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
