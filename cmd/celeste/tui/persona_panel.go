// TUI panel for the /persona command — personality slider controls.
//
// Displays 4 sliders (flirt, warmth, register, lewdness) + the R18
// toggle. Users navigate with arrow keys (up/down to select slider,
// left/right to adjust value) and press Enter to confirm or Esc to
// cancel. Changes are persisted to ~/.celeste/slider.json on confirm.
//
// The panel also supports preset save/load and reset-to-default.
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

// PersonaPanelModel is the bubbletea model for the /persona panel.
type PersonaPanelModel struct {
	sliders  *config.SliderConfig
	cursor   int // which slider is selected (0-4: flirt, warmth, register, lewdness, r18)
	width    int
	modified bool
}

// NewPersonaPanelModel creates a new persona panel loaded from disk.
func NewPersonaPanelModel() PersonaPanelModel {
	return PersonaPanelModel{
		sliders: config.LoadSliders(),
		cursor:  0,
	}
}

// SetWidth sets the panel width for rendering.
func (m PersonaPanelModel) SetWidth(w int) PersonaPanelModel {
	m.width = w
	return m
}

// Sliders returns the current slider config (for prompt composition).
func (m PersonaPanelModel) Sliders() *config.SliderConfig {
	return m.sliders
}

// Update handles key events for the persona panel.
func (m PersonaPanelModel) Update(msg tea.Msg) (PersonaPanelModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < 4 {
				m.cursor++
			}
		case "left", "h":
			m.adjustSlider(-1)
		case "right", "l":
			m.adjustSlider(1)
		case "r":
			m.sliders.Reset()
			m.modified = true
		}
	}
	return m, nil
}

// adjustSlider changes the currently selected slider value.
func (m *PersonaPanelModel) adjustSlider(delta int) {
	m.modified = true
	switch m.cursor {
	case 0:
		m.sliders.Flirt = clampSlider(m.sliders.Flirt + delta)
	case 1:
		m.sliders.Warmth = clampSlider(m.sliders.Warmth + delta)
	case 2:
		m.sliders.Register = clampSlider(m.sliders.Register + delta)
	case 3:
		if m.sliders.R18Enabled {
			m.sliders.Lewdness = clampSlider(m.sliders.Lewdness + delta)
		}
	case 4:
		// R18 toggle — any direction flips
		if delta != 0 {
			m.sliders.R18Enabled = !m.sliders.R18Enabled
			if !m.sliders.R18Enabled {
				m.sliders.Lewdness = 0
			}
		}
	}
}

func clampSlider(v int) int {
	if v < 0 {
		return 0
	}
	if v > 10 {
		return 10
	}
	return v
}

// Save persists the slider config to disk.
func (m *PersonaPanelModel) Save() error {
	return m.sliders.Save()
}

// Modified returns true if sliders were changed since panel opened.
func (m PersonaPanelModel) Modified() bool {
	return m.modified
}

// View renders the persona panel.
func (m PersonaPanelModel) View() string {
	w := m.width
	if w < 40 {
		w = 60
	}

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#d94f90")).
		Bold(true)

	mutedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#7a7085"))

	activeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#d94f90")).
		Bold(true)

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#f0ecf8")).
		Width(12)

	anchorLabels := map[int][4]string{
		0: {"professional", "playful", "flirty", "aggressive"},
		1: {"cold", "polite", "warm", "affectionate"},
		2: {"operator", "standard", "theatrical", "uwu"},
		3: {"SFW", "suggestive", "explicit", "R18"},
	}

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("  ✨ Persona Sliders"))
	sb.WriteString("\n")
	sb.WriteString(mutedStyle.Render("  ↑↓ select  ←→ adjust  r reset  esc close"))
	sb.WriteString("\n\n")

	type sliderDef struct {
		name  string
		value int
		idx   int
	}

	sliders := []sliderDef{
		{"Flirt", m.sliders.Flirt, 0},
		{"Warmth", m.sliders.Warmth, 1},
		{"Speech", m.sliders.Register, 2},
		{"Lewdness", m.sliders.Lewdness, 3},
	}

	anchorDescs := map[int][4]string{
		0: {
			"Task-focused, minimal personal commentary",
			"Light teasing woven into competence",
			"Confident compliments, natural double entendres",
			"Forward and unabashed, you set the pace",
		},
		1: {
			"Precise and efficient, emotion implied through action",
			"Genuine helpfulness, measured warmth",
			"Openly engaged — celebrates wins, notices effort",
			"Pet names feel natural, concern is proactive",
		},
		2: {
			"Short sentences, stripped to load-bearing words",
			"Natural conversational cadence, contractions fine",
			"Dramatic flair, vivid reactions, luxurious sentences",
			"Softened speech, playful affect, cozy energy",
		},
		3: {
			"General-audience, innuendo absent or deniable",
			"Light innuendo, imagination does the heavy lifting",
			"Overt sensuality, tension built through pacing",
			"Explicit when warranted, always with craft",
		},
	}

	for i, s := range sliders {
		cursor := "  "
		style := labelStyle
		if m.cursor == i {
			cursor = "▸ "
			style = labelStyle.Foreground(lipgloss.Color("#d94f90"))
		}

		// Render the slider bar
		bar := renderSliderBar(s.value, m.cursor == i)

		// Anchor label for current snap point
		anchor := config.SnapToAnchor(s.value)
		anchorLabel := anchorLabels[s.idx][anchor]

		// Dim lewdness if R18 is off
		if s.idx == 3 && !m.sliders.R18Enabled {
			bar = mutedStyle.Render(renderSliderBarPlain(s.value))
			anchorLabel = "locked (R18 off)"
		}

		sb.WriteString(fmt.Sprintf("%s%s %s  %s\n",
			cursor,
			style.Render(s.name),
			bar,
			mutedStyle.Render(anchorLabel),
		))

		// Show descriptor for the selected slider
		if m.cursor == i {
			desc := anchorDescs[s.idx][anchor]
			if s.idx == 3 && !m.sliders.R18Enabled {
				desc = "Enable R18 toggle below to unlock this slider"
			}
			sb.WriteString(fmt.Sprintf("                %s\n", mutedStyle.Render("↳ "+desc)))
		}
	}

	// R18 toggle
	sb.WriteString("\n")
	r18Cursor := "  "
	if m.cursor == 4 {
		r18Cursor = "▸ "
	}
	r18Label := "○ OFF"
	r18Style := mutedStyle
	if m.sliders.R18Enabled {
		r18Label = "● ON"
		r18Style = activeStyle
	}
	r18Name := labelStyle.Render("R18 Toggle")
	if m.cursor == 4 {
		r18Name = labelStyle.Copy().Foreground(lipgloss.Color("#d94f90")).Render("R18 Toggle")
	}
	sb.WriteString(fmt.Sprintf("%s%s %s\n", r18Cursor, r18Name, r18Style.Render(r18Label)))

	if m.modified {
		sb.WriteString("\n" + mutedStyle.Render("  Changes will be saved when you close the panel."))
	}

	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#8b5cf6")).
		Padding(1, 2).
		Width(w - 4)

	return panelStyle.Render(sb.String())
}

// renderSliderBar draws a visual slider bar [████░░░░░░] 7/10
func renderSliderBar(value int, active bool) string {
	filled := value
	empty := 10 - value

	filledChar := "█"
	emptyChar := "░"

	fillColor := lipgloss.Color("#8b5cf6")
	if active {
		fillColor = lipgloss.Color("#d94f90")
	}

	bar := lipgloss.NewStyle().Foreground(fillColor).Render(strings.Repeat(filledChar, filled))
	bar += lipgloss.NewStyle().Foreground(lipgloss.Color("#3a3050")).Render(strings.Repeat(emptyChar, empty))

	numStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#f0ecf8")).Bold(true)
	return fmt.Sprintf("[%s] %s", bar, numStyle.Render(fmt.Sprintf("%2d/10", value)))
}

// renderSliderBarPlain draws a dimmed slider bar for locked sliders.
func renderSliderBarPlain(value int) string {
	filled := value
	empty := 10 - value
	return fmt.Sprintf("[%s%s] %2d/10",
		strings.Repeat("█", filled),
		strings.Repeat("░", empty),
		value,
	)
}
