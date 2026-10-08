package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools/mcp"
)

const mcpTrustUsage = `Usage: celeste mcp trust [--yes] <server>
       celeste mcp untrust <server>

trust approves this directory's MCP server <server> (from .mcp.json or
.celeste/mcp.json), also one you declined: it shows the server's command,
args and source file and asks to confirm. Without a terminal it refuses
unless --yes is given.
  --yes  approve without asking (the server is still shown)

untrust forgets the approval or decline stored for <server> here, so the
chat asks about it again at its next launch.`

// runMCPTrust runs `celeste mcp trust|untrust` in the current directory.
func runMCPTrust(args []string) {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot determine working directory: %v\n", err)
		os.Exit(1)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot determine home directory: %v\n", err)
		os.Exit(1)
	}
	os.Exit(mcpTrustCommand(args, hooksCLI{
		cwd: cwd, home: home, in: os.Stdin, out: os.Stdout, errOut: os.Stderr,
		// Both ends must be a terminal, as for `celeste hooks trust`.
		interactive: hooks.IsTerminal(os.Stdin) && hooks.IsTerminal(os.Stdout),
	}))
}

// mcpTrustCommand implements `celeste mcp trust` and `celeste mcp untrust`;
// args[0] is the subcommand.
func mcpTrustCommand(args []string, c hooksCLI) int {
	sub := args[0]
	yes := false
	var names []string
	flags := true
	for _, a := range args[1:] {
		switch {
		case flags && a == "--":
			flags = false
		case flags && isHelpFlag(a):
			fmt.Fprintln(c.out, mcpTrustUsage)
			return 0
		case flags && sub == "trust" && (a == "--yes" || a == "-y"):
			yes = true
		case flags && strings.HasPrefix(a, "-"):
			fmt.Fprintf(c.errOut, "Unknown flag %q for mcp %s\n%s\n", a, sub, mcpTrustUsage)
			return 2
		default:
			names = append(names, a)
		}
	}
	if len(names) != 1 {
		fmt.Fprintf(c.errOut, "mcp %s takes one server name, got %q\n%s\n", sub, names, mcpTrustUsage)
		return 2
	}
	if sub == "untrust" {
		return mcpUntrust(names[0], c)
	}
	return mcpTrust(names[0], yes, c)
}

// workspaceMCPFiles are the two workspace MCP config candidates in cwd,
// lowest precedence first, whether or not they exist.
func workspaceMCPFiles(cwd string) []string {
	return []string{filepath.Join(cwd, ".mcp.json"), filepath.Join(cwd, ".celeste", "mcp.json")}
}

// showPath is p relative to cwd (the way mcp list shows it), made safe to
// print.
func showPath(cwd, p string) string {
	if rel, err := filepath.Rel(cwd, p); err == nil && !strings.HasPrefix(rel, "..") {
		return hooks.SafeText(filepath.Join(".", rel))
	}
	return hooks.SafeText(p)
}

func mcpTrust(name string, yes bool, c hooksCLI) int {
	// The definition the chat would start: the last workspace file that
	// defines name (LoadMerged's precedence), keyed and hashed as
	// loop.Setup checks it.
	var src hooks.Source
	found := false
	for _, p := range mcp.DiscoverConfigPaths(c.cwd, c.home) {
		if mcp.IsGlobalConfig(c.home, p) {
			continue
		}
		cfg, err := mcp.LoadConfig(p)
		if err != nil {
			fmt.Fprintf(c.errOut, "Error: %v\n", err)
			return 1
		}
		if sc, ok := cfg.Servers[name]; ok {
			src, found = hooks.MCPSource(p, name, sc.TrustSummary(), sc.TrustHash()), true
		}
	}
	if !found {
		for _, p := range mcp.GlobalConfigPaths(c.home) {
			if cfg, err := mcp.LoadConfig(p); err == nil {
				if _, ok := cfg.Servers[name]; ok {
					fmt.Fprintf(c.errOut, "MCP server %s is in your home config %s: it needs no approval.\n", strconv.Quote(name), hooks.SafeText(p))
					return 1
				}
			}
		}
		fmt.Fprintf(c.errOut, "Error: no MCP server %s in this directory's .mcp.json or .celeste/mcp.json (see celeste mcp list)\n", strconv.Quote(name))
		return 1
	}

	store := hooks.LoadTrust(c.home)
	if err := store.Err(); err != nil {
		fmt.Fprintf(c.errOut, "Error: %v\n", err)
		return 1
	}
	status := store.Status(src)
	file := showPath(c.cwd, hooks.SourceFile(src))
	if status == hooks.Trusted {
		fmt.Fprintf(c.out, "MCP server %s in %s is already approved.\n", strconv.Quote(name), file)
		return 0
	}
	fmt.Fprintf(c.out, "MCP server %s in %s (%s):\n", strconv.Quote(name), file, status)
	hooks.DescribeSource(c.out, src)
	fmt.Fprintln(c.out, "Starting it runs this command on this machine with your permissions (or connects to this URL).")
	switch {
	case yes:
	case c.interactive:
		fmt.Fprint(c.out, "Approve it? [y/N]: ")
		line, _ := bufio.NewReader(c.in).ReadString('\n')
		if answer := strings.ToLower(strings.TrimSpace(line)); answer != "y" && answer != "yes" {
			if line == "" {
				fmt.Fprintln(c.out)
			}
			fmt.Fprintln(c.out, "Not approved; nothing changed.")
			return 1
		}
	default:
		fmt.Fprintln(c.errOut, "Refusing to approve without confirmation: stdin/stdout is not a terminal. Review the server above, then re-run with --yes.")
		return 1
	}
	if err := store.Approve(src); err != nil {
		fmt.Fprintf(c.errOut, "Error: %v\n", err)
		return 1
	}
	fmt.Fprintf(c.out, "Approved %s; the chat starts it at its next launch while it is enabled.\n", strconv.Quote(name))
	return 0
}

func mcpUntrust(name string, c hooksCLI) int {
	store := hooks.LoadTrust(c.home)
	if err := store.Err(); err != nil {
		fmt.Fprintf(c.errOut, "Error: %v\n", err)
		return 1
	}
	// Both files' keys, whether or not the server (or the file) is still
	// there: a removed server's decision can be forgotten too.
	forgot := false
	for _, p := range workspaceMCPFiles(c.cwd) {
		ok, err := store.Forget(hooks.MCPSource(p, name, "", "").Path)
		if err != nil {
			fmt.Fprintf(c.errOut, "Error: %v\n", err)
			return 1
		}
		if ok {
			forgot = true
			fmt.Fprintf(c.out, "Forgot the decision on MCP server %s in %s; the chat asks about it again.\n", strconv.Quote(name), showPath(c.cwd, p))
		}
	}
	if !forgot {
		fmt.Fprintf(c.out, "No approval or decline stored for MCP server %s here.\n", strconv.Quote(name))
	}
	return 0
}
