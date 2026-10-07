package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools/mcp"
)

// fakeApproval is an MCPApproval over a map of states.
type fakeApproval struct {
	states   map[string]string
	approved []string
	err      error
}

func (f *fakeApproval) State(name string, _ mcp.ServerConfig) string { return f.states[name] }
func (f *fakeApproval) Describe(name string, cfg mcp.ServerConfig) string {
	return "source: \"./.mcp.json\"\ncommand: " + `"` + cfg.Command + `"`
}
func (f *fakeApproval) Approve(name string, _ mcp.ServerConfig) error {
	if f.err != nil {
		return f.err
	}
	f.approved = append(f.approved, name)
	f.states[name] = "approved"
	return nil
}

func key(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

func approvalPanel(state string) (MCPPanelModel, *fakeApproval) {
	fa := &fakeApproval{states: map[string]string{"repo": state}}
	p := NewMCPPanelModel()
	p.SetManager(nil, map[string]mcp.ServerConfig{"repo": {Command: "repo-cmd", Enabled: true, Origin: "/w/.mcp.json"}})
	p.SetApproval(fa)
	p.SetSize(100, 30)
	p.Show()
	return p, fa
}

// #411: a declined server is approved from /mcp only after the panel
// shows its command and the person confirms.
func TestMCPPanel_ApproveDeclinedConfirms(t *testing.T) {
	p, fa := approvalPanel("declined")
	require.Equal(t, "declined", p.servers[0].Approval)
	assert.Contains(t, stripANSI(p.View()), "declined")

	p, cmd := p.Update(key('a'))
	assert.Nil(t, cmd, "pressing a only asks")
	view := stripANSI(p.View())
	assert.Contains(t, view, `"repo-cmd"`, "the confirmation shows the command")
	assert.Contains(t, view, "y approve")
	assert.Empty(t, fa.approved)

	p, cmd = p.Update(key('y'))
	require.NotNil(t, cmd)
	msg := cmd()
	res, ok := msg.(MCPConnectResultMsg)
	require.True(t, ok, "got %T", msg)
	assert.Equal(t, "repo", res.Name)
	assert.Equal(t, []string{"repo"}, fa.approved)
	assert.NotContains(t, stripANSI(p.View()), "y approve", "the confirmation closes")
}

func TestMCPPanel_ApproveCancelled(t *testing.T) {
	for _, k := range []tea.KeyMsg{key('n'), {Type: tea.KeyEsc}, key('x')} {
		p, fa := approvalPanel("declined")
		p, _ = p.Update(key('a'))
		p, cmd := p.Update(k)
		assert.Nil(t, cmd)
		assert.Empty(t, fa.approved)
		assert.True(t, p.Active(), "cancelling the confirmation keeps the panel open")
		assert.NotContains(t, stripANSI(p.View()), "y approve")
	}
}

// c on a declined or pending workspace server goes through the same
// confirmation; on an approved or home one it connects straight away.
func TestMCPPanel_ConnectUnapprovedConfirms(t *testing.T) {
	for _, st := range []string{"declined", "pending"} {
		p, fa := approvalPanel(st)
		p, cmd := p.Update(key('c'))
		assert.Nil(t, cmd, st)
		assert.Contains(t, stripANSI(p.View()), `"repo-cmd"`, st)
		assert.Empty(t, fa.approved)
	}
	for _, st := range []string{"approved", ""} {
		p, _ := approvalPanel(st)
		_, cmd := p.Update(key('c'))
		assert.NotNil(t, cmd, "state %q connects directly", st)
	}
}

func TestMCPPanel_ApproveNotOfferedWhenNotNeeded(t *testing.T) {
	for _, st := range []string{"approved", ""} {
		p, _ := approvalPanel(st)
		p, cmd := p.Update(key('a'))
		assert.Nil(t, cmd)
		assert.NotContains(t, stripANSI(p.View()), "y approve", "state %q", st)
	}
}

func TestMCPPanel_ApproveFailureReported(t *testing.T) {
	p, fa := approvalPanel("declined")
	fa.err = errors.New("trusted.json is corrupt")
	p, _ = p.Update(key('a'))
	_, cmd := p.Update(key('y'))
	require.NotNil(t, cmd)
	res := cmd().(MCPConnectResultMsg)
	require.Error(t, res.Err)
	assert.True(t, strings.Contains(res.Err.Error(), "corrupt"))
}

func TestMCPHintsNameApprove(t *testing.T) {
	assert.Contains(t, hintsFor("chat", true), "a approve")
}
