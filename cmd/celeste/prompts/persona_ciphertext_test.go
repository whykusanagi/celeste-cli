package prompts

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/prompts/personacrypt"
)

// The committed ciphertext is exactly what SOURCE.json records, checked
// with no key (W5 ruling 19). make persona-check does the keyed comparison.
func TestPersonaCiphertextMatchesSource(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("persona", "SOURCE.json"))
	if err != nil {
		t.Fatalf("persona/SOURCE.json: %v (run make sync-persona)", err)
	}
	src, err := personacrypt.ParseSource(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range src.Profiles {
		sealed, err := os.ReadFile(filepath.Join("persona", e.File))
		if err != nil {
			t.Errorf("%s: %v", e.File, err)
			continue
		}
		if len(sealed) != e.CiphertextBytes || personacrypt.SHA256Hex(sealed) != e.CiphertextSHA256 {
			t.Errorf("persona/%s differs from SOURCE.json: hand-edited? re-run make sync-persona", e.File)
		}
		if !bytes.HasPrefix(sealed, []byte("CPv1")) {
			t.Errorf("persona/%s is not a sealed persona file", e.File)
		}
	}
}

// persona/ holds the sealed files, SOURCE.json and LICENSE, and nothing in
// it reads as plaintext (W5 rulings 2, 3).
func TestPersonaCiphertextDirectoryHoldsNoPlaintext(t *testing.T) {
	want := map[string]bool{"SOURCE.json": true, "LICENSE": true}
	for _, p := range personacrypt.Profiles {
		want[personacrypt.FileName(p)] = true
	}
	entries, err := os.ReadDir("persona")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !want[e.Name()] {
			t.Errorf("persona/%s should not be here: only the sealed files, SOURCE.json and LICENSE", e.Name())
			continue
		}
		if filepath.Ext(e.Name()) != ".enc" {
			continue
		}
		data, err := os.ReadFile(filepath.Join("persona", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, marker := range []string{"system_prompt", "Voice Boundary", "Celeste", "Kusanagi"} {
			if bytes.Contains(data, []byte(marker)) {
				t.Errorf("persona/%s contains %q in the clear", e.Name(), marker)
			}
		}
	}
	if len(entries) != len(want) {
		t.Errorf("persona/ has %d entries, want %d", len(entries), len(want))
	}
}
