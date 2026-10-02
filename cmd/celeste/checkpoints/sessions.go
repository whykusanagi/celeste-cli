package checkpoints

import (
	"os"
	"path/filepath"
)

// Root is where every session's checkpoints live: ~/.celeste/checkpoints.
func Root() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".celeste", "checkpoints")
}

// SessionDir is sessionID's directory under root (ruling 5).
func SessionDir(root, sessionID string) string {
	return filepath.Join(root, safeName(sessionID))
}

// safeName keeps [A-Za-z0-9._-] and replaces every other byte with '_'.
// "", "." and ".." get a leading '_' so they never name root or its parent.
func safeName(id string) string {
	b := []byte(id)
	for i, c := range b {
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-'
		if !ok {
			b[i] = '_'
		}
	}
	s := string(b)
	if s == "" || s == "." || s == ".." {
		s = "_" + s
	}
	return s
}
