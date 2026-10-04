package server

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/prompts/promptstest"
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

// MCP chat drops the guard's notice from its response, so the server log
// (stderr) carries it, once (#321; context_limit 8195 is used by no other
// test).
func TestMCPChatSmallWindowLogsTheNoticeOnce(t *testing.T) {
	promptstest.Install(t)
	var logs bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(prevOut); log.SetFlags(prevFlags) })
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "hi"}, fakeprovider.Turn{Text: "hi"})
	cfg, ws := contractCfg(t, srv)
	cfg.CelesteConfig.ContextLimit = 8195
	for i := int64(1); i <= 2; i++ {
		call(t, cfg, rpc{i, "tools/call", map[string]any{"name": "celeste", "arguments": map[string]any{"prompt": "hello", "mode": "chat", "workspace": ws}}})
	}
	if got := strings.Count(logs.String(), "[persona] Persona: using the lite profile"); got != 1 {
		t.Fatalf("want the notice logged once, got %d: %q", got, logs.String())
	}
}
