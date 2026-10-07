# Celeste CLI and Other Agentic Coding CLIs

*Written October 2026 for celeste 2.0. Every competitor cell comes from that
tool's public documentation or release notes, listed under [Sources](#sources)
with the date they were read. A cell that no source could confirm says
"unverified": it means we could not check it, not that the tool lacks it.
Celeste's column is checked against the code (the tool and provider counts are
pinned by a test).*

These tools change monthly. If a cell is out of date, open an issue with a link
to the source and it gets fixed.

## What celeste keeps that none of these ship

1. **A code graph with structural review.** `celeste index` builds a persistent
   index of symbols and call edges; `celeste_code_search` ranks code by concept
   (MinHash and BM25, fused), `celeste_code_graph` walks callers and callees, and
   `celeste_code_review` finds stubs, lazy redirects, placeholders, swallowed
   errors, TODOs and hardcoded values from the graph rather than by grep. See
   [CODEGRAPH.md](CODEGRAPH.md). The closest thing in the other tools is
   oh-my-pi's tree-sitter `ast_grep`/`ast_edit` and reviewer subagents, which
   search and edit structurally but keep no graph.
2. **MCP-server mode built around that graph.** `celeste serve` exposes
   celeste to Claude Code, Codex or any MCP client, including the code-graph
   tools as direct calls that return verbatim results without a model in the
   way. Claude Code (`claude mcp serve`) and Codex (`codex mcp-server`) can also
   be MCP servers, but they expose their agent or their editing tools, not an
   index.
3. **A character.** Celeste is a persona with its own voice, at four levels
   (full, spine, lite, off) that fit the context window; official builds carry
   it encrypted. None of the other tools' documentation describes one.

## Feature table

| Feature | **Celeste** | **Claude Code** | **Codex CLI** | **opencode** | **Crush** | **Gemini CLI** | **oh-my-pi** | **pi** |
|---|---|---|---|---|---|---|---|---|
| **Project context files** | `AGENTS.md` and `CLAUDE.md` up to the git root, under the project's `.grimoire` | `CLAUDE.md` (project, user, org); also reads `AGENTS.md` | `AGENTS.md` | `AGENTS.md`, falling back to `CLAUDE.md`; global `AGENTS.md` | `CRUSH.md` and `AGENTS.md`, plus `context_paths`; `initialize` writes `AGENTS.md` | `GEMINI.md`, hierarchical; `context.fileName` can add `AGENTS.md` | `AGENTS.md`; imports rules from `.claude`, `.cursor`, `.codex` and others | `AGENTS.md` or `CLAUDE.md` from the agent dir, parent dirs and cwd |
| **Skills** | Custom JSON tools (`~/.celeste/skills/*.json`); Agent Skills (`SKILL.md`) planned | Skills (also custom commands) | Skills | Agent Skills (`SKILL.md`), also from `.claude/skills` and `.agents/skills` | Agent Skills (`SKILL.md`) | Agent Skills | Skills, discovered from other tools' dotfiles; `manage_skill` tool | Agent Skills (`SKILL.md`) |
| **Hooks** | 8 events (`PreToolUse` … `SubagentStop`), JSON over stdin/stdout; a repository's hooks need `celeste hooks trust` | 30+ events; command, HTTP, MCP-tool, prompt and agent handlers | 12 events (`PreToolUse`, `PermissionRequest`, `PreCompact`, `SubagentStop`, …); command and MCP-tool handlers | JS/TS plugin events (`tool.execute.before`, `session.compacted`, …); no shell hooks | `PreToolUse` only, Claude Code-compatible ("preliminary") | Command hooks: `BeforeTool`, `AfterTool`, `BeforeModel`, `PreCompress`, `SessionStart` and more | TypeScript extensions | TypeScript extension events (e.g. `session_before_compact`) |
| **Permission model** | Modes default, strict, trust; allow/deny rules; a hook can force a prompt | Modes default, acceptEdits, plan, auto (classifier), dontAsk, bypassPermissions; allow/deny rules | `approval_policy` on-request, never or granular; command rules; auto-review | allow, ask or deny per tool with patterns; most default to allow; `--auto` | Asks before tool calls; allow list; `--yolo` | Approval modes default, auto_edit, plan; `--yolo`; policy engine; trusted folders | Permission prompts for destructive tools in ACP mode; TUI unverified | No per-call approval; project trust gates extensions only |
| **Sandbox** | Opt-in: seatbelt (macOS), bubblewrap (Linux), optional network cut; none on Windows | Shell commands: Seatbelt (macOS), bubblewrap (Linux, WSL2); none on native Windows | read-only, workspace-write or full access; Seatbelt (macOS), bubblewrap with Landlock fallback (Linux, WSL) | unverified | unverified | Seatbelt (macOS); Docker or Podman containers | unverified | None built in; docs recommend containers or VMs |
| **Compaction** | Client ladder: prune (never unseen results), opt-in Jev prune, summary with a state block; `/compact`; server compaction planned | Auto-compact and `/compact`; summarize from or up to a point via `/rewind` | Automatic and `/compact`; remote conversation compaction | Automatic (default on) and `/compact`; optional pruning of old tool output | Automatic summarization (can be disabled) | `/compress`; automatic at `model.compressionThreshold` | `checkpoint`/`rewind` tools collapse exploration; snapcompact | Automatic and `/compact`, client-side; extensions can replace it |
| **Checkpoints and rewind** | `/undo`, `/diff`, `/rewind [n]`, `/fork`, `celeste revert`; `bash` changes not tracked | Checkpoint per prompt; `/rewind` (Esc Esc) restores code and/or conversation; Bash changes not tracked; `/branch` | `/fork`; file rewind unverified | Snapshots; `/undo` and `/redo` revert files (git repository) | unverified | `/rewind` (Esc Esc) for conversation and/or code; checkpointing | `checkpoint` and `rewind` tools; fork and resume sessions | Session tree: `/tree` returns to any point, branches in one file; file restore unverified |
| **Subagents** | `spawn_agent` with types explore, general, review and a validated result; `/orchestrate` | Custom subagents; agent teams | Subagents and custom agents | General, Explore and Scout; @-mention or Task tool | unverified | Subagents, exposed as tools | `task` fans out parallel subagents, optionally in isolated worktrees, with schema-validated results | No (by design) |
| **Plan mode** | `/plan`: read-only tools until an approved `submit_plan`; chat only | `plan` permission mode | `/plan` or Shift+Tab | Plan agent (edits and bash ask first) | unverified | Read-only Plan Mode (`--approval-mode=plan`) | `/plan`; a plan model role | No (by design) |
| **MCP client** | stdio, SSE and streamable HTTP; `readOnlyHint` only from trusted servers | stdio, HTTP, SSE (deprecated), WebSocket | Yes (`codex mcp add`) | Local and remote servers | stdio, HTTP, SSE | Yes | Yes; discovers servers from other tools' configs | Yes (since 0.99; in 1.0, October 2026): stdio and streamable HTTP |
| **MCP server** | `celeste serve`, with direct code-graph tools | `claude mcp serve` (its tools) | `codex mcp-server` | unverified | unverified | unverified | unverified | No |
| **ACP** | `celeste acp` (sessions reopen with `session/load`) | Through Zed's adapter | Through an adapter | `opencode acp` | unverified | `gemini --acp` | `omp acp` | Through the `pi-acp` adapter |
| **LSP** | Planned (diagnostics after writes) | Code-intelligence plugins (type errors after edits, symbol navigation) | unverified | Diagnostics as feedback; off by default | Yes (gopls, typescript-language-server, …) | unverified | Built in: diagnostics on every write, 14 operations | unverified |
| **Providers** | 9 chat providers: Anthropic, OpenAI, xAI, Gemini, Vertex AI, OpenRouter, Venice, Sakana, local | Claude via the Anthropic API, Amazon Bedrock, Claude Platform on AWS, Google Cloud Agent Platform, Microsoft Foundry; LLM gateways | OpenAI; custom `model_providers`; Amazon Bedrock | 75+ via the AI SDK and Models.dev | 20+ (OpenAI, Anthropic, Gemini, Bedrock, Vertex AI, Azure, OpenRouter, Groq, …) and compatible APIs | Gemini via Google sign-in, Gemini API key or Vertex AI | 60+ | 15+ |
| **Local models** | Any OpenAI-compatible server on this machine or the local network (Ollama, LM Studio, llama.cpp, mlx), no key | unverified | `--oss` with Ollama or LM Studio | Ollama, LM Studio, llama.cpp | Ollama, llama.cpp, LM Studio, OMLX, LiteLLM | A local Gemma model for routing decisions only (experimental) | Ollama, LM Studio, llama.cpp, vLLM | A llama.cpp guide in its docs; others unverified |
| **Code graph and structural review** | Yes: persistent symbol and call-edge index, concept search, structural review, all as MCP tools | `/code-review` command; code graph unverified | Code review of local changes; code graph unverified | unverified | unverified | unverified | Tree-sitter `ast_grep`/`ast_edit`; `/review` reviewer subagents; no graph | unverified |
| **Persona** | Celeste, four levels sized to the window; encrypted in official builds | unverified | unverified | unverified | unverified | unverified | unverified | unverified |
| **Built-in tools** | 48 (plus `spawn_agent`, `post_message` and any MCP or custom tools) | unverified | unverified | unverified | unverified | unverified | 31 | unverified |

## Notes per tool

**Claude Code** (Anthropic). The broadest hook surface of the eight (over thirty
events, five handler types) and the most permission modes, including a
classifier-driven auto mode. Its shell sandbox uses the same OS mechanisms as
celeste's (Seatbelt, bubblewrap) and, like celeste's, has none on native
Windows. Checkpoints track the file tools, not Bash, which is celeste's rule
too. Its documentation covers Claude models only, through Anthropic or a cloud
platform.

**Codex CLI** (OpenAI). Sandbox first: every command runs under a sandbox mode,
and approvals sit on top. Hooks now cover twelve events, close to celeste's
eight plus permission and interrupt events. Local models through `--oss`
(Ollama, LM Studio). We found no rewind of file changes in its documentation.

**opencode.** The widest provider list (75+) and native ACP. Hooks are
JavaScript/TypeScript plugins rather than shell commands. Permissions default to
allow, and its documentation describes no OS sandbox. Snapshots back `/undo`
and `/redo`.

**Crush** (Charm). Like celeste, a Go binary. Agent Skills, LSP and three MCP
transports; hooks are limited to `PreToolUse` and marked preliminary. We found no
documentation for a sandbox, checkpoints, subagents, plan mode or ACP, so those
cells say "unverified". License: FSL-1.1-MIT.

**Gemini CLI** (Google). Native ACP (`gemini --acp`), a read-only Plan Mode,
`/rewind`, subagents, Agent Skills, and sandboxing through Seatbelt or
containers. It runs Gemini models; a local Gemma model only makes routing
decisions.

**oh-my-pi** (`omp`, a fork of pi). The most feature-dense of the group: built-in
LSP on every write, hash-anchored edits, parallel subagents in isolated
worktrees, a debugger, native ACP, 60+ providers, and "time-traveling stream
rules" for course correction, an idea close to celeste's stream rules. Its structural tools are
tree-sitter queries and rewrites; it keeps no code graph or index.

**pi.** Deliberately minimal: no subagents, no plan mode and no permission
prompts by design, with safety left to containers. Extensions, Agent Skills, a
branching session tree and a built-in MCP client (since 0.99; part of the 1.0
release on 1 October 2026). ACP comes through the `pi-acp` adapter.

## Sources

Retrieved 2026-10-02 unless noted. Celeste's column comes from this repository
([CHANGELOG.md](../CHANGELOG.md), [HOOKS.md](HOOKS.md), [SANDBOX.md](SANDBOX.md),
[SUBAGENTS.md](SUBAGENTS.md), [CODEGRAPH.md](CODEGRAPH.md),
[LLM_PROVIDERS.md](LLM_PROVIDERS.md)).

### Claude Code

- Documentation index: https://code.claude.com/docs/llms.txt
- Memory (`CLAUDE.md`, `AGENTS.md`): https://code.claude.com/docs/en/memory
- Hooks: https://code.claude.com/docs/en/hooks
- Permission modes: https://code.claude.com/docs/en/permission-modes
- Sandboxing: https://code.claude.com/docs/en/sandboxing
- Checkpointing: https://code.claude.com/docs/en/checkpointing
- MCP: https://code.claude.com/docs/en/mcp
- Subagents: https://code.claude.com/docs/en/sub-agents
- Code-intelligence plugins: https://code.claude.com/docs/en/plugins/code-intelligence
- Deployment options: https://code.claude.com/docs/en/third-party-integrations
- ACP agents list: https://agentclientprotocol.com/overview/agents

### Codex CLI

- Documentation index: https://developers.openai.com/codex/llms.txt
- Codex manual (AGENTS.md, skills, hooks, approvals, sandbox, `/plan`, `/compact`, `/fork`, `oss_provider`): https://learn.chatgpt.com/docs/codex-manual.md
- Codex as an MCP server: https://developers.openai.com/cookbook/examples/codex/codex_mcp_agents_sdk/building_consistent_workflows_codex_cli_agents_sdk
- ACP agents list: https://agentclientprotocol.com/overview/agents

### opencode

- Docs: https://opencode.ai/docs/
- Rules (context files): https://opencode.ai/docs/rules/
- Agent Skills: https://opencode.ai/docs/skills/
- Plugins: https://opencode.ai/docs/plugins/
- Permissions: https://opencode.ai/docs/permissions/
- Config (compaction, snapshots): https://opencode.ai/docs/config/
- TUI (`/undo`, `/redo`, `/compact`): https://opencode.ai/docs/tui/
- Agents: https://opencode.ai/docs/agents/
- MCP servers: https://opencode.ai/docs/mcp-servers/
- ACP: https://opencode.ai/docs/acp/
- LSP: https://opencode.ai/docs/lsp/
- Providers: https://opencode.ai/docs/providers/

### Crush

- README: https://github.com/charmbracelet/crush
- Hooks guide: https://github.com/charmbracelet/crush/tree/main/docs/hooks
- Config schema (`disable_auto_summarize`): https://github.com/charmbracelet/crush/blob/main/schema.json
- ACP agents list (Crush not listed): https://agentclientprotocol.com/overview/agents

### Gemini CLI

- README: https://github.com/google-gemini/gemini-cli
- Docs directory: https://github.com/google-gemini/gemini-cli/tree/main/docs (`cli/gemini-md.md`, `cli/skills.md`, `hooks/reference.md`, `cli/sandbox.md`, `cli/rewind.md`, `cli/checkpointing.md`, `cli/plan-mode.md`, `cli/acp-mode.md`, `core/subagents.md`, `core/local-model-routing.md`, `tools/mcp-server.md`, `reference/commands.md`, `reference/configuration.md`)

### oh-my-pi

- README: https://github.com/can1357/oh-my-pi

### pi

- Site: https://pi.dev/
- Coding agent README: https://github.com/badlogic/pi-mono/tree/main/packages/coding-agent
- Docs: https://github.com/badlogic/pi-mono/tree/main/packages/coding-agent/docs (`compaction.md`, `mcp.md`, `security.md`, `skills.md`, `providers.md`, `llama-cpp.md`)
- 1.0 release coverage: https://www.theregister.com/ai-and-ml/2026/10/02/pi-coding-agent-pulls-a-180-and-adds-mcp-support/5300678
- 1.0 release date (1 October 2026): https://gigazine.net/gsc_news/en/20261002-pi-1-0/
- ACP agents list (`pi-acp`): https://agentclientprotocol.com/overview/agents
