package steer

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/decide"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/loop"
)

func TestJudgeBandsAndSeverity(t *testing.T) {
	v := Judge(map[string]decide.Answer{
		QUnsafe:       {P: 0.9},
		QLooping:      {P: 0.75},
		QPersonaBreak: {P: 0.5},
		QDrifting:     {P: 0.2},
		QOnTrack:      {Score: 1}, // level 2 of 10: (9-1)/9 = 0.89 off track
		"unknown":     {P: 1},
	})
	if len(v.Act) != 3 || v.Act[0].ID != QUnsafe || v.Highest() != Blocker {
		t.Fatalf("act = %+v", v.Act)
	}
	if !v.Has(QOnTrack) || !v.Has(QLooping) || v.Has(QPersonaBreak) {
		t.Errorf("act = %+v", v.Act)
	}
	if len(v.Logged) != 1 || v.Logged[0].ID != QPersonaBreak {
		t.Errorf("0.30–0.70 must be logged only: %+v", v.Logged)
	}
	if !strings.Contains(v.Reminder(), "destructive") {
		t.Errorf("reminder = %q", v.Reminder())
	}
	if (Verdict{}).Highest() != 0 || Judge(nil).String() != "all clear" {
		t.Error("an empty verdict acts on nothing")
	}
}

func q(id string) decide.Question {
	for _, q := range BallotQuestions() {
		if q.ID == id {
			return q
		}
	}
	panic(id)
}

func answer(t *testing.T, id string, st decide.State) (float64, bool) {
	t.Helper()
	h := q(id).Heuristic
	if h == nil {
		return 0, false
	}
	a, ok := h(st)
	return a.P, ok
}

func TestBallotHeuristics(t *testing.T) {
	read := decide.CallView{Tool: "read_file", Args: `{"path":"a.go"}`}
	write := decide.CallView{Tool: "write_file", Args: `{"path":"a.go","content":"package a // darling~"}`}
	bash := decide.CallView{Tool: "bash", Args: `{"command":"go test ./..."}`}
	force := decide.CallView{Tool: "bash", Args: `{"command":"git push --force"}`}
	cases := []struct {
		id    string
		st    decide.State
		p     float64
		known bool
	}{
		{QLooping, decide.State{Turns: []decide.TurnView{{Calls: []decide.CallView{read}}, {Calls: []decide.CallView{read}}}}, 0.8, true},
		{QLooping, decide.State{Turns: []decide.TurnView{{Calls: []decide.CallView{read}}, {Calls: []decide.CallView{bash}}}}, 0.1, true},
		{QLooping, decide.State{Turns: []decide.TurnView{{}}}, 0, false},
		{QUnverified, decide.State{Turns: []decide.TurnView{{Calls: []decide.CallView{write}}, {Assistant: "All tests pass now."}}}, 0.8, true},
		{QUnverified, decide.State{Turns: []decide.TurnView{{Calls: []decide.CallView{write, bash}}, {Assistant: "All tests pass now."}}}, 0.1, true},
		{QUnverified, decide.State{Turns: []decide.TurnView{{Calls: []decide.CallView{write}}, {Assistant: "Next I will run the tests."}}}, 0.1, true},
		{QPersonaBreak, decide.State{Turns: []decide.TurnView{{Calls: []decide.CallView{write}}}}, 0.8, true},
		{QPersonaBreak, decide.State{Turns: []decide.TurnView{{Calls: []decide.CallView{bash}}}}, 0.1, true},
		{QUnsafe, decide.State{Turns: []decide.TurnView{{Calls: []decide.CallView{force}}}}, 0.8, true},
		{QUnsafe, decide.State{Call: &bash}, 0, false},
		{QDrifting, decide.State{}, 0, false},
	}
	for i, c := range cases {
		p, ok := answer(t, c.id, c.st)
		if ok != c.known || (ok && p != c.p) {
			t.Errorf("case %d %s: p=%v known=%v, want %v %v", i, c.id, p, ok, c.p, c.known)
		}
	}
}

// fixedOracle answers every ballot the same way.
type fixedOracle struct {
	mu  sync.Mutex
	ans map[string]decide.Answer
	n   int
}

func (f *fixedOracle) Ask(context.Context, string, []decide.Question) (map[string]decide.Answer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	return f.ans, nil
}

// turn plays one request through the session as the loop would.
func turn(s *Session, interrupt func()) {
	s.Request(0, interrupt)
	s.Observe(loop.Event{Kind: loop.EventAssistant, Text: "working"})
	s.Observe(loop.Event{Kind: loop.EventToolResult, Call: loop.ToolCall{Name: "read_file", Input: map[string]any{"path": "a"}}, Text: "x"})
	s.Observe(loop.Event{Kind: loop.EventTurnEnd})
}

func watch(mode string, ans map[string]decide.Answer) (*Session, *fixedOracle, *[]string) {
	o := &fixedOracle{ans: ans}
	var logged []string
	var mu sync.Mutex
	s := New(Options{Watchdog: mode, Oracle: o, Goal: "fix the build", Logf: func(l string) {
		mu.Lock()
		logged = append(logged, l)
		mu.Unlock()
	}})
	return s, o, &logged
}

func TestWatchdogBallotsEveryThreeRequests(t *testing.T) {
	s, o, _ := watch("on", nil)
	for i := 0; i < 7; i++ {
		turn(s, nil)
		s.Wait()
	}
	if o.n != 2 {
		t.Errorf("ballots = %d over 7 requests, want 2 (after 3 and 6)", o.n)
	}
}

func TestWatchdogConcernJoinsAtTheNextToolBoundary(t *testing.T) {
	s, _, _ := watch("on", map[string]decide.Answer{QLooping: {P: 0.9}})
	for i := 0; i < 3; i++ {
		turn(s, nil)
	}
	s.Wait()
	got := s.Reminders(loop.BoundaryTools)
	if len(got) != 1 || got[0].Source != "watchdog" || !strings.Contains(got[0].Text, "repeating the same steps") {
		t.Fatalf("reminders = %+v", got)
	}
}

func TestWatchdogBlockerInterruptsTheRequestInFlight(t *testing.T) {
	s, _, _ := watch("on", map[string]decide.Answer{QUnsafe: {P: 0.95}})
	turn(s, nil)
	turn(s, nil)
	interrupted := make(chan struct{}, 1)
	s.Request(0, func() { interrupted <- struct{}{} })
	s.Observe(loop.Event{Kind: loop.EventAssistant, Text: "x"})
	s.Observe(loop.Event{Kind: loop.EventTurnEnd}) // the third request ends: a ballot starts
	s.Request(0, func() { interrupted <- struct{}{} })
	s.Wait()
	select {
	case <-interrupted:
	default:
		t.Fatal("a blocker did not interrupt the request in flight")
	}
	if got := s.Reminders(loop.BoundaryRetry); len(got) != 1 || !strings.Contains(got[0].Text, "destructive") {
		t.Errorf("retry reminders = %+v", got)
	}
}

func TestWatchdogRateLimitsSteers(t *testing.T) {
	s, _, logged := watch("on", map[string]decide.Answer{QLooping: {P: 0.9}})
	s.o.Every = 1
	turn(s, nil)
	s.Wait()
	turn(s, nil)
	s.Wait()
	if got := s.Reminders(loop.BoundaryTools); len(got) != 1 {
		t.Errorf("reminders = %d, want 1 (one steer per 3 requests)", len(got))
	}
	if !strings.Contains(strings.Join(*logged, "\n"), "rate-limited") {
		t.Errorf("logged = %v", *logged)
	}
}

func TestWatchdogNitsRideOnTheNextReminder(t *testing.T) {
	s, _, _ := watch("on", map[string]decide.Answer{QPersonaBreak: {P: 0.9}})
	for i := 0; i < 3; i++ {
		turn(s, nil)
	}
	s.Wait()
	if got := s.Reminders(loop.BoundaryTools); len(got) != 0 {
		t.Fatalf("a nit alone must wait: %+v", got)
	}
	got := s.Reminders(loop.BoundaryRun)
	if len(got) != 1 || !strings.Contains(got[0].Text, "keep it out of files") {
		t.Errorf("run-start reminders = %+v", got)
	}
}

func TestWatchdogShadowNeverSteers(t *testing.T) {
	s, _, logged := watch("shadow", map[string]decide.Answer{QUnsafe: {P: 0.95}})
	called := false
	for i := 0; i < 3; i++ {
		turn(s, func() { called = true })
	}
	s.Wait()
	if called || len(s.Reminders(loop.BoundaryRun)) != 0 {
		t.Error("shadow steered")
	}
	if !strings.Contains(strings.Join(*logged, "\n"), "would steer") {
		t.Errorf("logged = %v", *logged)
	}
	if _, acting := s.Ballot(context.Background()); acting {
		t.Error("a shadow ballot must not act")
	}
}

func TestBallotNowForCompletion(t *testing.T) {
	s, _, _ := watch("on", map[string]decide.Answer{QUnverified: {P: 0.85}})
	v, acting := s.Ballot(context.Background())
	if !acting || !v.Has(QUnverified) {
		t.Errorf("verdict = %+v acting=%v", v, acting)
	}
	var none *Session
	if _, acting := none.Ballot(context.Background()); acting {
		t.Error("a nil session never acts")
	}
}

// blockingOracle answers only when its context ends or release closes.
type blockingOracle struct {
	started chan struct{}
	release chan struct{}
	ended   chan error
}

func (b *blockingOracle) Ask(ctx context.Context, _ string, _ []decide.Question) (map[string]decide.Answer, error) {
	b.started <- struct{}{}
	select {
	case <-ctx.Done():
		b.ended <- ctx.Err()
		return nil, ctx.Err()
	case <-b.release:
		b.ended <- nil
		return map[string]decide.Answer{QUnsafe: {P: 0.95}}, nil
	}
}

// A ballot in flight never delays a request, and closing the session (the
// run ended) cancels it; its late verdict is dropped.
func TestWatchdogBallotNeverBlocksAndIsCancelledWithTheSession(t *testing.T) {
	o := &blockingOracle{started: make(chan struct{}, 1), release: make(chan struct{}), ended: make(chan error, 1)}
	s := New(Options{Watchdog: "on", Oracle: o, Every: 1})
	done := make(chan struct{})
	go func() {
		turn(s, nil) // starts the ballot
		turn(s, nil) // the next request goes ahead while it runs
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a running ballot blocked the loop")
	}
	<-o.started
	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not cancel the ballot")
	}
	if err := <-o.ended; err == nil {
		t.Error("the ballot's context was not cancelled")
	}
	if got := s.Reminders(loop.BoundaryRun); len(got) != 0 {
		t.Errorf("a cancelled ballot steered: %+v", got)
	}
	turn(s, nil) // no ballot starts after Close
	select {
	case <-o.started:
		t.Error("a ballot started after Close")
	default:
	}
	var none *Session
	none.Close()
}

// The parent context (the run's) cancels a ballot too.
func TestWatchdogBallotFollowsTheRunContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	o := &blockingOracle{started: make(chan struct{}, 1), release: make(chan struct{}), ended: make(chan error, 1)}
	s := New(Options{Context: ctx, Watchdog: "on", Oracle: o, Every: 1})
	turn(s, nil)
	<-o.started
	cancel()
	if err := <-o.ended; err == nil {
		t.Error("cancelling the run did not cancel the ballot")
	}
	s.Wait()
}

// The ballot sees each call's arguments as valid JSON, long values cut,
// so its heuristics can still read a long write.
func TestBallotStateKeepsArgumentsParseable(t *testing.T) {
	o := &fixedOracle{}
	s := New(Options{Watchdog: "on", Oracle: o, Every: 1})
	s.Request(0, nil)
	s.Observe(loop.Event{Kind: loop.EventAssistant, Text: "writing"})
	long := strings.Repeat("x", 5000) + " darling"
	s.Observe(loop.Event{Kind: loop.EventToolResult, Call: loop.ToolCall{Name: "write_file", Input: map[string]any{"path": "a.go", "content": long}}})
	st := decide.ParseState(s.stateLocked().String())
	if argField(st.Turns[0].Calls[0].Args, "path") != "a.go" {
		t.Errorf("args = %q", st.Turns[0].Calls[0].Args)
	}
}
