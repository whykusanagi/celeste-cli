package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/providers"
)

// ActiveEndpoint is the endpoint the LLM client is talking to now.
type ActiveEndpoint struct {
	Provider string // detected from BaseURL
	BaseURL  string
	APIKey   string // only ever handed to the catalog fetch; never shown or logged
	Model    string
}

// ActiveEndpointer is implemented by LLM clients that can say which endpoint
// they're on, so the chat can resolve the model against what it serves.
type ActiveEndpointer interface {
	ActiveEndpoint() ActiveEndpoint
}

// catalogReadyMsg carries a catalog fetched off the Update goroutine.
type catalogReadyMsg struct {
	provider string
	baseURL  string
	models   []providers.CatalogModel
	ok       bool
}

// fetchCatalogCmd fetches an endpoint's catalog in a tea.Cmd, so Update
// never waits on the network.
// It reads the disk cache first and fetches only when that is missing or
// stale; a failed fetch falls back to the stale cache.
func fetchCatalogCmd(ep ActiveEndpoint) tea.Cmd {
	return func() tea.Msg {
		cached, stale, ok := providers.CachedCatalog(ep.Provider, ep.BaseURL, ep.APIKey)
		if ok && !stale {
			return catalogReadyMsg{provider: ep.Provider, baseURL: ep.BaseURL, models: cached, ok: true}
		}
		models, err := providers.RefreshCatalog(context.Background(), ep.Provider, ep.BaseURL, ep.APIKey)
		if err != nil {
			LogInfo(fmt.Sprintf("Model catalog for %s unavailable: %v", ep.Provider, err))
			return catalogReadyMsg{provider: ep.Provider, baseURL: ep.BaseURL, models: cached, ok: ok}
		}
		return catalogReadyMsg{provider: ep.Provider, baseURL: ep.BaseURL, models: models, ok: true}
	}
}

// resolveServedModel re-resolves m.model against the active endpoint's
// catalog as loaded in memory. With none in memory (or a stale one) it also
// returns a Cmd that reads the disk cache or fetches; the model stays as is
// until catalogReadyMsg arrives. No disk or network I/O happens here.
func (m AppModel) resolveServedModel() (AppModel, tea.Cmd) {
	src, ok := m.llmClient.(ActiveEndpointer)
	if !ok {
		return m, nil
	}
	ep := src.ActiveEndpoint()
	cat, stale, cached := providers.MemoryCatalog(ep.Provider, ep.BaseURL, ep.APIKey)
	var cmd tea.Cmd
	if (!cached || stale) && providers.HasCatalog(ep.Provider) {
		cmd = fetchCatalogCmd(ep)
	}
	if cached {
		m = m.applyResolvedModel(ep.Provider, cat)
	}
	return m, cmd
}

// onCatalogReady applies a fetched catalog if the chat is still on that
// endpoint.
func (m AppModel) onCatalogReady(msg catalogReadyMsg) AppModel {
	if !msg.ok {
		return m
	}
	src, ok := m.llmClient.(ActiveEndpointer)
	if !ok {
		return m
	}
	ep := src.ActiveEndpoint()
	if ep.Provider != msg.provider || !providers.SameEndpoint(ep.Provider, ep.BaseURL, msg.baseURL) {
		return m
	}
	return m.applyResolvedModel(ep.Provider, msg.models)
}

// applyResolvedModel switches to the served model when m.model is retired,
// says so in the chat, and recomputes the tool gate.
func (m AppModel) applyResolvedModel(provider string, cat []providers.CatalogModel) AppModel {
	model, note := providers.ResolveModel(provider, m.model, cat, true)
	if model != m.model {
		m.model = model
		m.header = m.header.SetModel(model)
		if switcher, ok := m.llmClient.(EndpointSwitcher); ok {
			if err := switcher.ChangeModel(model); err != nil {
				LogInfo(fmt.Sprintf("Error changing model: %v", err))
			}
		}
		if note != "" {
			m.chat = m.chat.AddSystemMessage("⚠ " + note)
			LogInfo(note)
		}
		m = m.syncStatusLine()
		m.persistSession()
	}
	if m.provider != "" {
		m.skillsEnabled = providers.ToolsEnabledForModel(m.provider, m.model)
		m.header = m.header.SetSkillsEnabled(m.skillsEnabled)
	}
	return m
}

// switchEndpoint switches the chat to another endpoint. The client switches
// first, so its new config decides the model; that model is then resolved
// against the endpoint's cached catalog (a Cmd fetches it when there is
// none).
func (m AppModel) switchEndpoint(endpoint string) (AppModel, tea.Cmd) {
	m.endpoint = endpoint
	m.provider = endpoint // provider names match endpoint names
	m.status = m.status.SetText(fmt.Sprintf("Switched to %s", m.endpoint))

	// Leaving Venice turns NSFW mode off.
	if m.endpoint != "venice" && m.nsfwMode {
		m.nsfwMode = false
		m.header = m.header.SetNSFWMode(false)
		LogInfo("NSFW mode disabled when switching away from Venice")
	}

	switcher, canSwitch := m.llmClient.(EndpointSwitcher)
	if canSwitch {
		if err := switcher.SwitchEndpoint(m.endpoint); err != nil {
			m.status = m.status.SetText(fmt.Sprintf("Error switching endpoint: %v", err))
		}
	}

	var cmd tea.Cmd
	if _, ok := m.llmClient.(ActiveEndpointer); ok {
		m, cmd = m.adoptActiveModel()
	} else if caps, ok := providers.GetProvider(m.provider); ok {
		// A client that can't say its endpoint gets the registry's model.
		if model := caps.PreferredToolModel; model != "" || caps.DefaultModel != "" {
			if model == "" {
				model = caps.DefaultModel
			}
			m.model = model
			m.header = m.header.SetModel(m.model)
			if canSwitch {
				if err := switcher.ChangeModel(m.model); err != nil {
					LogInfo(fmt.Sprintf("Error changing model: %v", err))
				}
			}
		}
		m.skillsEnabled = providers.ToolsEnabledForModel(m.provider, m.model)
	}
	LogInfo(fmt.Sprintf("Provider: %s, model: %s, skills enabled: %v", m.provider, m.model, m.skillsEnabled))

	m.header = m.header.SetEndpoint(m.endpoint)
	m.header = m.header.SetSkillsEnabled(m.skillsEnabled)
	m.persistSession()
	return m, cmd
}

// adoptActiveModel takes the model from the client's active endpoint (after
// a switch), recomputes the tool gate, and resolves it against what the
// endpoint serves.
func (m AppModel) adoptActiveModel() (AppModel, tea.Cmd) {
	src, ok := m.llmClient.(ActiveEndpointer)
	if !ok {
		return m, nil
	}
	ep := src.ActiveEndpoint()
	model := ep.Model
	if model == "" {
		model, _ = providers.ResolveModel(ep.Provider, "", nil, false)
		if switcher, ok := m.llmClient.(EndpointSwitcher); ok && model != "" {
			if err := switcher.ChangeModel(model); err != nil {
				LogInfo(fmt.Sprintf("Error changing model: %v", err))
			}
		}
	}
	m.model = model
	m.header = m.header.SetModel(m.model)
	if m.provider != "" {
		m.skillsEnabled = providers.ToolsEnabledForModel(m.provider, m.model)
		m.header = m.header.SetSkillsEnabled(m.skillsEnabled)
	}
	return m.resolveServedModel()
}
