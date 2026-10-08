package subagents

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/gittest"
)

func initRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	gittest.Run(t, dir, "init")
	gittest.Run(t, dir, "commit", "--allow-empty", "-m", "init")
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
	gittest.Run(t, wt.Path, "add", "out.txt")
	gittest.Run(t, wt.Path, "commit", "-m", "work")
	if err := MergeWorktree(repo, wt); err != nil {
		t.Fatalf("MergeWorktree: %v", err)
	}
	// after merge, out.txt should exist in the main repo
	if _, err := os.Stat(filepath.Join(repo, "out.txt")); err != nil {
		t.Fatalf("merged file missing in main repo: %v", err)
	}
	_ = RemoveWorktree(repo, wt)
}

// Aikido 806869318: the parent's merge of a lane runs none of the
// programs the repository's git config or hooks name. A lane shares the
// repository's config and hooks, so a sandboxed command in it could plant
// them for the unsandboxed merge. Configured merge drivers are replaced by
// git's own text merge, so the merge still succeeds.
func TestMergeWorktreeRunsNoRepositoryPrograms(t *testing.T) {
	repo := initRepo(t)
	marker := filepath.Join(t.TempDir(), "ran")
	write := func(path, body string, mode os.FileMode) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(repo, "f.txt"), "one\ntwo\nthree\nfour\nfive\n", 0o644)
	write(filepath.Join(repo, ".gitattributes"), "*.txt merge=custom\n", 0o644)
	gittest.Run(t, repo, "add", ".")
	gittest.Run(t, repo, "commit", "-m", "base")
	wt, err := AddWorktree(repo, "earth")
	if err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	defer func() { _ = RemoveWorktree(repo, wt) }()
	write(filepath.Join(wt.Path, "f.txt"), "one\ntwo\nthree\nfour\nFIVE\n", 0o644)
	gittest.Run(t, wt.Path, "commit", "-am", "lane")
	write(filepath.Join(repo, "f.txt"), "ONE\ntwo\nthree\nfour\nfive\n", 0o644)
	gittest.Run(t, repo, "commit", "-am", "main")

	script := "#!/bin/sh\necho x >> '" + marker + "'\n"
	gittest.Run(t, repo, "config", "merge.custom.driver", "echo x >> '"+marker+"'; false")
	gittest.Run(t, repo, "config", "core.fsmonitor", "echo x >> '"+marker+"'; false")
	for _, hook := range []string{"pre-merge-commit", "prepare-commit-msg", "commit-msg", "post-merge"} {
		write(filepath.Join(repo, ".git", "hooks", hook), script, 0o755)
	}
	if err := MergeWorktree(repo, wt); err != nil {
		t.Fatalf("MergeWorktree: %v", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the merge ran a program from the repository's config or hooks")
	}
	got, err := os.ReadFile(filepath.Join(repo, "f.txt"))
	if err != nil || string(got) != "ONE\ntwo\nthree\nfour\nFIVE\n" {
		t.Fatalf("merged f.txt = %q, %v", got, err)
	}
}
