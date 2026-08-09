package server

// buildCommit is the git commit the running binary was built from. It is set
// once at startup from main's ldflags-stamped CommitSHA.
//
// Why this exists: serverVersion is a release-please constant, so celeste_status
// reports the same string whether the process is a shipped release or a local
// build several merges ahead. That made a stale MCP server undiagnosable — a
// server process left running across a reinstall keeps serving the old binary,
// and nothing in its status output says so. The commit does.
var buildCommit string

// SetBuildCommit records the build commit for status reporting. Called from
// main before the server starts; safe to leave unset (tests, library use).
func SetBuildCommit(sha string) { buildCommit = sha }

// BuildCommit returns the build commit, or "unknown" when the binary was built
// without a stamp (a bare `go build` rather than `make install` or CI).
func BuildCommit() string {
	if buildCommit == "" {
		return "unknown"
	}
	return buildCommit
}
