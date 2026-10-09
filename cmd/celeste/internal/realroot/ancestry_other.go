//go:build !unix

package realroot

import (
	"errors"
	"io/fs"
	"os"
)

// ancestryCheckable is false: without a way to read a directory's real
// parent from its handle, an unreadable directory on the path fails the
// open instead of being walked below by path.
const ancestryCheckable = false

func openDirNoFollow(string) (*os.File, error) {
	return nil, errors.ErrUnsupported
}

func ancestorIs(*os.File, int, fs.FileInfo) (bool, error) {
	return false, errors.ErrUnsupported
}
