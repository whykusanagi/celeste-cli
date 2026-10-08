package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/sashabaranov/go-openai"
)

// responsesTurn is one Responses reply, assembled from its stream.
type responsesTurn struct {
	text   string
	calls  []ToolCallResult // ID is the call_id
	items  []json.RawMessage
	usage  *TokenUsage
	finish string
}

// responsesEvents is the part of *openai.ResponseStream the reader uses.
type responsesEvents interface {
	Recv() (openai.ResponseStreamEvent, error)
}

// errResponsesCut: the stream closed before a terminal event (ruling 9). It
// wraps io.ErrUnexpectedEOF so the retry policy treats it as a network
// failure; the Client does not retry once output reached the caller.
var errResponsesCut = fmt.Errorf("openai responses: stream closed before the response finished: %w", io.ErrUnexpectedEOF)

// responsesStreamError is a response.failed or error event. Its code tells
// the retry policy whether it stands for a transient 5xx or 429.
type responsesStreamError struct{ code, msg string }

func (e *responsesStreamError) Error() string {
	if e.code == "" {
		return "openai responses: " + e.msg
	}
	return "openai responses: " + e.code + ": " + e.msg
}

// retryKind maps the server's code to a retry class; ok is false for codes
// left to classifyError's message matching.
func (e *responsesStreamError) retryKind() (errKind, bool) {
	switch e.code {
	case "server_error", "server_is_overloaded", "internal_error":
		return kindServer, true
	case "rate_limit_exceeded", "slow_down":
		return kindRateLimit, true
	}
	return kindNone, false
}

// readResponses reads one reply. Text deltas and tool calls are emitted as
// StreamEvents as they arrive (tool calls keyed by call_id); each finished
// output item's bytes are kept exactly as sent (from the event's Raw). It
// returns at response.completed or response.incomplete; response.failed, an
// error event or a stream that ends first is an error.
func readResponses(stream responsesEvents, emit func(StreamEvent)) (responsesTurn, error) {
	var turn responsesTurn
	type pending struct{ callID, name, args string }
	calls := map[string]*pending{} // by item id
	items := map[int]json.RawMessage{}
	for {
		ev, err := stream.Recv()
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return turn, errResponsesCut
		}
		if err != nil {
			return turn, err
		}
		switch ev.Type {
		case openai.ResponseStreamEventOutputTextDelta, openai.ResponseStreamEventRefusalDelta:
			// A refusal is the model's answer: shown as text like any other.
			turn.text += ev.Delta
			emit(StreamEvent{Type: EventContentDelta, ContentDelta: ev.Delta})
		case openai.ResponseStreamEventOutputItemAdded:
			if ev.Item != nil && ev.Item.Type == "function_call" {
				calls[ev.Item.ID] = &pending{callID: ev.Item.CallID, name: ev.Item.Name}
				emit(StreamEvent{Type: EventToolUseStart, ToolUseID: ev.Item.CallID, ToolName: ev.Item.Name})
			}
		case openai.ResponseStreamEventFunctionArgumentsDelta:
			if p := calls[ev.ItemID]; p != nil {
				p.args += ev.Delta
				emit(StreamEvent{Type: EventToolUseInputDelta, ToolUseID: p.callID, InputDelta: ev.Delta})
			}
		case openai.ResponseStreamEventOutputItemDone:
			var wrap struct {
				Item json.RawMessage `json:"item"`
			}
			if json.Unmarshal(ev.Raw, &wrap) == nil && len(wrap.Item) > 0 {
				items[ev.OutputIndex] = wrap.Item
			}
			if ev.Item != nil && ev.Item.Type == "function_call" {
				p := calls[ev.Item.ID]
				if p == nil {
					emit(StreamEvent{Type: EventToolUseStart, ToolUseID: ev.Item.CallID, ToolName: ev.Item.Name})
					p = &pending{}
				}
				args := ev.Item.Arguments
				if args == "" {
					args = p.args
				}
				turn.calls = append(turn.calls, ToolCallResult{ID: ev.Item.CallID, Name: ev.Item.Name, Arguments: args})
				emit(StreamEvent{Type: EventToolUseDone, ToolUseID: ev.Item.CallID, ToolName: ev.Item.Name, CompleteInput: args})
			}
		case openai.ResponseStreamEventCompleted, openai.ResponseStreamEventIncomplete:
			turn.items = orderedItems(items)
			turn.usage = responsesUsage(ev.Response)
			turn.finish = responsesFinish(ev.Type, ev.Response, len(turn.calls))
			return turn, nil
		case openai.ResponseStreamEventFailed:
			e := &responsesStreamError{msg: "response failed"}
			if ev.Response != nil && ev.Response.Error != nil {
				e = &responsesStreamError{code: ev.Response.Error.Code, msg: ev.Response.Error.Message}
			}
			return turn, e
		case openai.ResponseStreamEventError:
			// OpenAI sends code and message nested under "error"; the
			// documented form has them at the top level.
			e := &responsesStreamError{code: ev.Code, msg: ev.Message}
			if e.msg == "" && ev.Error != nil {
				e = &responsesStreamError{code: ev.Error.Code, msg: ev.Error.Message}
			}
			if e.msg == "" {
				e.msg = "stream error"
			}
			return turn, e
		}
	}
}

func orderedItems(items map[int]json.RawMessage) []json.RawMessage {
	idx := make([]int, 0, len(items))
	for i := range items {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	out := make([]json.RawMessage, 0, len(idx))
	for _, i := range idx {
		out = append(out, items[i])
	}
	return out
}

func responsesUsage(r *openai.CreateResponseResponse) *TokenUsage {
	if r == nil || r.Usage == nil {
		return nil
	}
	u := &TokenUsage{PromptTokens: r.Usage.InputTokens, CompletionTokens: r.Usage.OutputTokens, TotalTokens: r.Usage.TotalTokens}
	if d := r.Usage.InputTokensDetails; d != nil {
		u.CacheReadTokens = d.CachedTokens
	}
	return u
}

// responsesFinish maps the terminal event to the finish reasons the other
// backends use (ruling 9).
func responsesFinish(typ openai.ResponseStreamEventType, r *openai.CreateResponseResponse, calls int) string {
	if typ == openai.ResponseStreamEventIncomplete {
		if r != nil && r.IncompleteDetails != nil && r.IncompleteDetails.Reason == "content_filter" {
			return "content_filter"
		}
		return "length"
	}
	if calls > 0 {
		return "tool_calls"
	}
	return "stop"
}
