package codegraph

import (
	"context"
	"path/filepath"
	"testing"
)

// A pre-cancelled context must abort the ctx-aware codegraph operations promptly
// instead of walking the corpus/repo to completion (task 349f1f14 complement).
func TestCodegraphHonorsCancelledContext(t *testing.T) {
	dir := t.TempDir()
	idx, err := NewIndexer(dir, filepath.Join(dir, "codegraph.db"))
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	defer idx.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	if err := idx.BuildWithContext(ctx); err != context.Canceled {
		t.Errorf("BuildWithContext: expected context.Canceled, got %v", err)
	}
	if err := idx.UpdateWithContext(ctx); err != context.Canceled {
		t.Errorf("UpdateWithContext: expected context.Canceled, got %v", err)
	}
	if _, err := idx.SemanticSearchWithContext(ctx, "anything", SemanticSearchOptions{}); err != context.Canceled {
		t.Errorf("SemanticSearchWithContext: expected context.Canceled, got %v", err)
	}
}

// The non-ctx wrappers still work (background context, no cancellation).
func TestCodegraphWrappersUseBackgroundContext(t *testing.T) {
	dir := t.TempDir()
	idx, err := NewIndexer(dir, filepath.Join(dir, "codegraph.db"))
	if err != nil {
		t.Fatalf("NewIndexer: %v", err)
	}
	defer idx.Close()

	if err := idx.Build(); err != nil {
		t.Errorf("Build wrapper: %v", err)
	}
	if err := idx.Update(); err != nil {
		t.Errorf("Update wrapper: %v", err)
	}
}
