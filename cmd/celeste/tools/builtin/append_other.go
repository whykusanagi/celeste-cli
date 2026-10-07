//go:build !unix && !windows

package builtin

import "os"

// openAppendNoFollow opens path for appending. On platforms without
// O_NOFOLLOW support, this falls back to regular OpenFile.
func openAppendNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
}
