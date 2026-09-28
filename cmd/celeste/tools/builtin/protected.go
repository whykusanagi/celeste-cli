package builtin

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// protectedHookFile reports why path (an absolute path already resolved by
// resolvePath -- workspace-joined and filepath.Clean'd) names a hook/trust
// file no tool may write, or "" if it doesn't. Only resolvePath's forWrite
// callers (write_file, patch_file, splice_file) check this; read_file,
// search and list_files pass forWrite=false, so a person (and the model,
// read-only) can always inspect these files -- only writes are refused
// (2.0 F0 fix round 1).
//
// This is the real enforcement boundary for file tools, unlike
// permissions.Checker's bash-only substring rule (best-effort, unparsed
// text). Because it operates on the final resolved path, it catches `..`,
// `//`, and -- on case-insensitive filesystems -- case variants of
// ~/.celeste/hooks.json, ~/.celeste/grimoire.md and ~/.celeste/trusted.json,
// plus a ~/.celeste that is itself a symlink (e.g. a dotfile manager) via
// os.SameFile directory identity. Any path whose last two components are
// .celeste/trusted.json is refused wherever it is: the trust store only
// ever lives under home, so any spelling of it is suspicious.
func protectedHookFile(path string) string {
	clean := filepath.Clean(path)
	base := filepath.Base(clean)
	parentBase := filepath.Base(filepath.Dir(clean))
	reason := func() string {
		return fmt.Sprintf("%s is protected: hooks and hook trust are changed by you, not by tools", clean)
	}

	if pathElemEqual(base, "trusted.json") && pathElemEqual(parentBase, ".celeste") {
		return reason()
	}

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	celesteDir := filepath.Join(home, ".celeste")
	names := []string{"hooks.json", "grimoire.md", "trusted.json"}

	for _, name := range names {
		if pathEqual(clean, filepath.Join(celesteDir, name)) {
			return reason()
		}
	}

	// A symlinked ~/.celeste (dotfile manager): same physical directory,
	// reached by a different route than the ~/.celeste spelling.
	if dirInfo, err := os.Stat(filepath.Dir(clean)); err == nil {
		if celesteInfo, err := os.Stat(celesteDir); err == nil && os.SameFile(dirInfo, celesteInfo) {
			for _, name := range names {
				if pathElemEqual(base, name) {
					return reason()
				}
			}
		}
	}

	return ""
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
