package tui

import (
	"slices"
	"testing"
)

// Every slash command the TUI handles is offered by typeahead, and /init
// offers its one argument.
func TestSuggestionsCoverHandledCommands(t *testing.T) {
	cases := []struct{ input, want string }{
		{"/comp", "compact"},
		{"/hand", "handoff"},
		{"/und", "undo"},
		{"/dif", "diff"},
		{"/init ", "init agents"},
		{"/init a", "init agents"},
	}
	for _, c := range cases {
		if got := computeSuggestions(c.input); !slices.Contains(got, c.want) {
			t.Errorf("computeSuggestions(%q) = %v, want it to offer %q", c.input, got, c.want)
		}
	}
}
