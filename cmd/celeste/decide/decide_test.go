package decide

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeOracle struct {
	ans   map[string]Answer
	err   error
	delay time.Duration
}

func (f fakeOracle) Ask(ctx context.Context, _ string, _ []Question) (map[string]Answer, error) {
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return f.ans, f.err
}

func yes(p float64) func(State) (Answer, bool) {
	return func(State) (Answer, bool) { return Answer{P: p}, true }
}

var twoQs = []Question{
	{ID: "a", Kind: YesNo, Text: "a?", Heuristic: yes(0.1)},
	{ID: "b", Kind: YesNo, Text: "b?", Heuristic: yes(0.2)},
	{ID: "c", Kind: YesNo, Text: "c? (no heuristic)"},
}

func TestHeuristicAnswersOnlyQuestionsWithOne(t *testing.T) {
	got, err := Heuristic{}.Ask(context.Background(), State{Goal: "g"}.String(), twoQs)
	if err != nil || len(got) != 2 || got["a"].P != 0.1 || got["a"].Source != "heuristic" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, ok := got["c"]; ok {
		t.Error("a question without a heuristic must stay unanswered")
	}
}

func TestParseStateTakesPlainText(t *testing.T) {
	if st := ParseState("fix the rust build"); st.Text != "fix the rust build" {
		t.Errorf("plain text = %+v", st)
	}
	if st := ParseState(State{Goal: "g", Turns: []TurnView{{Assistant: "hi"}}}.String()); st.Goal != "g" || st.Turns[0].Assistant != "hi" {
		t.Errorf("round trip = %+v", st)
	}
}

func TestGuardedFillsGapsFromHeuristics(t *testing.T) {
	ResetStats()
	o := Guarded(fakeOracle{ans: map[string]Answer{"a": {P: 0.9, Source: "jev"}}}, "ballot", nil)
	got, err := o.Ask(context.Background(), "s", twoQs)
	if err != nil {
		t.Fatal(err)
	}
	if got["a"].P != 0.9 || got["a"].Source != "jev" || got["b"].Source != "heuristic" {
		t.Errorf("got %+v", got)
	}
	snap := Snapshot()
	if snap["calls"] != 1 || snap["answered"] != 1 || snap["hit_rate"] != 1.0 {
		t.Errorf("snapshot = %v", snap)
	}
}

func TestGuardedFallsBackOnErrorAndTimeout(t *testing.T) {
	ResetStats()
	var logged []string
	logf := func(s string) { logged = append(logged, s) }
	failing := Guarded(fakeOracle{err: errors.New("jev: HTTP 403")}, "gate", logf)
	got, err := failing.Ask(context.Background(), "s", twoQs)
	if err != nil || got["a"].Source != "heuristic" || len(got) != 2 {
		t.Fatalf("error fallback = %+v, %v", got, err)
	}
	if len(logged) != 1 || !strings.Contains(logged[0], "403") {
		t.Errorf("logged = %v", logged)
	}

	start := time.Now()
	slow := Guarded(fakeOracle{delay: time.Minute}, "gate", logf)
	if got, _ := slow.Ask(context.Background(), "s", twoQs); got["a"].Source != "heuristic" {
		t.Errorf("timeout fallback = %+v", got)
	}
	if el := time.Since(start); el > Timeout+time.Second {
		t.Errorf("a slow oracle held the caller %v (cap %v)", el, Timeout)
	}
	snap := Snapshot()
	if snap["calls"] != 2 || snap["fallbacks"] != 2 || snap["hit_rate"] != 0.0 {
		t.Errorf("snapshot = %v", snap)
	}
	if snap["by_use"].(map[string]any)["gate"] == nil {
		t.Errorf("by_use = %v", snap["by_use"])
	}
}

func TestGuardedWithoutPrimaryIsTheHeuristicAndUncounted(t *testing.T) {
	ResetStats()
	got, _ := Guarded(nil, "ballot", nil).Ask(context.Background(), "s", twoQs)
	if got["a"].Source != "heuristic" || Snapshot()["calls"] != 0 {
		t.Errorf("got %+v, stats %v", got, Snapshot())
	}
}
