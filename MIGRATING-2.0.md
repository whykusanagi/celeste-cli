# Migrating to celeste 2.0

## Runtime mode (`classic` / `claw`) is gone

Chat always runs tools in a loop, so `classic` and `claw` were the same program.

| 1.x | 2.0 |
|---|---|
| `"runtime_mode"` in a config file | Removed automatically the first time the file loads; one line on stderr says so. |
| `"claw_max_tool_iterations": N` | Renamed to `"max_tool_iterations": N` on load. If both are set, `max_tool_iterations` wins. |
| `celeste -mode claw …` | Error. Delete the flag. |
| `celeste -claw-max-iterations N` | Still works, with a warning. Use `-max-tool-iterations N`. |
| `celeste config --set-mode …` | Error. Delete the call. |
| `celeste config --set-claw-max-iterations N` | Still works, with a warning. Use `--set-max-tool-iterations N`. |
| `celeste config --init celeste-classic` / `celeste-claw` | Error. Use `--init openai` (or any provider). |

## Typing speed

| 1.x | 2.0 |
|---|---|
| `"typing_speed": 40` or `25` in a config file | Removed automatically the first time the file loads (one line on stderr says so). 1.x ignored `typing_speed`; these were old defaults celeste itself wrote, and honouring them now would type slower than before. The new default is 60 characters per second. Any other value is yours and is kept and honoured. |

Every other key in your config file keeps its value. The MCP `celeste` tool's
`mode` argument (`chat` / `agent`) is unrelated and unchanged.

## `skip_persona_prompt` is gone

The persona is always on in chat and agent runs, for every provider.

| 1.x | 2.0 |
|---|---|
| `"skip_persona_prompt": true` in a config file | Removed automatically the first time the file loads; one line on stderr says so. |
| `"skip_persona_prompt": false` | Ignored; the next save drops it. |
| `celeste config --skip-persona …` | Error. Delete the call. |
| A DigitalOcean agent profile (`--init digitalocean` set `skip_persona_prompt: true`) | DigitalOcean agents now also get Celeste's persona, on top of the agent's own instructions. |
| Gemini with `skip_persona_prompt: true` sent no system prompt at all | Gemini gets the system prompt like every other provider. |

## Sessions and agent checkpoints

| 1.x | 2.0 |
|---|---|
| A saved session or agent run (`celeste resume`, `celeste agent --resume`) | Loads unchanged, except that a tool result over 128 KiB is cut to 128 KiB (start and end kept) when it loads, and the next save stores the cut version. If you need the full text of such a result, copy the session file (under `~/.celeste/sessions`) or the run checkpoint (under `~/.celeste/agent/runs`) before resuming it in 2.0. |
| Tool results between 64 KiB and 128 KiB | Sent whole. 1.x cut every tool result to 64 KiB on each request. |

## File checkpoints

| 1.x | 2.0 |
|---|---|
| Checkpoints lived in memory and were lost when celeste exited; `/undo` and `/diff` were placeholders; `celeste revert` could not find any checkpoint | Checkpoints are written to `~/.celeste/checkpoints/<session>/` with an `index.json`. `/undo`, `/diff` and `celeste revert <file> [--session id]` use them. |
| `~/.celeste/checkpoints/` directories left by 1.x (backups only) | Kept while they are among the 20 most recent or under 30 days old, then deleted at startup like any other session. 2.0 does not read them. |

A checkpoint is a full copy of the file as it was. There is no size limit:
a session keeps at most 100 checkpoints, so one that rewrites a large file
many times can use 100 times its size until the session is pruned. Backups
can hold secrets; the directory is created readable by you only.

Restoring a file (`/undo`, `celeste revert`, a failed write's rollback)
replaces it with a new file of the same mode, so other hard links to it,
another user's ownership, extended attributes and ACLs are not kept. When
celeste is not permitted to create files in the file's directory, it
overwrites the file in place instead, which keeps all of them but is not
atomic. It does so only when it can read the file's current contents, and
puts them back if the write fails halfway; if that fails too, the error
says so and the file may be left partly written.

Each checkpoint also records the file's size and SHA-256 as celeste's write
left it. `/undo` and `celeste revert` compare the file with that before
restoring; a file changed since (by you, a formatter, a command) is left
alone with a warning, until you repeat `/undo` or pass `--force`.

## Project context files

| 1.x | 2.0 |
|---|---|
| Automatic `.grimoire` / `.gitignore` creation (chat, MCP `mode: "chat"` and `mode: "agent"`) | Stops. Nothing is written into your project unless you ask: `/init` (or `celeste init`) writes `.grimoire`, `/init agents` (or `celeste init --agents`) also writes `AGENTS.md`. The chat suggests `/init` once per session when the project has no context. Existing `.grimoire` files are read as before. |
| The `project-init` "first visit" memory | No longer created. Existing ones stay. |
| `AGENTS.md` / `CLAUDE.md` | Read from the workspace up to the git root and added to the project context under the grimoire (the grimoire wins on conflict). 32 KiB per file, 64 KiB in all. |
## OpenAI uses the Responses API

| 1.x | 2.0 |
|---|---|
| The `openai` provider used Chat Completions (`/v1/chat/completions`) | It uses the Responses API (`/v1/responses`), with `store: false`; nothing is kept on OpenAI's side between requests. |
| An endpoint configured as `openai` that has no `/v1/responses` (a proxy, a gateway) | The first request gets a 404 or "unsupported endpoint"; celeste answers it through Chat Completions and stays on Chat Completions for the session. One log line says so. No config change needed. |
| Sessions saved before 2.0 | Load and continue unchanged; their messages are sent as plain messages. |

## Editing files

| 1.x | 2.0 |
|---|---|
| Editing a file not read in the session (`patch_file`, `write_file` over an existing file or appending to it, `splice_file`) | Refused with `read_file <path> first: celeste edits an existing file only after reading it in this session`. Read the file, then edit it. Creating a new file needs no read. In MCP chat each call starts with no reads, so read the file in the same call. |
| A file edited by `write_file`, `patch_file` or `splice_file` | Replaced through a temp file and a rename, keeping its mode. The replacement is a new file owned by you, so another user's ownership, extended attributes (such as macOS `com.apple.*`) and ACLs are not kept. A file in a directory celeste cannot write to can no longer be edited. |
