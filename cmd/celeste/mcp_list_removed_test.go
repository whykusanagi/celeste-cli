package main

import (
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools/mcp"
)

// A server deleted from its file between mcp list's two reads has no
// origin in the second read; the row says so instead of ending in
// "overridden by " with nothing after it (review m4).
func TestMCPRunsInServerRemovedSinceListing(t *testing.T) {
	show := func(p string) string { return p }
	cases := []mcpListEntry{
		{name: "g", cfg: mcp.ServerConfig{Enabled: true}, path: "global.json", global: true},
		{name: "w", cfg: mcp.ServerConfig{Enabled: true}, path: "ws.json"},
		{name: "off", cfg: mcp.ServerConfig{}, path: "global.json", global: true},
	}
	for _, e := range cases {
		other := ""
		if e.name == "off" {
			other = e.path
		}
		got := mcpRunsIn(e, "", other, show, "approved")
		if strings.HasSuffix(got, "by ") || strings.Contains(got, "uses )") || !strings.Contains(got, "removed since listing") {
			t.Errorf("%s: mcpRunsIn = %q, want it to say the server was removed since listing", e.name, got)
		}
	}
}
