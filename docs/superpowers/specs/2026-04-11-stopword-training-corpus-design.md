# Code Search Stop Word Training Corpus — Design Specification

**Date:** 2026-04-11
**Author:** Anthony Tellez (whyKusanagi)
**Status:** Specification
**Related:** `docs/CODEGRAPH_LSH_RESEARCH.md` Section 8.3

---

## 1. Problem

Celeste's semantic code search uses multi-source shingle enrichment to match natural-language queries against code symbols. The current stop word list contains ~80 hand-curated language keywords (func, return, if, def, class, etc.). This is insufficient:

- **Library name pollution:** `JQueryStatic` → ["j", "query", "static"] matches queries containing "query". Framework proper nouns decompose into misleading common-word shingles.
- **Ubiquitous identifier noise:** Tokens like "get", "set", "new", "error", "string" appear in >30% of all symbols, adding no discriminative value but consuming MinHash signature slots.
- **Test vocabulary bleed:** "mock", "fake", "assert", "expect" are present in ~50% of test files, pulling test infrastructure into non-test queries.

A corpus-derived stop word list would eliminate these noise sources while preserving discriminative tokens.

---

## 2. Goals

1. Produce a versioned `stopwords.json` artifact mapping `language → category → token list`
2. Derive stop words statistically from a diverse corpus of open-source code (not hand-curated)
3. Validate that applying the stop words improves precision@10 on the benchmark queries from the LSH research document
4. Keep all training infrastructure in a separate repository from celeste-cli
5. License-safe: only analyze permissively licensed code (Apache-2.0, MIT, BSD); the output is statistical aggregates (term frequencies), not copyrightable content

---

## 3. Training Corpus

### 3.1 Selection Criteria

- **Size:** 1,000+ source files in primary language (produces enough symbols for statistical significance)
- **License:** Apache-2.0 or MIT only (no GPL/AGPL to avoid any downstream concerns)
- **Diversity:** No two repos from the same domain category per language (maximizes vocabulary spread)
- **Activity:** Actively maintained (last commit within 6 months)
- **Quality:** Rich function signatures and documentation comments (better shingle source material)

### 3.2 Corpus — Go (8 repos)

| Repo | License | Domain | Est. Symbols |
|------|---------|--------|-------------|
| `kubernetes/kubernetes` | Apache-2.0 | Container orchestration | 200k+ |
| `moby/moby` | Apache-2.0 | Container runtime | 50k+ |
| `pingcap/tidb` | Apache-2.0 | Distributed SQL database | 100k+ |
| `go-gitea/gitea` | MIT | SCM / developer platform | 60k+ |
| `prometheus/prometheus` | Apache-2.0 | Monitoring / TSDB | 50k+ |
| `rclone/rclone` | MIT | CLI / cloud storage | 40k+ |
| `istio/istio` | Apache-2.0 | Service mesh / networking | 50k+ |
| `seaweedfs/seaweedfs` | Apache-2.0 | Distributed object storage | 30k+ |

**Domain coverage:** Infrastructure, databases, monitoring, networking, storage, CLI, developer tools. Excludes web frameworks (overrepresented in Go ecosystem).

### 3.3 Corpus — Python (8 repos)

| Repo | License | Domain | Est. Symbols |
|------|---------|--------|-------------|
| `huggingface/transformers` | Apache-2.0 | ML / LLM models | 100k+ |
| `apache/airflow` | Apache-2.0 | Workflow orchestration | 80k+ |
| `home-assistant/core` | Apache-2.0 | IoT / home automation | 100k+ |
| `langchain-ai/langchain` | MIT | AI agent framework | 60k+ |
| `vllm-project/vllm` | Apache-2.0 | LLM inference serving | 30k+ |
| `keras-team/keras` | Apache-2.0 | Deep learning framework | 20k+ |
| `commaai/openpilot` | MIT | Robotics / autonomous driving | 30k+ |
| `fastapi/fastapi` | MIT | Web framework / async API | 10k+ |

**Domain coverage:** ML, data pipelines, IoT, AI agents, inference, deep learning, robotics, web APIs. High docstring density across the set.

### 3.4 Corpus — TypeScript / JavaScript (8 repos)

| Repo | License | Domain | Est. Symbols |
|------|---------|--------|-------------|
| `microsoft/TypeScript` | Apache-2.0 | Compiler / language tooling | 100k+ |
| `backstage/backstage` | Apache-2.0 | Developer portal platform | 80k+ |
| `supabase/supabase` | Apache-2.0 | Backend-as-a-service | 60k+ |
| `microsoft/playwright` | Apache-2.0 | Test / browser automation | 30k+ |
| `prisma/prisma` | Apache-2.0 | ORM / database tooling | 30k+ |
| `vercel/next.js` | MIT | Web framework / SSR | 60k+ |
| `webpack/webpack` | MIT | Build tooling / bundler | 20k+ |
| `sveltejs/svelte` | MIT | Web framework / compiler | 15k+ |

**Domain coverage:** Compilers, developer platforms, BaaS, testing, ORMs, web frameworks, build tools.

### 3.5 Corpus — Rust (8 repos)

| Repo | License | Domain | Est. Symbols |
|------|---------|--------|-------------|
| `rust-lang/rust` | Apache-2.0 | Compiler / stdlib | 150k+ |
| `denoland/deno` | MIT | JS runtime / CLI | 30k+ |
| `astral-sh/uv` | Apache-2.0 | Package manager | 20k+ |
| `bevyengine/bevy` | Apache-2.0 | Game engine / ECS | 30k+ |
| `swc-project/swc` | Apache-2.0 | JS/TS compiler | 50k+ |
| `pola-rs/polars` | MIT | DataFrame / analytics | 40k+ |
| `influxdata/influxdb` | Apache-2.0 | Time-series database | 30k+ |
| `astral-sh/ruff` | MIT | Linter / code analysis | 30k+ |

**Domain coverage:** Compilers, runtimes, package management, game engines, data processing, databases, code analysis.

### 3.6 Estimated Total Corpus

| Language | Repos | Est. Symbols | Est. Source Files |
|----------|-------|-------------|------------------|
| Go | 8 | ~580k | ~30k |
| Python | 8 | ~430k | ~25k |
| TypeScript/JS | 8 | ~395k | ~20k |
| Rust | 8 | ~380k | ~15k |
| **Total** | **32** | **~1.8M** | **~90k** |

---

## 4. Pipeline Architecture

### 4.1 Repository Structure

```
celeste-stopwords/                   # Separate repository
├── README.md                        # Purpose, methodology, reproduction instructions
├── LICENSE                          # MIT
├── corpus.json                      # Repo list with pinned commit SHAs
├── pipeline/
│   ├── clone.sh                     # Shallow-clone all repos (--depth 1)
│   ├── index.go                     # Reuses celeste-cli's codegraph package to index each repo
│   ├── analyze.go                   # Computes token document frequency (DF) per language
│   ├── derive.go                    # Applies DF thresholds to produce stop word candidates
│   ├── validate.go                  # Runs benchmark queries with/without stop words, measures precision delta
│   └── export.go                    # Produces final stopwords.json
├── data/                            # gitignored — intermediate analysis data
│   ├── go/                          # Per-repo symbol dumps and token frequency tables
│   ├── python/
│   ├── typescript/
│   └── rust/
├── results/                         # Committed — analysis reports
│   ├── token_frequency_report.md    # Top-N tokens per language with DF and category
│   └── precision_validation.md      # Before/after precision@10 on benchmark queries
└── output/
    └── stopwords.json               # The artifact — committed and versioned
```

### 4.2 Processing Steps

```
Step 1: Clone          Shallow-clone 32 repos (~depth 1)
    │
Step 2: Index          Run celeste codegraph indexer on each repo
    │                  Extract all symbols, compute shingles (but NOT MinHash — just tokens)
    │
Step 3: Aggregate      Per language: compute token document frequency
    │                  DF(token) = (# symbols containing token) / (total symbols)
    │
Step 4: Categorize     Classify high-DF tokens into categories:
    │                  - Framework identifiers (proper nouns that decompose badly)
    │                  - Ubiquitous patterns (get/set/new/init/create)
    │                  - Type system noise (string/int/error/bool)
    │                  - Test infrastructure (mock/fake/assert/expect)
    │                  - Single-character tokens (a/b/c/i/j/k/n/x/y)
    │
Step 5: Threshold      Apply DF threshold per category:
    │                  - Ubiquitous patterns: DF > 5% → stop word
    │                  - Type system noise: DF > 10% → stop word
    │                  - Framework identifiers: curated from DF outliers
    │                  - Test infrastructure: DF > 3% in test files → stop word
    │
Step 6: Validate       Run LSH benchmark queries with and without stop words
    │                  Measure precision@10 delta on grafana, airi, celeste-cli
    │
Step 7: Export         Produce stopwords.json with provenance metadata
```

### 4.3 Output Format

```json
{
  "version": "1.0.0",
  "generated": "2026-04-15",
  "corpus": {
    "repos": 32,
    "total_symbols": 1800000,
    "languages": ["go", "python", "typescript", "javascript", "rust"]
  },
  "method": {
    "df_threshold_ubiquitous": 0.05,
    "df_threshold_type_noise": 0.10,
    "df_threshold_test": 0.03
  },
  "stop_words": {
    "universal": [
      "get", "set", "new", "init", "create", "make", "build",
      "from", "to", "with", "into", "string", "int", "bool",
      "error", "result", "value", "data", "info", "config",
      "test", "mock", "fake", "assert", "expect"
    ],
    "go": ["ctx", "err", "ok", "nil", "fmt", "log"],
    "python": ["self", "cls", "kwargs", "args", "none"],
    "typescript": ["readonly", "undefined", "promise", "async", "await"],
    "javascript": ["prototype", "constructor", "undefined", "null"],
    "rust": ["self", "impl", "pub", "crate", "mut", "ref"]
  },
  "compound_identifiers": [
    "jquery", "github", "mysql", "postgresql", "mongodb",
    "graphql", "webpack", "eslint", "prettier", "typescript",
    "javascript", "dockerfile", "kubernetes", "openapi"
  ]
}
```

**Notes on the format:**
- `universal` tokens are filtered for all languages
- Per-language tokens are additive (filtered in addition to universal)
- `compound_identifiers` are treated as atomic — not split by camelCase/snake_case
- The file includes provenance (`corpus`, `method`) for reproducibility

---

## 5. Validation Methodology

### 5.1 Precision@10 Benchmark

Using the 5 queries from the LSH research document, run semantic search on grafana, airi, and celeste-cli with and without stop words applied. Measure:

- **Precision@10 (human-judged):** Are the top-10 results semantically relevant to the query? Manual assessment per result (relevant / tangentially relevant / irrelevant).
- **False positive rate:** What fraction of top-10 results are irrelevant? (Currently ~20-30% on grafana)
- **jQuery test:** Does "database connection pool query" still return `JQueryStatic` in the top 10? (The stop word list should eliminate this specific false positive)

### 5.2 Regression Check

Verify that stop words don't filter out discriminative tokens by checking that:
- Q2 "http request handler middleware" still returns `mwFromHandler`, `CSRF.Middleware()` (the best-performing query)
- Q5 "error handling retry" still returns `retryResourceInterface` methods
- No true positive from the current results is lost

### 5.3 Acceptance Criteria

- jQuery false positive eliminated from Q3 results
- Overall false positive rate reduced by at least 30%
- No regression in true positive results for Q2 and Q5
- Stop word list has <500 entries (larger lists risk over-filtering)

---

## 6. Integration with celeste-cli

### 6.1 Import Path

The `stopwords.json` file is committed to celeste-cli at `cmd/celeste/codegraph/stopwords.json` and embedded via `//go:embed`.

```go
//go:embed stopwords.json
var stopWordsData []byte

type StopWords struct {
    Universal           []string            `json:"universal"`
    PerLanguage         map[string][]string `json:"stop_words"`
    CompoundIdentifiers []string            `json:"compound_identifiers"`
}
```

### 6.2 Application Points

1. **Shingle generation** (`shingle.go`): Filter tokens through the stop word set before adding to shingle list. Apply language-specific + universal lists.
2. **Query tokenization** (`index.go`): Filter query tokens through the universal stop word set before MinHash computation.
3. **Compound identifier detection** (`shingle.go`): Before camelCase splitting, check if the full identifier (lowercased) matches a compound identifier. If so, use the whole identifier as a single shingle instead of splitting.

### 6.3 Update Cycle

1. New version of `stopwords.json` produced in `celeste-stopwords` repo (quarterly or as needed)
2. PR opened against celeste-cli updating `cmd/celeste/codegraph/stopwords.json`
3. Benchmark run on celeste-cli to validate precision improvement
4. Merge and release

---

## 7. Open Questions

1. **DF threshold tuning:** The initial thresholds (5% for ubiquitous, 10% for type noise, 3% for test) are educated guesses. The actual values should be determined empirically by sweeping thresholds and measuring precision@10.

2. **Cross-language tokens:** Some tokens (like "error", "result", "config") appear across all languages. Should these be in `universal` or duplicated in per-language lists? Universal is simpler but may over-filter for languages where a token is actually discriminative.

3. **Project-specific stop words:** Should celeste-cli support user-defined stop word overrides (e.g., in `.grimoire`)? A project that heavily uses jQuery might want to un-stop "jquery" tokens.

4. **Versioning and backwards compatibility:** When the stop word list changes, existing MinHash signatures become stale (they were computed with the old shingle set). Should an index rebuild be triggered on stop word update?
