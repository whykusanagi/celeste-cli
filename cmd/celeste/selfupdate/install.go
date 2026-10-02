package selfupdate

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// install writes bin next to exe and swaps it in (ruling 26): one rename on
// unix (a new inode, so macOS's ad-hoc signature stays valid); on Windows
// the running file moves aside first. On any error the temp file is removed
// and exe is what it was.
func (u *Updater) install(exe string, bin []byte) (err error) {
	dir, name := filepath.Dir(exe), filepath.Base(exe)
	f, err := os.CreateTemp(dir, "."+name+".new-*")
	if err != nil {
		return fmt.Errorf("write next to %s: %w", name, err)
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmp)
		}
	}()
	if _, err = f.Write(bin); err != nil {
		f.Close()
		return fmt.Errorf("write the new %s: %w", name, err)
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("write the new %s: %w", name, err)
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("write the new %s: %w", name, err)
	}
	if err = os.Chmod(tmp, 0o755); err != nil {
		return fmt.Errorf("make the new %s executable: %w", name, err)
	}
	if u.GOOS == "windows" {
		return u.swapWindows(tmp, exe)
	}
	if err = u.Rename(tmp, exe); err != nil {
		return fmt.Errorf("replace %s: %w", name, err)
	}
	return nil
}

// swapWindows moves the running exe aside, which Windows allows, then puts
// the new file in its place; on failure it moves the old one back.
func (u *Updater) swapWindows(tmp, exe string) error {
	old := exe + ".old"
	if err := os.Remove(old); err != nil && !errors.Is(err, fs.ErrNotExist) {
		old = fmt.Sprintf("%s.old-%d", exe, u.Now().UnixNano()) // a previous one is still running
	}
	if err := u.Rename(exe, old); err != nil {
		return fmt.Errorf("move the running %s aside: %w", filepath.Base(exe), err)
	}
	if err := u.Rename(tmp, exe); err != nil {
		_ = u.Rename(old, exe)
		return fmt.Errorf("put the new %s in place: %w", filepath.Base(exe), err)
	}
	return nil
}

// CleanupOld deletes the <exe>.old* files a Windows upgrade leaves behind.
// Best effort: one still running stays until the next start.
func CleanupOld(exe string) {
	dir, name := filepath.Dir(exe), filepath.Base(exe)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), name+".old") {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
