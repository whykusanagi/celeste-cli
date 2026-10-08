package grimoire

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Aikido review of #421: a directory of the include path swapped for a
// symlink out of the repository after the containment check must not make
// the include read the file it now leads to.
func TestIncludeDirectorySwappedAfterCheckStaysInRepo(t *testing.T) {
	_, repo, _ := confineEnv(t)
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mk(t, filepath.Join(repo, ".grimoire"), "## Incantations\n@./docs/a.md\n")
	mk(t, filepath.Join(repo, "docs", "a.md"), "INSIDE\n")
	outside := filepath.Join(filepath.Dir(repo), "outside")
	mk(t, filepath.Join(outside, "a.md"), "OUTSIDE-SECRET\n")

	swapped := false
	testHookIncludeChecked = func(string) {
		if swapped {
			return
		}
		swapped = true
		docs := filepath.Join(repo, "docs")
		if err := os.Rename(docs, docs+"-old"); err != nil {
			t.Fatal(err)
		}
		symlinkOrSkip(t, outside, docs)
	}
	t.Cleanup(func() { testHookIncludeChecked = nil })

	out := renderAll(t, repo)
	if !swapped {
		t.Fatal("the include was never checked")
	}
	if strings.Contains(out, "OUTSIDE-SECRET") {
		t.Fatalf("an include read a file outside the repository:\n%s", out)
	}
}

// The repository root itself swapped for a symlink after the containment
// check is refused when the root is opened, not followed.
func TestIncludeRepoRootSwappedAfterCheckIsRefused(t *testing.T) {
	_, repo, _ := confineEnv(t)
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mk(t, filepath.Join(repo, ".grimoire"), "## Incantations\n@./docs/a.md\n")
	mk(t, filepath.Join(repo, "docs", "a.md"), "INSIDE\n")
	evil := filepath.Join(filepath.Dir(repo), "evil")
	mk(t, filepath.Join(evil, "docs", "a.md"), "OUTSIDE-SECRET\n")

	swapped := false
	testHookIncludeChecked = func(string) {
		if swapped {
			return
		}
		swapped = true
		if err := os.Rename(repo, repo+"-old"); err != nil {
			t.Fatal(err)
		}
		symlinkOrSkip(t, evil, repo)
	}
	t.Cleanup(func() { testHookIncludeChecked = nil })

	out := renderAll(t, repo)
	if !swapped {
		t.Fatal("the include was never checked")
	}
	if strings.Contains(out, "OUTSIDE-SECRET") {
		t.Fatalf("an include read a file outside the repository:\n%s", out)
	}
}
