package rules

import "testing"

// The shell reader must terminate and never panic on any input.
func FuzzDestructiveShell(f *testing.F) {
	for _, s := range []string{"rm -rf x", `sh -c 'rm -rf "$(echo x)"'`, "a\\", "$(", "`", "\"$(rm", "eval eval eval rm -rf /", "ssh -p", "2>&1 >"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		_ = destructiveShell(s, 0)
	})
}
