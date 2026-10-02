# Hooks

Hooks run your own commands at points in a Celeste session. They can block a tool call, rewrite its input, or add context for the model. For regex rules on the model's own output (not on tool events), see [STEERING.md](STEERING.md); a project grimoire's `## Stream Rules` section is trusted with the same `celeste hooks trust`.

**Where hooks load.** The chat UI (`celeste chat`, `celeste resume`), every agent run and MCP chat load hooks: `celeste agent`, the MCP server's `celeste` tool in both `mode: "agent"` and `mode: "chat"`, `/agent` in the chat, subagents, and `/orchestrate` lanes.

Agent runs and MCP chat are **non-interactive**: they never ask you to trust a repo's hooks. Untrusted repo hooks are skipped with a warning (`hooks: skipping …`); approve them ahead of time with `celeste hooks trust`. Your global `~/.celeste/hooks.json` always runs, so a global guard now protects agent runs and MCP chat too. The MCP server returns these warnings in the tool result, under `## Warnings`.

In agent runs, tool hooks fire on every tool call, and PreCompact and PostCompact fire around the agent's compaction summary (trigger `auto`, empty `custom_instructions`), never before plain pruning of old tool results. A PreCompact `deny` skips the summary and is reported as a warning. SessionStart and Stop belong to the top-level run: they fire for `celeste agent` and MCP agent mode. Subagents, `/orchestrate` lanes and `/agent` in the chat run inside a larger run, so they skip both; for `/agent`, the chat session has already fired SessionStart. Subagents and `/agent` still carry the chat's SessionStart `additionalContext` into their own system prompt, even though neither fires the hook again. UserPromptSubmit sees the goal of `celeste agent`, MCP agent mode and `/agent`, once, before any model call: a `deny` (or a failed hook) stops the run before it starts, and `additionalContext` is sent after the goal. Subagents and `/orchestrate` lanes skip it, because the model wrote their goals. Subagents and `/agent` in the chat share the chat's hooks: they see the hooks loaded when the chat started.

In MCP chat (`mode: "chat"`), tool hooks fire on every tool call. The server loads hooks once per workspace and reuses them across calls. Editing `hooks.json` or approving a hook applies from the next call, and `session_id` is shared by the calls that reuse the setup. Each call is its own session otherwise:
- SessionStart fires once per call (source `startup`), and its `additionalContext` goes into that call's system prompt.
- UserPromptSubmit sees the call's `prompt`. A `deny` (or a failed hook) refuses the call with an error result carrying `reason`; `additionalContext` is sent with the prompt.
- Stop follows the rule below.
- MCP chat prunes old tool results but never writes a summary, so PreCompact and PostCompact don't fire there.
- A failed hook's warning comes back on the call that ran it, under `## Warnings`.

## Where hooks live

| File | Trust |
|---|---|
| `~/.celeste/hooks.json` | Trusted: it is yours. |
| `~/.celeste/grimoire.md` (`## Hooks`) | Trusted, protocol v1. |
| `.celeste/hooks.json` in the project or any parent directory | Needs approval. |
| `.grimoire`, `.grimoire.local`, `.celeste/grimoire/*.md` in the project or any parent directory (`## Hooks`) | Needs approval, protocol v1. |

Celeste asks once per file, on the terminal before the chat UI opens, and records the approval in `~/.celeste/trusted.json`. What you approve is the file's location plus a hash of its hook definitions (event, matcher, command, timeout, protocol) — not the scripts those commands call. If a hook runs `./scripts/guard.sh`, changing that script does not need re-approval; only editing `hooks.json` itself does.

If the hooks in a file change (command, matcher, timeout or protocol), Celeste asks again. Formatting changes, and edits to the rest of a grimoire, don't trigger a new prompt.

A repo hook file that is a symlink is refused outright; copy it instead.

Only a person approves. Non-interactive runs never approve anything; untrusted or changed hooks are skipped with a warning, and Celeste starts up without them rather than failing to start. Non-interactive means:
- piped input or output;
- Git Bash/mintty consoles, which the terminal check does not recognize as a terminal — approve ahead of time with `celeste hooks trust` instead of expecting the startup prompt.

Approve ahead of time:

```
celeste hooks list                 # every source and its trust status
celeste hooks trust                # approve this directory's untrusted sources (asks y/N)
celeste hooks trust --yes [path]   # approve without asking: scripts, CI, mintty
```

`celeste hooks trust` with no path approves everything `celeste hooks list` would show for the current directory. A path argument can be a directory or a specific `hooks.json`/grimoire file that Celeste would otherwise load; pointing it at a file Celeste doesn't read from (wrong name, wrong location) is an error, not a silent no-op.

## hooks.json

```json
{
  "hooks": [
    {"event": "PreToolUse", "matcher": "bash", "command": "./scripts/guard.sh", "timeout": 10},
    {"event": "SessionStart", "command": "cat .celeste/session-notes.md | jq -Rs '{additionalContext: .}'"}
  ]
}
```

| Field | Meaning |
|---|---|
| `event` | `PreToolUse`, `PostToolUse`, `SessionStart`, `UserPromptSubmit`, `PreCompact`, `PostCompact`, `Stop`, `SubagentStop` |
| `matcher` | Tool events only: a tool name, or `*` (the default) |
| `command` | Run by `sh -c` (macOS, Linux) or `cmd.exe /c` (Windows), in the **project root** of the file that defines it (the directory holding `.celeste/`), or in the workspace for `~/.celeste` hooks. See [Where hooks run](#where-hooks-run). |
| `timeout` | Seconds, 1–600, default 30. On timeout the whole process tree is killed. |
| `protocol` | `v2` (default) or `v1` |

The whole file is rejected, with a warning, if any of these is true:
- it has an unknown field or an invalid entry;
- a `command` or `matcher` contains a control character or a bidi override;
- the file is over 1 MiB.

### Where hooks run

| Defined in | Working directory (`project_dir`) |
|---|---|
| `~/.celeste/hooks.json`, `~/.celeste/grimoire.md` | the workspace |
| `<dir>/.celeste/hooks.json` | `<dir>` |
| `<dir>/.celeste/grimoire/*.md` | `<dir>` (the directory holding `.celeste`, not `.celeste/grimoire`) |
| `<dir>/.grimoire`, `<dir>/.grimoire.local` | `<dir>` |

## Protocol v2

**Input.** A JSON object on stdin:
- always: `event`, `session_id`, `workspace`, `project_dir`;
- tool events: `tool_name` and `tool_input`;
- PostToolUse: also `tool_response` (`content`, `error`, `truncated`);
- UserPromptSubmit: `prompt`;
- SessionStart: `source` (`startup` or `resume`);
- PreCompact: `trigger` (`manual` or `auto`) and `custom_instructions`;
- PostCompact: `trigger` and `summary`;
- Stop and SubagentStop: `last_message`, plus `agent_id` for SubagentStop.

`project_dir` is the directory the hook runs in (see [Where hooks run](#where-hooks-run)). It is also set as `CELESTE_PROJECT_DIR` in the environment (below).

**Environment.** `CELESTE_HOOK_EVENT`, `CELESTE_WORKSPACE`, `CELESTE_PROJECT_DIR` and `CELESTE_SESSION_ID`. Tool events add `CELESTE_TOOL_NAME`, `CELESTE_TOOL_INPUT`, `CELESTE_TOOL_PATH` and `CELESTE_TOOL_COMMAND`.

A value that is too large (over 120 KiB on macOS/Linux, 8 KiB on Windows) or contains a NUL byte is left **empty**, and `CELESTE_TOOL_INPUT_TRUNCATED=1` is set. It is never cut short — a hook that only looks at a truncated prefix could be shown a harmless-looking start of an otherwise dangerous command. **A guard must read stdin**, which always carries the full input, instead of relying on the environment. A padded or oversized command shows up as an empty environment variable plus the truncation flag; a v1 hook depends entirely on the environment, so it fails (and blocks) when the value it needs was omitted.

**Output.** Nothing (allow), or one JSON object on stdout:

```json
{"decision": "allow|deny|ask", "reason": "…", "additionalContext": "…", "updatedInput": {…}}
```

- `decision` must be exactly `allow`, `deny` or `ask`, or absent. Anything else — an empty string, `"approve"`, `"block"`, a boolean, a number — is malformed output and fails the hook the same as bad JSON.
- `deny` blocks the tool call, prompt or compaction. Who sees `reason` depends on the event:
  - PreToolUse: the model, in the tool result;
  - UserPromptSubmit and PreCompact: you, in the chat (for MCP chat, the calling client, in the error result). The model never sees it. A blocked prompt is removed from the conversation and the saved session. Every prompt is checked once, including each queued steer and a prompt kept after an interrupt.
- `ask` shows the permission prompt even when the tool would be allowed. Without a prompt (headless), it denies.
- `allow` never skips the permission prompt: a hook can force an `ask`, and hard `always_deny` rules run before any hook, but nothing a hook returns can wave a call through your own permission rules.
- `additionalContext` reaches the model:
  - at the start of the tool result (PreToolUse, PostToolUse);
  - with the prompt (UserPromptSubmit), in every later request of the session. In the chat it is not saved: after `celeste resume`, earlier prompts are sent without it. An agent goal's context is part of the goal message, so a resumed agent run keeps it;
  - in the system prompt (SessionStart);
  - as summary instructions (PreCompact).

  PostCompact's `additionalContext` is currently ignored.
- `updatedInput` (PreToolUse) replaces the tool's input. It goes through the same `ValidateInput` and full permission check as the model's own input — a hook cannot use `updatedInput` to sneak past validation or the permission gate.

**Decisions only matter for some events.** PreToolUse, UserPromptSubmit and PreCompact are gating: `deny`/`ask` actually change what happens. SessionStart, PostToolUse and PostCompact are observational: any `decision` a hook returns for them is ignored, and only `additionalContext` (not for PostCompact, above) and, for a failure, the warning have an effect.

**Stop.** In `celeste agent`, MCP agent mode, MCP chat and the chat UI, a Stop hook's `deny` is acted on: when the run is about to finish, it continues with `reason` as the next instruction (`Continue.` if empty). This happens at most once per run and only while turns remain; a second `deny`, or one with no turns left, is reported as a warning and ignored. Stop fires only when a run finishes as completed, not when it stops on the turn cap, a guard, an error or an interrupt. The chat UI acts on it the same way: the continuation is shown in the chat, and the model receives the `reason` as a hidden instruction. While the hook runs, the chat's status bar says "Running Stop hook…"; Esc cancels the running hook, and the turn ends as interrupted without a warning.

**SubagentStop.** A subagent started with `spawn_agent` fires SubagentStop, not Stop, when it finishes as completed. `agent_id` is the subagent's ID, the one `spawn_agent` returns and `/agents` lists, and a resumed subagent keeps it (after a restart, when celeste no longer knows the original run, it is the checkpoint ID being resumed). `last_message` is its final reply. Its `deny` is acted on like Stop's: the subagent continues once with `reason` as the next instruction, only while turns remain. `/orchestrate` lanes and `/agent` in the chat fire neither Stop nor SubagentStop. Subagent hooks (tool hooks and SubagentStop) see the chat session's `session_id`. The lanes of one `/orchestrate` run share one `session_id`, of the form `orchestrator-<n>`.

A non-zero exit, a timeout, more than 1 MiB of stdout, or anything that is not one JSON object as above is a hook failure. This applies to protocol v1 too: a hook that floods stdout fails closed even if it exits 0, so a 1.x guard that used to print a lot of debug output and rely on the exit code now needs to keep stdout under 1 MiB. PreToolUse, UserPromptSubmit and PreCompact then **block** ("hook failed: …"). The other events show a warning in the chat and carry on — they never block the session.

Several hooks for one event run in order: global files, then repo files from the outermost directory in. The first `deny` stops the chain; an `ask` is remembered but doesn't stop later hooks from running; `updatedInput` from one hook is what the next hook sees; `additionalContext` from every hook that ran is joined (8 KiB total per event).

**Ordering with the permission prompt.** For a tool call: (1) a deny-only pass checks your own `always_deny` rules against the model's original input, before any hook process runs — a hard denial never reaches a hook; (2) PreToolUse hooks run; (3) if a hook rewrote the input, it is re-validated; (4) the full permission check runs on the final input, where a hook's `ask` forces a prompt that would otherwise not have appeared. This is a change from 1.x, where hooks ran after the prompt.

## Protocol v1 (grimoire `## Hooks`)

```markdown
## Hooks
### PreToolUse
- bash: ./scripts/check.sh {{command}}
```

These keep their 1.x behaviour:
- `{{workspace}}`, `{{tool}}`, `{{path}}` and `{{command}}` are substituted as quoted shell words;
- the payload is on stdin and in `CELESTE_*` (whole values up to the limits above);
- exit 0 allows. For PreToolUse any other exit blocks, with the output as the reason. For PostToolUse a non-zero exit only shows a warning;
- they run with `sh -c`, which on Windows needs a POSIX `sh` on `PATH`.

Only PreToolUse and PostToolUse are supported.

Changes in 2.0:
- hooks run **before** the permission prompt, not after;
- they run in the project root, not the workspace: the directory holding `.grimoire`/`.grimoire.local`, or the directory holding `.celeste` for `.celeste/grimoire/*.md` (the workspace for `~/.celeste/grimoire.md`);
- a v1 hook whose `CELESTE_TOOL_*` value had to be left empty fails, which blocks the tool call;
- a v1 hook that prints more than 1 MiB to stdout now fails closed even at exit 0 (1.x had no cap).

To migrate, move each line into `hooks.json` as `{"event": "PreToolUse", "matcher": "bash", "command": "./scripts/check.sh", "protocol": "v1"}`. Better still, drop `"protocol"` and have the script read stdin and print a v2 decision.

## Windows: don't reference `%CELESTE_*%` in a command line

A v2 hook's `command` runs through `cmd.exe /d /s /c "…"`. cmd.exe expands any `%VAR%` it finds in that command line, or in a batch file it calls, before your program ever sees it. If your hook writes something like `echo %CELESTE_TOOL_COMMAND%` directly in `hooks.json`'s `command`, or in a `.bat`/`.cmd` script it invokes without `setlocal enabledelayedexpansion`, the value gets substituted at parse time. Since `CELESTE_TOOL_COMMAND` and the other `CELESTE_*` variables carry attacker- or model-influenced text (the tool's own input), a value that happens to contain another `%...%` sequence can end up expanding to something you didn't intend, or breaking your command's quoting.

Read the JSON payload from stdin instead — it is never re-interpreted by the shell. If a script genuinely needs an environment variable's value inside conditional logic, use `setlocal enabledelayedexpansion` and `!VAR!`, which expands at run time rather than when cmd.exe first parses the line.

## Protected files

Celeste's own tools (`write_file`, `patch_file`, `splice_file`, and every other tool that writes through the same path resolution) refuse to write `~/.celeste/hooks.json`, `~/.celeste/grimoire.md` or `~/.celeste/trusted.json`, under any spelling (absolute, `~/`, `$HOME/`, `${HOME}/`, or a symlink that resolves to one of them), and refuse any `.celeste/trusted.json` wherever it is. `read_file` and search/list tools can still see these files — only writes are blocked.

This is defence in depth, not the trust boundary: it stops Celeste's own tools from quietly rewriting your hook trust or global hooks, but a `bash` tool call can still reach these files with ordinary shell redirection until the sandboxed execution environment lands. The trust model in this document — approval keyed to a found path and a hash of the definitions — doesn't depend on this guard; it's a safety net on top.

A repo's own `.celeste/hooks.json` is not protected the same way: Celeste can write it (so you can ask Celeste to set up your project's hooks), and any edit simply re-prompts for approval the next time hooks load.
