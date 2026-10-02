//go:build unix

package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Review Focus 3.
func TestRunShellKillsTheProcessGroup(t *testing.T) {
	dir := t.TempDir()
	res := RunShell(context.Background(), ShellOptions{Dir: dir, Command: "sleep 60 & echo $! > pid; wait", Timeout: 500 * time.Millisecond})
	if !res.TimedOut {
		t.Fatalf("result = %+v", res)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "pid"))
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if pid <= 0 {
		t.Fatalf("no pid recorded: %q", b)
	}
	deadline := time.Now().Add(2 * time.Second)
	for syscall.Kill(pid, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if syscall.Kill(pid, 0) == nil {
		t.Fatal("the background child survived the timeout")
	}
}

func TestRunShellWaitDelay(t *testing.T) {
	start := time.Now()
	// The grandchild keeps stdout open after the shell exits.
	RunShell(context.Background(), ShellOptions{Dir: t.TempDir(), Command: "(sleep 30 &) ; echo done", Timeout: 10 * time.Second})
	if time.Since(start) > 5*time.Second {
		t.Fatalf("a grandchild holding the pipe kept the call for %s", time.Since(start))
	}
}

// A background process still holding the output pipe when the shell exits
// is killed with the group once WaitDelay closes the pipe, instead of
// surviving the call unseen.
func TestRunShellKillsWhatHoldsThePipeAfterTheShellExits(t *testing.T) {
	dir := t.TempDir()
	res := RunShell(context.Background(), ShellOptions{Dir: dir, Command: "sleep 60 & echo $! > pid", Timeout: 10 * time.Second})
	if res.Err == nil || res.TimedOut {
		t.Fatalf("result = %+v", res)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "pid"))
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if pid <= 0 {
		t.Fatalf("no pid recorded: %q", b)
	}
	deadline := time.Now().Add(2 * time.Second)
	for syscall.Kill(pid, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if syscall.Kill(pid, 0) == nil {
		t.Fatal("the background child holding the pipe survived the call")
	}
}

// A background process that redirected its output is left running.
func TestRunShellLeavesADetachedBackgroundProcess(t *testing.T) {
	dir := t.TempDir()
	res := RunShell(context.Background(), ShellOptions{Dir: dir, Command: "sleep 60 > /dev/null 2>&1 & echo $! > pid", Timeout: 10 * time.Second})
	if res.Err != nil || res.ExitCode != 0 {
		t.Fatalf("result = %+v", res)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "pid"))
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if pid <= 0 {
		t.Fatalf("no pid recorded: %q", b)
	}
	defer syscall.Kill(pid, syscall.SIGKILL)
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("a detached background process was killed: %v", err)
	}
}
