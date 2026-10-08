package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/atomicfile"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/privfs"
)

// mcpServerName is the key celeste registers itself under in client configs.
const mcpServerName = "celeste"

// jsonClient is an MCP client whose config is JSON with a top-level mcpServers
// map (Claude Desktop, Claude Code ~/.claude.json, Cursor, and Celeste itself
// all share this shape).
type jsonClient struct {
	slug string
	name string
	path string
}

// jsonInstallClients returns the JSON-config MCP clients celeste can install
// itself into, resolved against home.
func jsonInstallClients(home string) []jsonClient {
	return []jsonClient{
		{"claude-desktop", "Claude Desktop", filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")},
		{"claude-code", "Claude Code", filepath.Join(home, ".claude.json")},
		{"cursor", "Cursor", filepath.Join(home, ".cursor", "mcp.json")},
		{"celeste-cli", "Celeste CLI", filepath.Join(home, ".celeste", "mcp.json")},
	}
}

// runMCPCommand handles `celeste mcp <subcommand>`.
func runMCPCommand(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, subcommandUsage["mcp"])
		os.Exit(1)
	}
	switch args[0] {
	case "install":
		runMCPInstall(args[1:])
	case "list":
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
		os.Exit(mcpListCommand(args[1:], cwd, home, os.Stdout, os.Stderr))
	case "trust", "untrust":
		runMCPTrust(args)
	default:
		fmt.Fprintf(os.Stderr, "Unknown mcp subcommand %q. Try: celeste mcp list, celeste mcp trust, celeste mcp untrust, celeste mcp install\n", args[0])
		os.Exit(1)
	}
}

// runMCPInstall writes celeste (self-located) into MCP client configs, so the
// spawn path can never go stale on reinstall.
func runMCPInstall(args []string) {
	fs := flag.NewFlagSet("mcp install", flag.ExitOnError)
	client := fs.String("client", "all", "which client(s): all|claude-desktop|claude-code|cursor|celeste-cli|codex")
	dryRun := fs.Bool("dry-run", false, "print changes without writing")
	port := fs.Int("port", 0, "if >0, configure SSE transport on this port instead of stdio")
	_ = fs.Parse(args)

	exe, err := resolveSelfPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error locating celeste binary: %v\n", err)
		os.Exit(1)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving home directory: %v\n", err)
		os.Exit(1)
	}

	serveArgs := []string{"serve"}
	if *port > 0 {
		serveArgs = []string{"serve", "--sse", "--port", strconv.Itoa(*port)}
	}

	// Codex uses TOML, not JSON. Rather than take a TOML dependency for a rarely
	// used target, print the block to paste. ponytail: revisit if Codex demand grows.
	if *client == "codex" {
		printCodexBlock(exe, serveArgs)
		return
	}

	entry := map[string]any{"command": exe, "args": serveArgs}
	all := jsonInstallClients(home)
	explicit := *client != "all"

	var targets []jsonClient
	for _, c := range all {
		if explicit && *client != c.slug {
			continue
		}
		targets = append(targets, c)
	}
	if len(targets) == 0 {
		fmt.Fprintf(os.Stderr, "Unknown client %q. Valid: all, claude-desktop, claude-code, cursor, celeste-cli, codex\n", *client)
		os.Exit(1)
	}

	fmt.Printf("Installing celeste MCP server → %s\n", exe)
	for _, c := range targets {
		// In "all" mode, skip clients whose config dir doesn't exist — they are
		// not installed, and creating their dirs would be presumptuous.
		if !explicit {
			if _, statErr := os.Stat(filepath.Dir(c.path)); statErr != nil {
				fmt.Printf("• %s: skipped (not installed)\n", c.name)
				continue
			}
		}
		status, err := upsertJSONConfig(c.path, mcpServerName, entry, *dryRun)
		if err != nil {
			fmt.Printf("✗ %s: %v\n", c.name, err)
			continue
		}
		fmt.Printf("✓ %s: %s (%s)\n", c.name, status, c.path)
	}
}

// resolveSelfPath returns the absolute, symlink-resolved path of the running
// celeste binary, so the value written into client configs is stable.
func resolveSelfPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Abs(exe)
}

// upsertJSONConfig merges {serverName: entry} into the mcpServers map of the
// JSON config at path, preserving every other field, and writes it back
// (backing up an existing file to <path>.bak first). It refuses to write
// through a symlink. With dryRun it reports the intended change without writing.
func upsertJSONConfig(path, serverName string, entry map[string]any, dryRun bool) (string, error) {
	// Never write through a symlink at the config path.
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("refusing to write through symlink %s", path)
	}

	doc := map[string]any{}
	existed := false
	if data, err := os.ReadFile(path); err == nil {
		existed = true
		if len(strings.TrimSpace(string(data))) > 0 {
			if err := json.Unmarshal(data, &doc); err != nil {
				return "", fmt.Errorf("parse %s: %w", path, err)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	servers, _ := doc["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers[serverName] = entry
	doc["mcpServers"] = servers

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	out = append(out, '\n')

	if dryRun {
		return "would write (dry-run)", nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if existed {
		if err := backupFile(path); err != nil {
			return "", err
		}
	}
	// 0600: client configs can hold API keys in env (Aikido 806869435).
	// WriteFile keeps an existing file's mode, so it is tightened too
	// (CodeRabbit review of #427).
	if err := os.WriteFile(path, out, privfs.FilePerm); err != nil {
		return "", err
	}
	if err := os.Chmod(path, privfs.FilePerm); err != nil {
		return "", err
	}
	if existed {
		return "updated", nil
	}
	return "created", nil
}

// backupFile copies path to path+".bak", readable only by the owner: the
// config can hold API keys in env (Aikido 806869435). The .bak is always a
// new 0600 file, and a .bak that is a symlink is refused rather than
// written through.
func backupFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	bak := path + ".bak"
	if fi, err := os.Lstat(bak); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to write through symlink %s", bak)
	}
	if testHookBackupChecked != nil {
		testHookBackupChecked(bak)
	}
	// A new file renamed over bak: a symlink swapped in after the check
	// above is replaced, never followed (Aikido review of #424).
	return atomicfile.Replace(bak, data, privfs.FilePerm)
}

// testHookBackupChecked, when set, runs in backupFile after the symlink
// check. Tests only.
var testHookBackupChecked func(bak string)

// printCodexBlock prints the TOML block to paste into ~/.codex/config.toml.
func printCodexBlock(exe string, serveArgs []string) {
	quoted := make([]string, len(serveArgs))
	for i, a := range serveArgs {
		quoted[i] = strconv.Quote(a)
	}
	fmt.Printf("Codex uses TOML. Add this to ~/.codex/config.toml:\n\n"+
		"[mcp_servers.%s]\ncommand = %s\nargs = [%s]\n",
		mcpServerName, strconv.Quote(exe), strings.Join(quoted, ", "))
}
