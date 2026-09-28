package builtin

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// protectedHookFile reports why candidate (an absolute path already resolved
// by resolvePath -- workspace-joined and filepath.Clean'd) or realCandidate
// (the same path with any symlinks along it resolved -- resolvePath already
// computes this for its own workspace-escape check) names a hook/trust file
// no tool may write, or "" if neither does. Only resolvePath's forWrite
// callers (write_file, patch_file, splice_file) check this; read_file,
// search and list_files pass forWrite=false, so a person can always inspect
// these files -- only writes are refused (2.0 F0 fix round 2).
//
// Both a lexical AND a symlink-resolved comparison run (belt and braces):
// the lexical one catches a direct spelling of the protected file; the
// resolved one catches indirection in either direction -- a workspace path
// that is itself a symlink INTO ~/.celeste (an in-workspace link.json ->
// .celeste/hooks.json), ~/.celeste/hooks.json itself being a symlink OUT to
// a dotfiles repo (stow-style management -- writing the dotfiles copy
// directly still changes what ~/.celeste/hooks.json reads), and a
// not-yet-existing ~/.celeste reached through a symlinked ancestor (macOS's
// /var -> /private/var: HOME and the workspace can each be spelled through a
// different route to the same physical directory).
func protectedHookFile(candidate, realCandidate string) string {
	clean := filepath.Clean(candidate)
	real := filepath.Clean(realCandidate)
	spellings := []string{clean}
	if real != clean {
		spellings = append(spellings, real)
	}

	for _, s := range spellings {
		if reason := protectedByTrustedJSONShape(s); reason != "" {
			return reason
		}
	}

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	celesteDir := filepath.Join(home, ".celeste")
	names := []string{"hooks.json", "grimoire.md", "trusted.json"}

	for _, name := range names {
		lexicalTarget := filepath.Clean(filepath.Join(celesteDir, name))
		targets := []string{lexicalTarget, resolveProtectedTarget(lexicalTarget)}
		for _, target := range targets {
			for _, s := range spellings {
				if pathEqual(s, target) {
					return fmt.Sprintf("%s is protected: hooks and hook trust are changed by you, not by tools", clean)
				}
			}
		}
	}

	// A symlinked ~/.celeste directory itself (dotfile manager): same
	// physical parent directory reached by a different route.
	if celesteInfo, err := os.Stat(celesteDir); err == nil {
		for _, s := range spellings {
			if dirInfo, err := os.Stat(filepath.Dir(s)); err == nil && os.SameFile(dirInfo, celesteInfo) {
				base := filepath.Base(s)
				for _, name := range names {
					if pathElemEqual(base, name) {
						return fmt.Sprintf("%s is protected: hooks and hook trust are changed by you, not by tools", clean)
					}
				}
			}
		}
	}

	return ""
}

// protectedByTrustedJSONShape refuses any path whose last two components
// are .celeste/trusted.json, wherever it is: the trust store only ever
// lives under home, so any spelling of it is suspicious.
func protectedByTrustedJSONShape(path string) string {
	base := filepath.Base(path)
	parentBase := filepath.Base(filepath.Dir(path))
	if pathElemEqual(base, "trusted.json") && pathElemEqual(parentBase, ".celeste") {
		return fmt.Sprintf("%s is protected: hooks and hook trust are changed by you, not by tools", path)
	}
	return ""
}

// resolveProtectedTarget resolves target (a hook/trust file's canonical path
// under home) as far as symlinks lead, TOLERATING a dangling final symlink.
// This is deliberately more permissive than resolveExisting (used for the
// path being WRITTEN, where refusing to resolve past a dangling symlink is
// the correct, intentional behavior): a *protected* target that is itself a
// dangling symlink must still be recognized, or an attacker could point
// ~/.celeste/hooks.json at a file a tool is about to create for the first
// time and slip past this check entirely. If the leaf itself isn't a
// symlink (or doesn't exist at all yet), this falls back to resolveExisting,
// which correctly resolves a symlinked ANCESTOR directory (the /var vs
// /private/var case) even when the leaf file doesn't exist.
func resolveProtectedTarget(target string) string {
	clean := filepath.Clean(target)
	if info, err := os.Lstat(clean); err == nil && info.Mode()&os.ModeSymlink != 0 {
		dir := filepath.Dir(clean)
		if realDir, err := filepath.EvalSymlinks(dir); err == nil {
			if link, err := os.Readlink(clean); err == nil {
				if !filepath.IsAbs(link) {
					link = filepath.Join(realDir, link)
				}
				return filepath.Clean(link)
			}
		}
	}
	if resolved, err := resolveExisting(clean); err == nil {
		return resolved
	}
	return clean
}

// pathEqual and pathElemEqual compare the way the filesystem would: exactly
// on Linux, case-insensitively on darwin and windows (both ship
// case-insensitive-by-default filesystems).
func pathEqual(a, b string) bool {
	if runtime.GOOS == "linux" {
		return a == b
	}
	return strings.EqualFold(a, b)
}

func pathElemEqual(a, b string) bool { return pathEqual(a, b) }
