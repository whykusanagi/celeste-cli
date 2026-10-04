package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/providers"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// N1: /nsfw then /safe puts the chat back exactly where it was: the same
// endpoint, key and model, for a profile with a Venice key configured. Before,
// /safe kept Venice's URL, key and model, so /set-model fugu then failed.
func TestSafeRestoresThePreNSFWEndpointKeyAndModel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("VENICE_API_KEY", "")
	t.Setenv("VENICE_API_BASE_URL", "")
	yes := true
	defer providers.SetCatalogForTest("sakana", []providers.CatalogModel{{ID: "fugu", Default: true, Tools: &yes}, {ID: "fugu-ultra", Tools: &yes}})()
	defer providers.SetCatalogForTest("venice", []providers.CatalogModel{{ID: "venice-uncensored-1-2", Default: true, Tools: &yes}})()

	dir := filepath.Join(home, ".celeste")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	skills := `{"venice_api_key":"venice-test-key","venice_base_url":"https://api.venice.ai/api/v1","venice_model":"venice-uncensored-1-2"}`
	if err := os.WriteFile(filepath.Join(dir, "skills.json"), []byte(skills), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{APIKey: "sakana-test-key", BaseURL: "https://api.sakana.ai/v1", Model: "fugu", Timeout: 10}
	app, deps, err := newChatApp(cfg, t.TempDir(), home)
	if err != nil {
		t.Fatal(err)
	}
	cleanupChatDeps(t, deps)
	var m tea.Model = app
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	m, _ = m.Update(tui.SendMessageMsg{Content: "/set-model fugu-ultra"})
	before := *deps.adapter.client.GetConfig()
	if before.Model != "fugu-ultra" {
		t.Fatalf("model before /nsfw = %q", before.Model)
	}

	m, _ = m.Update(tui.SendMessageMsg{Content: "/nsfw"})
	if c := deps.adapter.client.GetConfig(); c.APIKey != "venice-test-key" || !strings.Contains(c.BaseURL, "venice.ai") {
		t.Fatalf("/nsfw did not switch to Venice: url=%q", c.BaseURL)
	}

	m, _ = m.Update(tui.SendMessageMsg{Content: "/safe"})
	after := deps.adapter.client.GetConfig()
	if after.BaseURL != before.BaseURL || after.APIKey != before.APIKey || after.Model != before.Model {
		t.Errorf("/safe restored url=%q model=%q key-restored=%v, want url=%q model=%q",
			after.BaseURL, after.Model, after.APIKey == before.APIKey, before.BaseURL, before.Model)
	}
	if ep := deps.adapter.ActiveEndpoint(); ep.Provider != "sakana" {
		t.Errorf("provider after /safe = %q", ep.Provider)
	}
	header := strings.SplitN(ansi.Strip(m.View()), "\n", 2)[0]
	if strings.Contains(header, "venice") || !strings.Contains(header, "fugu-ultra") {
		t.Errorf("header after /safe: %s", header)
	}

	m, _ = m.Update(tui.SendMessageMsg{Content: "/set-model fugu"})
	for _, msg := range chatMessages(m) {
		if strings.Contains(msg.Content, "model not found") || strings.Contains(msg.Content, "not found for provider") {
			t.Errorf("/set-model fugu after /safe failed: %s", msg.Content)
		}
	}
	if got := deps.adapter.client.GetConfig().Model; got != "fugu" {
		t.Errorf("model after /set-model fugu = %q", got)
	}
}
