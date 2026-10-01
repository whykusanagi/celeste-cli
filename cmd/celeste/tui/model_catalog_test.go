package tui

import (
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
	m2, _ := step(t, m, catalogReadyMsg{provider: "venice", baseURL: "https://api.venice.ai/api/v1", models: []providers.CatalogModel{{ID: "x", Default: true}}, ok: true})
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
