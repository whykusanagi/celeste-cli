package builtin

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func setProtectedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func TestWriteFileDeniesHomeHookWhenWorkspaceIsHome(t *testing.T) {
	home := setProtectedHome(t)
	result, err := NewWriteFileTool(home).Execute(context.Background(), map[string]any{
		"path":    ".celeste/hooks.json",
		"content": "{}",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Error || !strings.Contains(result.Content, "protected") {
		t.Fatalf("result = %+v, want protected error", result)
	}
	if _, err := os.Stat(filepath.Join(home, ".celeste", "hooks.json")); !os.IsNotExist(err) {
		t.Fatalf("protected file stat err = %v, want not exist", err)
	}
}

func TestWriteFileAllowsRepoHookOutsideHome(t *testing.T) {
	home := setProtectedHome(t)
	workspace := t.TempDir()
	result, err := NewWriteFileTool(workspace).Execute(context.Background(), map[string]any{
		"path":    ".celeste/hooks.json",
		"content": "{}",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Error {
		t.Fatalf("result = %+v, want success (home %s)", result, home)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".celeste", "hooks.json")); err != nil {
		t.Fatalf("repo hook was not written: %v", err)
	}
}

func TestWriteFileDeniesTraversalToHomeHook(t *testing.T) {
	home := setProtectedHome(t)
	result, err := NewWriteFileTool(home).Execute(context.Background(), map[string]any{
		"path":    "sub/../.celeste/hooks.json",
		"content": "{}",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Error || !strings.Contains(result.Content, "protected") {
		t.Fatalf("result = %+v, want protected error", result)
	}
}

func TestWriteFileDeniesDoubleSlashToHomeGrimoire(t *testing.T) {
	home := setProtectedHome(t)
	result, err := NewWriteFileTool(home).Execute(context.Background(), map[string]any{
		"path":    ".celeste//grimoire.md",
		"content": "spell",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Error || !strings.Contains(result.Content, "protected") {
		t.Fatalf("result = %+v, want protected error", result)
	}
}

func TestWriteFileDeniesCaseVariantsOnCaseInsensitiveFilesystems(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("Linux filesystems are treated as case-sensitive")
	}
	home := setProtectedHome(t)
	tool := NewWriteFileTool(home)

	cases := []string{
		".CELESTE/HOOKS.JSON",
		filepath.Join(home, ".Celeste", "hooks.json"),
	}
	for _, path := range cases {
		result, err := tool.Execute(context.Background(), map[string]any{
			"path":    path,
			"content": "{}",
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !result.Error || !strings.Contains(result.Content, "protected") {
			t.Fatalf("%s: result = %+v, want protected error", path, result)
		}
	}
}

func TestWriteFileDeniesSymlinkedHomeCelesteByDirectoryIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	home := setProtectedHome(t)
	realDir := filepath.Join(t.TempDir(), "dotfiles-celeste")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realDir, filepath.Join(home, ".celeste")); err != nil {
		t.Fatal(err)
	}

	result, err := NewWriteFileTool(filepath.Dir(realDir)).Execute(context.Background(), map[string]any{
		"path":    filepath.Join(filepath.Base(realDir), "hooks.json"),
		"content": "{}",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Error || !strings.Contains(result.Content, "protected") {
		t.Fatalf("result = %+v, want protected error", result)
	}
}

func TestReadFileAllowsHomeHook(t *testing.T) {
	home := setProtectedHome(t)
	path := filepath.Join(home, ".celeste", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := NewReadFileTool(home).Execute(context.Background(), map[string]any{
		"path": ".celeste/hooks.json",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Error || result.Metadata["content"] != `{"ok":true}` {
		t.Fatalf("result = %+v, want readable hook content", result)
	}
}

func TestPatchFileDeniesHomeHook(t *testing.T) {
	home := setProtectedHome(t)
	path := filepath.Join(home, ".celeste", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := NewPatchFileTool(home).Execute(context.Background(), map[string]any{
		"path":       ".celeste/hooks.json",
		"old_string": "old",
		"new_string": "new",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Error || !strings.Contains(result.Content, "protected") {
		t.Fatalf("result = %+v, want protected error", result)
	}
}

func TestSpliceFileProtectsDestAndMoveSourceButAllowsCopySource(t *testing.T) {
	home := setProtectedHome(t)
	hookPath := filepath.Join(home, ".celeste", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hookPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hookPath, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(home, "source.txt")
	if err := os.WriteFile(sourcePath, []byte("alpha\nbeta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := NewSpliceFileTool(home)

	destResult, err := tool.Execute(context.Background(), map[string]any{
		"op":         "copy",
		"source":     "source.txt",
		"dest":       ".celeste/hooks.json",
		"start_line": 1,
		"end_line":   1,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !destResult.Error || !strings.Contains(destResult.Content, "protected") {
		t.Fatalf("dest result = %+v, want protected error", destResult)
	}

	moveResult, err := tool.Execute(context.Background(), map[string]any{
		"op":         "move",
		"source":     ".celeste/hooks.json",
		"dest":       "moved.txt",
		"start_line": 1,
		"end_line":   1,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !moveResult.Error || !strings.Contains(moveResult.Content, "protected") {
		t.Fatalf("move result = %+v, want protected error", moveResult)
	}

	copyResult, err := tool.Execute(context.Background(), map[string]any{
		"op":         "copy",
		"source":     ".celeste/hooks.json",
		"dest":       "copied.txt",
		"start_line": 1,
		"end_line":   1,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if copyResult.Error {
		t.Fatalf("copy result = %+v, want success", copyResult)
	}
	if data, err := os.ReadFile(filepath.Join(home, "copied.txt")); err != nil || string(data) != "one\n" {
		t.Fatalf("copied data = %q, err = %v; want one line copied", string(data), err)
	}
}
