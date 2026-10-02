//go:build unix

package builtin

import (
	"os"
	"syscall"
)

// hardLinked reports a regular file with more than one name. A rename
// would split it from its other names, so atomicWrite rewrites it in place.
func hardLinked(_ string, fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && fi.Mode().IsRegular() && st.Nlink > 1
}

// openInPlace opens path for an in-place rewrite without following a
// symlink swapped in at it.
func openInPlace(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_TRUNC|syscall.O_NOFOLLOW, 0)
}
