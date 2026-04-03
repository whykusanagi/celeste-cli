package hooks

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/grimoire"
)

func TestParseFromGrimoire_Nil(t *testing.T) {
	hooks := ParseFromGrimoire(nil)
	assert.Nil(t, hooks)
}

func TestParseFromGrimoire_Empty(t *testing.T) {
	g := &grimoire.Grimoire{}
	hooks := ParseFromGrimoire(g)
	assert.Empty(t, hooks)
}

func TestParseFromGrimoire_Basic(t *testing.T) {
	g := &grimoire.Grimoire{
		Hooks: []grimoire.HookEntry{
			{Phase: "PreToolUse", ToolName: "bash", Command: "echo pre-bash"},
			{Phase: "PostToolUse", ToolName: "", Command: "echo post-all"},
		},
	}

	hooks := ParseFromGrimoire(g)
	require.Len(t, hooks, 2)

	assert.Equal(t, "PreToolUse", hooks[0].Event)
	assert.Equal(t, "bash", hooks[0].Tool)
	assert.Equal(t, "echo pre-bash", hooks[0].Command)
	assert.Equal(t, defaultHookTimeout, hooks[0].Timeout)

	assert.Equal(t, "PostToolUse", hooks[1].Event)
	assert.Equal(t, "*", hooks[1].Tool) // empty tool becomes wildcard
	assert.Equal(t, "echo post-all", hooks[1].Command)
}
