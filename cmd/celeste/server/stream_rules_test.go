package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
)

func resultText(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var parsed struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || len(parsed.Content) == 0 {
		t.Fatalf("bad result: %s", raw)
	}
	return parsed.Content[0].Text
}

// stream_rules "on" in MCP chat: the claim is interrupted and the call
// returns the re-run's reply (2.0 W3). The backstop strip still applies
// with the default (TestServerChatStripsUnbackedAudioClaim).
func TestServerChatStreamRuleRerunsTheReply(t *testing.T) {
	llm := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{Text: "Audio saved: /tmp/x.mp3"},
		fakeprovider.Turn{Text: "I can't make audio here."},
	)
	cfg, ws := contractCfg(t, llm)
	cfg.CelesteConfig.StreamRules = "on"
	res := call(t, cfg, rpc{1, "tools/call", map[string]any{"name": "celeste", "arguments": map[string]any{"prompt": "say it", "mode": "chat", "workspace": ws}}})
	if got := resultText(t, res[1]); strings.TrimSpace(got) != "I can't make audio here." {
		t.Fatalf("result = %q", got)
	}
	if n := len(llm.Requests()); n != 2 {
		t.Errorf("requests = %d, want 2", n)
	}
}
