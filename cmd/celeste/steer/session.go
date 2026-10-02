// Package steer is stream rules and the watchdog for one session (2.0 W3,
// #175): Session implements loop.Steering. A session is a chat (across its
// turns), an agent run, or one MCP chat call.
package steer

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/decide"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/loop"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/rules"
)

// DefaultEvery is the ballot's cadence, in requests (spec: every 3 turns).
const DefaultEvery = 3

// steerGap is the rate limit: at most one watchdog steer per 3 requests.
const steerGap = 3

// watchdogSource is the Source of the watchdog's reminders.
const watchdogSource = "watchdog"

// keepTurns is how many recent turns the ballot sees.
const keepTurns = 4

// Options configure a Session.
type Options struct {
	Rules     *rules.Set
	RulesMode string // config.StreamRulesMode(): off, shadow or on
	// RuntimeVerifies: an agent run with verification commands (the
	// task-complete-before-verify rule stands down).
	RuntimeVerifies bool
	// Watchdog is config.WatchdogMode(): off, shadow or on.
	Watchdog string
	// Oracle answers the ballot; nil is the heuristic.
	Oracle decide.Oracle
	// Every is the ballot cadence in requests; 0 is DefaultEvery.
	Every int
	// Goal is what the session is for (the agent goal, the MCP prompt).
	// The chat sets it per turn with SetGoal.
	Goal string
	// Context is the run's: cancelling it cancels a background ballot, as
	// Close does. nil: context.Background().
	Context context.Context
	// Logf gets shadow lines, fires and verdicts. nil: none.
	Logf func(string)
}

// Session is one session's steering. Safe for concurrent use.
type Session struct {
	mu        sync.Mutex
	o         Options
	matcher   *rules.Matcher
	pending   map[loop.Boundary][]loop.Reminder
	interrupt func() // the request in flight's; nil between requests or past its re-runs

	// The watchdog.
	ctx        context.Context // cancelled by Close or the run's context
	cancel     context.CancelFunc
	requests   int
	turns      []decide.TurnView
	lastBallot int
	lastSteer  int // request of the last watchdog steer; 0: none yet
	nits       []string
	gen        int // bumped when the goal changes: older verdicts are dropped
	ballots    sync.WaitGroup
	busy       bool
}

// New returns a Session, or nil when there is nothing to steer: stream
// rules off (or none loaded) and the watchdog off. Use Steering to put it
// on a Loop, and Close it when the session ends.
func New(o Options) *Session {
	rulesOn := o.RulesMode != config.ModeOff && o.Rules.Len() > 0
	watch := o.Watchdog == config.ModeShadow || o.Watchdog == config.ModeOn
	if !rulesOn && !watch {
		return nil
	}
	if !rulesOn {
		o.Rules = nil
	}
	if !watch {
		o.Watchdog = config.ModeOff
	}
	if o.Logf == nil {
		o.Logf = func(string) {}
	}
	if o.Oracle == nil {
		o.Oracle = decide.Heuristic{}
	}
	if o.Every <= 0 {
		o.Every = DefaultEvery
	}
	parent := o.Context
	if parent == nil {
		parent = context.Background()
	}
	m := rules.NewMatcher(o.Rules)
	m.Facts().RuntimeVerifies = o.RuntimeVerifies
	s := &Session{o: o, matcher: m, pending: map[loop.Boundary][]loop.Reminder{}}
	s.ctx, s.cancel = context.WithCancel(parent)
	return s
}

// Steering is s as a loop.Steering: a nil Session gives a nil interface,
// never a non-nil interface holding a nil pointer.
func (s *Session) Steering() loop.Steering {
	if s == nil {
		return nil
	}
	return s
}

// SetGoal sets what the ballot judges progress against (the chat: each
// turn's prompt). A changed goal drops watchdog reminders and nits not yet
// handed out, and the verdict of a ballot still running. Nil-safe.
func (s *Session) SetGoal(goal string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if goal == s.o.Goal {
		return
	}
	// A verdict about the old goal would judge the new one ("drifting from
	// what the user asked"): drop the ones pending and the one running.
	s.o.Goal = goal
	s.gen++
	s.nits = nil
	for b, rs := range s.pending {
		kept := rs[:0]
		for _, r := range rs {
			if r.Source != watchdogSource {
				kept = append(kept, r)
			}
		}
		s.pending[b] = kept
	}
}

func (s *Session) Request(_ int, interrupt func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests++
	s.matcher.StartRequest()
	s.interrupt = interrupt
}

func (s *Session) Observe(ev loop.Event) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch ev.Kind {
	case loop.EventTextDelta:
		return s.act(s.matcher.Text(ev.Text))
	case loop.EventAssistant:
		if s.o.Watchdog != config.ModeOff {
			s.turns = append(s.turns, decide.TurnView{Assistant: cut(ev.Text, 1500)})
			if len(s.turns) > keepTurns {
				s.turns = s.turns[len(s.turns)-keepTurns:]
			}
		}
	case loop.EventToolResult:
		s.matcher.ToolResult(ev.Call.Name, ev.IsError)
		if n := len(s.turns); n > 0 && s.o.Watchdog != config.ModeOff {
			s.turns[n-1].Calls = append(s.turns[n-1].Calls, decide.CallView{
				Tool: ev.Call.Name, Args: callArgs(ev.Call.Input), Result: cut(ev.Text, 800), IsError: ev.IsError,
			})
		}
	case loop.EventTurnEnd:
		if s.o.Watchdog != config.ModeOff && s.requests-s.lastBallot >= s.o.Every {
			s.startBallotLocked()
		}
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

// act records rule hits and, when acting, queues their reminders. It
// reports whether one interrupts.
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
// had used its re-runs), so no reminder is lost. Batched watchdog nits
// ride on the first reminder handed out, or go alone at a run's start.
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
	if len(s.nits) > 0 {
		nits := "Watchdog: " + strings.Join(s.nits, " ")
		switch {
		case len(out) > 0:
			out[0].Text += "\n" + nits
			s.nits = nil
		case b == loop.BoundaryRun:
			out = append(out, loop.Reminder{Source: watchdogSource, Text: nits})
			s.nits = nil
		}
	}
	return out
}

// stateLocked is what the ballot sees. The oracle redacts it before it
// leaves the machine (decide.Jev, decide.LLM).
func (s *Session) stateLocked() decide.State {
	turns := make([]decide.TurnView, len(s.turns))
	for i, t := range s.turns {
		turns[i] = decide.TurnView{Assistant: t.Assistant, Calls: append([]decide.CallView(nil), t.Calls...)}
	}
	return decide.State{Goal: cut(s.o.Goal, 2000), Turns: turns}
}

// startBallotLocked asks the ballot in the background: the loop never
// waits for it. One runs at a time; none starts once the session is
// closed or its run's context is done.
func (s *Session) startBallotLocked() {
	if s.busy || s.ctx.Err() != nil {
		return
	}
	s.busy = true
	s.lastBallot = s.requests
	state := s.stateLocked().String()
	ctx, gen := s.ctx, s.gen
	s.ballots.Add(1)
	go func() {
		defer s.ballots.Done()
		ans, _ := s.o.Oracle.Ask(ctx, state, BallotQuestions())
		s.mu.Lock()
		defer s.mu.Unlock()
		s.busy = false
		if ctx.Err() != nil {
			return // the run ended: a late verdict steers nothing
		}
		if gen != s.gen {
			s.o.Logf("watchdog: verdict dropped (the goal changed)")
			return
		}
		if interrupt := s.applyLocked(Judge(ans)); interrupt != nil {
			// Outside the lock: the callback is the loop's, and may be
			// anything that is safe from any goroutine.
			s.mu.Unlock()
			interrupt()
			s.mu.Lock()
		}
	}()
}

// applyLocked acts on a verdict by severity: a blocker interrupts the
// request in flight (or joins before the next one), a concern joins at the
// next tool boundary, nits ride on the next reminder. At most one steer
// per steerGap requests; nits are not steers, so a rate-limited steer
// still batches the verdict's nits. It returns the interrupt to call (the
// request in flight's, for a blocker), which the caller calls after
// releasing the lock.
func (s *Session) applyLocked(v Verdict) (interrupt func()) {
	s.o.Logf("watchdog: " + v.String())
	if v.Highest() == 0 {
		return nil
	}
	if s.o.Watchdog != config.ModeOn {
		s.o.Logf("watchdog (shadow): would steer: " + v.Reminder())
		return nil
	}
	var steers Verdict
	for _, f := range v.Act {
		if f.Severity == Nit {
			if !slices.Contains(s.nits, advice[f.ID]) {
				s.nits = append(s.nits, advice[f.ID])
			}
		} else {
			steers.Act = append(steers.Act, f)
		}
	}
	sev := steers.Highest()
	if sev == 0 {
		return nil
	}
	if s.lastSteer > 0 && s.requests-s.lastSteer < steerGap {
		s.o.Logf("watchdog: steer rate-limited (one per 3 turns)")
		return nil
	}
	s.lastSteer = s.requests
	r := loop.Reminder{Source: watchdogSource, Text: steers.Reminder()}
	if sev == Blocker {
		s.pending[loop.BoundaryRetry] = append(s.pending[loop.BoundaryRetry], r)
		return s.interrupt // the request in flight, if any; a no-op once it returned
	}
	s.pending[loop.BoundaryTools] = append(s.pending[loop.BoundaryTools], r)
	return nil
}

// Ballot asks the ballot now and waits for the answer: the agent's
// completion gate (spec: "before a completion is accepted"). acting is
// false when the watchdog is off or in shadow (the verdict is then logged
// only). The oracle is Guarded, so this waits at most decide.Timeout.
// Nil-safe.
func (s *Session) Ballot(ctx context.Context) (v Verdict, acting bool) {
	if s == nil {
		return Verdict{}, false
	}
	s.mu.Lock()
	mode, state, oracle := s.o.Watchdog, s.stateLocked().String(), s.o.Oracle
	s.mu.Unlock()
	if mode == config.ModeOff {
		return Verdict{}, false
	}
	ans, _ := oracle.Ask(ctx, state, BallotQuestions())
	v = Judge(ans)
	s.o.Logf("watchdog (completion): " + v.String())
	return v, mode == config.ModeOn
}

// Wait blocks until a background ballot finishes (tests). Nil-safe.
func (s *Session) Wait() {
	if s != nil {
		s.ballots.Wait()
	}
}

// Close ends the session: a background ballot is cancelled and its
// verdict dropped, no new one starts, and Close returns once it has
// finished (so nothing logs after the run). Nil-safe.
func (s *Session) Close() {
	if s == nil {
		return
	}
	s.cancel()
	s.ballots.Wait()
}

// callArgs is a call's arguments as the ballot sees them: JSON, each
// string value cut to 600 bytes, so the arguments stay parseable however
// long a write is.
func callArgs(input map[string]any) string {
	short := make(map[string]any, len(input))
	for k, v := range input {
		if str, ok := v.(string); ok {
			v = cut(str, 600)
		}
		short[k] = v
	}
	b, _ := json.Marshal(short)
	return cut(string(b), 4000)
}

// clip shortens s to at most 80 bytes for a log line, on a rune boundary.
func clip(s string) string {
	if len(s) <= 80 {
		return s
	}
	return cut(s, 77) + "..."
}

// cut keeps at most n bytes of s, on a rune boundary.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
