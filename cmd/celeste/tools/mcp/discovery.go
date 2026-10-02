// cmd/celeste/tools/mcp/discovery.go
package mcp

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// discoverAndRegister queries the MCP server for available tools via
// tools/list, creates an MCPTool adapter for each, and registers them in the
// registry. The serverName is recorded in each tool's metadata for debugging
// and display. A non-nil resolve makes each tool look its server's client up
// at call time (MCPTool.resolve); with nil the tool keeps client. trusted
// honours the tools' readOnlyHint (ServerConfig.Trusted). It returns the
// names it registered: a tool whose name is taken is skipped with a warning.
func discoverAndRegister(ctx context.Context, client *Client, registry *tools.Registry, serverName string, resolve func(string) (*Client, bool), trusted bool) ([]string, error) {
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
		tool.resolve = resolve
		tool.trusted = trusted
		name := tool.Name()
		if !registerTool(registry, tool, func(s string) { log.Print(s) }) {
			continue
		}
		names = append(names, name)
		if verbose {
			log.Printf("[mcp] registered tool %q from server %q as %q", def.Name, serverName, name)
		}
	}

	if len(defs) > 0 {
		log.Printf("[mcp] discovered %d tools from server %q", len(defs), serverName)
	}

	return names, nil
}

// registerTool adds tool unless its name is taken (another server whose name
// sanitizes alike, or a custom tool); then it warns, naming both owners, and
// returns false. A registered MCP tool starts hidden; find_tools activates
// it on demand (only when discovery mode is on).
func registerTool(reg *tools.Registry, tool *MCPTool, warn func(string)) bool {
	if err := reg.Add(tool); err != nil {
		owner := "another tool"
		if old, ok := reg.Get(tool.Name()); ok {
			if o, ok := old.(*MCPTool); ok {
				owner = fmt.Sprintf("server %q tool %q", o.serverName, o.def.Name)
			} else {
				owner = fmt.Sprintf("%T", old)
			}
		}
		warn(fmt.Sprintf("[mcp] server %q tool %q not registered: %v (held by %s)", tool.serverName, tool.def.Name, err, owner))
		return false
	}
	reg.SetHidden(tool.Name(), true)
	return true
}
