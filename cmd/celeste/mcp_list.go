package main

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

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

	show := func(path string, isGlobal bool) string {
		base, prefix := cwd, "."
		if isGlobal {
			base, prefix = home, "~"
		}
		if rel, err := filepath.Rel(base, path); err == nil {
			return hooks.SafeText(filepath.Join(prefix, rel))
		}
		return hooks.SafeText(path)
	}

	code := 0
	var entries []mcpListEntry
	var good []string // the files that parse, in precedence order
	// The first file that does not parse: the runtime then starts no server
	// at all where it is loaded (LoadMerged fails as a whole), so a home
	// file stops every mode and a workspace file stops the chat.
	var homeBad, wsBad string
	// DiscoverConfigPaths is lowest precedence first: a later file defining
	// the same name replaces the earlier definition.
	for _, p := range lastOccurrences(mcp.DiscoverConfigPaths(cwd, home)) {
		isGlobal := mcp.IsGlobalConfig(home, p)
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
		good = append(good, p)
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
	// Which definition each mode starts comes from the runtime's own code
	// (loop.setupMCP): LoadMerged over every config for the chat, over the
	// home configs alone for every other mode. The files were just parsed,
	// but one edited since can fail now: report it rather than guess.
	chat, err := mcp.LoadMerged(good)
	if err != nil {
		fmt.Fprintf(errOut, "Error: %v\n", err)
		return 1
	}
	homeOnly, _ := mcp.SplitGlobal(good, home)
	other, err := mcp.LoadMerged(homeOnly)
	if err != nil {
		fmt.Fprintf(errOut, "Error: %v\n", err)
		return 1
	}

	rows := [][]string{{"NAME", "SOURCE", "TRANSPORT", "ENABLED", "TRUSTED", "APPROVAL", "RUNS IN"}}
	for _, e := range entries {
		trusted := "no"
		if e.cfg.Trusted {
			trusted = "yes"
			if !e.global {
				trusted = "ignored"
			}
		}
		ap := approval(e)
		runs := mcpRunsIn(e, chat.Servers[e.name].Origin, other.Servers[e.name].Origin, func(p string) string { return show(p, mcp.IsGlobalConfig(home, p)) }, ap == "approved")
		switch {
		case homeBad != "":
			runs = "none (" + homeBad + " does not parse)"
		case wsBad != "" && !e.global:
			runs = "none (" + wsBad + " does not parse)"
		case wsBad != "" && e.cfg.Enabled && strings.HasPrefix(runs, "all"):
			runs = "all but chat (" + wsBad + " does not parse)"
		}
		rows = append(rows, []string{
			hooks.SafeText(e.name), where(e), hooks.SafeText(e.cfg.Transport), yesNo(e.cfg.Enabled), trusted, ap, runs})
	}
	writeColumns(out, rows)
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

// mcpRunsIn says where e is the definition celeste starts, from the
// runtime's merges: chatOrigin is the file whose definition of e.name the
// chat starts, otherOrigin the one every other mode starts ("" for none).
// Only an enabled server starts on its own, and a workspace one only once
// approved.
func mcpRunsIn(e mcpListEntry, chatOrigin, otherOrigin string, show func(string) string, approved bool) string {
	same := func(p string) bool { return p != "" && filepath.Clean(p) == filepath.Clean(e.path) }
	inChat, inOther := same(chatOrigin), e.global && same(otherOrigin)
	// An empty origin means the server left its file between the two
	// reads (review m4): say so rather than name no file.
	const removed = "removed since listing"
	src := func(p string) string {
		if p == "" {
			return "nothing, " + removed
		}
		return show(p)
	}
	overridden := func(p string) string {
		if p == "" {
			return "none (" + removed + ")"
		}
		return "overridden by " + show(p)
	}
	switch {
	case e.global && !inOther:
		return overridden(otherOrigin)
	case !e.global && !inChat:
		return overridden(chatOrigin)
	case !e.cfg.Enabled && !inChat:
		return "off (chat uses " + src(chatOrigin) + ")"
	case !e.cfg.Enabled:
		// Manager.Start skips it; the chat's /mcp panel can still connect it.
		return "off (start it from the chat's /mcp)"
	case !e.global && approved:
		return "chat only"
	case !e.global:
		return "chat once approved"
	case !inChat:
		return "all but chat (chat uses " + src(chatOrigin) + ")"
	}
	return "all modes"
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// writeColumns prints rows as columns two spaces apart, padded by display
// width: tabwriter counts runes, so a wide character (CJK, emoji) shifted
// every column after it (#398 C3). The last column is not padded.
func writeColumns(out io.Writer, rows [][]string) {
	var widths []int
	for _, r := range rows {
		for i, c := range r {
			if i >= len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], lipgloss.Width(c))
		}
	}
	for _, r := range rows {
		var sb strings.Builder
		for i, c := range r {
			sb.WriteString(c)
			if i < len(r)-1 {
				sb.WriteString(strings.Repeat(" ", widths[i]-lipgloss.Width(c)+2))
			}
		}
		fmt.Fprintln(out, sb.String())
	}
}
