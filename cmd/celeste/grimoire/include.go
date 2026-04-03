package grimoire

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MaxIncludeDepth is the maximum nesting depth for @include resolution.
const MaxIncludeDepth = 3

// allowedExtensions lists text file extensions that can be included.
var allowedExtensions = map[string]bool{
	".md": true, ".txt": true, ".go": true, ".js": true, ".ts": true,
	".json": true, ".yaml": true, ".yml": true, ".toml": true,
	".cfg": true, ".ini": true, ".sh": true, ".py": true, ".rs": true,
}

// ResolveIncludes expands @./path and @~/path references in the Incantations section.
// It never returns an error for individual file failures; instead, it records
// errors on the individual IncludeRef entries.
func ResolveIncludes(g *Grimoire, baseDir string) error {
	visited := make(map[string]bool)
	totalSize := 0

	for i := range g.Incantations {
		resolveRef(&g.Incantations[i], baseDir, visited, 0, &totalSize)
	}

	return nil
}

func resolveRef(ref *IncludeRef, baseDir string, visited map[string]bool, depth int, totalSize *int) {
	if depth > MaxIncludeDepth {
		ref.Error = fmt.Sprintf("max include depth (%d) exceeded", MaxIncludeDepth)
		return
	}

	// Resolve the path
	absPath, err := resolvePath(ref.Path, baseDir)
	if err != nil {
		ref.Error = err.Error()
		return
	}
	ref.Resolved = absPath

	// Cycle detection
	if visited[absPath] {
		ref.Error = "cycle detected: already included"
		return
	}
	visited[absPath] = true

	// Check file extension
	ext := strings.ToLower(filepath.Ext(absPath))
	if !allowedExtensions[ext] {
		ref.Error = fmt.Sprintf("binary or unsupported file type: %s", ext)
		return
	}

	// Read file
	data, err := os.ReadFile(absPath)
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
	if *totalSize+len(data) > MaxSize {
		ref.Error = fmt.Sprintf("total include size would exceed %dKB limit", MaxSize/1024)
		return
	}
	*totalSize += len(data)

	content := string(data)
	ref.Content = content

	// Recursively resolve nested @includes found in the content
	resolveNestedIncludes(ref, filepath.Dir(absPath), visited, depth, totalSize)
}

// resolveNestedIncludes scans content for @./path and @~/path lines
// and appends their resolved content inline.
func resolveNestedIncludes(ref *IncludeRef, baseDir string, visited map[string]bool, depth int, totalSize *int) {
	lines := strings.Split(ref.Content, "\n")
	var result strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "@./") || strings.HasPrefix(trimmed, "@~/") {
			nested := &IncludeRef{Path: trimmed}
			resolveRef(nested, baseDir, visited, depth+1, totalSize)
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
func resolvePath(ref string, baseDir string) (string, error) {
	// Strip the @ prefix
	path := ref
	if strings.HasPrefix(path, "@") {
		path = path[1:]
	}

	if strings.HasPrefix(path, "~/") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot resolve home directory: %w", err)
		}
		path = filepath.Join(homeDir, path[2:])
	} else if strings.HasPrefix(path, "./") {
		path = filepath.Join(baseDir, path)
	} else {
		path = filepath.Join(baseDir, path)
	}

	return filepath.Clean(path), nil
}
