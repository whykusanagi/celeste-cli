package grimoire

import (
	"fmt"
	"io/fs"
	"os"
)

// checkOpened decides whether a context file opened at path may be read.
// opened (the handle's Stat) must be a regular file. before is the Lstat
// taken before the open: when it saw a symlink or a regular file, opened
// must be that same file (a symlink, or another file, swapped in after the
// path checks is refused). Anything else Lstat reports (Windows reparse
// points such as cloud placeholders, which Lstat calls irregular) is judged
// by the handle alone.
func checkOpened(path string, before, opened os.FileInfo) error {
	if !opened.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	if before.Mode()&fs.ModeSymlink != 0 || before.Mode().IsRegular() {
		if !os.SameFile(before, opened) {
			return fmt.Errorf("%s changed while it was being opened", path)
		}
	}
	return nil
}
