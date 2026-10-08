package gitsafe

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/gittest"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/sandbox"
)

// A .git FindRepo refuses (here a .git file whose gitdir names another
// repository, not a linked worktree's admin dir) is not rediscovered by
// celeste's git: git is pointed at no repository and refuses to run, so
// worktree add cannot change the repository the pointer names (Aikido
// review of #422).
func TestRefusedGitPointerIsNotFollowed(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	other := sandbox.Resolve(t.TempDir())
	gittest.Run(t, other, "init", "-q")
	if err := os.WriteFile(filepath.Join(other, "f"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, other, "add", "f")
	gittest.Run(t, other, "commit", "-q", "-m", "a")

	ws := sandbox.Resolve(t.TempDir())
	if err := os.WriteFile(filepath.Join(ws, ".git"), []byte("gitdir: "+filepath.Join(other, ".git")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r, found := sandbox.FindRepo(ws); !found || r.GitDir != "" {
		t.Fatalf("FindRepo = %+v, %v; want the pointer refused", r, found)
	}

	env := Env(ws)
	if got, _ := envValue(env, "GIT_DIR"); got != os.DevNull {
		t.Errorf("GIT_DIR = %q, want %q", got, os.DevNull)
	}
	if _, ok := envValue(env, "GIT_WORK_TREE"); ok {
		t.Error("GIT_WORK_TREE set for a refused repository")
	}

	lane := filepath.Join(t.TempDir(), "lane")
	cmd, err := Command(context.Background(), ws, "worktree", "add", "-b", "lane", lane)
	if err == nil {
		if out, rerr := cmd.CombinedOutput(); rerr == nil {
			t.Errorf("worktree add ran through a refused .git:\n%s", out)
		}
	}
	if _, err := os.Stat(filepath.Join(other, ".git", "worktrees")); err == nil {
		t.Error("the other repository got worktree metadata")
	}
	if out := gittest.Run(t, other, "branch", "--list", "lane"); out != "" {
		t.Errorf("the other repository got branch lane: %q", out)
	}
}

// A linked worktree moved after git worktree add is refused (its admin
// dir still points at the old place), and the error says how to fix it:
// git worktree repair. After the repair it is accepted again.
func TestRefusedMovedWorktreeSuggestsRepair(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	base := sandbox.Resolve(t.TempDir())
	main := filepath.Join(base, "main")
	if err := os.Mkdir(main, 0o755); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, main, "init", "-q")
	if err := os.WriteFile(filepath.Join(main, "f"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, main, "add", "f")
	gittest.Run(t, main, "commit", "-q", "-m", "a")
	gittest.Run(t, main, "worktree", "add", "-q", filepath.Join(base, "wt"))
	moved := filepath.Join(base, "wtmoved")
	if err := os.Rename(filepath.Join(base, "wt"), moved); err != nil {
		t.Fatal(err)
	}

	_, _, err := Prepare(context.Background(), moved, "status")
	if err == nil {
		t.Fatal("a moved worktree whose admin dir points elsewhere was accepted")
	}
	if !strings.Contains(err.Error(), "git worktree repair") {
		t.Errorf("error does not suggest git worktree repair: %v", err)
	}

	gittest.Run(t, moved, "worktree", "repair")
	if _, _, err := Prepare(context.Background(), moved, "status"); err != nil {
		t.Errorf("repaired worktree still refused: %v", err)
	}
}

// A git init --separate-git-dir layout is refused with an error that says
// the layout is not supported.
func TestRefusedSeparateGitDirSaysUnsupported(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	base := sandbox.Resolve(t.TempDir())
	gittest.Run(t, base, "init", "-q", "--separate-git-dir="+filepath.Join(base, "sep"), "wt")
	_, _, err := Prepare(context.Background(), filepath.Join(base, "wt"), "status")
	if err == nil {
		t.Fatal("separate git dir accepted")
	}
	if !strings.Contains(err.Error(), "--separate-git-dir") || !strings.Contains(err.Error(), "not support") {
		t.Errorf("error does not say the layout is unsupported: %v", err)
	}
}
