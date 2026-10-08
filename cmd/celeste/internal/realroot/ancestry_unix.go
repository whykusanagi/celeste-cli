//go:build unix

package realroot

import (
	"io/fs"
	"os"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// ancestryCheckable is true where ancestorIs works, so an unreadable
// directory on the path can be walked below by path.
const ancestryCheckable = true

// openDirNoFollow opens the directory at path, refusing a symlink there.
func openDirNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
}

// ancestorIs reports whether the directory up levels above f (through
// ".." on f's own descriptor, which names f's real parent whatever path
// opened it) is the one want describes. Reading it needs only search
// permission on the directories in between.
func ancestorIs(f *os.File, up int, want fs.FileInfo) (bool, error) {
	ws, ok := want.Sys().(*syscall.Stat_t)
	if !ok {
		return false, nil
	}
	var st unix.Stat_t
	rel := strings.TrimSuffix(strings.Repeat("../", up), "/")
	if err := unix.Fstatat(int(f.Fd()), rel, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return false, err
	}
	return uint64(st.Dev) == uint64(ws.Dev) && uint64(st.Ino) == uint64(ws.Ino), nil
}
