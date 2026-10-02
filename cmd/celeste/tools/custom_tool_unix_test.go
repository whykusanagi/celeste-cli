//go:build unix

package tools

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

func loadCustomTool(t *testing.T, command string) Tool {
	t.Helper()
	dir := t.TempDir()
	def := `{"name":"custom_x","description":"d","parameters":{"type":"object"},"command":` + strconv.Quote(command) + `}`
	if err := os.WriteFile(filepath.Join(dir, "custom_x.json"), []byte(def), 0o600); err != nil {
		t.Fatal(err)
	}
	r := NewRegistry()
	if err := r.LoadCustomTools(dir); err != nil {
		t.Fatal(err)
	}
	tool, ok := r.Get("custom_x")
	if !ok {
		t.Fatal("custom tool not registered")
	}
	return tool
}

func waitGone(t *testing.T, pidfile string) {
	t.Helper()
	b, _ := os.ReadFile(pidfile)
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
		t.Fatal("the custom tool's background child outlived the call")
	}
}

func TestCustomToolGetsItsInputOnStdin(t *testing.T) {
	res, err := loadCustomTool(t, "cat").Execute(context.Background(), map[string]any{"a": 1}, nil)
	if err != nil || res.Error || res.Content != `{"a":1}` {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

// A custom tool's background child holding the output pipe no longer keeps
// the call open: the runner gives it WaitDelay, then kills the group.
func TestCustomToolDoesNotWaitOnAGrandchildHoldingThePipe(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "pid")
	tool := loadCustomTool(t, "sleep 15 & echo $! > '"+pidfile+"'")
	start := time.Now()
	res, err := tool.Execute(context.Background(), map[string]any{}, nil)
	if took := time.Since(start); took > 8*time.Second {
		t.Fatalf("the call waited %s on the grandchild", took)
	}
	if err != nil || !res.Error {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	waitGone(t, pidfile)
}

// Cancelling the call kills the whole process group, not just sh.
func TestCustomToolCancelKillsTheGroup(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "pid")
	tool := loadCustomTool(t, "sleep 15 & echo $! > '"+pidfile+"'; wait")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	res, _ := tool.Execute(ctx, map[string]any{}, nil)
	if took := time.Since(start); took > 8*time.Second {
		t.Fatalf("the call waited %s after cancel", took)
	}
	if !res.Error {
		t.Fatalf("res = %+v", res)
	}
	waitGone(t, pidfile)
}

func TestCustomToolOutputIsCapped(t *testing.T) {
	res, _ := loadCustomTool(t, "head -c 500000 /dev/zero | tr '\\0' x").Execute(context.Background(), map[string]any{}, nil)
	if len(res.Content) > 70_000 {
		t.Fatalf("output not capped: %d bytes", len(res.Content))
	}
}
