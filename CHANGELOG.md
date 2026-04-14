# Changelog

All notable changes to Celeste CLI will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.9.1] - 2026-04-14

### Fixed

- **TUI streaming tick-complete race truncated short first chunks.** When grok streamed an
  assistant reply that started with a 1-3 character first delta (e.g. `"O"`), the typing
  animation exhausted its buffer on the very first `TickMsg` (50ms later, `charsPerTick=3`),
  committed that single character to session history, and stopped the ticker. Subsequent SSE
  chunks appended to a zombie buffer with no active renderer — the TUI chat bubble froze at
  the first chunk, session persistence wrote the 1-char string to disk, and the next LLM
  request was sent with `content_len=1` for that assistant message, losing the rest of the
  response as conversation context.

  Root cause was in `cmd/celeste/tui/app.go` — the `TickMsg` handler used
  `typingPos == len(typingContent)` as the sole termination condition without checking whether
  the network stream had actually finished. Fix adds an `AppModel.streamDone bool` that tracks
  `EventMessageDone` receipt; the tick-complete branch now has three cases:

  | `typingPos` vs `len` | `streamDone` | Action |
  |---|---|---|
  | `pos < len` | any | advance + reschedule (unchanged) |
  | `pos == len` | `false` | **idle**: reschedule a no-op tick so any late chunk gets picked up |
  | `pos == len` | `true` | commit to session + stop |

  `StreamChunkMsg(IsFirst=true)` resets `streamDone=false`. `StreamDoneMsg` sets it to true.
  `AgentProgressResponse` (non-streaming agent reply path that reuses the typing animation)
  primes `streamDone=true` up front so its animation commits on catch-up instead of idling
  forever waiting for a done event the agent path never sends.

  Validation: four new regression tests in `cmd/celeste/tui/streaming_race_test.go` reproduce
  the exact scenario and pin the new invariants. Confirmed in a live TUI session against
  grok-4-1-fast with a 630-character response rendered correctly on the first try — no
  re-prompt needed, no trailing anomalies in `~/.celeste/logs/celeste_2026-04-13.log`.

### Details

- Changed files: `cmd/celeste/tui/app.go`, `cmd/celeste/tui/streaming_race_test.go` (new),
  `cmd/celeste/main.go` (version constant), `cmd/celeste/server/server.go` (version constant),
  matching test assertions.
- Scope: this is the **only** behavioral change in v1.9.1. Everything else from v1.9.0 is
  untouched.

## [1.9.0] - 2026-04-13

Major search-quality bundle and MCP architecture rework. Eleven commits, ten closed feature
tasks, empirical A/B validation archives under
[celeste-stopwords/results/](https://github.com/whykusanagi/celeste-stopwords/tree/main/results).

SPEC §5.3 acceptance criteria all met on the grafana benchmark:
`JQueryStatic` absent from Q3 top 10, aggregate relevance 22/50 → 35/50 (+59% absolute),
no TP regressions on Q2/Q5, stopword list < 500 entries, corpus Apache-2.0/MIT.

### Added

- **Direct codegraph MCP tools.** Five new first-class MCP tools that bypass the chat LLM
  entirely so Claude Code / Cursor / any MCP client can call the codegraph directly:
  - `celeste_index` (operations: `status`, `update`, `rebuild`) — indexing is explicit; the
    query tools never auto-reindex. Progress notifications (`notifications/progress`) stream
    back through the stdio transport when the client provides a `progressToken`.
  - `celeste_code_search` — semantic search with MinHash+BM25 fusion + structural rerank
  - `celeste_code_review` — structural code review findings as verbatim JSON
  - `celeste_code_graph` — symbol callers/callees/references
  - `celeste_code_symbols` — list all symbols in a file or package

  Per-workspace `*codegraph.Indexer` cached on the server, lazy-opened, released on shutdown.
  Results returned verbatim as MCP `ContentBlock`s with no chat-LLM summarization and no
  `max_tokens` ceiling.

- **BM25 fused ranking.** Per-symbol term frequency and per-corpus IDF persisted in two new
  SQLite tables (`symbol_tokens`, `token_stats`). Query time merges Jaccard and BM25 via
  Reciprocal Rank Fusion (k=60). `SearchResult` gains `BM25Score` + `MatchedTokens`. Q1
  ("authentication session token validate") flipped from 2/10 relevant to 8/10 relevant on
  grafana after this landed — IDF-weighted tiebreaking on session/token tokens lifted the
  real auth API functions above the noise.

- **Tree-sitter TypeScript parser** (behind `//go:build cgo`). Replaces the regex-based
  `GenericParser` for `.ts` and `.tsx` files when celeste is built with CGo enabled. Accurate
  `call_expression` edge resolution instead of the old `\bname(` body-scan heuristic. On
  content-control (21 TS files) the edge count drops 445 → 211 (real, not overcounted) and
  zero-edge top-10 warnings drop 3 → 0. Python and Rust stay on the regex parser for v1.9.0;
  tracked for v2.1.0 as #18 and #19.

  `parser_ts_stub.go` provides a `//go:build !cgo` fallback that delegates to `GenericParser`
  so pure-Go cross-compile release binaries still work — they just lose the tree-sitter
  improvement. Users building from source get the full experience automatically.

- **Structural feature rerank layer.** `StructuralReranker` rescores candidates using features
  the RRF fusion can't see: matched-token-ratio, log-normalized edge density, function/method
  kind boost, zero-edge penalty. Pure Go, zero dependencies. Exposed via a `Reranker` interface
  on `SemanticSearchOptions` so a future embedding-based reranker (local llama.cpp bridge,
  grok/xAI embeddings, ONNX) can drop in without touching the pipeline.

- **Stopwords runtime integration.** Embedded `celeste-stopwords` v1.0.0 (CC BY 4.0) applied
  at both index time and query time. Filters universal + per-language noise tokens from
  shingle sets before MinHash so common tokens like `get`/`set`/`error`/`string` don't consume
  signature slots. Includes a downstream patch removing `query` from the TypeScript set to
  avoid asymmetric filtering breaking Q3 on TS codebases, plus `TestStopWords_PreserveTokensNotStopped`
  regression guard.

- **Reasoning metadata on `SearchResult`.** `EdgeCount`, `PathFlags`, `ConfidenceWarnings`,
  `MatchedTokens` — downstream LLMs can audit every search result instead of trusting a
  single similarity number. Confidence warnings surface zero-edge interfaces, low-confidence
  scores, declaration-only types, and path-demotion reasons.

- **Path-based post-ranking filter.** Tier-partitions `test` / `mock` / `generated` /
  `vendored` / `declaration` results below clean-path results with explicit `[mock]` etc.
  flags. Q2 ("http request handler middleware") previously had 100% mock handlers dominating
  the top 10 on grafana; v1.9.0 tiers them below production handlers. If the query itself
  asks for test/mock code (`queryWantsTests`), the filter backs off to respect user intent.

- **MinHash seed persistence.** Replaced `hash/maphash` (opaque, unserializable) with seeded
  FNV-1a (serializable uint64 seeds). The 128 seed values are stored in a new `meta` SQLite
  table at first `Build()`, so a subsequent process loading the same index restores the same
  hash family and signatures stay comparable across process invocations. Without this, the
  `celeste serve` MCP bridge silently returned noise because every new server process rolled
  fresh random seeds that had no relationship to the signatures already stored in the database.

### Changed

- **Anthropic backend default `max_tokens` raised from 8192 → 32768.** The old 8192 ceiling
  truncated the chat-mode MCP path mid-response when a sub-tool returned a multi-KB JSON blob
  and the chat LLM was asked to echo it. Claude opus/sonnet 4.x support up to 64K output
  tokens; 32K is a 4× budget with no downside (only used tokens are billed).

- **`ShinglesForSymbol` signature.** Now takes `lang string` so the embedded stopwords
  filter can apply the per-language set alongside the universal set. Callers that don't know
  the file language should pass `""` to get universal-only filtering.

- **`SearchResult` struct.** Extended with `BM25Score`, `MatchedTokens`, `EdgeCount`,
  `PathFlags`, `ConfidenceWarnings`. `Similarity` (Jaccard) is unchanged — existing callers
  that only read `Symbol` + `Similarity` keep working.

- **MinHash signatures computed by celeste-cli < 1.9.0 are semantically stale.** Existing
  indexes still work for symbol lookups, edges, and keyword search, but semantic search
  accuracy improves if you rebuild:

  ```bash
  # Via the CLI
  celeste index --rebuild

  # Via the MCP bridge
  # call celeste_index { operation: "rebuild", workspace: "/path/to/project" }
  ```

### Fixed

- **`splitCamelCase` end-of-acronym rule now requires 3+ consecutive uppercase letters.** Two
  uppercase letters followed by a lowercase word is a PascalCase boundary, not an acronym edge.
  Previously, `JQueryStatic` decomposed into `["J", "Query", "Static"]`, which caused the
  `jQueryStatic` pollution problem documented in
  [celeste-stopwords Issue #1](https://github.com/whykusanagi/celeste-stopwords/blob/main/docs/KNOWN_QUALITY_ISSUES.md):
  searches for "query" would match `JQueryStatic` via the stray `query` token, and ~1,650 other
  identifiers exhibited the same bug across a 31-repo training corpus. `HTTPServer`,
  `HTMLToMarkdown`, `CSVParser`, and every other 3+ consecutive-uppercase acronym still splits
  correctly.

  Affected identifier families now atomize correctly:
  - `JQuery` / `JQueryStatic` / `JQueryElement` / `JQueryPromise`
  - `IFoo` / `IArguments` / `IPromise` / `ICache` / any `I`-prefixed TypeScript interface
  - `IPv4` / `IPv6` / `VNode` / `ETag` / `OAuth2` / `XDist`

- **Two direct-tool MCP schema mismatches.** `celeste_code_symbols` advertised a `name` field
  that the underlying builtin tool doesn't accept; `celeste_code_graph` advertised an `action`
  discriminator that doesn't exist. Both schemas now mirror the real builtin tool parameters
  1:1 — name-based lookups go through `celeste_code_search` instead.

### CI / Release

- **Go toolchain bumped 1.26.1 → 1.26.2** in `.github/workflows/ci.yml` and `release.yml`.
  1.26.1 stdlib has 5 CVEs (`crypto/x509` × 3, `crypto/tls`, `html/template`) fixed in 1.26.2;
  `govulncheck` now returns "No vulnerabilities found".
- **Release workflow pins `CGO_ENABLED=0`** for cross-platform binary builds. Released
  artifacts ship with the pure-Go `parser_ts_stub.go` fallback for portability; users who want
  the tree-sitter improvement can `go install` from source with CGo enabled. A proper CGo
  cross-toolchain release workflow (zig cc or native-runner matrix) is queued for v2.1.0.
- **Dockerfile builder adds `build-base`** and sets `CGO_ENABLED=1` on the test-compile step
  so the codegraph test binary can link the tree-sitter C runtime. Runtime container still
  uses `CGO_ENABLED=0`; only test compilation needs the C toolchain.

### Details

- 14 commits on `feat/v1.9-quality-bundle` (PR #16) plus a schema fix direct to main plus
  PR #17 hardening `release.yml` for the tag build.
- Empirical validation archives under `celeste-stopwords/results/`:
  - `ab_test_TASK19_v1.9.0_grafana_app.txt` (path filter + reasoning metadata)
  - `ab_test_TASK20_v1.9.0_grafana_app.txt` (BM25 fused ranking)
  - `ab_test_TASK21_v1.9.0_grafana_app.txt` (stopwords runtime)
  - `ab_test_TASK24_rerank_content_control.txt` (structural rerank, shared-index A/B)
  - `ship_decision_v1.9.0.md` (full GO decision document)
- Known v2.1.0 follow-ups: #18 (tree-sitter Python), #19 (tree-sitter Rust), release workflow
  CGo cross-toolchain for pre-built TS parser.

## [1.8.0] - 2026-04-03

### Added
- **`.grimoire` Project Context**: Persona-themed project config files with auto-discovery, `@include` support, and `celeste init` auto-detection
- **Code Graph Index**: Structural code graph with Go AST parsing, regex extraction for other languages, SQLite storage via `modernc.org/sqlite`
- **Semantic Code Search**: MinHash over enriched shingles — concept-based search without embeddings or API calls
- **MCP Server Mode**: `celeste serve` exposes Celeste via stdio and authenticated SSE transports for Claude Code, Codex, or any MCP client
- **Git-Aware Context**: Startup snapshot injected into system prompt, plus `git_status` and `git_log` tools
- **Session Persistence**: JSONL append-only session logs with `celeste resume` and auto-resume
- **File Checkpointing**: Snapshots before writes, stale file detection, `/diff` session summary, `/undo` revert
- **Extended Thinking**: Provider-specific reasoning tokens (Claude, Gemini, xAI) with `/effort` command
- **Prompt Caching**: Static prefix / dynamic suffix structure for cache-friendly system prompts
- **Image Input**: Multimodal support — `read_file` detects images, base64-encodes for vision models
- **Web Search & Fetch**: DuckDuckGo search and URL-to-markdown tools (no API key required)
- **Cost Tracking**: Per-model pricing table, session cost accumulation, display in context bar
- **Hooks System**: Pre/post tool execution hooks defined in `.grimoire` with template variables
- **Plan Mode**: `/plan` enters read-only mode, writes plan file for user review, `/plan execute` runs it
- **Task Tracking**: `todo` tool for model self-management with TUI panel
- **Memory System**: Persistent learned knowledge at `~/.celeste/projects/`, heuristic extraction, staleness detection
- **Subagent Spawning**: `/spawn` and `spawn_agent` tool for foreground task delegation
- **Graceful Ctrl+C**: Single interrupt cancels current task, double exits. AbortSignal propagation through tools.
- **Build-time Version Injection**: Version, build tag, and commit SHA injected via ldflags in CI/CD

### Changed
- Version information now uses `var` instead of `const` for ldflags injection
- Release workflow injects commit SHA into binaries
- CI build artifacts include version + commit metadata

## [1.7.0] - 2026-03-31

### Added
- **Unified Tool Layer**: Single `Tool` interface replacing the old `skills/` package, with `tools/builtin/` for all implementations
- **Streaming Tool Executor**: Tools begin executing as LLM generates, with concurrent dispatch for read-only tools
- **Context Window Management**: Automatic token budget tracking, reactive/proactive compaction, tool result capping
- **Permission System**: Multi-layer allow/deny/ask rules with pattern matching, denial tracking, persistent config
- **MCP Client**: Model Context Protocol support with stdio/SSE transports for external tool servers
- **TUI Enhancements**: Tool progress indicators, context budget bar, permission prompts, MCP server panel

### Changed
- Replaced `cmd/celeste/skills/` package with `cmd/celeste/tools/` unified tool system
- All 23 built-in skills migrated to `tools/builtin/` with individual files
- All 6 dev tools migrated from `agent/dev_skills.go` to `tools/builtin/`
- LLM backends now support `SendMessageStreamEvents()` for granular streaming
- Agent runtime uses streaming events instead of sync batch execution
- Config token tracking delegates to new `context/` package

### Removed
- `cmd/celeste/skills/` package (replaced by `cmd/celeste/tools/`)
- `cmd/celeste/agent/dev_skills.go` (replaced by `tools/builtin/`)
- `cmd/celeste/llm/summarize.go` (replaced by `context/summarizer.go`)
- `cmd/celeste/config/context.go` token tracking (replaced by `context/budget.go`)

## [Unreleased - Pre-1.7]

### Added
- New autonomous `agent` command family for multi-turn task execution:
  - `celeste agent --goal ...`
  - `celeste agent --resume <run-id>`
  - `celeste agent --list-runs`
  - `celeste agent --eval <cases.json>`
- Agent runtime loop package (`cmd/celeste/agent`) with:
  - max-turn and max-tool safety controls
  - completion-marker controls (default `TASK_COMPLETE:`)
  - no-progress stop behavior
- Checkpointed long-horizon run persistence under `~/.celeste/agent/runs`.
- Agent-only development skills for coding/content workflows:
  - `dev_list_files`, `dev_read_file`, `dev_write_file`, `dev_search_files`, `dev_run_command`
- Eval harness for JSON-defined scenarios with pass/fail scoring.
- Phase 2 agent controls:
  - explicit planning phase with extracted plan steps
  - execution progress markers via `STEP_DONE: <n>`
  - verification gate via repeatable `--verify-cmd` commands and `--require-verify`
- Phase 3 agent deliverables:
  - per-run artifact bundles (`summary`, `run_state`, `plan`, `steps`, `verification`, optional git status/diff)
  - benchmark suite scaffolding via `celeste agent --benchmark <suite.json>`
  - optional benchmark JSON report export via `--benchmark-out`

### Testing
- Added new unit coverage for:
  - checkpoint save/load/list behavior
  - workspace path traversal guards
  - development skill execution paths
  - eval file parsing and result scoring

## [1.5.5] - 2026-02-27

### Fixed
- Restored tool definition delivery in chat mode so skill/function metadata is sent from the live LLM client instead of the TUI skills panel stub.
- Added multi-tool execution flow support: all tool calls returned in a single assistant turn are now executed and replied with matching `tool_call_id` results before continuation.
- Hardened tool argument parsing so malformed JSON arguments surface as explicit tool errors rather than silently falling back to empty argument maps.

### Changed
- Added schema validation for disk-loaded custom skills to reject malformed function definitions before they reach provider APIs.
- Hardened OpenAI/xAI tool serialization by skipping invalid tool payloads gracefully instead of sending malformed definitions.
- Improved Google/Vertex schema conversion compatibility for `required` fields across both `[]string` and `[]interface{}` input forms.

### Testing
- Added regression coverage for:
  - custom skill schema validation pass/fail paths
  - OpenAI and xAI tool serialization skip-on-error behavior
  - Google/Vertex `required` schema conversion compatibility
  - multi-tool TUI execution sequencing and single follow-up request semantics

## [1.5.4] - 2026-02-25

### Security
- Upgraded `go-ethereum` v1.16.8 → v1.17.0 to remediate GO-2026-4508 (DoS via malicious p2p message)

## [1.5.3] - 2026-02-25

### Added
- `upscale_image` tool call skill — upscale and enhance images via Venice.ai, now available as an LLM-callable tool in all modes (not limited to NSFW)
- `docs/plans/2026-02-24-clear-nsfw-upscale-design.md` — design doc for this release's changes
- `docs/plans/2026-02-24-clear-nsfw-upscale.md` — implementation plan used to build this release

### Changed
- `/clear` now performs a full session reset — clears chat history **and** starts a fresh session (equivalent to `/clear` + `/session new` in one command)
- Removed `upscale:` as an NSFW-mode media command; upscaling is now handled exclusively by the `upscale_image` tool call

## [1.5.2] - 2026-02-22

### Fixed
- `/tools` command from menu now correctly opens the skills browser instead of returning "Unknown command"
- `/nsfw` now toggles — typing it a second time disables NSFW mode (previously required `/safe` to exit)
- Context rot: UI notification messages (`role=system`) were being sent to the LLM on every request, bloating context with phantom system messages per session; LLM requests now only include user/assistant/tool messages
- Menu item selection now correctly executes the selected command (value-type `InputModel` mutation was being discarded)
- Skill browser selection now correctly populates the input field

### Changed
- Renamed project references from `CelesteCLI` to `Celeste CLI` across all documentation, scripts, and workflows

## [1.5.1] - 2026-02-19

### Added
- **Collections Support (xAI RAG)** - Upload custom documents for semantic search during chat
  - Create and manage collections via CLI and TUI
  - Upload documents (.md, .txt, .pdf, .html) up to 10MB each
  - Enable/disable collections for chat with active set management
  - Automatic semantic search integration with Grok models
  - 7 CLI commands: `create`, `list`, `upload`, `delete`, `enable`, `disable`, `show`
  - Interactive TUI: `/collections` command with navigation and toggle support
  - Management API client for xAI Collections API
  - High-level Collections Manager with config integration
  - Server-side RAG via xAI's built-in `collections_search` tool
  - Support for recursive directory uploads
  - Persistent configuration in `~/.celeste/config.json`
  - Document validation (format and size checking)

### Changed
- Extended `config.json` structure with Collections configuration:
  - Added `xai_management_api_key` field for Collections API authentication
  - Added `collections` object with enabled status and active collections list
  - Added `xai_features` object for xAI-specific feature flags
- Updated LLM backend to inject xAI built-in tools when collections enabled
- Enhanced TUI with collections view mode and `/collections` command
- Main application wired for Collections config propagation to LLM client

### Documentation
- Added `docs/COLLECTIONS.md` - Complete Collections user guide with:
  - Quick start tutorial for Collections setup
  - Full CLI commands reference with examples
  - TUI interface usage and keybindings
  - Best practices for organizing collections and documents
  - Troubleshooting guide for common issues
  - Advanced usage patterns (batch operations, git hooks, context switching)
  - API integration details and limitations
  - FAQ section covering common questions
- Updated `README.md` - Added Collections Support section and feature bullet
- Updated `docs/LLM_PROVIDERS.md` - Added Collections column to compatibility matrix showing xAI as only supported provider

## [1.4.0] - 2025-12-18

### Added
- **Wallet Security Monitoring** - Comprehensive wallet threat detection and alerting
  - Monitor multiple wallet addresses across networks (Ethereum, Polygon, Arbitrum, Optimism, Base)
  - Real-time polling every 5 minutes (configurable)
  - 6 wallet management operations: add, remove, list, check security, get alerts, acknowledge alerts
  - 4 threat detection types:
    - **Dust attacks** - Detect tiny value transfers (< 0.001 ETH) used for address poisoning
    - **NFT scams** - Flag unsolicited NFT transfers from unknown contracts
    - **Large transfers** - Alert on significant outgoing funds (> 1 ETH or > 10% of balance)
    - **Dangerous approvals** - Detect unlimited token approvals (2^256-1) and high-value approvals
  - Alert system with severity-based classification (critical, high, medium, low)
  - Persistent alert history stored in `~/.celeste/wallet_alerts.json`
  - Alert acknowledgment system to track reviewed threats
  - CLI commands for wallet management via `celeste skill wallet_security`
- **Background Monitoring Daemon** - Automatic wallet security monitoring
  - Run wallet checks in background at configurable intervals
  - Commands: `celeste wallet-monitor start/stop/status`
  - Fork to background process with PID file management
  - Graceful shutdown with SIGTERM handling
  - Configurable poll interval via `wallet_security_poll_interval`
  - Automatic logging of security events with timestamps
- **Token Approval Monitoring** - ERC20 approval event tracking
  - Monitor `Approval(address,address,uint256)` events via `eth_getLogs`
  - Detect unlimited approvals (max uint256 = 2^256-1)
  - Flag high-value approvals (> 1 million tokens)
  - Alert severity: HIGH for unlimited, MEDIUM for high-value
  - Track spender contracts and approved amounts
- **IPFS File Upload** - Binary file support for IPFS
  - Upload files via `--file_path` parameter
  - Support for all file types: images, PDFs, archives, audio/video, binaries
  - Automatic file size and name detection
  - Returns filename, size, type, and CID in response
  - Preserves original string content upload functionality
- **Wallet Security Storage**
  - `~/.celeste/wallet_security.json` - Monitored wallets configuration
  - `~/.celeste/wallet_alerts.json` - Security alerts history log
  - `~/.celeste/wallet_monitor.pid` - Daemon process ID
  - Automatic directory creation and file management
- **Enhanced Configuration**
  - Added `WalletSecuritySettingsConfig` with poll interval and alert level settings
  - Config fields: `wallet_security_enabled`, `wallet_security_poll_interval`, `wallet_security_alert_level`

### Changed
- Extended Alchemy integration for wallet security monitoring using `alchemy_getAssetTransfers` and `eth_getLogs` APIs
- Enhanced alert display system with severity-based styling (leveraging existing TUI components)
- Updated ConfigLoader interface with `GetWalletSecurityConfig()` method
- IPFS skill description updated to reflect file upload support

### Documentation
- Added `docs/WALLET_SECURITY.md` - Complete wallet security monitoring guide with:
  - Setup instructions for wallet monitoring
  - Threat detection patterns and explanations
  - Background daemon usage and configuration
  - Token approval monitoring details
  - Usage examples for all operations
- Updated `docs/IPFS_SETUP.md` - Added file upload documentation with examples for binary files

## [1.3.0] - 2025-12-18

### Added
- **IPFS Integration** - Decentralized content management
  - Upload and download content via IPFS (returns CID)
  - Pin management (pin, unpin, list pins)
  - Multi-provider support (Infura, Pinata, custom nodes)
  - Gateway URL generation for public access
  - Official go-ipfs-http-client library integration
- **Alchemy Blockchain API** - Comprehensive blockchain data access
  - Wallet operations: ETH/token balances, transaction history, asset transfers
  - Token data: Real-time metadata and comprehensive token information
  - NFT APIs: Query NFTs by owner, metadata, collection info
  - Transaction monitoring: Gas prices, transaction receipts, block information
  - Multi-network support: Ethereum, Arbitrum, Optimism, Polygon, Base (mainnet + testnets)
  - JSON-RPC interface with proper error handling
- **Blockchain Monitoring** - Real-time blockchain event tracking
  - Watch addresses for new transactions across multiple blocks
  - Get latest block information with transaction details
  - Query specific blocks by number (hex or decimal)
  - Asset transfer tracking (external, internal, ERC20, ERC721, ERC1155)
  - Network-specific monitoring with configurable poll intervals
- **Modern Crypto Utilities**
  - Ethereum address validation using go-ethereum (EIP-55 checksumming)
  - Wei ↔ Ether ↔ Gwei conversion helpers with big.Int precision
  - Production-ready rate limiting using golang.org/x/time/rate
  - Multi-network URL construction and validation
  - Chain ID support for all major networks
- **Enhanced Configuration System**
  - Network-specific settings for L2 support
  - Environment variable overrides for CI/CD (`CELESTE_IPFS_API_KEY`, `CELESTE_ALCHEMY_API_KEY`)
  - Flexible provider configuration (Infura, Pinata, custom endpoints)
  - Crypto-specific config fields in config.json and skills.json
  - ConfigLoader interface with GetIPFSConfig(), GetAlchemyConfig(), GetBlockmonConfig()

### Changed
- Upgraded to modern production-grade Go crypto libraries:
  - `github.com/ethereum/go-ethereum@v1.16.7` - Official Ethereum Go implementation
  - `github.com/ipfs/go-ipfs-http-client@v0.7.0` - Official IPFS HTTP client
  - `github.com/ipfs/go-cid@v0.6.0` - Content Identifier handling
  - `golang.org/x/time@v0.14.0` - Token bucket rate limiting
- Improved error handling for external API integrations
- Enhanced skills.json structure for crypto service configuration
- Better address normalization with proper checksum validation

### Documentation
- Added `docs/IPFS_SETUP.md` - Infura IPFS configuration guide
- Added `docs/ALCHEMY_SETUP.md` - Alchemy API setup and usage
- Added `docs/BLOCKCHAIN_MONITORING.md` - Real-time monitoring guide

## [1.1.0] - 2025-12-14

### Added
- **One-shot CLI commands** for all features (context, stats, export, session, config, skills)
  - Execute any command without entering TUI: `./celeste context`, `./celeste stats`
  - Direct skill execution: `./celeste skill <name> [--args]`
  - Comprehensive skill testing with `./celeste skill generate_uuid`, etc.
- **Context Management System**
  - Token usage tracking with input/output breakdown
  - Retroactive token calculation for session history
  - Context window monitoring and warnings
  - Auto-summarization when approaching limits
- **Enhanced Session Persistence**
  - Message persistence across sessions
  - Session metadata tracking (token counts, model info)
  - Improved session loading and restoration
- Interactive model selector with arrow key navigation
- Flickering corruption animation for stats dashboard
- GitHub Actions CI/CD pipeline
- Comprehensive test coverage
- Security vulnerability scanning
- Cross-platform build support

### Fixed
- **Token counting** - Now correctly displays input/output token breakdown
- **All 18 skills** - 100% functional from CLI one-shot commands:
  - Type conversion for numeric arguments (length, value, amount)
  - Parameter name corrections (encoded, text, from_timezone, etc.)
  - Weather skill accepts both string and numeric zip codes
- Session persistence and provider detection issues
- Code formatting issues
- Dependency version compatibility

### Changed
- Improved documentation structure
- Enhanced error handling
- Model selector with arrow key navigation
- Stats dashboard with corruption animation effects

### Documentation
- Added `ONESHOT_COMMANDS.md` - Complete CLI command reference
- Added `docs/TEST_RESULTS.md` - Test verification results for all skills
- Added corruption aesthetic validation guides
- Added brand system documentation (migrated to corrupted-theme package)

## [1.0.2] - 2025-12-03

### Added
- **Bubble Tea TUI**: Complete rewrite with flicker-free terminal UI
  - Scrollable chat viewport with PgUp/PgDown navigation
  - Input history with arrow key navigation
  - Real-time skills panel showing execution status
  - Corrupted theme styling (pink/purple aesthetic)
- **Named Configurations**: Multi-profile config support
  - `celeste -config openai chat` for OpenAI
  - `celeste -config grok chat` for xAI/Grok
  - Template system for quick config creation
- **Skills System**: OpenAI function calling support
  - Tarot reading (3-card and Celtic Cross)
  - NSFW mode (Venice.ai integration)
  - Content generation (Twitter, TikTok, YouTube, Discord)
  - Image generation (Venice.ai)
  - Weather lookup
  - Unit/timezone/currency converters
  - Hash/Base64/UUID/Password generators
  - QR code generation
  - Twitch live status checking
  - YouTube video lookup
  - Reminders and notes
- **Session Management**: Conversation persistence
  - Auto-save and resume sessions
  - Session listing and loading
  - Message history with timestamps
- **Simulated Typing**: Smooth text rendering
  - Configurable typing speed
  - Corruption effects during typing
  - Better UX for streamed responses

### Changed
- **Architecture**: Modular package structure
  - `cmd/Celeste/tui/` - Bubble Tea components
  - `cmd/Celeste/llm/` - LLM client
  - `cmd/Celeste/config/` - Configuration management
  - `cmd/Celeste/skills/` - Skills registry and execution
  - `cmd/Celeste/prompts/` - System prompts
- **Configuration**: JSON-based config system
  - Migrated from `.celesteAI` to `~/.celeste/config.json`
  - Separate `secrets.json` for sensitive data
  - Environment variable override support
- **Binary Name**: Renamed from `celestecli` to `Celeste`

### Removed
- Legacy main_old.go (3,481 lines)
- Old configuration format
- Deprecated Python utilities

### Fixed
- API key exposure in error messages
- Config file permission issues
- Session not saving in some scenarios
- Weather skill error handling

### Security
- Added SECURITY.md with vulnerability reporting process
- Implemented secret masking in config display
- Improved API key storage with separate secrets file
- Added .gitignore protection for sensitive files

## [2.0.0] - Previous Release

### Added
- Initial CLI implementation
- Basic LLM integration
- Configuration file support

## [1.0.0] - Initial Release

### Added
- Basic functionality
- Simple command-line interface

---

## Release Links

- [Unreleased](https://github.com/whykusanagi/celeste-cli/compare/v1.5.2...HEAD)
- [1.5.2](https://github.com/whykusanagi/celeste-cli/compare/v1.5.1...v1.5.2)
- [1.5.1](https://github.com/whykusanagi/celeste-cli/compare/v1.4.0...v1.5.1)
- [1.4.0](https://github.com/whykusanagi/celeste-cli/compare/v1.3.0...v1.4.0)
- [1.3.0](https://github.com/whykusanagi/celeste-cli/compare/v1.1.0...v1.3.0)
- [1.1.0](https://github.com/whykusanagi/celeste-cli/compare/v1.0.2...v1.1.0)
- [1.0.2](https://github.com/whykusanagi/celeste-cli/releases/tag/v1.0.2)
- [1.0.0](https://github.com/whykusanagi/celeste-cli/releases/tag/v1.0.0)

## How to Update

### From 0.x to 1.0+

The configuration format has changed:

**Old format** (`.celesteAI`):
```
api_key=sk-xxx
base_url=https://api.openai.com/v1
```

**New format** (`~/.celeste/config.json`):
```json
{
  "api_key": "",
  "base_url": "https://api.openai.com/v1",
  "model": "gpt-4o-mini",
  "timeout": 60,
  "skip_persona_prompt": false,
  "simulate_typing": true,
  "typing_speed": 40
}
```

**Migration steps**:
1. Backup your old config: `cp ~/.celesteAI ~/.celesteAI.backup`
2. Install new version: `make install`
3. Run config migration: `celeste config --show` (auto-migrates)
4. Verify settings: `celeste config --show`
5. Test: `celeste chat`

### Breaking Changes in 1.0+

- Command name changed from `celestecli` to `Celeste`
- Config file location changed to `~/.celeste/`
- Session format incompatible with 2.x (will create new sessions)
- Some command flags renamed for consistency

---

## Support

- **Issues**: [GitHub Issues](https://github.com/whykusanagi/celeste-cli/issues)
- **Security**: See [SECURITY.md](SECURITY.md)
- **Contributing**: See [CONTRIBUTING.md](CONTRIBUTING.md)
