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

// oNoFollow makes an open fail on a symlink in the final component.
const oNoFollow = syscall.O_NOFOLLOW
