package grimoire

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/pathutil"
)

// MaxIncludeDepth is the maximum nesting depth for @include resolution.
const MaxIncludeDepth = 3

// allowedExtensions lists text file extensions that can be included.
var allowedExtensions = map[string]bool{
	".md": true, ".txt": true, ".go": true, ".js": true, ".ts": true,
	".json": true, ".yaml": true, ".yml": true, ".toml": true,
	".cfg": true, ".ini": true, ".sh": true, ".py": true, ".rs": true,
}

// includeScope is what one grimoire's includes may reach. The user's own
// ~/.celeste grimoire may include anything, @~/ paths too. A grimoire found
// in the workspace is repository content: its includes, nested ones too,
// must resolve (symlinks followed) inside the repository and outside its
// .git, and @~/ is refused, so a cloned repository cannot put one of the
// user's files into the prompt.
type includeScope struct {
	global bool
	root   string // real repository root; used when !global ("" admits nothing)
}

// repoScope confines includes to the git root above dir, or to dir itself
// outside a repository.
func repoScope(dir string) includeScope {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return includeScope{}
	}
	root, ok := GitRoot(abs)
	if !ok {
		root = abs
	}
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return includeScope{}
	}
	return includeScope{root: real}
}

// includeState is shared by every include of one load: cycle detection and
// the MaxSize budget.
type includeState struct {
	visited   map[string]bool
	totalSize int
}

func newIncludeState() *includeState {
	return &includeState{visited: make(map[string]bool)}
}

// ResolveIncludes expands @./path references in the Incantations section,
// confined to the repository holding baseDir (see includeScope).
// It never returns an error for individual file failures; instead, it records
// errors on the individual IncludeRef entries.
func ResolveIncludes(g *Grimoire, baseDir string) error {
	resolveIncludesIn(g, baseDir, repoScope(baseDir), newIncludeState())
	return nil
}

func resolveIncludesIn(g *Grimoire, baseDir string, scope includeScope, st *includeState) {
	for i := range g.Incantations {
		resolveRef(&g.Incantations[i], baseDir, scope, st, 0)
	}
}

func resolveRef(ref *IncludeRef, baseDir string, scope includeScope, st *includeState, depth int) {
	if depth > MaxIncludeDepth {
		ref.Error = fmt.Sprintf("max include depth (%d) exceeded", MaxIncludeDepth)
		return
	}

	// Resolve the path
	absPath, err := resolvePath(ref.Path, baseDir, scope)
	if err != nil {
		ref.Error = err.Error()
		return
	}
	ref.Resolved = absPath

	// The real file is what gets read: confine it, not its spelling.
	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		ref.Error = fmt.Sprintf("cannot read: %s", err.Error())
		return
	}
	if !scope.global && (scope.root == "" || !pathutil.Within(scope.root, realPath) ||
		pathutil.Within(filepath.Join(scope.root, ".git"), realPath)) {
		ref.Error = "not included: it resolves outside the repository"
		return
	}

	// Cycle detection
	if st.visited[realPath] {
		ref.Error = "cycle detected: already included"
		return
	}
	st.visited[realPath] = true

	// Check file extension (of the name and of the file it resolves to)
	ext := strings.ToLower(filepath.Ext(absPath))
	if !allowedExtensions[ext] || !allowedExtensions[strings.ToLower(filepath.Ext(realPath))] {
		ref.Error = fmt.Sprintf("binary or unsupported file type: %s", ext)
		return
	}

	// Read the file: a regular file only (no FIFO or device, no symlink
	// swapped in after the checks above), never past the size budget.
	f, _, err := openRegular(realPath)
	if err != nil {
		ref.Error = fmt.Sprintf("cannot read: %s", err.Error())
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(MaxSize-st.totalSize)+1))
	f.Close()
	if err != nil {
		ref.Error = fmt.Sprintf("cannot read: %s", err.Error())
		return
	}

	// Check for binary content (null bytes in first 512 bytes)
	checkLen := len(data)
	if checkLen > 512 {
		checkLen = 512
	}
	if bytes.ContainsRune(data[:checkLen], 0) {
		ref.Error = "binary file detected (contains null bytes)"
		return
	}

	// Check total size cap
	if st.totalSize+len(data) > MaxSize {
		ref.Error = fmt.Sprintf("total include size would exceed %dKB limit", MaxSize/1024)
		return
	}
	st.totalSize += len(data)

	content := string(data)
	ref.Content = content

	// Recursively resolve nested @includes found in the content
	resolveNestedIncludes(ref, filepath.Dir(absPath), scope, st, depth)
}

// resolveNestedIncludes scans content for @./path and @~/path lines
// and appends their resolved content inline.
func resolveNestedIncludes(ref *IncludeRef, baseDir string, scope includeScope, st *includeState, depth int) {
	lines := strings.Split(ref.Content, "\n")
	var result strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "@./") || strings.HasPrefix(trimmed, "@~/") {
			nested := &IncludeRef{Path: trimmed}
			resolveRef(nested, baseDir, scope, st, depth+1)
			if nested.Content != "" {
				result.WriteString(nested.Content)
				result.WriteString("\n")
			} else if nested.Error != "" {
				result.WriteString(fmt.Sprintf("<!-- %s: %s -->\n", nested.Path, nested.Error))
			}
		} else {
			result.WriteString(line)
			result.WriteString("\n")
		}
	}
	ref.Content = strings.TrimRight(result.String(), "\n")
}

// resolvePath resolves an @./path or @~/path reference to an absolute path.
// Only the global grimoire's scope may use @~/.
func resolvePath(ref string, baseDir string, scope includeScope) (string, error) {
	// Strip the @ prefix
	path := ref
	path = strings.TrimPrefix(path, "@")

	if strings.HasPrefix(path, "~/") {
		if !scope.global {
			return "", fmt.Errorf("@~/ includes are only allowed in ~/.celeste/grimoire.md")
		}
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot resolve home directory: %w", err)
		}
		path = filepath.Join(homeDir, path[2:])
	} else {
		path = filepath.Join(baseDir, path)
	}

	return filepath.Clean(path), nil
}
