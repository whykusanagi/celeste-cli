package main

import (
	"strconv"
	"strings"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools/mcp"
)

// mcpPanelApproval is the /mcp panel's tui.MCPApproval: the hooks trust
// store, with the keys and hashes loop.Setup checks a workspace server by.
type mcpPanelApproval struct{ home string }

// source is the trust source of a workspace server; ok is false for a
// server that needs no approval (a home config, or no known file).
func (a mcpPanelApproval) source(name string, cfg mcp.ServerConfig) (hooks.Source, bool) {
	if a.home == "" || cfg.Origin == "" || mcp.IsGlobalConfig(a.home, cfg.Origin) {
		return hooks.Source{}, false
	}
	return hooks.MCPSource(cfg.Origin, name, cfg.TrustSummary(), cfg.TrustHash()), true
}

func (a mcpPanelApproval) State(name string, cfg mcp.ServerConfig) string {
	src, ok := a.source(name, cfg)
	if !ok {
		return ""
	}
	switch hooks.LoadTrust(a.home).Status(src) {
	case hooks.Trusted:
		return "approved"
	case hooks.Declined:
		return "declined"
	}
	return "pending"
}

func (a mcpPanelApproval) Describe(name string, cfg mcp.ServerConfig) string {
	src, ok := a.source(name, cfg)
	if !ok {
		return ""
	}
	var b strings.Builder
	b.WriteString("source: " + strconv.Quote(hooks.SourceFile(src)) + "\n")
	hooks.DescribeSource(&b, src)
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	return strings.Join(lines, "\n")
}

func (a mcpPanelApproval) Approve(name string, cfg mcp.ServerConfig) error {
	src, ok := a.source(name, cfg)
	if !ok {
		return nil
	}
	store := hooks.LoadTrust(a.home)
	if err := store.Err(); err != nil {
		return err
	}
	return store.Approve(src)
}
