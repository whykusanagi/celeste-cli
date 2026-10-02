package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Platform is one entry of the release asset contract (ruling 30).
type Platform struct{ GOOS, GOARCH, Archive, Binary string }

// Platforms is every platform release.yml publishes. Renaming any of these
// breaks every installed updater; TestReleaseWorkflowKeepsTheUpdaterContract
// checks the workflow still produces them.
var Platforms = []Platform{
	{"linux", "amd64", "celeste-linux-amd64.tar.gz", "celeste-linux-amd64"},
	{"linux", "arm64", "celeste-linux-arm64.tar.gz", "celeste-linux-arm64"},
	{"darwin", "amd64", "celeste-darwin-amd64.tar.gz", "celeste-darwin-amd64"},
	{"darwin", "arm64", "celeste-darwin-arm64.tar.gz", "celeste-darwin-arm64"},
	{"windows", "amd64", "celeste-windows-amd64.zip", "celeste-windows-amd64.exe"},
}

var (
	ErrNoAsset    = errors.New("there is no official build for this platform")
	ErrBadArchive = errors.New("the release archive does not hold the expected binary")
	ErrTooLarge   = errors.New("the download is larger than celeste allows")
)

// limits caps every read (ruling 26). A variable so tests can lower it.
var limits = struct{ Checksums, Signature, Manifest, Archive, Binary int64 }{
	Checksums: 64 << 10,
	Signature: 16 << 10,
	Manifest:  256 << 10,
	Archive:   256 << 20,
	Binary:    512 << 20,
}

// Asset returns the contract entry for goos/goarch; false for a platform
// celeste does not release.
func Asset(goos, goarch string) (Platform, bool) {
	for _, p := range Platforms {
		if p.GOOS == goos && p.GOARCH == goarch {
			return p, true
		}
	}
	return Platform{}, false
}

// Extract returns the single regular-file entry named binary from a .tar.gz
// or .zip archive (by archiveName's extension), in memory. Every other entry
// is skipped and nothing is written to disk, so entry names can't escape
// anywhere.
func Extract(archiveName string, data []byte, binary string) ([]byte, error) {
	switch {
	case strings.HasSuffix(archiveName, ".tar.gz"):
		return extractTarGz(data, binary)
	case strings.HasSuffix(archiveName, ".zip"):
		return extractZip(data, binary)
	}
	return nil, fmt.Errorf("%w: unknown archive type %s", ErrBadArchive, archiveName)
}

func extractTarGz(data []byte, binary string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadArchive, err)
	}
	tr := tar.NewReader(gz)
	var out []byte
	found := false
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrBadArchive, err)
		}
		if h.Name != binary {
			continue
		}
		if found || h.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("%w: %s is not one regular file", ErrBadArchive, binary)
		}
		if out, err = readCapped(tr, limits.Binary); err != nil {
			return nil, err
		}
		found = true
	}
	if !found {
		return nil, fmt.Errorf("%w: no %s inside", ErrBadArchive, binary)
	}
	return out, nil
}

func extractZip(data []byte, binary string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadArchive, err)
	}
	var hit *zip.File
	for _, f := range zr.File {
		if f.Name != binary {
			continue
		}
		if hit != nil || !f.Mode().IsRegular() {
			return nil, fmt.Errorf("%w: %s is not one regular file", ErrBadArchive, binary)
		}
		hit = f
	}
	if hit == nil {
		return nil, fmt.Errorf("%w: no %s inside", ErrBadArchive, binary)
	}
	rc, err := hit.Open()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadArchive, err)
	}
	defer rc.Close()
	return readCapped(rc, limits.Binary)
}

// readCapped reads all of r, failing with ErrTooLarge past max bytes.
func readCapped(r io.Reader, max int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadArchive, err)
	}
	if int64(len(b)) > max {
		return nil, ErrTooLarge
	}
	return b, nil
}
