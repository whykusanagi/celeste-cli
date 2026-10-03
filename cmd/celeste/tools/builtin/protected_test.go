package builtin

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
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

func TestWriteFileDeniesStowStyleHookSymlinkWithExistingTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	home := setProtectedHome(t)
	dotfiles := t.TempDir()
	target := filepath.Join(dotfiles, "celeste", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	homeCeleste := filepath.Join(home, ".celeste")
	if err := os.MkdirAll(homeCeleste, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(homeCeleste, "hooks.json")); err != nil {
		t.Fatal(err)
	}

	result, err := NewWriteFileTool(dotfiles).Execute(context.Background(), map[string]any{
		"path":    "celeste/hooks.json",
		"content": "rewritten\n",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Error || !strings.Contains(result.Content, "protected") {
		t.Fatalf("result = %+v, want protected error", result)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "original\n" {
		t.Fatalf("target content = %q, err = %v; want unchanged original", string(data), err)
	}
}

func TestWriteFileDeniesStowStyleHookSymlinkWithDanglingTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	home := setProtectedHome(t)
	dotfiles := t.TempDir()
	target := filepath.Join(dotfiles, "celeste", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	homeCeleste := filepath.Join(home, ".celeste")
	if err := os.MkdirAll(homeCeleste, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(homeCeleste, "hooks.json")); err != nil {
		t.Fatal(err)
	}

	result, err := NewWriteFileTool(dotfiles).Execute(context.Background(), map[string]any{
		"path":    "celeste/hooks.json",
		"content": "created\n",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Error || !strings.Contains(result.Content, "protected") {
		t.Fatalf("result = %+v, want protected error", result)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("target stat err = %v, want not exist", err)
	}
}

func TestWriteFileDeniesWorkspaceSymlinkToHomeHook(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	home := setProtectedHome(t)
	hookPath := filepath.Join(home, ".celeste", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hookPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hookPath, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(hookPath, filepath.Join(home, "link.json")); err != nil {
		t.Fatal(err)
	}

	result, err := NewWriteFileTool(home).Execute(context.Background(), map[string]any{
		"path":    "link.json",
		"content": "rewritten\n",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Error || !strings.Contains(result.Content, "protected") {
		t.Fatalf("result = %+v, want protected error", result)
	}
	if data, err := os.ReadFile(hookPath); err != nil || string(data) != "original\n" {
		t.Fatalf("hook content = %q, err = %v; want unchanged original", string(data), err)
	}
}

func TestPatchFileDeniesWorkspaceSymlinkToHomeHook(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	home := setProtectedHome(t)
	hookPath := filepath.Join(home, ".celeste", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hookPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hookPath, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(hookPath, filepath.Join(home, "link.json")); err != nil {
		t.Fatal(err)
	}

	result, err := NewPatchFileTool(home).Execute(context.Background(), map[string]any{
		"path":       "link.json",
		"old_string": "original",
		"new_string": "rewritten",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Error || !strings.Contains(result.Content, "protected") {
		t.Fatalf("result = %+v, want protected error", result)
	}
	if data, err := os.ReadFile(hookPath); err != nil || string(data) != "original\n" {
		t.Fatalf("hook content = %q, err = %v; want unchanged original", string(data), err)
	}
}

func TestWriteFileDeniesHomeHookThroughDifferentSymlinkSpellings(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	root := t.TempDir()
	realHome := filepath.Join(root, "real-home")
	if err := os.MkdirAll(realHome, 0o755); err != nil {
		t.Fatal(err)
	}
	homeAlias := filepath.Join(root, "home-alias")
	if err := os.Symlink(realHome, homeAlias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", homeAlias)
	t.Setenv("USERPROFILE", homeAlias)

	result, err := NewWriteFileTool(realHome).Execute(context.Background(), map[string]any{
		"path":    ".celeste/grimoire.md",
		"content": "spell\n",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Error || !strings.Contains(result.Content, "protected") {
		t.Fatalf("result = %+v, want protected error", result)
	}
	if _, err := os.Stat(filepath.Join(realHome, ".celeste", "grimoire.md")); !os.IsNotExist(err) {
		t.Fatalf("protected file stat err = %v, want not exist", err)
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

func TestWriteFileDeniesStowStyleHookWithResolvedWorkspaceSpelling(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	home := setProtectedHome(t)
	root := t.TempDir()
	realDotfiles := filepath.Join(root, "real-dotfiles")
	if err := os.MkdirAll(filepath.Join(realDotfiles, "celeste"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(realDotfiles, "celeste", "hooks.json")
	if err := os.WriteFile(target, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dotfilesAlias := filepath.Join(root, "dotfiles-alias")
	if err := os.Symlink(realDotfiles, dotfilesAlias); err != nil {
		t.Fatal(err)
	}
	homeCeleste := filepath.Join(home, ".celeste")
	if err := os.MkdirAll(homeCeleste, 0o755); err != nil {
		t.Fatal(err)
	}
	// hooks.json's link TEXT goes through the alias spelling; the workspace
	// below is given as the already-resolved spelling -- the reported bypass.
	if err := os.Symlink(filepath.Join(dotfilesAlias, "celeste", "hooks.json"), filepath.Join(homeCeleste, "hooks.json")); err != nil {
		t.Fatal(err)
	}

	result, err := NewWriteFileTool(realDotfiles).Execute(context.Background(), map[string]any{
		"path":    "celeste/hooks.json",
		"content": "rewritten\n",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Error || !strings.Contains(result.Content, "protected") {
		t.Fatalf("result = %+v, want protected error", result)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "original\n" {
		t.Fatalf("target content = %q, err = %v; want unchanged original", string(data), err)
	}
}

func TestWriteFileDeniesTwoLinkChainWithFinalPresent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	home := setProtectedHome(t)
	work := t.TempDir()
	bPath := filepath.Join(work, "B.json")
	if err := os.WriteFile(bPath, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	aPath := filepath.Join(work, "A.json")
	if err := os.Symlink(bPath, aPath); err != nil {
		t.Fatal(err)
	}
	homeCeleste := filepath.Join(home, ".celeste")
	if err := os.MkdirAll(homeCeleste, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(aPath, filepath.Join(homeCeleste, "hooks.json")); err != nil {
		t.Fatal(err)
	}

	resultB, err := NewWriteFileTool(work).Execute(context.Background(), map[string]any{
		"path":    "B.json",
		"content": "pwned-b\n",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !resultB.Error || !strings.Contains(resultB.Content, "protected") {
		t.Fatalf("B result = %+v, want protected error", resultB)
	}
	if data, _ := os.ReadFile(bPath); string(data) != "original\n" {
		t.Fatalf("B content = %q, want unchanged", data)
	}

	// Writing to A (an intermediate hop) must be denied too.
	resultA, err := NewWriteFileTool(work).Execute(context.Background(), map[string]any{
		"path":    "A.json",
		"content": "pwned-a\n",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !resultA.Error || !strings.Contains(resultA.Content, "protected") {
		t.Fatalf("A result = %+v, want protected error", resultA)
	}
}

func TestWriteFileDeniesTwoLinkChainWithDanglingFinal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	home := setProtectedHome(t)
	work := t.TempDir()
	bPath := filepath.Join(work, "B.json") // never created: dangling until the write
	aPath := filepath.Join(work, "A.json")
	if err := os.Symlink(bPath, aPath); err != nil {
		t.Fatal(err)
	}
	homeCeleste := filepath.Join(home, ".celeste")
	if err := os.MkdirAll(homeCeleste, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(aPath, filepath.Join(homeCeleste, "hooks.json")); err != nil {
		t.Fatal(err)
	}

	resultB, err := NewWriteFileTool(work).Execute(context.Background(), map[string]any{
		"path":    "B.json",
		"content": "created-b\n",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !resultB.Error || !strings.Contains(resultB.Content, "protected") {
		t.Fatalf("B result = %+v, want protected error", resultB)
	}
	if _, err := os.Stat(bPath); !os.IsNotExist(err) {
		t.Fatalf("B stat err = %v, want not exist", err)
	}

	// Writing to A (itself a dangling-chain symlink, since B doesn't exist)
	// must also be denied -- here it's caught by resolvePath's pre-existing,
	// unrelated dangling-symlink escape check (A's own EvalSymlinks fails
	// because B doesn't exist yet), not necessarily the "protected" message,
	// so only assert that it's an error.
	resultA, err := NewWriteFileTool(work).Execute(context.Background(), map[string]any{
		"path":    "A.json",
		"content": "created-a\n",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !resultA.Error {
		t.Fatalf("A result = %+v, want an error", resultA)
	}
}

func TestWriteFileDeniesSymlinkedParentDirOfDotfilesTargetPresent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	home := setProtectedHome(t)
	root := t.TempDir()
	realDir := filepath.Join(root, "R")
	if err := os.MkdirAll(filepath.Join(realDir, "celeste"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(realDir, "celeste", "hooks.json")
	if err := os.WriteFile(target, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dSymlink := filepath.Join(root, "D")
	if err := os.Symlink(realDir, dSymlink); err != nil {
		t.Fatal(err)
	}
	homeCeleste := filepath.Join(home, ".celeste")
	if err := os.MkdirAll(homeCeleste, 0o755); err != nil {
		t.Fatal(err)
	}
	// hooks.json -> D/celeste/hooks.json (D, not R, is the spelling used).
	if err := os.Symlink(filepath.Join(dSymlink, "celeste", "hooks.json"), filepath.Join(homeCeleste, "hooks.json")); err != nil {
		t.Fatal(err)
	}

	result, err := NewWriteFileTool(realDir).Execute(context.Background(), map[string]any{
		"path":    "celeste/hooks.json",
		"content": "pwned\n",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Error || !strings.Contains(result.Content, "protected") {
		t.Fatalf("result = %+v, want protected error", result)
	}
	if data, _ := os.ReadFile(target); string(data) != "original\n" {
		t.Fatalf("target content = %q, want unchanged", data)
	}
}

func TestWriteFileDeniesSymlinkedParentDirOfDotfilesTargetDangling(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	home := setProtectedHome(t)
	root := t.TempDir()
	realDir := filepath.Join(root, "R")
	if err := os.MkdirAll(filepath.Join(realDir, "celeste"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(realDir, "celeste", "hooks.json") // never created
	dSymlink := filepath.Join(root, "D")
	if err := os.Symlink(realDir, dSymlink); err != nil {
		t.Fatal(err)
	}
	homeCeleste := filepath.Join(home, ".celeste")
	if err := os.MkdirAll(homeCeleste, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dSymlink, "celeste", "hooks.json"), filepath.Join(homeCeleste, "hooks.json")); err != nil {
		t.Fatal(err)
	}

	result, err := NewWriteFileTool(realDir).Execute(context.Background(), map[string]any{
		"path":    "celeste/hooks.json",
		"content": "created\n",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Error || !strings.Contains(result.Content, "protected") {
		t.Fatalf("result = %+v, want protected error", result)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("target stat err = %v, want not exist", err)
	}
}

func TestWriteFileAllowsUnrelatedFilesNearSymlinkChains(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	home := setProtectedHome(t)
	work := t.TempDir()
	bPath := filepath.Join(work, "B.json")
	if err := os.WriteFile(bPath, []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	aPath := filepath.Join(work, "A.json")
	if err := os.Symlink(bPath, aPath); err != nil {
		t.Fatal(err)
	}
	homeCeleste := filepath.Join(home, ".celeste")
	if err := os.MkdirAll(homeCeleste, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(aPath, filepath.Join(homeCeleste, "hooks.json")); err != nil {
		t.Fatal(err)
	}

	result, err := NewWriteFileTool(work).Execute(context.Background(), map[string]any{
		"path":    "notes.md",
		"content": "unrelated\n",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Error {
		t.Fatalf("result = %+v, want success for an unrelated file", result)
	}
}

func TestWriteFileSymlinkCycleDoesNotHangOrDenyUnrelatedWrites(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	home := setProtectedHome(t)
	work := t.TempDir()
	aPath := filepath.Join(work, "a.json")
	bPath := filepath.Join(work, "b.json")
	if err := os.Symlink(bPath, aPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(aPath, bPath); err != nil {
		t.Fatal(err)
	}
	homeCeleste := filepath.Join(home, ".celeste")
	if err := os.MkdirAll(homeCeleste, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(aPath, filepath.Join(homeCeleste, "hooks.json")); err != nil {
		t.Fatal(err)
	}

	done := make(chan tools.ToolResult, 1)
	go func() {
		result, _ := NewWriteFileTool(work).Execute(context.Background(), map[string]any{
			"path":    "notes.md",
			"content": "unrelated\n",
		}, nil)
		done <- result
	}()
	select {
	case result := <-done:
		if result.Error {
			t.Fatalf("result = %+v, want success for an unrelated file", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("write_file did not return: a symlink cycle likely caused a hang")
	}
}
