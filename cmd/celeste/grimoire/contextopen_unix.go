//go:build unix

package grimoire

import (
	"fmt"
	"os"
	"syscall"
)

// openRegular opens path for reading and checks, through the descriptor,
// that it is a regular file. The final component is not followed (a
// symlink swapped in after the path checks fails) and the open does not
// block (a FIFO swapped in is refused, not waited on). A directory swapped
// higher up between check and open remains a residual race here; a
// repository include is opened with openRegularIn, which has none.
func openRegular(path string) (*os.File, os.FileInfo, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, nil, fmt.Errorf("%s is not a regular file", path)
	}
	return f, info, nil
}

// openRegularIn is openRegular for rel inside root: no component may lead
// out of root, the final one is not followed and the open does not block.
func openRegularIn(root *os.Root, rel string) (*os.File, error) {
	f, err := root.OpenFile(rel, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = fmt.Errorf("%s is not a regular file", rel)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
