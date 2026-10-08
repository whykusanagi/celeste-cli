//go:build windows

package builtin

import (
	"os"
	"syscall"
)

const fileFlagOpenReparsePoint = 0x00200000

// hardLinked reports a regular file with more than one name. A rename
// would split it from its other names, so atomicWrite rewrites it in place.
func hardLinked(path string, fi os.FileInfo) bool {
	if !fi.Mode().IsRegular() {
		return false
	}
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	h, err := syscall.CreateFile(p, 0,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS|fileFlagOpenReparsePoint, 0)
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(h, &info); err != nil {
		return false
	}
	return info.NumberOfLinks > 1
}

// oNoFollow: Windows has no O_NOFOLLOW; os.Root still keeps opens inside
// the workspace.
const oNoFollow = 0
