package collections

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

// collectionsHome is a fresh ~/.celeste holding config.json and, when
// profile is not empty, config.work.json.
func collectionsHome(t *testing.T, global, profile string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(config.EnvAPIKey, "")
	t.Setenv(config.EnvAPIEndpoint, "")
	dir := filepath.Join(home, ".celeste")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if global != "" {
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(global), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if profile != "" {
		if err := os.WriteFile(filepath.Join(dir, "config.work.json"), []byte(profile), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("%s: %v", filepath.Base(path), err)
	}
	return raw
}

func activeIDs(t *testing.T, raw map[string]any) []any {
	t.Helper()
	c, ok := raw["collections"].(map[string]any)
	if !ok {
		t.Fatalf("no collections block in %v", raw)
	}
	ids, _ := c["active_collections"].([]any)
	return ids
}

// #324: with a named profile active, saving a collections change must go to
// that profile's file and leave config.json (and its sandbox block, which
// profiles inherit) byte-for-byte unchanged.
func TestSaveConfigWritesActiveProfileNotConfigJSON(t *testing.T) {
	const global = `{"model":"base-model","sandbox":{"enabled":true,"network":false}}`
	for _, tc := range []struct{ name, load string }{
		{"default-flagged profile", ""},
		{"explicit -config profile", "work"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := collectionsHome(t, global,
				`{"default":true,"model":"work-model","api_key":"k","sandbox":{"enabled":false}}`)
			cfg, err := config.LoadNamed(tc.load)
			if err != nil {
				t.Fatal(err)
			}
			m := NewManager(nil, cfg)
			if err := m.EnableCollection("col_1"); err != nil {
				t.Fatal(err)
			}
			if err := m.SaveConfig(); err != nil {
				t.Fatal(err)
			}

			got, err := os.ReadFile(filepath.Join(dir, "config.json"))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != global {
				t.Fatalf("config.json changed by a profile's collections save:\n got %s\nwant %s", got, global)
			}
			prof := readJSON(t, filepath.Join(dir, "config.work.json"))
			if ids := activeIDs(t, prof); len(ids) != 1 || ids[0] != "col_1" {
				t.Fatalf("profile active_collections = %v, want [col_1]", ids)
			}
			if prof["model"] != "work-model" || prof["default"] != true {
				t.Fatalf("profile keys lost: %v", prof)
			}
			sb, _ := prof["sandbox"].(map[string]any)
			if sb == nil || sb["enabled"] != false || sb["network"] != nil {
				t.Fatalf("profile sandbox = %v, want only its own {enabled:false}", prof["sandbox"])
			}
		})
	}
}

// Without a profile the change goes to config.json, keeping its other keys
// and not copying in values merged from skills.json or secrets.json.
func TestSaveConfigWithoutProfileWritesConfigJSON(t *testing.T) {
	dir := collectionsHome(t, `{"model":"base-model","sandbox":{"enabled":true,"network":false}}`, "")
	if err := os.WriteFile(filepath.Join(dir, "skills.json"), []byte(`{"venice_api_key":"skills-secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadNamed("")
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(nil, cfg)
	if err := m.EnableCollection("col_2"); err != nil {
		t.Fatal(err)
	}
	if err := m.SaveConfig(); err != nil {
		t.Fatal(err)
	}
	raw := readJSON(t, filepath.Join(dir, "config.json"))
	if ids := activeIDs(t, raw); len(ids) != 1 || ids[0] != "col_2" {
		t.Fatalf("active_collections = %v, want [col_2]", ids)
	}
	sb, _ := raw["sandbox"].(map[string]any)
	if raw["model"] != "base-model" || sb == nil || sb["enabled"] != true || sb["network"] != false {
		t.Fatalf("config.json keys lost: %v", raw)
	}
	if _, ok := raw["venice_api_key"]; ok {
		t.Fatalf("skills.json key copied into config.json: %v", raw)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.work.json")); !os.IsNotExist(err) {
		t.Fatalf("a profile file appeared: %v", err)
	}
}

// Disabling the last collection still saves the (empty) list.
func TestSaveConfigPersistsDisable(t *testing.T) {
	dir := collectionsHome(t, `{"model":"m"}`,
		`{"model":"w","collections":{"enabled":true,"active_collections":["a","b"],"auto_enable":true}}`)
	cfg, err := config.LoadNamed("work")
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(nil, cfg)
	_ = m.DisableCollection("a")
	_ = m.DisableCollection("b")
	if err := m.SaveConfig(); err != nil {
		t.Fatal(err)
	}
	cfg2, err := config.LoadNamed("work")
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Collections == nil || len(cfg2.Collections.ActiveCollections) != 0 {
		t.Fatalf("reloaded collections = %+v, want empty active list", cfg2.Collections)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "config.json")); string(got) != `{"model":"m"}` {
		t.Fatalf("config.json changed: %s", got)
	}
}
