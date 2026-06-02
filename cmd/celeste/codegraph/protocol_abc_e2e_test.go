//go:build cgo

package codegraph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProtocolABC_EndToEnd verifies that Protocol/ABC/abstractmethod metadata
// flows from parse → store → FindAllFunctionsWithEdges and that Protocol methods
// are NOT reported as STUB smells.
//
// Method names are deliberately distinct across classes to avoid the
// (name, kind, package, file) uniqueness key in UpsertSymbol collapsing
// same-named methods from different classes into a single DB row.
func TestProtocolABC_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "proto.py", `from typing import Protocol
from abc import ABC, abstractmethod

class Renderable(Protocol):
    def render(self): ...

class BaseProcessor(ABC):
    @abstractmethod
    def process(self): ...
`)

	dbPath := filepath.Join(dir, ".celeste", "codegraph.db")
	os.MkdirAll(filepath.Dir(dbPath), 0755)

	idx, err := NewIndexer(dir, dbPath)
	require.NoError(t, err)
	defer idx.Close()

	require.NoError(t, idx.Build())

	// --- Verify via FindAllFunctionsWithEdges ---
	fns, err := idx.store.FindAllFunctionsWithEdges()
	require.NoError(t, err)

	byName := make(map[string]FunctionEdgeInfo)
	for _, f := range fns {
		byName[f.Name] = f
	}

	// "render" must carry BaseClasses containing "Protocol"
	render, ok := byName["render"]
	assert.True(t, ok, "render should be indexed")
	if ok {
		assert.Contains(t, render.BaseClasses, "Protocol",
			"render.BaseClasses should contain 'Protocol', got %q", render.BaseClasses)
	}

	// "process" must carry Decorators containing "abstractmethod" and BaseClasses "ABC"
	process, ok := byName["process"]
	assert.True(t, ok, "abstract process should be indexed")
	if ok {
		assert.Contains(t, process.Decorators, "abstractmethod",
			"process.Decorators should contain 'abstractmethod', got %q", process.Decorators)
		assert.Contains(t, process.BaseClasses, "ABC",
			"process.BaseClasses should contain 'ABC', got %q", process.BaseClasses)
	}

	// --- Verify via smell detection: Protocol/abstract methods must NOT be stubs ---
	smells, err := idx.FindCodeSmells([]CodeSmellKind{SmellStub}, 100, true)
	require.NoError(t, err)

	stubNames := make(map[string]bool)
	for _, s := range smells {
		stubNames[s.Name] = true
	}

	assert.False(t, stubNames["render"],
		"Protocol method 'render' must not be flagged as STUB")
	assert.False(t, stubNames["process"],
		"abstractmethod 'process' must not be flagged as STUB")
}
