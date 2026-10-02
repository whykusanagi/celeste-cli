package sandbox

import (
	"os"
	"path/filepath"
	"strings"
)

// GitDirs returns the git directories of the repository workspace is in,
// resolved and sorted: its git dir and, for a linked worktree, the
// repository's common dir (objects, refs). For a workspace at a plain
// repository's root they are inside it already; for a linked worktree (a
// subagent's isolated lane, or a user's own) or a subdirectory of a
// repository they are not, and without them git add and git commit fail
// under the sandbox. nil outside a repository.
//
// It reads the .git file or directory itself, as git does, rather than
// running git: the nearest ancestor's .git wins, a "gitdir:" line is
// relative to the file's directory, and a "commondir" file in the git
// dir is relative to the git dir.
func GitDirs(workspace string) []string {
	for dir := Resolve(workspace); ; dir = filepath.Dir(dir) {
		dotGit := filepath.Join(dir, ".git")
		info, err := os.Stat(dotGit)
		if err == nil {
			gitDir := dotGit
			if !info.IsDir() {
				gitDir = readGitDirFile(dotGit)
				if gitDir == "" {
					return nil
				}
			}
			dirs := []string{gitDir}
			if b, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
				common := strings.TrimSpace(string(b))
				if common != "" {
					if !filepath.IsAbs(common) {
						common = filepath.Join(gitDir, common)
					}
					dirs = append(dirs, common)
				}
			}
			return Normalize(dirs)
		}
		if filepath.Dir(dir) == dir {
			return nil
		}
	}
}

// readGitDirFile returns the directory a ".git" file's "gitdir:" line
// names, absolute; "" when the file has none.
func readGitDirFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "gitdir:")
		if !ok {
			continue
		}
		dir := strings.TrimSpace(rest)
		if dir == "" {
			return ""
		}
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(filepath.Dir(path), dir)
		}
		return dir
	}
	return ""
}
