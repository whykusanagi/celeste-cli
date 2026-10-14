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

func TestConfigSetKeyReportsFailedLegacySaveWithoutExposingKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CELESTE_API_KEY", "")
	dir := filepath.Join(home, ".celeste")
	if err := os.MkdirAll(filepath.Join(dir, "secrets.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	key := "sk-sensitive-1234567890"
	res := Execute(&Command{Name: "config", Args: []string{"set-key", key}}, &CommandContext{})
	if res.Success || !strings.Contains(res.Message, "Failed to save secrets") {
		t.Fatalf("failed legacy save should be reported: %q", res.Message)
	}
	for _, fragment := range []string{key, key[:4], key[len(key)-4:]} {
		if strings.Contains(res.Message, fragment) {
			t.Fatal("a failed save exposed credential bytes")
		}
	}
}
