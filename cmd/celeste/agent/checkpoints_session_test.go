package agent

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

// Codex review (F4-2): a resumed run files its checkpoints under its own
// run ID, so `celeste revert <file> --session <run-id>` finds what the
// resumed part changed; a new run uses its new run ID.
func TestRunnerCheckpointSessionIsTheRunID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cfg := &config.Config{APIKey: "k", BaseURL: "http://127.0.0.1:1", Model: "fake-model", Timeout: 10}

	opts := DefaultOptions()
	opts.Workspace = t.TempDir()
	opts.ResumeRunID = "20261001-120000.000000000-7"
	r, err := NewRunner(cfg, opts, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if got := filepath.Base(r.env.Snapshots.Dir()); got != opts.ResumeRunID {
		t.Fatalf("resumed run's checkpoint session = %s, want %s", got, opts.ResumeRunID)
	}

	opts.ResumeRunID = ""
	r2, err := NewRunner(cfg, opts, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Close()
	if got := filepath.Base(r2.env.Snapshots.Dir()); got != r2.firstRunID {
		t.Fatalf("new run's checkpoint session = %s, want its run ID %s", got, r2.firstRunID)
	}
}
