package builtin

import (
	"os"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/gittest"
)

// TestMain isolates git: tests and the code under test run git, which must
// never reach a repository a parent process exported (git rebase -x sets
// GIT_DIR).
func TestMain(m *testing.M) {
	gittest.Isolate()
	os.Exit(m.Run())
}
