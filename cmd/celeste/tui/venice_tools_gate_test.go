package tui

import (
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/providers"
)

// veniceStubCatalog is the fixed Venice tool-support map every test in this
// file stubs in, so none of them hit the live api.venice.ai (#151 W6b
// review, M4).
var veniceStubCatalog = map[string]bool{
	"venice-uncensored": false,
	"llama-3.3-70b":     true,
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
	defer providers.StubVeniceToolCatalogForTest(veniceStubCatalog)()

	uncensored := &config.Session{}
	uncensored.SetEndpoint("venice")
	uncensored.SetModel("venice-uncensored")

	m := NewApp(nil).WithEndpoint("venice")
	m = m.SetSessionManager(&fakeSessions{session: uncensored}, uncensored)
	if m.skillsEnabled {
		t.Error("skillsEnabled is true for venice-uncensored, which has no tool support")
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
// SetSessionManager or EndpointChange's auto-select ever runs) must not
// default a ToolsPerModel provider to "tools enabled". Gate on the model
// that will actually be in effect (the provider's own default) instead of
// the model's zero value.
func TestWithEndpoint_VeniceDefaultModelHasNoToolsBeforeSessionRestores(t *testing.T) {
	defer providers.StubVeniceToolCatalogForTest(veniceStubCatalog)()

	m := NewApp(nil).WithEndpoint("venice")
	if m.model != "" {
		t.Fatalf("model = %q, want \"\" (WithEndpoint alone never picks Venice's model)", m.model)
	}
	if m.skillsEnabled {
		t.Error("skillsEnabled is true with no model chosen yet; Venice's own default (venice-uncensored) has no tools")
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
