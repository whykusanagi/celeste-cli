package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveCollectionsEdgeCases(t *testing.T) {
	cols := &CollectionsConfig{Enabled: true, ActiveCollections: []string{"c"}}

	t.Run("no config.json yet creates one with only collections", func(t *testing.T) {
		dir := sandboxHome(t, "", "")
		if err := SaveCollections(&Config{Collections: cols}); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Collections == nil || len(cfg.Collections.ActiveCollections) != 1 {
			t.Fatalf("collections = %+v", cfg.Collections)
		}
		info, err := os.Stat(filepath.Join(dir, "config.json"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
		}
	})

	t.Run("a deleted profile is not recreated", func(t *testing.T) {
		dir := sandboxHome(t, `{"model":"m"}`, `{"model":"w"}`)
		cfg, err := LoadNamed("work")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(dir, "config.work.json")); err != nil {
			t.Fatal(err)
		}
		cfg.Collections = cols
		if err := SaveCollections(cfg); err == nil {
			t.Fatal("SaveCollections recreated a deleted profile")
		}
		if _, err := os.Stat(filepath.Join(dir, "config.work.json")); !os.IsNotExist(err) {
			t.Fatalf("profile file exists: %v", err)
		}
	})

	t.Run("an unparseable file is left alone", func(t *testing.T) {
		dir := sandboxHome(t, `{"model":"m"}`, "")
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "config.json")
		if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg.Collections = cols
		if err := SaveCollections(cfg); err == nil {
			t.Fatal("SaveCollections overwrote an unparseable config.json")
		}
		if b, _ := os.ReadFile(path); string(b) != "{not json" {
			t.Fatalf("config.json = %s", b)
		}
	})

	t.Run("nil collections removes the key", func(t *testing.T) {
		sandboxHome(t, `{"model":"m","collections":{"enabled":true}}`, "")
		if err := SaveCollections(&Config{}); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Collections != nil || cfg.Model != "m" {
			t.Fatalf("after save: model %q collections %+v", cfg.Model, cfg.Collections)
		}
	})
}
