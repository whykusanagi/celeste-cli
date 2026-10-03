// Package promptstest installs the synthetic test persona for tests outside
// package prompts (W5 ruling 19): realistic profile sizes (full ~10.5k
// tokens, spine ~6k, lite ~3k) through the decrypt path, sealed under the
// public test key. The text is filler, not Celeste's persona.
package promptstest

import (
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/prompts/personacrypt/personacrypttest"
)

// Install makes the process run the test persona until t ends. The persona
// is process-wide, so a test that calls it must not be parallel.
func Install(t testing.TB) {
	t.Helper()
	t.Cleanup(prompts.UsePersonaSource(personacrypttest.FS(prompts.VoiceBoundary), personacrypttest.Key))
}
