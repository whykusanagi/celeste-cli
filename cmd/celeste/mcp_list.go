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

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools/mcp"
)

const mcpListUsage = `Usage: celeste mcp list

Lists the MCP servers celeste reads from your home configs (~/.celeste,
~/.claude and ~/.cursor mcp.json) and this directory's .mcp.json and
.celeste/mcp.json: each server's source, transport, whether it is enabled
and trusted, whether a workspace server is approved to start, and where it
runs. Commands, arguments, URLs and env values are never shown.`

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

	show := func(path string, isGlobal bool) string {
		base, prefix := cwd, "."
		if isGlobal {
			base, prefix = home, "~"
		}
		if rel, err := filepath.Rel(base, path); err == nil {
			return safeText(filepath.Join(prefix, rel))
		}
		return safeText(path)
	}

	code := 0
	var entries []mcpListEntry
	// The first file that does not parse: the runtime then starts no server
	// at all where it is loaded (LoadMerged fails as a whole), so a home
	// file stops every mode and a workspace file stops the chat.
	var homeBad, wsBad string
	// DiscoverConfigPaths is lowest precedence first: a later file defining
	// the same name replaces the earlier definition.
	for _, p := range lastOccurrences(mcp.DiscoverConfigPaths(cwd, home)) {
		isGlobal := global[filepath.Clean(p)]
		cfg, err := mcp.LoadConfig(p)
		if err != nil {
			fmt.Fprintf(errOut, "Error: %v\n", err)
			code = 1
			switch {
			case isGlobal && homeBad == "":
				homeBad = show(p, true)
			case !isGlobal && wsBad == "":
				wsBad = show(p, false)
			}
			continue
		}
		names := make([]string, 0, len(cfg.Servers))
		for name := range cfg.Servers {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			entries = append(entries, mcpListEntry{name: name, cfg: cfg.Servers[name], path: p, global: isGlobal})
		}
	}
	switch {
	case homeBad != "":
		fmt.Fprintf(errOut, "celeste starts no MCP servers in any mode until %s is fixed.\n", homeBad)
	case wsBad != "":
		fmt.Fprintf(errOut, "In this directory the chat starts no MCP servers until %s is fixed.\n", wsBad)
	}
	if len(entries) == 0 {
		if code == 0 {
			fmt.Fprintln(out, "No MCP servers configured.")
		}
		return code
	}

	store := hooks.LoadTrust(home)
	if err := store.Err(); err != nil {
		fmt.Fprintf(errOut, "Warning: %v; repository servers show as pending until it is fixed\n", err)
	}
	approval := func(e mcpListEntry) string {
		if e.global {
			return "-" // the user's own config: no approval needed
		}
		switch store.Status(hooks.MCPSource(e.path, e.name, e.cfg.TrustSummary(), e.cfg.TrustHash())) {
		case hooks.Trusted:
			return "approved"
		case hooks.Changed:
			return "pending (changed)"
		}
		return "pending"
	}
	where := func(e mcpListEntry) string { return show(e.path, e.global) }

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tSOURCE\tTRANSPORT\tENABLED\tTRUSTED\tAPPROVAL\tRUNS IN")
	for i, e := range entries {
		trusted := "no"
		if e.cfg.Trusted {
			trusted = "yes"
			if !e.global {
				trusted = "ignored"
			}
		}
		ap := approval(e)
		runs := mcpRunsIn(entries, i, where, ap == "approved")
		switch {
		case homeBad != "":
			runs = "none (" + homeBad + " does not parse)"
		case wsBad != "" && !e.global:
			runs = "none (" + wsBad + " does not parse)"
		case wsBad != "" && e.cfg.Enabled && strings.HasPrefix(runs, "all"):
			runs = "all but chat (" + wsBad + " does not parse)"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			safeText(e.name), where(e), safeText(e.cfg.Transport), yesNo(e.cfg.Enabled), trusted, ap, runs)
	}
	_ = tw.Flush()
	fmt.Fprintln(out, `
Workspace configs start only in the interactive chat, and each enabled
server there only once you approve it (the chat asks at launch, or run
celeste hooks trust); agent runs, celeste acp and MCP chat use the home
configs. "trusted" counts only in a home config.`)
	return code
}

// lastOccurrences drops a path that appears again later, keeping the
// later one: run from the home directory, ~/.celeste/mcp.json is both a
// home and a workspace candidate, and the later position is the
// precedence it has at runtime.
func lastOccurrences(paths []string) []string {
	var out []string
	for i, p := range paths {
		dup := false
		for _, later := range paths[i+1:] {
			if filepath.Clean(later) == filepath.Clean(p) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, p)
		}
	}
	return out
}

// mcpRunsIn says where entries[i] is the definition celeste starts; only an
// enabled server starts on its own, and a workspace one only once approved.
// Workspace configs follow every home config in precedence, and only the
// chat loads them (loop.setupMCP).
func mcpRunsIn(entries []mcpListEntry, i int, show func(mcpListEntry) string, approved bool) string {
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
	case !e.global && approved:
		return "chat only"
	case !e.global:
		return "chat once approved"
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
