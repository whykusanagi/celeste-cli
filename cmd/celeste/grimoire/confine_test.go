package grimoire

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// confineEnv is a fake home holding a secret and a repo (no .git: the
// workspace is the root) that the secret must never reach.
func confineEnv(t *testing.T) (home, repo, secret string) {
	t.Helper()
	base := t.TempDir()
	home = filepath.Join(base, "home")
	repo = filepath.Join(base, "repo")
	secret = filepath.Join(home, "secret.yml")
	mk(t, secret, "token: SECRET-OUTSIDE\n")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home, repo, secret
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

func renderAll(t *testing.T, dir string) string {
	t.Helper()
	g, err := LoadAll(dir)
	if err != nil {
		t.Fatal(err)
	}
	return g.Render()
}

// Aikido 806869326: a repo .grimoire or fragment that is a symlink is not
// read; neither is a fragment directory that links out of the repo.
func TestLoadAllSkipsSymlinkedRepoSources(t *testing.T) {
	_, repo, _ := confineEnv(t)
	outside := filepath.Join(filepath.Dir(repo), "outside.md")
	mk(t, outside, "## Bindings\n- LEAKED-OUTSIDE\n")
	symlinkOrSkip(t, outside, filepath.Join(repo, ".grimoire"))
	symlinkOrSkip(t, outside, filepath.Join(repo, ".celeste", "grimoire", "a.md"))
	if out := renderAll(t, repo); strings.Contains(out, "LEAKED-OUTSIDE") {
		t.Fatalf("a symlinked repo grimoire was read:\n%s", out)
	}

	repo2 := filepath.Join(filepath.Dir(repo), "repo2")
	fragDir := filepath.Join(filepath.Dir(repo), "frags")
	mk(t, filepath.Join(fragDir, "b.md"), "## Bindings\n- LEAKED-DIR\n")
	symlinkOrSkip(t, fragDir, filepath.Join(repo2, ".celeste", "grimoire"))
	if out := renderAll(t, repo2); strings.Contains(out, "LEAKED-DIR") {
		t.Fatalf("a fragment directory linking out of the repo was read:\n%s", out)
	}
}

// Aikido 806869326: a repo grimoire's read is capped at MaxSize.
func TestLoadAllCapsRepoGrimoireSize(t *testing.T) {
	_, repo, _ := confineEnv(t)
	body := "## Bindings\n" + strings.Repeat("- filler line for the size cap\n", 4096)
	mk(t, filepath.Join(repo, ".grimoire"), body)
	if out := renderAll(t, repo); len(out) > MaxSize+1024 {
		t.Fatalf("rendered %d bytes from a %d byte grimoire, cap is %d", len(out), len(body), MaxSize)
	}
}

// Aikido 806869326 / 806781982: a repo grimoire cannot include from the
// home directory, above the repo, or through a symlink leaving the repo.
func TestRepoGrimoireIncludesStayInRepo(t *testing.T) {
	_, repo, secret := confineEnv(t)
	symlinkOrSkip(t, secret, filepath.Join(repo, "docs", "link.yml"))
	mk(t, filepath.Join(repo, "docs", "ok.md"), "INSIDE-OK")
	mk(t, filepath.Join(repo, ".grimoire"), strings.Join([]string{
		"## Incantations",
		"@~/secret.yml",
		"@./../home/secret.yml",
		"@./docs/link.yml",
		"@./docs/ok.md",
	}, "\n")+"\n")
	out := renderAll(t, repo)
	if strings.Contains(out, "SECRET-OUTSIDE") {
		t.Fatalf("a repo grimoire included a file outside the repo:\n%s", out)
	}
	if !strings.Contains(out, "INSIDE-OK") {
		t.Fatalf("an include inside the repo was dropped:\n%s", out)
	}
}

// Nested includes inherit the repo's confinement.
func TestRepoGrimoireNestedIncludesStayInRepo(t *testing.T) {
	_, repo, _ := confineEnv(t)
	mk(t, filepath.Join(repo, "a.md"), "A-INSIDE\n@~/secret.yml\n")
	mk(t, filepath.Join(repo, ".grimoire"), "## Incantations\n@./a.md\n")
	out := renderAll(t, repo)
	if strings.Contains(out, "SECRET-OUTSIDE") || !strings.Contains(out, "A-INSIDE") {
		t.Fatalf("nested include escaped or the parent was dropped:\n%s", out)
	}
}

// The user's own ~/.celeste/grimoire.md keeps @~/ includes.
func TestGlobalGrimoireKeepsHomeIncludes(t *testing.T) {
	home, repo, _ := confineEnv(t)
	mk(t, filepath.Join(home, "notes.md"), "GLOBAL-NOTE")
	mk(t, filepath.Join(home, ".celeste", "grimoire.md"), "## Incantations\n@~/notes.md\n")
	if out := renderAll(t, repo); !strings.Contains(out, "GLOBAL-NOTE") {
		t.Fatalf("the global grimoire lost its @~/ include:\n%s", out)
	}
}
