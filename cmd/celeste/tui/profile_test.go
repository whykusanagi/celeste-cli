package tui

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/providers"
)

// stubProfiles makes loadProfile serve these profiles by name.
func stubProfiles(t *testing.T, profiles map[string]*config.Config) {
	t.Helper()
	orig := loadProfile
	t.Cleanup(func() { loadProfile = orig })
	loadProfile = func(name string) (*config.Config, error) {
		if cfg, ok := profiles[name]; ok {
			return cfg, nil
		}
		return nil, errors.New("config '" + name + "' not found")
	}
}

// profileResolved sends content and returns the ProfileResolvedMsg its
// command produces.
func profileResolved(t *testing.T, m AppModel, content string) (AppModel, ProfileResolvedMsg) {
	t.Helper()
	m, cmd := step(t, m, SendMessageMsg{Content: content})
	for _, msg := range collectMsgs(cmd) {
		if r, ok := msg.(ProfileResolvedMsg); ok {
			return m, r
		}
	}
	t.Fatalf("%s resolved no profile", content)
	return m, ProfileResolvedMsg{}
}

// /config <profile> takes the profile's provider, model and tool support,
// not the previous provider's (before 2.0 F2e the provider became the
// profile's name, GetProvider failed, and skillsEnabled kept its old value).
func TestConfigProfileTakesItsProvidersToolSupport(t *testing.T) {
	stubProfiles(t, map[string]*config.Config{
		"voice": {BaseURL: "https://api.elevenlabs.io/v1", Model: "eleven-turbo"},
		"work":  {BaseURL: "https://api.openai.com/v1", Model: "gpt-4o"},
	})
	m, _ := newQueueTestApp()
	m = m.WithEndpoint("openai")
	require.True(t, m.skillsEnabled)

	m, res := profileResolved(t, m, "/config voice")
	assert.False(t, m.skillsEnabled, "tools stayed on before the profile resolved")
	m, _ = step(t, m, res)
	assert.Equal(t, "elevenlabs", m.provider)
	assert.Equal(t, "eleven-turbo", m.model)
	assert.False(t, m.skillsEnabled, "a provider without function calling kept the previous provider's tools")

	m, res = profileResolved(t, m, "/config work")
	m, _ = step(t, m, res)
	assert.Equal(t, "openai", m.provider)
	assert.Equal(t, "gpt-4o", m.model)
	assert.True(t, m.skillsEnabled)
	assert.True(t, m.toolsOffered())
}

// A profile that cannot be loaded, or whose base URL matches no provider,
// offers no tools; a result for an endpoint the chat has left is dropped.
func TestConfigProfileUnknownOrStale(t *testing.T) {
	stubProfiles(t, map[string]*config.Config{
		"odd":  {BaseURL: "https://llm.example.invalid/v1", Model: "m1"},
		"work": {BaseURL: "https://api.openai.com/v1", Model: "gpt-4o"},
	})
	m, _ := newQueueTestApp()
	m = m.WithEndpoint("openai")

	m, res := profileResolved(t, m, "/config odd")
	m, _ = step(t, m, res)
	assert.False(t, m.skillsEnabled)
	assert.Equal(t, "odd", m.provider, "an unknown provider keeps the profile name")
	assert.Equal(t, "m1", m.model)

	m, res = profileResolved(t, m, "/config missing")
	m, _ = step(t, m, res)
	assert.False(t, m.skillsEnabled)

	m, stale := profileResolved(t, m, "/config work")
	m, _ = step(t, m, SendMessageMsg{Content: "/config openai"})
	require.True(t, m.skillsEnabled)
	m, _ = step(t, m, stale)
	assert.Equal(t, "openai", m.provider, "a stale profile result replaced the current provider")
}

// A client that reports its active endpoint (the chat's adapter) has
// already loaded the profile when it switched: the provider comes from its
// base URL, in memory, with no command (2.0 F2e, re-anchored on #232's
// switchEndpoint).
func TestConfigProfileOnAnEndpointReportingClient(t *testing.T) {
	stubProfiles(t, nil) // the client loads the profile, never the TUI
	yes := true
	defer providers.SetCatalogForTest("openai", []providers.CatalogModel{{ID: "gpt-4o", Default: true, Tools: &yes}})()
	client := &endpointClient{onSwitch: func(name string) ActiveEndpoint {
		if name == "work" {
			return ActiveEndpoint{Provider: "openai", BaseURL: "https://api.openai.com/v1", Model: "gpt-4o"}
		}
		return ActiveEndpoint{Provider: "unknown", BaseURL: "https://llm.example.invalid/v1", Model: "m1"}
	}}
	m := NewApp(client).WithEndpoint("elevenlabs")
	require.False(t, m.skillsEnabled)

	m, cmd := m.switchEndpoint("work")
	assert.Equal(t, "openai", m.provider)
	assert.Equal(t, "gpt-4o", m.model)
	assert.True(t, m.skillsEnabled, "an OpenAI profile offered no tools")
	assert.Nil(t, cmd, "a reporting client's profile was resolved again (its catalog is cached)")

	m, _ = m.switchEndpoint("odd")
	assert.Equal(t, "odd", m.provider, "an unknown provider keeps the profile name")
	assert.False(t, m.skillsEnabled, "the previous provider's tools stayed on")
}
