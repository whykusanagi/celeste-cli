package server

import (
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/prompts/promptstest"
)

func chatSystem(t *testing.T, srv *fakeprovider.Server) string {
	t.Helper()
	reqs := srv.Requests()
	if len(reqs) == 0 {
		t.Fatal("no request reached the provider")
	}
	return reqs[0].Body["messages"].([]any)[0].(map[string]any)["content"].(string)
}

func profileText(t *testing.T, p prompts.Profile) string {
	t.Helper()
	pp, err := prompts.LoadProfile(p)
	if err != nil {
		t.Fatal(err)
	}
	return pp.SystemPrompt
}

// MCP chat is chat: the full profile on a large window (spec W5 "Profiles
// by mode").
func TestMCPChatUsesTheFullProfile(t *testing.T) {
	promptstest.Install(t)
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "hi"})
	cfg, ws := contractCfg(t, srv)
	cfg.CelesteConfig.ContextLimit = 200000
	call(t, cfg, rpc{1, "tools/call", map[string]any{"name": "celeste", "arguments": map[string]any{"prompt": "hello", "mode": "chat", "workspace": ws}}})
	if !strings.HasPrefix(chatSystem(t, srv), profileText(t, prompts.ProfileFull)) {
		t.Fatal("MCP chat does not start with the full profile")
	}
}

// At the local default window MCP chat steps down, and the response shape
// stays frozen: no notice in it (ruling 7).
func TestMCPChatSmallWindowAddsNoNotice(t *testing.T) {
	promptstest.Install(t)
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "hi"})
	cfg, ws := contractCfg(t, srv)
	res := call(t, cfg, rpc{1, "tools/call", map[string]any{"name": "celeste", "arguments": map[string]any{"prompt": "hello", "mode": "chat", "workspace": ws}}})
	if !strings.HasPrefix(chatSystem(t, srv), profileText(t, prompts.ProfileLite)) {
		t.Fatal("MCP chat at 8,192 did not step down to lite")
	}
	if out := string(res[1]); strings.Contains(out, "Persona:") || strings.Contains(out, "context_limit") {
		t.Fatalf("the persona notice reached the MCP response: %s", out)
	}
}

// celeste_content steps down too, without a notice in its response.
func TestMCPContentSmallWindowStepsDown(t *testing.T) {
	promptstest.Install(t)
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "post"})
	cfg, _ := contractCfg(t, srv)
	res := call(t, cfg, rpc{1, "tools/call", map[string]any{"name": "celeste_content", "arguments": map[string]any{"prompt": "a post", "format": "short"}}})
	if !strings.HasPrefix(chatSystem(t, srv), profileText(t, prompts.ProfileLite)) {
		t.Fatal("celeste_content at 8,192 did not step down to lite")
	}
	if out := string(res[1]); strings.Contains(out, "Persona:") {
		t.Fatalf("the persona notice reached the MCP response: %s", out)
	}
}
