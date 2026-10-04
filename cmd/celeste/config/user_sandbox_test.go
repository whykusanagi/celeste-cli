package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// sandboxHome is a fresh ~/.celeste with config.json holding global and,
// when profile is not empty, config.work.json flagged "default": true.
func sandboxHome(t *testing.T, global, profile string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(EnvAPIKey, "")
	t.Setenv(EnvAPIEndpoint, "")
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

func TestProfileInheritsSandboxFromConfigJSON(t *testing.T) {
	sandboxHome(t,
		`{"model":"m","sandbox":{"enabled":true,"network":false,"writable":["/opt/cache"]}}`,
		`{"default":true,"model":"m"}`)
	cfg, err := LoadNamed("")
	if err != nil {
		t.Fatal(err)
	}
	s := cfg.Sandbox
	if s == nil || s.Enabled == nil || !*s.Enabled || s.Network == nil || *s.Network || len(s.Writable) != 1 || s.Writable[0] != "/opt/cache" {
		t.Fatalf("active profile without a sandbox block: sandbox = %+v, want config.json's", s)
	}
	// An explicitly named profile inherits too.
	cfg, err = LoadNamed("work")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sandbox == nil || cfg.Sandbox.Enabled == nil || !*cfg.Sandbox.Enabled {
		t.Fatalf("-config work: sandbox = %+v, want enabled", cfg.Sandbox)
	}
}

func TestProfileSandboxWinsOverConfigJSON(t *testing.T) {
	sandboxHome(t,
		`{"sandbox":{"enabled":true,"network":false,"writable":["/opt/cache"]}}`,
		`{"default":true,"model":"m","sandbox":{"enabled":false,"writable":[]}}`)
	cfg, err := LoadNamed("")
	if err != nil {
		t.Fatal(err)
	}
	s := cfg.Sandbox
	if s == nil || s.Enabled == nil || *s.Enabled {
		t.Fatalf("profile's enabled:false must win: %+v", s)
	}
	if len(s.Writable) != 0 {
		t.Fatalf("profile's writable:[] must win: %+v", s.Writable)
	}
	// A key the profile leaves unset comes from config.json.
	if s.Network == nil || *s.Network {
		t.Fatalf("network unset in the profile: want config.json's false, got %+v", s.Network)
	}
}

func TestNoProfileSandboxFromConfigJSON(t *testing.T) {
	sandboxHome(t, `{"model":"m","sandbox":{"enabled":true}}`, "")
	cfg, err := LoadNamed("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sandbox == nil || cfg.Sandbox.Enabled == nil || !*cfg.Sandbox.Enabled {
		t.Fatalf("no profile: sandbox = %+v, want config.json's", cfg.Sandbox)
	}
}

func TestProfileNoSandboxAnywhere(t *testing.T) {
	sandboxHome(t, `{"model":"m"}`, `{"default":true,"model":"m"}`)
	cfg, err := LoadNamed("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sandbox != nil {
		t.Fatalf("sandbox = %+v, want nil", cfg.Sandbox)
	}
	// A malformed config.json does not break the profile.
	dir := sandboxHome(t, `{"sandbox":`, `{"default":true,"model":"m"}`)
	if _, err := LoadNamed(""); err != nil {
		t.Fatalf("malformed %s broke the profile: %v", filepath.Join(dir, "config.json"), err)
	}
}

func compactJSON(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var b bytes.Buffer
	if len(raw) == 0 {
		return ""
	}
	if err := json.Compact(&b, raw); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// Saving a profile must not copy config.json's sandbox into it, or later
// edits to config.json would stop reaching that profile.
func TestSaveNamedKeepsInheritedSandboxOut(t *testing.T) {
	dir := sandboxHome(t,
		`{"sandbox":{"enabled":true,"network":false}}`,
		`{"default":true,"model":"m","sandbox":{"network":true}}`)
	cfg, err := LoadNamed("work")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Model = "m2"
	if err := SaveNamed("work", cfg); err != nil {
		t.Fatal(err)
	}
	if err := SetDefaultProfile("work"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.work.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Model   string          `json:"model"`
		Sandbox json.RawMessage `json:"sandbox"`
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Model != "m2" {
		t.Fatalf("model = %q", saved.Model)
	}
	if compactJSON(t, saved.Sandbox) != `{"network":true}` {
		t.Fatalf("saved sandbox = %s, want only the profile's own {\"network\":true}", saved.Sandbox)
	}

	// A sandbox the caller changes is the profile's now and is saved.
	cfg, err = LoadNamed("work")
	if err != nil {
		t.Fatal(err)
	}
	off := false
	cfg.Sandbox.Enabled = &off
	if err := SaveNamed("work", cfg); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(filepath.Join(dir, "config.work.json"))
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if compactJSON(t, saved.Sandbox) != `{"enabled":false,"network":true}` {
		t.Fatalf("changed sandbox saved as %s", saved.Sandbox)
	}
}
