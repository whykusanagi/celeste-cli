package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// W-S1: /agent list-runs and help only report; the chat used to announce
// "🤖 Agent running: list-runs" before the list. A goal and resume, which
// run the agent, still get the banner.
func TestAgentInfoCommandsShowNoRunningBanner(t *testing.T) {
	for _, args := range []string{"list-runs", "list", "--list-runs", "help", "-h", "--help"} {
		client := &fakeAgentLLMClient{}
		m := NewApp(client)
		model, _ := m.Update(SendMessageMsg{Content: "/agent " + args})
		m = model.(AppModel)
		require.Len(t, client.agentArgs, 1, args)
		assert.False(t, hasSystemMessageContaining(m.chat.GetMessages(), "Agent running"), "/agent %s announced a run", args)
	}

	for _, args := range []string{"fix flaky tests", "resume 123"} {
		client := &fakeAgentLLMClient{}
		m := NewApp(client)
		model, _ := m.Update(SendMessageMsg{Content: "/agent " + args})
		m = model.(AppModel)
		assert.True(t, hasSystemMessageContaining(m.chat.GetMessages(), "Agent running: "+args), args)
	}
}

// W-M1: /menu lists the 2.0 commands, and every item it sends is a slash
// command the chat knows (selecting one sends "/<name>" with no arguments;
// exit quits).
func TestMenuListsKnownAnd20Commands(t *testing.T) {
	var names []string
	for _, it := range menuItems {
		names = append(names, it.Name)
		if it.Name == "exit" {
			continue
		}
		assert.Contains(t, knownCommands, it.Name, "/menu offers /%s, which the chat does not know", it.Name)
	}
	// The menu does not scroll: the header (2 rows), the items and the
	// footer (2 rows) fit a 24-row terminal.
	assert.LessOrEqual(t, len(menuItems)+4, 24)
	for _, want := range []string{"plan", "diff", "fork", "grimoire", "compact", "handoff", "mcp", "persona", "memories", "costs"} {
		assert.Contains(t, names, want, "/menu lacks /%s", want)
	}
	// One keypress sends the bare command, so the menu, where people
	// browse, offers nothing that changes files or takes back chat with no
	// confirmation; /help lists /undo and /rewind.
	for _, unwanted := range []string{"undo", "rewind"} {
		assert.NotContains(t, names, unwanted, "/menu offers /%s, which acts on one keypress", unwanted)
	}
}

// A menu item sends its slash command with no arguments. (exit quits
// directly; exit_and_typeahead_test covers it, #314.)
func TestMenuItemSendsSlashCommand(t *testing.T) {
	m := NewApp(&fakeAgentLLMClient{})
	m.viewMode = "menu"
	menu := NewMenuModel()
	m.menuModel = &menu
	model, cmd := m.Update(menuItemSelectedMsg{command: "diff"})
	m = model.(AppModel)
	require.NotNil(t, cmd)
	assert.Equal(t, SendMessageMsg{Content: "/diff"}, cmd())
	assert.Equal(t, "chat", m.viewMode)
}

// W-D2: the debug text no longer names DigitalOcean GenAI Agents.
func TestDebugTextHasNoDigitalOcean(t *testing.T) {
	m := NewApp(&fakeAgentLLMClient{})
	model, _ := m.Update(SendMessageMsg{Content: "debug"})
	m = model.(AppModel)
	msgs := m.chat.GetMessages()
	require.NotEmpty(t, msgs)
	assert.True(t, hasSystemMessageContaining(msgs, "Available Tools"))
	for _, msg := range msgs {
		assert.NotContains(t, msg.Content, "DigitalOcean")
	}
}

// W-H2: autocomplete offers /plan help, /list-models and /image-model. It
// does not offer /plan cancel, which only says the command is gone.
func TestAutocompleteOffersPlanHelpAndModelAliases(t *testing.T) {
	assert.Contains(t, computeSuggestions("/plan "), "plan help")
	assert.NotContains(t, computeSuggestions("/plan "), "plan cancel")
	assert.True(t, slices.Contains(computeSuggestions("/list"), "list-models"))
	assert.True(t, slices.Contains(computeSuggestions("/image"), "image-model"))
	for _, c := range knownCommands {
		assert.Equal(t, strings.TrimSpace(c), c)
	}
}
