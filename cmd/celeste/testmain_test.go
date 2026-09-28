package main

import (
	"os"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/hooktest"
)

func TestMain(m *testing.M) {
	hooktest.RunIfHelper()
	os.Exit(m.Run())
}
