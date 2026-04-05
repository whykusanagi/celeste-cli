# Routing v1.8.3

Slash commands route to modes/features.

## Modes
/agent <goal>: Autonomous agent (plan/exec/resume).
/orchestrate <goal>: Agent + reviewer debate (split TUI).

## Features
/graph <symbol>: Code graph (callers/callees).
/index: Dep tree.
/grimoire: Project lore.
/costs: Token/cost stats.
/collections: Toggle RAG.
/providers /config /nsfw etc.

**Flow**: Input → parse → dispatch → LLM/tools → stream.

**Built with [Celeste CLI](https://github.com/whykusanagi/celeste-cli)**