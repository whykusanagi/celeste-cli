package tui

import (
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/providers"
)

// veniceTestCatalog builds the fixed Venice catalog the tests in this file
// install, so none of them reach api.venice.ai (#151 W6b review, M4). The
// default model's tool support is a parameter: the gate must follow it.
func veniceTestCatalog(defaultHasTools bool) []providers.CatalogModel {
	yes, no := true, false
	def := defaultHasTools
	return []providers.CatalogModel{
		{ID: "venice-uncensored-1-2", Tools: &def, Default: true},
		{ID: "e2ee-venice-uncensored-24b-p", Tools: &no},
		{ID: "llama-3.3-70b", Tools: &yes},
	}
}

// fakeSessions is a minimal SessionManager whose Load/NewSession return a
// fixed session, so SetSessionManager's model-restore path runs in a test.
type fakeSessions struct{ session *config.Session }

func (f *fakeSessions) NewSession() interface{}                    { return f.session }
func (f *fakeSessions) Save(interface{}) error                     { return nil }
func (f *fakeSessions) Load(string) (interface{}, error)           { return f.session, nil }
func (f *fakeSessions) List() ([]interface{}, error)               { return nil, nil }
func (f *fakeSessions) Delete(string) error                        { return nil }
func (f *fakeSessions) MergeSessions(a, _ interface{}) interface{} { return a }

// #151 W6b: Venice is a ToolsPerModel provider — some models support tools,
// most don't (notably the default, venice-uncensored). The chat must offer
// tools exactly when the selected model supports them, not "never" for every
// Venice model.
func TestSetSessionManager_VeniceGatesToolsPerModel(t *testing.T) {
	defer providers.SetCatalogForTest("venice", veniceTestCatalog(true))()

	uncensored := &config.Session{}
	uncensored.SetEndpoint("venice")
	uncensored.SetModel("e2ee-venice-uncensored-24b-p")

	m := NewApp(nil).WithEndpoint("venice")
	m = m.SetSessionManager(&fakeSessions{session: uncensored}, uncensored)
	if m.skillsEnabled {
		t.Error("skillsEnabled is true for a Venice model whose catalog entry has no tool support")
	}

	toolCapable := &config.Session{}
	toolCapable.SetEndpoint("venice")
	toolCapable.SetModel("llama-3.3-70b")

	m2 := NewApp(nil).WithEndpoint("venice")
	m2 = m2.SetSessionManager(&fakeSessions{session: toolCapable}, toolCapable)
	if !m2.skillsEnabled {
		t.Error("skillsEnabled is false for a non-uncensored Venice model that supports tools")
	}
}

// Review finding: WithEndpoint alone (no model chosen yet — the state a
// brand-new AppModel is in the instant the provider is detected, before
// SetSessionManager or EndpointChange's auto-select ever runs) must gate a
// ToolsPerModel provider on the model that will actually be in effect (the
// provider's own default), not on the model's zero value.
func TestWithEndpoint_VeniceGatesOnTheDefaultModelBeforeSessionRestores(t *testing.T) {
	for _, defaultHasTools := range []bool{true, false} {
		restore := providers.SetCatalogForTest("venice", veniceTestCatalog(defaultHasTools))
		m := NewApp(nil).WithEndpoint("venice")
		restore()
		if m.model != "" {
			t.Fatalf("model = %q, want \"\" (WithEndpoint alone never picks Venice's model)", m.model)
		}
		if m.skillsEnabled != defaultHasTools {
			t.Errorf("skillsEnabled = %v, want the default model's catalog tool support %v", m.skillsEnabled, defaultHasTools)
		}
	}
}

// Non-ToolsPerModel providers are unaffected: their gate stays at the
// provider-level SupportsFunctionCalling regardless of model.
func TestSetSessionManager_HostedProviderUnaffectedByModel(t *testing.T) {
	s := &config.Session{}
	s.SetEndpoint("openai")
	s.SetModel("gpt-4.1-nano")

	m := NewApp(nil).WithEndpoint("openai")
	m = m.SetSessionManager(&fakeSessions{session: s}, s)
	if !m.skillsEnabled {
		t.Error("skillsEnabled is false for openai, which supports function calling")
	}
}
