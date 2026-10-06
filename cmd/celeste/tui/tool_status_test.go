package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

// failedToolTurn runs one turn whose single tool call fails.
func failedToolTurn(t *testing.T, m AppModel, name, errText string) AppModel {
	t.Helper()
	m, _ = step(t, m, SendMessageMsg{Content: "go"})
	m, _ = feed(t, m, TurnStartMsg{Turn: 1})
	m, _ = feed(t, m, ToolStartMsg{ID: "call_f", Name: name})
	m, _ = feed(t, m, ToolResultMsg{ID: "call_f", Name: name, Content: `{"error":true,"message":"` + errText + `"}`, IsError: true})
	return m
}

// lineWith is the frame's first row containing s.
func lineWith(frame, s string) string {
	for _, l := range strings.Split(frame, "\n") {
		if strings.Contains(l, s) {
			return l
		}
	}
	return ""
}

// #398 T2: a failed call shows ✗ in the Ctrl+K log, not ✓.
func TestCtrlKLogShowsFailedCallsAsFailed(t *testing.T) {
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			m := newAuditApp(t, &fakeCompactClient{}, sz.w, sz.h)
			m = failedToolTurn(t, m, "fail_tool", "boom")
			m = pressCtrlK(m)
			frame := auditView(m)
			assertFrameFits(t, frame, sz.w, sz.h)
			row := lineWith(frame, "fail_tool (")
			require.NotEmpty(t, row, "the call log row is on screen:\n%s", frame)
			assert.Contains(t, row, "✗")
			assert.NotContains(t, row, "✓")
			assert.Contains(t, frame, "→ Error: boom")
		})
	}
}

// #398 T3: a failed call's ⚙ status row shows ✗, never ✓.
func TestSkillsRowShowsFailedCallWithCross(t *testing.T) {
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			m := newAuditApp(t, &fakeCompactClient{}, sz.w, sz.h)
			m = failedToolTurn(t, m, "mcp__stub__fail", "disk full")
			frame := auditView(m)
			assertFrameFits(t, frame, sz.w, sz.h)
			row := lineWith(frame, "⚙ mcp__stub__fail")
			require.NotEmpty(t, row, "the ⚙ row is on screen:\n%s", frame)
			assert.Contains(t, row, "✗")
			assert.NotContains(t, row, "✓")
			assert.Contains(t, row, "disk full")
		})
	}
}

// #398 C2: a long error fits the terminal and ends with … at both sizes.
func TestSkillsErrorRowFitsWithEllipsis(t *testing.T) {
	long := strings.Repeat("fatal: first line of the failure second line ", 6)
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			m := newAuditApp(t, &fakeCompactClient{}, sz.w, sz.h)
			m = failedToolTurn(t, m, "mcp__stub__rpcfail", long)
			frame := auditView(m)
			assertFrameFits(t, frame, sz.w, sz.h)
			row := strings.TrimRight(lineWith(frame, "⚙ mcp__stub__rpcfail"), " ")
			require.NotEmpty(t, row, "the ⚙ row is on screen:\n%s", frame)
			assert.True(t, strings.HasSuffix(row, "…"), "row %q must end with …", row)
			assert.LessOrEqual(t, lipgloss.Width(row), sz.w)
		})
	}
}

func resize(t *testing.T, m AppModel, w, h int) AppModel {
	t.Helper()
	m, _ = step(t, m, tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

// #398 C1: the ⚙ row of the last call does not carry over into the session
// /clear starts.
func TestClearResetsSkillsRow(t *testing.T) {
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			m, _ := newCompactTestApp(t)
			mgr := &diskSessions{mgr: config.NewSessionManager()}
			m = m.SetSessionManager(mgr, mgr.mgr.NewSession())
			m = resize(t, m, sz.w, sz.h)
			m = runToolTurn(t, m)
			m, _ = feed(t, m, TurnDoneMsg{Stop: "done"})
			require.Contains(t, auditView(m), "⚙ tool_a ✓")

			m, _ = step(t, m, SendMessageMsg{Content: "/clear"})
			frame := auditView(m)
			assertFrameFits(t, frame, sz.w, sz.h)
			assert.NotContains(t, frame, "⚙ tool_a")
			assert.Contains(t, frame, "New session created")
		})
	}
}

// #398 C1: nor into the session /handoff starts.
func TestHandoffResetsSkillsRow(t *testing.T) {
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			m, ready := startTestHandoff(t, nil)
			m = resize(t, m, sz.w, sz.h)
			require.Contains(t, auditView(m), "⚙ tool_a ✓")

			m, _ = step(t, m, ready)
			frame := auditView(m)
			assertFrameFits(t, frame, sz.w, sz.h)
			assert.Contains(t, frame, "New session started")
			assert.NotContains(t, frame, "⚙ tool_a")
		})
	}
}
