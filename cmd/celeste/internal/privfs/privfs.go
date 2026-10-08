// Package privfs creates and tightens account-private files and
// directories: credentials, logs, transcripts and the rest of ~/.celeste.
// Directories are 0700 and files 0600. MkdirAll and OpenFile never tighten
// a path that already exists, so these helpers also strip the group and
// other bits from one an older version (or a umask) left open. On Windows
// POSIX bits mean nothing, so nothing is tightened there.
package privfs

import (
	"os"
	"runtime"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/atomicfile"
)

const (
	// DirPerm is the mode of a private directory.
	DirPerm os.FileMode = 0o700
	// FilePerm is the mode of a new private file.
	FilePerm os.FileMode = 0o600
)

// MkdirAll creates dir and any missing parents with DirPerm, and tightens
// dir itself when it already exists with group or other bits. Parents
// that already exist are left as they are.
func MkdirAll(dir string) error {
	if err := os.MkdirAll(dir, DirPerm); err != nil {
		return err
	}
	_, err := Tighten(dir)
	return err
}

// Tighten strips the group and other permission bits from an existing path
// (the owner's bits are kept, so a read-only file stays read-only). It
// reports whether the mode changed. A missing path is not an error.
func Tighten(path string) (bool, error) {
	if runtime.GOOS == "windows" {
		return false, nil
	}
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	perm := fi.Mode().Perm()
	if perm&0o077 == 0 {
		return false, nil
	}
	if err := os.Chmod(path, perm&0o700); err != nil {
		return false, err
	}
	return true, nil
}

// WriteFile replaces path atomically (see atomicfile.Write) with an
// owner-only mode: an existing file keeps its owner bits and loses the
// group and other bits; a new file is FilePerm. It reports whether an
// existing file was more open than that, so the caller can say so.
func WriteFile(path string, data []byte) (loosened bool, err error) {
	perm := FilePerm
	if fi, statErr := os.Stat(path); statErr == nil {
		existing := fi.Mode().Perm()
		if runtime.GOOS != "windows" {
			loosened = existing&0o077 != 0
			perm = existing & 0o700
			if perm == 0 {
				perm = FilePerm
			}
		}
	}
	return loosened, atomicfile.Write(path, data, perm)
}

// OpenAppend opens path for appending, creating it FilePerm and tightening
// an existing file.
func OpenAppend(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, FilePerm)
	if err != nil {
		return nil, err
	}
	if _, err := Tighten(path); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}
