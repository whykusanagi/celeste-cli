package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
)

// A skill name long enough to fill the row must not push the ✓ or ✗ off
// the end: the row would show a name but not whether the call worked.
func TestSkillsStatusRowKeepsGlyphWhenNameFillsRow(t *testing.T) {
	name := "mcp__" + strings.Repeat("server", 8) + "__" + strings.Repeat("tool", 10)
	for _, w := range []int{20, 40, 80} {
		s := NewSkillsModel()
		s.width = w
		done := strings.TrimRight(ansi.Strip(s.SetCompleted(name).collapsedView()), " ")
		assert.LessOrEqual(t, lipgloss.Width(done), w, "completed row %q", done)
		assert.True(t, strings.HasSuffix(done, "✓"), "w=%d completed row %q must end with ✓", w, done)
		assert.Contains(t, done, "…", "w=%d completed row %q must show the cut", w, done)

		failed := strings.TrimRight(ansi.Strip(s.SetError(name, errors.New("connection refused")).collapsedView()), " ")
		assert.LessOrEqual(t, lipgloss.Width(failed), w, "failed row %q", failed)
		assert.Contains(t, failed, "✗", "w=%d failed row %q must keep ✗", w, failed)
		assert.NotContains(t, failed, "✓")
	}
}

// Short names keep the original layout.
func TestSkillsStatusRowShortNameUnchanged(t *testing.T) {
	s := NewSkillsModel()
	s.width = 80
	assert.Equal(t, " ⚙ read_file ✓", strings.TrimRight(ansi.Strip(s.SetCompleted("read_file").collapsedView()), " "))
	assert.Equal(t, " ⚙ read_file ✗ no such file", strings.TrimRight(ansi.Strip(s.SetError("read_file", errors.New("no such file")).collapsedView()), " "))
}

// The whole chat at 80x24 with a failed long-named tool call: the status
// row fits and keeps its ✗ (snapshot in testdata).
func TestSkillsStatusRowSnapshot80x24(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	name := "mcp__" + strings.Repeat("server", 8) + "__" + strings.Repeat("tool", 10)
	for _, tc := range []struct {
		golden string
		set    func(SkillsModel) SkillsModel
		glyph  string
	}{
		{"skills_failed_80x24.golden", func(s SkillsModel) SkillsModel { return s.SetError(name, errors.New("connection refused")) }, "✗"},
		{"skills_completed_80x24.golden", func(s SkillsModel) SkillsModel { return s.SetCompleted(name) }, "✓"},
	} {
		t.Run(tc.golden, func(t *testing.T) {
			var tm tea.Model = NewApp(nil)
			tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			m := tm.(AppModel)
			m.skills = tc.set(m.skills)
			plain := ansi.Strip(m.View())
			lines := strings.Split(plain, "\n")
			assert.LessOrEqual(t, len(lines), 24)
			found := false
			for i, l := range lines {
				assert.LessOrEqual(t, ansi.StringWidth(l), 80, "row %d %q", i, l)
				if strings.Contains(l, "⚙ mcp__") {
					found = true
					assert.Contains(t, l, tc.glyph, "status row %q", l)
				}
			}
			assert.True(t, found, fmt.Sprintf("no status row in:\n%s", plain))
			checkSplitGolden(t, tc.golden, plain)
		})
	}
}
