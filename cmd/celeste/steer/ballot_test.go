package steer

import (
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/decide"
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
