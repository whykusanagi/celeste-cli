package main

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"unicode"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools/mcp"
)

const mcpListUsage = `Usage: celeste mcp list

Lists the MCP servers celeste reads from your home configs (~/.celeste,
~/.claude and ~/.cursor mcp.json) and this directory's .mcp.json and
.celeste/mcp.json: each server's source, transport, whether it is enabled
and trusted, and where it runs. Commands, arguments, URLs and env values
are never shown.`

// mcpListEntry is one server as one config file defines it.
type mcpListEntry struct {
	name   string
	cfg    mcp.ServerConfig
	path   string
	global bool
}

// mcpListCommand implements `celeste mcp list` for the workspace cwd.
func mcpListCommand(args []string, cwd, home string, out, errOut io.Writer) int {
	if len(args) > 0 {
		if isHelpFlag(args[0]) {
			fmt.Fprintln(out, mcpListUsage)
			return 0
		}
		fmt.Fprintf(errOut, "mcp list takes no arguments, got %q\n%s\n", args, mcpListUsage)
		return 2
	}

	global := map[string]bool{}
	for _, p := range mcp.GlobalConfigPaths(home) {
		global[filepath.Clean(p)] = true
	}

	code := 0
	var entries []mcpListEntry
	// DiscoverConfigPaths is lowest precedence first: a later file defining
	// the same name replaces the earlier definition.
	for _, p := range mcp.DiscoverConfigPaths(cwd, home) {
		cfg, err := mcp.LoadConfig(p)
		if err != nil {
			fmt.Fprintf(errOut, "Error: %v\n", err)
			code = 1
			continue
		}
		names := make([]string, 0, len(cfg.Servers))
		for name := range cfg.Servers {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			entries = append(entries, mcpListEntry{name: name, cfg: cfg.Servers[name], path: p, global: global[filepath.Clean(p)]})
		}
	}
	if len(entries) == 0 {
		if code == 0 {
			fmt.Fprintln(out, "No MCP servers configured.")
		}
		return code
	}

	show := func(e mcpListEntry) string {
		base, prefix := cwd, "."
		if e.global {
			base, prefix = home, "~"
		}
		if rel, err := filepath.Rel(base, e.path); err == nil {
			return safeText(filepath.Join(prefix, rel))
		}
		return safeText(e.path)
	}

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tSOURCE\tTRANSPORT\tENABLED\tTRUSTED\tRUNS IN")
	for i, e := range entries {
		trusted := "no"
		if e.cfg.Trusted {
			trusted = "yes"
			if !e.global {
				trusted = "ignored"
			}
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			safeText(e.name), show(e), safeText(e.cfg.Transport), yesNo(e.cfg.Enabled), trusted,
			mcpRunsIn(entries, i, show))
	}
	_ = tw.Flush()
	fmt.Fprintln(out, `
Workspace configs start only in the interactive chat; agent runs, celeste acp
and MCP chat use the home configs. "trusted" counts only in a home config.`)
	return code
}

// mcpRunsIn says where entries[i] is the definition celeste starts; only an
// enabled server starts on its own. Workspace configs follow every home
// config in precedence, and only the chat loads them (loop.setupMCP).
func mcpRunsIn(entries []mcpListEntry, i int, show func(mcpListEntry) string) string {
	e := entries[i]
	var wsOverride string
	for _, later := range entries[i+1:] {
		if later.name != e.name {
			continue
		}
		if later.global == e.global {
			return "overridden by " + show(later)
		}
		if wsOverride == "" {
			wsOverride = show(later)
		}
	}
	switch {
	case !e.cfg.Enabled && wsOverride != "":
		return "off (chat uses " + wsOverride + ")"
	case !e.cfg.Enabled:
		// Manager.Start skips it; the chat's /mcp panel can still connect it.
		return "off (start it from the chat's /mcp)"
	case !e.global:
		return "chat only"
	case wsOverride != "":
		return "all but chat (chat uses " + wsOverride + ")"
	}
	return "all modes"
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// safeText returns s, or s Go-quoted when it holds characters that would act
// on the terminal: config content is shown, never interpreted.
func safeText(s string) string {
	if strings.IndexFunc(s, func(r rune) bool { return !unicode.IsPrint(r) }) >= 0 {
		return strconv.Quote(s)
	}
	return s
}
