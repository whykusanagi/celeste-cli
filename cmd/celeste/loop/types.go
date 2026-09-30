// Package loop is celeste's one tool loop (2.0 F2): every mode streams a
// request, runs the model's tool calls and feeds the results back through
// Loop.Run. Permissions (Gate), hooks, limits and compaction are separate
// control planes supplied by each adopter.
package loop

import (
	"context"
	"errors"
	"sync"
	"time"

	ctxmgr "github.com/whykusanagi/celeste-cli/cmd/celeste/context"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// Message is one chat message. The loop copies the history it is given and
// only appends to the copy.
type Message = tui.ChatMessage

// LLM is the part of *llm.Client the loop uses.
type LLM interface {
	SendMessageStreamEvents(ctx context.Context, messages []tui.ChatMessage, tools []tui.SkillDefinition, cb llm.StreamEventCallback) error
	GetSkills() []tui.SkillDefinition
}

const (
	// DefaultMaxTurns bounds a Run whose Limits.MaxTurns is unset.
	DefaultMaxTurns = 25
	// DefaultToolTimeout bounds a tool without its own tools.Timeouter.
	DefaultToolTimeout = 45 * time.Second
	// DefaultHookBudget covers one PreToolUse and one PostToolUse hook at
	// F0's default 30 s timeout each.
	DefaultHookBudget = 60 * time.Second
)

// Limits is plain data. For guards, caps and spill, zero means off.
type Limits struct {
	MaxTurns           int           // <=0: DefaultMaxTurns
	MaxCallsPerTurn    int           // calls beyond it are dropped before the turn is recorded
	IdenticalCalls     int           // stop when the identical call batch repeats this many turns
	NoProgressTurns    int           // stop when tools return identical results this many turns
	SpillBytes         int           // results larger than this spill to disk
	ToolTimeout        time.Duration // <=0: DefaultToolTimeout; a tools.Timeouter overrides it
	RequestTimeout     time.Duration // per-request deadline; the client's per-attempt deadline still applies
	MaxInvalidArgTurns int           // stop after this many turns in a row with bad tool arguments
	TextToolCalls      bool          // parse <tool_call> blocks from text when a turn has no native calls
	// HookBudget is how long PreToolUse/PostToolUse hooks may add to a call
	// before the watchdog abandons it (tool timeout + budget; gated runs:
	// counted from the Gate's answer). <=0: DefaultHookBudget.
	HookBudget time.Duration
}

// DefaultLimits are the spec's values: 25 turns, identical-call guard 3,
// progress guard 6, spill at 128 KiB, 45 s per tool.
func DefaultLimits() Limits {
	return Limits{
		MaxTurns:        DefaultMaxTurns,
		IdenticalCalls:  3,
		NoProgressTurns: 6,
		SpillBytes:      ctxmgr.DefaultMaxToolResultBytes,
		ToolTimeout:     DefaultToolTimeout,
		HookBudget:      DefaultHookBudget,
	}
}

func (lim Limits) withDefaults() Limits {
	if lim.MaxTurns <= 0 {
		lim.MaxTurns = DefaultMaxTurns
	}
	if lim.ToolTimeout <= 0 {
		lim.ToolTimeout = DefaultToolTimeout
	}
	if lim.HookBudget <= 0 {
		lim.HookBudget = DefaultHookBudget
	}
	return lim
}

// StopReason says why Run returned.
type StopReason string

const (
	StopDone        StopReason = "done"         // a turn with no tool calls
	StopCap         StopReason = "cap"          // Limits.MaxTurns
	StopIdentical   StopReason = "identical"    // identical-call guard
	StopProgress    StopReason = "progress"     // progress guard
	StopInterrupted StopReason = "interrupted"  // ctx cancelled
	StopInvalidArgs StopReason = "invalid_args" // Limits.MaxInvalidArgTurns
	StopError       StopReason = "error"        // the provider failed; Run returns the error
)

// Result describes one Run. The completion gate is not here: the agent layer
// (and W3) read it.
type Result struct {
	FinalText         string // last assistant text, verbatim
	ToolCallsLastTurn int
	NoToolTurns       int // consecutive tool-free turns at the end of this Run
	StopReason        StopReason
	Turns             int // turns started this Run (an overflow retry is the same turn)
	ToolCalls         int // tool calls executed this Run
}

// EventKind identifies an Event.
type EventKind int

const (
	// EventTurnStart (Turn) may repeat with the same Turn number after an
	// overflow retry re-runs the turn: consumers should key on Turn, not
	// count events.
	EventTurnStart  EventKind = iota
	EventTextDelta            // Text
	EventAssistant            // Turn, Text, ToolNames, Usage, Elapsed: the model's reply
	EventToolStart            // Call
	EventToolResult           // Call, Text (what the model receives), IsError
	EventCompacted            // Text
	EventSteered              // Text
	EventNotice               // Text: a non-fatal problem (hook error, spill failure)
	EventTurnEnd              // Turn, History: a consistent history snapshot
	EventDone                 // Result, Err: always the last event of a Run
)

// Event is one step of a Run, for renderers and adopters.
type Event struct {
	Kind      EventKind
	Turn      int
	Text      string
	ToolNames []string
	Usage     *llm.TokenUsage
	Elapsed   time.Duration
	Call      ToolCall
	IsError   bool
	History   []Message
	Result    Result
	Err       error
}

// ToolCall is one call as hooks and events see it.
type ToolCall struct {
	ID    string
	Name  string
	Input map[string]any
}

// Gate answers a permission Ask for one run. Its lifetime differs by mode
// (TUI modal, agent prompt or none, MCP none), so each adopter passes it in.
// A nil Gate means an Ask is denied. Loop serializes calls to Ask itself
// (gateMu in exec.go), so a Gate implementation need not handle concurrent
// calls even when parallel-safe tool calls all Ask in the same batch.
type Gate interface {
	Ask(ctx context.Context, req tools.PermissionRequest) tools.PermissionResponse
}

// GateFunc adapts a function to Gate.
type GateFunc func(ctx context.Context, req tools.PermissionRequest) tools.PermissionResponse

func (f GateFunc) Ask(ctx context.Context, req tools.PermissionRequest) tools.PermissionResponse {
	return f(ctx, req)
}

// PromptGate adapts a blocking tools.PromptFunc (the TUI modal) to a Gate.
// A nil fn gives a nil Gate. Asks are serialized: parallel calls in one
// batch must not open two modals at once. When ctx ends first (the call
// was abandoned, Esc, or the chat quitting) the ask answers deny at once;
// the prompt's own goroutine keeps the lock until the prompt returns, so a
// later ask never opens a second modal over one still showing. An ask whose
// ctx ended while it waited for the lock never calls fn.
func PromptGate(fn tools.PromptFunc) Gate {
	if fn == nil {
		return nil
	}
	var mu sync.Mutex
	return GateFunc(func(ctx context.Context, req tools.PermissionRequest) tools.PermissionResponse {
		if ctx.Err() != nil {
			return tools.PermissionResponse{Decision: "deny"}
		}
		answer := make(chan tools.PermissionResponse, 1)
		go func() {
			mu.Lock()
			defer mu.Unlock()
			if ctx.Err() != nil {
				answer <- tools.PermissionResponse{Decision: "deny"}
				return
			}
			answer <- fn(req)
		}()
		select {
		case r := <-answer:
			return r
		case <-ctx.Done():
			return tools.PermissionResponse{Decision: "deny"}
		}
	})
}

// Compactor keeps the history inside the window. The loop calls it before
// every request, and once with force set after a context-overflow error.
// lastUsage is the provider's count for the previous request (nil when there
// was none since the last call). notes become EventCompacted. Compaction
// hooks (PreCompact/PostCompact) are the compactor's business: it knows when
// a summary is written.
type Compactor interface {
	Compact(ctx context.Context, history []Message, lastUsage *llm.TokenUsage, force bool) (out []Message, notes []string, changed bool)
}

// ErrTurnTimeout matches (errors.Is) a request that hit Limits.RequestTimeout.
var ErrTurnTimeout = errors.New("loop: request exceeded its per-turn timeout")

// TurnTimeoutError keeps the provider's message unchanged and marks it as
// a per-turn timeout.
type TurnTimeoutError struct {
	Timeout time.Duration
	Err     error
}

func (e *TurnTimeoutError) Error() string        { return e.Err.Error() }
func (e *TurnTimeoutError) Unwrap() error        { return e.Err }
func (e *TurnTimeoutError) Is(target error) bool { return target == ErrTurnTimeout }

// Loop runs one conversation's tool loop. Configure the exported fields,
// then call Run (once per step; the Loop may be reused).
type Loop struct {
	Client  LLM
	Tools   *tools.Registry
	Limits  Limits
	Gate    Gate      // nil: an Ask is denied
	Compact Compactor // nil: no compaction
	// Tool hooks run inside Tools (F0); the loop fires no hooks itself.
	// SessionID names the spill directory for oversized results.
	SessionID string
	// SpillDir overrides the spill base directory; "" uses the default.
	SpillDir string

	mu        sync.Mutex
	steers    []string
	events    chan Event
	lastUsage *llm.TokenUsage // Run's goroutine only
	spillSeq  int             // Run's goroutine only
	gateMu    sync.Mutex      // serializes calls to Gate.Ask across a batch
}

// Events returns the event stream. Call it before Run. Sends are unbuffered:
// the loop waits for the consumer to take each event, so a consumer that has
// received event N+1 has finished handling event N, and once Run returns the
// consumer has handled everything before EventDone. A consumer must keep
// reading until EventDone. Without a call to Events the loop emits nothing.
func (l *Loop) Events() <-chan Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.events == nil {
		l.events = make(chan Event)
	}
	return l.events
}

func (l *Loop) emit(ev Event) {
	l.mu.Lock()
	ch := l.events
	l.mu.Unlock()
	if ch != nil {
		ch <- ev
	}
}
