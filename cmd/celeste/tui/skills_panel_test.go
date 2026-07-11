package tui

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSkillsModel_CollapsedIsQuietWhenIdle(t *testing.T) {
	// Default (collapsed) chat rendering shows nothing when idle — the skill
	// count + model moved to the status line; nav keys to the hints row.
	view := NewSkillsModel().
		SetSize(120, 10).
		SetConfig("openai", "gpt-4o-mini", true, false, 5, "").
		SetCurrentInput("").
		View()
	assert.Empty(t, view)
}

func TestSkillsModel_ExpandedShowsFullPanel(t *testing.T) {
	view := NewSkillsModel().
		SetSize(120, 10).
		SetConfig("openai", "gpt-4o-mini", true, false, 5, "").
		SetExpanded(true).
		View()
	assert.Contains(t, view, "Skills: enabled (5 loaded)")
	assert.Contains(t, view, "Backend: openai")
	assert.Contains(t, view, "Model: gpt-4o-mini")
	assert.Contains(t, view, "type `skills` to browse")
}

func TestSkillsModel_CollapsedShowsActiveSignal(t *testing.T) {
	model := NewSkillsModel().
		SetSize(100, 10).
		SetConfig("openai", "gpt-4o-mini", true, false, 3, "")

	assert.Contains(t, model.SetExecuting("get_weather").View(), "get_weather")
	assert.Contains(t, model.SetExecuting("get_weather").SetCompleted("get_weather").View(), "get_weather")
	assert.Contains(t, model.SetExecuting("get_weather").SetError("get_weather", errors.New("boom")).View(), "boom")
}

func TestSkillsModel_CollapsedShowsDisabledReason(t *testing.T) {
	// Why skills are off is important signal — keep it even when collapsed.
	view := NewSkillsModel().
		SetSize(100, 10).
		SetConfig("venice", "venice-uncensored", false, true, 0, "NSFW Mode - Venice doesn't support tools").
		View()
	assert.Contains(t, view, "NSFW Mode - Venice doesn't support tools")
}

func TestSkillsModel_Getters(t *testing.T) {
	m := NewSkillsModel().SetConfig("openai", "gpt-4o-mini", true, false, 7, "")
	assert.True(t, m.Enabled())
	assert.Equal(t, 7, m.Count())
}
