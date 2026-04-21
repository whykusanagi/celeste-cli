// Package config — user identity persistence.
//
// Stores who Celeste is currently talking to so she calibrates her
// relationship dynamic appropriately. Without this, she defaults to
// addressing everyone as Kusanagi (twin/Onii-chan), which is wrong
// for other users.
//
// Persistence: ~/.celeste/user.json
// Set via: /user <name> in TUI, or edit the file directly.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// UserIdentity holds the current user's identity for prompt calibration.
type UserIdentity struct {
	// Name is the user's display name. Empty means use the default title.
	Name string `json:"name"`

	// Title is the abyss-themed fallback when Name is empty.
	// Default: "Summoner" — they invoked Celeste via the CLI.
	Title string `json:"title,omitempty"`
}

// DefaultUserIdentity returns the factory default — an unnamed summoner.
func DefaultUserIdentity() *UserIdentity {
	return &UserIdentity{
		Name:  "",
		Title: "Summoner",
	}
}

// DisplayName returns the name to use when addressing the user.
// Returns Name if set, otherwise Title.
func (u *UserIdentity) DisplayName() string {
	if u.Name != "" {
		return u.Name
	}
	if u.Title != "" {
		return u.Title
	}
	return "Summoner"
}

// IsKusanagi returns true if the user has identified as Kusanagi.
func (u *UserIdentity) IsKusanagi() bool {
	return u.Name == "Kusanagi" || u.Name == "kusanagi" || u.Name == "Kusa"
}

// UserPath returns the path to the user identity file.
func UserPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".celeste", "user.json")
}

// LoadUser loads the user identity from disk. Returns default if
// the file doesn't exist or is malformed.
func LoadUser() *UserIdentity {
	data, err := os.ReadFile(UserPath())
	if err != nil {
		return DefaultUserIdentity()
	}
	var u UserIdentity
	if err := json.Unmarshal(data, &u); err != nil {
		return DefaultUserIdentity()
	}
	if u.Title == "" {
		u.Title = "Summoner"
	}
	return &u
}

// Save writes the user identity to disk.
func (u *UserIdentity) Save() error {
	data, err := json.MarshalIndent(u, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal user identity: %w", err)
	}
	dir := filepath.Dir(UserPath())
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	return os.WriteFile(UserPath(), data, 0644)
}
