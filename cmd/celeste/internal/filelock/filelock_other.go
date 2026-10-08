//go:build !(darwin || linux || freebsd || openbsd || netbsd || dragonfly || windows)

package filelock

import "os"

// Supported reports whether Lock excludes anything on this platform.
const Supported = false

func tryLock(*os.File) error { return nil }

func unlockFile(*os.File) error { return nil }
