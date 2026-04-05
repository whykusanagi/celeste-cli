# Celeste CLI Roadmap

*Last updated: April 2026 (v1.8.3)*

## Current Release: v1.8.3

- 40 built-in tools (dev, code graph, collections, web, crypto, productivity)
- Graph-based code review with 6 structural detection categories
- Real token streaming with corruption typing animation
- MinHash semantic search across code graph
- MCP server mode (stdio transport) for Claude Code integration
- Collections search via xAI hybrid RAG
- Persistent project memories and todo lists
- Markdown rendering with corrupted theme
- 7 LLM providers (Grok/xAI, OpenAI, Anthropic native, Google, Venice, Vertex AI, OpenRouter)
- Companion app: [celeste-for-claude](https://github.com/whykusanagi/celeste-for-claude)

## v1.9 Planned

### Code Intelligence
- [ ] LSP integration (go to definition, find references, rename)
- [ ] Cross-package edge resolution improvements for the code graph
- [ ] Per-language parser improvements (better JS/Python call extraction)

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
- [ ] Alt-screen mode (preserve terminal scrollback on exit)
- [ ] Session resume from TUI

### Platform
- [ ] `celeste models` command (query provider APIs for available models)
- [ ] Plugin system for community tools
- [ ] Web UI mode (browser-based TUI)
