package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/selfupdate/selfupdatetest"
)

// Ruling 30: the contract table, checked against the helper's independent copy.
func TestAssetMatchesTheContract(t *testing.T) {
	if len(Platforms) != len(selfupdatetest.Platforms) {
		t.Fatalf("%d platforms, the contract has %d", len(Platforms), len(selfupdatetest.Platforms))
	}
	for _, want := range selfupdatetest.Platforms {
		got, ok := Asset(want.GOOS, want.GOARCH)
		if !ok || got.Archive != want.Archive || got.Binary != want.Binary {
			t.Errorf("Asset(%s, %s) = %+v %v, want %s / %s", want.GOOS, want.GOARCH, got, ok, want.Archive, want.Binary)
		}
	}
	for _, p := range [][2]string{{"linux", "386"}, {"windows", "arm64"}, {"freebsd", "amd64"}} {
		if _, ok := Asset(p[0], p[1]); ok {
			t.Errorf("Asset(%s, %s) is ok; celeste does not release it", p[0], p[1])
		}
	}
}

func TestExtractTarGzAndZip(t *testing.T) {
	tgz := selfupdatetest.TarGz(t, map[string][]byte{"README": []byte("x"), "celeste-linux-amd64": []byte("bin")})
	if got, err := Extract("celeste-linux-amd64.tar.gz", tgz, "celeste-linux-amd64"); err != nil || string(got) != "bin" {
		t.Fatalf("tar.gz: %q, %v", got, err)
	}
	zz := selfupdatetest.Zip(t, map[string][]byte{"celeste-windows-amd64.exe": []byte("exe")})
	if got, err := Extract("celeste-windows-amd64.zip", zz, "celeste-windows-amd64.exe"); err != nil || string(got) != "exe" {
		t.Fatalf("zip: %q, %v", got, err)
	}
}

// Review Focus 9: only one regular file with the exact name is accepted.
func TestExtractRejectsTheWrongEntry(t *testing.T) {
	missing := selfupdatetest.TarGz(t, map[string][]byte{"celeste": []byte("bin")})
	if _, err := Extract("celeste-linux-amd64.tar.gz", missing, "celeste-linux-amd64"); !errors.Is(err, ErrBadArchive) {
		t.Errorf("missing entry: err = %v", err)
	}
	nested := selfupdatetest.TarGz(t, map[string][]byte{"dir/celeste-linux-amd64": []byte("bin")})
	if _, err := Extract("celeste-linux-amd64.tar.gz", nested, "celeste-linux-amd64"); !errors.Is(err, ErrBadArchive) {
		t.Errorf("nested entry: err = %v", err)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "celeste-linux-amd64", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"})
	_ = tw.Close()
	_ = gz.Close()
	if _, err := Extract("celeste-linux-amd64.tar.gz", buf.Bytes(), "celeste-linux-amd64"); !errors.Is(err, ErrBadArchive) {
		t.Errorf("symlink entry: err = %v", err)
	}
	if _, err := Extract("celeste-linux-amd64.tar.gz", []byte("not gzip"), "celeste-linux-amd64"); !errors.Is(err, ErrBadArchive) {
		t.Errorf("garbage: err = %v", err)
	}
	if _, err := Extract("celeste-linux-amd64.rar", nil, "celeste-linux-amd64"); !errors.Is(err, ErrBadArchive) {
		t.Errorf("unknown type: err = %v", err)
	}
}

func TestExtractCapsTheBinary(t *testing.T) {
	prev := limits
	t.Cleanup(func() { limits = prev })
	limits.Binary = 4
	tgz := selfupdatetest.TarGz(t, map[string][]byte{"celeste-linux-amd64": []byte("12345")})
	if _, err := Extract("celeste-linux-amd64.tar.gz", tgz, "celeste-linux-amd64"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}
