package compact

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// history builds a conversation from tool calls: each step is an assistant
// message calling one tool and its result of size bytes.
type step struct {
	name, args string
	size       int
}

func history(steps ...step) []tui.ChatMessage {
	msgs := []tui.ChatMessage{{Role: "user", Content: "do the task"}}
	for i, s := range steps {
		id := fmt.Sprintf("call_%d", i)
		msgs = append(msgs,
			tui.ChatMessage{Role: "assistant", ToolCalls: []tui.ToolCallInfo{{ID: id, Name: s.name, Arguments: s.args}}},
			tui.ChatMessage{Role: "tool", ToolCallID: id, Name: s.name, Content: strings.Repeat("x", s.size)},
		)
	}
	return msgs
}

func toolResults(msgs []tui.ChatMessage) map[string]string {
	out := map[string]string{}
	for _, m := range msgs {
		if m.Role == "tool" {
			out[m.ToolCallID] = m.Content
		}
	}
	return out
}

func TestThreshold(t *testing.T) {
	cases := map[int]int{
		1_000_000: 900_000, // 10% reserve
		200_000:   180_000,
		100_000:   84_000, // 16k floor
		8_192:     4_096,  // never below half the window
	}
	for window, want := range cases {
		if got := Threshold(window); got != want {
			t.Errorf("Threshold(%d) = %d, want %d", window, got, want)
		}
	}
}

func TestPlanBelowThresholdDoesNothing(t *testing.T) {
	msgs := history(step{"read_file", `{"path":"a.go"}`, 4000}, step{"read_file", `{"path":"a.go"}`, 4000})
	if res := Plan(msgs, Options{Window: 200_000}); res.Pruned() {
		t.Fatalf("pruned below the threshold: %s", res.Summary())
	}
}

// Supersession needs no model call: a file read again, a file edited after
// it was read, and a read-only command run again all make the earlier
// result stale.
func TestPlanSupersession(t *testing.T) {
	big := 40_000 // bytes ≈ 10k tokens
	msgs := history(
		step{"read_file", `{"path":"a.go"}`, big},        // 0: re-read later → superseded
		step{"read_file", `{"path":"b.go"}`, big},        // 1: edited later → superseded
		step{"bash", `{"command":"go test ./..."}`, big}, // 2: re-run later → superseded
		step{"read_file", `{"path":"c.go"}`, big},        // 3: untouched
		step{"patch_file", `{"path":"./b.go","old_string":"x"}`, 100},
		step{"read_file", `{"path":"a.go"}`, 400},
		step{"bash", `{"command":"go test ./..."}`, 400},
	)
	// Force: we only want to see which results supersession picks.
	res := Plan(msgs, Options{Window: 1_000_000, Force: true})
	superseded := map[string]bool{}
	for _, e := range res.Edits {
		if e.Superseded {
			superseded[e.ToolCallID] = true
		}
	}
	for _, id := range []string{"call_0", "call_1", "call_2"} {
		if !superseded[id] {
			t.Errorf("%s should be superseded; got %v", id, superseded)
		}
	}
	if superseded["call_3"] {
		t.Error("call_3 (c.go, never touched again) must not be marked superseded")
	}
}

// Over the threshold, the oldest large results are elided until usage is
// back under target, the newest ~40k tokens are protected, and every tool
// call keeps a result.
func TestPlanElidesOldestAndProtectsTail(t *testing.T) {
	var steps []step
	for i := 0; i < 30; i++ {
		steps = append(steps, step{"read_file", fmt.Sprintf(`{"path":"f%d.go"}`, i), 40_000}) // ~10k tokens each
	}
	msgs := history(steps...)
	window := 200_000
	used := Estimate(msgs) // ~300k: well over
	res := Plan(msgs, Options{Window: window, Used: used})
	if !res.Pruned() {
		t.Fatal("expected a prune over the threshold")
	}
	out := Apply(msgs, res.Edits)
	if after := Estimate(out); after > Threshold(window) {
		t.Errorf("still over threshold after prune: %d > %d", after, Threshold(window))
	}
	// Oldest first: call_0 elided, and the newest result untouched.
	results := toolResults(out)
	if !strings.Contains(results["call_0"], "recall_tool_result") {
		t.Error("oldest result was not elided")
	}
	if strings.Contains(results["call_29"], "recall_tool_result") {
		t.Error("newest result is in the protected tail and must not be elided")
	}
	if len(results) != 30 {
		t.Errorf("every tool call must keep a result: got %d of 30", len(results))
	}
	if len(toolResults(msgs)["call_0"]) != 40_000 {
		t.Error("Apply mutated the input history")
	}
}

// A small overshoot isn't worth breaking the prompt cache for, unless forced.
func TestPlanMinimumSaving(t *testing.T) {
	msgs := history(step{"read_file", `{"path":"a.go"}`, 2_000}, step{"read_file", `{"path":"b.go"}`, 200_000})
	window := 64_000
	used := Threshold(window) + 1_000
	if res := Plan(msgs, Options{Window: window, Used: used}); res.SavedTokens > 0 && res.SavedTokens < 500 {
		t.Errorf("pruned for a trivial saving: %s", res.Summary())
	}
	if res := Plan(msgs, Options{Window: 1_000_000, Force: true}); !res.Pruned() {
		t.Error("Force should prune even below the threshold")
	}
}

func TestPruneSpillsAndRecalls(t *testing.T) {
	store := &Store{Dir: t.TempDir()}
	msgs := history(step{"read_file", `{"path":"a.go"}`, 40_000}, step{"read_file", `{"path":"b.go"}`, 100})
	original := toolResults(msgs)["call_0"]

	out, res := Prune(msgs, Options{Window: 40_000, Force: true}, store)
	if !res.Pruned() {
		t.Fatal("expected a prune")
	}
	if toolResults(out)["call_0"] == original {
		t.Fatal("result was not replaced")
	}
	got, err := store.Load("call_0")
	if err != nil || got != original {
		t.Fatalf("spilled body not recoverable: err=%v, len=%d", err, len(got))
	}
	info, err := os.Stat(filepath.Join(store.Dir, "call_0.txt"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("spill file should be private (0600): %v %v", err, info.Mode())
	}

	// Pruning again doesn't re-prune placeholders.
	_, again := Prune(out, Options{Window: 40_000, Force: true}, store)
	for _, e := range again.Edits {
		if e.ToolCallID == "call_0" {
			t.Error("a placeholder was pruned again")
		}
	}

	if _, res := Prune(msgs, Options{Window: 40_000, Force: true}, nil); res.Pruned() {
		t.Error("with no store nothing may be pruned")
	}
}

func TestStoreRejectsBadIDs(t *testing.T) {
	store := &Store{Dir: t.TempDir()}
	for _, id := range []string{"../escape", "a/b", "", "..", strings.Repeat("a", 200)} {
		if err := store.Save(id, "x"); err == nil {
			t.Errorf("Save(%q) should fail", id)
		}
		if _, err := store.Load(id); err == nil {
			t.Errorf("Load(%q) should fail", id)
		}
	}
}

// A scorer reorders elision: least-needed first, so a result the scorer
// rates as needed survives even when it is the oldest (#175). A scorer that
// returns nothing (error, timeout, shadow mode) leaves today's oldest-first order.
func TestPlanScorerOrdersElision(t *testing.T) {
	var steps []step
	for i := 0; i < 30; i++ {
		steps = append(steps, step{"read_file", fmt.Sprintf(`{"path":"f%d.go"}`, i), 40_000})
	}
	msgs := history(steps...)
	window := 200_000
	used := Estimate(msgs)

	var seen []Candidate
	keepOldest := func(c []Candidate) map[string]float64 {
		seen = c
		scores := map[string]float64{}
		for _, x := range c {
			scores[x.ToolCallID] = 0.1
		}
		scores["call_0"] = 0.9
		return scores
	}
	res := Plan(msgs, Options{Window: window, Used: used, Score: keepOldest})
	out := Apply(msgs, res.Edits)
	if strings.Contains(toolResults(out)["call_0"], "recall_tool_result") {
		t.Error("the scorer rated call_0 as needed, but it was elided")
	}
	if after := Estimate(out); after > Threshold(window) {
		t.Errorf("still over threshold after a scored prune: %d > %d", after, Threshold(window))
	}
	if len(seen) == 0 || seen[0].Name != "read_file" || seen[0].Args["path"] != "f0.go" || seen[0].Content == "" {
		t.Errorf("scorer got incomplete candidates: %+v", seen)
	}

	res = Plan(msgs, Options{Window: window, Used: used, Score: func([]Candidate) map[string]float64 { return nil }})
	if !strings.Contains(toolResults(Apply(msgs, res.Edits))["call_0"], "recall_tool_result") {
		t.Error("a scorer returning nil must fall back to oldest-first")
	}
}

// When supersession alone meets the target, there is nothing to order, so
// the scorer (a third-party call) must not run.
func TestPlanSkipsScorerWhenSupersessionSuffices(t *testing.T) {
	var steps []step
	for i := 0; i < 10; i++ {
		steps = append(steps, step{"read_file", fmt.Sprintf(`{"path":"u%d.go"}`, i), 40_000}) // unique: elision candidates
	}
	for i := 0; i < 20; i++ {
		steps = append(steps, step{"read_file", `{"path":"same.go"}`, 40_000}) // superseded but the last
	}
	msgs := history(steps...)
	called := false
	res := Plan(msgs, Options{Window: 200_000, Used: Estimate(msgs), Score: func([]Candidate) map[string]float64 {
		called = true
		return nil
	}})
	if res.Superseded == 0 || res.Elided != 0 {
		t.Fatalf("setup: want a supersession-only prune, got %+v", res)
	}
	if called {
		t.Error("scorer was called although supersession already met the target")
	}
}
