package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// W-D1: the slash commands the TUI handles itself have no dead fallbacks
// here; /skills reload and /skills info could never reach them.
func TestExecuteHasNoTUIOnlyStubs(t *testing.T) {
	for _, name := range []string{"tools", "skills", "menu", "context", "stats", "export"} {
		res := Execute(&Command{Name: name, Args: []string{"reload"}}, &CommandContext{})
		assert.NotContains(t, res.Message, "requires app context", name)
		assert.Contains(t, res.Message, "Unknown command", name)
	}
}
