//go:build windows

package grimoire

import (
	"fmt"
	"os"
)

// openRegular opens path for reading and checks, through the descriptor,
// that it is a regular file and the one Lstat saw (not a reparse point
// swapped in). Check and open are two steps here, so a swap between them
// remains a residual race.
func openRegular(path string) (*os.File, os.FileInfo, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("%s is not a regular file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() || !os.SameFile(before, info) {
		f.Close()
		return nil, nil, fmt.Errorf("%s changed while it was being opened", path)
	}
	return f, info, nil
}
