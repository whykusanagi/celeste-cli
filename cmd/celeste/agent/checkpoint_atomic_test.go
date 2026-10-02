package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCheckpointSaveIsAtomicAnd0600(t *testing.T) {
	dir := t.TempDir()
	s := &CheckpointStore{runsDir: dir}
	path := filepath.Join(dir, "r1.json")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(&RunState{RunID: "r1"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) == "old" {
		t.Fatalf("checkpoint not replaced: %q, %v", data, err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("mode = %o, want 600", got)
	}
	if os.Geteuid() == 0 {
		return
	}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	if err := s.Save(&RunState{RunID: "r1", Goal: "new"}); err == nil {
		t.Fatal("save into a read-only directory succeeded")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(data) {
		t.Fatalf("failed save changed the checkpoint: %q", after)
	}
}
