package grimoire

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// CodeRabbit review of #421: a directory above a nested .git is not the
// user's own just for being above it. An extracted archive holding a
// .grimoire and, deeper, a .git must not get the user's include scope:
// its @~/ include is refused and a symlinked .grimoire there is not read.
func TestLoadAllArchiveAboveNestedRepoIsNotUserOwned(t *testing.T) {
	home, _, _ := confineEnv(t)
	archive := filepath.Join(home, "Downloads", "pkg")
	repo := filepath.Join(archive, "inner")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mk(t, filepath.Join(archive, ".grimoire"), "## Incantations\n@~/secret.yml\n")
	if out := renderAll(t, repo); strings.Contains(out, "SECRET-OUTSIDE") {
		t.Fatalf("a grimoire above a nested repository included a home file:\n%s", out)
	}

	outside := filepath.Join(filepath.Dir(home), "elsewhere.md")
	mk(t, outside, "## Bindings\n- LEAKED-ARCHIVE\n")
	if err := os.Remove(filepath.Join(archive, ".grimoire")); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, outside, filepath.Join(archive, ".grimoire"))
	if out := renderAll(t, repo); strings.Contains(out, "LEAKED-ARCHIVE") {
		t.Fatalf("a symlinked grimoire above a nested repository was read:\n%s", out)
	}
}

// The repository's own directory is never user-owned, also when it sits
// right under home.
func TestUserOwnedDirsRepoUnderHomeIsRepoContent(t *testing.T) {
	home, _, _ := confineEnv(t)
	repo := filepath.Join(home, "proj")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	owned := userOwnedDirs(repo)
	if owned(repo) {
		t.Fatal("the repository root was taken for a user-owned directory")
	}
	if !owned(home) {
		t.Fatal("home is user-owned")
	}
}
