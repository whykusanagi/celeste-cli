package codegraph

import (
	"context"
	"sort"
	"strings"
)

// metaGraphVersion is the meta key holding the graph version the index was
// built with. graphVersion is bumped whenever stored edges or symbols change
// meaning; Update rebuilds an index stamped with any other value.
//
//	"2": type-checked Go edges, qualified names, interface methods (#375).
const (
	metaGraphVersion = "graph_version"
	graphVersion     = "2"
	// metaGoModules holds goModFingerprint as of the last Go pass.
	metaGoModules = "go_modules"
	// metaBuildInProgress is set before a full build empties the graph and
	// cleared only once every pass has committed. A build interrupted in
	// between (cancelled, killed, power loss) leaves file records whose
	// hashes match while edges are missing, so Update finishes the build
	// when it finds the mark instead of trusting the hashes (#388), without
	// emptying the graph again (#391).
	metaBuildInProgress = "build_in_progress"
	// metaGoPassPending is set before the Go pass writes its first row and
	// cleared once its edges, implementations and module fingerprint are
	// stored. Update reruns the Go pass when it finds the mark.
	metaGoPassPending = "go_pass_pending"
)

// indexGo runs the type-checked Go pass over every Go file in the workspace
// and stores the results. changed lists files whose symbols must be
// re-stored (nil means all, as in a full Build); other files keep their
// symbol rows unless the analysis now gives them different symbols. Every
// Go-sourced edge is rewritten, because a change in one package can move
// edges that start in another.
func (idx *Indexer) indexGo(ctx context.Context, goFiles []string, changed map[string]bool) error {
	res, err := analyzeGo(ctx, idx.workspace, goFiles)
	if err != nil {
		return err
	}
	// From here the pass stores symbols and file records before it rewrites
	// the edges; an interruption in between must not look finished.
	if err := idx.store.SetMeta(metaGoPassPending, []byte(idx.token)); err != nil {
		return err
	}

	resolutions := make(map[string]string, len(res.files))
	parsed := make(map[string]bool, len(res.files))
	for _, fr := range res.files {
		parsed[fr.rel] = true
		resolutions[fr.rel] = fr.resolution
		restore := changed == nil || changed[fr.rel] || !idx.sameSymbols(fr)
		if !restore {
			continue
		}
		if changed != nil {
			_ = idx.store.DeleteFileSymbols(fr.rel)
		}
		idx.storeFileSymbols(fr.rel, "go", fr.symbols)
		idx.storeFileRecord(fr.rel, "go", fr.resolution)
	}
	// A changed file that no longer parses loses its old symbols, as the
	// per-file path always did; its file record is left stale so the next
	// update retries it.
	for rel := range changed {
		if !parsed[rel] {
			_ = idx.store.DeleteFileSymbols(rel)
		}
	}
	if err := idx.store.SetFileResolutions(resolutions); err != nil {
		return err
	}

	quals, err := idx.store.GoQualIDs()
	if err != nil {
		return err
	}
	edges := make([]Edge, 0, len(res.edges))
	for _, e := range res.edges {
		src, ok := idx.goEdgeSource(e, quals)
		if !ok {
			continue
		}
		tgt, ok := idx.goEdgeTarget(e, quals)
		if !ok || tgt == src && e.kind != EdgeCalls {
			continue
		}
		edges = append(edges, Edge{SourceID: src, TargetID: tgt, Kind: e.kind})
	}
	if err := idx.store.ReplaceGoEdges(edges); err != nil {
		return err
	}

	impl := make(map[int64]string, len(res.implements))
	for q, names := range res.implements {
		if id, ok := quals[q]; ok {
			impl[id] = implementsList(names)
		}
	}
	if err := idx.store.SetGoImplements(impl); err != nil {
		return err
	}
	if err := idx.store.SetMeta(metaGoModules, []byte(goModFingerprint(idx.workspace, goFiles))); err != nil {
		return err
	}
	return idx.store.DeleteMetaIf(metaGoPassPending, idx.token)
}

// goModulesChanged reports whether go.mod/go.sum changed since the last Go
// pass.
func (idx *Indexer) goModulesChanged(goFiles []string) bool {
	stored, err := idx.store.GetMeta(metaGoModules)
	if err != nil {
		return true
	}
	return string(stored) != goModFingerprint(idx.workspace, goFiles)
}

func (idx *Indexer) goEdgeSource(e goEdge, quals map[string]int64) (int64, bool) {
	if e.srcQual != "" {
		id, ok := quals[e.srcQual]
		return id, ok
	}
	return idx.store.GetSymbolIDByNameInFile(e.srcName, e.file)
}

// goEdgeTarget resolves an edge target. A qualified target resolves exactly
// or not at all: falling back to a bare name is what produced the wrong
// cross-package edges of #375. Heuristic targets (files that did not
// type-check) use the name lookup every other language uses (resolveTarget:
// callables first, the caller's file first), then the name after the last
// dot.
func (idx *Indexer) goEdgeTarget(e goEdge, quals map[string]int64) (int64, bool) {
	if e.tgtQual != "" {
		id, ok := quals[e.tgtQual]
		return id, ok
	}
	if id, ok := idx.resolveTarget(e.tgtName, e.kind, e.file); ok {
		return id, true
	}
	if i := strings.LastIndex(e.tgtName, "."); i >= 0 {
		return idx.resolveTarget(e.tgtName[i+1:], e.kind, e.file)
	}
	return 0, false
}

// sameSymbols reports whether the stored symbols of an unchanged file match
// what the analysis produced now (names, kinds, qualified names). They can
// differ without the file changing, e.g. when a sibling file's package
// clause or the module path changed.
func (idx *Indexer) sameSymbols(fr goFileResult) bool {
	stored, err := idx.store.GetSymbolsByFile(fr.rel)
	if err != nil || len(stored) == 0 && len(fr.symbols) > 0 {
		return false
	}
	key := func(s Symbol) string {
		return s.Name + "\x00" + string(s.Kind) + "\x00" + s.QualName
	}
	a := make([]string, 0, len(stored))
	for _, s := range stored {
		a = append(a, key(s))
	}
	b := make([]string, 0, len(fr.symbols))
	seen := map[string]bool{}
	for _, s := range fr.symbols {
		k := key(s)
		// UpsertSymbol collapses duplicates (several init funcs); compare
		// the same way.
		if !seen[k] {
			seen[k] = true
			b = append(b, k)
		}
	}
	if len(a) != len(b) {
		return false
	}
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
