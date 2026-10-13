package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

func TestConfigSetKeyDoesNotEchoKeyAndUpdatesLegacyFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CELESTE_API_KEY", "")
	if err := os.MkdirAll(filepath.Join(home, ".celeste"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"x", "sk-sensitive-1234567890"} {
		res := Execute(&Command{Name: "config", Args: []string{"set-key", key}}, &CommandContext{})
		if !res.Success || strings.Contains(res.Message, key) {
			t.Fatalf("set-key did not safely succeed: %q", res.Message)
		}
		cfg, err := config.Load()
		if err != nil || cfg.APIKey != key {
			t.Fatalf("saved key was not reloaded: %v", err)
		}
	}
}
