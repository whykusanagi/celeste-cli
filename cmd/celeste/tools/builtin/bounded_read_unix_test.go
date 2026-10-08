//go:build unix

package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Aikido 806869908: a FIFO in the workspace is refused at once by the
// reading tools instead of blocking them.
func TestReadToolsRefuseAFIFO(t *testing.T) {
	ws := realTempDir(t)
	fifo := filepath.Join(ws, "pipe.txt")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("no FIFOs here: %v", err)
	}
	t.Cleanup(func() {
		// Unblock a reader left behind by a failing run.
		if f, err := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			f.Close()
		}
	})
	done := make(chan bool, 1)
	go func() {
		res, err := NewReadFileTool(ws).Execute(context.Background(), map[string]any{"path": "pipe.txt"}, nil)
		done <- err == nil && res.Error
	}()
	select {
	case refused := <-done:
		if !refused {
			t.Error("read_file read a FIFO")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("read_file blocked on a FIFO")
	}
}

// Aikido 806869908: patch_file refuses a file over its cap before reading
// it, and read_file reads only its ceiling of a huge (sparse) file.
func TestReadToolsBoundHugeFiles(t *testing.T) {
	ws := realTempDir(t)
	big := filepath.Join(ws, "big.txt")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxEditBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	data, err := readFileNoFollow(big, maxEditBytes)
	if err == nil || !strings.Contains(err.Error(), "too large") || data != nil {
		t.Fatalf("a file over the cap was read: %d bytes, err %v", len(data), err)
	}
	res := run(t, NewReadFileTool(ws), context.Background(), map[string]any{"path": "big.txt"})
	if res.Error {
		t.Fatalf("read_file: %s", res.Content)
	}
	if res.Metadata["total_bytes"] != maxEditBytes+1 {
		t.Errorf("total_bytes = %v, want the file's size", res.Metadata["total_bytes"])
	}
}
