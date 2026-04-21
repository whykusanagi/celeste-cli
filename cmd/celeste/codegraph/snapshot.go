// Graph snapshot and diff — tracks what changed between index builds.
//
// A snapshot captures the set of symbol names + kinds and edge pairs at
// a point in time (keyed by git commit SHA). Diffing two snapshots
// reveals added/removed symbols and edges, enabling blast-radius
// analysis for code review.
package codegraph

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// Snapshot captures the graph state at a point in time.
type Snapshot struct {
	CommitSHA   string            `json:"commit_sha"`
	Timestamp   time.Time         `json:"timestamp"`
	SymbolCount int               `json:"symbol_count"`
	EdgeCount   int               `json:"edge_count"`
	Symbols     map[string]string `json:"symbols"` // name → kind
	Edges       []string          `json:"edges"`   // "source→target:kind"
}

// SnapshotDiff describes what changed between two graph states.
type SnapshotDiff struct {
	BeforeSHA      string      `json:"before_sha"`
	AfterSHA       string      `json:"after_sha"`
	AddedSymbols   []string    `json:"added_symbols"`
	RemovedSymbols []string    `json:"removed_symbols"`
	AddedEdges     []string    `json:"added_edges"`
	RemovedEdges   []string    `json:"removed_edges"`
	Summary        DiffSummary `json:"summary"`
}

// DiffSummary holds aggregate counts for a graph diff.
type DiffSummary struct {
	SymbolsAdded   int `json:"symbols_added"`
	SymbolsRemoved int `json:"symbols_removed"`
	EdgesAdded     int `json:"edges_added"`
	EdgesRemoved   int `json:"edges_removed"`
}

// TakeSnapshot captures the current graph state from the store.
func (idx *Indexer) TakeSnapshot() (*Snapshot, error) {
	commitSHA := detectGitCommit(idx.workspace)

	symbols, err := idx.store.AllSymbolNamesAndKinds()
	if err != nil {
		return nil, fmt.Errorf("snapshot symbols: %w", err)
	}

	edges, err := idx.store.AllEdgeKeys()
	if err != nil {
		return nil, fmt.Errorf("snapshot edges: %w", err)
	}

	return &Snapshot{
		CommitSHA:   commitSHA,
		Timestamp:   time.Now(),
		SymbolCount: len(symbols),
		EdgeCount:   len(edges),
		Symbols:     symbols,
		Edges:       edges,
	}, nil
}

// DiffSnapshots compares two snapshots and returns what changed.
func DiffSnapshots(before, after *Snapshot) *SnapshotDiff {
	beforeSyms := mapKeys(before.Symbols)
	afterSyms := mapKeys(after.Symbols)

	beforeEdgeSet := toSet(before.Edges)
	afterEdgeSet := toSet(after.Edges)

	added := setDiff(afterSyms, beforeSyms)
	removed := setDiff(beforeSyms, afterSyms)
	addedEdges := setDiff(afterEdgeSet, beforeEdgeSet)
	removedEdges := setDiff(beforeEdgeSet, afterEdgeSet)

	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(addedEdges)
	sort.Strings(removedEdges)

	// Cap output at 100 entries each
	if len(added) > 100 {
		added = added[:100]
	}
	if len(removed) > 100 {
		removed = removed[:100]
	}
	if len(addedEdges) > 100 {
		addedEdges = addedEdges[:100]
	}
	if len(removedEdges) > 100 {
		removedEdges = removedEdges[:100]
	}

	return &SnapshotDiff{
		BeforeSHA:      before.CommitSHA,
		AfterSHA:       after.CommitSHA,
		AddedSymbols:   added,
		RemovedSymbols: removed,
		AddedEdges:     addedEdges,
		RemovedEdges:   removedEdges,
		Summary: DiffSummary{
			SymbolsAdded:   len(setDiff(afterSyms, beforeSyms)),
			SymbolsRemoved: len(setDiff(beforeSyms, afterSyms)),
			EdgesAdded:     len(setDiff(afterEdgeSet, beforeEdgeSet)),
			EdgesRemoved:   len(setDiff(beforeEdgeSet, afterEdgeSet)),
		},
	}
}

// SaveSnapshot persists a snapshot to the store.
func (idx *Indexer) SaveSnapshot(snap *Snapshot) error {
	data, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}
	return idx.store.SaveSnapshot(snap.CommitSHA, snap.Timestamp, data)
}

// LoadSnapshot retrieves a snapshot by commit SHA.
func (idx *Indexer) LoadSnapshot(commitSHA string) (*Snapshot, error) {
	data, err := idx.store.LoadSnapshot(commitSHA)
	if err != nil {
		return nil, err
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("unmarshal snapshot: %w", err)
	}
	return &snap, nil
}

// LatestSnapshot returns the most recent snapshot, or nil if none exist.
func (idx *Indexer) LatestSnapshot() (*Snapshot, error) {
	data, err := idx.store.LatestSnapshot()
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("unmarshal snapshot: %w", err)
	}
	return &snap, nil
}

func detectGitCommit(workspace string) string {
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	cmd.Dir = workspace
	out, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func mapKeys(m map[string]string) map[string]bool {
	s := make(map[string]bool, len(m))
	for k := range m {
		s[k] = true
	}
	return s
}

func toSet(items []string) map[string]bool {
	s := make(map[string]bool, len(items))
	for _, item := range items {
		s[item] = true
	}
	return s
}

func setDiff(a, b map[string]bool) []string {
	var result []string
	for k := range a {
		if !b[k] {
			result = append(result, k)
		}
	}
	return result
}
