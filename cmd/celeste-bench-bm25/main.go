// celeste-bench-bm25: throwaway benchmark tool for Task 20 BM25 A/B.
// Builds a fresh codegraph over a target directory and runs the 5
// SPEC §5.1 benchmark queries through SemanticSearchWithOptions, writing
// results in the same format used by prior ab_test_TASK*.txt archives.
//
// Usage:
//
//	go run ./cmd/celeste-bench-bm25 -workspace <dir> -out <file>
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/codegraph"
)

type bench struct {
	ID   string
	Text string
}

var queries = []bench{
	{"Q1", "authentication session token validate"},
	{"Q2", "http request handler middleware"},
	{"Q3", "database connection pool query"},
	{"Q4", "file read write parse"},
	{"Q5", "error handling retry"},
}

func main() {
	workspace := flag.String("workspace", "", "Absolute path to workspace to index")
	out := flag.String("out", "", "Path to write benchmark output")
	dbPath := flag.String("db", "", "SQLite db path (default: temp)")
	flag.Parse()

	if *workspace == "" || *out == "" {
		log.Fatal("both -workspace and -out required")
	}

	db := *dbPath
	if db == "" {
		f, err := os.CreateTemp("", "bm25-bench-*.db")
		if err != nil {
			log.Fatalf("tempfile: %v", err)
		}
		f.Close()
		os.Remove(f.Name())
		db = f.Name()
	}

	// Fresh build every invocation so df/idf + seeds are deterministic
	// relative to this run.
	os.Remove(db)
	os.Remove(db + "-wal")
	os.Remove(db + "-shm")

	idx, err := codegraph.NewIndexer(*workspace, db)
	if err != nil {
		log.Fatalf("new indexer: %v", err)
	}
	defer idx.Close()

	if err := idx.Build(); err != nil {
		log.Fatalf("build: %v", err)
	}
	stats, err := idx.Stats()
	if err != nil {
		log.Fatalf("stats: %v", err)
	}

	var buf strings.Builder
	fmt.Fprintf(&buf, "Opening: %s\n", db)
	fmt.Fprintf(&buf, "  %d files, %d symbols, %d edges\n\n", stats.TotalFiles, stats.TotalSymbols, stats.TotalEdges)

	// Run each benchmark query twice: once with rerank disabled
	// (fusion-only baseline) and once with the default structural
	// reranker. Both share the same Indexer + same MinHash seeds so
	// the only variable is the rerank layer. Anything other than
	// rerank behavior reflects the shared state, not test noise.
	runOne := func(label string, reranker codegraph.Reranker, disable bool) {
		fmt.Fprintf(&buf, "## %s\n\n", label)
		for _, q := range queries {
			fmt.Fprintf(&buf, "### %s: %q\n", q.ID, q.Text)
			results, err := idx.SemanticSearchWithOptions(q.Text, codegraph.SemanticSearchOptions{
				TopK:            10,
				ApplyPathFilter: true,
				Reranker:        reranker,
				DisableRerank:   disable,
			})
			if err != nil {
				fmt.Fprintf(&buf, "  ERROR: %v\n\n", err)
				continue
			}
			if len(results) == 0 {
				fmt.Fprintln(&buf, "  (no results)")
			}
			for i, r := range results {
				flagStr := ""
				if len(r.PathFlags) > 0 {
					flagStr = " [" + strings.Join(r.PathFlags, ",") + "]"
				}
				matched := ""
				if len(r.MatchedTokens) > 0 {
					matched = " {" + strings.Join(r.MatchedTokens, ",") + "}"
				}
				fmt.Fprintf(&buf,
					"  %2d.  jac=%.4f  bm25=%.2f  edges=%-3d %-42s %-10s %s:%d%s%s\n",
					i+1, r.Similarity, r.BM25Score, r.EdgeCount,
					r.Symbol.Name, r.Symbol.Kind,
					r.Symbol.File, r.Symbol.Line, flagStr, matched,
				)
				for _, w := range r.ConfidenceWarnings {
					fmt.Fprintf(&buf, "         ⚠ %s\n", w)
				}
			}
			fmt.Fprintln(&buf)
		}
	}

	runOne("FUSION ONLY (rerank disabled)", nil, true)
	runOne("FUSION + STRUCTURAL RERANK", codegraph.NewStructuralReranker(), false)

	if err := os.WriteFile(*out, []byte(buf.String()), 0644); err != nil {
		log.Fatalf("write out: %v", err)
	}
	fmt.Printf("wrote %s (%d bytes)\n", *out, len(buf.String()))
}
