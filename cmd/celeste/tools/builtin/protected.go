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

// protectedWriteGuard is the kernel-level half of the protection above
// (2.0 F0 fix round 4). protectedHookFile reasons about path strings and
// symlink text, which cannot see every route the kernel takes: a dangling
// chain through a directory symlink whose target the write itself creates
// (~/.celeste/hooks.json -> L/x/hooks.json, L -> <workspace>/newdir), or
// link text with ".." after a symlinked component ("S/../hooks-real.json",
// which filepath.Clean collapses but the kernel resolves through S's
// target). The guard asks the filesystem instead: os.SameFile against what
// each protected name currently resolves to.
//
//   - Before writing, if the destination exists and is the same file as a
//     protected target, the write is refused (nothing has changed yet).
//   - If the destination did not exist, there is nothing to compare before
//     writing; after the write, if the new file turned out to be a protected
//     target, it is removed (along with any directories this write created)
//     and the write is refused.
//   - Creating directories can change what a protected name resolves to
//     without the written file being that target (fix round 5):
//     ~/L -> <workspace>/d/sub with sub missing, and hooks.json ->
//     ~/L/../evil.json. Writing d/evil.json is harmless while sub is
//     missing; writing d/sub/.keep then creates sub, and hooks.json starts
//     resolving to the planted file. mkdirAll therefore snapshots each
//     protected target before MkdirAll, and verify refuses the write (and
//     undoes it) if any target appeared or changed identity.
//
// This file-tool protection is defence in depth, not the trust boundary:
// bash can still reach these files until W4's sandbox lands.
type protectedWriteGuard struct {
	path    string
	existed bool
	dirGuard
}

// dirGuard is the directory-creation half of the guard (fix round 5),
// shared by write_file (through protectedWriteGuard) and every other tool
// that creates directories from a model-influenced path (through
// guardedMkdirAll).
type dirGuard struct {
	createdDirs []string // deepest first; only filled by mkdirAll
	// before holds os.Stat of each protected target taken right before
	// MkdirAll (nil entry = did not resolve); nil slice = no snapshot.
	before []os.FileInfo
}

var protectedHomeNames = []string{"hooks.json", "grimoire.md", "trusted.json"}

// statProtectedTargets stats what each ~/.celeste/<name> resolves to right
// now; a nil entry means it does not resolve.
func statProtectedTargets() []os.FileInfo {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	infos := make([]os.FileInfo, len(protectedHomeNames))
	for i, name := range protectedHomeNames {
		if info, err := os.Stat(filepath.Join(home, ".celeste", name)); err == nil {
			infos[i] = info
		}
	}
	return infos
}

// protectedTargetsChanged reports whether any protected target resolves
// now but did not before, or resolves to a different file than before.
// A hook file atomically replaced by the person mid-write (new inode) reads
// as changed too: a rare false deny, preferred over a missed plant.
func (g *dirGuard) protectedTargetsChanged() bool {
	if g.before == nil {
		return false
	}
	after := statProtectedTargets()
	if len(after) != len(g.before) {
		return false
	}
	for i, now := range after {
		was := g.before[i]
		if now == nil {
			continue // still missing, or gone: nothing now resolves to our write
		}
		if was == nil || !os.SameFile(was, now) {
			return true
		}
	}
	return false
}

// guardProtectedWrite runs the pre-write kernel check for path (already
// passed through resolvePath) and records whether it existed.
func guardProtectedWrite(path string) (*protectedWriteGuard, error) {
	g := &protectedWriteGuard{path: path}
	if _, err := os.Lstat(path); err == nil {
		g.existed = true
		if info, err := os.Stat(path); err == nil && sameAsProtectedTarget(info) {
			return nil, protectedError(path)
		}
	}
	return g, nil
}

// mkdirAll snapshots what each protected target resolves to, records which
// of dir and its ancestors do not exist yet (so a refused or failed write
// can remove exactly what it created), then runs os.MkdirAll(dir). The
// caller must still check protectedTargetsChanged afterwards, and undo on
// error; guardedMkdirAll bundles all of that.
func (g *dirGuard) mkdirAll(dir string) error {
	g.before = statProtectedTargets()
	g.createdDirs = nil
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Lstat(d); err == nil {
			break
		}
		g.createdDirs = append(g.createdDirs, d)
		if filepath.Dir(d) == d {
			break
		}
	}
	return os.MkdirAll(dir, 0755)
}

// removeCreatedDirs removes the directories mkdirAll recorded as missing,
// deepest first and non-recursively, so only while they are still empty.
func (g *dirGuard) removeCreatedDirs() {
	for _, dir := range g.createdDirs {
		_ = os.Remove(dir) // only succeeds while empty
	}
}

// guardedMkdirAll is os.MkdirAll(dir, 0755) for tools other than write_file
// whose directory comes from a model-influenced path (the TTS tools): if
// creating the directories changed what a protected name resolves to, the
// created directories are removed and the protected error is returned.
// undo removes the created directories (while empty); call it when the
// write that follows fails. It is never nil.
func guardedMkdirAll(dir string) (undo func(), err error) {
	g := &dirGuard{}
	if err := g.mkdirAll(dir); err != nil {
		g.removeCreatedDirs()
		return func() {}, err
	}
	if g.protectedTargetsChanged() {
		g.removeCreatedDirs()
		return func() {}, protectedError(dir)
	}
	return g.removeCreatedDirs, nil
}

// verify runs the post-write kernel check. If the freshly created file is a
// protected target, or the directories this write created changed what a
// protected name resolves to, the file and those directories are removed
// and the protected error is returned.
func (g *protectedWriteGuard) verify() error {
	if g.existed {
		return nil // the pre-write check was authoritative for this file
	}
	info, err := os.Stat(g.path)
	isTarget := err == nil && sameAsProtectedTarget(info)
	if !isTarget && !g.protectedTargetsChanged() {
		return nil
	}
	g.undo()
	return protectedError(g.path)
}

// undo removes what this write created: the destination file, only if it
// did not exist before, then the directories mkdirAll recorded as missing
// (deepest first, non-recursive, so only while empty). write_file
// defers it on every error return after MkdirAll (fix round 6): a write
// that fails between MkdirAll and verify must not leave behind directories
// that change what a protected name resolves to.
func (g *protectedWriteGuard) undo() {
	if !g.existed {
		_ = os.Remove(g.path)
	}
	g.removeCreatedDirs()
}

// sameAsProtectedTarget reports whether info is, per the kernel, the same
// file that ~/.celeste/{hooks.json,grimoire.md,trusted.json} resolve to.
func sameAsProtectedTarget(info os.FileInfo) bool {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return false
	}
	for _, name := range protectedHomeNames {
		target, err := os.Stat(filepath.Join(home, ".celeste", name))
		if err == nil && os.SameFile(info, target) {
			return true
		}
	}
	return false
}

func protectedError(path string) error {
	return fmt.Errorf("%s is protected: hooks and hook trust are changed by you, not by tools", path)
}
