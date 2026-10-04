package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
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

// V6: at 80 columns and up, a long session name is truncated so the status
// line stays one row and the layout keeps its line.
func TestStatusLineNeverWrapsWithLongSession(t *testing.T) {
	long := "fork of 1759512345678901234 with a very long descriptive session name"
	for _, w := range []int{80, 100, 120} {
		sl := NewStatusLineModel().SetWidth(w).
			SetGit("fix/2.0-tui-markdown-statusline", 3, 2, 1).SetProject("celeste-cli").
			SetModel("fugu").SetSkills(true, 47).SetEffort("high").SetPermMode("default").
			SetPlan(true).SetSession(long)
		out := stripANSI(sl.View())
		if h := lipgloss.Height(out); h != 1 {
			t.Errorf("width %d: status line is %d rows, want 1:\n%s", w, h, out)
		}
		if got := lipgloss.Width(out); got > w {
			t.Errorf("width %d: status line is %d cells", w, got)
		}
		if !strings.Contains(out, "fugu") {
			t.Errorf("width %d: model segment dropped: %q", w, out)
		}
	}
	// A name that fits is shown whole.
	out := stripANSI(NewStatusLineModel().SetWidth(120).SetModel("fugu").SetSession("night-session").View())
	if !strings.Contains(out, "night-session") || strings.Contains(out, "…") {
		t.Errorf("short session altered: %q", out)
	}
}

func TestStatusLineFrameKeepsRowsWithLongSession(t *testing.T) {
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			m := newAuditApp(t, &fakeToolLLMClient{}, sz.w, sz.h)
			m.effort = "high"
			s := &config.Session{Name: "fork of 1759512345678901234 with a very long descriptive session name that keeps going"}
			m.currentSession = s
			updated, _ := m.Update(GitStatusMsg{Repo: true, Branch: "fix/2.0-tui-markdown-statusline", Dirty: 3, Ahead: 2, Behind: 1})
			m = updated.(AppModel)
			frame := auditView(m)
			if n := len(strings.Split(frame, "\n")); n != sz.h {
				t.Errorf("frame has %d rows, want %d:\n%s", n, sz.h, frame)
			}
			assertFrameFits(t, frame, sz.w, sz.h)
			if row := statusRow(t, frame); !strings.Contains(row, "fugu") {
				t.Errorf("status row = %q", row)
			}
		})
	}
}

func TestStatusLineNarrowNeverWraps(t *testing.T) {
	out := stripANSI(NewStatusLineModel().SetWidth(40).
		SetGit("feature/a-really-long-branch-name-that-goes-on", 12, 3, 4).
		SetModel("some-long-model-name").SetSession("night").View())
	if h := lipgloss.Height(out); h != 1 {
		t.Errorf("narrow status line is %d rows, want 1:\n%s", h, out)
	}
}
