package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/memories"
)

// fitRow keeps a one-row field to one row: a multi-line value (bash
// stderr, a memory description) is joined before the cut, so a failing
// tool cannot grow the skills row to dozens of lines.
func TestFitRowIsOneRow(t *testing.T) {
	got := fitRow("line one\nline two\r\nline three", 12)
	if lipgloss.Height(got) != 1 || lipgloss.Width(got) > 12 {
		t.Fatalf("fitRow = %q, want one row of at most 12 cells", got)
	}
	if got != "line one li…" {
		t.Fatalf("fitRow = %q", got)
	}
}

func TestSkillsRowStaysOneRowOnMultiLineError(t *testing.T) {
	err := errors.New(strings.Repeat("x\n", 40))
	s := SkillsModel{}.SetError("bash", err)
	if h := lipgloss.Height(s.View()); h != 1 {
		t.Fatalf("collapsed skills row is %d rows, want 1", h)
	}
	off := SkillsModel{disabledReason: strings.Repeat("why\n", 30)}
	if h := lipgloss.Height(off.View()); h != 1 {
		t.Fatalf("skills-off row is %d rows, want 1", h)
	}
}

func TestMemoryManagerRowStaysOneRowOnMultiLineDescription(t *testing.T) {
	view := func(desc string) string {
		m := MemoryManagerModel{
			memories:  []*memories.Memory{{Name: "n", Type: "project", Description: desc}},
			confirmed: -1,
			expanded:  -1,
			width:     100,
			height:    40,
		}
		return m.View()
	}
	one := lipgloss.Height(view("short"))
	many := lipgloss.Height(view(strings.Repeat("d\n", 20)))
	if many != one {
		t.Fatalf("multi-line description renders %d rows, single-line %d", many, one)
	}
}
