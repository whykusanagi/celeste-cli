package compact

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// The fixed prefix a chat request carried in the L2 smoke run: the spine
// persona (~7.3k) and 49 tool schemas (~8.7k).
const smokeOverhead = 7_300 + 8_700

// Large windows, and any window with no fixed prefix, keep the threshold
// they always had.
func TestThresholdForKeepsTheOldThresholdWhenTheOverheadFits(t *testing.T) {
	for _, c := range []struct{ window, overhead int }{
		{200_000, smokeOverhead}, {200_000, 0}, {1_000_000, smokeOverhead}, {128_000, smokeOverhead},
		{32_768, 0}, {8_192, 0}, {40_000, 0},
	} {
		if got, want := ThresholdFor(c.window, c.overhead), Threshold(c.window); got != want {
			t.Errorf("ThresholdFor(%d, %d) = %d, want the old %d", c.window, c.overhead, got, want)
		}
	}
	if got, want := KeepWithin(200_000, smokeOverhead), KeepFor(200_000); got != want {
		t.Errorf("KeepWithin(200k) = %d, want the old %d", got, want)
	}
}

// L2: at 32k the old threshold (16,384) sat below the ~16k fixed prefix,
// so every request was "over" with no history at all. The overhead-aware
// threshold leaves the history most of what the prefix and the reply
// reserve leave over.
func TestThresholdForLeavesHistoryRoomOnACrowdedWindow(t *testing.T) {
	for _, c := range []struct{ window, overhead int }{{32_768, smokeOverhead}, {8_192, 2_500}} {
		th := ThresholdFor(c.window, c.overhead)
		budget := HistoryBudget(c.window, c.overhead)
		if budget <= 0 {
			t.Fatalf("HistoryBudget(%d, %d) = %d, want room for history", c.window, c.overhead, budget)
		}
		if room := th - c.overhead; room < budget/2 {
			t.Errorf("window %d: threshold %d leaves the history %d, want at least half the %d budget", c.window, th, room, budget)
		}
		if th > c.window-replyReserve(c.window) {
			t.Errorf("window %d: threshold %d eats the reply reserve", c.window, th)
		}
		if keep := KeepWithin(c.window, c.overhead); keep <= 0 || keep >= th-c.overhead {
			t.Errorf("window %d: kept tail %d must be under the history room %d", c.window, keep, th-c.overhead)
		}
	}
}

// There is nothing to summarize when the history is the kept tail, or the
// only thing before it is the previous summary.
func TestNeedsSummaryOnlyWhenSomethingIsSummarizable(t *testing.T) {
	const window = 32_768
	small := []tui.ChatMessage{msg("user", "hi"), msg("assistant", "hello")}
	if NeedsSummary(small, window, Estimate(small)+smokeOverhead) {
		t.Fatal("a two-message chat under a 16k prefix needs no summary")
	}
	// Over the threshold by usage, but all of it inside the kept tail.
	tail := []tui.ChatMessage{msg("user", strings.Repeat("a", 4*3_000))}
	if NeedsSummary(tail, window, window) {
		t.Fatal("a history that is all kept tail has nothing to summarize")
	}
	// A previous summary followed by the kept tail: summarizing would only
	// re-summarize the summary.
	prev := append(SummaryMessages(strings.Repeat("s", 4*2_500), "", true), tail...)
	if NeedsSummary(prev, window, Estimate(prev)+smokeOverhead) {
		t.Fatal("only the previous summary sits before the tail")
	}
	// Enough new history before the tail.
	var long []tui.ChatMessage
	for i := 0; i < 12; i++ {
		long = append(long, msg("user", strings.Repeat("q", 4*400)), msg("assistant", strings.Repeat("r", 4*400)))
	}
	if !NeedsSummary(long, window, Estimate(long)+smokeOverhead) {
		t.Fatal("~9.6k of history over a 16k prefix at 32k should summarize")
	}
	// A prefix that fills the window leaves no budget: a summary cannot help.
	if NeedsSummary(long, window, Estimate(long)+window) {
		t.Fatal("no history budget: nothing a summary can fix")
	}
}

// simulate runs turns of a chat through a meter the way the chat's
// compactor does: the provider counts the prefix plus the history, a
// summary is attempted whenever NeedsSummary says so, and a summary that
// comes back ErrNothingToSummarize is the "Summary skipped" line. It
// returns how many summaries were applied and how many were skipped.
func simulate(t *testing.T, window, overhead, turns, turnTokens int) (applied, skipped int, maxUsed int) {
	t.Helper()
	m := NewMeter(overhead)
	var history []tui.ChatMessage
	sum := func(context.Context, string, string) (string, error) {
		return "## Goal\nkeep chatting\n## Next step\ncontinue", nil
	}
	prompt := 0
	for i := 0; i < turns; i++ {
		history = append(history, msg("user", fmt.Sprintf("turn %d ", i)+strings.Repeat("u", 4*turnTokens/2)))
		m.Observe(history, prompt)
		used := m.Used(history)
		if NeedsSummary(history, window, used) {
			out, _, err := Summarize(context.Background(), history, SummaryOptions{Window: window, Overhead: used - Estimate(history)}, sum)
			switch {
			case errors.Is(err, ErrNothingToSummarize):
				skipped++
			case err != nil:
				t.Fatal(err)
			default:
				applied++
				history = out
			}
		}
		m.Sending(history)
		prompt = Estimate(history) + overhead // what the provider reports
		maxUsed = max(maxUsed, prompt)
		history = append(history, msg("assistant", strings.Repeat("a", 4*turnTokens/2)))
	}
	return applied, skipped, maxUsed
}

// L2: at 32k with the smoke run's prefix, a chat must not end every turn
// with "Summarizing older context…" then "Summary skipped", and the
// requests must stay inside the window.
func TestNoSummaryLoopAt32k(t *testing.T) {
	const window = 32_768
	applied, skipped, maxUsed := simulate(t, window, smokeOverhead, 40, 600)
	if skipped != 0 {
		t.Fatalf("%d summaries skipped in 40 turns: the summary loop is back", skipped)
	}
	if applied == 0 || applied > 10 {
		t.Fatalf("applied %d summaries in 40 turns of ~600 tokens, want a few", applied)
	}
	if maxUsed > window-replyReserve(window) {
		t.Fatalf("a request reached %d tokens: no room for the reply in %d", maxUsed, window)
	}
	// The first turns carry almost no history: no summary at all.
	if a, s, _ := simulate(t, window, smokeOverhead, 4, 600); a+s != 0 {
		t.Fatalf("summarized in the first 4 turns (%d applied, %d skipped)", a, s)
	}
	// The smoke run: short exchanges. ~4k of history in 40 turns fits.
	if a, s, _ := simulate(t, window, smokeOverhead, 40, 100); a+s != 0 {
		t.Fatalf("40 short turns summarized (%d applied, %d skipped)", a, s)
	}
}

func TestNoSummaryLoopAt8k(t *testing.T) {
	const window = 8_192
	applied, skipped, maxUsed := simulate(t, window, 2_500, 40, 300)
	if skipped != 0 || applied == 0 {
		t.Fatalf("8k: applied %d, skipped %d", applied, skipped)
	}
	if maxUsed > window-replyReserve(window) {
		t.Fatalf("a request reached %d tokens in an 8k window", maxUsed)
	}
}

// At 200k the overhead-aware rules decide exactly as before.
func TestSummaryAt200kIsUnchanged(t *testing.T) {
	const window = 200_000
	var history []tui.ChatMessage
	for i := 0; i < 400; i++ {
		history = append(history, msg("user", strings.Repeat("u", 4*300)), msg("assistant", strings.Repeat("a", 4*300)))
		used := Estimate(history) + smokeOverhead
		old := used > Threshold(window) && CutIndex(history, KeepFor(window)) > 0
		if got := NeedsSummary(history, window, used); got != old {
			t.Fatalf("turn %d (used %d): NeedsSummary = %v, the old rule said %v", i, used, got, old)
		}
	}
}

// Plan aims at ThresholdFor: on a window the prefix crowds, the target
// leaves the history its room instead of pruning toward a threshold the
// prefix alone nearly fills.
func TestPlanAimsAtTheOverheadAwareThreshold(t *testing.T) {
	const window, overhead = 40_000, 12_000
	var steps []step
	for i := 0; i < 20; i++ {
		steps = append(steps, step{"read_file", fmt.Sprintf(`{"path":"f%02d.go"}`, i), 6_000})
	}
	msgs := history(steps...)
	used := Estimate(msgs) + overhead
	blind := Plan(msgs, Options{Window: window, Used: used})
	aware := Plan(msgs, Options{Window: window, Used: used, Overhead: overhead})
	if !blind.Pruned() || !aware.Pruned() {
		t.Fatalf("both should prune %d tokens on a 40k window (blind %d, aware %d)", used, blind.Elided, aware.Elided)
	}
	if aware.Elided >= blind.Elided {
		t.Fatalf("elided %d with the prefix known, %d without: want fewer", aware.Elided, blind.Elided)
	}
	// Under the overhead-aware threshold nothing is pruned.
	small := history(steps[:3]...)
	if res := Plan(small, Options{Window: 32_768, Used: Estimate(small) + smokeOverhead, Overhead: smokeOverhead}); res.Pruned() {
		t.Fatalf("pruned %s under the overhead-aware threshold", res.Summary())
	}
}

// The urgent floor: any history before the kept tail besides a previous
// summary counts; the tail alone, or a summary alone, does not.
func TestHasHistoryToSummarize(t *testing.T) {
	const window = 32_768
	tail := []tui.ChatMessage{msg("user", strings.Repeat("a", 4*12_000))}
	if HasHistoryToSummarize(tail, window, smokeOverhead) {
		t.Fatal("one huge message is all kept tail")
	}
	prev := append(SummaryMessages("the summary", "", true), tail...)
	if HasHistoryToSummarize(prev, window, smokeOverhead) {
		t.Fatal("only the previous summary sits before the tail")
	}
	head := append([]tui.ChatMessage{msg("user", "start"), msg("assistant", "ok")}, tail...)
	if !HasHistoryToSummarize(head, window, smokeOverhead) {
		t.Fatal("two messages before the tail can be summarized when urgent")
	}
}

// A prefix that fills a tiny window (16k under the smoke run's 16k prefix)
// leaves no history budget. No proactive summary runs: the next request
// overflows and the forced compaction after the overflow recovers. A
// summary that does run (the urgent rung, a manual /compact) keeps only
// the newest message. Pinned so a change to either is deliberate.
func TestNoHistoryBudgetFailsOpenToOverflowRecovery(t *testing.T) {
	const window = 16_384
	if b := HistoryBudget(window, smokeOverhead); b != 0 {
		t.Fatalf("HistoryBudget(16k, 16k prefix) = %d, want 0", b)
	}
	var long []tui.ChatMessage
	for i := 0; i < 12; i++ {
		long = append(long, msg("user", strings.Repeat("q", 4*400)), msg("assistant", strings.Repeat("r", 4*400)))
	}
	if NeedsSummary(long, window, Estimate(long)+smokeOverhead) {
		t.Fatal("no history budget: no proactive summary, overflow recovery handles it")
	}
	if k := KeepWithin(window, smokeOverhead); k != 1 {
		t.Fatalf("KeepWithin(16k, 16k prefix) = %d, want 1 (the newest message)", k)
	}
	sum := func(context.Context, string, string) (string, error) { return "## Goal\nx", nil }
	out, _, err := Summarize(context.Background(), long, SummaryOptions{Window: window, Overhead: smokeOverhead}, sum)
	if err != nil {
		t.Fatal(err)
	}
	if last := out[len(out)-1]; last.Content != long[len(long)-1].Content {
		t.Fatal("a forced summary must keep the newest message")
	}
	if Estimate(out) >= Estimate(long) {
		t.Fatal("a forced summary must shrink the history")
	}
}
