# Command Routing

How user input is routed through the TUI to modes, tools, and LLM providers.

## Slash Commands

All slash commands are parsed in `tui/app.go` and dispatched before reaching the LLM:

```
User Input (/command args)
    ↓
Parse as Command (tui/app.go switch)
    ↓
├── /agent <goal>        → Agent runtime (agent/runtime.go)
├── /orchestrate <goal>  → Orchestrator (orchestrator/)
├── /plan <goal>         → Create plan via LLM → .celeste/plan.md
├── /graph               → Interactive graph browser view
├── /index               → Code graph dependency tree
├── /grimoire            → Read .grimoire from disk
├── /memories            → Memory manager view
├── /collections         → Collections manager view
├── /costs               → Session token/cost stats
├── /context             → Context budget display
├── /effort <level>      → Set reasoning effort
├── /endpoint <name>     → Switch LLM provider
├── /model <name>        → Change model
├── /nsfw                → Switch to Venice uncensored
├── /safe                → Return to safe mode
├── /clear               → Clear chat history
├── /help                → Show help
└── (other)              → commands.Execute() fallback
```

## Chat Message Flow

Non-command messages go through the LLM pipeline:

```
User Message
    ↓
AddUserMessage → chat history
    ↓
SendMessage (TUIClientAdapter)
    ↓
SendMessageStreamEvents (LLM backend)
    ↓
StreamChunkMsg → typing animation (corruption at cursor)
    ↓
Tool calls? → SkillCallBatchMsg → execute tools → send results → re-call LLM
    ↓
StreamDoneMsg → final content → session persistence
```

## Tool Execution

Tools auto-loop: the LLM calls tools, results are sent back, LLM decides if more calls are needed. Safety cap at 50 turns.

```
LLM Response
    ↓
Has tool_calls? ──No──→ Display response
    ↓ Yes
SkillCallBatchMsg
    ↓
Registry.Execute() for each tool
    ↓
Tool results → AddToolResult to chat
    ↓
Re-send to LLM (auto-loop)
    ↓
Repeat until no tool_calls or cap reached
```

## Provider Detection

Provider is auto-detected from the `base_url` in config:

| URL Pattern | Provider | Features |
|---|---|---|
| `api.x.ai` | Grok/xAI | Collections, 2M context |
| `api.openai.com` | OpenAI | Full function calling |
| `api.anthropic.com` | Anthropic | Native SDK, prompt caching |
| `api.venice.ai` | Venice | NSFW, image generation |
| `generativelanguage.googleapis.com` | Gemini | Free tier |
| `openrouter.ai` | OpenRouter | Multi-model |

## MCP Routing

When running as an MCP server (`celeste serve`), requests route through:

```
MCP Client → stdio/SSE → Server.dispatch()
    ↓
├── celeste tool        → runChatMode (tools auto-loop) or runAgentMode
├── celeste_content     → Content generation with persona
└── celeste_status      → Health/config check
```

Built with [Celeste CLI](https://github.com/whykusanagi/celeste-cli)
