package agent

import (
	"os"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/gittest"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/hooktest"
)

func TestMain(m *testing.M) {
	hooktest.RunIfHelper() // the test binary doubles as the hook command (F0)
	gittest.Isolate()      // git run by the code under test stays in its temp repos
	os.Exit(m.Run())
}

// isolateHome points HOME and USERPROFILE at a temp dir: NewRunner now loads
// ~/.celeste/hooks.json, MCP configs and skills through loop.Setup.
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}
