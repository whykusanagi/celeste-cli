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
	// ReadOnly stays read-only even inside a writable directory: each git
	// dir's config and hooks (GitProtected). Resolved.
	ReadOnly []string
	Network  bool
}

// buildCaches are the per-user build caches that are writable when they
// exist (spec §6.3: the sandbox must not break builds). Each tool's whole
// cache home, not just its download directory: go get writes
// $GOPATH/pkg/sumdb as well as pkg/mod, cargo takes a lock in $CARGO_HOME
// itself and gradle's wrapper and daemon live beside its caches. The
// environment's GOPATH (every entry), GOMODCACHE, CARGO_HOME and
// GRADLE_USER_HOME win over the defaults under home.
func buildCaches(home string) []string {
	var dirs []string
	inHome := func(rel ...string) string {
		if home == "" {
			return ""
		}
		return filepath.Join(append([]string{home}, rel...)...)
	}
	orHome := func(env string, rel ...string) string {
		if v := os.Getenv(env); v != "" {
			return v
		}
		return inHome(rel...)
	}
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		for _, g := range filepath.SplitList(gopath) {
			if g != "" {
				dirs = append(dirs, filepath.Join(g, "pkg"))
			}
		}
	} else {
		dirs = append(dirs, inHome("go", "pkg"))
	}
	dirs = append(dirs,
		os.Getenv("GOMODCACHE"),
		orHome("CARGO_HOME", ".cargo"),
		orHome("GRADLE_USER_HOME", ".gradle"),
		inHome(".npm"),
		inHome(".m2", "repository"),
	)
	return dirs
}

// DefaultWritable is the writable list for home and workspace: the
// workspace, the temp directories, the user cache directory and the build
// caches that exist (buildCaches). Resolved, de-duplicated and sorted.
func DefaultWritable(home, workspace string) []string {
	dirs := []string{workspace, os.TempDir(), "/tmp", "/private/tmp"}
	if cache, err := os.UserCacheDir(); err == nil {
		dirs = append(dirs, cache)
	}
	dirs = append(dirs, buildCaches(home)...)
	var out []string
	for _, d := range dirs {
		if d == "" {
			continue
		}
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
