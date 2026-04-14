# Celeste CLI Roadmap

*Last updated: April 2026 (v1.9.1)*

## Current Release: v1.9.1

### Shipped in v1.9.0 / v1.9.1

- Everything from v1.8.x (40 tools, 7 LLM providers, code graph, MCP server mode, etc.)
- **Direct codegraph MCP tools** — `celeste_index`, `celeste_code_search`,
  `celeste_code_review`, `celeste_code_graph`, `celeste_code_symbols` exposed as
  first-class MCP tools that bypass the chat LLM and return verbatim results
- **BM25 fused ranking** (additive, via Reciprocal Rank Fusion with k=60) alongside
  the MinHash Jaccard signal — Q1 relevance flipped 2/10 → 8/10 on the grafana benchmark
- **Tree-sitter TypeScript parser** (behind `//go:build cgo`) replacing the regex
  generic parser for `.ts` and `.tsx`, with accurate `call_expression` edge resolution
- **Structural feature rerank** — pure-Go rescoring on matched-token-ratio, edge
  density, kind boost, zero-edge penalty
- **Stopwords runtime integration** — celeste-stopwords v1.0.0 (CC BY 4.0) embedded
  and applied at both index and query time
- **Reasoning metadata** on every search result (`EdgeCount`, `PathFlags`,
  `ConfidenceWarnings`, `MatchedTokens`) so downstream LLMs can audit findings
- **Path-based post-ranking filter** demoting test/mock/generated/vendored/declaration
  results below clean-path matches
- **MinHash seed persistence** — signatures comparable across process boundaries
- **MCP progress notifications** (`notifications/progress`) streaming from
  long-running `celeste_index rebuild/update` operations
- **Anthropic backend `max_tokens` 8192 → 32768** for the chat-mode path
- **TUI streaming tick-complete race fix (v1.9.1)** — short first streaming chunks
  no longer truncate assistant replies to 1 char
- Companion artifact: [celeste-stopwords](https://github.com/whykusanagi/celeste-stopwords) v1.0.0
- Companion app: [celeste-for-claude](https://github.com/whykusanagi/celeste-for-claude)

See [CHANGELOG.md](../CHANGELOG.md) for the full v1.9.0 bundle details and the
`celeste-stopwords/results/` archives for per-task A/B validation.

## v2.1.0 Planned

### Parser coverage (tracked as GitHub issues)
- [ ] [#18 — Tree-sitter parser for Python](https://github.com/whykusanagi/celeste-cli/issues/18)
- [ ] [#19 — Tree-sitter parser for Rust](https://github.com/whykusanagi/celeste-cli/issues/19)
- [ ] Release workflow CGo cross-toolchain (zig-cc or native-runner matrix) so
      pre-built binaries ship with the tree-sitter improvement instead of the stub

### Code Intelligence
- [ ] LSP integration (go to definition, find references, rename)
- [ ] Automatic stale-index detection with rebuild prompt
- [ ] Pluggable embedding-based reranker (local llama.cpp bridge / ONNX) behind the
      existing `Reranker` interface — no cloud dependency

### Agent & Planning
- [ ] Unified plan + todo system (plan steps auto-create todos)
- [ ] Agent mode progress streaming over MCP
- [ ] Interactive permission prompts in TUI during agent mode

### Collections & RAG
- [ ] Multi-provider collections (not just xAI)
- [ ] Auto-index codebase into collections for RAG-enhanced development
- [ ] Collection content preview in TUI

### TUI
- [ ] Proper graph visualization (graphviz integration or canvas rendering)
- [ ] Session resume from TUI

### Platform
- [ ] `celeste models` command (query provider APIs for available models)
- [ ] Plugin system for community tools
- [ ] Web UI mode (browser-based TUI)
