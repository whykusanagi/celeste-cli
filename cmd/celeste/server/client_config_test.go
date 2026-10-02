package server

import (
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// The Google backend sends no system prompt at all when SkipPersonaPrompt is
// set, and the server's prompts (content format, chat) are not the persona:
// the user's skip_persona_prompt must not strip them.
func TestServerClientsKeepTheirSystemPromptWhenPersonaIsSkipped(t *testing.T) {
	cfg := &config.Config{BaseURL: "https://generativelanguage.googleapis.com/v1", Model: "m", SkipPersonaPrompt: true}
	if serverClientConfig(cfg).SkipPersonaPrompt {
		t.Error("serverClientConfig must not carry SkipPersonaPrompt")
	}
	c := newChatClient(cfg, tools.NewRegistry(), "system")
	if c.GetConfig().SkipPersonaPrompt {
		t.Error("the MCP chat client carries SkipPersonaPrompt")
	}
}
