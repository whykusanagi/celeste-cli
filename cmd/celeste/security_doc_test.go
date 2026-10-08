package main

import (
	"os"
	"strings"
	"testing"
)

// Aikido 806869823: the Keybase copy of the release key lacks the signing
// subkey, so SECURITY.md's manual steps import the repository's key file
// and use the other sources only to cross-check the primary fingerprint.
func TestSecurityDocManualStepsImportTheRepositoryKey(t *testing.T) {
	b, err := os.ReadFile("../../SECURITY.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := strings.ReplaceAll(string(b), "\r\n", "\n")
	start := strings.Index(doc, "**Manual Verification Steps**")
	if start < 0 {
		t.Fatal("SECURITY.md has no Manual Verification Steps")
	}
	steps := doc[start:]
	if end := strings.Index(steps, "\n\n"); end >= 0 {
		steps = steps[:end]
	}
	if strings.Contains(steps, "from Keybase or GitHub") {
		t.Error("manual steps still import the key from Keybase or GitHub")
	}
	for _, want := range []string{"gpg --import whykusanagi.asc", "make import-key"} {
		if !strings.Contains(steps, want) {
			t.Errorf("manual steps lack %q", want)
		}
	}
}
