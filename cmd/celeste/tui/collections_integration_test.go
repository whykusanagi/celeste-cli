//go:build integration
// +build integration

package tui

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/collections"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

// TestCollectionsModel_Integration tests the collections TUI model with real API calls
func TestCollectionsModel_Integration(t *testing.T) {
	// Load config
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Check if management key is configured
	if cfg.XAIManagementAPIKey == "" {
		t.Skip("xAI Management API key not configured, skipping integration test")
	}

	// Create client and manager
	client := collections.NewClient(cfg.XAIManagementAPIKey)
	manager := collections.NewManager(client, cfg)

	// Create model
	model := NewCollectionsModel(manager)

	t.Run("ModelInitialization", func(t *testing.T) {
		// Initialize model
		cmd := model.Init()
		if cmd == nil {
			t.Fatal("Init() returned nil command")
		}

		// Execute the command to load collections
		msg := cmd()
		if msg == nil {
			t.Fatal("loadCollections() returned nil message")
		}

		// Update model with loaded collections
		updated, _ := model.Update(msg)
		model = updated.(CollectionsModel)

		// Verify collections were loaded
		if model.err != nil {
			t.Fatalf("Failed to load collections: %v", model.err)
		}

		t.Logf("Successfully loaded %d collections", len(model.collections))
	})

	t.Run("ViewRendering", func(t *testing.T) {
		// Load collections first
		msg := model.Init()()
		updated, _ := model.Update(msg)
		model = updated.(CollectionsModel)

		// Render view
		view := model.View()
		if view == "" {
			t.Fatal("View() returned empty string")
		}

		// Check for expected elements
		expectedElements := []string{
			"Active Collections",
			"Available Collections",
			"Navigate",
			"Toggle",
			"Back to Chat",
		}

		for _, element := range expectedElements {
			if !strings.Contains(view, element) {
				t.Errorf("View missing expected element: %q", element)
			}
		}

		t.Logf("View rendered successfully with %d characters", len(view))
	})

	t.Run("Navigation", func(t *testing.T) {
		// Load collections first
		msg := model.Init()()
		updated, _ := model.Update(msg)
		model = updated.(CollectionsModel)

		if len(model.collections) < 2 {
			t.Skip("Need at least 2 collections to test navigation")
		}

		initialCursor := model.cursor

		// Test down arrow
		keyMsg := tea.KeyMsg{Type: tea.KeyDown}
		updated, _ = model.Update(keyMsg)
		model = updated.(CollectionsModel)

		if model.cursor == initialCursor {
			t.Error("Down arrow did not move cursor")
		}

		// Test up arrow
		keyMsg = tea.KeyMsg{Type: tea.KeyUp}
		updated, _ = model.Update(keyMsg)
		model = updated.(CollectionsModel)

		if model.cursor != initialCursor {
			t.Error("Up arrow did not return cursor to initial position")
		}

		// Test 'j' key (vim down)
		keyMsg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
		updated, _ = model.Update(keyMsg)
		model = updated.(CollectionsModel)

		if model.cursor == initialCursor {
			t.Error("'j' key did not move cursor down")
		}

		// Test 'k' key (vim up)
		keyMsg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}
		updated, _ = model.Update(keyMsg)
		model = updated.(CollectionsModel)

		if model.cursor != initialCursor {
			t.Error("'k' key did not return cursor to initial position")
		}

		t.Log("All navigation keys working correctly")
	})

	t.Run("ToggleFunctionality", func(t *testing.T) {
		// Load collections first
		msg := model.Init()()
		updated, _ := model.Update(msg)
		model = updated.(CollectionsModel)

		if len(model.collections) == 0 {
			t.Skip("No collections to test toggle")
		}

		// Get initial active count
		initialActive := len(manager.GetActiveCollectionIDs())

		// Get the collection we're about to toggle
		collectionID := model.collections[model.cursor].ID
		wasActive := model.activeIDs[collectionID]

		// Simulate Space key press
		keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}}
		updated, _ = model.Update(keyMsg)
		model = updated.(CollectionsModel)

		// Check if toggle worked
		newActive := len(manager.GetActiveCollectionIDs())
		isNowActive := model.activeIDs[collectionID]

		if wasActive {
			// Was active, should now be inactive
			if isNowActive {
				t.Error("Collection was not disabled")
			}
			if newActive >= initialActive {
				t.Errorf("Active count did not decrease: %d -> %d", initialActive, newActive)
			}
		} else {
			// Was inactive, should now be active
			if !isNowActive {
				t.Error("Collection was not enabled")
			}
			if newActive <= initialActive {
				t.Errorf("Active count did not increase: %d -> %d", initialActive, newActive)
			}
		}

		// Verify config was saved by reloading
		reloadedCfg, err := config.Load()
		if err != nil {
			t.Fatalf("Failed to reload config: %v", err)
		}

		// Check if the change persisted
		persisted := false
		if reloadedCfg.Collections != nil {
			for _, id := range reloadedCfg.Collections.ActiveCollections {
				if id == collectionID {
					persisted = true
					break
				}
			}
		}

		if isNowActive && !persisted {
			t.Error("Toggle change was not persisted to config")
		}

		t.Logf("Toggle successful: %d -> %d active collections", initialActive, newActive)
	})
}

// TestCollectionsModel_EmptyState tests behavior with no collections
func TestCollectionsModel_EmptyState(t *testing.T) {
	// Create a mock manager with empty collections
	cfg := &config.Config{}
	client := collections.NewClient("fake-key")
	manager := collections.NewManager(client, cfg)

	model := NewCollectionsModel(manager)

	// Simulate empty collections loaded
	msg := collectionsLoadedMsg{
		collections: []collections.Collection{},
		err:         nil,
	}

	updated, _ := model.Update(msg)
	model = updated.(CollectionsModel)

	view := model.View()
	if !strings.Contains(view, "No collections found") {
		t.Error("Empty state message not displayed")
	}
}

// TestCollectionsModel_ErrorState tests behavior with API error
func TestCollectionsModel_ErrorState(t *testing.T) {
	cfg := &config.Config{}
	client := collections.NewClient("fake-key")
	manager := collections.NewManager(client, cfg)

	model := NewCollectionsModel(manager)

	// Simulate error
	msg := collectionsLoadedMsg{
		collections: nil,
		err:         os.ErrInvalid,
	}

	updated, _ := model.Update(msg)
	model = updated.(CollectionsModel)

	view := model.View()
	if !strings.Contains(view, "Error") {
		t.Error("Error message not displayed")
	}
}
