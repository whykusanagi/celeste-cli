package compact

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/decide"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/jev"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// history has three old 40 KiB reads (r0 oldest) and a fresh question.
func actingHistory() []tui.ChatMessage {
	big := strings.Repeat("x", 40*1024)
	msgs := []tui.ChatMessage{{Role: "user", Content: "read the files"}}
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("r%d", i)
		msgs = append(msgs,
			tui.ChatMessage{Role: "assistant", ToolCalls: []tui.ToolCallInfo{{ID: id, Name: "read_file", Arguments: fmt.Sprintf(`{"path":"f%d.txt"}`, i)}}},
			tui.ChatMessage{Role: "tool", ToolCallID: id, Name: "read_file", Content: big})
	}
	return append(msgs, tui.ChatMessage{Role: "user", Content: "now f0 only"})
}

func jevServer(t *testing.T, answers map[string]float64, status int) *jev.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != 0 {
			http.Error(w, "down", status)
			return
		}
		out := map[string]any{}
		for id, p := range answers {
			out[id] = map[string]any{"type": "noul", "noul": p}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "jev-test", "answers": out})
	}))
	t.Cleanup(srv.Close)
	return &jev.Client{Key: "k", URL: srv.URL}
}

func elided(res Result) []string {
	var ids []string
	for _, e := range res.Edits {
		ids = append(ids, e.ToolCallID)
	}
	return ids
}

// "on": the result Jev says is still needed (r0) is kept; without Jev the
// oldest (r0) goes first.
func TestWithJevOnOrdersElisionByNeed(t *testing.T) {
	decide.ResetStats()
	msgs := actingHistory()
	base := Options{Window: 20_000, Force: true}
	rules := Plan(msgs, base)
	if ids := elided(rules); len(ids) == 0 || ids[0] != "r0" {
		t.Fatalf("rules elided %v, want r0 first", ids)
	}
	c := jevServer(t, map[string]float64{"r0": 0.95, "r1": 0.05, "r2": 0.10}, 0)
	var logged []string
	opts, report := WithJev(context.Background(), c, "on", msgs, base, func(s string) { logged = append(logged, s) }, false)
	res := Plan(msgs, opts)
	report(res)
	if ids := elided(res); len(ids) == 0 || ids[0] != "r1" {
		t.Fatalf("jev on elided %v, want r1 (least needed) first", ids)
	}
	if len(logged) != 1 || !strings.Contains(logged[0], "scored 3 of 3") {
		t.Errorf("logged = %v", logged)
	}
	if s := decide.Snapshot(); s["by_use"].(map[string]any)["prune"] == nil {
		t.Errorf("stats = %v", s)
	}
}

// Any Jev error leaves the rules' order.
func TestWithJevOnFailsOpen(t *testing.T) {
	msgs := actingHistory()
	c := jevServer(t, nil, http.StatusServiceUnavailable)
	opts, _ := WithJev(context.Background(), c, "on", msgs, Options{Window: 20_000, Force: true}, func(string) {}, false)
	if ids := elided(Plan(msgs, opts)); len(ids) == 0 || ids[0] != "r0" {
		t.Errorf("elided %v, want the rules' order (r0 first)", ids)
	}
}

func TestWithJevOffOrNilClientIsUntouched(t *testing.T) {
	for _, c := range []*jev.Client{nil, {Key: "k", URL: "http://127.0.0.1:1"}} {
		opts, _ := WithJev(context.Background(), c, "off", nil, Options{Window: 1}, nil, false)
		if opts.Score != nil {
			t.Error("off must not score")
		}
	}
}
