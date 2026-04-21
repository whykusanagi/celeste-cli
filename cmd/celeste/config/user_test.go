package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultUserIdentity(t *testing.T) {
	u := DefaultUserIdentity()
	if u.Name != "" {
		t.Fatalf("expected empty name, got %q", u.Name)
	}
	if u.Title != "Summoner" {
		t.Fatalf("expected title 'Summoner', got %q", u.Title)
	}
	if u.DisplayName() != "Summoner" {
		t.Fatalf("expected display name 'Summoner', got %q", u.DisplayName())
	}
	if u.IsKusanagi() {
		t.Fatal("default user should not be Kusanagi")
	}
}

func TestUserIdentityDisplayName(t *testing.T) {
	tests := []struct {
		name, title, expect string
	}{
		{"", "Summoner", "Summoner"},
		{"Alice", "", "Alice"},
		{"Kusanagi", "", "Kusanagi"},
		{"", "", "Summoner"},      // fallback
		{"Bob", "Visitor", "Bob"}, // name takes precedence
	}
	for _, tt := range tests {
		u := &UserIdentity{Name: tt.name, Title: tt.title}
		if got := u.DisplayName(); got != tt.expect {
			t.Errorf("DisplayName(%q, %q) = %q, want %q", tt.name, tt.title, got, tt.expect)
		}
	}
}

func TestIsKusanagi(t *testing.T) {
	tests := []struct {
		name   string
		expect bool
	}{
		{"Kusanagi", true},
		{"kusanagi", true},
		{"Kusa", true},
		{"Alice", false},
		{"", false},
		{"KUSANAGI", false}, // case-sensitive except the three variants
	}
	for _, tt := range tests {
		u := &UserIdentity{Name: tt.name}
		if got := u.IsKusanagi(); got != tt.expect {
			t.Errorf("IsKusanagi(%q) = %v, want %v", tt.name, got, tt.expect)
		}
	}
}

func TestUserSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	// os.UserHomeDir() uses HOME on Unix, USERPROFILE on Windows
	t.Setenv("HOME", tmpDir)
	t.Setenv("USERPROFILE", tmpDir)

	// Ensure config dir exists
	os.MkdirAll(filepath.Join(tmpDir, ".celeste"), 0755)

	// Save a user
	u := &UserIdentity{Name: "TestUser", Title: "Summoner"}
	if err := u.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Load it back
	loaded := LoadUser()
	if loaded.Name != "TestUser" {
		t.Fatalf("expected name 'TestUser', got %q", loaded.Name)
	}

	// Verify file contents
	data, err := os.ReadFile(filepath.Join(tmpDir, ".celeste", "user.json"))
	if err != nil {
		t.Fatalf("read file failed: %v", err)
	}
	var parsed UserIdentity
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if parsed.Name != "TestUser" {
		t.Fatalf("file contains wrong name: %q", parsed.Name)
	}
}

func TestLoadUserDefault(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("USERPROFILE", tmpDir)

	// No file exists — should return default
	u := LoadUser()
	if u.Name != "" {
		t.Fatalf("expected empty name for missing file, got %q", u.Name)
	}
	if u.Title != "Summoner" {
		t.Fatalf("expected title 'Summoner' for missing file, got %q", u.Title)
	}
}
