package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
)

func TestSplitPanelAddActionEntry(t *testing.T) {
	p := NewSplitPanel(80, 24)
	p.AddAction("classified: code (0.94)")
	p.AddAction("reading main.go")
	assert.Len(t, p.Actions(), 2)
	assert.Equal(t, "classified: code (0.94)", p.Actions()[0])
}

func TestSplitPanelSetDiff(t *testing.T) {
	p := NewSplitPanel(80, 24)
	p.SetDiff("main.go", "@@ -1,3 +1,5 @@\n+func foo() {}")
	assert.Equal(t, "main.go", p.DiffFile())
	assert.Contains(t, p.DiffContent(), "func foo")
}

func TestSplitPanelRenderHasBothPanels(t *testing.T) {
	p := NewSplitPanel(100, 30)
	p.AddAction("doing something")
	p.SetDiff("foo.go", "- old\n+ new")
	rendered := p.View()
	assert.True(t, strings.Contains(rendered, "doing something"), "left panel missing")
	assert.True(t, strings.Contains(rendered, "foo.go"), "right panel missing")
}

func TestSplitPanelCapsActionsAt200(t *testing.T) {
	p := NewSplitPanel(80, 24)
	for i := 0; i < 250; i++ {
		p.AddAction("entry")
	}
	assert.LessOrEqual(t, len(p.Actions()), 200)
}

// V21: the panel renders exactly width x height, whatever it holds: wide
// runes, long lines, a multi-line verdict, a long diff.
func TestSplitPanelViewIsExactlyItsSize(t *testing.T) {
	fill := func(p *SplitPanel) {
		p.AddAction("🔍 [reviewer] " + strings.Repeat("wide 漢字 ", 20))
		for i := 0; i < 60; i++ {
			p.AddAction("entry")
		}
	}
	contents := map[string]func(p *SplitPanel){
		"output": func(p *SplitPanel) { p.SetOutput(strings.Repeat("🙂 output line that is long enough to wrap\n", 50)) },
		"diff": func(p *SplitPanel) {
			p.SetDiff("main.go", strings.Repeat("+ added 漢字 line that is long enough to wrap twice over\n", 80))
		},
		"verdict": func(p *SplitPanel) {
			p.SetVerdict(strings.Repeat("✓ approved with a verdict line long enough to wrap\n", 50))
		},
		"empty": func(p *SplitPanel) {},
	}
	for name, set := range contents {
		for _, sz := range [][2]int{{120, 26}, {80, 9}, {41, 5}, {200, 60}} {
			p := NewSplitPanel(sz[0], sz[1])
			fill(p)
			set(p)
			lines := strings.Split(p.View(), "\n")
			if len(lines) != sz[1] {
				t.Errorf("%s %v: %d rows", name, sz, len(lines))
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w != sz[0] {
					t.Errorf("%s %v: row %d is %d wide", name, sz, i, w)
				}
			}
		}
	}
}

// Under 40 columns the feed alone is shown: an entry holding newlines (a
// multi-line verdict) must not make it taller than the panel.
func TestSplitPanelNarrowCapsRowsNotEntries(t *testing.T) {
	p := NewSplitPanel(30, 4)
	p.AddAction("first")
	p.AddAction("verdict line 1\nverdict line 2\nverdict line 3")
	p.AddAction("last")
	lines := strings.Split(p.View(), "\n")
	if len(lines) != 4 {
		t.Fatalf("narrow view is %d rows, want 4: %q", len(lines), lines)
	}
	if !strings.Contains(lines[3], "last") || !strings.Contains(lines[0], "verdict line 1") {
		t.Errorf("narrow view = %q, want the latest 4 rows", lines)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > 30 {
			t.Errorf("row %d is %d wide", i, w)
		}
	}
}

// leftPane returns the left pane's inner rows of a rendered split panel,
// borders and padding stripped.
func leftPane(p *SplitPanel) []string {
	var out []string
	lines := strings.Split(ansi.Strip(p.View()), "\n")
	for _, l := range lines[1 : len(lines)-1] {
		r := []rune(l)
		out = append(out, strings.TrimSpace(string(r[1:p.width/2-1])))
	}
	return out
}

func paneHas(rows []string, s string) bool {
	for _, r := range rows {
		if strings.Contains(r, s) {
			return true
		}
	}
	return false
}

// #353: when the whole feed fits, PgUp has nothing to show: no scroll
// markers, and every entry stays on screen.
func TestSplitPanelPgUpWithShortFeedKeepsEntries(t *testing.T) {
	p := NewSplitPanel(100, 30)
	for i := 0; i < 6; i++ {
		p.AddAction(fmt.Sprintf("entry %d", i))
	}
	p.ScrollUp(5)
	rows := leftPane(p)
	for i := 0; i < 6; i++ {
		assert.True(t, paneHas(rows, fmt.Sprintf("entry %d", i)), "entry %d hidden after PgUp: %q", i, rows)
	}
	assert.False(t, paneHas(rows, "older"), "scroll marker over a feed that fits: %q", rows)
	assert.False(t, paneHas(rows, "newer"), "scroll marker over a feed that fits: %q", rows)
	assert.True(t, p.AtBottom())
}

// #353: paging a long feed shows a full pane of older entries, and the
// markers count what is really hidden above and below.
func TestSplitPanelPgUpShowsOlderEntriesAndTrueCounts(t *testing.T) {
	p := NewSplitPanel(100, 12) // inner rows 10: header, 8 entries, marker
	for i := 0; i < 40; i++ {
		p.AddAction(fmt.Sprintf("entry %02d", i))
	}
	p.ScrollUp(5)
	rows := leftPane(p)
	for i := 27; i <= 34; i++ {
		assert.True(t, paneHas(rows, fmt.Sprintf("entry %02d", i)), "entry %02d missing: %q", i, rows)
	}
	assert.False(t, paneHas(rows, "entry 35"), "%q", rows)
	assert.True(t, paneHas(rows, "↑ 27 older"), "marker: %q", rows)
	assert.True(t, paneHas(rows, "↓ 5 newer"), "marker: %q", rows)

	// Paging past the top stops at the first page, still full.
	p.ScrollUp(500)
	rows = leftPane(p)
	for i := 0; i <= 7; i++ {
		assert.True(t, paneHas(rows, fmt.Sprintf("entry %02d", i)), "entry %02d missing at the top: %q", i, rows)
	}
	assert.False(t, paneHas(rows, "older"), "nothing is older than the first page: %q", rows)
	assert.True(t, paneHas(rows, "↓ 32 newer"), "marker: %q", rows)

	// One PgDn from the top moves back down at once.
	p.ScrollDown(5)
	rows = leftPane(p)
	assert.True(t, paneHas(rows, "entry 12"), "%q", rows)
	assert.True(t, paneHas(rows, "↓ 27 newer"), "%q", rows)
}

// #353: an entry spanning several lines counts its lines, so the page and
// the markers stay right.
func TestSplitPanelScrollCountsMultiLineEntries(t *testing.T) {
	p := NewSplitPanel(100, 12)
	for i := 0; i < 20; i++ {
		p.AddAction(fmt.Sprintf("entry %02d", i))
	}
	p.AddAction("verdict a\nverdict b\nverdict c")
	rows := leftPane(p)
	assert.True(t, paneHas(rows, "verdict c"), "latest lines missing: %q", rows)
	assert.True(t, paneHas(rows, "↑ 15 older"), "marker: %q", rows)
	assert.Len(t, rows, 10)
}

// #353 end to end: PgUp in the /orch split view with a short feed keeps
// the feed on screen and shows no scroll markers.
func TestOrchSplitViewPgUpShortFeed(t *testing.T) {
	m := NewApp(&fakeToolLLMClient{})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(AppModel)
	m.splitPanelMode = true
	m.splitPanel = NewSplitPanel(120, 30)
	for i := 0; i < 6; i++ {
		m.splitPanel.AddAction(fmt.Sprintf("lane step %d", i))
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	view := ansi.Strip(updated.(AppModel).View())
	for i := 0; i < 6; i++ {
		assert.Contains(t, view, fmt.Sprintf("lane step %d", i))
	}
	assert.NotContains(t, view, "older")
	assert.NotContains(t, view, "newer")
}

// rightPane returns the right pane's inner rows of a rendered split panel.
func rightPane(p *SplitPanel) []string {
	var out []string
	lines := strings.Split(ansi.Strip(p.View()), "\n")
	for _, l := range lines[1 : len(lines)-1] {
		r := []rune(l)
		out = append(out, strings.TrimSpace(string(r[p.width/2+1:len(r)-1])))
	}
	return out
}

// #353 (right pane): the "line x-y / n" marker promises more of a long
// diff; paging the right pane must show it, and stop at the last page.
func TestSplitPanelRightPanePagesLongDiff(t *testing.T) {
	p := NewSplitPanel(100, 12) // inner rows 10: header, 8 lines, marker
	var diff []string
	for i := 0; i < 30; i++ {
		diff = append(diff, fmt.Sprintf("+ line %02d", i))
	}
	p.SetDiff("main.go", strings.Join(diff, "\n"))
	rows := rightPane(p)
	assert.True(t, paneHas(rows, "line 1-8 / 30"), "%q", rows)

	p.ScrollRight(8)
	rows = rightPane(p)
	assert.True(t, paneHas(rows, "+ line 08"), "%q", rows)
	assert.True(t, paneHas(rows, "line 9-16 / 30"), "%q", rows)

	p.ScrollRight(500)
	rows = rightPane(p)
	assert.True(t, paneHas(rows, "+ line 22"), "the last page is full: %q", rows)
	assert.True(t, paneHas(rows, "line 23-30 / 30"), "%q", rows)

	p.ScrollRight(-500)
	assert.True(t, paneHas(rightPane(p), "line 1-8 / 30"))
}

// ctrl+↓ / ctrl+↑ page the right pane in the /orch split view.
func TestOrchSplitViewCtrlArrowsPageRightPane(t *testing.T) {
	m := NewApp(&fakeToolLLMClient{})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(AppModel)
	m.splitPanelMode = true
	m.splitPanel = NewSplitPanel(120, 30)
	var diff []string
	for i := 0; i < 200; i++ {
		diff = append(diff, fmt.Sprintf("+ row %03d", i))
	}
	m.splitPanel.SetDiff("main.go", strings.Join(diff, "\n"))
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlDown})
	m = updated.(AppModel)
	assert.Positive(t, m.splitPanel.rightScroll)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlUp})
	assert.Zero(t, updated.(AppModel).splitPanel.rightScroll)
}
