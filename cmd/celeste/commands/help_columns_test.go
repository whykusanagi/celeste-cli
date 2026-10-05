package commands

import (
	"strings"
	"testing"
)

// helpDescColumn is where every description in chatCommandsHelp starts.
const helpDescColumn = 21

// C2 and the /config rows: every /help description starts at the same
// column, at least two spaces after its command, so a wrapped row hangs
// under its description at any width. A command too long for that sits on
// its own row, with its description on the next one at the column.
func TestHelpDescriptionsShareOneColumn(t *testing.T) {
	rows := strings.Split(chatCommandsHelp, "\n")
	pad := strings.Repeat(" ", helpDescColumn)
	for i, r := range rows {
		if !strings.HasPrefix(r, "  /") {
			continue
		}
		if !strings.Contains(r[2:], "  ") {
			// A command alone: the next row is its description.
			if i+1 >= len(rows) || !strings.HasPrefix(rows[i+1], pad) || rows[i+1][helpDescColumn] == ' ' {
				t.Errorf("command %q has no description row at column %d", r, helpDescColumn)
			}
			continue
		}
		if r[helpDescColumn] == ' ' || r[helpDescColumn-1] != ' ' || r[helpDescColumn-2] != ' ' {
			t.Errorf("row %q: its description does not start at column %d after two spaces", r, helpDescColumn)
		}
	}
}
