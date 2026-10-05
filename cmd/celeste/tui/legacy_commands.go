package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/commands"
)

// legacyCommands are the bare words (no slash) the chat runs as commands,
// kept for backward compatibility; the quit words are isQuitWord's. It is
// the one list: the chat dispatches these, and isLegacyTextCommand holds
// them back during a handoff.
var legacyCommands = map[string]func(AppModel) (AppModel, tea.Cmd){
	"clear":  AppModel.legacyClear,
	"help":   AppModel.legacyHelp,
	"tools":  AppModel.legacySkills,
	"skills": AppModel.legacySkills,
	"debug":  AppModel.legacyDebug,
}

func (m AppModel) legacyClear() (AppModel, tea.Cmd) {
	m.chat = m.chat.Clear()
	m.untrackPlan()
	m.status = m.status.SetText("Chat cleared")
	return m, nil
}

// legacyHelp uses the context-aware /help command instead of a static text.
func (m AppModel) legacyHelp() (AppModel, tea.Cmd) {
	helpCmd := &commands.Command{Name: "help"}
	ctx := &commands.CommandContext{NSFWMode: m.nsfwMode}
	result := commands.Execute(helpCmd, ctx)
	if result.Success {
		m.chat = m.chat.AddSystemMessage(result.Message)
	}
	return m, nil
}

// legacySkills switches to the interactive skills browser.
func (m AppModel) legacySkills() (AppModel, tea.Cmd) {
	m.viewMode = "skills"
	skillsList := []SkillDefinition{}
	if m.llmClient != nil {
		skillsList = m.llmClient.GetSkills()
	}
	model := NewSkillsBrowserModel(skillsList)
	model.width, model.height = m.width, m.height
	m.skillsBrowser = &model
	return m, m.skillsBrowser.Init()
}

// legacyDebug shows the tools/skills debug info.
func (m AppModel) legacyDebug() (AppModel, tea.Cmd) {
	skills := m.getAvailableSkills()
	debugMsg := fmt.Sprintf("📋 Available Tools (%d):\n", len(skills))
	for _, s := range skills {
		debugMsg += fmt.Sprintf("  • %s: %s\n", s.Name, s.Description)
	}
	debugMsg += "\n⚠️  Tool calls need a model and endpoint that support tool calling.\n"
	debugMsg += fmt.Sprintf("\nLog file: %s", GetLogPath())
	m.chat = m.chat.AddSystemMessage(debugMsg)
	return m, nil
}
