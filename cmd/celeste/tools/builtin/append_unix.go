//go:build unix

package builtin

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// openAppendNoFollow opens path for appending without following a symlink
// in its final component. This prevents TOCTOU attacks where a validated
// path is replaced with a symlink between validation and open.
func openAppendNoFollow(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND|syscall.O_NOFOLLOW, 0644)
	if err != nil {
		// Linux and macOS report ELOOP; FreeBSD and NetBSD EMLINK.
		if errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.EMLINK) {
			return nil, fmt.Errorf("%s: the file changed to a symlink while it was being opened", path)
		}
		return nil, err
	}
	return f, nil
}
