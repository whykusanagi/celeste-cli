package config

import (
	"runtime"
	"testing"
)

// Aikido 806869780: no home directory fails closed instead of resolving
// ~/.celeste against the current directory.
func TestHomeDirFailsClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", "")
	}
	t.Setenv("HOME", "")
	if h, err := HomeDir(); err == nil {
		t.Errorf("HomeDir() = %q with no HOME, want an error", h)
	}
	t.Setenv("HOME", "relative/home")
	t.Setenv("USERPROFILE", "relative/home")
	if h, err := HomeDir(); err == nil {
		t.Errorf("HomeDir() = %q for a relative HOME, want an error", h)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if h, err := HomeDir(); err != nil || h != home {
		t.Errorf("HomeDir() = %q, %v", h, err)
	}
}
