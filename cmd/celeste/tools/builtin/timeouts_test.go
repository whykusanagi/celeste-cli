package builtin

import (
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
)

// These values replace the tool-name table the chat's own tool execution
// used to keep in main.go; the chat runs its tools on loop.Loop (2.0 F2d).
func TestLongRunningToolsCarryTheirTimeouts(t *testing.T) {
	ws := t.TempDir()
	cases := []struct {
		tool tools.Tool
		want time.Duration
	}{
		{NewBashTool(ws, nil), 5 * time.Minute},
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
