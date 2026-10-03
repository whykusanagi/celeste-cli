package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/grimoire"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/llm"
)

// statusFields is the part of celeste_status that #210 adds.
type statusFields struct {
	Grimoire struct {
		Loaded  bool     `json:"loaded"`
		Sources []string `json:"sources"`
	} `json:"grimoire"`
	Project struct {
		Indexed      bool `json:"indexed"`
		TotalFiles   int  `json:"total_files"`
		TotalSymbols int  `json:"total_symbols"`
	} `json:"project"`
	SessionCost struct {
		TotalCostUSD     float64 `json:"total_cost_usd"`
		InputTokens      int     `json:"input_tokens"`
		OutputTokens     int     `json:"output_tokens"`
		Requests         int     `json:"requests"`
		UnpricedRequests int     `json:"unpriced_requests"`
	} `json:"session_cost"`
}

// parseStatus decodes the JSON text block of a celeste_status result and
// fails if any of the three #210 keys is missing.
func parseStatus(t *testing.T, raw json.RawMessage) statusFields {
	t.Helper()
	var res struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &res); err != nil || len(res.Content) != 1 {
		t.Fatalf("bad status result: %s", raw)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(res.Content[0].Text), &keys); err != nil {
		t.Fatalf("status text is not a JSON object: %v\n%s", err, res.Content[0].Text)
	}
	for _, k := range []string{"grimoire", "project", "session_cost"} {
		if _, ok := keys[k]; !ok {
			t.Fatalf("celeste_status has no %q field:\n%s", k, res.Content[0].Text)
		}
	}
	var st statusFields
	if err := json.Unmarshal([]byte(res.Content[0].Text), &st); err != nil {
		t.Fatalf("status text is not JSON: %v\n%s", err, res.Content[0].Text)
	}
	return st
}

// A fresh server on an unindexed workspace with no grimoire reports all
// three fields, empty, and leaves no trace under ~/.celeste/projects.
func TestStatusReportsEmptyGrimoireProjectAndCost(t *testing.T) {
	cfg, _ := contractCfg(t, nil)
	res := call(t, cfg, rpc{1, "tools/call", map[string]any{"name": "celeste_status", "arguments": map[string]any{}}})
	st := parseStatus(t, res[1])
	if st.Grimoire.Loaded || len(st.Grimoire.Sources) != 0 {
		t.Errorf("grimoire = %+v, want not loaded", st.Grimoire)
	}
	if st.Project.Indexed {
		t.Errorf("project = %+v, want not indexed", st.Project)
	}
	if st.SessionCost.Requests != 0 || st.SessionCost.TotalCostUSD != 0 {
		t.Errorf("session_cost = %+v, want zero", st.SessionCost)
	}
	// contractCfg points HOME at a temp dir. A status call must not create
	// the per-project directory for a workspace that was never indexed.
	home, _ := os.UserHomeDir()
	if _, err := os.Stat(filepath.Join(home, ".celeste", "projects")); !os.IsNotExist(err) {
		t.Errorf("celeste_status created %s (stat err: %v)", filepath.Join(home, ".celeste", "projects"), err)
	}
}

// After a chat call, a content call and an index rebuild on a workspace with
// a .grimoire, all three fields report them. The fake provider bills 100
// prompt and 10 completion tokens per request; gpt-4.1 is priced at $2.50 /
// $15.00 per million, so each request costs $0.0004.
func TestStatusReportsGrimoireProjectAndCost(t *testing.T) {
	fake := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{Text: "hello"},
		fakeprovider.Turn{Text: "A short post."},
	)
	cfg, ws := contractCfg(t, fake)
	cfg.CelesteConfig.Model = "gpt-4.1"
	if _, err := grimoire.Init(ws); err != nil {
		t.Fatal(err)
	}
	res := call(t, cfg,
		rpc{1, "tools/call", map[string]any{"name": "celeste", "arguments": map[string]any{"prompt": "hi", "mode": "chat", "workspace": ws}}},
		rpc{2, "tools/call", map[string]any{"name": "celeste_content", "arguments": map[string]any{"prompt": "write a post"}}},
		rpc{3, "tools/call", map[string]any{"name": "celeste_index", "arguments": map[string]any{"operation": "rebuild", "workspace": ws}}},
		rpc{4, "tools/call", map[string]any{"name": "celeste_status", "arguments": map[string]any{}}},
	)
	st := parseStatus(t, res[4])
	if !st.Grimoire.Loaded || len(st.Grimoire.Sources) == 0 || st.Grimoire.Sources[len(st.Grimoire.Sources)-1] != filepath.Join(ws, ".grimoire") {
		t.Errorf("grimoire = %+v, want loaded from %s", st.Grimoire, filepath.Join(ws, ".grimoire"))
	}
	if !st.Project.Indexed || st.Project.TotalFiles != 1 || st.Project.TotalSymbols != 2 {
		t.Errorf("project = %+v, want indexed with 1 file and 2 symbols (main, helper)", st.Project)
	}
	sc := st.SessionCost
	if sc.Requests != 2 || sc.InputTokens != 200 || sc.OutputTokens != 20 || sc.UnpricedRequests != 0 || sc.TotalCostUSD != 0.0008 {
		t.Errorf("session_cost = %+v, want 2 priced requests, 200/20 tokens, $0.0008", sc)
	}
}

// An MCP agent run's requests reach session_cost through OnTurnStats. The
// run's request count depends on the agent's phases, so the test checks that
// every request the fake served was counted, at 100/10 tokens each.
func TestStatusCountsAgentRunCost(t *testing.T) {
	fake := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{Text: "1. Say done."},
		fakeprovider.Turn{Text: "Done.\nTASK_COMPLETE"},
		fakeprovider.Turn{Text: "TASK_COMPLETE"},
		fakeprovider.Turn{Text: "TASK_COMPLETE"},
	)
	cfg, ws := contractCfg(t, fake)
	cfg.CelesteConfig.Model = "gpt-4.1"
	res := call(t, cfg,
		rpc{1, "tools/call", map[string]any{"name": "celeste", "arguments": map[string]any{"prompt": "say done", "mode": "agent", "workspace": ws}}},
		rpc{2, "tools/call", map[string]any{"name": "celeste_status", "arguments": map[string]any{}}},
	)
	n := len(fake.Requests())
	sc := parseStatus(t, res[2]).SessionCost
	if n == 0 || sc.Requests != n || sc.InputTokens != 100*n || sc.OutputTokens != 10*n {
		t.Fatalf("session_cost = %+v after %d agent requests, want %d requests at 100/10 tokens\nagent result: %s", sc, n, n, res[1])
	}
}

// fugu and local models have no price: tokens count, cost stays 0, and the
// request is reported as unpriced. A request with no usage is not counted.
func TestSessionCostCountsUnpricedModels(t *testing.T) {
	var c sessionCost
	c.record("fugu", &llm.TokenUsage{PromptTokens: 50, CompletionTokens: 5})
	c.record("gpt-4.1", nil)
	got := c.snapshot()
	if got["requests"] != 1 || got["unpriced_requests"] != 1 || got["input_tokens"] != 50 || got["total_cost_usd"] != float64(0) {
		t.Fatalf("snapshot = %v", got)
	}
}
