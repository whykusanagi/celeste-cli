//go:build unix

package checkpoints

import "syscall"

// oNoFollow makes an open fail on a symlink in the final component.
const oNoFollow = syscall.O_NOFOLLOW

// oNonblock keeps an open from waiting on a FIFO or device.
const oNonblock = syscall.O_NONBLOCK
