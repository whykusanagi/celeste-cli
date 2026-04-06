# Command Routing

How user input is routed through the TUI to modes, tools, and LLM providers.

## Slash Commands

All slash commands are parsed in `tui/app.go` and dispatched before reaching the LLM:

```mermaid
flowchart TD
    Input["User Input"] --> Parse{"Slash command?"}
    Parse -->|No| LLM["Send to LLM"]
    Parse -->|Yes| Dispatch["Command Dispatch"]

    Dispatch --> Agent["/agent → agent/runtime.go"]
    Dispatch --> Orch["/orchestrate → orchestrator/"]
    Dispatch --> Plan["/plan → create .celeste/plan.md"]
    Dispatch --> Graph["/graph → graph browser view"]
    Dispatch --> Index["/index → dependency tree"]
    Dispatch --> Grim["/grimoire → read .grimoire"]
    Dispatch --> Mem["/memories → memory manager"]
    Dispatch --> Coll["/collections → collections view"]
    Dispatch --> Settings["/endpoint /model /effort /nsfw /safe"]
    Dispatch --> Info["/costs /context /help /clear"]
```

## Chat Message Flow

Non-command messages go through the LLM streaming pipeline:

```mermaid
sequenceDiagram
    participant User
    participant TUI as TUI (app.go)
    participant Adapter as TUIClientAdapter
    participant LLM as LLM Backend
    participant Tools as Tool Registry

    User->>TUI: Send message
    TUI->>TUI: AddUserMessage
    TUI->>Adapter: SendMessage()
    Adapter->>LLM: SendMessageStreamEvents()

    loop Real-time streaming
        LLM-->>TUI: StreamChunkMsg (content delta)
        TUI->>TUI: Typing animation + corruption at cursor
    end

    alt Has tool_calls
        LLM-->>TUI: SkillCallBatchMsg
        TUI->>Tools: Registry.Execute() per tool
        Tools-->>TUI: Tool results
        TUI->>LLM: Re-send with results (auto-loop)
    else No tool_calls
        LLM-->>TUI: StreamDoneMsg
        TUI->>TUI: Final content + session persist
    end
```

## Tool Execution Loop

Tools auto-loop with a 50-turn safety cap:

```mermaid
flowchart TD
    Response["LLM Response"] --> HasTools{"Has tool_calls?"}
    HasTools -->|No| Display["Display response"]
    HasTools -->|Yes| Batch["SkillCallBatchMsg"]
    Batch --> Execute["Registry.Execute() per tool"]
    Execute --> Results["Add tool results to chat"]
    Results --> Cap{"Turn < 50?"}
    Cap -->|Yes| Resend["Re-send to LLM"]
    Resend --> Response
    Cap -->|No| Stop["Safety cap reached"]
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

```mermaid
flowchart LR
    Client["MCP Client"] -->|stdio/SSE| Server["Server.dispatch()"]
    Server --> Celeste["celeste tool"]
    Server --> Content["celeste_content"]
    Server --> Status["celeste_status"]

    Celeste -->|chat| Chat["runChatMode\n(tools auto-loop)"]
    Celeste -->|agent| Agent["runAgentMode\n(autonomous)"]
```

Built with [Celeste CLI](https://github.com/whykusanagi/celeste-cli)
