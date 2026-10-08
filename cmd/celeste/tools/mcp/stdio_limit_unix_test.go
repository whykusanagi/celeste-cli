//go:build !windows

package mcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestStdioTransportRejectsOversizedLine: a response line over the limit
// fails Receive instead of growing without bound (Aikido 806869944).
func TestStdioTransportRejectsOversizedLine(t *testing.T) {
	withResponseLimit(t, 1024)
	script := `printf '{"jsonrpc":"2.0","id":1,"result":"'; head -c 8192 /dev/zero | tr '\000' a; printf '"}\n'; sleep 5`
	tr, err := newStdioTransport("", "sh", []string{"-c", script}, nil)
	require.NoError(t, err)
	defer tr.Close()
	_, err = tr.Receive()
	require.Error(t, err)
	require.Contains(t, err.Error(), "too large")
}
