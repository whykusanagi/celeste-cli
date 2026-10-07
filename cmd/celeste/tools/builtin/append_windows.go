//go:build windows

package builtin

import (
	"fmt"
	"os"
)

// openAppendNoFollow opens path for appending, refusing a symlink or other
// reparse point there. Windows has no O_NOFOLLOW, so Lstat then Open leaves
// a narrower window than unix, but still prevents following symlinks.
func openAppendNoFollow(path string) (*os.File, error) {
	fi, err := os.Lstat(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil && fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return nil, fmt.Errorf("%s is a symlink or reparse point; not followed", path)
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
}
