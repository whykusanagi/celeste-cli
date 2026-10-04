package tui

import (
	"strings"
	"testing"
)

// statusRow returns the status-line row of the composed frame: the row just
// above the key hints.
func statusRow(t *testing.T, frame string) string {
	t.Helper()
	lines := strings.Split(frame, "\n")
	for i, l := range lines {
		if strings.Contains(l, "/ commands") || strings.Contains(l, "esc close") || strings.Contains(l, "esc interrupt") {
			if i == 0 {
				break
			}
			return strings.TrimRight(lines[i-1], " ")
		}
	}
	t.Fatalf("no key-hint row in frame:\n%s", frame)
	return ""
}

// V1: the skills segment reflects the tools actually offered, not a copy of
// the skills panel that only View() ever configured.
func TestStatusLineSkillsSegmentShowsOfferedTools(t *testing.T) {
	skills := []SkillDefinition{{Name: "read_file"}, {Name: "write_file"}, {Name: "bash"}}
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			m := newAuditApp(t, &fakeToolLLMClient{skills: skills}, sz.w, sz.h)
			m.skillsEnabled = true
			updated, _ := m.Update(GitStatusMsg{Repo: false})
			m = updated.(AppModel)

			frame := auditView(m)
			row := statusRow(t, frame)
			if !strings.Contains(row, "⚙ 3") || strings.Contains(row, "⚙ off") {
				t.Errorf("status row = %q, want ⚙ 3", row)
			}
			assertFrameFits(t, frame, sz.w, sz.h)

			// Tools withdrawn (NSFW) shows off without waiting for a resync.
			m.nsfwMode = true
			if row := statusRow(t, auditView(m)); !strings.Contains(row, "⚙ off") {
				t.Errorf("NSFW status row = %q, want ⚙ off", row)
			}
		})
	}
}
