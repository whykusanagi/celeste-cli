//go:build unix

package checkpoints

import "syscall"

// oNoFollow makes an open fail on a symlink in the final component.
const oNoFollow = syscall.O_NOFOLLOW
