//go:build !(darwin || linux || freebsd || openbsd || netbsd || dragonfly || windows)

package codegraph

import "os"

// lockIsNoop reports whether lockFile excludes nothing on this platform.
const lockIsNoop = true

// lockFile has no OS file lock to take on this platform; the owner tokens
// in the marks still keep one indexer from clearing another's.
func lockFile(*os.File) error { return nil }

func unlockFile(*os.File) error { return nil }
