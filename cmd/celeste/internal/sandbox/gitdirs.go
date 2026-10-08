package sandbox

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Repo is the repository a directory is in, as FindRepo found it.
type Repo struct {
	WorkTree  string // the directory holding .git
	DotGit    string // that .git, a directory or a file
	GitDir    string // "" when the .git was refused
	CommonDir string // the GitDir itself unless it is a linked worktree's
}

// FindRepo returns the repository dir is in, resolved: found is false
// when there is no .git in dir or above it. It reads the .git file or
// directory itself, as git does, rather than running git: the nearest
// ancestor's .git wins. The workspace's content controls that .git (a
// sandboxed command can write one), so nothing it names is believed
// unless git itself would have laid it out that way: a symlinked .git is
// refused; a .git file's "gitdir:" line must name a linked worktree's
// admin dir (<common>/worktrees/<name>, whose gitdir file points back to
// this .git and whose commondir is <common>, a real git dir) or a
// submodule's git dir (whose core.worktree points back to this
// directory). A refused .git gives a Repo with no GitDir. A plain .git
// directory is its own git dir and common dir; a commondir file in it is
// ignored.
func FindRepo(dir string) (r Repo, found bool) {
	for dir = Resolve(dir); ; dir = filepath.Dir(dir) {
		dotGit := filepath.Join(dir, ".git")
		info, err := os.Lstat(dotGit)
		if err == nil {
			r = Repo{WorkTree: dir, DotGit: dotGit}
			switch {
			case info.IsDir():
				r.GitDir, r.CommonDir = dotGit, dotGit
			case info.Mode().IsRegular():
				r.GitDir, r.CommonDir = gitFileDirs(dir, dotGit)
			}
			// Anything else (a symlink) is a .git git would not read.
			return r, true
		}
		if filepath.Dir(dir) == dir {
			return Repo{}, false
		}
	}
}

// GitDirs returns the git directories of the repository workspace is in
// (FindRepo), resolved and sorted: its git dir and, for a linked worktree,
// the repository's common dir (objects, refs). For a workspace at a plain
// repository's root they are inside it already; for a linked worktree (a
// subagent's isolated lane, or a user's own), a submodule or a
// subdirectory of a repository they are not, and without them git add and
// git commit fail under the sandbox. nil outside a repository and for a
// refused .git.
func GitDirs(workspace string) []string {
	r, _ := FindRepo(workspace)
	if r.GitDir == "" {
		return nil
	}
	return Normalize([]string{r.GitDir, r.CommonDir})
}

// GitProtected returns the paths in each of gitDirs that git outside the
// sandbox would run programs from, or would follow to another git dir,
// and that therefore stay read-only to sandboxed commands: config and
// config.worktree (core.fsmonitor, merge and filter drivers,
// core.hooksPath), hooks, and commondir and gitdir (git takes its config
// from the dir a commondir names, in any git dir, a plain .git included).
// A symlinked one is listed as the link and as its target, so neither can
// be replaced.
func GitProtected(gitDirs []string) []string {
	var out []string
	for _, d := range gitDirs {
		for _, name := range []string{"config", "config.worktree", "hooks", "commondir", "gitdir"} {
			p := filepath.Join(d, name)
			out = append(out, p, Resolve(p))
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// GitPointers returns the paths that tell git where workspace's git dirs
// are, which a sandboxed command must not change: each git dir's commondir
// and gitdir, the workspace repository's .git when it is a file, and
// workspace/.git when the repository's root is above the workspace (a
// .git there would be found first). They are read-only under the sandbox
// where they exist (ReadOnly); bubblewrap cannot bind over a missing one,
// so the runner also checks them after every sandboxed command
// (SnapshotPaths, RestorePaths). Resolved and sorted; nil outside a
// repository.
func GitPointers(workspace string) []string {
	r, found := FindRepo(workspace)
	if !found {
		return nil
	}
	var out []string
	for _, d := range []string{r.GitDir, r.CommonDir} {
		if d != "" {
			out = append(out, filepath.Join(d, "commondir"), filepath.Join(d, "gitdir"))
		}
	}
	if info, err := os.Lstat(r.DotGit); err == nil && info.Mode().IsRegular() {
		out = append(out, r.DotGit)
	}
	if ws := Resolve(workspace); ws != r.WorkTree {
		out = append(out, filepath.Join(ws, ".git"))
	}
	return Normalize(out)
}

// MakeGitProtected creates the GitProtected paths missing in each of
// gitDirs, as git would: an empty hooks directory and an empty
// config.worktree (read only when extensions.worktreeConfig is on), so
// bubblewrap, which can bind only over a path that exists, keeps them
// read-only too. config itself is never created. Errors are ignored: a
// path that cannot be created cannot be planted either.
func MakeGitProtected(gitDirs []string) {
	for _, d := range gitDirs {
		if info, err := os.Lstat(d); err != nil || !info.IsDir() {
			continue
		}
		_ = os.Mkdir(filepath.Join(d, "hooks"), 0o755)
		if f, err := os.OpenFile(filepath.Join(d, "config.worktree"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644); err == nil {
			_ = f.Close()
		}
	}
}

// gitFileDirs returns the git dir and common dir a ".git" file in dir
// names, "" unless they check out (FindRepo).
func gitFileDirs(dir, dotGit string) (gitDir, commonDir string) {
	named := readGitDirFile(dotGit)
	if named == "" {
		return "", ""
	}
	gitDir = Resolve(named)
	if !isGitDir(gitDir) {
		return "", ""
	}
	if filepath.Base(filepath.Dir(gitDir)) == "worktrees" {
		// A linked worktree's admin dir: <common>/worktrees/<name>.
		back := readRelative(gitDir, "gitdir")
		common := readRelative(gitDir, "commondir")
		want := filepath.Dir(filepath.Dir(gitDir))
		if back == "" || Resolve(back) != Resolve(dotGit) ||
			common == "" || Resolve(common) != want || !isGitDir(want) {
			return "", ""
		}
		return gitDir, want
	}
	// A submodule's git dir: no commondir, and core.worktree names dir.
	if _, err := os.Lstat(filepath.Join(gitDir, "commondir")); err == nil {
		return "", ""
	}
	wt := coreWorktree(filepath.Join(gitDir, "config"))
	if wt == "" {
		return "", ""
	}
	if !filepath.IsAbs(wt) {
		wt = filepath.Join(gitDir, wt)
	}
	if Resolve(wt) != dir {
		return "", ""
	}
	return gitDir, gitDir
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
