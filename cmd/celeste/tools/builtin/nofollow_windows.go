//go:build windows

package builtin

import (
	"os"
)

// openNoFollow opens real, the symlink-resolved path resolvePathReal
// checked, refusing a symlink or other reparse point there (ruling 4).
// Uses a directory handle to verify the parent hasn't changed, mitigating
// TOCTOU attacks where an ancestor is replaced with a symlink.
func openNoFollow(real string) (*os.File, error) {
	return openNoFollowSecure(real)
}
