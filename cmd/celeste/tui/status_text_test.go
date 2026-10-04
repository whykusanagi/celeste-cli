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

// A Venice media generation runs without m.streaming; a command sent while it
// is in flight must not reset its progress text to Ready (V17 review).
func TestMediaGenerationStatusSurvivesNextCommand(t *testing.T) {
	m := newAuditApp(t, &fakeToolLLMClient{}, 120, 40)
	m.nsfwMode = true
	m = auditSend(t, m, "image: a lighthouse at dusk")
	if !m.mediaInFlight {
		t.Fatal("media command did not mark generation in flight")
	}
	m = auditSend(t, m, "/costs")
	if row := lastRow(auditView(m)); !strings.Contains(row, "generation in progress") {
		t.Errorf("status row = %q, want the generation progress text", row)
	}
	updated, _ := m.Update(MediaResultMsg{Success: true, MediaType: "image", URL: "https://example.invalid/x.png"})
	m = updated.(AppModel)
	if m.mediaInFlight {
		t.Error("media result did not clear the in-flight flag")
	}
	m = auditSend(t, m, "/costs")
	if row := lastRow(auditView(m)); !strings.Contains(row, "Ready") {
		t.Errorf("after the result status row = %q, want Ready", row)
	}
}
