package grimoire

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGrimoire_Empty(t *testing.T) {
	g := &Grimoire{}
	assert.Empty(t, g.Bindings)
	assert.Empty(t, g.Rituals)
	assert.Empty(t, g.Incantations)
	assert.Empty(t, g.Wards)
	assert.Empty(t, g.Hooks)
	assert.Equal(t, "", g.Render())
}

func TestGrimoire_Render(t *testing.T) {
	g := &Grimoire{
		Bindings: []string{"This is a Go project"},
		Rituals:  []string{"Always run tests before committing"},
	}
	rendered := g.Render()
	assert.Contains(t, rendered, "Bindings")
	assert.Contains(t, rendered, "This is a Go project")
	assert.Contains(t, rendered, "Rituals")
	assert.Contains(t, rendered, "Always run tests before committing")
}

func TestGrimoire_TotalSize(t *testing.T) {
	g := &Grimoire{
		Bindings: []string{"short"},
	}
	assert.Greater(t, g.TotalSize(), 0)
}

func TestGrimoire_IsEmpty(t *testing.T) {
	g := &Grimoire{}
	assert.True(t, g.IsEmpty())

	g.Bindings = []string{"something"}
	assert.False(t, g.IsEmpty())
}

func TestGrimoire_RenderIncantations(t *testing.T) {
	g := &Grimoire{
		Incantations: []IncludeRef{
			{Path: "@./docs/ARCH.md", Content: "# Architecture\nSome content"},
			{Path: "@./missing.md", Error: "file not found"},
		},
	}
	rendered := g.Render()
	assert.Contains(t, rendered, "Architecture")
	assert.Contains(t, rendered, "file not found")
}

func TestGrimoire_RenderWards(t *testing.T) {
	g := &Grimoire{
		Wards: []string{"Do not modify secrets.go"},
	}
	rendered := g.Render()
	assert.Contains(t, rendered, "Wards")
	assert.Contains(t, rendered, "Do not modify secrets.go")
}
