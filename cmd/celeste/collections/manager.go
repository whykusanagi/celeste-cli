package collections

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

// Manager provides high-level collections management
type Manager struct {
	client *Client
	config *config.Config
}

// NewManager creates a new collections manager
func NewManager(client *Client, cfg *config.Config) *Manager {
	return &Manager{
		client: client,
		config: cfg,
	}
}

// EnableCollection adds a collection to the active set
func (m *Manager) EnableCollection(collectionID string) error {
	if m.config.Collections == nil {
		m.config.Collections = &config.CollectionsConfig{
			Enabled:           true,
			ActiveCollections: []string{},
			AutoEnable:        true,
		}
	}

	// Check if already enabled
	for _, id := range m.config.Collections.ActiveCollections {
		if id == collectionID {
			return nil // Already enabled
		}
	}

	// Add to active collections
	m.config.Collections.ActiveCollections = append(
		m.config.Collections.ActiveCollections,
		collectionID,
	)

	return nil
}

// DisableCollection removes a collection from the active set
func (m *Manager) DisableCollection(collectionID string) error {
	if m.config.Collections == nil {
		return nil // Nothing to disable
	}

	// Filter out the collection
	var active []string
	for _, id := range m.config.Collections.ActiveCollections {
		if id != collectionID {
			active = append(active, id)
		}
	}

	m.config.Collections.ActiveCollections = active
	return nil
}

// GetActiveCollections returns the list of active collection IDs
func (m *Manager) GetActiveCollections() []string {
	if m.config.Collections == nil {
		return []string{}
	}
	return m.config.Collections.ActiveCollections
}

// GetActiveCollectionIDs returns a map of active collection IDs for quick lookup
func (m *Manager) GetActiveCollectionIDs() map[string]bool {
	activeIDs := make(map[string]bool)
	if m.config.Collections != nil {
		for _, id := range m.config.Collections.ActiveCollections {
			activeIDs[id] = true
		}
	}
	return activeIDs
}

// ListCollections fetches all collections from the API
func (m *Manager) ListCollections() ([]Collection, error) {
	return m.client.ListCollections()
}

// SaveConfig saves the configuration to disk
func (m *Manager) SaveConfig() error {
	return config.Save(m.config)
}

// ValidateDocument checks if a document is valid for upload
func ValidateDocument(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	// Check if it's a directory
	if info.IsDir() {
		return fmt.Errorf("path is a directory, not a file")
	}

	// Check size limit (10MB)
	const maxSize = 10 * 1024 * 1024
	if info.Size() > maxSize {
		return fmt.Errorf("file too large: %d bytes (max %d bytes)", info.Size(), maxSize)
	}

	// Check supported formats
	ext := strings.ToLower(filepath.Ext(path))
	supported := []string{".md", ".txt", ".pdf", ".html", ".htm"}

	isSupported := false
	for _, s := range supported {
		if ext == s {
			isSupported = true
			break
		}
	}

	if !isSupported {
		return fmt.Errorf("unsupported format: %s (supported: .md, .txt, .pdf, .html)", ext)
	}

	return nil
}
