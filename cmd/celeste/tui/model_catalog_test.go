package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/providers"
)

// endpointClient is a fake LLM client whose active endpoint the test sets,
// recording model changes.
type endpointClient struct {
	fakeToolLLMClient
	ep       ActiveEndpoint
	models   []string
	onSwitch func(endpoint string) ActiveEndpoint
}

func (c *endpointClient) SwitchEndpoint(endpoint string) error {
	if c.onSwitch != nil {
		c.ep = c.onSwitch(endpoint)
	}
	return nil
}
func (c *endpointClient) ChangeModel(model string) error {
	c.models = append(c.models, model)
	c.ep.Model = model
	return nil
}
func (c *endpointClient) ActiveEndpoint() ActiveEndpoint { return c.ep }

const veniceCatalogJSON = `{"data":[
 {"id":"e2ee-venice-uncensored-24b-p","type":"text","model_spec":{"traits":[],"capabilities":{"supportsFunctionCalling":false}}},
 {"id":"venice-uncensored-1-2","type":"text","model_spec":{"traits":["default"],"capabilities":{"supportsFunctionCalling":true}}}
]}`

func chatText(m AppModel) string {
	var b strings.Builder
	for _, msg := range m.chat.GetMessages() {
		b.WriteString(msg.Content)
		b.WriteString("\n")
	}
	return b.String()
}

// Switching to an endpoint with no cached catalog must not fetch inside
// Update: the model stays as configured, a tea.Cmd fetches, and its
// catalogReadyMsg re-resolves the model and the tool gate.
func TestEndpointSwitch_NoCacheFetchesInACmd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Cleanup(providers.ForgetCatalogsForTest)

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		_, _ = w.Write([]byte(veniceCatalogJSON))
	}))
	defer srv.Close()

	client := &endpointClient{onSwitch: func(string) ActiveEndpoint {
		return ActiveEndpoint{Provider: "venice", BaseURL: srv.URL, APIKey: "k", Model: "venice-uncensored"}
	}}
	m := NewApp(client).WithEndpoint("sakana")

	type result struct {
		m   AppModel
		cmd tea.Cmd
	}
	done := make(chan result, 1)
	go func() {
		mm, cmd := m.switchEndpoint("venice")
		done <- result{mm, cmd}
	}()
	var r result
	select {
	case r = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the endpoint switch blocked on the catalog fetch")
	}
	if r.m.model != "venice-uncensored" {
		t.Errorf("model before the catalog = %q, want the configured one kept", r.m.model)
	}
	if r.cmd == nil {
		t.Fatal("no catalog fetch command")
	}

	close(release)
	msg := r.cmd()
	ready, ok := msg.(catalogReadyMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want catalogReadyMsg", msg)
	}
	m2, _ := step(t, r.m, ready)
	if m2.model != "venice-uncensored-1-2" {
		t.Errorf("model after the catalog = %q, want venice-uncensored-1-2", m2.model)
	}
	if got := client.models[len(client.models)-1]; got != "venice-uncensored-1-2" {
		t.Errorf("client model = %q", got)
	}
	if !m2.skillsEnabled {
		t.Error("the resolved model supports tools per the catalog; skills must be on")
	}
	if !strings.Contains(chatText(m2), "venice no longer serves venice-uncensored; using venice-uncensored-1-2") {
		t.Errorf("chat lacks the note:\n%s", chatText(m2))
	}
}

// With the catalog cached, the switch resolves at once and fetches nothing.
func TestEndpointSwitch_CachedCatalogResolvesImmediately(t *testing.T) {
	yes := true
	defer providers.SetCatalogForTest("venice", []providers.CatalogModel{{ID: "venice-uncensored-1-2", Default: true, Tools: &yes}})()
	client := &endpointClient{onSwitch: func(string) ActiveEndpoint {
		return ActiveEndpoint{Provider: "venice", BaseURL: "https://api.venice.ai/api/v1", Model: "venice-uncensored"}
	}}
	m, cmd := NewApp(client).switchEndpoint("venice")
	if cmd != nil {
		t.Error("a fresh cached catalog needs no fetch")
	}
	if m.model != "venice-uncensored-1-2" || !m.skillsEnabled {
		t.Errorf("model=%q skills=%v", m.model, m.skillsEnabled)
	}
}

// A catalogReadyMsg for an endpoint the chat has since left is ignored.
func TestCatalogReady_StaleEndpointIgnored(t *testing.T) {
	client := &endpointClient{ep: ActiveEndpoint{Provider: "sakana", BaseURL: "https://api.sakana.ai/v1", Model: "fugu"}}
	m := NewApp(client).WithEndpoint("sakana")
	m.model = "fugu"
	defer providers.SetCatalogForTest("sakana", []providers.CatalogModel{{ID: "other", Default: true}})()
	m2, _ := step(t, m, catalogReadyMsg{endpoint: providers.EndpointID("venice", "https://api.venice.ai/api/v1", "")})
	if m2.model != "fugu" || len(client.models) != 0 {
		t.Errorf("a stale catalog changed the model to %q", m2.model)
	}
}

// Session restore replaces a retired session model from the cached catalog.
func TestSetSessionManager_ResolvesRetiredSessionModel(t *testing.T) {
	yes := true
	defer providers.SetCatalogForTest("venice", []providers.CatalogModel{{ID: "venice-uncensored-1-2", Default: true, Tools: &yes}})()
	s := &config.Session{}
	s.SetEndpoint("venice")
	s.SetModel("venice-uncensored")
	client := &endpointClient{ep: ActiveEndpoint{Provider: "venice", BaseURL: "https://api.venice.ai/api/v1", Model: "venice-uncensored-1-2"}}
	m := NewApp(client).WithEndpoint("venice")
	m = m.SetSessionManager(&fakeSessions{session: s}, s)
	if m.model != "venice-uncensored-1-2" {
		t.Errorf("restored model = %q, want venice-uncensored-1-2", m.model)
	}
	if !strings.Contains(chatText(m), "no longer serves venice-uncensored") {
		t.Errorf("chat lacks the note:\n%s", chatText(m))
	}
}

// A resumed session on another provider than the client's is not resolved
// against the client's catalog, where every model would look retired.
func TestSetSessionManager_OtherProviderSessionLeftAlone(t *testing.T) {
	defer providers.SetCatalogForTest("sakana", []providers.CatalogModel{{ID: "fugu"}})()
	s := &config.Session{}
	s.SetEndpoint("venice")
	s.SetModel("venice-uncensored-1-2")
	client := &endpointClient{ep: ActiveEndpoint{Provider: "sakana", BaseURL: "https://api.sakana.ai/v1", Model: "fugu"}}
	m := NewApp(client).WithEndpoint("venice")
	m = m.SetSessionManager(&fakeSessions{session: s}, s)
	if m.model != "venice-uncensored-1-2" || len(client.models) != 0 {
		t.Errorf("model = %q, changes = %v", m.model, client.models)
	}
}

// Update never reads the disk cache: with only a disk cache, the switch
// keeps the model and a Cmd loads it.
func TestEndpointSwitch_DiskCacheLoadsInACmd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Cleanup(providers.ForgetCatalogsForTest)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(veniceCatalogJSON))
	}))
	defer srv.Close()
	if _, err := providers.RefreshCatalog(context.Background(), "venice", srv.URL, "k"); err != nil {
		t.Fatal(err)
	}
	providers.ForgetCatalogsForTest() // a new process: only the disk cache is left

	client := &endpointClient{onSwitch: func(string) ActiveEndpoint {
		return ActiveEndpoint{Provider: "venice", BaseURL: srv.URL, APIKey: "k", Model: "venice-uncensored"}
	}}
	m, cmd := NewApp(client).switchEndpoint("venice")
	if m.model != "venice-uncensored" || cmd == nil {
		t.Fatalf("model=%q cmd=%v: the switch must defer to a Cmd", m.model, cmd != nil)
	}
	srv.Close() // the Cmd must be served by the disk cache
	m2, _ := step(t, m, cmd())
	if m2.model != "venice-uncensored-1-2" {
		t.Errorf("model = %q", m2.model)
	}
}

// /set-model X --force pins X: a catalog that loads later must not replace
// it, until the endpoint changes.
func TestSetModelForcePinsAgainstLateCatalog(t *testing.T) {
	yes := true
	client := &endpointClient{ep: ActiveEndpoint{Provider: "venice", BaseURL: "https://api.venice.ai/api/v1", Model: "venice-uncensored-1-2"}}
	m := NewApp(client).WithEndpoint("venice")
	m.model = "venice-uncensored-1-2"
	m, _ = step(t, m, SendMessageMsg{Content: "/set-model my-private-model --force"})
	if m.model != "my-private-model" || !m.modelPinned {
		t.Fatalf("model=%q pinned=%v", m.model, m.modelPinned)
	}
	defer providers.SetCatalogForTest("venice", []providers.CatalogModel{{ID: "venice-uncensored-1-2", Default: true, Tools: &yes}})()
	m, _ = step(t, m, catalogReadyMsg{endpoint: endpointID(client.ep)})
	if m.model != "my-private-model" {
		t.Errorf("a late catalog replaced the pinned model with %q", m.model)
	}
	m, _ = m.switchEndpoint("venice")
	if m.modelPinned {
		t.Error("an endpoint switch must clear the pin")
	}
}

// pin_model / CELESTE_PIN_MODEL reach the TUI through the endpoint.
func TestPinnedEndpointIsNotResolved(t *testing.T) {
	defer providers.SetCatalogForTest("venice", []providers.CatalogModel{{ID: "venice-uncensored-1-2", Default: true}})()
	client := &endpointClient{onSwitch: func(string) ActiveEndpoint {
		return ActiveEndpoint{Provider: "venice", BaseURL: "https://api.venice.ai/api/v1", Model: "venice-uncensored", Pinned: true}
	}}
	m, cmd := NewApp(client).switchEndpoint("venice")
	if m.model != "venice-uncensored" || cmd != nil {
		t.Errorf("model=%q cmd=%v", m.model, cmd != nil)
	}
}

// A restored session model the startup didn't check is checked from Init.
func TestSetSessionManager_UncheckedModelSchedulesACheck(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Cleanup(providers.ForgetCatalogsForTest)
	s := &config.Session{}
	s.SetEndpoint("venice")
	s.SetModel("venice-uncensored")
	client := &endpointClient{ep: ActiveEndpoint{Provider: "venice", BaseURL: "http://127.0.0.1:1/v1", Model: "venice-uncensored"}}
	m := NewApp(client).WithEndpoint("venice")
	m = m.SetSessionManager(&fakeSessions{session: s}, s)
	if !m.modelCheckPending {
		t.Fatal("no catalog in memory: the restored model must be checked")
	}
	if _, cmd := m.resolveServedModel(); cmd == nil {
		t.Error("Init has no check to run")
	}
}

// Picking from the model list clears a --force pin.
func TestSelectorPickClearsPin(t *testing.T) {
	client := &endpointClient{ep: ActiveEndpoint{Provider: "venice", BaseURL: "https://api.venice.ai/api/v1"}}
	m := NewApp(client).WithEndpoint("venice")
	m.modelPinned = true
	m.selectorActive = true
	m, _ = step(t, m, SelectorResultMsg{Selected: &SelectorItem{ID: "venice-uncensored-1-2"}})
	if m.modelPinned || m.model != "venice-uncensored-1-2" {
		t.Errorf("model=%q pinned=%v", m.model, m.modelPinned)
	}
}

// A --force pin is saved with the session and survives a resume: the
// pinned model is kept, with no note.
func TestForcePinSurvivesResume(t *testing.T) {
	defer providers.SetCatalogForTest("venice", []providers.CatalogModel{{ID: "venice-uncensored-1-2", Default: true}})()
	s := &config.Session{}
	s.SetEndpoint("venice")
	s.SetModel("venice-uncensored-1-2")
	client := &endpointClient{ep: ActiveEndpoint{Provider: "venice", BaseURL: "https://api.venice.ai/api/v1", Model: "venice-uncensored-1-2"}}
	m := NewApp(client).WithEndpoint("venice")
	m = m.SetSessionManager(&fakeSessions{session: s}, s)
	step(t, m, SendMessageMsg{Content: "/set-model my-private-model --force"})
	if !s.GetModelPinned() || s.GetModel() != "my-private-model" {
		t.Fatalf("session: model=%q pinned=%v", s.GetModel(), s.GetModelPinned())
	}

	client2 := &endpointClient{ep: ActiveEndpoint{Provider: "venice", BaseURL: "https://api.venice.ai/api/v1", Model: "my-private-model"}}
	r := NewApp(client2).WithEndpoint("venice")
	r = r.SetSessionManager(&fakeSessions{session: s}, s)
	if r.model != "my-private-model" || !r.modelPinned || strings.Contains(chatText(r), "no longer serves") {
		t.Errorf("resumed: model=%q pinned=%v chat:\n%s", r.model, r.modelPinned, chatText(r))
	}
}

// A name typed into /set-model that the provider then reports gone is not
// swapped for another model: the chat says it wasn't found and keeps the
// previous one.
func TestSetModelTypedNameNotFoundKeepsPrevious(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	// The test catalog is complete: an unlisted name counts as a 404.
	defer providers.SetCatalogForTest("anthropic", []providers.CatalogModel{{ID: "claude-sonnet-4-5-20250929"}, {ID: "claude-opus-4-5-20251101"}})()
	client := &endpointClient{ep: ActiveEndpoint{Provider: "anthropic", BaseURL: "https://api.anthropic.com/v1", Model: "claude-sonnet-4-5-20250929"}}
	m := NewApp(client).WithEndpoint("anthropic")
	m.model = "claude-sonnet-4-5-20250929"
	m, cmd := step(t, m, SendMessageMsg{Content: "/set-model claude-sonnet-9-typo"})
	m = runCatalogCmds(t, m, cmd)
	if m.model != "claude-sonnet-4-5-20250929" {
		t.Errorf("model = %q, want the previous model kept", m.model)
	}
	if got := client.models[len(client.models)-1]; got != "claude-sonnet-4-5-20250929" {
		t.Errorf("client model = %q", got)
	}
	text := chatText(m)
	if !strings.Contains(text, "model not found: claude-sonnet-9-typo") || strings.Contains(text, "no longer serves") {
		t.Errorf("chat:\n%s", text)
	}
	if cfg, err := config.Load(); err == nil && cfg.Model == "claude-sonnet-9-typo" {
		t.Error("the config still holds the name that was not found")
	}
}

// runCatalogCmds runs cmd (and any batch inside it) and feeds back the
// catalogReadyMsgs it yields.
func runCatalogCmds(t *testing.T, m AppModel, cmd tea.Cmd) AppModel {
	t.Helper()
	if cmd == nil {
		return m
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			m = runCatalogCmds(t, m, c)
		}
	case catalogReadyMsg:
		m, _ = step(t, m, msg)
	}
	return m
}
