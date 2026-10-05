package commands

import (
	"regexp"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// C6: no /stats row starts with the padding a styled block leaves after a
// line break, so the closing ▓▒░ rules start at column 0 and fit in 80
// columns.
func TestStatsRowsStartAtTheirOwnColumn(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	s := &config.Session{}
	tracker := config.NewContextTracker(s, "m", 100_000)
	for range 5 { // random phrases: try several
		res := HandleStatsCommand(nil, tracker)
		if !res.Success {
			t.Fatal(res.Message)
		}
		for _, row := range strings.Split(ansiSeq.ReplaceAllString(res.Message, ""), "\n") {
			if strings.Contains(row, "═══") && !strings.HasPrefix(row, "▓▒░ ═") {
				t.Errorf("rule does not start at column 0: %q", row)
			}
			if strings.HasPrefix(row, " ") && strings.TrimSpace(row) == "" && row != "" {
				t.Errorf("a padding-only row: %q", row)
			}
			if strings.HasPrefix(row, "                                ") && strings.Contains(row, "▓") {
				t.Errorf("row pushed right by padding: %q", row)
			}
		}
	}
}
