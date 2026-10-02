package selfupdate

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/selfupdate/selfupdatetest"
)

const tag = "v2.0.0"

var official = []byte("official celeste v2.0.0")

// writeExe makes a stand-in for the running executable and returns its
// symlink-free path (t.TempDir is under a symlink on macOS).
func writeExe(t *testing.T, name string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, name)
	if err := os.WriteFile(exe, []byte("go install build"), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

func testUpdater(s *selfupdatetest.Server, key *selfupdatetest.Key, exe string) *Updater {
	u := New()
	u.Base = s.URL
	u.Hosts = []string{s.Host()}
	u.Transport = s.Client().Transport
	u.PublicKey = key.Public
	u.GOOS, u.GOARCH = "linux", "amd64"
	u.Exe = exe
	return u
}

func release(t *testing.T) (*selfupdatetest.Key, *selfupdatetest.Server) {
	t.Helper()
	key := selfupdatetest.NewKey(t, time.Now().Add(-time.Hour), 0)
	return key, selfupdatetest.Serve(t, tag, selfupdatetest.Build(t, key, tag, official))
}

func assertUnchanged(t *testing.T, exe string) {
	t.Helper()
	if b, err := os.ReadFile(exe); err != nil || string(b) != "go install build" {
		t.Fatalf("the executable changed: %q, %v", b, err)
	}
	assertNoTempFiles(t, exe)
}

func assertNoTempFiles(t *testing.T, exe string) {
	t.Helper()
	entries, _ := os.ReadDir(filepath.Dir(exe))
	for _, e := range entries {
		if strings.Contains(e.Name(), ".new-") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

// Review Focus 11.
func TestUpgradeInstallsTheVerifiedBinary(t *testing.T) {
	key, s := release(t)
	exe := writeExe(t, "celeste")
	got, err := testUpdater(s, key, exe).Upgrade(context.Background(), tag)
	if err != nil {
		t.Fatal(err)
	}
	if got != exe {
		t.Fatalf("installed at %s, want %s", got, exe)
	}
	b, _ := os.ReadFile(exe)
	if !bytes.Equal(b, official) {
		t.Fatalf("exe = %q", b)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(exe); fi.Mode().Perm() != 0o755 {
			t.Fatalf("mode %v", fi.Mode())
		}
	}
	assertNoTempFiles(t, exe)
}

func TestUpgradeReplacesTheSymlinkTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	key, s := release(t)
	exe := writeExe(t, "celeste-real")
	link := filepath.Join(filepath.Dir(exe), "celeste")
	if err := os.Symlink(exe, link); err != nil {
		t.Fatal(err)
	}
	if _, err := testUpdater(s, key, link).Upgrade(context.Background(), tag); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Lstat(link); fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink was replaced instead of its target")
	}
	if b, _ := os.ReadFile(exe); !bytes.Equal(b, official) {
		t.Fatalf("target = %q", b)
	}
}

// Review Focus 9: every failed check leaves the executable byte-identical.
func TestUpgradeRejectsAnUnverifiedRelease(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, key *selfupdatetest.Key, files map[string][]byte)
		want   error // nil: any error
	}{
		{"checksums signed by another key", func(t *testing.T, _ *selfupdatetest.Key, f map[string][]byte) {
			f["checksums.txt.asc"] = selfupdatetest.NewKey(t, time.Now().Add(-time.Hour), 0).Sign(t, f["checksums.txt"], time.Time{})
		}, ErrBadSignature},
		{"checksums edited after signing", func(t *testing.T, _ *selfupdatetest.Key, f map[string][]byte) {
			f["checksums.txt"] = append(f["checksums.txt"], '\n')
		}, ErrBadSignature},
		{"no checksums signature", func(t *testing.T, _ *selfupdatetest.Key, f map[string][]byte) {
			delete(f, "checksums.txt.asc")
		}, nil},
		{"manifest signed by another key", func(t *testing.T, _ *selfupdatetest.Key, f map[string][]byte) {
			f["manifest.json.asc"] = selfupdatetest.NewKey(t, time.Now().Add(-time.Hour), 0).Sign(t, f["manifest.json"], time.Time{})
		}, ErrBadSignature},
		{"a correctly signed manifest for another tag", func(t *testing.T, key *selfupdatetest.Key, f map[string][]byte) {
			f["manifest.json"] = []byte(`{"tag": "v1.16.0"}`)
			selfupdatetest.Resign(t, key, f)
		}, ErrManifest},
		{"a signed manifest that does not list this archive", func(t *testing.T, key *selfupdatetest.Key, f map[string][]byte) {
			f["manifest.json"] = []byte(`{"tag": "v2.0.0", "artifacts": []}`)
			selfupdatetest.Resign(t, key, f)
		}, ErrManifest},
		{"archive swapped after signing", func(t *testing.T, _ *selfupdatetest.Key, f map[string][]byte) {
			f["celeste-linux-amd64.tar.gz"] = selfupdatetest.TarGz(t, map[string][]byte{"celeste-linux-amd64": []byte("evil")})
		}, ErrChecksum},
		{"signed archive without the binary", func(t *testing.T, key *selfupdatetest.Key, f map[string][]byte) {
			f["celeste-linux-amd64.tar.gz"] = selfupdatetest.TarGz(t, map[string][]byte{"celeste": []byte("x")})
			selfupdatetest.Resign(t, key, f)
		}, ErrBadArchive},
		{"this platform not in the checksums", func(t *testing.T, key *selfupdatetest.Key, f map[string][]byte) {
			delete(f, "celeste-linux-amd64.tar.gz")
			selfupdatetest.Resign(t, key, f)
		}, ErrChecksum},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			key := selfupdatetest.NewKey(t, time.Now().Add(-time.Hour), 0)
			files := selfupdatetest.Build(t, key, tag, official)
			c.mutate(t, key, files)
			s := selfupdatetest.Serve(t, tag, files)
			exe := writeExe(t, "celeste")
			_, err := testUpdater(s, key, exe).Upgrade(context.Background(), tag)
			if err == nil || (c.want != nil && !errors.Is(err, c.want)) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			assertUnchanged(t, exe)
		})
	}
}

// Ruling 26: the signed manifest binds the download to its version. Someone
// who can replace release assets but not sign them copies an older signed
// release's checksums, their signature and its archive into this release,
// keeping this release's real signed manifest: the archive is not the one
// this version's manifest lists, so nothing is installed.
func TestUpgradeRejectsChecksumsFromAnotherRelease(t *testing.T) {
	key := selfupdatetest.NewKey(t, time.Now().Add(-time.Hour), 0)
	files := selfupdatetest.Build(t, key, tag, official)
	old := selfupdatetest.Build(t, key, "v1.0.0", []byte("OLD VULNERABLE"))
	for _, name := range []string{"checksums.txt", "checksums.txt.asc", "celeste-linux-amd64.tar.gz"} {
		files[name] = old[name]
	}
	s := selfupdatetest.Serve(t, tag, files)
	exe := writeExe(t, "celeste")
	if _, err := testUpdater(s, key, exe).Upgrade(context.Background(), tag); !errors.Is(err, ErrChecksum) {
		t.Fatalf("err = %v, want ErrChecksum", err)
	}
	assertUnchanged(t, exe)
}

func TestUpgradeRejectsAnOversizedArchive(t *testing.T) {
	prev := limits
	t.Cleanup(func() { limits = prev })
	limits.Archive = 16
	key, s := release(t)
	exe := writeExe(t, "celeste")
	if _, err := testUpdater(s, key, exe).Upgrade(context.Background(), tag); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	assertUnchanged(t, exe)
}

func TestUpgradeRejectsAnUnreleasedPlatform(t *testing.T) {
	key, s := release(t)
	exe := writeExe(t, "celeste")
	u := testUpdater(s, key, exe)
	u.GOARCH = "386"
	if _, err := u.Upgrade(context.Background(), tag); !errors.Is(err, ErrNoAsset) {
		t.Fatalf("err = %v, want ErrNoAsset", err)
	}
	if n := s.Requests(); n != 0 {
		t.Fatalf("%d requests for a platform with no asset", n)
	}
	assertUnchanged(t, exe)
}

func TestUpgradeRejectsABadTag(t *testing.T) {
	key, s := release(t)
	exe := writeExe(t, "celeste")
	if _, err := testUpdater(s, key, exe).Upgrade(context.Background(), "v2.0.0/../../x"); !errors.Is(err, ErrBadTag) {
		t.Fatalf("err = %v, want ErrBadTag", err)
	}
	if s.Requests() != 0 {
		t.Fatal("a bad tag reached the network")
	}
}

func TestUpgradeFailsClosedOffline(t *testing.T) {
	key, s := release(t)
	exe := writeExe(t, "celeste")
	u := testUpdater(s, key, exe)
	s.Close()
	if _, err := u.Upgrade(context.Background(), tag); err == nil {
		t.Fatal("upgrade succeeded with the server down")
	}
	assertUnchanged(t, exe)
}

func TestGuardRefusesPlainHTTP(t *testing.T) {
	key, s := release(t)
	exe := writeExe(t, "celeste")
	u := testUpdater(s, key, exe)
	u.Base = "http://" + s.Host()
	if _, err := u.Upgrade(context.Background(), tag); !errors.Is(err, ErrHost) {
		t.Fatalf("err = %v, want ErrHost", err)
	}
	if s.Requests() != 0 {
		t.Fatal("a plain-HTTP request went out")
	}
	assertUnchanged(t, exe)
}

// The real host redirects downloads to an asset host; one off the list is
// refused before any request reaches it.
func TestGuardRefusesAnOffListRedirect(t *testing.T) {
	key := selfupdatetest.NewKey(t, time.Now().Add(-time.Hour), 0)
	files := selfupdatetest.Build(t, key, tag, official)
	assets := selfupdatetest.Serve(t, tag, files)
	s := selfupdatetest.Serve(t, tag, files)
	s.AssetBase = assets.URL
	exe := writeExe(t, "celeste")
	u := testUpdater(s, key, exe)
	if _, err := u.Upgrade(context.Background(), tag); !errors.Is(err, ErrHost) {
		t.Fatalf("err = %v, want ErrHost", err)
	}
	if assets.Requests() != 0 {
		t.Fatal("the off-list host was contacted")
	}
	assertUnchanged(t, exe)

	u.Hosts = append(u.Hosts, assets.Host()) // like release-assets.githubusercontent.com
	if _, err := u.Upgrade(context.Background(), tag); err != nil {
		t.Fatalf("an allowed redirect failed: %v", err)
	}
}

func TestDefaultHostsAreGitHubOnly(t *testing.T) {
	want := []string{"github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com"}
	if strings.Join(Hosts, ",") != strings.Join(want, ",") {
		t.Fatalf("Hosts = %v", Hosts)
	}
	if u := New(); u.Base != "https://github.com/whykusanagi/celeste-cli" || !bytes.Equal(u.PublicKey, ReleaseKey) {
		t.Fatalf("New() = %s, key from elsewhere", u.Base)
	}
}

func TestLatestTag(t *testing.T) {
	key, s := release(t)
	u := testUpdater(s, key, "")
	s.Latest = "v2.1.0"
	if got, err := u.LatestTag(context.Background()); err != nil || got != "v2.1.0" {
		t.Fatalf("LatestTag = %q, %v", got, err)
	}
	for _, bad := range []string{"latest", "v2.1.0/../../evil", "2.1.0"} {
		s.Latest = bad
		if got, err := u.LatestTag(context.Background()); !errors.Is(err, ErrBadTag) {
			t.Errorf("Latest %q: got %q, err %v, want ErrBadTag", bad, got, err)
		}
	}
}
