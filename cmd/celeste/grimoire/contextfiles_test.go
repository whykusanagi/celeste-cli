package grimoire

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func mk(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestContextFilesWalkToTheGitRoot(t *testing.T) {
	outer := t.TempDir()
	root := filepath.Join(outer, "repo")
	ws := filepath.Join(root, "services", "api")
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mk(t, filepath.Join(outer, "AGENTS.md"), "ABOVE THE ROOT")
	mk(t, filepath.Join(root, "AGENTS.md"), "root agents")
	mk(t, filepath.Join(root, "CLAUDE.md"), "root claude")
	mk(t, filepath.Join(root, "services", "AGENTS.md"), "services agents")
	mk(t, filepath.Join(ws, "main.go"), "package main")

	files, warns := ContextFiles(ws)
	if len(warns) != 0 {
		t.Fatalf("warnings: %v", warns)
	}
	var got []string
	for _, f := range files {
		got = append(got, filepath.ToSlash(f.Rel)+"="+f.Content)
	}
	want := "AGENTS.md=root agents,CLAUDE.md=root claude,services/AGENTS.md=services agents"
	if strings.Join(got, ",") != want {
		t.Fatalf("got %s, want %s", strings.Join(got, ","), want)
	}
	out := RenderContextFiles(files)
	if !strings.HasPrefix(out, "# Project instructions\n") || !strings.Contains(out, "the grimoire wins") || strings.Contains(out, "ABOVE THE ROOT") {
		t.Fatalf("render:\n%s", out)
	}
}

func TestContextFilesWithoutAGitRootReadTheWorkspaceOnly(t *testing.T) {
	outer := t.TempDir()
	ws := filepath.Join(outer, "ws")
	mk(t, filepath.Join(outer, "AGENTS.md"), "parent")
	mk(t, filepath.Join(ws, "CLAUDE.md"), "here")
	files, _ := ContextFiles(ws)
	if len(files) != 1 || files[0].Content != "here" {
		t.Fatalf("files = %+v", files)
	}
}

func TestContextFilesAreCapped(t *testing.T) {
	ws := t.TempDir()
	mk(t, filepath.Join(ws, "AGENTS.md"), strings.Repeat("é", 40<<10)) // 80 KiB of 2-byte runes
	files, warns := ContextFiles(ws)
	if len(files) != 1 || len(files[0].Content) > contextFileCap || files[0].Truncated == 0 || len(warns) != 1 {
		t.Fatalf("cap not applied: files=%d warns=%v", len(files), warns)
	}
	if !strings.HasSuffix(strings.TrimSpace(RenderContextFiles(files)), "bytes]") {
		t.Error("the render must mark the cut")
	}
}

func TestContextFilesTotalCap(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	ws := filepath.Join(root, "a", "b")
	big := strings.Repeat("x", 40<<10)
	mk(t, filepath.Join(root, "AGENTS.md"), big)
	mk(t, filepath.Join(root, "CLAUDE.md"), big)
	mk(t, filepath.Join(root, "a", "AGENTS.md"), big)
	mk(t, filepath.Join(ws, "AGENTS.md"), big)
	files, warns := ContextFiles(ws)
	total := 0
	for _, f := range files {
		total += len(f.Content)
	}
	if total > contextTotalCap {
		t.Fatalf("total %d exceeds %d", total, contextTotalCap)
	}
	if len(warns) == 0 {
		t.Fatal("cut files must warn")
	}
}

func TestContextFilesSkipNonRegularFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink to /dev/zero is unix only")
	}
	ws := t.TempDir()
	if err := os.Symlink("/dev/zero", filepath.Join(ws, "AGENTS.md")); err != nil {
		t.Skip(err)
	}
	files, _ := ContextFiles(ws)
	if len(files) != 0 {
		t.Fatalf("a device must not be read: %+v", files[0].Rel)
	}
}

func TestGitRootAcceptsAGitFile(t *testing.T) {
	root := t.TempDir()
	mk(t, filepath.Join(root, ".git"), "gitdir: elsewhere\n")
	sub := filepath.Join(root, "x")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got, ok := GitRoot(sub)
	want, _ := filepath.Abs(root)
	if !ok || got != want {
		t.Fatalf("GitRoot = %q, %v; want %q", got, ok, want)
	}
}

func TestContextFilesCutOnARuneBoundary(t *testing.T) {
	ws := t.TempDir()
	mk(t, filepath.Join(ws, "AGENTS.md"), "a"+strings.Repeat("é", 20<<10)) // odd offset: the cap splits a rune
	files, _ := ContextFiles(ws)
	if len(files) != 1 || !utf8ValidString(files[0].Content) || len(files[0].Content) != contextFileCap-1 {
		t.Fatalf("bad cut: files=%d", len(files))
	}
}

func TestContextFilesSkipBinary(t *testing.T) {
	ws := t.TempDir()
	mk(t, filepath.Join(ws, "CLAUDE.md"), "\xff\xfe\x00binary")
	files, warns := ContextFiles(ws)
	if len(files) != 0 || len(warns) != 1 {
		t.Fatalf("binary file read: files=%d warns=%v", len(files), warns)
	}
}

func utf8ValidString(s string) bool { return strings.ToValidUTF8(s, "") == s }
