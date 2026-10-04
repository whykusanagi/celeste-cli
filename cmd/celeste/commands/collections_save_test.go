package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

// #324: `celeste -config work collections enable|disable` must save to
// config.work.json, never over config.json.
func TestCollectionsEnableDisableSaveToActiveProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(config.EnvAPIKey, "")
	t.Setenv(config.EnvAPIEndpoint, "")
	dir := filepath.Join(home, ".celeste")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	const global = `{"model":"base","sandbox":{"enabled":true}}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(global), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.work.json"), []byte(`{"model":"w","api_key":"k"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.LoadNamed("work")
	if err != nil {
		t.Fatal(err)
	}
	if r := handleCollectionsEnable([]string{"col_9"}, cfg); !r.Success {
		t.Fatalf("enable: %s", r.Message)
	}
	got, _ := config.LoadNamed("work")
	if got.Collections == nil || len(got.Collections.ActiveCollections) != 1 || got.Collections.ActiveCollections[0] != "col_9" {
		t.Fatalf("profile collections after enable = %+v", got.Collections)
	}
	if r := handleCollectionsDisable([]string{"col_9"}, cfg); !r.Success {
		t.Fatalf("disable: %s", r.Message)
	}
	got, _ = config.LoadNamed("work")
	if got.Collections == nil || len(got.Collections.ActiveCollections) != 0 {
		t.Fatalf("profile collections after disable = %+v", got.Collections)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "config.json")); string(b) != global {
		t.Fatalf("config.json changed: %s", b)
	}
}
