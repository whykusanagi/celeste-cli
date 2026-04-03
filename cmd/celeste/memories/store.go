package memories

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Store manages memory files on disk under a project-specific directory.
type Store struct {
	baseDir string // e.g. ~/.celeste/projects/<hash>/memories/
}

// NewStore creates a Store for the given project directory.
// It derives a hash-based path under ~/.celeste/projects/<hash>/memories/.
func NewStore(projectDir string) *Store {
	homeDir, _ := os.UserHomeDir()
	h := sha256.Sum256([]byte(projectDir))
	hash := fmt.Sprintf("%x", h[:8])
	base := filepath.Join(homeDir, ".celeste", "projects", hash, "memories")
	return &Store{baseDir: base}
}

// NewStoreWithBase creates a Store with an explicit base directory (useful for testing).
func NewStoreWithBase(baseDir string) *Store {
	return &Store{baseDir: baseDir}
}

// BaseDir returns the store's base directory.
func (s *Store) BaseDir() string {
	return s.baseDir
}

// Save writes a memory to disk as a markdown file.
func (s *Store) Save(memory *Memory) error {
	if memory.Name == "" {
		return fmt.Errorf("memory name is required")
	}
	if err := os.MkdirAll(s.baseDir, 0755); err != nil {
		return fmt.Errorf("failed to create memories directory: %w", err)
	}
	path := filepath.Join(s.baseDir, sanitizeFilename(memory.Name)+".md")
	return os.WriteFile(path, memory.Serialize(), 0644)
}

// Load reads a memory by name from disk.
func (s *Store) Load(name string) (*Memory, error) {
	path := filepath.Join(s.baseDir, sanitizeFilename(name)+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("memory '%s' not found: %w", name, err)
	}
	return ParseMemory(data)
}

// Delete removes a memory file by name.
func (s *Store) Delete(name string) error {
	path := filepath.Join(s.baseDir, sanitizeFilename(name)+".md")
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("failed to delete memory '%s': %w", name, err)
	}
	return nil
}

// List returns all memories in the store.
func (s *Store) List() ([]*Memory, error) {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var memories []*Memory
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		if entry.Name() == "MEMORY.md" {
			continue // skip the index file
		}
		data, err := os.ReadFile(filepath.Join(s.baseDir, entry.Name()))
		if err != nil {
			continue
		}
		m, err := ParseMemory(data)
		if err != nil {
			continue
		}
		memories = append(memories, m)
	}
	return memories, nil
}

// ListByType returns all memories of a given type.
func (s *Store) ListByType(memType string) ([]*Memory, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	var filtered []*Memory
	for _, m := range all {
		if m.Type == memType {
			filtered = append(filtered, m)
		}
	}
	return filtered, nil
}

// sanitizeFilename converts a memory name to a safe filename.
func sanitizeFilename(name string) string {
	// Replace spaces and unsafe chars with hyphens, lowercase.
	safe := strings.ToLower(name)
	safe = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		if r == ' ' {
			return '-'
		}
		return -1
	}, safe)
	// Collapse multiple hyphens.
	for strings.Contains(safe, "--") {
		safe = strings.ReplaceAll(safe, "--", "-")
	}
	return strings.Trim(safe, "-")
}
