package grimoire

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// A grimoire over MaxSize is cut on a rune boundary, never mid-character.
func TestReadSourceCutsOnRuneBoundary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".grimoire")
	// "## Bindings\n" is 12 bytes; pad so a 3-byte rune straddles MaxSize.
	head := "## Bindings\n- "
	pad := strings.Repeat("a", MaxSize-len(head)-1)
	mk(t, path, head+pad+strings.Repeat("あ", 16)+"\n")
	data, err := readSource(GrimoireSource{Path: path, dir: dir}, false)
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.ValidString(data) {
		t.Fatalf("cut left invalid UTF-8 at the end: % x", data[len(data)-4:])
	}
	if len(data) > MaxSize {
		t.Fatalf("read %d bytes, cap is %d", len(data), MaxSize)
	}
}

// Grimoires above the git root (~/.grimoire, a parent directory) and the
// ~/.celeste/grimoire fragments are the user's own, not repository
// content: a dotfile-managed symlink is still read.
func TestLoadAllReadsUserOwnedSymlinkedGrimoireAboveRepo(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	repo := filepath.Join(home, "dev", "proj")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	dot := filepath.Join(home, "dotfiles", "grimoire")
	mk(t, dot, "## Bindings\n- FROM-DOTFILES\n")
	symlinkOrSkip(t, dot, filepath.Join(home, ".grimoire"))
	frag := filepath.Join(home, "dotfiles", "frag.md")
	mk(t, frag, "## Bindings\n- FROM-HOME-FRAGMENT\n")
	symlinkOrSkip(t, frag, filepath.Join(home, ".celeste", "grimoire", "f.md"))
	parent := filepath.Join(home, "dotfiles", "dev-grimoire")
	mk(t, parent, "## Bindings\n- FROM-PARENT\n")
	symlinkOrSkip(t, parent, filepath.Join(home, "dev", ".grimoire"))

	out := renderAll(t, repo)
	for _, want := range []string{"FROM-DOTFILES", "FROM-HOME-FRAGMENT", "FROM-PARENT"} {
		if !strings.Contains(out, want) {
			t.Errorf("user-owned grimoire %s not read:\n%s", want, out)
		}
	}

	// The repository's own grimoire still may not be a symlink.
	inRepo := filepath.Join(base, "elsewhere.md")
	mk(t, inRepo, "## Bindings\n- LEAKED-REPO\n")
	symlinkOrSkip(t, inRepo, filepath.Join(repo, ".grimoire"))
	if out := renderAll(t, repo); strings.Contains(out, "LEAKED-REPO") {
		t.Fatalf("a symlinked repository grimoire was read:\n%s", out)
	}
}
