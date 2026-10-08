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
// subagent's isolated lane, or a user's own), a submodule or a
// subdirectory of a repository they are not, and without them git add and
// git commit fail under the sandbox. nil outside a repository.
//
// It reads the .git file or directory itself, as git does, rather than
// running git: the nearest ancestor's .git wins. The workspace's content
// controls that .git (a sandboxed command can write one), so nothing it
// names is believed unless git itself would have laid it out that way: a
// symlinked .git is refused; a .git file's "gitdir:" line must name a
// linked worktree's admin dir (<common>/worktrees/<name>, whose gitdir
// file points back to this .git and whose commondir is <common>, a real
// git dir) or a submodule's git dir (whose core.worktree points back to
// this directory). Anything else gives nil. A plain .git directory is its
// own git dir; a commondir file in it is ignored.
func GitDirs(workspace string) []string {
	for dir := Resolve(workspace); ; dir = filepath.Dir(dir) {
		dotGit := filepath.Join(dir, ".git")
		info, err := os.Lstat(dotGit)
		if err == nil {
			switch {
			case info.IsDir():
				return Normalize([]string{dotGit})
			case info.Mode().IsRegular():
				return Normalize(gitFileDirs(dir, dotGit))
			}
			return nil // a symlink, or anything else git would not read
		}
		if filepath.Dir(dir) == dir {
			return nil
		}
	}
}

// gitFileDirs returns the git dirs a ".git" file in dir names, nil
// unless they check out (GitDirs).
func gitFileDirs(dir, dotGit string) []string {
	named := readGitDirFile(dotGit)
	if named == "" {
		return nil
	}
	gitDir := Resolve(named)
	if !isGitDir(gitDir) {
		return nil
	}
	if filepath.Base(filepath.Dir(gitDir)) == "worktrees" {
		// A linked worktree's admin dir: <common>/worktrees/<name>.
		back := readRelative(gitDir, "gitdir")
		common := readRelative(gitDir, "commondir")
		want := filepath.Dir(filepath.Dir(gitDir))
		if back == "" || Resolve(back) != Resolve(dotGit) ||
			common == "" || Resolve(common) != want || !isGitDir(want) {
			return nil
		}
		return []string{gitDir, want}
	}
	// A submodule's git dir: no commondir, and core.worktree names dir.
	if _, err := os.Lstat(filepath.Join(gitDir, "commondir")); err == nil {
		return nil
	}
	wt := coreWorktree(filepath.Join(gitDir, "config"))
	if wt == "" {
		return nil
	}
	if !filepath.IsAbs(wt) {
		wt = filepath.Join(gitDir, wt)
	}
	if Resolve(wt) != dir {
		return nil
	}
	return []string{gitDir}
}

// isGitDir reports a directory holding HEAD and objects, as every real git
// dir does.
func isGitDir(dir string) bool {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "HEAD")); err != nil {
		return false
	}
	// A linked worktree's admin dir has no objects of its own.
	if filepath.Base(filepath.Dir(dir)) == "worktrees" {
		return true
	}
	info, err := os.Stat(filepath.Join(dir, "objects"))
	return err == nil && info.IsDir()
}

// readRelative returns the path held in the file name inside dir, made
// absolute against dir; "" when there is none.
func readRelative(dir, name string) string {
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return ""
	}
	p := strings.TrimSpace(string(b))
	if p == "" {
		return ""
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	return p
}

// coreWorktree returns core.worktree from the git config file at path,
// read just enough for a submodule's config; "" when it is not set.
func coreWorktree(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	section, value := "", ""
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "" || line[0] == '#' || line[0] == ';':
			continue
		case line[0] == '[':
			section = strings.ToLower(strings.TrimSpace(strings.Trim(line, "[]")))
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if ok && section == "core" && strings.EqualFold(strings.TrimSpace(key), "worktree") {
			value = strings.Trim(strings.TrimSpace(val), `"`)
		}
	}
	return value
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
