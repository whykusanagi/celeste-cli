package subagents

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Worktree is an isolated git worktree for a subagent.
type Worktree struct {
	Path   string
	Branch string
}

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// sanitizeWorktreeName returns a safe slug from name, keeping only ASCII
// letters, digits, hyphens, and underscores. Any other character (including
// path separators and spaces) is replaced with '-'. An empty result falls
// back to "subagent".
func sanitizeWorktreeName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	s := b.String()
	if s == "" {
		s = "subagent"
	}
	return s
}

// AddWorktree creates a worktree for `name` on a new branch under
// <repo>/.celeste/worktrees/<name>.
func AddWorktree(repo, name string) (*Worktree, error) {
	// Defensive: keep the worktree dir name to a safe slug so a caller-supplied
	// name can't escape the worktrees directory.
	name = sanitizeWorktreeName(name)
	branch := "subagent/" + name
	path := filepath.Join(repo, ".celeste", "worktrees", name)
	if _, err := runGit(repo, "worktree", "add", "-b", branch, path); err != nil {
		return nil, err
	}
	return &Worktree{Path: path, Branch: branch}, nil
}

// MergeWorktree merges the worktree's branch back into the repo's current branch.
// Returns an error on conflict so the caller can surface it.
func MergeWorktree(repo string, wt *Worktree) error {
	_, err := runGit(repo, "merge", "--no-edit", wt.Branch)
	return err
}

// RemoveWorktree force-removes the worktree dir and deletes its branch.
func RemoveWorktree(repo string, wt *Worktree) error {
	if _, err := runGit(repo, "worktree", "remove", "--force", wt.Path); err != nil {
		return err
	}
	_, _ = runGit(repo, "branch", "-D", wt.Branch) // best-effort branch cleanup
	return nil
}
