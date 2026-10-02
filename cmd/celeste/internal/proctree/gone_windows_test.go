//go:build windows

package proctree

import (
	"time"

	"golang.org/x/sys/windows"
)

// gone reports whether process pid has exited within d. A pid that can no
// longer be opened has exited.
func gone(pid int, d time.Duration) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return true
	}
	defer windows.CloseHandle(h)
	ev, err := windows.WaitForSingleObject(h, uint32(d/time.Millisecond))
	return err == nil && ev == windows.WAIT_OBJECT_0
}
