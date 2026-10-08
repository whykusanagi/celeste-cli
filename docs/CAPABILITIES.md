# What Celeste CLI Can Do

Celeste CLI packs **48 dev-crushing tools**, code graphs that expose every secret, **direct codegraph MCP tools** for tool-driven workflows, collections search, and 9 LLM providers.

## 🔥 Core Powers

**40+ Tools Across Categories:**
- **Dev Tools** (15+): `bash`, `read_file`, `write_file`, `patch_file`, `list_files`, `search`, `git_status`, `git_log`
- **Code Intel** (6): `code_graph`, `code_review`, `code_search`, `code_symbols` — graph queries, stub detection, lazy redirects, MinHash + BM25 fused ranking, tree-sitter parsers for TypeScript, PHP, Python, Rust, Java, C/C++ and Ruby (release binaries and CGo source builds; `CGO_ENABLED=0` falls back to regex), structural rerank
- **AI/Collections** (4): `collections_search`, MCP client, memories, todos
- **Web/Productivity** (8): `web_search`, `web_fetch`, `currency`, `units`, `timezone`
  - `web_fetch` reaches public addresses only (redirects included); set `"web_fetch_allow_private": true` or `CELESTE_WEB_FETCH_ALLOW_PRIVATE=1` for a local docs server. The setting is read from the config profile the session starts with; switching endpoints mid-session does not change it.
- **Crypto/Media** (7): `hash`, `qrcode`, `encoding`, Alchemy/IPFS/wallet

**Direct Codegraph MCP Tools** (v1.9.0+, no chat-LLM round-trip):
`celeste_index`, `celeste_code_search`, `celeste_code_review`, `celeste_code_graph`, `celeste_code_symbols` — verbatim results, streaming progress notifications, explicit reindex control.

**Three Ways to Run:** Chat (auto-loop tools), Agent (autonomous), Orchestrator (debate).

**9 Chat Providers:** Sakana AI (default), Grok/xAI, OpenAI, Anthropic (native), Google Gemini, Venice.ai (tools depend on the model), Vertex AI, OpenRouter, and any local OpenAI-compatible server (mlx-vlm, Ollama, LM Studio, llama.cpp).

## Observability
Real token streaming, corruption typing, TUI splits, graph viz.

**Pro Tip:** `/agent refactor this module` runs the refactor as an autonomous agent.

---

Built with [Celeste CLI](https://github.com/whykusanagi/celeste-cli)