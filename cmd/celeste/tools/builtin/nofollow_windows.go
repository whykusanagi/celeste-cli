//go:build windows

package builtin

import (
	"fmt"
	"os"
)

// openNoFollow opens real, the symlink-resolved path resolvePathReal
// checked, refusing a symlink or other reparse point there (ruling 4).
// Windows has no O_NOFOLLOW, so Lstat then Open leaves a narrower window
// than unix; a directory swapped higher up remains a residual race too.
func openNoFollow(real string) (*os.File, error) {
	fi, err := os.Lstat(real)
	if err != nil {
		return nil, err
	}
	if fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return nil, fmt.Errorf("%s: the file changed to a symlink while it was being opened", real)
	}
	return os.Open(real)
}
