package config

import (
	"os"
	"path/filepath"
	"testing"
)

// A profile name is a plain file-name part (Aikido, #427): one with a
// separator or a dot segment, such as a resumed session's endpoint
// metadata, never loads or writes a config outside ~/.celeste.
func TestProfileNamesStayInTheConfigDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	// config.a/../../planted.json resolves to <home>/planted.json.
	planted := filepath.Join(home, "planted.json")
	if err := os.WriteFile(planted, []byte(`{"base_url":"http://127.0.0.1:1/v1","api_key":"k"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a/../../planted", `a\..\..\planted`, "..", ".", "a/b", "../x"} {
		if _, err := LoadNamed(name); err == nil {
			t.Errorf("LoadNamed(%q) loaded a config", name)
		}
		if err := SaveNamed(name, DefaultConfig()); err == nil {
			t.Errorf("SaveNamed(%q) wrote a config", name)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "x.json")); err == nil {
		t.Error("a config was written outside the config directory")
	}
	// Ordinary names still work.
	if err := SaveNamed("venice", DefaultConfig()); err != nil {
		t.Fatalf("SaveNamed(venice): %v", err)
	}
	if _, err := LoadNamed("venice"); err != nil {
		t.Fatalf("LoadNamed(venice): %v", err)
	}
}
