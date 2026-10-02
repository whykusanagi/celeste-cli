// Package steer is stream rules (and, from W3-2, the watchdog) for one
// session (2.0 W3, #175): Session implements loop.Steering. A session is a
// chat (across its turns), an agent run, or one MCP chat call.
package steer

import (
	"fmt"
	"sync"
	"unicode/utf8"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/loop"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/rules"
)

// Options configure a Session.
type Options struct {
	Rules     *rules.Set
	RulesMode string // config.StreamRulesMode(): off, shadow or on
	// RuntimeVerifies: an agent run with verification commands (the
	// task-complete-before-verify rule stands down).
	RuntimeVerifies bool
	// Logf gets shadow lines ("rule X would interrupt") and fires. nil: none.
	Logf func(string)
}

// Session is one session's steering. Safe for concurrent use.
type Session struct {
	mu        sync.Mutex
	o         Options
	matcher   *rules.Matcher
	pending   map[loop.Boundary][]loop.Reminder
	interrupt func() // the request in flight's; nil between requests or past its re-runs
}

// New returns a Session, or nil when there is nothing to steer (stream
// rules off or no rules). Use Steering to put it on a Loop.
func New(o Options) *Session {
	if o.RulesMode == config.ModeOff || o.Rules.Len() == 0 {
		return nil
	}
	if o.Logf == nil {
		o.Logf = func(string) {}
	}
	m := rules.NewMatcher(o.Rules)
	m.Facts().RuntimeVerifies = o.RuntimeVerifies
	return &Session{o: o, matcher: m, pending: map[loop.Boundary][]loop.Reminder{}}
}

// Steering is s as a loop.Steering: a nil Session gives a nil interface,
// never a non-nil interface holding a nil pointer.
func (s *Session) Steering() loop.Steering {
	if s == nil {
		return nil
	}
	return s
}

// Facts are the session's rule facts (TTS ran, spawn ran). Nil-safe.
func (s *Session) Facts() rules.Facts {
	if s == nil {
		return rules.Facts{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return *s.matcher.Facts()
}

func (s *Session) Request(_ int, interrupt func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.matcher.StartRequest()
	s.interrupt = interrupt
}

func (s *Session) Observe(ev loop.Event) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch ev.Kind {
	case loop.EventTextDelta:
		return s.act(s.matcher.Text(ev.Text))
	case loop.EventToolResult:
		s.matcher.ToolResult(ev.Call.Name, ev.IsError)
	}
	return false
}

var _ loop.StreamEnder = (*Session)(nil)

// EndStream scans the reply text the matcher's batching held back
// (loop.StreamEnder).
func (s *Session) EndStream() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.act(s.matcher.Flush())
}

func (s *Session) Calls(_ int, calls []loop.ToolCall) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	in := make([]rules.Call, len(calls))
	for i, c := range calls {
		in[i] = rules.Call{ID: c.ID, Name: c.Name, Input: c.Input}
	}
	return s.act(s.matcher.Calls(in))
}

// act records hits and, when acting, queues their reminders. It reports
// whether one interrupts.
func (s *Session) act(hits []rules.Hit) bool {
	interrupt := false
	for _, h := range hits {
		acting := s.o.RulesMode == config.ModeOn
		rules.Record(h.Rule.Name, acting)
		if !acting {
			s.o.Logf(fmt.Sprintf("stream rule %s would %s (matched %q in %s)", h.Rule.Name, h.Rule.Action, clip(h.Text), h.Scope))
			continue
		}
		s.o.Logf(fmt.Sprintf("stream rule %s: %s (matched %q in %s)", h.Rule.Name, h.Rule.Action, clip(h.Text), h.Scope))
		r := loop.Reminder{Source: "rule:" + h.Rule.Name, Text: h.Rule.Message}
		switch h.Rule.Action {
		case rules.Interrupt:
			s.pending[loop.BoundaryRetry] = append(s.pending[loop.BoundaryRetry], r)
			interrupt = true
		case rules.Append:
			s.pending[loop.BoundaryTools] = append(s.pending[loop.BoundaryTools], r)
		case rules.Queue:
			s.pending[loop.BoundaryRun] = append(s.pending[loop.BoundaryRun], r)
		}
	}
	return interrupt
}

// Reminders hands out what is due at b. A run's start also takes appended
// reminders a final reply left behind, and any boundary but a retry takes
// interrupt reminders whose interrupt the loop did not honour (the turn
// had used its re-runs), so no reminder is lost.
func (s *Session) Reminders(b loop.Boundary) []loop.Reminder {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []loop.Reminder
	take := func(k loop.Boundary) {
		out = append(out, s.pending[k]...)
		delete(s.pending, k)
	}
	switch b {
	case loop.BoundaryRetry:
		take(loop.BoundaryRetry)
	case loop.BoundaryTools:
		take(loop.BoundaryRetry)
		take(loop.BoundaryTools)
	case loop.BoundaryRun:
		take(loop.BoundaryRetry)
		take(loop.BoundaryTools)
		take(loop.BoundaryRun)
	}
	return out
}

// clip shortens s to at most 80 bytes for a log line, on a rune boundary.
func clip(s string) string {
	if len(s) <= 80 {
		return s
	}
	cut := 77
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}
