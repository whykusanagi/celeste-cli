package sandbox

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestProfileAllowsOnlyWritableDirs(t *testing.T) {
	p := Policy{Enabled: true, Workspace: "/ws", Writable: []string{"/a", "/ws"}, Network: true}
	prof := Profile(p)
	for _, want := range []string{
		"(version 1)",
		"(allow default)",
		"(deny file-write*)",
		`(subpath "/a")`,
		`(subpath "/ws")`,
		`(literal "/dev/null")`,
		`(literal "/dev/tty")`,
		`(regex #"^/dev/fd/")`,
	} {
		if !strings.Contains(prof, want) {
			t.Errorf("profile lacks %s:\n%s", want, prof)
		}
	}
	if strings.Count(prof, "(subpath ") != 2 {
		t.Errorf("want one subpath per writable dir:\n%s", prof)
	}
	if strings.Contains(prof, "network") {
		t.Errorf("network on: no network rules:\n%s", prof)
	}

	p.Network = false
	prof = Profile(p)
	if !strings.Contains(prof, "(deny network*)") || !strings.Contains(prof, "(allow network* (local unix-socket))") {
		t.Errorf("network off: both network lines:\n%s", prof)
	}
	if strings.Index(prof, "(deny network*)") > strings.Index(prof, "(allow network*") {
		t.Errorf("the unix-socket allowance must follow the deny (last rule wins):\n%s", prof)
	}
}

func TestProfileQuotesPaths(t *testing.T) {
	prof := Profile(Policy{Enabled: true, Writable: []string{`/x"y\z`}, Network: true})
	if !strings.Contains(prof, `(subpath "/x\"y\\z")`) {
		t.Fatalf("path not escaped:\n%s", prof)
	}
}

func TestDefaultWritableIncludesWorkspaceTempAndCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CACHE_HOME", "")
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".npm"), 0o755); err != nil {
		t.Fatal(err)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Skip(err)
	}
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	got := DefaultWritable(home, ws)
	for _, want := range []string{ws, os.TempDir(), cache, filepath.Join(home, ".npm")} {
		if !slices.Contains(got, Resolve(want)) {
			t.Errorf("DefaultWritable lacks %s: %v", want, got)
		}
	}
	if slices.Contains(got, Resolve(filepath.Join(home, ".cargo", "registry"))) {
		t.Errorf("a cache dir that does not exist is left out: %v", got)
	}
	if !slices.IsSorted(got) || len(slices.Compact(slices.Clone(got))) != len(got) {
		t.Errorf("not sorted and de-duplicated: %v", got)
	}
}

func TestResolveFollowsSymlinksAndKeepsMissingPaths(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip(err)
	}
	want, _ := filepath.EvalSymlinks(real)
	if got := Resolve(link); got != want {
		t.Fatalf("Resolve(link) = %s, want %s", got, want)
	}
	if got := Resolve(filepath.Join(link, "nope", "deeper")); got != filepath.Join(want, "nope", "deeper") {
		t.Fatalf("Resolve(link/nope/deeper) = %s, want it under %s", got, want)
	}
}
