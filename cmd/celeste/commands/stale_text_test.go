package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// W-H3: /safe returns to the endpoint used before /nsfw, not to OpenAI.
func TestNSFWHelpSafeLineNamesNoOpenAI(t *testing.T) {
	res := Execute(&Command{Name: "help"}, &CommandContext{NSFWMode: true})
	assert.NotContains(t, res.Message, "Return to safe mode (OpenAI)")
}
