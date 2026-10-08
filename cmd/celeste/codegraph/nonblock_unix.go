//go:build unix

package codegraph

import "syscall"

// oNonblock keeps an open from waiting on a FIFO or device.
const oNonblock = syscall.O_NONBLOCK
