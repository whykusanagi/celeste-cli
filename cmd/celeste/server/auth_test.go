package server

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestGenerateToken(t *testing.T) {
	token, err := generateToken()
	if err != nil {
		t.Fatalf("generateToken error: %v", err)
	}
	if len(token) != tokenLength*2 {
		t.Fatalf("expected %d char hex string, got %d", tokenLength*2, len(token))
	}

	// Verify uniqueness (second call should be different)
	token2, err := generateToken()
	if err != nil {
		t.Fatalf("second generateToken error: %v", err)
	}
	if token == token2 {
		t.Fatal("two generated tokens should differ")
	}
}

func TestLoadOrCreateTokenNew(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "token")

	token, err := loadOrCreateToken(path)
	if err != nil {
		t.Fatalf("loadOrCreateToken error: %v", err)
	}
	if len(token) != tokenLength*2 {
		t.Fatalf("expected %d char token, got %d", tokenLength*2, len(token))
	}

	// File should exist with correct permissions
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat token file: %v", err)
	}
	if runtime.GOOS != "windows" {
		perm := info.Mode().Perm()
		if perm != 0600 {
			t.Fatalf("expected 0600 permissions, got %04o", perm)
		}
	}
}

func TestLoadOrCreateTokenExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")

	// Write a valid token
	existing := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	os.WriteFile(path, []byte(existing+"\n"), 0600)

	token, err := loadOrCreateToken(path)
	if err != nil {
		t.Fatalf("loadOrCreateToken error: %v", err)
	}
	if token != existing {
		t.Fatalf("expected existing token, got %q", token)
	}
}

func TestLoadOrCreateTokenRegenerate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")

	// Write a too-short token
	os.WriteFile(path, []byte("tooshort\n"), 0600)

	token, err := loadOrCreateToken(path)
	if err != nil {
		t.Fatalf("loadOrCreateToken error: %v", err)
	}
	if len(token) != tokenLength*2 {
		t.Fatalf("expected %d char token after regeneration, got %d", tokenLength*2, len(token))
	}
	if token == "tooshort" {
		t.Fatal("should have regenerated the token")
	}
}
