package tui

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSkillsModelViewEnabled(t *testing.T) {
	model := NewSkillsModel().
		SetSize(120, 10).
		SetConfig("openai", "gpt-4o-mini", true, false, 5, "").
		SetCurrentInput("")

	view := model.View()
	assert.Contains(t, view, "Skills: enabled (5 loaded)")
	assert.Contains(t, view, "Backend: openai")
	assert.Contains(t, view, "Model: gpt-4o-mini")
	assert.Contains(t, view, "type `skills` to browse")
}

func TestSkillsModelExecutionStateTransitions(t *testing.T) {
	model := NewSkillsModel().
		SetSize(100, 10).
		SetConfig("openai", "gpt-4o-mini", true, false, 3, "")

	executing := model.SetExecuting("get_weather").View()
	assert.Contains(t, executing, "Executing: get_weather")

	completed := model.SetExecuting("get_weather").SetCompleted("get_weather").View()
	assert.Contains(t, completed, "Last completed: get_weather")

	errored := model.SetExecuting("get_weather").SetError("get_weather", errors.New("boom")).View()
	assert.Contains(t, errored, "Last error (get_weather): boom")
}

func TestSkillsModelViewDisabledReason(t *testing.T) {
	model := NewSkillsModel().
		SetSize(100, 10).
		SetConfig("venice", "venice-uncensored", false, true, 0, "NSFW Mode - Venice doesn't support tools")

	view := model.View()
	assert.Contains(t, view, "Skills: disabled")
	assert.Contains(t, view, "Reason: NSFW Mode - Venice doesn't support tools")
	assert.Contains(t, view, "NSFW mode routes through Venice and disables tool calls")
}
