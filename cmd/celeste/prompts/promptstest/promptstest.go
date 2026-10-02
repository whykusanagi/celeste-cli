// Package promptstest installs the synthetic persona sealed under the
// public test key, for tests outside package prompts (W5 ruling 19).
// Tests that call Install must not call t.Parallel: the persona is
// process-wide.
package promptstest

import (
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/prompts/personacrypt/personacrypttest"
)

// Install makes this test run the test persona: realistic profile sizes
// (full ~10.5k tokens, spine ~6k, lite ~3k) through the real decrypt path.
func Install(t testing.TB) {
	t.Helper()
	t.Cleanup(prompts.UsePersonaSource(personacrypttest.FS(prompts.VoiceBoundary), personacrypttest.Key))
}
