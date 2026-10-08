package codegraph

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A file the walk listed and that was then replaced by a symlink to a file
// outside the workspace must not be parsed, hashed or type-checked: the
// indexing reads go through the workspace root and refuse anything but a
// regular file (Aikido review of #421).
func TestIndexingRefusesFileSwappedForSymlink(t *testing.T) {
	outside := t.TempDir()
	ws := t.TempDir()
	files := map[string]string{
		"a.go": "package a\n\nfunc OutsideOnlySecret() {}\n",
		"a.py": "def outside_only_secret():\n    pass\n",
		"a.ts": "export function outsideOnlySecret() {}\n",
		"a.rb": "def outside_only_secret\nend\n",
	}
	for name, body := range files {
		target := filepath.Join(outside, name)
		if err := os.WriteFile(target, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(ws, name)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}

	store, err := NewStore(filepath.Join(t.TempDir(), "idx.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	idx := NewIndexerWithStore(store, ws)

	for name := range files {
		res, err := idx.parseFile(name)
		if err == nil && res != nil {
			for _, s := range res.Symbols {
				if strings.Contains(strings.ToLower(s.Name), "outside") {
					t.Errorf("%s: parsed outside symbol %q through a symlink", name, s.Name)
				}
			}
			if len(res.Source) > 0 {
				t.Errorf("%s: read %d bytes through a symlink", name, len(res.Source))
			}
		}
		if h, err := idx.hashFile(name); err == nil {
			t.Errorf("%s: hashed through a symlink (%s)", name, h)
		}
	}

	res, err := analyzeGo(context.Background(), ws, []string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range res.files {
		for _, s := range f.symbols {
			if strings.Contains(s.Name, "Outside") {
				t.Errorf("type-checked Go pass read %q through a symlink", s.Name)
			}
		}
	}
}

// A regular workspace file is still parsed and hashed.
func TestIndexingReadsRegularWorkspaceFile(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "a.py"), []byte("def inside_fn():\n    pass\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(filepath.Join(t.TempDir(), "idx.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	idx := NewIndexerWithStore(store, ws)
	res, err := idx.parseFile("a.py")
	if err != nil || res == nil {
		t.Fatalf("parseFile: %v", err)
	}
	found := false
	for _, s := range res.Symbols {
		found = found || s.Name == "inside_fn"
	}
	if !found {
		t.Errorf("inside_fn not parsed: %+v", res.Symbols)
	}
	if _, err := idx.hashFile("a.py"); err != nil {
		t.Errorf("hashFile: %v", err)
	}
}

// The workspace, or a directory above it, replaced by a symlink after the
// indexer was opened on it is refused, not followed: the indexer keeps
// the real workspace it was opened on and opens it without following a
// symlink on its path.
func TestIndexingRefusesWorkspaceSwappedForSymlink(t *testing.T) {
	for _, swapParent := range []bool{false, true} {
		base, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		parent := filepath.Join(base, "p")
		ws := filepath.Join(parent, "ws")
		if err := os.MkdirAll(ws, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(ws, "a.py"), []byte("def inside_fn():\n    pass\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		evil := filepath.Join(base, "evil")
		if err := os.MkdirAll(filepath.Join(evil, "ws"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(evil, "ws", "a.py"), []byte("def outside_only_secret():\n    pass\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		store, err := NewStore(filepath.Join(t.TempDir(), "idx.db"))
		if err != nil {
			t.Fatal(err)
		}
		idx := NewIndexerWithStore(store, ws)

		swapped, target := ws, filepath.Join(evil, "ws")
		if swapParent {
			swapped, target = parent, evil
		}
		if err := os.Rename(swapped, swapped+".moved"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, swapped); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if res, err := idx.parseFile("a.py"); err == nil && res != nil {
			for _, s := range res.Symbols {
				if strings.Contains(s.Name, "outside") {
					t.Errorf("swapParent=%v: parsed %q through a replaced workspace", swapParent, s.Name)
				}
			}
		}
		store.Close()
	}
}
