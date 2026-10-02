package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeEnvProfile(t *testing.T, home, body string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := filepath.Join(home, ".celeste")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.envtest.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestEnvOverridesWinOverTheFile(t *testing.T) {
	home := t.TempDir()
	writeEnvProfile(t, home, `{"api_key":"file-key","base_url":"https://file.example/v1","model":"m","tarot_auth_token":"file-tarot"}`)
	t.Setenv("CELESTE_API_KEY", "env-key")
	t.Setenv("CELESTE_API_ENDPOINT", "https://env.example/v1")
	t.Setenv("TAROT_AUTH_TOKEN", "env-tarot")

	cfg, err := LoadNamedWithEnv("envtest")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != "env-key" || cfg.BaseURL != "https://env.example/v1" || cfg.TarotAuthToken != "env-tarot" {
		t.Errorf("env did not win: key=%q url=%q tarot=%q", cfg.APIKey, cfg.BaseURL, cfg.TarotAuthToken)
	}
}

func TestEnvOverridesEachVarAlone(t *testing.T) {
	for _, tc := range []struct {
		env  string
		get  func(*Config) string
		want string
	}{
		{"CELESTE_API_KEY", func(c *Config) string { return c.APIKey }, "k-env"},
		{"CELESTE_API_ENDPOINT", func(c *Config) string { return c.BaseURL }, "https://e.example/v1"},
		{"TAROT_AUTH_TOKEN", func(c *Config) string { return c.TarotAuthToken }, "t-env"},
	} {
		t.Run(tc.env, func(t *testing.T) {
			for _, v := range []string{"CELESTE_API_KEY", "CELESTE_API_ENDPOINT", "TAROT_AUTH_TOKEN"} {
				t.Setenv(v, "")
			}
			t.Setenv(tc.env, tc.want)
			cfg := &Config{APIKey: "file", BaseURL: "https://file/v1", TarotAuthToken: "file"}
			ApplyEnvOverrides(cfg)
			if got := tc.get(cfg); got != tc.want {
				t.Errorf("%s: got %q, want %q", tc.env, got, tc.want)
			}
		})
	}
}

func TestEnvUnsetOrEmptyKeepsTheFile(t *testing.T) {
	for _, v := range []string{"CELESTE_API_KEY", "CELESTE_API_ENDPOINT", "TAROT_AUTH_TOKEN"} {
		t.Setenv(v, "  ")
	}
	cfg := &Config{APIKey: "file", BaseURL: "https://file/v1", TarotAuthToken: "file-t"}
	ApplyEnvOverrides(cfg)
	if cfg.APIKey != "file" || cfg.BaseURL != "https://file/v1" || cfg.TarotAuthToken != "file-t" {
		t.Errorf("blank env changed the config: %+v", cfg)
	}
}

// An override is for this run only: LoadNamed (which SetDefault and the
// config command save back) must not see it, so it is never written out.
func TestPlainLoadNamedIgnoresEnv(t *testing.T) {
	home := t.TempDir()
	writeEnvProfile(t, home, `{"api_key":"file-key","base_url":"https://file.example/v1","model":"m"}`)
	t.Setenv("CELESTE_API_KEY", "env-key")
	cfg, err := LoadNamed("envtest")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != "file-key" {
		t.Errorf("LoadNamed applied the env override (%q); it would be saved to disk", cfg.APIKey)
	}
}

// A config loaded with overrides can be saved later (collections pruning,
// /collections): the override must not reach any file.
func TestSavingAnEnvOverriddenConfigDoesNotWriteTheOverride(t *testing.T) {
	home := t.TempDir()
	writeEnvProfile(t, home, `{"api_key":"file-key","base_url":"https://file.example/v1","model":"m","tarot_auth_token":"file-tarot"}`)
	t.Setenv("CELESTE_API_KEY", "env-key")
	t.Setenv("CELESTE_API_ENDPOINT", "https://env.example/v1")
	t.Setenv("TAROT_AUTH_TOKEN", "env-tarot")
	cfg, err := LoadNamedWithEnv("envtest")
	if err != nil {
		t.Fatal(err)
	}
	copied := *cfg // chat paths copy the config by value
	copied.Model = "changed"
	if err := SaveNamed("envtest", &copied); err != nil {
		t.Fatal(err)
	}
	if err := Save(&copied); err != nil {
		t.Fatal(err)
	}
	if err := SaveSkillsConfig(&copied); err != nil {
		t.Fatal(err)
	}
	if err := SaveSecrets(&copied); err != nil {
		t.Fatal(err)
	}
	if copied.APIKey != "env-key" {
		t.Fatalf("saving changed the in-memory override: %q", copied.APIKey)
	}
	files, _ := filepath.Glob(filepath.Join(home, ".celeste", "*.json"))
	for _, f := range files {
		data, _ := os.ReadFile(f)
		for _, secret := range []string{"env-key", "env.example", "env-tarot"} {
			if strings.Contains(string(data), secret) {
				t.Errorf("%s holds an env override (%s)", filepath.Base(f), secret)
			}
		}
	}
	data, _ := os.ReadFile(NamedConfigPath("envtest"))
	if !strings.Contains(string(data), `"changed"`) || !strings.Contains(string(data), "file-key") {
		t.Errorf("the edit and the file's own key must be saved: %s", data)
	}
}

// A value the user changed after the override is theirs to save.
func TestSavingAChangedValueAfterOverrideKeepsTheChange(t *testing.T) {
	home := t.TempDir()
	writeEnvProfile(t, home, `{"api_key":"file-key","base_url":"https://file.example/v1","model":"m"}`)
	t.Setenv("CELESTE_API_KEY", "env-key")
	cfg, _ := LoadNamedWithEnv("envtest")
	cfg.APIKey = "typed-by-user"
	if err := SaveNamed("envtest", cfg); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(NamedConfigPath("envtest"))
	if !strings.Contains(string(data), "typed-by-user") {
		t.Errorf("a changed key must be saved: %s", data)
	}
}
