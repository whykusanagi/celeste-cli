package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/decide"
)

// Whole-word matching (2.0 W3): "rust" is not in "trust", "api" not in
// "capital"; plurals, verb endings and file extensions still match.
func TestClassifyHeuristicWholeWords(t *testing.T) {
	for goal, want := range map[string]TaskLane{
		"explain why customers trust us":       LaneContent,
		"summarize the capital markets report": LaneContent,
		"the tests are failing in ci":          LaneCode,
		"fixed the parser? check it":           LaneReview,
		"look at auth_test.go":                 LaneCode,
		"rendering the intro video":            LaneMedia,
		// Substring matching put these in code ("rust", "api").
		"we trust the capital plan": LaneUnknown,
	} {
		if got, _ := ClassifyHeuristic(goal); got != want {
			t.Errorf("%q = %s, want %s", goal, got, want)
		}
	}
}

type routeJev struct {
	lane string
	err  error
}

func (r routeJev) Ask(context.Context, string, []decide.Question) (map[string]decide.Answer, error) {
	if r.err != nil {
		return nil, r.err
	}
	return map[string]decide.Answer{"lane": {Choice: r.lane, Confidence: 0.8, Probs: map[string]float64{r.lane: 0.9}, Source: "jev"}}, nil
}

func withRoute(t *testing.T, o decide.Oracle) {
	t.Helper()
	old := routeOracle
	routeOracle = func(func(string)) decide.Oracle { return decide.Guarded(o, "route", nil) }
	t.Cleanup(func() { routeOracle = old })
}

func TestClassifyJevRoute(t *testing.T) {
	goal := "fix the flaky test" // heuristic: code
	withRoute(t, routeJev{lane: "review"})
	if lane, _, note := Classify(context.Background(), goal, "", nil); lane != LaneCode || note != "" {
		t.Errorf("off: %s %q", lane, note)
	}
	if lane, _, note := Classify(context.Background(), goal, "shadow", nil); lane != LaneCode || !strings.Contains(note, "would pick review") {
		t.Errorf("shadow: %s %q", lane, note)
	}
	if lane, conf, _ := Classify(context.Background(), goal, "on", nil); lane != LaneReview || conf != 0.8 {
		t.Errorf("on: %s %v", lane, conf)
	}
	withRoute(t, routeJev{err: errors.New("jev: HTTP 529")})
	if lane, _, note := Classify(context.Background(), goal, "on", nil); lane != LaneCode || note != "" {
		t.Errorf("on, Jev down: %s %q (want the heuristic)", lane, note)
	}
}
