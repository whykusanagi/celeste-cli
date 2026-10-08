//go:build windows

package mcp

// openNonBlock is 0 on Windows, which has no FIFOs at a file path.
const openNonBlock = 0
