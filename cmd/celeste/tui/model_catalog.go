package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/providers"
)

// ActiveEndpoint is the endpoint the LLM client is talking to now.
type ActiveEndpoint struct {
	Provider string // detected from BaseURL
	BaseURL  string
	APIKey   string // only ever handed to the catalog fetch; never shown or logged
	Model    string
	// AgentModel and SmallModel are checked with Model in the same fetch.
	AgentModel string
	SmallModel string
	// Pinned turns resolution off (pin_model / CELESTE_PIN_MODEL=1).
	Pinned bool
}

// ActiveEndpointer is implemented by LLM clients that can say which endpoint
// they're on, so the chat can resolve the model against what it serves.
type ActiveEndpointer interface {
	ActiveEndpoint() ActiveEndpoint
}

// ServedModelsRefresher is implemented by clients that keep other models
// (agent, small) for the endpoint; the chat calls it after a catalog loads
// so they are re-resolved from memory too.
type ServedModelsRefresher interface {
	RefreshServedModels()
}

// catalogReadyMsg says an endpoint's catalog (and checks of its unlisted
// models) has been loaded into memory, off the Update goroutine.
type catalogReadyMsg struct {
	endpoint string // providers.EndpointID
}

func endpointID(ep ActiveEndpoint) string {
	return providers.EndpointID(ep.Provider, ep.BaseURL, ep.APIKey)
}

// prepareModelsCmd loads an endpoint's catalog and checks its unlisted
// models in a tea.Cmd, so Update never waits on disk or the network.
func prepareModelsCmd(ep ActiveEndpoint, models ...string) tea.Cmd {
	return func() tea.Msg {
		providers.PrepareModels(context.Background(), ep.Provider, ep.BaseURL, ep.APIKey, models...)
		return catalogReadyMsg{endpoint: endpointID(ep)}
	}
}

// resolveServedModel re-resolves m.model from what this process knows the
// active endpoint serves. When more could be known (no catalog in memory, a
// stale one, an unchecked miss) it also returns a Cmd that loads it; the
// model stays as is until catalogReadyMsg arrives. No disk or network I/O
// happens here.
func (m AppModel) resolveServedModel() (AppModel, tea.Cmd) {
	m, pending := m.resolveFromMemory()
	if !pending {
		return m, nil
	}
	ep := m.llmClient.(ActiveEndpointer).ActiveEndpoint()
	return m, prepareModelsCmd(ep, m.model, ep.AgentModel, ep.SmallModel)
}

// resolveFromMemory applies ResolveFromMemory to m.model unless the model
// is pinned, and recomputes the tool gate.
func (m AppModel) resolveFromMemory() (AppModel, bool) {
	src, ok := m.llmClient.(ActiveEndpointer)
	if !ok {
		return m, false
	}
	ep := src.ActiveEndpoint()
	if m.modelPinned || ep.Pinned {
		return m.recomputeSkills(), false
	}
	model, note, pending := providers.ResolveFromMemory(ep.Provider, ep.BaseURL, ep.APIKey, m.model)
	if m.modelTrial != "" && m.modelTrial == m.model {
		if model != m.model {
			// The name the user just typed isn't served: say so and go
			// back, rather than pick some other model for them.
			typed, previous := m.modelTrial, m.modelBeforeTrial
			m.modelTrial, m.modelBeforeTrial = "", ""
			m.chat = m.chat.AddSystemMessage("❌ model not found: " + typed)
			if previous == "" {
				previous = model
			}
			// /set-model saved the typed name to the config; undo that too.
			if cfg, err := config.Load(); err == nil && cfg.Model == typed {
				cfg.Model = previous
				_ = config.Save(cfg)
			}
			return m.applyModel(previous, ""), false
		}
		if !pending {
			m.modelTrial, m.modelBeforeTrial = "", "" // confirmed
		}
	}
	return m.applyModel(model, note), pending
}

// onCatalogReady re-resolves once a catalog loaded, if the chat is still on
// that endpoint.
func (m AppModel) onCatalogReady(msg catalogReadyMsg) AppModel {
	src, ok := m.llmClient.(ActiveEndpointer)
	if !ok || endpointID(src.ActiveEndpoint()) != msg.endpoint {
		return m
	}
	if r, ok := m.llmClient.(ServedModelsRefresher); ok {
		r.RefreshServedModels()
	}
	m, _ = m.resolveFromMemory()
	return m
}

// applyModel switches to model if it differs, says why in the chat, and
// recomputes the tool gate.
func (m AppModel) applyModel(model, note string) AppModel {
	if model != "" && model != m.model {
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
	return m.recomputeSkills()
}

func (m AppModel) recomputeSkills() AppModel {
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
	m.modelPinned = false // a /set-model --force pin belongs to the old endpoint
	m.modelTrial, m.modelBeforeTrial = "", ""
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

	// The provider is the one the client now talks to (2.0 F2e): a client
	// that reports its endpoint has just loaded the endpoint's config
	// (config.<name>.json, whose base URL may belong to another provider
	// than the name suggests), so when it reports a known provider, that
	// provider gates the tools. For any other client a name that is not a
	// provider is a profile, read off the Update goroutine
	// (resolveProfile); until it arrives no tools are offered, never the
	// previous provider's answer.
	_, isProvider := providers.GetProvider(m.provider)
	if src, ok := m.llmClient.(ActiveEndpointer); ok {
		if p := src.ActiveEndpoint().Provider; p != "" {
			if _, known := providers.GetProvider(p); known {
				m.provider = p
			}
		}
	}

	var cmd tea.Cmd
	if _, ok := m.llmClient.(ActiveEndpointer); ok {
		m, cmd = m.adoptActiveModel() // an unknown provider offers no tools
	} else if !isProvider {
		m.skillsEnabled = false
		cmd = resolveProfile(m.endpoint)
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
