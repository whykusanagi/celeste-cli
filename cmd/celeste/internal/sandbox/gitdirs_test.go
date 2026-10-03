package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A linked worktree's git dir and the repository's common dir are outside
// the worktree: git add and git commit write there.
func TestGitDirsOfALinkedWorktree(t *testing.T) {
	repo := Resolve(t.TempDir())
	if err := os.MkdirAll(filepath.Join(repo, ".git", "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	lane := filepath.Join(repo, ".celeste", "worktrees", "fire")
	gitdir := filepath.Join(repo, ".git", "worktrees", "fire")
	writeFile(t, filepath.Join(lane, ".git"), "gitdir: "+gitdir+"\n")
	writeFile(t, filepath.Join(gitdir, "commondir"), "../..\n")

	got := GitDirs(filepath.Join(lane, "sub")) // a subdirectory finds it too
	if !slices.Equal(got, []string{filepath.Join(repo, ".git"), gitdir}) {
		t.Fatalf("GitDirs = %v", got)
	}
}

func TestGitDirsOfAPlainRepoAndNoRepo(t *testing.T) {
	repo := Resolve(t.TempDir())
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := GitDirs(filepath.Join(repo, "a", "b")); !slices.Equal(got, []string{filepath.Join(repo, ".git")}) {
		t.Fatalf("GitDirs(subdir) = %v", got)
	}
	// A relative gitdir: line resolves against the .git file's directory.
	sub := filepath.Join(repo, "mod")
	writeFile(t, filepath.Join(sub, ".git"), "gitdir: ../.git/modules/mod\n")
	if err := os.MkdirAll(filepath.Join(repo, ".git", "modules", "mod"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := GitDirs(sub); !slices.Equal(got, []string{filepath.Join(repo, ".git", "modules", "mod")}) {
		t.Fatalf("GitDirs(submodule) = %v", got)
	}
	if got := GitDirs(t.TempDir()); len(got) != 0 {
		// t.TempDir is under the system temp dir, never inside a repository.
		t.Fatalf("GitDirs(no repo) = %v", got)
	}
}

// Review Important 1: git commit in a linked worktree works under the
// sandbox when the git dirs are writable.
func TestGitCommitInALinkedWorktreeUnderTheSandbox(t *testing.T) {
	if _, ok := Available(); !ok {
		t.Skip("no OS sandbox here")
	}
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	home := Resolve(t.TempDir())
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") && !strings.HasPrefix(kv, "HOME=") {
			env = append(env, kv)
		}
	}
	env = append(env, "HOME="+home, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command(gitBin, args...)
		cmd.Dir, cmd.Env = dir, env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	repo := Resolve(t.TempDir())
	run(repo, "init", "-q", "-b", "main")
	run(repo, "commit", "-q", "--allow-empty", "-m", "root")
	lane := filepath.Join(repo, ".celeste", "worktrees", "fire")
	run(repo, "worktree", "add", "-q", "-b", "fire", lane)

	// The lane and its git dirs only: every test directory is under the temp
	// dir, which DefaultWritable allows, so it would hide a missing git dir.
	p := Policy{Enabled: true, Workspace: lane, Network: true,
		Writable: Normalize(append([]string{lane}, GitDirs(lane)...))}
	argv, _ := Wrap(p, "echo hi > f.txt && git add f.txt && git commit -q -m lane")
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir, cmd.Env = lane, env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sandboxed git commit: %v\n%s", err, out)
	}
	cmd = exec.Command(gitBin, "log", "--format=%s", "-1", "fire")
	cmd.Dir, cmd.Env = repo, env
	if out, err := cmd.Output(); err != nil || strings.TrimSpace(string(out)) != "lane" {
		t.Fatalf("the lane's commit is missing: %q %v", out, err)
	}
}
