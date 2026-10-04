package tui

import (
	"strings"
	"testing"
)

// lastRow is the status bar: the frame's bottom row.
func lastRow(frame string) string {
	lines := strings.Split(frame, "\n")
	return strings.TrimRight(lines[len(lines)-1], " ")
}

// V17: a status text such as "Selection cancelled" or "Model changed to …"
// belongs to the command that set it; the next command clears it.
func TestStaleStatusTextClearsOnNextCommand(t *testing.T) {
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			m := newAuditApp(t, &fakeToolLLMClient{}, sz.w, sz.h)
			for _, stale := range []string{"Selection cancelled", "Model changed to: grok-4"} {
				m.status = m.status.SetText(stale)
				if row := lastRow(auditView(m)); !strings.Contains(row, stale) {
					t.Fatalf("status row = %q, want %q", row, stale)
				}
				m = auditSend(t, m, "/costs")
				frame := auditView(m)
				if row := lastRow(frame); strings.Contains(row, stale) || !strings.Contains(row, "Ready") {
					t.Errorf("after /costs status row = %q, want Ready", row)
				}
				assertFrameFits(t, frame, sz.w, sz.h)
			}
		})
	}
}
