package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// MCP-server chat mode applies the user's permission rules: Trust mode lets
// ordinary writes through, but always-deny rules still hold (#187). It used
// to register the builtins with no checker at all.
func TestChatRegistryAppliesPermissionRules(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	workspace := t.TempDir()

	registry := newChatRegistry(workspace)

	res, err := registry.Execute(context.Background(), "bash", map[string]any{"command": "sudo ls"})
	if err != nil {
		t.Fatalf("execute bash: %v", err)
	}
	if !res.Error || !strings.Contains(res.Content, "Permission denied") {
		t.Errorf("sudo was not blocked by the default deny rule: %+v", res)
	}

	res, err = registry.Execute(context.Background(), "write_file", map[string]any{
		"path":    "note.txt",
		"content": "hello\n",
	})
	if err != nil {
		t.Fatalf("execute write_file: %v", err)
	}
	if res.Error {
		t.Fatalf("trust mode should allow write_file: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(workspace, "note.txt")); err != nil {
		t.Errorf("write_file did not write: %v", err)
	}
}
