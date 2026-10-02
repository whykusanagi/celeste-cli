package builtin

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/checkpoints"
)

// Review Focus 2 (tool level).
func TestPatchRequiresARead(t *testing.T) {
	ws := t.TempDir()
	p := filepath.Join(ws, "doc.md")
	if err := os.WriteFile(p, []byte("# Title\nold\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ft := checkpoints.NewFileTracker()
	patch := NewPatchFileTool(ws, WithPatchFileTracker(ft))
	read := NewReadFileTool(ws, WithReadFileTracker(ft))
	res, _ := patch.Execute(context.Background(), map[string]any{"path": "doc.md", "old_string": "old", "new_string": "new"}, nil)
	if !res.Error || !strings.Contains(res.Content, "read_file doc.md first") {
		t.Fatalf("result = %+v", res)
	}
	if b, _ := os.ReadFile(p); string(b) != "# Title\nold\n" {
		t.Fatal("the file must be untouched")
	}
	if res, _ := read.Execute(context.Background(), map[string]any{"path": "doc.md"}, nil); res.Error {
		t.Fatal(res.Content)
	}
	if res, _ := patch.Execute(context.Background(), map[string]any{"path": "doc.md", "old_string": "old", "new_string": "new"}, nil); res.Error {
		t.Fatal(res.Content)
	}
}

func TestWriteFileNewFileNeedsNoRead(t *testing.T) {
	ws := t.TempDir()
	tool := NewWriteFileTool(ws, WithWriteFileTracker(checkpoints.NewFileTracker()))
	if res, _ := tool.Execute(context.Background(), map[string]any{"path": "new.md", "content": "hi"}, nil); res.Error {
		t.Fatal(res.Content)
	}
	// Written in this session counts as read: a second write goes through.
	if res, _ := tool.Execute(context.Background(), map[string]any{"path": "new.md", "content": "again"}, nil); res.Error {
		t.Fatal(res.Content)
	}
}

func TestWriteFileOverwriteAndAppendNeedARead(t *testing.T) {
	ws := t.TempDir()
	p := filepath.Join(ws, "log.txt")
	if err := os.WriteFile(p, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := NewWriteFileTool(ws, WithWriteFileTracker(checkpoints.NewFileTracker()))
	for _, appendMode := range []bool{false, true} {
		res, _ := tool.Execute(context.Background(), map[string]any{"path": "log.txt", "content": "two\n", "append": appendMode}, nil)
		if !res.Error || !strings.Contains(res.Content, "read_file log.txt first") {
			t.Fatalf("append=%v: %+v", appendMode, res)
		}
	}
	if b, _ := os.ReadFile(p); string(b) != "one\n" {
		t.Fatalf("file = %q", b)
	}
}

func TestSpliceNeedsReadsOfExistingFiles(t *testing.T) {
	ws := t.TempDir()
	src, dst := filepath.Join(ws, "a.txt"), filepath.Join(ws, "b.txt")
	for p, body := range map[string]string{src: "a\nb\n", dst: "x\n"} {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ft := checkpoints.NewFileTracker()
	splice := NewSpliceFileTool(ws, WithSpliceFileTracker(ft))
	in := map[string]any{"op": "copy", "source": "a.txt", "dest": "b.txt", "start_line": 1, "end_line": 1}
	if res, _ := splice.Execute(context.Background(), in, nil); !res.Error || !strings.Contains(res.Content, "read_file a.txt first") {
		t.Fatalf("unread source: %+v", res)
	}
	_ = ft.RecordRead(src)
	if res, _ := splice.Execute(context.Background(), in, nil); !res.Error || !strings.Contains(res.Content, "read_file b.txt first") {
		t.Fatalf("unread existing dest: %+v", res)
	}
	// A new destination needs no read.
	in["dest"] = "c.txt"
	if res, _ := splice.Execute(context.Background(), in, nil); res.Error {
		t.Fatal(res.Content)
	}
	if b, _ := os.ReadFile(dst); string(b) != "x\n" {
		t.Fatalf("dest changed: %q", b)
	}
}

// Review Focus 4.
func TestAtomicWriteKeepsTheMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	for _, mode := range []os.FileMode{0o755, 0o600} {
		ws, tool, ft := patchEnv(t, map[string]string{"run.sh": "echo a\n"})
		p := filepath.Join(ws, "run.sh")
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
		_ = ft.RecordRead(p)
		if res, _ := tool.Execute(context.Background(), map[string]any{"path": "run.sh", "old_string": "a", "new_string": "b"}, nil); res.Error {
			t.Fatal(res.Content)
		}
		if info, _ := os.Stat(p); info.Mode().Perm() != mode {
			t.Fatalf("mode = %v, want %v", info.Mode().Perm(), mode)
		}
		entries, _ := os.ReadDir(ws)
		if len(entries) != 1 {
			t.Fatalf("a temp file was left behind: %v", entries)
		}
	}
}

// The write replaces the file by a rename: a reader holding the old file
// open keeps the old bytes, never a half-written mix.
func TestAtomicWriteReplacesByRename(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("an open file blocks the rename on Windows")
	}
	ws, tool, _ := patchEnv(t, map[string]string{"a.txt": "old\n"})
	p := filepath.Join(ws, "a.txt")
	before, _ := os.Stat(p)
	if res, _ := tool.Execute(context.Background(), map[string]any{"path": "a.txt", "old_string": "old", "new_string": "new"}, nil); res.Error {
		t.Fatal(res.Content)
	}
	after, _ := os.Stat(p)
	if os.SameFile(before, after) {
		t.Fatal("the file was rewritten in place, not replaced")
	}
}

// A symlink inside the workspace to another workspace file is edited
// through: the link stays a link and its target changes (CLAUDE.md ->
// AGENTS.md is common). The rename lands on the checked target, so a
// symlink swapped in at that path later is replaced, never written through.
func TestAtomicWriteKeepsWorkspaceSymlinks(t *testing.T) {
	ws, tool, ft := patchEnv(t, map[string]string{"AGENTS.md": "old\n"})
	link := filepath.Join(ws, "CLAUDE.md")
	if err := os.Symlink("AGENTS.md", link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_ = ft.RecordRead(link)
	if res, _ := tool.Execute(context.Background(), map[string]any{"path": "CLAUDE.md", "old_string": "old", "new_string": "new"}, nil); res.Error {
		t.Fatal(res.Content)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the symlink was replaced: %v %v", fi, err)
	}
	if b, _ := os.ReadFile(filepath.Join(ws, "AGENTS.md")); string(b) != "new\n" {
		t.Fatalf("target = %q", b)
	}
}

func TestAtomicWriteNewFileGetsDefaultMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "new.txt")
	if err := atomicWrite(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %v", fi.Mode().Perm())
	}
}

// Review M4: a read through one name of a file counts for an edit through
// another (a symlink to it, or a different case on a case-insensitive
// filesystem).
func TestReadCountsAcrossNamesOfOneFile(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "AGENTS.md"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("AGENTS.md", filepath.Join(ws, "CLAUDE.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	ft := checkpoints.NewFileTracker()
	read := NewReadFileTool(ws, WithReadFileTracker(ft))
	patch := NewPatchFileTool(ws, WithPatchFileTracker(ft))
	if res, _ := read.Execute(context.Background(), map[string]any{"path": "AGENTS.md"}, nil); res.Error {
		t.Fatal(res.Content)
	}
	if res, _ := patch.Execute(context.Background(), map[string]any{"path": "CLAUDE.md", "old_string": "old", "new_string": "new"}, nil); res.Error {
		t.Fatal(res.Content)
	}
	if b, _ := os.ReadFile(filepath.Join(ws, "AGENTS.md")); string(b) != "new\n" {
		t.Fatalf("got %q", b)
	}
}
