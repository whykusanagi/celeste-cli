package hooks

import (
	"os"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/hooktest"
)

func TestMain(m *testing.M) {
	hooktest.RunIfHelper()
	os.Exit(m.Run())
}
