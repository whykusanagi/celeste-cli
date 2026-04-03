package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const tokenLength = 32 // 32 bytes = 64 hex chars

// defaultTokenPath returns the default path for the server token file.
func defaultTokenPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory: %w", err)
	}
	return filepath.Join(home, ".celeste", "server.token"), nil
}

// loadOrCreateToken reads the bearer token from path, or generates a new one
// if the file does not exist. The token file is created with 0600 permissions.
// If path is empty, the default path (~/.celeste/server.token) is used.
func loadOrCreateToken(path string) (string, error) {
	if path == "" {
		var err error
		path, err = defaultTokenPath()
		if err != nil {
			return "", err
		}
	}

	// Try to read existing token
	data, err := os.ReadFile(path)
	if err == nil {
		token := strings.TrimSpace(string(data))
		if len(token) >= tokenLength*2 { // at least 64 hex chars
			return token, nil
		}
		// Token file exists but is too short -- regenerate
	}

	// Generate new token
	token, err := generateToken()
	if err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}

	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("create token directory: %w", err)
	}

	// Write token with restrictive permissions
	if err := os.WriteFile(path, []byte(token+"\n"), 0600); err != nil {
		return "", fmt.Errorf("write token file: %w", err)
	}

	return token, nil
}

// generateToken creates a cryptographically random hex token.
func generateToken() (string, error) {
	b := make([]byte, tokenLength)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}
