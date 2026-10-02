//go:build unix

package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Review Focus 4: a file swapped for a symlink after resolvePath's check
// must not be read through.
func TestOpenNoFollowRefusesASwappedSymlink(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "ws")
	if err := os.Mkdir(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(root, "secret")
	if err := os.WriteFile(secret, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := filepath.Join(ws, "a.txt")
	if err := os.WriteFile(a, []byte("inside"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, real, err := resolvePathReal(ws, "a.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	// The swap, after the check.
	if err := os.Remove(a); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, a); err != nil {
		t.Fatal(err)
	}
	data, err := readFileNoFollow(real)
	if err == nil || len(data) != 0 {
		t.Fatalf("read through a swapped symlink: data=%q err=%v", data, err)
	}
	if !strings.Contains(err.Error(), "changed to a symlink") {
		t.Fatalf("error = %v", err)
	}
}

// A symlink that stays inside the workspace still reads: the read opens
// the resolved path, which is a regular file.
func TestReadToolsReadThroughResolvedPath(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "b.txt"), []byte("hello from b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(ws, "b.txt"), filepath.Join(ws, "link.txt")); err != nil {
		t.Fatal(err)
	}
	res, err := NewReadFileTool(ws).Execute(context.Background(), map[string]any{"path": "link.txt"}, nil)
	if err != nil || res.Error || !strings.Contains(res.Content, "hello from b") {
		t.Fatalf("read_file through an inner link: %+v err=%v", res, err)
	}
	res, err = NewPatchFileTool(ws).Execute(context.Background(), map[string]any{"path": "link.txt", "old_string": "hello", "new_string": "bye"}, nil)
	if err != nil || res.Error {
		t.Fatalf("patch_file through an inner link: %+v err=%v", res, err)
	}
	if b, _ := os.ReadFile(filepath.Join(ws, "b.txt")); string(b) != "bye from b\n" {
		t.Fatalf("b.txt = %q", b)
	}
}

// search must not read a file outside the workspace through a symlink
// inside it.
func TestSearchDoesNotFollowASymlinkOut(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "ws")
	if err := os.Mkdir(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "secret"), []byte("needle-outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "secret"), filepath.Join(ws, "out.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "in.txt"), []byte("needle-inside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(ws, "in.txt"), filepath.Join(ws, "inner-link.txt")); err != nil {
		t.Fatal(err)
	}
	res, err := NewSearchTool(ws).Execute(context.Background(), map[string]any{"pattern": "needle"}, nil)
	if err != nil || res.Error {
		t.Fatalf("search: %+v err=%v", res, err)
	}
	if strings.Contains(res.Content, "needle-outside") || !strings.Contains(res.Content, "needle-inside") {
		t.Fatalf("search content = %s", res.Content)
	}
	// Searching the inner link itself still reads its in-workspace target.
	res, err = NewSearchTool(ws).Execute(context.Background(), map[string]any{"pattern": "needle", "path": "inner-link.txt"}, nil)
	if err != nil || res.Error || !strings.Contains(res.Content, "needle-inside") {
		t.Fatalf("search of an inner link: %+v err=%v", res, err)
	}
}
