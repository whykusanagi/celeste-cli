package server

import "testing"

// A stale MCP server process is invisible today: celeste_status reports the
// compiled-in serverVersion constant, which is identical whether the running
// binary is a release build or a local build several merges ahead. Reporting
// the build commit makes staleness diagnosable without restarting anything.
func TestBuildCommit_DefaultAndOverride(t *testing.T) {
	orig := buildCommit
	t.Cleanup(func() { buildCommit = orig })

	buildCommit = ""
	if got := BuildCommit(); got != "unknown" {
		t.Errorf("BuildCommit() with no stamp = %q, want %q", got, "unknown")
	}

	SetBuildCommit("4078dec")
	if got := BuildCommit(); got != "4078dec" {
		t.Errorf("BuildCommit() = %q, want %q", got, "4078dec")
	}
}
