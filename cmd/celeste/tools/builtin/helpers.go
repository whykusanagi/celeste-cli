package builtin

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// resolvePath checks that the resolved absolute path stays within the workspace.
func resolvePath(workspace, input string) (string, error) {
	workspace = filepath.Clean(workspace)
	if input == "" {
		input = "."
	}

	var candidate string
	if filepath.IsAbs(input) {
		candidate = filepath.Clean(input)
	} else {
		candidate = filepath.Clean(filepath.Join(workspace, input))
	}

	if !withinDir(workspace, candidate) {
		return "", fmt.Errorf("path escapes workspace: %s", input)
	}

	// The check above is lexical, so a symlink inside the workspace that
	// points outside it would pass. Resolve symlinks on both sides and check
	// again (#187).
	realWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		realWorkspace = workspace
	}
	realCandidate, err := resolveExisting(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", input, err)
	}
	if !withinDir(realWorkspace, realCandidate) {
		return "", fmt.Errorf("path escapes workspace through a symlink: %s", input)
	}
	return candidate, nil
}

// withinDir reports whether path is dir or lies under it (both cleaned).
func withinDir(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// resolveExisting resolves symlinks in the longest existing prefix of path and
// re-attaches the rest, so a file that doesn't exist yet can still be checked.
// A dangling symlink is an error: writing through it would create its target,
// wherever that is.
func resolveExisting(path string) (string, error) {
	rest := ""
	cur := path
	for {
		if _, err := os.Lstat(cur); err == nil {
			real, err := filepath.EvalSymlinks(cur)
			if err != nil {
				return "", err
			}
			return filepath.Join(real, rest), nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return path, nil
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

func getStringArg(args map[string]any, key, fallback string) string {
	if v, ok := args[key]; ok {
		switch s := v.(type) {
		case string:
			return s
		case fmt.Stringer:
			return s.String()
		}
	}
	return fallback
}

func getBoolArg(args map[string]any, key string, fallback bool) bool {
	if v, ok := args[key]; ok {
		switch b := v.(type) {
		case bool:
			return b
		case string:
			parsed, err := strconv.ParseBool(strings.TrimSpace(b))
			if err == nil {
				return parsed
			}
		}
	}
	return fallback
}

func getIntArg(args map[string]any, key string, fallback int) int {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case int:
			return n
		case int32:
			return int(n)
		case int64:
			return int(n)
		case float32:
			return int(n)
		case float64:
			return int(n)
		case string:
			parsed, err := strconv.Atoi(strings.TrimSpace(n))
			if err == nil {
				return parsed
			}
		}
	}
	return fallback
}

func fileSize(info os.FileInfo) int64 {
	if info == nil {
		return 0
	}
	return info.Size()
}
