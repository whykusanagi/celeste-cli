package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
