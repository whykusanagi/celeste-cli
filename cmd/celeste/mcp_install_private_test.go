package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// MCP client configs can hold server env values: a new one is owner-only,
// and the .bak of an owner-only file is too (Aikido 806869849).
func TestUpsertJSONConfigKeepsConfigsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	dir := t.TempDir()
	fresh := filepath.Join(dir, "new.json")
	if _, err := upsertJSONConfig(fresh, "celeste", map[string]any{"command": "/c"}, false); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(dir, "old.json")
	if err := os.WriteFile(existing, []byte(`{"mcpServers":{"x":{"env":{"T":"v"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing+".bak", []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := upsertJSONConfig(existing, "celeste", map[string]any{"command": "/c"}, false); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{fresh, existing, existing + ".bak"} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %v, want 0600", filepath.Base(p), fi.Mode().Perm())
		}
	}
}
