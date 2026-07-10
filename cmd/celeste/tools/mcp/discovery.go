// cmd/celeste/tools/mcp/discovery.go
package mcp

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// DiscoverAndRegister queries the MCP server for available tools via tools/list,
// creates an MCPTool adapter for each, and registers them in the registry.
// The serverName is recorded in each tool's metadata for debugging and display.
func DiscoverAndRegister(ctx context.Context, client *Client, registry *tools.Registry, serverName string) ([]string, error) {
	defs, err := client.ListTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("discover tools from %s: %w", serverName, err)
	}

	// Per-tool registration is noisy (dozens of lines at startup, and in TUI
	// mode it interleaves with the rendered UI). Gate it behind CELESTE_MCP_DEBUG;
	// the summary line below is enough for normal use.
	verbose := os.Getenv("CELESTE_MCP_DEBUG") != ""
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		tool := NewMCPTool(def, client, serverName)
		registry.Register(tool)
		names = append(names, def.Name)
		if verbose {
			log.Printf("[mcp] registered tool %q from server %q", def.Name, serverName)
		}
	}

	if len(defs) > 0 {
		log.Printf("[mcp] discovered %d tools from server %q", len(defs), serverName)
	}

	return names, nil
}
