package loop

import (
	"sync"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// ErrRuleInterrupt marks a request a stream rule or the watchdog cut short
// (2.0 W3). The loop re-runs the turn; Run never returns it.
var ErrRuleInterrupt = llm.ErrRuleInterrupt

// DefaultMaxRuleInterrupts bounds re-runs of one turn. Past it, steering
// fails open: the reply goes through and the interrupt is dropped.
const DefaultMaxRuleInterrupts = 2

// MetaReminder marks a reminder message in the history; its value is the
// Reminder's Source.
const MetaReminder = "steering_reminder"

// Reminder is text a stream rule or the watchdog adds to the conversation.
type Reminder struct {
	Source string // "rule:<name>" or "watchdog"
	Text   string
}

// Boundary is where reminders join the history.
type Boundary int

const (
	BoundaryRun   Boundary = iota // before a Run's first request (queued reminders)
	BoundaryTools                 // before every later request, after a turn's tool results
	BoundaryRetry                 // before re-running an interrupted turn
)

// Steering is stream rules and the watchdog as the loop sees them (W3).
// Its methods are called on Run's goroutine, except the interrupt function
// Request hands over, which is safe from any goroutine.
type Steering interface {
	// Request is called before each request. interrupt cuts that request
	// short; it is a no-op once the request has returned, and nil when the
	// turn has used its re-runs (Observe's answer is then ignored too).
	Request(turn int, interrupt func())
	// Observe sees every event before consumers do. True interrupts the
	// request in flight (only text deltas arrive during one).
	Observe(ev Event) bool
	// Calls sees a turn's complete tool calls before they are recorded or
	// run. True drops the turn's reply and re-runs it.
	Calls(turn int, calls []ToolCall) bool
	// Reminders removes and returns the reminders due at b.
	Reminders(b Boundary) []Reminder
}

// ReminderMessage is a reminder as the history holds it: a hidden user
// message (not shown in the chat, never checked by UserPromptSubmit) in a
// <system-reminder> block.
func ReminderMessage(r Reminder) Message {
	return Message{
		Role:      "user",
		Content:   "<system-reminder>\n" + r.Text + "\n</system-reminder>",
		Timestamp: time.Now(),
		Metadata:  map[string]any{"hidden": true, tui.MetaPromptHookDone: true, MetaReminder: r.Source},
	}
}

// joinReminders appends the reminders due at b (EventRule each).
func (l *Loop) joinReminders(msgs []Message, b Boundary) []Message {
	if l.Steering == nil {
		return msgs
	}
	for _, r := range l.Steering.Reminders(b) {
		m := ReminderMessage(r)
		msgs = append(msgs, m)
		l.emit(Event{Kind: EventRule, Text: r.Source, Msg: m})
	}
	return msgs
}

// interruptor is one request's interrupt switch.
type interruptor struct {
	mu     sync.Mutex
	fired  bool
	closed bool
	cancel func()
}

func (i *interruptor) fire() {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed || i.fired {
		return
	}
	i.fired = true
	i.cancel()
}

// close ends the request: later fires are no-ops. It reports whether one
// fired.
func (i *interruptor) close() bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.closed = true
	return i.fired
}
