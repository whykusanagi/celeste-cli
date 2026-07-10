package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestMCPPanel_ConnectKeyEmitsCommand(t *testing.T) {
	p := NewMCPPanelModel()
	p.servers = []MCPServerInfo{{Name: "srv", Connected: false, Enabled: true}}
	p.active = true
	p.cursor = 0

	updated, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	assert.True(t, updated.active)
	assert.NotNil(t, cmd, "pressing 'c' on a disconnected server must dispatch a connect command")
}

func TestMCPPanel_DisconnectKeyOnlyWhenConnected(t *testing.T) {
	p := NewMCPPanelModel()
	p.servers = []MCPServerInfo{{Name: "srv", Connected: false, Enabled: true}}
	p.active = true
	p.cursor = 0

	// 'd' on a disconnected server is a no-op (no command).
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	assert.Nil(t, cmd)

	p.servers[0].Connected = true
	_, cmd = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	assert.NotNil(t, cmd, "pressing 'd' on a connected server must dispatch a disconnect command")
}
