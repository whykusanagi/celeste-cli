package grimoire

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	contextFileCap  = 32 << 10
	contextTotalCap = 64 << 10
)

// contextFileNames are read in this order in each directory (2.0 W4).
var contextFileNames = []string{"AGENTS.md", "CLAUDE.md"}

// ContextFile is one AGENTS.md or CLAUDE.md the project context carries.
type ContextFile struct {
	Path      string // absolute
	Rel       string // relative to the git root (or the workspace)
	Content   string
	Truncated int // bytes cut by the caps; 0 when whole
}

// GitRoot returns the nearest ancestor of dir (or dir) holding .git, a
// directory or (in a worktree) a file.
func GitRoot(dir string) (string, bool) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", false
	}
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// contextDirs lists the directories from the git root (or, outside a
// repository, the workspace alone) down to the workspace, root first.
func contextDirs(workspace string) (root string, dirs []string, ok bool) {
	ws, err := filepath.Abs(workspace)
	if err != nil {
		return "", nil, false
	}
	root, found := GitRoot(ws)
	if !found {
		root = ws
	}
	for d := ws; ; d = filepath.Dir(d) {
		dirs = append([]string{d}, dirs...)
		if d == root || filepath.Dir(d) == d {
			break
		}
	}
	return root, dirs, true
}

// ContextFilePaths are the AGENTS.md and CLAUDE.md paths ContextFiles
// would read for workspace, present or not, in reading order.
func ContextFilePaths(workspace string) []string {
	_, dirs, ok := contextDirs(workspace)
	if !ok {
		return nil
	}
	var out []string
	for _, d := range dirs {
		for _, name := range contextFileNames {
			out = append(out, filepath.Join(d, name))
		}
	}
	return out
}

// ContextFiles reads AGENTS.md and CLAUDE.md from the git root down to the
// workspace (rulings 1 and 3): regular files only, 32 KiB per file and
// 64 KiB in all. Warnings name each file a cap cut or skipped.
func ContextFiles(workspace string) ([]ContextFile, []string) {
	root, _, ok := contextDirs(workspace)
	if !ok {
		return nil, nil
	}
	var out []ContextFile
	var warns []string
	total := 0
	for _, p := range ContextFilePaths(workspace) {
		info, err := os.Stat(p)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		budget := min(contextFileCap, contextTotalCap-total)
		if budget <= 0 {
			warns = append(warns, fmt.Sprintf("context files: %s skipped (the %d KiB total is used)", p, contextTotalCap>>10))
			continue
		}
		text, cut, ok := readCapped(p, budget, info.Size())
		if !ok {
			warns = append(warns, fmt.Sprintf("context files: %s skipped (not UTF-8 text)", p))
			continue
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			rel = filepath.Base(p)
		}
		out = append(out, ContextFile{Path: p, Rel: rel, Content: text, Truncated: cut})
		total += len(text)
		if cut > 0 {
			warns = append(warns, fmt.Sprintf("context files: %s cut to %d KiB", p, len(text)>>10))
		}
	}
	return out, warns
}

// readCapped reads at most limit bytes of path, ending on a rune boundary.
// It reports the bytes left unread and false for a file that is not UTF-8.
func readCapped(path string, limit int, size int64) (string, int, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, false
	}
	defer f.Close()
	buf, err := io.ReadAll(io.LimitReader(f, int64(limit)))
	if err != nil {
		return "", 0, false
	}
	if len(buf) == limit {
		// A cut may split the last rune: drop its leading bytes.
		for i := 0; i < utf8.UTFMax-1 && len(buf) > 0; i++ {
			if r, n := utf8.DecodeLastRune(buf); r != utf8.RuneError || n != 1 {
				break
			}
			buf = buf[:len(buf)-1]
		}
	}
	if !utf8.Valid(buf) {
		return "", 0, false
	}
	cut := 0
	if size > int64(len(buf)) {
		cut = int(size - int64(len(buf)))
	}
	return string(buf), cut, true
}

// RenderContextFiles is the project context's "# Project instructions"
// section (ruling 2); "" for no files.
func RenderContextFiles(files []ContextFile) string {
	if len(files) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("# Project instructions\n")
	b.WriteString("From AGENTS.md / CLAUDE.md in this repository. Where they conflict with the grimoire above, the grimoire wins.\n")
	for _, f := range files {
		fmt.Fprintf(&b, "\n## %s\n\n%s\n", filepath.ToSlash(f.Rel), strings.TrimSpace(f.Content))
		if f.Truncated > 0 {
			fmt.Fprintf(&b, "[…truncated: %d bytes]\n", f.Truncated)
		}
	}
	return b.String()
}
