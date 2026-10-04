package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

// V15: /handoff starts a new session, so the context bar and the tracker
// start from zero for it instead of keeping the old session's numbers.
func TestHandoffResetsContextBar(t *testing.T) {
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			m, _ := newCompactTestApp(t)
			mgr := &diskSessions{mgr: config.NewSessionManager()}
			old := mgr.mgr.NewSession()
			m = m.SetSessionManager(mgr, old)
			m.contextTracker = config.NewContextTracker(old, "test-model", 100_000)
			m.contextTracker.CurrentTokens = 50_000
			sized, _ := m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
			m = sized.(AppModel)

			m = runToolTurn(t, m)
			m, _ = feed(t, m, TurnDoneMsg{Stop: "done"})
			m, _ = step(t, m, ContextBudgetMsg{UsedTokens: 50_000, MaxTokens: 100_000, UsagePercent: 50, TurnCount: 7})
			require.Contains(t, auditView(m), "50%")

			m, cmd := step(t, m, SendMessageMsg{Content: "/handoff"})
			m = runCmd(t, m, cmd)
			require.Equal(t, "handoff notes for go", m.input.Value())

			assert.Equal(t, 0, m.contextBar.usedTokens)
			assert.Equal(t, 0, m.contextBar.turnCount)
			assert.Equal(t, 100_000, m.contextBar.maxTokens, "the window size is kept")
			newSess, ok := m.currentSession.(*config.Session)
			require.True(t, ok)
			require.NotEqual(t, old.ID, newSess.ID)
			assert.Same(t, newSess, m.contextTracker.Session, "the tracker follows the new session")
			assert.Equal(t, 0, m.contextTracker.Budget.TurnCount)

			frame := auditView(m)
			assert.NotContains(t, frame, "50%")
			assert.NotContains(t, frame, "turn: 7")
			if sz.w >= 80 {
				assert.True(t, strings.Contains(frame, "turn: 0"), frame)
			}
			assertFrameFits(t, frame, sz.w, sz.h)
		})
	}
}

// /clear also starts a new, empty session, so it resets the bar the same way.
func TestClearResetsContextBar(t *testing.T) {
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			m := newAuditApp(t, &fakeToolLLMClient{}, sz.w, sz.h)
			mgr := &diskSessions{mgr: config.NewSessionManager()}
			old := mgr.mgr.NewSession()
			m = m.SetSessionManager(mgr, old)
			m.contextTracker = config.NewContextTracker(old, "test-model", 100_000)
			m, _ = step(t, m, ContextBudgetMsg{UsedTokens: 50_000, MaxTokens: 100_000, UsagePercent: 50, TurnCount: 7})
			require.Contains(t, auditView(m), "50%")

			m = auditSend(t, m, "/clear")
			frame := auditView(m)
			assert.NotContains(t, frame, "50%")
			assert.NotContains(t, frame, "turn: 7")
			assert.NotSame(t, old, m.contextTracker.Session)
			assertFrameFits(t, frame, sz.w, sz.h)
		})
	}
}
