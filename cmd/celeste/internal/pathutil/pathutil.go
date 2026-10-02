// Package pathutil compares file paths the way the filesystem does.
package pathutil

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Same reports whether a and b name the same file: equal once absolute
// and clean, once symlinks are resolved (macOS temp and home directories
// are often symlinked), or, when both exist, by file identity (letter case
// on a case-insensitive volume, a hard link). Windows paths compare
// without case.
func Same(a, b string) bool {
	a, b = absClean(a), absClean(b)
	if equal(a, b) || equal(Real(a), Real(b)) {
		return true
	}
	ai, aerr := os.Stat(a)
	bi, berr := os.Stat(b)
	return aerr == nil && berr == nil && os.SameFile(ai, bi)
}

// Real resolves symlinks in p, or in its directory when p itself is gone
// (an undone creation, a file not written yet). It returns p when neither
// resolves.
func Real(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	if d, err := filepath.EvalSymlinks(filepath.Dir(p)); err == nil {
		return filepath.Join(d, filepath.Base(p))
	}
	return p
}

// equal compares two clean paths; Windows paths are case-insensitive.
func equal(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func absClean(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return filepath.Clean(p)
}
