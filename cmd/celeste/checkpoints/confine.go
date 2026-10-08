package checkpoints

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/atomicfile"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/realroot"
)

// confine names path inside a real directory for Entry.Root and Entry.Rel:
// the workspace, or path's own directory when workspace is "". ok is false
// when path's directory cannot be resolved inside it.
func confine(workspace, path string) (root, rel string, ok bool) {
	base := workspace
	if base == "" {
		base = filepath.Dir(path)
	}
	realBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", "", false
	}
	// The file as the write tools reach it: an existing path is resolved
	// whole, so a change made through an in-workspace symlink (CLAUDE.md
	// -> AGENTS.md) is undone on the file it changed, the link kept.
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		realParent, perr := filepath.EvalSymlinks(filepath.Dir(path))
		if perr != nil {
			return "", "", false
		}
		real = filepath.Join(realParent, filepath.Base(path))
	}
	rel, err = filepath.Rel(realBase, real)
	if err != nil || !filepath.IsLocal(rel) {
		return "", "", false
	}
	return realBase, rel, true
}

// fileRef is an entry's file as undo reaches it. With a root (Entry.Root
// set) every access goes through an os.Root on that directory, which
// refuses a symlink or ".." leading out of it at any component, so a
// directory replaced by a symlink after the change cannot send the restore
// elsewhere (Aikido 806869815). An entry from an index written before
// Entry.Root is confined to its own directory the same way when its path
// resolves inside it (so CLAUDE.md -> AGENTS.md is undone on AGENTS.md,
// the link kept); otherwise it is the path, with the final component never
// followed on a write.
type fileRef struct {
	path string
	root *os.Root
	rel  string
}

// testHookBeforeRootOpen runs between the root check and the open; tests
// replace the root there.
var testHookBeforeRootOpen func()

// openRef opens e's file for undo; close it when done.
func openRef(e Entry) (fileRef, error) {
	if e.Root == "" {
		if dir, rel, ok := confine("", e.Path); ok {
			if root, err := realroot.Open(dir); err == nil {
				return fileRef{path: e.Path, root: root, rel: rel}, nil
			}
		}
		return fileRef{path: e.Path}, nil
	}
	if !filepath.IsLocal(e.Rel) {
		return fileRef{}, fmt.Errorf("invalid checkpoint location for %s", e.Path)
	}
	// The root itself must still be the directory it was: one of its
	// ancestors replaced by a symlink is refused here, and realroot.Open
	// refuses one replaced between this check and the open (Aikido review
	// of #421).
	if real, err := filepath.EvalSymlinks(e.Root); err != nil || real != e.Root {
		return fileRef{}, fmt.Errorf("%s: its directory moved or was replaced since the change; not restored", e.Path)
	}
	if testHookBeforeRootOpen != nil {
		testHookBeforeRootOpen()
	}
	root, err := realroot.Open(e.Root)
	if err != nil {
		return fileRef{}, err
	}
	return fileRef{path: e.Path, root: root, rel: e.Rel}, nil
}

func (f fileRef) close() {
	if f.root != nil {
		_ = f.root.Close()
	}
}

func (f fileRef) open(flag int) (*os.File, error) {
	if f.root != nil {
		return f.root.OpenFile(f.rel, flag, 0)
	}
	return os.OpenFile(f.path, flag|oNoFollow, 0)
}

// openRead opens the file for reading without waiting on a FIFO or
// device, and refuses anything but a regular file (one swapped for a FIFO
// since the change must not hang undo). Without a root the final
// component is never followed (CodeRabbit review of #421).
func (f fileRef) openRead() (*os.File, error) {
	var fh *os.File
	var err error
	var before os.FileInfo
	if f.root != nil {
		fh, err = f.root.OpenFile(f.rel, os.O_RDONLY|oNonblock, 0)
	} else {
		// Lstat first and compare after the open: oNoFollow is 0 where
		// there is no O_NOFOLLOW.
		if before, err = os.Lstat(f.path); err == nil && !before.Mode().IsRegular() {
			return nil, fmt.Errorf("%s is not a regular file", f.path)
		}
		if err == nil {
			fh, err = os.OpenFile(f.path, os.O_RDONLY|oNonblock|oNoFollow, 0)
		}
	}
	if err != nil {
		return nil, err
	}
	info, err := fh.Stat()
	if err != nil {
		_ = fh.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() || (before != nil && !os.SameFile(before, info)) {
		_ = fh.Close()
		return nil, fmt.Errorf("%s is not a regular file", f.path)
	}
	return fh, nil
}

func (f fileRef) readFile() ([]byte, error) {
	fh, err := f.openRead()
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	return io.ReadAll(fh)
}

// stat is the file's FileInfo; without a root, a symlink's own.
func (f fileRef) stat() (os.FileInfo, error) {
	if f.root != nil {
		return f.root.Stat(f.rel)
	}
	return os.Lstat(f.path)
}

func (f fileRef) remove() error {
	if f.root != nil {
		return f.root.Remove(f.rel)
	}
	return os.Remove(f.path)
}

// replace atomically replaces the file with data; a symlink in its place
// is replaced, not followed.
func (f fileRef) replace(data []byte, perm os.FileMode) error {
	if f.root != nil {
		return atomicfile.ReplaceIn(f.root, f.rel, data, perm)
	}
	return atomicfile.ReplaceKeepMode(f.path, data, perm)
}

// state is StateOf for the file as undo reaches it.
func (f fileRef) state() (FileState, error) {
	fh, err := f.openRead()
	if err != nil {
		return FileState{}, err
	}
	defer fh.Close()
	h := sha256.New()
	n, err := io.Copy(h, fh)
	if err != nil {
		return FileState{}, err
	}
	return FileState{Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

// entryState is e's file's FileState, read the way undo would reach it. A
// file undo cannot reach (its directory now leads elsewhere) reads as an
// error, not as gone.
func entryState(e Entry) (FileState, error) {
	ref, err := openRef(e)
	if err != nil {
		return FileState{}, err
	}
	defer ref.close()
	return ref.state()
}
