package main

import (
	"os"
	"strings"
	"testing"
)

// Aikido 806869823: `make import-key` trusts only the repository's own key
// file, and fails unless that file holds exactly the release key.
func TestMakefileImportKeyChecksTheFingerprint(t *testing.T) {
	b, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	mk := strings.ReplaceAll(string(b), "\r\n", "\n")
	start := strings.Index(mk, "\nimport-key:")
	if start < 0 {
		t.Fatal("Makefile has no import-key target")
	}
	recipe := mk[start+1:]
	if end := strings.Index(recipe, "\n\n"); end >= 0 {
		recipe = recipe[:end]
	}
	for _, want := range []string{"whykusanagi.asc", "--import-options show-only", "exit 1"} {
		if !strings.Contains(recipe, want) {
			t.Errorf("import-key recipe lacks %q", want)
		}
	}
	// The fingerprints may live in Makefile variables.
	for _, want := range []string{releasePrimaryFpr, releaseSubkeyFpr} {
		if !strings.Contains(mk, want) {
			t.Errorf("Makefile does not pin the release key fingerprint %s", want)
		}
	}
	if strings.Contains(recipe, "keybase.io") || strings.Contains(recipe, "curl") {
		t.Error("import-key still fetches the key over the network")
	}
	// The check runs before the import.
	if c, i := strings.Index(recipe, "show-only"), strings.Index(recipe, "gpg --batch --import whykusanagi.asc"); c < 0 || i < 0 || c > i {
		t.Errorf("import-key must check the fingerprint (at %d) before importing (at %d)", c, i)
	}
}
