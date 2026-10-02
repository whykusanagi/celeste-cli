package grimoire

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The two containment checks in ContextFiles (the repository, then .git)
// keep their lexical boundaries: a sibling directory that only shares the
// repository's name prefix is outside it, a "..foo" directory and a ".git2"
// directory are not .git.
func TestContextFilesContainmentBoundaries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on windows")
	}
	cases := []struct {
		name, target string // target is relative to the repository root
		read         bool
		warn         string
	}{
		{"sibling sharing the repo prefix", filepath.Join("..", "repo2", "AGENTS.md"), false, "outside the repository"},
		{"parent directory", filepath.Join("..", "AGENTS.md"), false, "outside the repository"},
		{"dot-dot prefixed directory inside", filepath.Join("..foo", "AGENTS.md"), true, ""},
		{"directory sharing the .git prefix", filepath.Join(".git2", "AGENTS.md"), true, ""},
		{"inside .git", filepath.Join(".git", "x", "AGENTS.md"), false, "not AGENTS.md or CLAUDE.md"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			outer := t.TempDir()
			root := filepath.Join(outer, "repo")
			if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
				t.Fatal(err)
			}
			mk(t, filepath.Join(outer, "AGENTS.md"), "TARGET")
			mk(t, filepath.Join(outer, "repo2", "AGENTS.md"), "TARGET")
			mk(t, filepath.Join(root, "..foo", "AGENTS.md"), "TARGET")
			mk(t, filepath.Join(root, ".git2", "AGENTS.md"), "TARGET")
			mk(t, filepath.Join(root, ".git", "x", "AGENTS.md"), "TARGET")
			if err := os.Symlink(c.target, filepath.Join(root, "AGENTS.md")); err != nil {
				t.Skip(err)
			}
			files, warns := ContextFiles(root)
			read := strings.Contains(RenderContextFiles(files), "TARGET")
			if read != c.read {
				t.Fatalf("read = %v, want %v (warnings %v)", read, c.read, warns)
			}
			if c.warn == "" && len(warns) != 0 {
				t.Fatalf("unexpected warnings %v", warns)
			}
			if c.warn != "" && (len(warns) != 1 || !strings.Contains(warns[0], c.warn)) {
				t.Fatalf("warnings %v, want one containing %q", warns, c.warn)
			}
		})
	}
}
