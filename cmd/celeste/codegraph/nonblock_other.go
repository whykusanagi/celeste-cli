//go:build !unix

package codegraph

// oNonblock: no FIFOs to wait on here.
const oNonblock = 0
