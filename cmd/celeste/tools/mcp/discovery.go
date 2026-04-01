// cmd/celeste/tools/mcp/discovery.go
package mcp

import (
	"context"
	"fmt"
	"log"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// DiscoverAndRegister queries the MCP server for available tools via tools/list,
// creates an MCPTool adapter for each, and registers them in the registry.
// The serverName is recorded in each tool's metadata for debugging and display.
func DiscoverAndRegister(ctx context.Context, client *Client, registry *tools.Registry, serverName string) error {
	defs, err := client.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("discover tools from %s: %w", serverName, err)
	}

	for _, def := range defs {
		tool := NewMCPTool(def, client, serverName)
		registry.Register(tool)
		log.Printf("[mcp] registered tool %q from server %q", def.Name, serverName)
	}

	if len(defs) > 0 {
		log.Printf("[mcp] discovered %d tools from server %q", len(defs), serverName)
	}

	return nil
}
