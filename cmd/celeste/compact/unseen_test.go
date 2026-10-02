package compact

import (
	"fmt"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// batch is one assistant turn calling n tools in parallel and their results.
func batch(prefix string, n, size int) []tui.ChatMessage {
	asst := tui.ChatMessage{Role: "assistant"}
	var results []tui.ChatMessage
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("%s_%d", prefix, i)
		asst.ToolCalls = append(asst.ToolCalls, tui.ToolCallInfo{ID: id, Name: "read_file", Arguments: fmt.Sprintf(`{"path":"%s%d.go"}`, prefix, i)})
		results = append(results, tui.ChatMessage{Role: "tool", ToolCallID: id, Name: "read_file", Content: strings.Repeat("x", size)})
	}
	return append([]tui.ChatMessage{asst}, results...)
}

// #234 thrash: 22 results larger than the protected tail were elided before
// the model saw them, then recalled, then elided again.
func TestPlanNeverElidesUnseenResults(t *testing.T) {
	msgs := []tui.ChatMessage{{Role: "user", Content: "summarize every file"}}
	msgs = append(msgs, batch("old", 6, 8_000)...) // seen: 12k tokens
	fresh := batch("new", 22, 6_000)               // unseen: 33k tokens
	msgs = append(msgs, fresh...)
	res := Plan(msgs, Options{Window: 40_000, Used: 50_000, Unseen: len(fresh)})
	for _, e := range res.Edits {
		if strings.HasPrefix(e.ToolCallID, "new_") {
			t.Fatalf("elided %s, which the model has not seen yet", e.ToolCallID)
		}
	}
	if !res.Pruned() {
		t.Fatal("the seen results should still be pruned")
	}
	// An overflow (Force) may cut anything: the request failed anyway.
	forced := Plan(msgs, Options{Window: 40_000, Used: 50_000, Unseen: len(fresh), Force: true})
	elidedNew := false
	for _, e := range forced.Edits {
		elidedNew = elidedNew || strings.HasPrefix(e.ToolCallID, "new_")
	}
	if !elidedNew {
		t.Error("a forced prune should be allowed to elide unseen results")
	}
}

func TestPlanKeepsRecallResultsOnAProactivePrune(t *testing.T) {
	msgs := history(
		step{"recall_tool_result", `{"id":"call_x"}`, 40_000},
		step{"read_file", `{"path":"a.go"}`, 40_000},
		step{"read_file", `{"path":"b.go"}`, 40_000},
		step{"read_file", `{"path":"c.go"}`, 40_000},
	)
	res := Plan(msgs, Options{Window: 40_000, Used: 60_000})
	for _, e := range res.Edits {
		if e.Name == "recall_tool_result" {
			t.Fatal("a proactive prune elided a recalled body")
		}
	}
	forced := Plan(msgs, Options{Window: 40_000, Used: 60_000, Force: true})
	if results := toolResults(Apply(msgs, forced.Edits)); !strings.Contains(results["call_0"], "recall_tool_result with id") {
		t.Error("a forced prune may elide a recalled body")
	}
}

// #234 caveat 1: fugu sent {"path":…,"start_line":0,"end_line":0} and later
// {"path":…}; a re-read must supersede the first read either way.
// The first read is 25k tokens so it sits outside a forced prune's 20k
// protected tail.
func TestSupersessionIgnoresReadFileDefaults(t *testing.T) {
	for _, first := range []string{
		`{"path":"a.go","start_line":0,"end_line":0}`,
		`{"path":"a.go","start_line":1}`,
		`{"end_line":0,"path":"a.go"}`,
	} {
		msgs := history(step{"read_file", first, 100_000}, step{"read_file", `{"path":"a.go"}`, 400})
		res := Plan(msgs, Options{Window: 1_000_000, Force: true})
		if res.Superseded != 1 {
			t.Errorf("%s then a plain re-read: superseded = %d, want 1", first, res.Superseded)
		}
	}
	// A different range is a different read.
	msgs := history(step{"read_file", `{"path":"a.go","start_line":200}`, 100_000}, step{"read_file", `{"path":"a.go"}`, 400})
	if res := Plan(msgs, Options{Window: 1_000_000, Force: true}); res.Superseded != 0 {
		t.Error("a read of another range must not be superseded")
	}
}
