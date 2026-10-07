# Migrating to celeste 2.0

Before you start, back up `~/.celeste` (for example `cp -R ~/.celeste ~/.celeste-1x-backup`).
2.0 removes or renames a few config keys the first time any celeste command
(`celeste chat`, `celeste config --show`, ...) loads that config file; the
sections below say which. Then install 2.0 and check it:

```bash
go install github.com/whykusanagi/celeste-cli/v2/cmd/celeste@latest
which celeste          # the same $GOPATH/bin/celeste (or ~/go/bin/celeste) as 1.x
celeste version        # prints 2.x
celeste update         # a go install build: installs the official binary (below)
celeste persona verify # official persona: ...
```

## Install path

2.0 moved the Go module to `github.com/whykusanagi/celeste-cli/v2`, so `go install`
needs the new path:

```bash
go install github.com/whykusanagi/celeste-cli/v2/cmd/celeste@latest
```

| 1.x | 2.0 |
|---|---|
| `go install` on the old path (without `/v2`) at `@latest` | Still installs the newest 1.x release, never 2.0. The last 1.x release prints, at most once a day on stderr, where 2.0 lives (`CELESTE_NO_V2_NOTICE=1` hides it). Install from the `/v2` path above. |
| A signed binary from the [Releases](https://github.com/whykusanagi/celeste-cli/releases) page | Unchanged. Verify it as described in [VERIFY.md](VERIFY.md). |
| Go code that imports celeste packages | Change the imports to `github.com/whykusanagi/celeste-cli/v2/cmd/...`. |

## Release binaries include tree-sitter

1.x release binaries were built with `CGO_ENABLED=0`, so the code graph parsed every language
except Go with regex. 2.0 release binaries are built with CGo on each platform's own runner and
include the tree-sitter parsers for TypeScript, PHP, Python, Rust, Java, C/C++ and Ruby, with
more accurate call edges ([#376](https://github.com/whykusanagi/celeste-cli/issues/376)).
Java, C, C++ and Ruby files are now indexed too; 1.x skipped them.
Rebuild an index made by 1.x to get them: `celeste index rebuild`.

| Platform | 2.0 release binary |
|---|---|
| Linux (amd64, arm64) | Still statically linked: it needs no particular glibc and runs on the same systems as 1.x. |
| macOS (Intel, Apple silicon) | Links only the system libraries, as before. Needs macOS 12 or later, which Go 1.26 already required. |
| Windows (amd64) | Needs no DLL beyond the ones Windows ships. |

A build from source compiles the parsers with the local C compiler. With `CGO_ENABLED=0` it
still builds and falls back to the regex parsers. So does a build with `CGO_ENABLED` and `CC`
both unset on a machine whose default C compiler is missing, since Go then turns CGo off by
itself; an explicit `CGO_ENABLED=1`, or a `CC` naming a compiler that is missing, fails the
build instead.

## The persona

Celeste's persona is not open source. Official release binaries carry the key that
decrypts it; every other build runs a short public persona and says so. See
[docs/PERSONALITY.md](docs/PERSONALITY.md).

| 1.x | 2.0 |
|---|---|
| `~/.celeste/celeste_essence.json` | No longer read, and there is no replacement override. celeste logs once that it is ignored; you can delete it. |
| Every build had the same persona | An official release binary (the Releases page, or a `go install` build after it upgrades itself, below) runs the full persona. A build from a checkout or a fork runs the public persona: one identity line, the rule against claiming an action a tool didn't report, and the voice boundary. It says so once at startup. |
| No way to check | `celeste persona verify` prints `official persona: ...` and exits 0 on an official binary; it exits 1 and names the reason on any other build. |
| One persona size for every model | The persona is picked by the model's context window: chat uses the `full` profile, agent runs `spine`. A profile larger than a quarter of the window steps down (`full`, `spine`, `lite`); `lite` stays while it fits in half the window, and below that only the identity, the honesty rule and the voice boundary stay. The chat, `celeste agent` and `celeste message` say once which profile they use. A local model with no `context_limit` (a server on this machine or the local network, by its host) uses the window its server reports (Ollama, llama.cpp, LM Studio), or 8,192 tokens when it reports none, so set `context_limit` in your config to the server's real window to get `full`. The tool definitions are fitted the same way ([#310](https://github.com/whykusanagi/celeste-cli/issues/310)): when the persona and every tool schema would leave less than a quarter of the window for history, celeste sends a core set of tools (read, write, patch, list, search, bash, todo, plan mode's `submit_plan` and `find_tools`) with short descriptions, adds the others while they fit (MCP tools last), and says so once. `find_tools` activates any tool that was left out, and a tool it activated is never dropped. |

## Self-update (`celeste update`)

| 1.x | 2.0 |
|---|---|
| A `go install` build ran as built | The first time it runs a command (any but `help`, `version`, `update` and `persona`), a `go install` build of a release tag downloads the official release binary of the same version, checks its GPG signature and checksums with the release key built into celeste ([VERIFY.md](VERIFY.md) lists the checks), replaces itself and starts again, so it runs the full persona. One line on stderr says so. If the download fails, nothing is replaced, that run uses the public persona, and celeste tries again in an hour. `celeste serve` and `celeste acp` never replace themselves mid-run: they install the update for the next launch. |
| | Set `CELESTE_NO_AUTO_UPGRADE=1` to keep the binary `go install` built. A build from a checkout (`make install`, `go build`) never downloads anything. |
| No update command | `celeste update` installs the newest official release; `celeste update --check` only reports it. Neither ever downgrades. On a build from a checkout, both exit 1 and tell you to pull and rebuild. |

## One tool loop

Chat, `celeste agent`, the MCP `celeste` tool (`mode: "chat"` and `mode: "agent"`),
`/agent`, subagents and `/orchestrate` lanes now run on the same tool loop, with the
same caps, guards, permissions and hooks. These are intentional changes:

| 1.x | 2.0 |
|---|---|
| Each mode had its own loop and rules | One loop. A run stops after 3 identical tool calls in a row, 6 turns without progress, or 3 turns of invalid arguments. The chat's 25-turn cap counts every model turn. |
| Tool calls ran one at a time (agent runs, MCP chat) | Calls marked safe (reads, searches) run in parallel; their results keep the call order. |
| One timeout for every tool in agent runs | Each tool has its own timeout: 45 s by default, `bash` and `generate_speech` 5 min, `spawn_agent` 10 min, `audio_render` 2 min, `ask` and `submit_plan` 30 min (they wait on your answer). These override `--tool-timeout` in agent runs. |
| Large tool results were trimmed on every request | A result over 128 KiB is saved whole to a file readable by you only (0600, in a 0700 directory, also in the chat; 1.x wrote 0644 files) and the conversation keeps 128 KiB of it, start and end. See "Sessions and agent checkpoints". |
| Agent runs and MCP chat loaded only part of your setup | They also load hooks, custom tools (`~/.celeste/skills`), MCP servers from your home configs, memories, the grimoire and the code-graph summary. With 3 or more custom tools or a large MCP server, a run passes 40 tools and switches on tool discovery (`find_tools`). |
| A repository's `.mcp.json` or `.celeste/mcp.json` started in every mode | Starts only in the interactive chat. Agent runs, MCP chat, `celeste acp` and subagents use your home-level MCP configs only. |
| MCP chat sent a failed tool call to the model as plain text | It sends `{"error":true,"message":...,"tool":...}`. |
| `/orchestrate` silently denied every tool that needed approval | It asks with the chat's permission prompt. A headless orchestrator still denies, and says so. "Always allow" from the `/agent` and `/orchestrate` prompts is saved to `permissions.json` like the chat's. |
| A `permissions.json` that could not be parsed was replaced with defaults on the next save | It is never overwritten; the error shows as a warning. Saves are atomic. |
| `UserPromptSubmit` hooks ran in the chat only | They also check the goal of `celeste agent`, MCP agent mode and `/agent`, and every MCP chat prompt. A `deny` stops the run before any model call. |
| Esc during a chat turn could leave half a turn in history | Esc stops the turn; a partial reply stays on screen, and nothing half-finished is saved. Messages typed during a turn join it at the next tool boundary. |
| `celeste agent` used the chat model | It uses `agent_model` when set, as subagents and MCP agent mode already did. |

## Request timeout

| 1.x | 2.0 |
|---|---|
| `timeout` capped the whole request: a reply still streaming after 60 s failed | `timeout` is a stall timeout: a request fails when nothing arrives for that long. A reply that keeps streaming runs to a cap of 30 minutes (or 3× `timeout`). Hosted providers still fail after 60 s of silence by default. |
| A local server got the same 60 s, so a cold first turn on a local model failed | A local server (decided by its host: `127.0.0.1`, `localhost`, a private address, a single-label name or a `.local` host, the same rule that makes it the **local** provider with tools and the 8,192 window fallback) whose `timeout` is unset or 60 gets 600 s. Any other value you set is kept. The first byte of a reply from a local server may take 30 minutes (or `timeout`, when longer); the stall timeout applies between chunks. |
| No way to set it from the CLI | `celeste config --set-timeout <seconds>` (0 = default); `config` shows the value in use. |
| A compaction summary or `/handoff` failed after a fixed 3 minutes | Summaries get the same stall timeout and cap as a chat turn, so a cold local model can finish one. |
| `celeste agent` ignored `timeout` and gave each turn 90 s | It uses `timeout` like the chat. `-request-timeout` still bounds a whole turn; without it the 30-minute cap does. |
| A turn that failed left its message, and retrying sent the prompt twice | Sending the same text again retries the unanswered message; the request carries it once. |

## Permission prompt

| 1.x | 2.0 |
|---|---|
| A single key answered the permission prompt, so typed text could answer it (the `a` in a sentence allowed the call once) | `a` (allow once) or `A` (always allow) only picks the answer; Enter confirms it. Any other key after `a` or `A` cancels the pick, so text starting with `a` or `Always` allows nothing. Enter with nothing picked does nothing. |
| | Typed text never answers it: a printable key that is not one of the prompt's keys (or a paste) starts typing mode, where every key, `d` and `D` included, is ignored with a hint until you press Enter. `d` (deny) and `D` (always deny) still answer at once outside typing mode. Esc always denies. |

## Hooks

1.x ran hooks from grimoire `## Hooks` sections. 2.0 adds `hooks.json`
(`~/.celeste/hooks.json` or a repository's `.celeste/hooks.json`) with a JSON
protocol, and asks before running a repository's hooks. See [docs/HOOKS.md](docs/HOOKS.md).

| 1.x | 2.0 |
|---|---|
| Grimoire `## Hooks` sections | Still read, as protocol v1 (`sh -c`, as before); no conversion is needed, and your files are never rewritten. `~/.celeste/grimoire.md` is trusted; a project's `.grimoire` hooks need approval. Moving to `hooks.json` is optional. |
| A repository's hooks ran without asking | The chat asks once per file before it opens and records the approval in `~/.celeste/trusted.json`. Changing a hook's command, matcher, timeout or protocol asks again. |
| | Non-interactive runs (agent runs, MCP chat, `celeste acp`, piped input) never ask: untrusted repository hooks are skipped with a warning. Approve them ahead of time, from the repository: `celeste hooks list` shows each source and its status, `celeste hooks trust` approves this directory's untrusted sources (asks y/N), and `celeste hooks trust --yes [path]` approves without asking (scripts, CI). |
| Pre-tool hooks ran after the permission prompt | They run before it, after a deny-only pass, so a hook's side effects can happen even if you then deny the call. A hook's `ask` forces the prompt; in a headless run that means deny. |
| Hooks inherited every `CELESTE_*` variable and any amount of output | They don't inherit `CELESTE_*` variables. Output over 1 MiB fails the hook, including a v1 hook that exits 0. A tool input too large for the environment is left out (`CELESTE_TOOL_INPUT_TRUNCATED=1`), never cut short. |
| A grimoire hook with a tab or other control character | Skipped. Hooks run in their source's project root. |
| Tools could write `~/.celeste/hooks.json`, `grimoire.md` and `trusted.json` | File tools refuse to; reading them still works. `bash` commands that name them are denied (best effort). |
| Agent runs, MCP chat and subagents ran no hooks | Your global hooks run everywhere; see "One tool loop". |

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

## Environment variables override the config file

| 1.x | 2.0 |
|---|---|
| `CELESTE_API_KEY`, `CELESTE_API_ENDPOINT` and `TAROT_AUTH_TOKEN` were listed in the help but never read | They are read and **override** the config file's `api_key`, `base_url` and `tarot_auth_token` for the chat, `celeste message`, `celeste agent`, `celeste skill`, `celeste serve` and `celeste acp`. A blank variable changes nothing. The value applies to that run only and is never written to the config file. If one of them is still exported from an old setup (a shell profile, a CI secret), celeste now uses it instead of the config file: unset it to go back to the file's value. |

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
alone with a warning, until you repeat `/undo` or pass `--force`. The
check does not lock out other programs: one that writes the file after
the check and before the restore replaces it has that edit overwritten
without a warning.

## Project context files

| 1.x | 2.0 |
|---|---|
| Automatic `.grimoire` / `.gitignore` creation (chat, MCP `mode: "chat"` and `mode: "agent"`) | Stops. Nothing is written into your project unless you ask: `/init` (or `celeste init`) writes `.grimoire`, `/init agents` (or `celeste init --agents`) also writes `AGENTS.md`. The chat suggests `/init` once per session when the project has no context. Existing `.grimoire` files are read as before. |
| The `project-init` "first visit" memory | No longer created. Existing ones stay. |
| `AGENTS.md` / `CLAUDE.md` | Read from the workspace up to the git root and added to the project context under the grimoire (the grimoire wins on conflict). 32 KiB per file, 64 KiB in all. |

## MCP client

| 1.x | 2.0 |
|---|---|
| MCP `readOnlyHint` | Ignored unless the server is marked `"trusted": true`. A trusted server's tools that set `readOnlyHint: true` count as read-only, so they run without asking in default mode and are also available to explore subagents. `"trusted"` is read only from your home-level configs (`~/.celeste/mcp.json`, `~/.claude/mcp.json`, `~/.cursor/mcp.json`); in a project's `.mcp.json` or `.celeste/mcp.json` it has no effect. Set it only for servers you control. |
| Two MCP servers whose names sanitize to the same tool name (`a.b` and `a_b`) | The server whose name sorts first keeps the tool (the same one on every launch); the other server's tool is not registered, with a warning naming both. Disconnecting the second server no longer removes the first one's tool. |
| A custom JSON tool (`~/.celeste/skills/*.json`) named like a built-in tool or another custom tool | No longer replaces it: the built-in (or the first file, in directory order) keeps the name, the file is skipped with a warning, and the other files still load. Rename the tool to use it. |

## Custom tools (`~/.celeste/skills/*.json`)

A custom JSON tool's `command` now runs through the same shell runner as `bash`
(without its denylist):

| 1.x | 2.0 |
|---|---|
| The command ran with `/bin/sh -c` | It runs with `sh -c`, the first `sh` on your `PATH`. |
| The tool returned stdout only; stderr was dropped | It returns stdout and stderr combined, in the order they were written. |
| Output of any size was returned | Output is capped at 64,000 bytes, with `[output truncated at 64000 bytes]` appended. |
| No timeout of its own | 2 minutes (sooner if the caller's deadline ends first); the command and everything it started are killed. |
| A background process that kept the output open could hang the call | It gets 2 seconds after the command exits, then it is killed and the call fails, saying so. A background process that redirected its output keeps running. |

| 1.x | 2.0 |
|---|---|
| The `openai` provider used Chat Completions (`/v1/chat/completions`) | It uses the Responses API (`/v1/responses`), with `store: false`: OpenAI keeps no response state between requests (each request sends the whole conversation), though its own abuse-monitoring retention still applies. |
| An endpoint configured as `openai` that has no `/v1/responses` (a proxy, a gateway) | The first request gets a 404 or "unsupported endpoint"; celeste answers it through Chat Completions and stays on Chat Completions for that endpoint until celeste restarts (for `celeste serve`, every later call too). One log line says so. No config change needed. |
| Sessions saved before 2.0 | Load and continue unchanged; their messages are sent as plain messages. |

## Editing files

| 1.x | 2.0 |
|---|---|
| Editing a file not read in the session (`patch_file`, `write_file` over an existing file or appending to it, `splice_file`) | Refused with `read_file <path> first: celeste edits an existing file only after reading it in this session`. Read the file, then edit it. Creating a new file needs no read. In MCP chat each call starts with no reads, so read the file in the same call. |
| A file edited by `write_file`, `patch_file` or `splice_file` | Replaced through a temp file and a rename, keeping its mode. The replacement is a new file owned by you, so another user's ownership, extended attributes (such as macOS `com.apple.*`) and ACLs are not kept. A file in a directory celeste cannot write to can no longer be edited. |

## Plans

| 1.x | 2.0 |
|---|---|
| `/plan <goal>` wrote `.celeste/plan.md` via the model | `/plan` enters plan mode (read-only tools until you approve a plan); approved plans live in `.celeste/plan.json` and the todo list. See `docs/PLAN_MODE.md`. |
| `/plan cancel` | Gone. `/plan off` leaves plan mode; delete `.celeste/plan.json` to drop an approved plan. |
| `celeste plan` read `.celeste/plan.md`, `PLAN.md`, `plan.md`, `CODEBASE_FIX_PLAN.md` or `FIX_PLAN.md` | Shows `.celeste/plan.json` with todo status; a leftover `.celeste/plan.md` is shown with a note when there is no `plan.json`. Other files are no longer read. |

## Sandbox for `bash`

The sandbox is new and off by default in 2.0, so nothing changes until you turn it on. See [docs/SANDBOX.md](docs/SANDBOX.md).

| 1.x | 2.0 |
|---|---|
| `bash` commands could write anywhere you can | Unchanged by default. With `"sandbox": {"enabled": true}` in `~/.celeste/config.json` (it applies with a named profile active too; a profile's own `sandbox` keys win), `bash` runs under seatbelt (macOS) or bubblewrap (Linux) and can write only to the workspace, temp and cache directories. Per workspace, `.celeste/config.json` takes `sandbox.enabled`, `sandbox.writable` and `sandbox.network`. A blocked write's error names the sandbox and the key to change. A repository's loosening (`enabled: false`, `network: true`, `writable`) applies only after `celeste hooks trust`; its tightening applies always. |
| Linux without bubblewrap, Windows | With the sandbox on: one warning (Linux) or log line (Windows), and commands run with the denylist only. |

## Sessions: `/rewind` and `/fork`

| 1.x | 2.0 |
|---|---|
| Session lists in creation order | `celeste resume`, `/session list` and the `/session` picker list this project's sessions first, marked `(this project)`. New sessions record their workspace; older session files load unchanged. |
| No way to take back a prompt | `/rewind [n]` takes back the last n prompts: it restores the files those turns changed from the session's checkpoints, ends the chat before the n-th last prompt and puts that prompt back in the input box. Files changed by `bash` are not restored. It is refused across a `/compact` summary. |
| | `/fork` continues in a copy of the session; the original stays as it was. |

## Images

| 1.x | 2.0 |
|---|---|
| An image too big for the provider failed the whole request (a 400 on Anthropic) | `read_file` reduces an image to a size every provider accepts (5 MB of base64, 8000 px on the long edge) and says it did, or refuses it with the limit named. When a request is sent, each provider's own limits are applied again, and an image that still can't fit is replaced with a short note. |
| An animated GIF | Sent as its first frame where the provider takes no animation. |
| WebP | Passed through when the provider accepts it, otherwise refused or replaced with a note. It is never converted: convert it to PNG or JPEG yourself. |
| Many images in one Anthropic request | More than 20 images caps each at 2000 px. |

## Editors: `celeste acp`

`celeste acp` is new: an [Agent Client Protocol](https://agentclientprotocol.com)
agent for editors such as Zed and JetBrains, over stdio. Each editor session gets
the chat's tools, persona, hooks and project context for the editor's folder, and
is saved as a celeste session. It skips untrusted repository hooks and the
repository's MCP configs, and asks for tool permissions through the editor.
Nothing changes for existing setups.
