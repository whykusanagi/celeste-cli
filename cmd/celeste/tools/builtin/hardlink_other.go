//go:build !unix && !windows

package builtin

import "os"

// hardLinked: the link count is not available here, so every file is
// replaced by a rename.
func hardLinked(string, os.FileInfo) bool { return false }

// oNoFollow: not available here; os.Root still keeps opens inside the
// workspace.
const oNoFollow = 0
