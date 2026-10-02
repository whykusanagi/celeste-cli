//go:build unix

package proctree

import (
	"syscall"
	"time"
)

// gone reports whether process pid has exited (and been reaped) within d.
func gone(pid int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
	return true
}
