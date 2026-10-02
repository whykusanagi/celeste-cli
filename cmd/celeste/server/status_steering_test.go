package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/decide"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/rules"
)

type steeringStatus struct {
	Health      string `json:"health"`
	Completions struct {
		OK        int    `json:"ok"`
		Failed    int    `json:"failed"`
		LastError string `json:"last_error"`
	} `json:"completions"`
	Rules struct {
		Mode     string         `json:"mode"`
		Fires    int            `json:"fires"`
		Shadowed int            `json:"shadowed"`
		ByRule   map[string]int `json:"by_rule"`
	} `json:"rules"`
	Oracle struct {
		Mode string            `json:"mode"`
		Jev  map[string]string `json:"jev"`
	} `json:"oracle"`
}

func statusOf(t *testing.T, raw json.RawMessage) steeringStatus {
	t.Helper()
	var st steeringStatus
	if err := json.Unmarshal([]byte(resultText(t, raw)), &st); err != nil {
		t.Fatalf("status is not JSON: %v", err)
	}
	return st
}

// health reports "degraded" while the latest completion failed, and "ok"
// again once one succeeds (2.0 W3: health said ok while completions timed
// out).
func TestStatusHealthFollowsCompletions(t *testing.T) {
	fake := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{Status: 400, Body: `{"error":{"message":"model overloaded"}}`},
		fakeprovider.Turn{Text: "A short post."},
	)
	cfg, ws := contractCfg(t, fake)
	res := call(t, cfg,
		rpc{1, "tools/call", map[string]any{"name": "celeste", "arguments": map[string]any{"prompt": "hi", "mode": "chat", "workspace": ws}}},
		rpc{2, "tools/call", map[string]any{"name": "celeste_status", "arguments": map[string]any{}}},
		rpc{3, "tools/call", map[string]any{"name": "celeste_content", "arguments": map[string]any{"prompt": "write a post"}}},
		rpc{4, "tools/call", map[string]any{"name": "celeste_status", "arguments": map[string]any{}}},
	)
	if st := statusOf(t, res[2]); st.Health != "degraded" || st.Completions.Failed != 1 || !strings.Contains(st.Completions.LastError, "400") {
		t.Errorf("after a failed completion: %+v", st)
	}
	if st := statusOf(t, res[4]); st.Health != "ok" || st.Completions.OK != 1 || st.Completions.LastError != "" {
		t.Errorf("after a good completion: %+v", st)
	}
}

// Rule fires are counted, in shadow by default; the oracle reports its
// mode and the jev_* switches.
func TestStatusCountsRuleFires(t *testing.T) {
	rules.ResetStats()
	decide.ResetStats()
	fake := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "Audio saved: /tmp/x.mp3"})
	cfg, ws := contractCfg(t, fake)
	cfg.CelesteConfig.JevRoute = "shadow"
	res := call(t, cfg,
		rpc{1, "tools/call", map[string]any{"name": "celeste", "arguments": map[string]any{"prompt": "say it", "mode": "chat", "workspace": ws}}},
		rpc{2, "tools/call", map[string]any{"name": "celeste_status", "arguments": map[string]any{}}},
	)
	st := statusOf(t, res[2])
	if st.Rules.Mode != "shadow" || st.Rules.Fires != 1 || st.Rules.Shadowed != 1 || st.Rules.ByRule["unbacked-audio-claim"] != 1 {
		t.Errorf("rules = %+v", st.Rules)
	}
	if st.Oracle.Mode != "heuristic" || st.Oracle.Jev["route"] != "shadow" || st.Oracle.Jev["gate"] != "off" {
		t.Errorf("oracle = %+v", st.Oracle)
	}
}
