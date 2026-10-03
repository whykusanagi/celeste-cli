# Celeste in your editor (ACP)

`celeste acp` runs celeste as an [Agent Client Protocol](https://agentclientprotocol.com) agent: the editor starts it, talks to it over stdin and stdout (newline-delimited JSON-RPC, protocol version 1), and shows its replies, tool calls, plans and permission prompts in its own agent panel. Zed and the JetBrains IDEs speak ACP.

```
celeste [-config <name>] acp
```

You never run it by hand; the editor does. `-config <name>` picks a named config (see [Named Configs](../README.md#named-configs-multi-profile)).

## Setup

Install celeste and configure a provider first (`celeste config`), and check that `celeste` is on the `PATH` the editor sees. Celeste uses its own config and keys, never the editor's: the editor's model picker and API keys do not apply to it.

### Zed

Add an agent server to Zed's `settings.json` (Zed > Settings > Open Settings):

```json
{
  "agent_servers": {
    "Celeste": {
      "type": "custom",
      "command": "celeste",
      "args": ["acp"],
      "env": {}
    }
  }
}
```

Then open the agent panel, start a new thread and pick **Celeste**. Use the full path to the binary as `command` if Zed does not find it. To use a named config, pass `"args": ["-config", "work", "acp"]`.

### JetBrains

In the AI Assistant's chat, open the agent menu and choose to add a custom agent (Settings > Tools > AI Assistant > Agents in recent versions). The IDE opens its ACP configuration file (`acp.json`); add celeste with the same command:

```json
{
  "agent_servers": {
    "Celeste": {
      "command": "celeste",
      "args": ["acp"]
    }
  }
}
```

Celeste then appears in the agent menu of the AI Assistant chat.

## What the editor sees

- **Replies** stream as message chunks. Compaction notes and other notices arrive as thoughts.
- **Tool calls** show with a title (`read_file: main.go`, `bash: go test ./...`), a kind the editor can draw an icon for (read, search, edit, execute, fetch, other), the file they touch, their input and their result (cut at 8 KiB in the editor; the model gets all of it).
- **Plans**: when the model updates the workspace todo list (the `todo` tool), the editor's plan shows it.
- **Permission prompts**: a tool that needs approval (by your `permissions.json` rules or a `PreToolUse` hook asking) shows the editor's prompt with **Allow**, **Always allow `<tool>`** (for this session only; nothing is written to `permissions.json`) and **Reject**. A hook's "ask" is always asked, also after "Always allow".
- **Cancel** stops the turn, a pending permission prompt included (it counts as a reject).
- **Sessions**: each editor thread is a celeste session (the same ID), saved after every prompt with the editor's folder as its workspace. `celeste resume` lists it. When the editor reopens a thread (`session/load`), celeste replays it, your prompts, its replies and each tool call with its result, and the next prompt continues with the whole history.

Each session gets the chat's tools, persona, project context (grimoire, `AGENTS.md`/`CLAUDE.md`, memories, git snapshot, code graph) and hooks for the editor's folder. When the context window is small, the persona steps down and the first prompt shows a notice saying so.

## MCP servers

The editor can pass its own MCP servers in `session/new`; celeste starts the stdio ones in the session's folder (`http` and `sse` servers are not supported and are ignored). Your global MCP config (`~/.celeste/mcp.json`) applies too, and wins over an editor server with the same name. A repository's own MCP config (`.mcp.json`, `.celeste/mcp.json`) is never started in an ACP session: it would run a command before you could be asked.

Each session starts its own set of MCP servers, the global ones included, and stops them when the editor closes. Several open threads mean several copies of each server; keep heavy servers in the editor's per-project settings rather than the global config if that matters.

## Repository hooks

Hooks follow the same trust rules as everywhere (see [HOOKS.md](HOOKS.md)): your global `~/.celeste/hooks.json` always runs, and a repository's hooks (`.celeste/hooks.json`, a project grimoire's hooks or `## Stream Rules`) run only once trusted.

An untrusted repository hook file is skipped when the session starts. At the session's **first prompt**, celeste asks through the editor's permission prompt, once per file, showing its commands: **Trust these hooks** stores the approval (as `celeste hooks trust` does) and the hooks run in that same prompt; **Skip** runs the prompt without them, and celeste does not ask again in that session. A trusted file that changes is untrusted again until approved. A repository's `.celeste/config.json` sandbox loosening (`"enabled": false`, `"network": true` or extra `"writable"` paths) is asked about the same way, as **Trust these settings**; Skip keeps the sandbox as it was. You can also trust a repository ahead of time with `celeste hooks trust` in its folder. A session reopened in another folder asks about that folder's files at its next prompt.

## Logs

Stdout carries only the protocol. Celeste's logs and warnings (setup warnings, hook warnings, MCP failures) go to the log file in `~/.celeste/logs`; look there when something does not show up in the editor.

## Limits

- No images or audio in prompts; embedded files and file links are supported.
- Celeste reads and writes files and runs commands itself: it does not use the editor's file system or terminal methods, so unsaved editor buffers are not seen.
- No session modes (plan mode) and no ACP authentication methods.
- One prompt at a time per session; a second one while the first runs gets an error.
