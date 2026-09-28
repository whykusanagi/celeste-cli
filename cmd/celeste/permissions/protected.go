package permissions

import (
	"path/filepath"
	"strings"
)

// protectedFragments are spellings of the files a tool may never modify:
// the user's global hooks (trusted without approval) and the hook trust
// store. A tool that could write them could grant hooks to itself (2.0 F0).
// Repo .celeste/hooks.json is not listed: it is untrusted until approved,
// and any change re-prompts.
func protectedFragments(home string) []string {
	var out []string
	for _, rel := range []string{".celeste/hooks.json", ".celeste/grimoire.md", ".celeste/trusted.json"} {
		out = append(out, "~/"+rel, "$HOME/"+rel, "${HOME}/"+rel)
		if home != "" {
			out = append(out, filepath.ToSlash(filepath.Join(home, filepath.FromSlash(rel))))
		}
	}
	// The trust store only ever exists under home; any spelling of it is protected.
	return append(out, ".celeste/trusted.json")
}

// touchesProtected reports the first protected spelling found in any string
// anywhere in input (nested maps and lists included).
func touchesProtected(input map[string]any, fragments []string) (string, bool) {
	var hit string
	var walk func(v any) bool
	walk = func(v any) bool {
		switch x := v.(type) {
		case string:
			s := filepath.ToSlash(x)
			for _, f := range fragments {
				if strings.Contains(s, f) {
					hit = f
					return true
				}
			}
		case map[string]any:
			for _, e := range x {
				if walk(e) {
					return true
				}
			}
		case []any:
			for _, e := range x {
				if walk(e) {
					return true
				}
			}
		}
		return false
	}
	for _, v := range input {
		if walk(v) {
			return hit, true
		}
	}
	return "", false
}

func protectedPaths(home string) map[string]bool {
	paths := make(map[string]bool)
	if home == "" {
		return paths
	}
	for _, rel := range []string{".celeste/hooks.json", ".celeste/grimoire.md", ".celeste/trusted.json"} {
		paths[filepath.Clean(filepath.Join(home, filepath.FromSlash(rel)))] = true
	}
	return paths
}

// touchesProtectedRelative catches the case where the workspace itself is the
// home directory, so a repo-relative spelling names a global hook/trust file.
func touchesProtectedRelative(input map[string]any, home string, cwd string) (string, bool) {
	protected := protectedPaths(home)
	if len(protected) == 0 || cwd == "" {
		return "", false
	}
	cwd = filepath.Clean(cwd)
	var hit string
	var walk func(v any) bool
	walk = func(v any) bool {
		switch x := v.(type) {
		case string:
			if strings.HasPrefix(x, "~") || strings.HasPrefix(x, "$HOME") || strings.HasPrefix(x, "${HOME}") || filepath.IsAbs(x) {
				return false
			}
			resolved := filepath.Clean(filepath.Join(cwd, x))
			if protected[resolved] {
				hit = resolved
				return true
			}
		case map[string]any:
			for _, e := range x {
				if walk(e) {
					return true
				}
			}
		case []any:
			for _, e := range x {
				if walk(e) {
					return true
				}
			}
		}
		return false
	}
	for _, v := range input {
		if walk(v) {
			return hit, true
		}
	}
	return "", false
}
