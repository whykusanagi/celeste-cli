package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/providers"
)

// The unverified mark and the no-skills warning are separate facts: a forced
// model without function calling shows both.
func TestHeaderShowsUnverifiedAndNoSkillsMarks(t *testing.T) {
	h := NewHeaderModel().SetWidth(120)
	h = h.SetEndpoint("sakana").SetModel("x").SetModelUnverified(true)

	view := stripANSI(h.SetSkillsEnabled(false).View())
	assert.Contains(t, view, "x ? ⚠")

	view = stripANSI(h.SetSkillsEnabled(true).View())
	assert.Contains(t, view, "x ?")
	assert.NotContains(t, view, "✓")

	view = stripANSI(h.SetModelUnverified(false).SetSkillsEnabled(false).View())
	assert.Contains(t, view, "x ⚠")
	assert.NotContains(t, view, "?")
}

// A catalog loaded later that lists the forced model verifies it: the header
// drops "?" and the session stops saving model_unverified.
func TestCatalogListingTheForcedModelClearsTheUnverifiedMark(t *testing.T) {
	yes := true
	restore := providers.SetCatalogForTest("sakana", []providers.CatalogModel{{ID: "fugu", Default: true, Tools: &yes}})
	defer func() { restore() }()
	s := &config.Session{}
	s.SetEndpoint("sakana")
	s.SetModel("fugu")
	client := &endpointClient{ep: ActiveEndpoint{Provider: "sakana", BaseURL: "https://api.sakana.ai/v1", Model: "fugu"}}
	m := newAuditApp(t, client, 120, 40)
	m.provider = "sakana"
	m = m.SetSessionManager(&fakeSessions{session: s}, s)
	m = auditSend(t, m, "/set-model my-private-model --force")
	require.True(t, m.header.modelUnverified)
	require.True(t, s.GetModelUnverified())
	client.ep.Model = "my-private-model"

	// A catalog that still does not list it changes nothing.
	m, _ = step(t, m, catalogReadyMsg{endpoint: endpointID(client.ep)})
	assert.True(t, m.header.modelUnverified)

	restore()
	restore = providers.SetCatalogForTest("sakana", []providers.CatalogModel{
		{ID: "fugu", Default: true, Tools: &yes},
		{ID: "my-private-model", Tools: &yes},
	})
	m, _ = step(t, m, catalogReadyMsg{endpoint: endpointID(client.ep)})
	assert.False(t, m.header.modelUnverified)
	assert.False(t, s.GetModelUnverified())
	assert.Equal(t, "my-private-model", m.model, "the pin still holds")
	header := strings.SplitN(auditView(m), "\n", 2)[0]
	assert.NotContains(t, header, "my-private-model ?", "header: %s", header)
}

// /endpoint venice while in NSFW mode keeps NSFW mode on, so /safe must still
// restore the endpoint /nsfw left, not rebuild one from Venice's profile.
func TestEndpointVeniceInNSFWModeKeepsTheSafeSnapshot(t *testing.T) {
	c := newSnapshotClient()
	m := newAuditApp(t, c, 120, 40)
	m.provider = "sakana"
	m = auditSend(t, m, "/nsfw")
	require.NotNil(t, m.safe)
	m, _ = m.switchEndpoint("venice")
	require.True(t, m.nsfwMode)
	assert.NotNil(t, m.safe)
	m = auditSend(t, m, "/safe")
	assert.Equal(t, 1, c.restored)
	assert.Equal(t, "sakana-key", c.ep.APIKey)
	assert.Equal(t, "fugu", m.model)
}

type fakeResumeClient struct {
	fakeToolLLMClient
	err error
}

func (f *fakeResumeClient) ResumeSubagent(context.Context, string) (string, error) {
	return "", f.err
}

// /agents resume shows a provider error without go-openai's "error, ".
func TestAgentsResumeFailureDropsTheProviderPrefix(t *testing.T) {
	m := newAuditApp(t, &fakeResumeClient{err: errors.New(apiError404)}, 120, 40)
	updated, cmd := m.Update(SendMessageMsg{Content: "/agents resume cp-1"})
	_ = updated
	require.NotNil(t, cmd)
	msg, ok := cmd().(AgentProgressMsg)
	require.True(t, ok)
	assert.Contains(t, msg.Text, "Resume failed: status code: 404")
	assert.NotContains(t, msg.Text, "error, status")
}

// An orchestrator error event shows a provider error the same way.
func TestOrchestratorErrorDropsTheProviderPrefix(t *testing.T) {
	client := &fakeOrchClient{}
	m := NewApp(client)
	m, _ = step(t, m, SendMessageMsg{Content: "/orch say hi"})
	require.Len(t, client.runs, 1)
	m, _ = step(t, m, OrchestratorEventMsg{Kind: 8, Text: "primary: " + apiError404, Run: client.runs[0]})
	assert.True(t, chatHasText(m, "primary: status code: 404"))
	assert.False(t, chatHasText(m, "error, status"))
	assert.NotContains(t, m.status.text, "error, status")
}

// The /index banner rules never outgrow a tiny chat, and a zero or negative
// width does not panic.
func TestIndexBannerRulesFitNarrowWidths(t *testing.T) {
	idx := smallIndexer(t)
	for _, w := range []int{-3, 0, 4, 10, 19} {
		out := stripANSI(RenderCodeGraphConstellation(idx, w))
		require.Contains(t, out, "CODE GRAPH")
		limit := w
		if limit < 4 {
			limit = 4
		}
		for _, l := range strings.Split(out, "\n") {
			body := strings.TrimSpace(l)
			if body != "" && (strings.Trim(body, "▀") == "" || strings.Trim(body, "▄") == "") {
				assert.LessOrEqual(t, len([]rune(l)), limit, "width %d rule: %q", w, l)
			}
		}
	}
}

// N5 leftover: an info-only /agent command never says "Running agent...".
func TestAgentInfoCommandsDoNotSayRunningAgent(t *testing.T) {
	for _, sub := range []string{"list-runs", "help"} {
		m := newAuditApp(t, &fakeAgentLLMClient{}, 120, 40)
		m = auditSend(t, m, "/agent "+sub)
		assert.NotContains(t, m.status.text, "Running agent", sub)
	}
	m := newAuditApp(t, &fakeAgentLLMClient{}, 120, 40)
	m = auditSend(t, m, "/agent fix the tests")
	assert.Contains(t, m.status.text, "Running agent")
}
