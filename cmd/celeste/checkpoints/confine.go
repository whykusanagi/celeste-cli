package checkpoints

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/atomicfile"
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
	realParent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", "", false
	}
	rel, err = filepath.Rel(realBase, filepath.Join(realParent, filepath.Base(path)))
	if err != nil || !filepath.IsLocal(rel) {
		return "", "", false
	}
	return realBase, rel, true
}

// fileRef is an entry's file as undo reaches it. With a root (Entry.Root
// set) every access goes through an os.Root on that directory, which
// refuses a symlink or ".." leading out of it at any component, so a
// directory replaced by a symlink after the change cannot send the restore
// elsewhere (Aikido 806869815). Without one (an index written before
// Entry.Root) it is the path, with the final component never followed on
// a write.
type fileRef struct {
	path string
	root *os.Root
	rel  string
}

// openRef opens e's file for undo; close it when done.
func openRef(e Entry) (fileRef, error) {
	if e.Root == "" {
		return fileRef{path: e.Path}, nil
	}
	if !filepath.IsLocal(e.Rel) {
		return fileRef{}, fmt.Errorf("invalid checkpoint location for %s", e.Path)
	}
	// The root itself must still be the directory it was: one of its
	// ancestors replaced by a symlink is refused here, before it is opened.
	if real, err := filepath.EvalSymlinks(e.Root); err != nil || real != e.Root {
		return fileRef{}, fmt.Errorf("%s: its directory moved or was replaced since the change; not restored", e.Path)
	}
	root, err := os.OpenRoot(e.Root)
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

func (f fileRef) readFile() ([]byte, error) {
	if f.root != nil {
		return f.root.ReadFile(f.rel)
	}
	return os.ReadFile(f.path)
}

func (f fileRef) stat() (os.FileInfo, error) {
	if f.root != nil {
		return f.root.Stat(f.rel)
	}
	return os.Stat(f.path)
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
	if f.root == nil {
		return StateOf(f.path)
	}
	fh, err := f.root.Open(f.rel)
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
