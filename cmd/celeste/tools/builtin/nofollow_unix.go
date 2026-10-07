//go:build unix

package builtin

import (
	"os"
)

// openNoFollow opens real, the symlink-resolved path resolvePathReal
// checked, without following a symlink in its final component (ruling 4):
// one swapped in after the check fails instead of escaping the workspace.
// Uses openat relative to a validated parent directory to prevent TOCTOU
// attacks where an ancestor directory is replaced with a symlink.
func openNoFollow(real string) (*os.File, error) {
	return openNoFollowSecure(real)
}
