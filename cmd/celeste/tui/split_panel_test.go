package tui

import (
	"strings"
	"testing"

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
