package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// What appeared is removed, what changed is rewritten, and what did not
// change is left alone, a symlink included.
func TestRestorePathsPutsPointersBack(t *testing.T) {
	dir := Resolve(t.TempDir())
	kept := filepath.Join(dir, "kept")
	changed := filepath.Join(dir, "changed")
	planted := filepath.Join(dir, "commondir")
	plantedDir := filepath.Join(dir, ".git")
	link := filepath.Join(dir, "link")
	writeFile(t, kept, "a\n")
	writeFile(t, changed, "gitdir: x\n")
	if err := os.Symlink(kept, link); err != nil {
		t.Skip("no symlinks here:", err)
	}
	before := SnapshotPaths([]string{kept, changed, planted, plantedDir, link})
	if err := RestorePaths(before); err != nil {
		t.Fatalf("nothing changed: %v", err)
	}

	writeFile(t, changed, "gitdir: elsewhere\n")
	writeFile(t, planted, "../.fake\n")
	writeFile(t, filepath.Join(plantedDir, "config"), "[core]\n")
	err := RestorePaths(before)
	if err == nil || !strings.Contains(err.Error(), planted) || !strings.Contains(err.Error(), changed) || strings.Contains(err.Error(), kept) {
		t.Fatalf("RestorePaths = %v", err)
	}
	if b, _ := os.ReadFile(changed); string(b) != "gitdir: x\n" {
		t.Errorf("changed = %q", b)
	}
	for _, p := range []string{planted, plantedDir} {
		if _, err := os.Lstat(p); err == nil {
			t.Errorf("%s is still there", p)
		}
	}
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("the unchanged symlink was removed: %v", err)
	}
	if b, _ := os.ReadFile(kept); string(b) != "a\n" {
		t.Errorf("kept = %q", b)
	}
	if err := RestorePaths(before); err != nil {
		t.Fatalf("after restoring: %v", err)
	}
}
