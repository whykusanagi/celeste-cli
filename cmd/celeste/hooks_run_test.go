package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/rules"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools/mcp"
)

func hooksCLIFixture(t *testing.T, in string, interactive bool) (hooksCLI, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws := t.TempDir()
	writeHooksFile(t, filepath.Join(home, ".celeste", "hooks.json"), hooks.Definition{Event: hooks.EventStop, Command: "echo global"})
	writeHooksFile(t, filepath.Join(ws, ".celeste", "hooks.json"), hooks.Definition{Event: hooks.EventPreToolUse, Command: "echo repo"})
	var out, errOut bytes.Buffer
	return hooksCLI{cwd: ws, home: home, in: strings.NewReader(in), out: &out, errOut: &errOut, interactive: interactive}, &out, &errOut
}

func repoLoaded(t *testing.T, c hooksCLI) bool {
	t.Helper()
	r, err := hooks.Load(hooks.Options{Workspace: c.cwd, Home: c.home, Warn: func(string) {}})
	if err != nil {
		t.Fatal(err)
	}
	return r.Has(hooks.EventPreToolUse)
}

func TestHooksListShowsTrustStatus(t *testing.T) {
	c, out, _ := hooksCLIFixture(t, "", false)
	if code := hooksCommand([]string{"list"}, c); code != 0 {
		t.Fatalf("exit %d", code)
	}
	s := out.String()
	for _, want := range []string{"[global, trusted]", "[repo, untrusted]", `"echo repo"`, "PreToolUse"} {
		if !strings.Contains(s, want) {
			t.Errorf("list output missing %q:\n%s", want, s)
		}
	}
}

func TestHooksTrustRefusesWithoutConfirmation(t *testing.T) {
	c, _, errOut := hooksCLIFixture(t, "", false)
	if code := hooksCommand([]string{"trust"}, c); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "--yes") {
		t.Fatalf("stderr = %q", errOut.String())
	}
	if _, err := os.Stat(hooks.TrustPath(c.home)); err == nil {
		t.Fatal("trusted.json written without confirmation")
	}
	if repoLoaded(t, c) {
		t.Fatal("repo hooks trusted without confirmation")
	}
}

func TestHooksTrustYes(t *testing.T) {
	c, out, _ := hooksCLIFixture(t, "", false)
	if code := hooksCommand([]string{"trust", "--yes"}, c); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out.String(), "echo repo") {
		t.Fatalf("trust did not show what it approved:\n%s", out.String())
	}
	if !repoLoaded(t, c) {
		t.Fatal("repo hooks not trusted after --yes")
	}
	out.Reset()
	hooksCommand([]string{"trust", "--yes"}, c)
	if !strings.Contains(out.String(), "Nothing to trust") {
		t.Fatalf("second trust = %q", out.String())
	}
}

func TestHooksTrustInteractive(t *testing.T) {
	c, _, _ := hooksCLIFixture(t, "n\n", true)
	hooksCommand([]string{"trust"}, c)
	if repoLoaded(t, c) {
		t.Fatal("declined hooks were trusted")
	}
	c.in = strings.NewReader("y\n")
	if code := hooksCommand([]string{"trust"}, c); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !repoLoaded(t, c) {
		t.Fatal("approved hooks not trusted")
	}
}

func TestHooksTrustFilePath(t *testing.T) {
	c, _, _ := hooksCLIFixture(t, "", false)
	if code := hooksCommand([]string{"trust", "--yes", filepath.Join(c.cwd, ".celeste", "hooks.json")}, c); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !repoLoaded(t, c) {
		t.Fatal("file-path trust did not approve the file")
	}
}

func TestHooksTrustRejectsFilesCelesteDoesNotLoad(t *testing.T) {
	c, _, errOut := hooksCLIFixture(t, "", false)
	stray := filepath.Join(c.cwd, "hooks.json")
	writeHooksFile(t, stray, hooks.Definition{Event: hooks.EventStop, Command: "echo stray"})
	if code := hooksCommand([]string{"trust", "--yes", stray}, c); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "not a hook file") {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

func TestHooksUsage(t *testing.T) {
	c, _, errOut := hooksCLIFixture(t, "", false)
	if code := hooksCommand(nil, c); code != 2 || !strings.Contains(errOut.String(), "celeste hooks list") {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
}

// Final review fix 3: finding nothing is not "already trusted".
func TestHooksTrustNoSources(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	var out, errOut bytes.Buffer
	c := hooksCLI{cwd: t.TempDir(), home: home, in: strings.NewReader(""), out: &out, errOut: &errOut}
	if code := hooksCommand([]string{"trust", "--yes"}, c); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if s := out.String(); !strings.Contains(s, "no hook sources found") || strings.Contains(s, "already trusted") {
		t.Fatalf("stdout = %q", s)
	}
}

// Final review fix 3: extra arguments and unknown flags are usage errors,
// not silently a different path.
func TestHooksRejectsExtraArgsAndUnknownFlags(t *testing.T) {
	for _, args := range [][]string{
		{"list", "extra"},
		{"trust", "--force"},
		{"trust", "-n"},
		{"trust", "--yes", "a", "b"},
	} {
		c, _, errOut := hooksCLIFixture(t, "", false)
		if code := hooksCommand(args, c); code != 2 || !strings.Contains(errOut.String(), "Usage:") {
			t.Errorf("%q: exit %d, stderr %q; want 2 with usage", args, code, errOut.String())
		}
		if repoLoaded(t, c) {
			t.Errorf("%q trusted repo hooks", args)
		}
	}
	c, _, _ := hooksCLIFixture(t, "", false)
	if code := hooksCommand([]string{"trust", "--yes", "--", c.cwd}, c); code != 0 || !repoLoaded(t, c) {
		t.Fatalf("-- path: exit %d", code)
	}
}

// A repo grimoire's "## Stream Rules" is listed and trusted like repo hooks
// (2.0 W3, ruling 18): `celeste hooks trust` approves it in the same store,
// and hooks.Load neither runs nor warns about it.
func TestHooksTrustCoversStreamRules(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws := t.TempDir()
	grim := filepath.Join(ws, ".grimoire")
	body := "### no-baz\n---\ncondition: baz\n---\nNo baz."
	if err := os.WriteFile(grim, []byte("## Stream Rules\n"+body+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	c := hooksCLI{cwd: ws, home: home, in: strings.NewReader(""), out: &out, errOut: &errOut}
	if code := hooksCommand([]string{"list"}, c); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if s := out.String(); !strings.Contains(s, "[repo-stream-rules, untrusted]") || !strings.Contains(s, "condition: baz") {
		t.Fatalf("list output:\n%s", s)
	}
	sec := []rules.Section{{Source: grim, Body: body}}
	if got := rules.Trusted(home, sec, nil, func(string) {}); len(got) != 0 {
		t.Fatal("stream rules trusted before `hooks trust`")
	}
	out.Reset()
	if code := hooksCommand([]string{"trust", "--yes"}, c); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if got := rules.Trusted(home, sec, nil, func(string) {}); len(got) != 1 {
		t.Fatalf("stream rules not trusted after --yes:\n%s", out.String())
	}
	var warns []string
	if _, err := hooks.Load(hooks.Options{Workspace: ws, Home: home, Warn: func(s string) { warns = append(warns, s) }}); err != nil || len(warns) != 0 {
		t.Errorf("hooks.Load must ignore stream rule sources: err=%v warns=%v", err, warns)
	}
}

func TestHooksListShowsSandboxFile(t *testing.T) {
	c, out, _ := hooksCLIFixture(t, "", false)
	writeFile(t, filepath.Join(c.cwd, ".celeste"), "config.json", `{"sandbox":{"enabled":false}}`)
	if code := hooksCommand([]string{"list"}, c); code != 0 {
		t.Fatalf("exit %d", code)
	}
	s := out.String()
	if !strings.Contains(s, "config.json#sandbox") || !strings.Contains(s, "repo-sandbox, untrusted") {
		t.Fatalf("list = %s", s)
	}
}

func TestHooksTrustApprovesSandboxFile(t *testing.T) {
	c, out, _ := hooksCLIFixture(t, "", false)
	p := filepath.Join(c.cwd, ".celeste", "config.json")
	writeFile(t, filepath.Dir(p), "config.json", `{"sandbox":{"writable":["build"]}}`)
	if code := hooksCommand([]string{"trust", "--yes", p}, c); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "Trusted") {
		t.Fatalf("out = %s", out.String())
	}
	srcs, _ := sandboxSources(c.cwd)
	if len(srcs) != 1 || hooks.LoadTrust(c.home).Status(srcs[0]) != hooks.Trusted {
		t.Fatalf("not trusted: %+v", srcs)
	}
	// A tightening-only file has nothing to trust.
	writeFile(t, filepath.Dir(p), "config.json", `{"sandbox":{"network":false}}`)
	if srcs, _ := sandboxSources(c.cwd); len(srcs) != 0 {
		t.Fatalf("tightening listed: %+v", srcs)
	}
}

// A workspace's MCP servers are listed and approved like repo hooks, with
// the key and hash the chat checks before starting one; a home config's
// servers need no approval and are not listed.
func TestHooksTrustApprovesWorkspaceMCPServers(t *testing.T) {
	c, out, _ := hooksCLIFixture(t, "", false)
	writeFile(t, filepath.Join(c.home, ".celeste"), "mcp.json", `{"mcpServers":{"mine":{"command":"m","enabled":true}}}`)
	writeFile(t, c.cwd, ".mcp.json", `{"mcpServers":{"repo":{"command":"r","args":["-x"],"enabled":true}}}`)
	if code := hooksCommand([]string{"list"}, c); code != 0 {
		t.Fatalf("list exit %d", code)
	}
	if s := out.String(); !strings.Contains(s, ".mcp.json#mcp:repo") || !strings.Contains(s, "repo-mcp, untrusted") || strings.Contains(s, "#mcp:mine") {
		t.Fatalf("list = %s", s)
	}

	out.Reset()
	p := filepath.Join(c.cwd, ".mcp.json")
	if code := hooksCommand([]string{"trust", "--yes", p}, c); code != 0 {
		t.Fatalf("trust exit %d: %s", code, out.String())
	}
	sc := mcp.ServerConfig{Transport: "stdio", Command: "r", Args: []string{"-x"}}
	if st := hooks.LoadTrust(c.home).Status(hooks.MCPSource(p, "repo", sc.TrustSummary(), sc.TrustHash())); st != hooks.Trusted {
		t.Fatalf("status after trust = %s\n%s", st, out.String())
	}
	if !strings.Contains(out.String(), `"r"`) {
		t.Errorf("trust did not show the command it approves:\n%s", out.String())
	}
}

// #411: hooks declined in the chat are remembered; `celeste hooks trust`
// lists them as declined and approves them only after a confirmed yes.
func TestHooksTrustOverridesADecline(t *testing.T) {
	c, out, _ := hooksCLIFixture(t, "n\n", true)
	srcs, _, err := hooks.Discover(c.cwd, c.home)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range srcs {
		if !s.Global() {
			if err := hooks.LoadTrust(c.home).Decline(s); err != nil {
				t.Fatal(err)
			}
		}
	}
	if code := hooksCommand([]string{"list"}, c); code != 0 || !strings.Contains(out.String(), "[repo, declined]") {
		t.Fatalf("list exit %d:\n%s", code, out.String())
	}

	out.Reset()
	hooksCommand([]string{"trust"}, c)
	if !strings.Contains(out.String(), "echo repo") || !strings.Contains(out.String(), "[y/N]") {
		t.Fatalf("trust did not show the declined hooks and ask:\n%s", out.String())
	}
	if repoLoaded(t, c) {
		t.Fatal("a no in hooks trust approved the hooks")
	}
	c.interactive = false
	if code := hooksCommand([]string{"trust"}, c); code != 1 || repoLoaded(t, c) {
		t.Fatalf("trust without a terminal: exit %d", code)
	}
	c.interactive, c.in = true, strings.NewReader("y\n")
	if code := hooksCommand([]string{"trust"}, c); code != 0 || !repoLoaded(t, c) {
		t.Fatalf("a confirmed yes did not approve the declined hooks (exit %d)", code)
	}
}

// Review m1: `celeste hooks trust` never records a no, so its prompt must
// not claim one is remembered, for hooks or for a workspace MCP server.
func TestHooksTrustPromptDoesNotClaimANoIsRemembered(t *testing.T) {
	c, out, _ := hooksCLIFixture(t, "n\nn\n", true)
	writeFile(t, c.cwd, ".mcp.json", `{"mcpServers":{"repo":{"command":"r","enabled":true}}}`)
	hooksCommand([]string{"trust"}, c)
	s := out.String()
	if !strings.Contains(s, "echo repo") || !strings.Contains(s, `"r"`) {
		t.Fatalf("trust did not ask about the hooks and the MCP server:\n%s", s)
	}
	if strings.Contains(s, "A no is remembered") {
		t.Errorf("hooks trust claims a no is remembered, but it saves nothing:\n%s", s)
	}
	if !strings.Contains(s, "A no leaves the stored decision unchanged.") {
		t.Errorf("hooks trust does not say what a no does:\n%s", s)
	}
	srcs, _, err := hooks.Discover(c.cwd, c.home)
	if err != nil {
		t.Fatal(err)
	}
	for _, src := range srcs {
		if st := hooks.LoadTrust(c.home).Status(src); !src.Global() && st != hooks.Untrusted {
			t.Errorf("%s: status after a no = %s, want untrusted", src.Path, st)
		}
	}
}
