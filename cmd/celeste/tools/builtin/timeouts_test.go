package builtin

import (
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// These values replace the tool-name table that used to live in main.go's
// TUIClientAdapter.ExecuteSkill (2.0 F2).
func TestLongRunningToolsCarryTheirTimeouts(t *testing.T) {
	ws := t.TempDir()
	cases := []struct {
		tool tools.Tool
		want time.Duration
	}{
		{NewBashTool(ws), 5 * time.Minute},
		{NewTTSTool(ws), 5 * time.Minute},
		{NewAudioProjectTool(ws), 2 * time.Minute},
		{NewReadFileTool(ws), 45 * time.Second}, // no own timeout: the default
	}
	for _, c := range cases {
		if got := tools.TimeoutFor(c.tool, 45*time.Second); got != c.want {
			t.Errorf("%s timeout = %v, want %v", c.tool.Name(), got, c.want)
		}
	}
}
