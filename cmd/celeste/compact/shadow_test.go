package compact

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/jev"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

func TestShadowReportComparesRulesAndJev(t *testing.T) {
	cands := []Candidate{
		{ToolCallID: "a", Name: "read_file", Args: map[string]any{"path": "tokens.go"}, Tokens: 1000},
		{ToolCallID: "b", Name: "read_file", Args: map[string]any{"path": "youtube.go"}, Tokens: 1000},
		{ToolCallID: "c", Name: "web_fetch", Args: map[string]any{"url": "https://example.com"}, Tokens: 1000},
	}
	// Rules elided the two oldest; Jev rates the oldest as needed.
	rules := map[string]bool{"a": true, "b": true}
	scores := map[string]float64{"a": 0.71, "b": 0.06, "c": 0.03}
	got := shadowReport(scores, cands, rules)
	for _, want := range []string{"rules elided 2 of 3", "jev would elide 2", "agree 1", "tokens.go", "0.71"} {
		if !strings.Contains(got, want) {
			t.Errorf("report missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "youtube.go=0.06") {
		t.Errorf("report must list Jev's probability per candidate:\n%s", got)
	}
	all := map[string]bool{"a": true, "b": true, "c": true}
	if got := shadowReport(scores, cands, all); !strings.Contains(got, "no choice") {
		t.Errorf("when every candidate is elided, agreement is meaningless: %q", got)
	}
	if got := shadowReport(nil, cands, rules); !strings.Contains(got, "no verdict") {
		t.Errorf("a failed call must say so: %q", got)
	}
}

// Shadow mode must not change what Plan does.
func TestShadowOptionsKeepRulesOrder(t *testing.T) {
	var steps []step
	for i := 0; i < 30; i++ {
		steps = append(steps, step{"read_file", `{"path":"f` + string(rune('a'+i)) + `.go"}`, 40_000})
	}
	msgs := history(steps...)
	base := Options{Window: 200_000, Used: Estimate(msgs)}
	want := Plan(msgs, base)
	opts, report := Shadow(nil, msgs, base, func(string) {}, true)
	got := Plan(msgs, opts)
	if len(got.Edits) != len(want.Edits) || got.Edits[0].ToolCallID != want.Edits[0].ToolCallID {
		t.Errorf("shadow changed the plan: %d edits vs %d", len(got.Edits), len(want.Edits))
	}
	report(got) // nil client: must not panic or call out
}

func TestGoalAndLatest(t *testing.T) {
	msgs := []tui.ChatMessage{
		{Role: "user", Content: "fix the parser"},
		{Role: "assistant", Content: "ok"},
		{Role: "user", Content: "now the tests"},
		{Role: "assistant", Content: "done"},
	}
	if g, l := goalAndLatest(msgs); g != "fix the parser" || l != "now the tests" {
		t.Errorf("got %q, %q", g, l)
	}
}

func TestGoalAndLatestSkipsHiddenAndDuplicates(t *testing.T) {
	hidden := map[string]any{"hidden": true}
	msgs := []tui.ChatMessage{
		{Role: "user", Content: "[identity directive]", Metadata: hidden},
		{Role: "user", Content: "fix the parser"},
		{Role: "assistant", Content: "ok"},
		{Role: "user", Content: "[prompt refresh]", Metadata: hidden},
	}
	if g, l := goalAndLatest(msgs); g != "fix the parser" || l != "" {
		t.Errorf("got %q, %q; want the visible request and no separate latest", g, l)
	}
}

// Redact before cutting, as for tool content: a secret straddling the cut
// must not leave an unredactable fragment.
func TestGoalAndLatestRedactsBeforeCutting(t *testing.T) {
	// Cut first, only "Bearer abcdefghij" would remain: too short for the
	// bearer pattern, so the fragment would leak.
	prefix := strings.Repeat("a", maxGoalChars-len(" Bearer ")-10) + " "
	g, _ := goalAndLatest([]tui.ChatMessage{{Role: "user", Content: prefix + "Bearer abcdefghijklmnopqrstuvwxyz"}})
	if strings.Contains(g, "abcdefghij") {
		t.Errorf("token fragment survived in the goal: %q", g[len(g)-30:])
	}
}

func TestDescribeCutsOnRuneBoundary(t *testing.T) {
	d := describe(Candidate{Name: "read_file", Args: map[string]any{"path": strings.Repeat("é", 40)}})
	if !utf8.ValidString(d) {
		t.Errorf("invalid UTF-8: %q", d)
	}
}

// No elision means nothing to compare: no call to Jev, no log line.
func TestShadowReportSkipsWhenNothingElided(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Jev was called although the rules elided nothing")
	}))
	defer srv.Close()
	var steps []step
	for i := 0; i < 30; i++ {
		steps = append(steps, step{"read_file", fmt.Sprintf(`{"path":"f%d.go"}`, i), 40_000})
	}
	msgs := history(steps...)
	logged := false
	opts, report := Shadow(&jev.Client{Key: "k", URL: srv.URL}, msgs, Options{Window: 200_000, Used: Estimate(msgs)}, func(string) { logged = true }, false)
	Plan(msgs, opts) // captures candidates
	report(Result{}) // ...but the prune was abandoned
	if logged {
		t.Error("logged a report for a prune that did not happen")
	}
}

// Synchronous mode (agent runs) logs before report returns, so nothing
// writes to the caller's output after the run.
func TestShadowSyncLogsBeforeReturning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"answers":{}}`))
	}))
	defer srv.Close()
	var steps []step
	for i := 0; i < 30; i++ {
		steps = append(steps, step{"read_file", fmt.Sprintf(`{"path":"f%d.go"}`, i), 40_000})
	}
	msgs := history(steps...)
	var line string
	opts, report := Shadow(&jev.Client{Key: "k", URL: srv.URL}, msgs, Options{Window: 200_000, Used: Estimate(msgs)}, func(s string) { line = s }, false)
	report(Plan(msgs, opts))
	if !strings.Contains(line, "jev shadow") {
		t.Errorf("sync report did not log before returning: %q", line)
	}
}
