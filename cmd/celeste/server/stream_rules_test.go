package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/loop"
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

// A reply a stream rule dropped was still billed by the provider, so it
// counts in session_cost (coordinator ruling on W3-1); it never reaches the
// history. Chat and agent mode both.
func TestStreamRuleDroppedRepliesCountInSessionCost(t *testing.T) {
	for _, mode := range []string{"chat", "agent"} {
		t.Run(mode, func(t *testing.T) {
			fake := fakeprovider.NewOpenAI(t,
				fakeprovider.Turn{Text: "TASK_COMPLETE: Audio saved: /tmp/x.mp3"},
				fakeprovider.Turn{Text: "TASK_COMPLETE: no audio was made"},
				fakeprovider.Turn{Text: "TASK_COMPLETE: no audio was made"},
				fakeprovider.Turn{Text: "TASK_COMPLETE: no audio was made"},
			)
			cfg, ws := contractCfg(t, fake)
			cfg.CelesteConfig.Model = "gpt-4.1"
			cfg.CelesteConfig.StreamRules = "on"
			res := call(t, cfg,
				rpc{1, "tools/call", map[string]any{"name": "celeste", "arguments": map[string]any{"prompt": "say it", "mode": mode, "workspace": ws}}},
				rpc{2, "tools/call", map[string]any{"name": "celeste_status", "arguments": map[string]any{}}},
			)
			n := len(fake.Requests())
			if n < 2 {
				t.Fatalf("requests = %d: the rule never dropped a reply\n%s", n, res[1])
			}
			if sc := parseStatus(t, res[2]).SessionCost; sc.Requests != n || sc.InputTokens != 100*n {
				t.Fatalf("session_cost = %+v after %d requests (one dropped)", sc, n)
			}
		})
	}
}

// One MCP chat call's steering: nothing when stream rules and the
// watchdog are both off; the watchdog alone is enough for a session.
func TestChatSteeringFollowsTheConfig(t *testing.T) {
	cfg, ws := contractCfg(t, nil)
	env, err := loop.Setup(loop.ModeMCPChat, cfg.CelesteConfig, ws, loop.SetupOptions{Warn: func(string) {}})
	if err != nil {
		t.Fatal(err)
	}
	defer env.Close()
	c := *cfg.CelesteConfig
	c.StreamRules, c.Watchdog = "off", "off"
	if chatSteering(context.Background(), &c, env, "hi", ws) != nil {
		t.Error("both off must give no steering")
	}
	c.Watchdog = "shadow"
	if chatSteering(context.Background(), &c, env, "hi", ws) == nil {
		t.Error("the watchdog alone needs a session")
	}
}
