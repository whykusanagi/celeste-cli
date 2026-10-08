package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrNoHome is HomeDir's error when there is no usable home directory.
var ErrNoHome = errors.New("no home directory: set HOME (USERPROFILE on Windows) to an absolute path")

// HomeDir returns the user's home directory, or ErrNoHome when it is
// unset, empty or relative. It fails closed: joining ".celeste" onto an
// empty or relative home would read config, skills, hooks and permission
// rules from the current directory, which may be an untrusted checkout.
func HomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || !filepath.IsAbs(home) {
		return "", ErrNoHome
	}
	return home, nil
}

// CheckHome is HomeDir's check for the start of a celeste command.
func CheckHome() error {
	if _, err := HomeDir(); err != nil {
		return fmt.Errorf("celeste needs a home directory: %w", err)
	}
	return nil
}
