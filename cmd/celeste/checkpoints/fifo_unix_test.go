//go:build unix

package checkpoints

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A file swapped for a FIFO since the change does not hang undo: reading
// its state or contents fails instead of waiting for a writer.
func TestUndoReadsDoNotBlockOnAFIFO(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		dir := t.TempDir()
		f := filepath.Join(dir, "f.txt")
		if err := syscall.Mkfifo(f, 0o644); err != nil {
			t.Skipf("mkfifo unavailable: %v", err)
		}
		real, err := filepath.EvalSymlinks(dir)
		require.NoError(t, err)
		e := Entry{Path: f, Root: real, Rel: "f.txt"}
		if legacy {
			e = Entry{Path: f}
		}
		done := make(chan [2]error, 1)
		go func() {
			ref, err := openRef(e)
			if err != nil {
				done <- [2]error{err, err}
				return
			}
			defer ref.close()
			_, serr := ref.state()
			_, rerr := ref.readFile()
			done <- [2]error{serr, rerr}
		}()
		select {
		case errs := <-done:
			require.Error(t, errs[0], "legacy=%v: state of a FIFO", legacy)
			require.Error(t, errs[1], "legacy=%v: readFile of a FIFO", legacy)
		case <-time.After(5 * time.Second):
			// Unblock the reader so the test binary can exit.
			if w, err := os.OpenFile(f, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
				_ = w.Close()
			}
			t.Fatalf("legacy=%v: undo blocked on a FIFO", legacy)
		}
	}
}
