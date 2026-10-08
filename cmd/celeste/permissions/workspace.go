package permissions

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// workspace is the directory a run's relative paths are resolved against,
// as given and with its symlinks resolved.
type workspace struct {
	dir, real string
}

// newWorkspace is dir's workspace, nil for "" or a directory it can't make
// absolute.
func newWorkspace(dir string) *workspace {
	if dir == "" {
		return nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil
	}
	w := &workspace{dir: filepath.Clean(abs), real: filepath.Clean(abs)}
	if r, err := filepath.EvalSymlinks(w.dir); err == nil {
		w.real = r
	}
	return w
}

// pathForms are the spellings a path rule is matched against: the cleaned
// path as given and, with the workspace known, the workspace-relative path
// it names lexically (an absolute path inside the workspace) and with
// symlinks resolved. For a restricting rule a path that resolves outside
// the workspace is also matched absolute, with symlinks resolved; for a
// permitting one, a path inside the workspace that resolves outside it gets
// a "../" form, which is never permitted.
func pathForms(arg string, ws *workspace, restricting bool) []string {
	forms := []string{path.Clean(filepath.ToSlash(arg))}
	if ws == nil {
		return forms
	}
	add := func(p string) {
		for _, f := range forms {
			if f == p {
				return
			}
		}
		forms = append(forms, p)
	}
	full := arg
	if !filepath.IsAbs(full) {
		full = filepath.Join(ws.dir, full)
	}
	full = filepath.Clean(full)
	inside := false
	for _, root := range []string{ws.dir, ws.real} {
		if rel, ok := relInside(root, full); ok {
			if filepath.IsAbs(arg) {
				// Rules are workspace-relative: the absolute spelling of
				// a path inside the workspace is not one a rule names.
				forms = forms[:0]
			}
			add(rel)
			inside = true
			break
		}
	}
	real, ok := resolveExisting(full)
	if !ok {
		return forms
	}
	if rel, ok := relInside(ws.real, real); ok {
		add(rel)
	} else if restricting {
		add(filepath.ToSlash(real))
	} else if inside {
		add("../" + filepath.ToSlash(real))
	}
	return forms
}

// relInside is target relative to root (slash-separated) when target is
// root or under it.
func relInside(root, target string) (string, bool) {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") || filepath.IsAbs(rel) {
		return "", false
	}
	return rel, true
}

// resolveExisting is p with the symlinks of its longest existing prefix
// resolved and the rest appended.
func resolveExisting(p string) (string, bool) {
	rest := ""
	for cur := p; ; {
		if _, err := os.Lstat(cur); err == nil {
			if r, err := filepath.EvalSymlinks(cur); err == nil {
				return filepath.Join(r, rest), true
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", false
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}
