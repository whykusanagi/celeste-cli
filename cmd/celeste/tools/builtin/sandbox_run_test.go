package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/sandbox"
)

// Review Focus 1.
func TestSandboxAllowsWorkspaceTempAndCaches(t *testing.T) {
	kind, ok := sandbox.Available()
	if !ok {
		t.Skip("no OS sandbox here")
	}
	ws := sandbox.Resolve(t.TempDir())
	p := sandbox.Policy{Enabled: true, Workspace: ws, Writable: sandbox.DefaultWritable(os.Getenv("HOME"), ws), Network: true}
	tmp := filepath.Join(os.TempDir(), "celeste-sbx-test-"+filepath.Base(ws))
	t.Cleanup(func() { _ = os.Remove(tmp) })
	res := RunShell(context.Background(), ShellOptions{Dir: ws, Command: "echo hi > in_ws.txt && echo tmp > '" + tmp + "'", Timeout: 10 * time.Second, Policy: &p})
	if res.ExitCode != 0 || res.Sandbox != kind {
		t.Fatalf("result = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(ws, "in_ws.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestBlockedWriteNamesTheKey(t *testing.T) {
	if _, ok := sandbox.Available(); !ok {
		t.Skip("no OS sandbox here")
	}
	ws, outside := sandbox.Resolve(t.TempDir()), t.TempDir()
	p := sandbox.Policy{Enabled: true, Workspace: ws, Writable: []string{ws}, Network: true}
	res := RunShell(context.Background(), ShellOptions{Dir: ws, Command: "echo x > " + filepath.Join(outside, "nope.txt"), Timeout: 10 * time.Second, Policy: &p})
	if res.ExitCode == 0 || !strings.Contains(res.Hint, "sandbox.writable") {
		t.Fatalf("result = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(outside, "nope.txt")); err == nil {
		t.Fatal("the write outside the workspace happened")
	}
}

// The denylist still runs first under the sandbox (second layer).
func TestSandboxedRunStillChecksTheDenylist(t *testing.T) {
	t.Cleanup(sandbox.SetAvailableForTest(sandbox.KindSeatbelt, true))
	p := sandbox.Policy{Enabled: true, Workspace: t.TempDir(), Network: true}
	res := RunShell(context.Background(), ShellOptions{Dir: p.Workspace, Command: "sudo true", Timeout: 5 * time.Second, Policy: &p})
	if res.Blocked == "" {
		t.Fatalf("result = %+v", res)
	}
}

func TestBashErrorCarriesTheSandboxHint(t *testing.T) {
	if _, ok := sandbox.Available(); !ok {
		t.Skip("no OS sandbox here")
	}
	ws, outside := sandbox.Resolve(t.TempDir()), t.TempDir()
	p := sandbox.Policy{Enabled: true, Workspace: ws, Writable: []string{ws}, Network: true}
	bt := NewBashTool(ws, &p)
	r, err := bt.Execute(context.Background(), map[string]any{"command": "echo x > " + filepath.Join(outside, "nope.txt")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(r.Content), &m); err != nil {
		t.Fatal(err)
	}
	e, _ := m["error"].(string)
	if !strings.HasPrefix(e, "exit status ") || !strings.Contains(e, "sandbox.writable") {
		t.Fatalf("error = %q", e)
	}
}

func TestBashWithoutAPolicyRunsUnsandboxed(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("sh")
	}
	ws, outside := t.TempDir(), t.TempDir()
	bt := NewBashTool(ws, nil)
	r, err := bt.Execute(context.Background(), map[string]any{"command": "echo x > " + filepath.Join(outside, "ok.txt")}, nil)
	if err != nil || r.Error {
		t.Fatalf("%v %+v", err, r)
	}
	if _, err := os.Stat(filepath.Join(outside, "ok.txt")); err != nil {
		t.Fatal(err)
	}
}
