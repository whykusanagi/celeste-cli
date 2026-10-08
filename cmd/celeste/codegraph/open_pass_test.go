package codegraph

import (
	"os"
	"path/filepath"
	"testing"
)

// A workspace that can no longer be opened (here replaced by a symlink
// after the indexer resolved it) fails Build and Update before the store
// is touched, instead of committing an empty or partial index.
func TestBuildAndUpdateFailWhenWorkspaceCannotBeOpened(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ws := filepath.Join(base, "ws")
	if err := os.Mkdir(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "a.py"), []byte("def helper():\n    return 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := NewIndexer(ws, filepath.Join(t.TempDir(), "cg.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	if err := idx.Build(); err != nil {
		t.Fatal(err)
	}
	files, err := idx.store.GetAllFiles()
	if err != nil || len(files) == 0 {
		t.Fatalf("first build stored %d files, %v", len(files), err)
	}

	other := filepath.Join(base, "other")
	if err := os.Rename(ws, other); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, ws); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if err := idx.Build(); err == nil {
		t.Error("Build succeeded with a workspace it cannot open")
	}
	if err := idx.Update(); err == nil {
		t.Error("Update succeeded with a workspace it cannot open")
	}
	after, err := idx.store.GetAllFiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(files) {
		t.Errorf("index changed: %d files before, %d after", len(files), len(after))
	}
	if v, _ := idx.store.GetMeta(metaBuildInProgress); len(v) != 0 {
		t.Error("a failed open left the build marked in progress")
	}
}

// The files a pass indexes are listed through the root it opened: a
// workspace directory replaced after the open does not decide what the
// pass indexes or drops (review of the round 3 branch).
func TestBuildWalksThePinnedRoot(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ws := filepath.Join(base, "ws")
	if err := os.Mkdir(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "a.py"), []byte("def helper():\n    return 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := NewIndexer(ws, filepath.Join(t.TempDir(), "cg.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	swapped := false
	testHookPassOpened = func() {
		if swapped {
			return
		}
		swapped = true
		if err := os.Rename(ws, ws+".old"); err != nil {
			t.Error(err)
			return
		}
		if err := os.Mkdir(ws, 0o755); err != nil {
			t.Error(err)
			return
		}
		if err := os.WriteFile(filepath.Join(ws, "b.py"), []byte("def other():\n    return 2\n"), 0o644); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { testHookPassOpened = nil })
	if err := idx.Build(); err != nil {
		t.Fatal(err)
	}
	if !swapped {
		t.Fatal("hook never ran")
	}
	files, err := idx.store.GetAllFiles()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range files {
		got = append(got, f.Path)
	}
	if len(got) != 1 || got[0] != "a.py" {
		t.Errorf("indexed %v, want the pinned root's [a.py]", got)
	}
}
