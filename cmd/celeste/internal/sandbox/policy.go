// Package sandbox runs model-chosen shell commands under the operating
// system's sandbox (2.0 W4, #176): a seatbelt profile on macOS, bubblewrap
// on Linux. Writes are limited to the workspace, temp and cache
// directories; reads are not restricted; the network can be cut. Windows
// has no sandbox: commands run with the bash denylist alone.
package sandbox

import (
	"os"
	"path/filepath"
	"slices"
)

// DefaultEnabled is whether the sandbox is on when no config says
// otherwise. Off for 2.0 (opt in with "sandbox": {"enabled": true} in
// ~/.celeste/config.json), so upgrading never breaks a build that writes
// outside the workspace.
const DefaultEnabled = false

// Policy is what one shell command may do.
type Policy struct {
	Enabled   bool
	Workspace string   // resolved
	Writable  []string // resolved, de-duplicated, sorted; includes the defaults
	Network   bool
}

// buildCaches are the per-user build caches that are writable when they
// exist (spec §6.3: the sandbox must not break builds).
var buildCaches = []string{
	".npm",
	filepath.Join("go", "pkg", "mod"),
	filepath.Join(".cargo", "registry"),
	filepath.Join(".gradle", "caches"),
	filepath.Join(".m2", "repository"),
}

// DefaultWritable is the writable list for home and workspace: the
// workspace, the temp directories, the user cache directory and the build
// caches under home that exist. Resolved, de-duplicated and sorted.
func DefaultWritable(home, workspace string) []string {
	dirs := []string{workspace, os.TempDir(), "/tmp", "/private/tmp"}
	if cache, err := os.UserCacheDir(); err == nil {
		dirs = append(dirs, cache)
	}
	if home != "" {
		for _, c := range buildCaches {
			dirs = append(dirs, filepath.Join(home, c))
		}
	}
	var out []string
	for _, d := range dirs {
		if d == workspace {
			out = append(out, d)
			continue
		}
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			out = append(out, d)
		}
	}
	return Normalize(out)
}

// Resolve returns path made absolute, with symlinks resolved (the
// sandboxes compare real paths: /tmp is /private/tmp on macOS). For a
// path that does not exist yet, its deepest existing ancestor is resolved
// and the rest kept.
func Resolve(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	}
	rest := ""
	for dir := abs; ; dir = filepath.Dir(dir) {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(real, rest)
		}
		if filepath.Dir(dir) == dir {
			return abs
		}
		rest = filepath.Join(filepath.Base(dir), rest)
	}
}

// Normalize resolves every path, then sorts and de-duplicates the list.
func Normalize(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "" {
			continue
		}
		out = append(out, Resolve(p))
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// ForWorkspace is p for a run in another workspace (an isolated worktree):
// the new workspace replaces the old one in Writable; the rest carries over.
func (p Policy) ForWorkspace(workspace string) Policy {
	ws := Resolve(workspace)
	var writable []string
	for _, w := range p.Writable {
		if w != p.Workspace {
			writable = append(writable, w)
		}
	}
	p.Workspace = ws
	p.Writable = Normalize(append(writable, ws))
	return p
}
