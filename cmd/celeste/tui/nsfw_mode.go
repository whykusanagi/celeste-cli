package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// safeState is the chat as /nsfw found it: the client's endpoint snapshot
// and the chat's own view of it, so /safe can put both back exactly (N1).
type safeState struct {
	snapshot   any
	endpoint   string
	provider   string
	model      string
	pinned     bool
	unverified bool
}

// setNSFWMode turns NSFW mode on (switch to Venice, saving the endpoint it
// leaves) or off (/safe: restore that endpoint, its key and model). Before
// N1, /safe rebuilt the endpoint from the Venice profile it was leaving, so
// the chat kept Venice's URL and model, and /set-model then failed.
func (m AppModel) setNSFWMode(on bool) (AppModel, tea.Cmd) {
	was := m.nsfwMode
	if !on && !was {
		// /safe outside NSFW mode has nothing to undo; switching back by
		// name here would leave an endpoint chosen since (/endpoint).
		return m, nil
	}
	m.nsfwMode = on
	m.header = m.header.SetNSFWMode(on)
	var cmd tea.Cmd

	if on {
		if !was {
			m.safe = nil
			if snap, ok := m.llmClient.(EndpointSnapshotter); ok {
				m.safe = &safeState{
					snapshot:   snap.SnapshotEndpoint(),
					endpoint:   m.endpoint,
					provider:   m.provider,
					model:      m.model,
					pinned:     m.modelPinned,
					unverified: m.header.modelUnverified,
				}
			}
			m.safeEndpoint = m.endpoint
		}
		m.endpoint = "venice"
		m.header = m.header.SetEndpoint(m.endpoint)
		if switcher, ok := m.llmClient.(EndpointSwitcher); ok {
			if err := switcher.SwitchEndpoint(m.endpoint); err != nil {
				m.status = m.status.SetText(fmt.Sprintf("Error switching to Venice: %v", err))
			}
		}
		m.modelPinned = false // a --force pin belongs to the old endpoint
		m, cmd = m.adoptActiveModel()
		m.persistSession()
		return m, cmd
	}

	if was && m.safe != nil {
		saved := *m.safe
		m.safe = nil
		snap, _ := m.llmClient.(EndpointSnapshotter)
		if snap != nil {
			if err := snap.RestoreEndpoint(saved.snapshot); err != nil {
				LogInfo(fmt.Sprintf("Error restoring the safe endpoint: %v", err))
			} else {
				m.endpoint, m.provider = saved.endpoint, saved.provider
				m.model, m.modelPinned = saved.model, saved.pinned
				m.modelTrial, m.modelBeforeTrial = "", ""
				m.header = m.header.SetEndpoint(m.endpoint).SetModel(m.model).SetModelUnverified(saved.unverified)
				m = m.recomputeSkills().syncStatusLine()
				m.persistSession()
				return m, nil
			}
		}
	}

	// No snapshot (a client that cannot take one): switch back by name.
	m.safe = nil
	if m.safeEndpoint != "" {
		m.endpoint = m.safeEndpoint
	} else {
		m.endpoint = "openai" // no safe endpoint saved
	}
	m.header = m.header.SetEndpoint(m.endpoint)
	if switcher, ok := m.llmClient.(EndpointSwitcher); ok {
		if err := switcher.SwitchEndpoint(m.endpoint); err != nil {
			m.status = m.status.SetText(fmt.Sprintf("Error switching endpoint: %v", err))
		}
	}
	m.modelPinned = false // a --force pin belongs to the old endpoint
	m, cmd = m.adoptActiveModel()
	m.persistSession()
	return m, cmd
}
