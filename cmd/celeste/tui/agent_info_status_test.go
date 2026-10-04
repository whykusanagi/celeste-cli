package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// statusBarRow is the frame's last row: the status bar.
func statusBarRow(frame string) string {
	lines := strings.Split(strings.TrimRight(frame, "\n"), "\n")
	return lines[len(lines)-1]
}

// N5: /agent list-runs and /agent help run nothing, so the status bar must
// not say "Agent run complete" after them. A goal run still does.
func TestAgentInfoCommandsDoNotReportARunAt80And120(t *testing.T) {
	for _, sz := range auditSizes {
		for _, sub := range []string{"list-runs", "help"} {
			t.Run(sz.name+"/"+sub, func(t *testing.T) {
				m := newAuditApp(t, &fakeAgentLLMClient{}, sz.w, sz.h)
				m = auditSend(t, m, "/agent "+sub)
				m, _ = step(t, m, AgentCommandResultMsg{Output: "No agent runs yet.", AgentRun: m.agentRun})
				frame := auditView(m)
				assertFrameFits(t, frame, sz.w, sz.h)
				bar := statusBarRow(frame)
				assert.NotContains(t, bar, "Agent run complete")
				assert.Contains(t, bar, "Ready")
				assert.False(t, m.streaming)
			})
		}
	}
	m := newAuditApp(t, &fakeAgentLLMClient{}, 120, 40)
	m = auditSend(t, m, "/agent fix the tests")
	m, _ = step(t, m, AgentCommandResultMsg{Output: "done", AgentRun: m.agentRun})
	assert.Contains(t, statusBarRow(auditView(m)), "Agent run complete")
}
