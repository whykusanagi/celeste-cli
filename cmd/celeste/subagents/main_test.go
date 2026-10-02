package subagents

import (
	"os"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/gittest"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/hooktest"
)

func TestMain(m *testing.M) {
	hooktest.RunIfHelper() // the test binary doubles as the hook command (F0)
	gittest.Isolate()      // git run by tests and the code under test stays in its temp repos
	os.Exit(m.Run())
}
