package main

import (
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools/mcp"
)

// hooksCLI is the environment `celeste hooks` runs in, injectable for tests.
type hooksCLI struct {
	cwd, home   string
	in          io.Reader
	out, errOut io.Writer
	interactive bool
}

const hooksUsage = `Usage:
  celeste hooks list                  Show hook sources here and whether each is trusted
  celeste hooks trust [--yes] [path]  Approve repo hooks, stream rules, sandbox settings and MCP
                                      servers (path: a directory, a .celeste/hooks.json,
                                      .celeste/config.json, .mcp.json, .celeste/mcp.json or
                                      grimoire file; default: the current directory)
`

func runHooksCommand(args []string) {
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
	os.Exit(hooksCommand(args, hooksCLI{
		cwd: cwd, home: home, in: os.Stdin, out: os.Stdout, errOut: os.Stderr,
		// Both ends must be a terminal, or the y/N prompt could be
		// answered or hidden by whatever is piped in or out.
		interactive: hooks.IsTerminal(os.Stdin) && hooks.IsTerminal(os.Stdout),
	}))
}

func hooksCommand(args []string, c hooksCLI) int {
	if len(args) == 0 {
		fmt.Fprint(c.errOut, hooksUsage)
		return 2
	}
	switch args[0] {
	case "list":
		if len(args) > 1 {
			fmt.Fprintf(c.errOut, "hooks list takes no arguments, got %q\n%s", args[1:], hooksUsage)
			return 2
		}
		return hooksList(c)
	case "trust":
		return hooksTrust(args[1:], c)
	case "help", "-h", "--help":
		fmt.Fprint(c.out, hooksUsage)
		return 0
	}
	fmt.Fprintf(c.errOut, "Unknown hooks command %q\n%s", args[0], hooksUsage)
	return 2
}

func hooksList(c hooksCLI) int {
	srcs, warnings, err := hooks.Discover(c.cwd, c.home)
	if err != nil {
		fmt.Fprintf(c.errOut, "Error: %v\n", err)
		return 1
	}
	sbx, sbxWarns := sandboxSources(c.cwd)
	srv, srvWarns := mcpSources(c.cwd, c.home, "")
	srcs = append(append(srcs, sbx...), srv...)
	for _, w := range append(append(warnings, sbxWarns...), srvWarns...) {
		fmt.Fprintln(c.errOut, w)
	}
	if len(srcs) == 0 {
		fmt.Fprintln(c.out, "No hooks configured.")
		return 0
	}
	store := hooks.LoadTrust(c.home)
	if err := store.Err(); err != nil {
		fmt.Fprintf(c.errOut, "Warning: %v\n", err)
	}
	for _, s := range srcs {
		fmt.Fprintf(c.out, "%s  [%s, %s]\n", strconv.Quote(s.Path), s.Kind, store.Status(s))
		hooks.DescribeSource(c.out, s)
	}
	return 0
}

func hooksTrust(args []string, c hooksCLI) int {
	yes := false
	var paths []string
	flags := true
	for _, a := range args {
		switch {
		case flags && a == "--":
			flags = false
		case flags && (a == "--yes" || a == "-y"):
			yes = true
		case flags && strings.HasPrefix(a, "-"):
			fmt.Fprintf(c.errOut, "Unknown flag %q for hooks trust\n%s", a, hooksUsage)
			return 2
		default:
			paths = append(paths, a)
		}
	}
	if len(paths) > 1 {
		fmt.Fprintf(c.errOut, "hooks trust takes at most one path, got %q\n%s", paths, hooksUsage)
		return 2
	}
	target := c.cwd
	if len(paths) == 1 {
		target = paths[0]
	}
	srcs, warnings, err := trustSources(target, c.home)
	for _, w := range warnings {
		fmt.Fprintln(c.errOut, w)
	}
	if err != nil {
		fmt.Fprintf(c.errOut, "Error: %v\n", err)
		return 1
	}
	store := hooks.LoadTrust(c.home)
	if err := store.Err(); err != nil {
		fmt.Fprintf(c.errOut, "Error: %v\n", err)
		return 1
	}
	var pending []hooks.Source
	for _, s := range srcs {
		if store.Status(s) != hooks.Trusted {
			pending = append(pending, s)
		}
	}
	if len(srcs) == 0 {
		fmt.Fprintf(c.out, "Nothing to trust: no hook sources found at %s.\n", strconv.Quote(target))
		return 0
	}
	if len(pending) == 0 {
		fmt.Fprintln(c.out, "Nothing to trust: every hook source here is already trusted.")
		return 0
	}

	var approve hooks.ApproveFunc
	switch {
	case yes:
		approve = func(src hooks.Source, st hooks.TrustStatus) bool {
			fmt.Fprintf(c.out, "Trusting %s (%s, was %s):\n", strconv.Quote(src.Path), src.Kind, st)
			hooks.DescribeSource(c.out, src)
			return true
		}
	case c.interactive:
		approve = hooks.PromptApprover(c.in, c.out)
	default:
		fmt.Fprintln(c.errOut, "Refusing to trust hooks without confirmation: stdin/stdout is not a terminal. Review `celeste hooks list`, then re-run with --yes.")
		return 1
	}

	code := 0
	for _, s := range pending {
		if !approve(s, store.Status(s)) {
			fmt.Fprintf(c.out, "Skipped %s\n", strconv.Quote(s.Path))
			continue
		}
		if err := store.Approve(s); err != nil {
			fmt.Fprintf(c.errOut, "Error: %v\n", err)
			code = 1
			continue
		}
		fmt.Fprintf(c.out, "Trusted %s\n", strconv.Quote(s.Path))
	}
	return code
}

// trustSources is what `celeste hooks trust` acts on: hooks.SourcesAt,
// plus the workspace's sandbox settings and MCP servers for a directory, or
// only those of a .celeste/config.json, .mcp.json or .celeste/mcp.json file.
func trustSources(target, home string) ([]hooks.Source, []string, error) {
	abs, err := filepath.Abs(target)
	if err != nil {
		return nil, nil, err
	}
	if filepath.Base(abs) == "config.json" && filepath.Base(filepath.Dir(abs)) == ".celeste" {
		if _, err := os.Stat(abs); err != nil {
			return nil, nil, err
		}
		srcs, warns := sandboxSources(filepath.Dir(filepath.Dir(abs)))
		return srcs, warns, nil
	}
	if isWorkspaceMCPFile(abs) {
		if _, err := os.Stat(abs); err != nil {
			return nil, nil, err
		}
		dir := filepath.Dir(abs)
		if filepath.Base(dir) == ".celeste" {
			dir = filepath.Dir(dir)
		}
		srcs, warns := mcpSources(dir, home, abs)
		return srcs, warns, nil
	}
	srcs, warns, err := hooks.SourcesAt(abs, home)
	if err != nil {
		return srcs, warns, err
	}
	if info, statErr := os.Stat(abs); statErr == nil && info.IsDir() {
		sbx, sbxWarns := sandboxSources(abs)
		srv, srvWarns := mcpSources(abs, home, "")
		srcs, warns = append(append(srcs, sbx...), srv...), append(append(warns, sbxWarns...), srvWarns...)
	}
	return srcs, warns, nil
}

// sandboxSources is the trust source for workspace's .celeste/config.json
// "sandbox" settings when they loosen the sandbox (tightening needs no
// trust), refusing a symlinked file as loop.Setup does.
func sandboxSources(workspace string) ([]hooks.Source, []string) {
	s, path, body, err := config.LoadWorkspaceSandbox(workspace)
	if err != nil {
		return nil, []string{fmt.Sprintf("sandbox: skipping %s: %v", strconv.Quote(path), err)}
	}
	if !s.Loosens() {
		return nil, nil
	}
	if err := hooks.CheckRepoSandbox(path); err != nil {
		return nil, []string{fmt.Sprintf("sandbox: skipping %s: %v", strconv.Quote(path), err)}
	}
	return []hooks.Source{hooks.SandboxSource(path, body)}, nil
}

// isWorkspaceMCPFile reports whether path is named like a workspace MCP
// config: .mcp.json, or mcp.json in a .celeste directory.
func isWorkspaceMCPFile(path string) bool {
	base := filepath.Base(path)
	return base == ".mcp.json" || (base == "mcp.json" && filepath.Base(filepath.Dir(path)) == ".celeste")
}

// mcpSources is a trust source for each server in workspace's .mcp.json
// and .celeste/mcp.json (only file's, when file is set), keyed and hashed as
// loop.Setup checks them before the chat starts one. Home-level configs need
// no approval and are left out.
func mcpSources(workspace, home, file string) ([]hooks.Source, []string) {
	var srcs []hooks.Source
	var warns []string
	for _, p := range mcp.DiscoverConfigPaths(workspace, home) {
		if mcp.IsGlobalConfig(home, p) || (file != "" && filepath.Clean(p) != filepath.Clean(file)) {
			continue
		}
		cfg, err := mcp.LoadConfig(p)
		if err != nil {
			warns = append(warns, fmt.Sprintf("mcp: skipping %s: %v", strconv.Quote(p), err))
			continue
		}
		for _, name := range slices.Sorted(maps.Keys(cfg.Servers)) {
			sc := cfg.Servers[name]
			srcs = append(srcs, hooks.MCPSource(p, name, sc.TrustSummary(), sc.TrustHash()))
		}
	}
	return srcs, warns
}
