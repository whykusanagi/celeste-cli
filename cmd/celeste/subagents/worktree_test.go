package subagents

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func initRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestWorktreeAddRemove(t *testing.T) {
	repo := initRepo(t)
	wt, err := AddWorktree(repo, "fire")
	if err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Fatalf("worktree dir missing: %v", err)
	}
	if wt.Branch == "" {
		t.Fatal("worktree branch empty")
	}
	if err := RemoveWorktree(repo, wt); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree dir should be gone after remove")
	}
	_ = filepath.Join // keep import if unused otherwise
}

func TestMergeWorktree(t *testing.T) {
	repo := initRepo(t)
	wt, err := AddWorktree(repo, "water")
	if err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	// make a commit in the worktree
	newfile := filepath.Join(wt.Path, "out.txt")
	if err := os.WriteFile(newfile, []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "out.txt"}, {"commit", "-m", "work"}} {
		c := exec.Command("git", args...)
		c.Dir = wt.Path
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := MergeWorktree(repo, wt); err != nil {
		t.Fatalf("MergeWorktree: %v", err)
	}
	// after merge, out.txt should exist in the main repo
	if _, err := os.Stat(filepath.Join(repo, "out.txt")); err != nil {
		t.Fatalf("merged file missing in main repo: %v", err)
	}
	_ = RemoveWorktree(repo, wt)
}
