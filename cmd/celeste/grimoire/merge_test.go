package grimoire

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMerge_Empty(t *testing.T) {
	result := Merge()
	assert.True(t, result.IsEmpty())
	assert.NotNil(t, result.RawSections)
}

func TestMerge_Single(t *testing.T) {
	g := &Grimoire{
		Sources:  []string{"/repo/.grimoire"},
		Bindings: []string{"Go project"},
		Rituals:  []string{"Always test"},
	}
	result := Merge(g)
	assert.Equal(t, []string{"Go project"}, result.Bindings)
	assert.Equal(t, []string{"Always test"}, result.Rituals)
}

func TestMerge_PriorityOrder(t *testing.T) {
	global := &Grimoire{
		Sources:  []string{"~/.celeste/grimoire.md"},
		Bindings: []string{"global binding"},
		Rituals:  []string{"global ritual"},
	}
	project := &Grimoire{
		Sources:  []string{"/repo/.grimoire"},
		Bindings: []string{"project binding"},
		Wards:    []string{"protect secrets.go"},
	}
	local := &Grimoire{
		Sources:  []string{"/repo/.grimoire.local"},
		Bindings: []string{"local binding"},
	}

	result := Merge(global, project, local)
	assert.Len(t, result.Bindings, 3)
	assert.Equal(t, "global binding", result.Bindings[0])
	assert.Equal(t, "project binding", result.Bindings[1])
	assert.Equal(t, "local binding", result.Bindings[2])
	assert.Len(t, result.Rituals, 1)
	assert.Len(t, result.Wards, 1)
	assert.Len(t, result.Sources, 3)
}

func TestMerge_NilEntries(t *testing.T) {
	g := &Grimoire{
		Bindings: []string{"valid"},
	}
	result := Merge(nil, g, nil)
	assert.Equal(t, []string{"valid"}, result.Bindings)
}

func TestMerge_RawSectionsOverride(t *testing.T) {
	g1 := &Grimoire{
		RawSections: map[string]string{"Custom": "from g1"},
	}
	g2 := &Grimoire{
		RawSections: map[string]string{"Custom": "from g2"},
	}
	result := Merge(g1, g2)
	assert.Equal(t, "from g2", result.RawSections["Custom"])
}

func TestMerge_HooksAppended(t *testing.T) {
	g1 := &Grimoire{
		Hooks: []HookEntry{{Phase: "PreToolUse", ToolName: "bash", Command: "vet"}},
	}
	g2 := &Grimoire{
		Hooks: []HookEntry{{Phase: "PostToolUse", ToolName: "write_file", Command: "fmt"}},
	}
	result := Merge(g1, g2)
	assert.Len(t, result.Hooks, 2)
}
