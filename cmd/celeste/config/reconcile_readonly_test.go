package config

import (
	"os"
	"path/filepath"
	"testing"
)

// A profile the user made read-only is not rewritten by reconciliation:
// the atomic write replaces the file by rename, which the file's mode does
// not stop, so the mode is checked first.
func TestPersistReconciledLeavesAReadOnlyProfile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.work.json")
	const orig = `{"model": "old-model"}`
	if err := os.WriteFile(path, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	if err := persistReconciled(path, &Config{Model: "new-model"}); err != nil {
		t.Fatalf("persistReconciled: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != orig {
		t.Errorf("read-only profile rewritten: %s", got)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm()&0o200 != 0 {
		t.Errorf("mode = %v, want still read-only", info.Mode().Perm())
	}
}
