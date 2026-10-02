package selfupdate

import (
	"fmt"
	"os"
	"path/filepath"
)

// VerifyDist checks a local release directory exactly as the updater checks
// a download (ruling 30): both signatures with pubKey, the manifest's tag,
// and for every platform of the contract the archive's signed checksum and
// the binary inside it. release.yml runs it through the release binary, with
// its embedded key, before anything is published.
func VerifyDist(dir, tag string, pubKey []byte) error {
	if !ValidTag(tag) {
		return fmt.Errorf("%w: %q", ErrBadTag, tag)
	}
	get := func(name string, max int64) ([]byte, error) {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		if int64(len(b)) > max {
			return nil, fmt.Errorf("%w: %s", ErrTooLarge, name)
		}
		return b, nil
	}
	for _, p := range Platforms {
		if _, err := verifyRelease(get, pubKey, tag, p); err != nil {
			return fmt.Errorf("%s/%s: %w", p.GOOS, p.GOARCH, err)
		}
	}
	return nil
}
