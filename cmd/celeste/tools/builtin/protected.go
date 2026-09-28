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
		for _, target := range protectedTargetsFor(home, name) {
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

// protectedTargetsFor returns every path that names home's <name> hook/trust
// file: the lexical ~/.celeste/<name>, and every hop of its symlink chain --
// not just the final destination. Writing directly to an intermediate hop
// changes what the chain's end reads, exactly as writing the end itself
// would, so each hop is protected too.
//
// filepath.EvalSymlinks resolves a WHOLE chain (any number of hops, and any
// symlinked directory anywhere inside it, like a symlinked parent
// directory of the final target) in one call -- but only when every hop's
// target fully exists; it refuses a chain with a dangling link anywhere
// (same as resolveExisting, for the same reason: it can't canonicalize a
// path that doesn't fully exist). Since a stow-managed ~/.celeste/hooks.json
// can be set up before the file it names has been created, this also walks
// the chain by hand -- capped at 40 hops against a cycle -- so a dangling
// final (or intermediate) target is still recognized.
func protectedTargetsFor(home, name string) []string {
	lexical := filepath.Clean(filepath.Join(home, ".celeste", name))
	targets := []string{lexical}

	if resolved, err := filepath.EvalSymlinks(lexical); err == nil {
		return append(targets, filepath.Clean(resolved))
	}

	cur := lexical
	seen := map[string]bool{cur: true}
	for hop := 0; hop < 40; hop++ {
		info, err := os.Lstat(cur)
		if err != nil {
			// cur doesn't exist at all, not even as a dangling symlink:
			// resolve as far as the existing prefix goes and stop.
			if resolved, err := resolveExisting(cur); err == nil {
				targets = append(targets, filepath.Clean(resolved))
			}
			return targets
		}
		if info.Mode()&os.ModeSymlink == 0 {
			// cur exists and isn't itself a symlink: the chain's real end.
			// Its own directory may still be reached through a symlink.
			if resolved, err := resolveExisting(cur); err == nil {
				targets = append(targets, filepath.Clean(resolved))
			} else {
				targets = append(targets, cur)
			}
			return targets
		}

		link, err := os.Readlink(cur)
		if err != nil {
			return targets
		}
		dir := filepath.Dir(cur)
		resolvedDir := dir
		if d, err := filepath.EvalSymlinks(dir); err == nil {
			resolvedDir = d
		} else if d, err := resolveExisting(dir); err == nil {
			resolvedDir = d
		}
		if !filepath.IsAbs(link) {
			link = filepath.Join(resolvedDir, link)
		}
		cur = filepath.Clean(link)
		if seen[cur] {
			return targets // cyclic chain; stop rather than loop forever
		}
		seen[cur] = true
		targets = append(targets, cur) // this hop stays protected too
	}
	return targets
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
