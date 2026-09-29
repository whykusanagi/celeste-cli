package server

import (
	"fmt"
	"os"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/hooktest"
)

func TestMain(m *testing.M) {
	hooktest.RunIfHelper() // the test binary doubles as the hook command (F0)
	// Backstop: no server test may touch the real home, even one that forgets
	// to isolate itself. Tests that need their own HOME still t.Setenv it.
	home, err := os.MkdirTemp("", "celeste-server-home-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv("HOME", home)
	os.Setenv("USERPROFILE", home)
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}
