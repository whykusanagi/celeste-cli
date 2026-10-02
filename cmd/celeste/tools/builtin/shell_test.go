package builtin

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestRunShellBlocksTheDenylistBeforeRunning(t *testing.T) {
	dir := t.TempDir()
	res := RunShell(context.Background(), ShellOptions{Dir: dir, Command: "touch ran; sudo true", Timeout: 5 * time.Second})
	if res.Blocked == "" {
		t.Fatal("sudo must be blocked")
	}
	if _, err := os.Stat(filepath.Join(dir, "ran")); err == nil {
		t.Fatal("nothing may run when the command is blocked")
	}
}

func TestRunShellPassesStdinAndCapsOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	res := RunShell(context.Background(), ShellOptions{Dir: t.TempDir(), Command: "cat", Stdin: []byte(`{"a":1}`), Timeout: 5 * time.Second})
	if res.Output != `{"a":1}` || res.ExitCode != 0 {
		t.Fatalf("result = %+v", res)
	}
	res = RunShell(context.Background(), ShellOptions{Dir: t.TempDir(), Command: "head -c 200000 /dev/zero | tr '\\0' x", Timeout: 5 * time.Second})
	if !res.Truncated || len(res.Output) != maxCommandOutput {
		t.Fatalf("cap: truncated=%v len=%d", res.Truncated, len(res.Output))
	}
}

func TestRunShellReportsAnExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	res := RunShell(context.Background(), ShellOptions{Dir: t.TempDir(), Command: "echo out; echo err >&2; exit 3", Timeout: 5 * time.Second})
	if res.ExitCode != 3 || res.Err != nil || res.TimedOut || res.Output != "out\nerr\n" {
		t.Fatalf("result = %+v", res)
	}
}
