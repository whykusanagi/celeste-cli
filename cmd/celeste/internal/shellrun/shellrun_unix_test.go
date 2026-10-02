//go:build unix

package shellrun

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
func TestRunKillsTheProcessGroup(t *testing.T) {
	dir := t.TempDir()
	res := Run(context.Background(), Options{Dir: dir, Command: "sleep 60 & echo $! > pid; wait", Timeout: 500 * time.Millisecond})
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

func TestRunWaitDelay(t *testing.T) {
	start := time.Now()
	// The grandchild keeps stdout open after the shell exits.
	Run(context.Background(), Options{Dir: t.TempDir(), Command: "(sleep 30 &) ; echo done", Timeout: 10 * time.Second})
	if time.Since(start) > 5*time.Second {
		t.Fatalf("a grandchild holding the pipe kept the call for %s", time.Since(start))
	}
}

// A background process still holding the output pipe when the shell exits
// is killed with the group once WaitDelay closes the pipe, instead of
// surviving the call unseen.
func TestRunKillsWhatHoldsThePipeAfterTheShellExits(t *testing.T) {
	dir := t.TempDir()
	res := Run(context.Background(), Options{Dir: dir, Command: "sleep 60 & echo $! > pid", Timeout: 10 * time.Second})
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

// A non-zero exit does not let a background process holding the pipe
// escape: the shell's exit status is kept and the holder is still stopped.
func TestRunKillsWhatHoldsThePipeAfterAFailingShell(t *testing.T) {
	dir := t.TempDir()
	res := Run(context.Background(), Options{Dir: dir, Command: "sleep 60 & echo $! > pid; exit 3", Timeout: 10 * time.Second})
	if res.Err == nil || res.TimedOut || res.ExitCode != 3 {
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
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatal("the background child holding the pipe survived a failing shell")
	}
}

// A background process that redirected its output is left running.
func TestRunLeavesADetachedBackgroundProcess(t *testing.T) {
	dir := t.TempDir()
	res := Run(context.Background(), Options{Dir: dir, Command: "sleep 60 > /dev/null 2>&1 & echo $! > pid", Timeout: 10 * time.Second})
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

// Args runs a program directly, no shell, with the same held-pipe handling.
func TestRunArgsKillsWhatHoldsThePipeAfterAFailingProgram(t *testing.T) {
	dir := t.TempDir()
	res := Run(context.Background(), Options{Dir: dir, Args: []string{"sh", "-c", "sleep 60 & echo $! > pid; exit 3"}, Timeout: 10 * time.Second})
	if res.Err == nil || res.TimedOut || res.ExitCode != 3 {
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
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatal("the background child holding the pipe survived")
	}
}

func TestRunArgsDoesNotUseAShell(t *testing.T) {
	res := Run(context.Background(), Options{Dir: t.TempDir(), Args: []string{"echo", "$HOME; true"}, Timeout: 5 * time.Second})
	if res.Output != "$HOME; true\n" || res.ExitCode != 0 {
		t.Fatalf("result = %+v", res)
	}
}
