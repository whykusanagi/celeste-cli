package tui

import (
	"strings"
	"testing"

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
