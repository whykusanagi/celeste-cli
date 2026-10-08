//go:build !windows

package mcp

import "syscall"

// openNonBlock keeps opening a FIFO from blocking until a writer appears.
const openNonBlock = syscall.O_NONBLOCK
