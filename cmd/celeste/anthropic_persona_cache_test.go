package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/prompts/promptstest"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

type anthropicSystemBlock struct {
	Text         string          `json:"text"`
	CacheControl json.RawMessage `json:"cache_control"`
}

func anthropicSystem(t *testing.T, srv *fakeprovider.Server, i int) ([]anthropicSystemBlock, []json.RawMessage) {
	t.Helper()
	reqs := srv.Requests()
	if len(reqs) <= i {
		t.Fatalf("only %d requests", len(reqs))
	}
	var body struct {
		System json.RawMessage `json:"system"`
	}
	if err := json.Unmarshal(reqs[i].Raw, &body); err != nil {
		t.Fatal(err)
	}
	var blocks []anthropicSystemBlock
	var raw []json.RawMessage
	if err := json.Unmarshal(body.System, &blocks); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body.System, &raw); err != nil {
		t.Fatal(err)
	}
	return blocks, raw
}

// #309: the chat's Anthropic requests put a cache breakpoint right after
// the static persona, and /user (a refresh with a new identity) leaves that
// block's bytes as they were, so the next turn reads the persona from cache.
func TestChatAnthropicPersonaBlockSurvivesUserChange(t *testing.T) {
	promptstest.Install(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	srv := fakeprovider.NewAnthropic(t, fakeprovider.Turn{Text: "one"}, fakeprovider.Turn{Text: "two"})
	cfg := &config.Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "claude-opus-5", Timeout: 10, ContextLimit: 200000}
	lc := llm.ConfigFrom(cfg)
	lc.Backend = llm.BackendTypeAnthropic
	a := &TUIClientAdapter{client: llm.NewClient(lc, nil), baseConfig: cfg}
	a.applySystemPrompt()
	history := []tui.ChatMessage{{Role: "user", Content: "hi"}}
	if _, err := a.client.SendMessageSync(context.Background(), history, nil); err != nil {
		t.Fatal(err)
	}
	if err := (&config.UserIdentity{Name: "Alice"}).Save(); err != nil {
		t.Fatal(err)
	}
	a.RefreshSystemPrompt()
	if _, err := a.client.SendMessageSync(context.Background(), history, nil); err != nil {
		t.Fatal(err)
	}

	persona := profileBytes(t, prompts.ProfileFull)
	first, firstRaw := anthropicSystem(t, srv, 0)
	second, secondRaw := anthropicSystem(t, srv, 1)
	for i, sys := range [][]anthropicSystemBlock{first, second} {
		if len(sys) != 2 {
			t.Fatalf("request %d: %d system blocks, want the persona and the dynamic part", i, len(sys))
		}
		if sys[0].Text != persona {
			t.Fatalf("request %d: the first system block is not exactly the full profile", i)
		}
		if !strings.Contains(string(sys[0].CacheControl), "ephemeral") {
			t.Fatalf("request %d: the persona block carries no cache breakpoint", i)
		}
		if len(sys[1].CacheControl) != 0 {
			t.Fatalf("request %d: the dynamic block carries a breakpoint", i)
		}
	}
	if string(firstRaw[0]) != string(secondRaw[0]) {
		t.Fatal("/user changed the persona block's bytes")
	}
	if strings.Contains(first[1].Text, "Alice") || !strings.Contains(second[1].Text, "Alice") {
		t.Fatal("the user identity is not in the dynamic block")
	}
	if got, want := a.client.SystemPrompt(), first[0].Text; !strings.HasPrefix(got, want) {
		t.Fatal("the client's whole prompt does not start with the persona")
	}
}
