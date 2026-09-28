package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/hooks"
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
