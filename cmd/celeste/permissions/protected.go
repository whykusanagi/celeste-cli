package permissions

import (
	"path/filepath"
	"strings"
)

// protectedFragments are spellings of the files a shell command must never
// name: the user's global hooks (trusted without approval) and the hook
// trust store. A command that could rewrite them could grant hooks to
// itself (2.0 F0).
//
// This is best-effort defence in depth for bash's `command` argument only
// (see checker.go's Step 0): a substring match on unparsed shell text
// cannot catch every spelling a shell would still expand to the same file
// (`..`, quoting, indirection) -- it is not the trust boundary. The real,
// exact enforcement for file tools (write_file, patch_file, splice_file) is
// tools/builtin's resolvePath, which resolves the actual path being
// written.
func protectedFragments(home string) []string {
	var out []string
	for _, rel := range []string{".celeste/hooks.json", ".celeste/grimoire.md", ".celeste/trusted.json"} {
		out = append(out, "~/"+rel, "$HOME/"+rel, "${HOME}/"+rel)
		if home != "" {
			out = append(out, filepath.ToSlash(filepath.Join(home, filepath.FromSlash(rel))))
		}
	}
	return append(out, ".celeste/trusted.json")
}

// commandNamesProtectedFile reports the first protected spelling found
// literally in cmd (a bash tool's `command` argument).
func commandNamesProtectedFile(cmd string, fragments []string) (string, bool) {
	s := filepath.ToSlash(cmd)
	for _, f := range fragments {
		if containsProtectedFragment(s, f) {
			return f, true
		}
	}
	return "", false
}

func containsProtectedFragment(s, fragment string) bool {
	start := 0
	for {
		idx := strings.Index(s[start:], fragment)
		if idx < 0 {
			return false
		}
		end := start + idx + len(fragment)
		if end == len(s) || isShellPathBoundary(s[end]) {
			return true
		}
		start = end
	}
}

func isShellPathBoundary(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '"', '\'', '`', ';', '&', '|', '<', '>', ')', '(', ']':
		return true
	default:
		return false
	}
}
