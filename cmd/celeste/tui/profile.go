package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/providers"
)

// ProfileResolvedMsg is what a config profile named as an endpoint
// (/config <profile>, /endpoint <profile>) resolves to (2.0 F2e), for a
// client that cannot report its active endpoint (see resolveProfile).
type ProfileResolvedMsg struct {
	Endpoint string // the profile name, as switched to
	Provider string // the provider its base URL belongs to; "" when it could not be loaded or matches none
	Model    string // the profile's model; "" when it could not be loaded
	Tools    bool   // providers.ToolsEnabledForModel(Provider, Model)
}

// loadProfile reads a named config profile. A var so tests can stub it.
var loadProfile = config.LoadNamed

// resolveProfile reads the profile and works out its provider, model and
// tool support. Only clients that cannot report their active endpoint
// (not ActiveEndpointer) take this path; the chat's adapter reports its
// endpoint, and switchEndpoint reads the provider from it instead. It is a command, not inline in Update: LoadNamed reads a
// file, and ToolsEnabledForModel can fetch Venice's live model catalog.
func resolveProfile(endpoint string) tea.Cmd {
	return func() tea.Msg {
		msg := ProfileResolvedMsg{Endpoint: endpoint}
		cfg, err := loadProfile(endpoint)
		if err != nil {
			LogInfo(fmt.Sprintf("profile %s: %v", endpoint, err))
			return msg
		}
		msg.Model = cfg.Model
		if p := providers.DetectProvider(cfg.BaseURL); p != "unknown" {
			msg.Provider = p
			msg.Tools = providers.ToolsEnabledForModel(p, cfg.Model)
		}
		return msg
	}
}

// applyProfile takes a resolved profile's provider, model and tool support.
// A result for an endpoint the chat has since left is dropped.
func (m AppModel) applyProfile(msg ProfileResolvedMsg) AppModel {
	if msg.Endpoint != m.endpoint {
		return m
	}
	if msg.Provider != "" {
		m.provider = msg.Provider
	}
	if msg.Model != "" {
		m.model = msg.Model
		m.header = m.header.SetModel(m.model)
	}
	m.skillsEnabled = msg.Tools
	m.header = m.header.SetSkillsEnabled(m.skillsEnabled)
	LogInfo(fmt.Sprintf("Profile %s: provider %s, model %s, skills enabled: %v", msg.Endpoint, m.provider, m.model, m.skillsEnabled))
	return m
}
