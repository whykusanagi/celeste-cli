//go:build unix

package builtin

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// openNoFollow opens real, the symlink-resolved path resolvePathReal
// checked, without following a symlink in its final component (ruling 4):
// one swapped in after the check fails instead of escaping the workspace.
// A directory swapped higher up between check and open remains a residual
// race.
func openNoFollow(real string) (*os.File, error) {
	f, err := os.OpenFile(real, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		// Linux and macOS report ELOOP; FreeBSD and NetBSD EMLINK.
		if errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.EMLINK) {
			return nil, fmt.Errorf("%s: the file changed to a symlink while it was being opened", real)
		}
		return nil, err
	}
	return f, nil
}
