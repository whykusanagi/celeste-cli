//go:build !unix && !windows

package builtin

import "os"

// hardLinked: the link count is not available here, so every file is
// replaced by a rename.
func hardLinked(string, os.FileInfo) bool { return false }

func openInPlace(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
}
