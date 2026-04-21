package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// SkillsModel renders lightweight runtime status for skill/tool usage in chat view.
type SkillsModel struct {
	currentInput string
	width        int
	height       int
	menuState    string

	endpoint       string
	model          string
	skillsEnabled  bool
	nsfw           bool
	skillsCount    int
	disabledReason string

	executingSkill string
	lastCompleted  string
	lastErrorSkill string
	lastError      string
	confirmMode    bool // show confirm/auto indicator
}

func NewSkillsModel() SkillsModel {
	return SkillsModel{}
}

func (s SkillsModel) SetCurrentInput(input string) SkillsModel {
	s.currentInput = input
	return s
}

func (s SkillsModel) SetSize(width, height int) SkillsModel {
	s.width = width
	s.height = height
	return s
}

func (s SkillsModel) SetMenuState(state string) SkillsModel {
	s.menuState = state
	return s
}

func (s SkillsModel) SetExecuting(name string) SkillsModel {
	s.executingSkill = name
	s.lastError = ""
	s.lastErrorSkill = ""
	return s
}

func (s SkillsModel) SetError(name string, err error) SkillsModel {
	s.executingSkill = ""
	s.lastErrorSkill = name
	if err != nil {
		s.lastError = err.Error()
	}
	return s
}

func (s SkillsModel) SetCompleted(name string) SkillsModel {
	s.executingSkill = ""
	s.lastCompleted = name
	s.lastError = ""
	s.lastErrorSkill = ""
	return s
}

func (s SkillsModel) SetConfig(endpoint, model string, enabled bool, nsfw bool, count int, reason string) SkillsModel {
	s.endpoint = endpoint
	s.model = model
	s.skillsEnabled = enabled
	s.nsfw = nsfw
	s.skillsCount = count
	s.disabledReason = reason
	return s
}

func (s SkillsModel) View() string {
	modeLabel := "auto"
	if s.confirmMode {
		modeLabel = "confirm"
	}
	lines := []string{
		skillsHeaderLine(s.skillsEnabled, s.skillsCount),
		fmt.Sprintf("Backend: %s | Model: %s | Mode: %s", safeLabel(s.endpoint), safeLabel(s.model), modeLabel),
	}

	if s.executingSkill != "" {
		lines = append(lines, SkillExecutingStyle.Render("Executing: "+s.executingSkill))
	} else if s.lastError != "" {
		lines = append(lines, SkillErrorStyle.Render("Last error ("+safeLabel(s.lastErrorSkill)+"): "+truncateLine(s.lastError, 80)))
	} else if s.lastCompleted != "" {
		lines = append(lines, SkillCompletedStyle.Render("Last completed: "+s.lastCompleted))
	}

	if !s.skillsEnabled && s.disabledReason != "" {
		lines = append(lines, "Reason: "+truncateLine(s.disabledReason, 90))
	}

	if s.menuState != "" {
		lines = append(lines, "Mode: "+s.menuState)
	}

	if hint := skillsInputHint(s.currentInput, s.nsfw, s.skillsEnabled); hint != "" {
		lines = append(lines, hint)
	}

	// Navigation keybinds
	navHint := lipgloss.NewStyle().Foreground(lipgloss.Color("#6b7280")).Render(
		"⇧↑/⇧↓ scroll  PgUp/PgDn page  Home/End jump  Ctrl+K calls  Ctrl+C×2 quit")
	lines = append(lines, navHint)

	content := strings.Join(lines, "\n")
	style := SkillsPanelStyle
	if s.width > 0 {
		panelWidth := s.width - 2
		if panelWidth < 20 {
			panelWidth = 20
		}
		style = style.Width(panelWidth)
	}

	return style.Render(content)
}

func skillsHeaderLine(enabled bool, count int) string {
	if enabled {
		return SkillNameStyle.Render(fmt.Sprintf("Skills: enabled (%d loaded)", count))
	}
	return SkillErrorStyle.Render("Skills: disabled")
}

func skillsInputHint(input string, nsfw, enabled bool) string {
	if nsfw {
		return "Hint: NSFW mode routes through Venice and disables tool calls."
	}
	if !enabled {
		return "Hint: switch to a tool-capable model/provider to enable skills."
	}

	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "Hint: type `skills` to browse available tool functions."
	}
	if strings.HasPrefix(trimmed, "/") {
		return "Hint: slash command detected; commands run before model/tool dispatch."
	}
	return ""
}

func safeLabel(value string) string {
	if strings.TrimSpace(value) == "" {
		return "n/a"
	}
	return value
}

func truncateLine(value string, max int) string {
	if max <= 3 || len(value) <= max {
		return value
	}
	return value[:max-3] + "..."
}
