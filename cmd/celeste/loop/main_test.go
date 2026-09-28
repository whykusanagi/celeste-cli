package loop

import (
	"os"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/hooktest"
)

func TestMain(m *testing.M) {
	hooktest.RunIfHelper() // the test binary doubles as the hook command (F0)
	os.Exit(m.Run())
}
