package selfupdate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/selfupdate/selfupdatetest"
)

func writeDist(t *testing.T, files map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	for name, b := range files {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// Ruling 30: release.yml runs this over dist/ before publishing.
func TestVerifyDist(t *testing.T) {
	key := selfupdatetest.NewKey(t, time.Now().Add(-time.Hour), 0)
	good := func() map[string][]byte { return selfupdatetest.Build(t, key, "v2.0.0", []byte("bin")) }
	if err := VerifyDist(writeDist(t, good()), "v2.0.0", key.Public); err != nil {
		t.Fatalf("a good release: %v", err)
	}
	cases := []struct {
		name   string
		tag    string
		mutate func(map[string][]byte)
		want   error
		says   string
	}{
		{"wrong tag", "v2.0.1", func(map[string][]byte) {}, ErrManifest, ""},
		{"bad tag", "2.0.0", func(map[string][]byte) {}, ErrBadTag, ""},
		{"edited archive", "v2.0.0", func(f map[string][]byte) { f["celeste-darwin-arm64.tar.gz"] = []byte("x") }, ErrChecksum, "darwin/arm64"},
		{"missing windows zip", "v2.0.0", func(f map[string][]byte) { delete(f, "celeste-windows-amd64.zip") }, nil, "windows/amd64"},
		{"unsigned checksums", "v2.0.0", func(f map[string][]byte) { delete(f, "checksums.txt.asc") }, nil, ""},
		{"checksums signed by another key", "v2.0.0", func(f map[string][]byte) {
			f["checksums.txt.asc"] = selfupdatetest.NewKey(t, time.Now().Add(-time.Hour), 0).Sign(t, f["checksums.txt"], time.Time{})
		}, ErrBadSignature, ""},
	}
	for _, c := range cases {
		f := good()
		c.mutate(f)
		err := VerifyDist(writeDist(t, f), c.tag, key.Public)
		if err == nil || (c.want != nil && !errors.Is(err, c.want)) || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s: err = %v, want %v naming %q", c.name, err, c.want, c.says)
		}
	}
}

// The real v1.16.0 signatures verify through the same path (the archives
// are not committed, so only the signature and tag checks run here; Step 6
// runs the whole thing on a downloaded release).
func TestVerifyDistReadsTheRealSignedFiles(t *testing.T) {
	err := VerifyDist(filepath.Join("testdata", "v1.16.0"), "v1.16.0", ReleaseKey)
	if err == nil || !strings.Contains(err.Error(), "celeste-linux-amd64.tar.gz") {
		t.Fatalf("err = %v, want the first missing archive", err)
	}
	if errors.Is(err, ErrBadSignature) || errors.Is(err, ErrManifest) {
		t.Fatalf("the real signed files failed: %v", err)
	}
}
