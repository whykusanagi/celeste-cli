package compact

import (
	"context"
	"errors"
	"testing"
)

func TestMinSavingsScalesWithTheWindow(t *testing.T) {
	cases := map[int]int{40_000: 4_000, 64_000: 6_400, 200_000: 20_000, 1_000_000: 20_000}
	for window, want := range cases {
		if got := minSavings(window); got != want {
			t.Errorf("minSavings(%d) = %d, want %d", window, got, want)
		}
	}
}

func TestKeepForScalesWithTheWindow(t *testing.T) {
	cases := map[int]int{0: 20_000, 40_000: 10_000, 80_000: 20_000, 1_000_000: 20_000}
	for window, want := range cases {
		if got := KeepFor(window); got != want {
			t.Errorf("KeepFor(%d) = %d, want %d", window, got, want)
		}
	}
}

// #234: on a 40k window the protected tail is 10k, so a prune that cannot
// reach the target saves less than the old fixed 20k and was skipped.
func TestPlanPrunesOnSmallWindows(t *testing.T) {
	msgs := history(
		step{"read_file", `{"path":"a.go"}`, 8_000},
		step{"read_file", `{"path":"b.go"}`, 8_000},
		step{"read_file", `{"path":"c.go"}`, 8_000},
		step{"read_file", `{"path":"d.go"}`, 8_000},
		step{"read_file", `{"path":"e.go"}`, 14_000},
		step{"read_file", `{"path":"f.go"}`, 14_000},
		step{"read_file", `{"path":"g.go"}`, 14_000},
	)
	res := Plan(msgs, Options{Window: 40_000, Used: 40_000})
	if !res.Pruned() {
		t.Fatal("a 40k window over its threshold should prune what it can")
	}
	if res.SavedTokens < minSavings(40_000) {
		t.Errorf("saved %d, want at least %d", res.SavedTokens, minSavings(40_000))
	}
}

// #234 caveat 3: at a 40k window, /compact after a prune found nothing to
// summarize because the summary kept the newest 20k.
func TestSummarizeKeepsLessOnSmallWindows(t *testing.T) {
	// ~16k tokens: inside the 20k default tail, over a 40k window's 10k.
	var steps []step
	for i := 0; i < 8; i++ {
		steps = append(steps, step{"read_file", `{"path":"f.go"}`, 8_000})
	}
	msgs := history(steps...)
	f := &fakeSummarizer{reply: "## Goal\ndo the task"}
	if _, _, err := Summarize(context.Background(), msgs, SummaryOptions{}, f.fn); !errors.Is(err, ErrNothingToSummarize) {
		t.Fatalf("test setup: with the 20k default the history should be too small, got %v", err)
	}
	out, res, err := Summarize(context.Background(), msgs, SummaryOptions{Window: 40_000}, f.fn)
	if err != nil {
		t.Fatalf("with a 40k window the summary should keep ~10k and shrink the rest: %v", err)
	}
	if !IsSummary(out[0]) || res.TokensAfter >= res.TokensBefore {
		t.Fatalf("summary did not shrink the history: %+v", res)
	}
}
