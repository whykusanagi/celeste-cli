# Changelog

All notable changes to Celeste CLI will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Unreleased

### Breaking Changes

* **config:** `skip_persona_prompt` and `celeste config --skip-persona` are removed: the persona is always on in chat and agent runs, for every provider. DigitalOcean agents, which have their own built-in persona, now also get Celeste's. A config with `"skip_persona_prompt": true` loses the key on load, with one note on stderr; `false` is ignored. See MIGRATING-2.0.md.

### Features

* **acp:** `celeste acp`: an Agent Client Protocol agent for Zed and JetBrains, over stdio ([#176](https://github.com/whykusanagi/celeste-cli/issues/176)). Each editor session gets the chat's tools, persona, hooks and project context for the editor's folder (plus the editor's stdio MCP servers) and is saved as a celeste session, its history updated after each prompt. Prompts run on celeste's tool loop: replies stream as message chunks, tool calls show with their kind, title and file, a `todo` result updates the editor's plan, and compaction notes arrive as thoughts. Tools that need approval ask through the editor's permission prompt (allow once, always allow for the session, reject); cancelling stops the turn, a pending permission prompt included. Editor threads can be reopened: `session/load` replays the conversation, tool calls included, and the next prompt continues it. An untrusted repository hook file is asked about once, at the session's first prompt, through the editor's permission prompt (its commands shown); trusting it stores the approval and the hooks run in that prompt, skipping runs without them. Celeste uses its own config and keys (`-config <name>` picks a profile); stdout carries only the protocol, and logs go to `~/.celeste/logs`. See `docs/ACP.md` for the Zed and JetBrains setup.
* **anthropic:** Claude's thinking is kept and sent back on later turns, in its original order and byte for byte, also after `celeste resume` ([#192](https://github.com/whykusanagi/celeste-cli/issues/192)). Models that take a thinking budget (Haiku 4.5 and older) now think on tool-loop continuations too, whenever the previous turn's thinking is replayed. On Anthropic's API a request that replays thinking asks the API to drop, rather than reject, any block whose history changed; elsewhere a refused request is resent once without the old thinking. Changing the system prompt (`/user`, `/confirm`, a persona change) drops replayed thinking once, at that point.
* **checkpoints:** file checkpoints are kept on disk per session (`~/.celeste/checkpoints/<session>/index.json` plus backups), so `/undo`, `/diff` and `celeste revert` work, also after `celeste resume` and from another terminal. `/undo` restores the last changed file (repeat to walk back; when the file is no longer as celeste's change left it, by an edit, a formatter or a command, the first `/undo` says so and a second one overwrites it); `/diff` lists the files the session changed with line counts (binary and large files summarized); `celeste revert <file> [--session id] [--force]` restores a file from the latest session that changed it, and refuses a file changed since unless `--force`. `/undo` and `celeste revert` wait while a write is in progress. Subagents and `/agent` runs file their changes under the chat's session. The last 20 sessions, or anything changed in the last 30 days, are kept; older ones are deleted at startup.
* **steering:** stream rules ([#175](https://github.com/whykusanagi/celeste-cli/issues/175)). A rule is a regex on the reply as it streams, or on a tool call's arguments, with a reminder that joins only when it fires; it can stop the reply and re-run the turn. Rules live in `~/.celeste/rules/*.md` and in a grimoire `## Stream Rules` section (a project grimoire's rules must be trusted like repo hooks, with `celeste hooks trust`); five ship built in (persona voice in files, an unbacked "Audio saved:" claim, `TASK_COMPLETE` before checking, `git push --force` / `rm -rf`, strike ladders). `stream_rules` is `shadow` by default (match and log only); set it to `on` to act. See `docs/STEERING.md`.
* **steering:** a watchdog ballot every 3 turns (`watchdog`: off by default) that can warn a run that is looping, drifting, claiming unchecked success or about to do something destructive, and a completion gate for agent runs (`completion_gate`: shadow by default) that needs `TASK_COMPLETE` at the start of the reply's first or last line and, with the watchdog on, no unchecked success claim ([#175](https://github.com/whykusanagi/celeste-cli/issues/175)). What goes to Jev or the small model for steering or pruning has file paths made workspace-relative or replaced with `<path>`, as well as secrets redacted.
* **subagents:** `spawn_agent` takes `type`: `explore` (read-only tools, the persona off: her identity line, the honesty rule and the voice boundary; the small model), `review` (read, git and code-graph tools, the persona off, the agent model) or `general` (the default: every tool, the persona, the agent model). Subagents finish with `submit_result`, and the parent gets a validated `{summary, findings, files}` as JSON (a subagent that never calls it still returns that shape, its final text as the summary, with a `warning`). `/agents` shows each typed run's summary. See `docs/SUBAGENTS.md` ([#176](https://github.com/whykusanagi/celeste-cli/issues/176)).
* **steering:** Jev can act, each opt-in: `jev_prune: on` prunes what Jev judges least needed first, `jev_gate` asks before destructive, data-leaking or out-of-scope tool calls (it can only add a question), and `jev_route` picks `/orchestrate`'s lane. `celeste config --init jev` saves the key and turns all three on in shadow mode ([#175](https://github.com/whykusanagi/celeste-cli/issues/175)).
* **server:** `celeste_status` reports `completions`, `oracle` (latency, hit rate) and `rules` (fire counts); `health` is `degraded` while the latest completion failed.
* **providers:** celeste uses the model the provider serves now. A configured or default model the provider has retired falls back to the provider's current default (for Venice, the model it flags as default) and celeste says so. Model lists are cached for 24h in `~/.celeste/cache/models`; the config file is not rewritten. Applies to the chat, `/endpoint`, `/set-model`, `celeste agent`, `celeste message` and `celeste serve`.
* **providers:** Anthropic's model list is read from `GET /v1/models`.
* **config:** `"pin_model": true` (or `CELESTE_PIN_MODEL=1`) turns model resolution off; `/set-model <name> --force` pins a model for the session.
* **context:** `AGENTS.md` and `CLAUDE.md` are read from the workspace up to the git root (root first, `AGENTS.md` before `CLAUDE.md` in each directory) and added to the project context under the grimoire, which wins where they conflict ([#176](https://github.com/whykusanagi/celeste-cli/issues/176)). Every mode gets them: the chat, `celeste agent` and MCP chat. Each file is cut at 32 KiB and all of them at 64 KiB, with a marker and a warning. A context file that is a symlink is read only when it points to another `AGENTS.md` or `CLAUDE.md` inside the repository and outside `.git` (a `CLAUDE.md -> AGENTS.md` link is read once); an empty file is skipped. `/grimoire` and `celeste grimoire` show them too.
* **context:** `/init` in the chat writes `.grimoire` for the project, and `/init agents` also writes an `AGENTS.md` with the detected build and test commands; `celeste init --agents` does the same from the shell. An existing file is never overwritten. A chat session in a project with no `.grimoire` and no `AGENTS.md`/`CLAUDE.md` says so once at the start.
* **context (breaking):** celeste no longer creates `.grimoire` or edits `.gitignore` in your project: not in the chat, not in MCP `mode: "chat"` or `mode: "agent"`. Run `/init` (or `celeste init`) when you want one. See `MIGRATING-2.0.md`.
* **tools:** `patch_file` takes `edits[]`: several `{old_string, new_string, replace_all?}` edits to one file (1-50), applied in order, each to the result of the previous ones, all or nothing; a failing edit names itself (`edit 3: ...`) and nothing is written ([#176](https://github.com/whykusanagi/celeste-cli/issues/176)).
* **tools:** when `old_string` is not found exactly, `patch_file` tries a whitespace- and indentation-tolerant line match, used only when exactly one place matches; `new_string` is re-indented to the file and the result carries `"fuzzy": true` and a unified diff of the change. `replace_all` never uses it ([#176](https://github.com/whykusanagi/celeste-cli/issues/176)).
* **sessions:** messages can carry a provider's own reply format (`provider_blocks` in session files and agent checkpoints), kept byte for byte across save and resume. Nothing fills it yet; Anthropic thinking replay and the OpenAI Responses backend build on it. Older session files load unchanged.
* **providers:** the `openai` provider talks to OpenAI's Responses API. A reasoning model's reasoning is kept (encrypted, as OpenAI returns it) and sent back on later turns, also after `celeste resume`. An endpoint that has no Responses API is answered through Chat Completions for the rest of the session, with one log line. Other OpenAI-compatible providers (Venice, OpenRouter, local servers) still use Chat Completions.
* **sandbox:** `bash` can run under the OS sandbox, opt-in for 2.0 (`"sandbox": {"enabled": true}` in `~/.celeste/config.json`): a seatbelt profile on macOS, bubblewrap on Linux when installed. Commands can write only to the workspace, temp directories, the user cache directory and existing build caches (`~/go/pkg`, `~/.cargo`, `~/.gradle`, `~/.npm`, `~/.m2/repository`, or `$GOPATH`, `$GOMODCACHE`, `$CARGO_HOME` and `$GRADLE_USER_HOME` when set), and the repository's git directories; `"network": false` cuts the network. Per workspace, `.celeste/config.json` takes `sandbox.enabled`, `sandbox.writable` and `sandbox.network`; a repository's loosening applies only once trusted with `celeste hooks trust` (non-interactive runs skip it with a warning), its tightening always. A blocked write or lookup gets an error naming the sandbox and the key to change. Linux without a usable `bwrap` warns once and runs with the denylist; Windows has no sandbox. Hooks, custom tools and `--verify-cmd` are not sandboxed. See `docs/SANDBOX.md` ([#176](https://github.com/whykusanagi/celeste-cli/issues/176)).
* **providers:** the `openai` provider talks to OpenAI's Responses API. A reasoning model's reasoning is kept (encrypted, as OpenAI returns it) and sent back on later turns, also after `celeste resume`. An endpoint that has no Responses API is answered through Chat Completions until restart, with one log line. Other OpenAI-compatible providers (Venice, OpenRouter, local servers) still use Chat Completions.
* **compact:** compaction summaries carry an authoritative state block ([#200](https://github.com/whykusanagi/celeste-cli/issues/200)): the workspace todo list, the files the session changed (from the checkpoint index) and the voice rule, rendered from celeste's own records rather than from what the summarizer kept.
* **chat:** `/rewind [n]` takes back the last n prompts (default 1): the files their turns changed are restored from the session's checkpoints (created files are deleted), the chat ends before the n-th last prompt, and that prompt is put back in the input box. As with `/undo`, a file changed outside celeste since is not overwritten until the same `/rewind` is repeated. A rewind past a `/compact` summary is refused; files changed by `bash` are not restored, nor a subagent's unless the rewound turns changed a file themselves before it ran ([#176](https://github.com/whykusanagi/celeste-cli/issues/176)).
* **chat:** plan mode ([#176](https://github.com/whykusanagi/celeste-cli/issues/176)). `/plan` restricts the model to read-only tools (the status line shows `PLAN`) until it calls `submit_plan` with 1-30 ordered steps; you approve the plan in a question or send it back. Approving saves it to `.celeste/plan.json`, adds one todo item per step, ends plan mode and lets the same turn continue with every tool. Other tool calls are refused before they run, whatever the permission mode. `/plan <goal>` also sends the goal as the prompt, `/plan off` leaves, `/plan show` and `celeste plan` show the plan with each step's todo status. Plan mode is chat-only. See `docs/PLAN_MODE.md`.
* **chat:** `/fork` continues in a copy of the session (messages, tool calls, provider blocks, model and workspace), named `fork of <name>`; the original stays as it was (`/session resume <id>` goes back). Files are not copied, and file checkpoints stay with the session the chat started in: resumed in a later run, the fork has no `/undo` or `/rewind` history, and the original's `/undo` or `/rewind` can revert the fork's changes ([#176](https://github.com/whykusanagi/celeste-cli/issues/176)).
* **mcp:** `celeste mcp list` shows the MCP servers configured in your home configs and the current directory's `.mcp.json` and `.celeste/mcp.json`: each server's source, transport, enabled and trusted flags, whether a project server is approved or pending, and where it runs (every mode, the chat only once approved, overridden by another file, or nowhere while a config does not parse). Commands, arguments, URLs and env values are never printed ([#354](https://github.com/whykusanagi/celeste-cli/issues/354)).

### Changed

* **sessions:** sessions record their workspace (`workspace` in the session file); `celeste resume`, `/session list` and the `/session` picker list this project's sessions first (same git root, or the same directory outside a repository), marked "this project", then the rest, each newest first. Older session files load unchanged and are listed with the rest ([#176](https://github.com/whykusanagi/celeste-cli/issues/176)).
* **mcp:** a server's `readOnlyHint` is honoured only when the server is marked `"trusted": true` in a home-level MCP config (`~/.celeste/mcp.json`, `~/.claude/mcp.json`, `~/.cursor/mcp.json`); a project's `.mcp.json` cannot mark its own server trusted. Tool annotations (`readOnlyHint`, `destructiveHint`, `idempotentHint`, `openWorldHint`) are parsed from `tools/list` ([#176](https://github.com/whykusanagi/celeste-cli/issues/176)).
* **tools:** external tools never replace a registered one: an MCP tool whose name another server already holds, or a custom JSON tool (`~/.celeste/skills/*.json`) named like a built-in, is skipped with a warning naming both, and the rest still load ([#176](https://github.com/whykusanagi/celeste-cli/issues/176)). See MIGRATING-2.0.md.
* **tools:** `write_file`, `patch_file` and `splice_file` write atomically: a temp file in the same directory, then a rename, so a reader never sees a half-written file. An existing file keeps its mode (a 0755 script stays executable, a 0600 file stays private); a new file gets 0644 under your umask, as before. A symlink inside the workspace is edited through to its target and stays a link; a file with several hard links is rewritten in place. `write_file` with `append: true` still appends in place. The replacement is a new file owned by you: another user's ownership, extended attributes and ACLs are not kept. A file in a directory celeste cannot write to can no longer be edited, since the temp file cannot be created there ([#176](https://github.com/whykusanagi/celeste-cli/issues/176)).
* **tools:** must-read-before-edit: `patch_file`, `write_file` (overwrite or append) and `splice_file` refuse an existing file the session has not read with `read_file <path> first: ...`; creating a new file needs no read. MCP chat (`celeste` with `mode: "chat"`) forgets reads between calls, so a patch there needs a `read_file` of the file in the same call ([#176](https://github.com/whykusanagi/celeste-cli/issues/176)).
* **tools, agent:** custom JSON tools (`~/.celeste/skills/*.json` `command`), `--verify-cmd` and the agent's artifact bundle use the same shell runner as the bash tool: each command runs in its own process group, killed whole on timeout or cancel, with a bounded wait for a background process holding its output. What changes for you:
  * A custom tool's output is stdout and stderr combined (it was stdout only), capped at 64,000 bytes with an `[output truncated at 64000 bytes]` marker. Custom tools time out after 2 minutes unless the caller's deadline ends them first.
  * A command that exits but leaves a background process holding its output now fails (a custom tool call or a verify check) and the process is stopped; redirect its output (`cmd > log 2>&1 &`) to keep it running.
  * A verify check that times out or is killed reports exit code -1.
  * Custom tools and verify commands run `sh` from `PATH` instead of `/bin/sh`.
  * `git_diff.patch` and `git_status.txt` are kept whole up to 64 MiB; beyond that they end with a `# celeste: output truncated` line.

### Bug Fixes

* **anthropic:** the persona is cached on its own again. The system prompt goes out as two blocks, the persona with a cache breakpoint and the rest (user identity, sliders, project context, git, memories, date) after it, so `/user`, `/confirm` or a new day re-writes only that rest, not the whole prompt ([#309](https://github.com/whykusanagi/celeste-cli/issues/309)). Chat, `celeste message`, `celeste agent`, ACP sessions and MCP chat and content calls all send it this way; a request still carries at most four breakpoints.
* **mcp:** a project's `.mcp.json` or `.celeste/mcp.json` no longer starts its servers in the chat unasked. A cloned repository could set `"enabled": true` itself and run any command at launch. An enabled project server now starts only once you approve it: the chat shows its command, args, env names and URL and asks before starting it, and stores the approval in `~/.celeste/trusted.json` like repo hooks. Editing its command, args, env or URL asks again. Without a terminal to ask on it is skipped with a warning; approve it with `celeste hooks trust` (which now also lists and approves project MCP servers), or connect it by hand from `/mcp`. Home-level configs need no approval ([#354](https://github.com/whykusanagi/celeste-cli/issues/354)).
* **timeouts:** a local model's slow first turn no longer fails ([#328](https://github.com/whykusanagi/celeste-cli/issues/328), local smoke L1, L3, L5). The config's `timeout` is a stall timeout: a request fails only when nothing arrives for that long, and a reply that keeps streaming (reasoning included) runs to a cap of 30 minutes or 3× the timeout. A local server (`127.0.0.1`, `localhost`, a private address, a `.local` host) left at the 60 s default gets 600 s; hosted providers keep 60 s. `celeste config --set-timeout <seconds>` sets it and `config` shows it. `celeste agent` uses the profile's timeout instead of its own 90 s turn limit (`-request-timeout` still bounds a whole turn). Sending the same message again after a turn failed unanswered retries it once instead of sending it twice. Compaction summaries and `/handoff` (chat, agent and ACP) get the same stall timeout and cap instead of a fixed 3 minutes ([#345](https://github.com/whykusanagi/celeste-cli/issues/345)). On a Gemini profile, a long summary or single-message reply no longer fails with a stall after 60 s while it is still generating, and response bytes count as activity the way they do for every other provider ([#349](https://github.com/whykusanagi/celeste-cli/issues/349)). With thinking on, Gemini's thoughts no longer appear in the reply or a summary; they feed the thinking indicator instead. A local server also gets 30 minutes (or the `timeout`, when longer, up to the cap) for the first byte of a reply, separate from the stall timeout between chunks: a cold prefill on a loaded machine took ~563 s, and the first request after leaving plan mode 11.5 minutes ([#359](https://github.com/whykusanagi/celeste-cli/issues/359)).
* **agent:** `celeste agent --resume <id> --max-turns N` replaces the run's saved turn limit, so a run that stopped at `max_turns_reached` continues ([#316](https://github.com/whykusanagi/celeste-cli/issues/316)). A resumed run no longer keeps the previous attempt's error, stop reason or finish time, such as "run cancelled" next to status completed ([#317](https://github.com/whykusanagi/celeste-cli/issues/317)).
* **chat:** an approved plan's todo list is kept current ([#325](https://github.com/whykusanagi/celeste-cli/issues/325)). The approval result gives each step's todo id and the `todo` update calls, and after three tool turns with no change to the plan's todo items Celeste gets a hidden reminder naming the open steps (never in plan mode, at most twice per plan, ending when every step is done or removed, on `/clear` and on a new, resumed or cleared session).
* **compact:** on small windows (e.g. 32K) whose system prompt and tool schemas fill about half the window, compaction now decides on the history beyond that fixed prefix, so turns no longer end with "Summarizing older context…" then "Summary skipped". The threshold rises by at most the prefix, and a summary runs only when there is history besides a previous summary to replace. The tool schemas themselves are fitted to small windows too (see the next entry).
* **tools:** on a small context window the tool schemas no longer overflow it ([#310](https://github.com/whykusanagi/celeste-cli/issues/310)). At 8,192 tokens the chat's first request was ~13.9k (persona ~3.8k, 49 tools ~9.8k) and compaction started on turn 1. When the persona and every tool schema would leave less than a quarter of the window for history, the chat, `celeste agent`, MCP chat, ACP and subagents send a core set of tools (read, write, patch, list, search, bash, todo, `submit_plan`, `find_tools`) with short descriptions, then the other tools while they fit, MCP tools last, and say so once. `find_tools` activates any tool left out; the tools it activated are kept newest first while they fit (the most recent always), so repeated searches cannot overflow the window again. Each chat, ACP editor session and agent run is told once. At 32K and above the tool set is unchanged. ACP's compaction now counts the tool schemas too. The tool count in the chat's status panel, like the `/skills` browser, now shows the tools actually sent (the fitted set), not every registered tool.
* **config:** a local endpoint with no `context_limit` uses the window the server reports: Ollama's loaded model (`/api/ps`) or its `num_ctx`, llama.cpp's `/props`, LM Studio's loaded context length. Only when the server has never reported one does the 8,192 fallback apply; `celeste config` shows "reported by the server". A reported window is kept when Ollama unloads an idle model, the endpoint's API key is sent to a local server that requires one, and the chat asks the server in the background, never while drawing or handling a key.
* **plan mode:** Enter on its own, or typed text plus Enter, no longer approves a plan. The approval question now pre-selects **Keep planning**; to approve, move to **Approve and start** (↓, `j` or `2`) and press Enter. While an ask question is open, a key that is not one of its keys (a letter, `/`, a paste) moves the cursor back to the first option and shows "Typing is ignored here. Choose an option: ↑/↓ then Enter", and the next Enter only clears that hint. In the pre-release smoke, typing `/plan off` and Enter into the open question approved the plan and turned the write tools back on.
* **ask:** the `ask` tool waits 30 minutes for an answer, like `submit_plan`; it used to time out after 45 s ([#356](https://github.com/whykusanagi/celeste-cli/issues/356)). The question's modal shows when it expires. Esc or the end of the turn still cancel it.
* **todo:** creating, updating and listing todo items no longer asks for approval in the default mode ([#357](https://github.com/whykusanagi/celeste-cli/issues/357)); `delete` and `clear_done` still ask, strict mode asks for all of them, and an always-deny rule or a PreToolUse hook can still stop them. Like every read-only call, they are allowed before pattern rules are checked.
* **plan mode:** after a plan is approved, the rest of the turn logs and meters the full tool set it offers the model ([#360](https://github.com/whykusanagi/celeste-cli/issues/360)).
* **permissions:** typed text no longer answers the permission prompt. Its keys are single letters, so the `a` in `/plan off` used to allow the call once. `a` (allow once) and `A` (always allow) now only pick the answer, and Enter confirms it; any other key cancels the pick, so prose that starts with `a` or `Always` allows nothing. A printable key that is not one of the prompt's keys (or a paste) starts typing mode: later letters are ignored and a hint says so, and Enter clears the hint. `d` and `D` still deny at once unless typing mode is on (then they are text too, so prose like `hello Dan` saves no deny rule; press Enter first). Esc always denies, and Enter with nothing picked does nothing. The box is also closed now: the right border is drawn on every row, the bottom row matches the top, the box fills the terminal width, and long tool arguments wrap inside it.
* **sandbox:** the `sandbox` settings in `~/.celeste/config.json` apply when a named profile is active (`-config <name>`, or a `config.<name>.json` with `"default": true`). Before, config.json was never read then and the sandbox stayed off without a warning. A profile can still set its own `sandbox` object; each key it sets wins over config.json's, and saving the profile does not copy config.json's keys into it. A config.json that cannot be read or parsed (or whose `sandbox` has a wrong type) now stops a profile from loading, naming the file, as it already did with no profile.
* **google:** each Gemini tool call gets its own ID (`call_<tool>_<n>`, or the ID the API sends); every call of a tool used to share `call_<tool>`. Function responses are named after the function they answer. `/rewind` refuses to restore files when a rewound turn's call ID also appears earlier in the chat (older Gemini sessions, some local OpenAI-compatible servers), since it could not tell which turn's changes to undo; `/undo` still works there. A rewind with checkpoints off rewinds the chat without restoring files, and one whose oldest changes the 100-entry cap already dropped says so.
* **tools:** an image read with `read_file` is reduced to a size every provider accepts (5 MB base64, 8000 px), or refused with the limit named, and each backend re-fits or replaces an image its provider would reject instead of failing the request ([#239](https://github.com/whykusanagi/celeste-cli/issues/239)). An animated GIF goes out as its first frame where a provider takes no animation; WebP is passed through or refused (convert it to PNG or JPEG), never converted. A request with more than 20 images caps each at 2000 px on Anthropic.
* **hooks, tools (Windows):** a hook, `bash` command, custom tool or `--verify-cmd` runs in its own Job Object, so cancelling it or its timeout ends everything it started at once, and the call returns promptly. It used to run `taskkill /T` and wait for it, which could take seconds and miss a process whose parent had already exited.
* **compact:** Compaction on small context windows ([#234](https://github.com/whykusanagi/celeste-cli/issues/234)): pruning counts the system prompt and tool schemas, never elides a tool result before the model has seen it (or a body it just recalled), and its minimum saving and the summary's kept tail scale with the window. Re-reads with default line arguments supersede the earlier read. An automatic summary that finds nothing to shrink says so.
* **tools:** `write_file`, `patch_file` and `splice_file` take their checkpoint only after the call validated, right before writing, and a write that fails puts the file back. A `patch_file` whose text was not found no longer leaves a checkpoint behind, and `splice_file` in copy mode no longer checkpoints its untouched source.
* **tools:** `bash` commands run in their own process group; a timeout kills that group (on Windows, the process tree as far as `taskkill /T` can see it); a process that detached into a group or session of its own survives it, and a background process that keeps the output open can no longer hold the call more than 2 seconds after the shell exits, and is stopped then, whether the shell succeeded or failed (redirect its output to keep it running). The command denylist runs first, as before, with its patterns compiled once ([#176](https://github.com/whykusanagi/celeste-cli/issues/176)).
* **tools:** `read_file`, `patch_file` and `splice_file` read the path they checked without following a symlink swapped in after the check, so the swap fails instead of reading outside the workspace; `search` does not open symlinked files, so it no longer follows one out of the workspace. On Windows the check and the open are two steps, so a symlink swapped in between them can still be followed; a symlink's in-workspace target is still searched under its own path ([#176](https://github.com/whykusanagi/celeste-cli/issues/176)).
* **orchestrator:** goal classification matches whole words: "trust" no longer routes to code as "rust", nor "capital" as "api". This changes which lane some `/orchestrate` goals take.
* **server:** `celeste_status` no longer reports `health: ok` while every completion is failing.
* **config:** `CELESTE_API_KEY`, `CELESTE_API_ENDPOINT` and `TAROT_AUTH_TOKEN` are read now (environment > config file) for `chat`, `message`, `agent`, skill execution and `serve`; they were documented but ignored. They are never written to a config file.
* **bash:** the bash tool reads a command as the shell does before it runs it, and refuses a recursive `rm` of the root, a home directory (`~`, `~user`, `$HOME`) or a top-level directory even without `-f` (a tool call has no TTY, so `rm -r` never prompts), and a recursive forced `rm` of any absolute path. Spellings that used to slip past are refused too: split or reordered flags, GNU prefixes (`--rec --for`), quoting, `\rm` and `/bin/rm`, `$IFS`, `$'...'`, line continuations, and commands nested in `sh -c`/`bash -lc`, `eval`, `env -S`, `script -c`, `ssh`, `$( )` and backticks. Build output (`./build`, `node_modules`) and paths below home (`~/project/build`) are still allowed.
* **steering:** the `destructive-bash` stream rule and the watchdog's "unsafe" check read commands with the same shell reader and see every `rm` the bash tool refuses (before, `bash -lc 'rm -rf ~'` or `rm -rf${IFS}/` was blocked but never flagged).
* **llm:** `gpt-5` models get the thinking level as `reasoning_effort` on Chat Completions too, including when an endpoint without the Responses API falls back to it; only the o-series did before.
* **llm:** `google_credentials_file` and `google_use_adc` reach every client path (chat, `/endpoint`, `/set-model`, `celeste message`, MCP chat); only agent runs and the summarizer passed them before.
* **chat:** `simulate_typing` and `typing_speed` take effect: `false` shows replies at once, `typing_speed` is characters per second (1-1000). The default is now 60 (was 40) so the default pace is unchanged; a `typing_speed` of 40 or 25 (the values celeste once wrote as defaults, never read until now) is removed from the file at startup, with a note, so those configs keep today's pace. Any other value is kept.
* **chat:** `/session merge|resume|new|...` run instead of opening the picker ([#235](https://github.com/whykusanagi/celeste-cli/issues/235)); `/session new <name>` keeps its name; `/context compact` saves the session; session files are written 0600.
* **providers:** Venice's default `venice-uncensored` is no longer served and caused routing errors; its offline fallback is now `venice-uncensored-1-2`.
* **chat:** switching endpoints, `/set-model` and the model picker no longer wait on a network request inside the UI loop.
* **chat:** when a tool result is too large and is saved to disk, the model now sees the "full output saved to" notice, which names the id `recall_tool_result` takes to page through the full output. A second, per-request 64 KiB trim used to cut the notice off ([#211](https://github.com/whykusanagi/celeste-cli/issues/211)).
* **loop:** a tool result is capped once, at 128 KiB, when it is recorded, including when the spill file cannot be written. Requests and their retries send the conversation unchanged; nothing trims tool results per request any more. Results between 64 and 128 KiB are now sent whole, so with a small context window the first request after one can overflow it, and the compactor then prunes that result.
* **chat:** `/set-model` and a switch between two OpenAI-compatible endpoints now change the model and URL the next request goes to, keeping the system prompt and thinking level. Before, the client kept its previous backend until the switch was to a different kind of provider.
* **providers:** when OpenAI refuses the reasoning items celeste replays (for example after the API key changed) and the request sent without them fails too, the items are still dropped from the history, so the next turn does not fail the same way.
* **providers:** local reasoning models (llama.cpp, LM Studio, vLLM, Ollama, DeepSeek): a `<think>` block a server puts at the start of a reply is kept out of the reply and of the history sent back; `<think>` tags later in a reply are left as written. Reasoning in `reasoning` or `reasoning_content` deltas, or in a leading `<think>` block, shows as a thinking count in the status bar while the model thinks. A connection drop while the model is still only thinking is retried.

### Hooks

* UserPromptSubmit now also checks the goal of `celeste agent`, MCP
  `mode: "agent"` and `/agent` in the chat, once, before any model call.
  A `deny` (or a failed hook) stops the run with
  `goal blocked by a UserPromptSubmit hook: <reason>`; `additionalContext`
  is sent after the goal, as in the chat. Subagents and `/orchestrate`
  lanes are not checked: their goals are written by the model.
* MCP chat runs UserPromptSubmit through the same loop as the chat UI. The
  error result for a blocked prompt is unchanged.

### Chat

* Subagents and `/agent` run inside the chat's environment: they reuse its
  MCP servers, hooks and code graph instead of starting their own, sharing
  only the global MCP servers the chat is running (`~/.celeste/mcp.json`
  and the other home configs), never a repository's. Editing `hooks.json`
  or a global MCP config file on disk applies to them from the next chat
  session; a server connected or disconnected through the `/mcp` panel
  reaches the next subagent right away, and `permissions.json` and custom
  skills reload for every subagent regardless.
* Subagents share the chat's MCP connections: disconnecting a server in
  `/mcp` affects subagents still using it until it is connected again (they
  then use the new connection), and calls to one MCP server run one at a
  time. A call waiting for a busy server gives up when its run is cancelled.
* A permission or question prompt from a turn, `/agent` or `/orchestrate`
  run that has already ended is answered (denied or cancelled) instead of
  appearing after the fact, and a prompt from an earlier run never shows up
  as part of the next one.
* The spinner and the typing keep a steady speed in long tool turns (each
  tool step used to add another animation timer).
* `/config <profile>` (and `/endpoint <profile>`) now uses the profile's
  provider, model and tool support; before, it kept the previous provider's
  tool setting.

### Agent runs

* `celeste agent` runs on `agent_model` when one is set, as subagents and
  MCP agent mode already did.

## [1.16.0](https://github.com/whykusanagi/celeste-cli/compare/v1.15.1...v1.16.0) (2026-08-19)


### Features

* **providers:** recognise local OpenAI-compatible endpoints as tool-capable ([#142](https://github.com/whykusanagi/celeste-cli/issues/142)) ([045b524](https://github.com/whykusanagi/celeste-cli/commit/045b5241b2eea8f99737974351a03ed0fe151e98))


### Bug Fixes

* **config:** don't trust a coincidental model-name match on a local endpoint ([#150](https://github.com/whykusanagi/celeste-cli/issues/150)) ([2a6be1f](https://github.com/whykusanagi/celeste-cli/commit/2a6be1f5d7f8ead1a224af0dda91dd04da3eb2b0))
* **config:** stop deleting context_limit for models we cannot size ([#146](https://github.com/whykusanagi/celeste-cli/issues/146)) ([0162e2b](https://github.com/whykusanagi/celeste-cli/commit/0162e2bf3cc89aaa1a9d5c803eda3c72642136f0))

## [1.15.1](https://github.com/whykusanagi/celeste-cli/compare/v1.15.0...v1.15.1) (2026-08-18)


### Bug Fixes

* **agent:** stop headless agent runs silently no-opping on every mutating tool ([#134](https://github.com/whykusanagi/celeste-cli/issues/134)) ([65b5281](https://github.com/whykusanagi/celeste-cli/commit/65b5281f124a616a6128881df84b87d5ba9660d9))
* **ci:** build on Go 1.26.6 — 1.26.5 has 7 called stdlib vulnerabilities ([#138](https://github.com/whykusanagi/celeste-cli/issues/138)) ([712e83a](https://github.com/whykusanagi/celeste-cli/commit/712e83af62c202b64bf5a7a84c7c12ba7cbbf288))
* **deps:** bump github.com/anthropics/anthropic-sdk-go ([#136](https://github.com/whykusanagi/celeste-cli/issues/136)) ([7a780a7](https://github.com/whykusanagi/celeste-cli/commit/7a780a7533acc8cfc14a7c1cdfa0bfd3c533e15d))
* **deps:** bump golang.org/x/text in the golang-x group ([#135](https://github.com/whykusanagi/celeste-cli/issues/135)) ([0b3c517](https://github.com/whykusanagi/celeste-cli/commit/0b3c5173d8547812c1cb251e37d649bdb43db61b))
* **deps:** bump google.golang.org/genai from 1.66.0 to 1.68.0 ([#137](https://github.com/whykusanagi/celeste-cli/issues/137)) ([6a49910](https://github.com/whykusanagi/celeste-cli/commit/6a49910de4811155f315a3138ec3032495544b34))

## [1.15.0](https://github.com/whykusanagi/celeste-cli/compare/v1.14.0...v1.15.0) (2026-08-11)


### Features

* **agent:** offload planning to fugu when the model orchestrates server-side ([#119](https://github.com/whykusanagi/celeste-cli/issues/119)) ([64d354a](https://github.com/whykusanagi/celeste-cli/commit/64d354af34d6508a14ee5e047ed64aafe81239d9))
* **build:** stamp local builds with the git commit, report it in celeste_status ([#124](https://github.com/whykusanagi/celeste-cli/issues/124)) ([438d169](https://github.com/whykusanagi/celeste-cli/commit/438d16914a18768cc34c132ee50c25e594e7f977))
* **server:** hand back a run handle when an MCP agent run exceeds 60s ([#129](https://github.com/whykusanagi/celeste-cli/issues/129)) ([108f5ff](https://github.com/whykusanagi/celeste-cli/commit/108f5ff5044482e41306017a3aced9d0868a8e72))


### Bug Fixes

* **agent:** actionable timeout message + conductor-aware deadlines ([#113](https://github.com/whykusanagi/celeste-cli/issues/113)) ([#125](https://github.com/whykusanagi/celeste-cli/issues/125)) ([09a3fa2](https://github.com/whykusanagi/celeste-cli/commit/09a3fa203eb7ecbaf591822858b5461b5ead7f65))
* **build:** make the smoke release gate test the binary it just built ([#127](https://github.com/whykusanagi/celeste-cli/issues/127)) ([bd324b4](https://github.com/whykusanagi/celeste-cli/commit/bd324b4849a522963c3945255aaf8720d22fa702))
* **commands:** stop parsing pasted file paths as slash commands ([#126](https://github.com/whykusanagi/celeste-cli/issues/126)) ([f87d49c](https://github.com/whykusanagi/celeste-cli/commit/f87d49cf5dc2a1aabfa0c0251934694ac2d6c042))
* **deps:** bump github.com/anthropics/anthropic-sdk-go ([#112](https://github.com/whykusanagi/celeste-cli/issues/112)) ([90b2ec9](https://github.com/whykusanagi/celeste-cli/commit/90b2ec97df9260409f9ac881f5c1fcc881ba9677))
* **deps:** bump github.com/ethereum/go-ethereum from 1.17.4 to 1.17.5 ([#115](https://github.com/whykusanagi/celeste-cli/issues/115)) ([71afb21](https://github.com/whykusanagi/celeste-cli/commit/71afb21f2385abc4445499ffb0187ee2bb76d4fe))
* **deps:** bump github.com/sashabaranov/go-openai from 1.41.2 to 1.42.0 ([#116](https://github.com/whykusanagi/celeste-cli/issues/116)) ([6569c32](https://github.com/whykusanagi/celeste-cli/commit/6569c321261f0ef7c7be46681290b35aaabc9710))
* **deps:** bump google.golang.org/genai from 1.63.0 to 1.66.0 ([#118](https://github.com/whykusanagi/celeste-cli/issues/118)) ([c099f04](https://github.com/whykusanagi/celeste-cli/commit/c099f04bdce0d2ac92963d6f6273cf5812416082))
* **deps:** bump google.golang.org/grpc to v1.82.1 (GO-2026-6061) ([#120](https://github.com/whykusanagi/celeste-cli/issues/120)) ([aa23710](https://github.com/whykusanagi/celeste-cli/commit/aa23710ec5539f428de4d3aab81628bc7d837448))
* **deps:** bump modernc.org/sqlite from 1.53.0 to 1.56.0 ([#117](https://github.com/whykusanagi/celeste-cli/issues/117)) ([c27c5af](https://github.com/whykusanagi/celeste-cli/commit/c27c5afba2d315d7a8e12820dd75feb4619efd17))
* **google:** carry thought_signature across the stream-event boundary ([#131](https://github.com/whykusanagi/celeste-cli/issues/131)) ([f4ab737](https://github.com/whykusanagi/celeste-cli/commit/f4ab737455a8cb96fdc48a97b87fe05935eb4c25))
* **google:** echo Gemini 3.x thought_signature so agent mode survives turn 2 ([#128](https://github.com/whykusanagi/celeste-cli/issues/128)) ([1f97bae](https://github.com/whykusanagi/celeste-cli/commit/1f97bae8838688bb09103ce369d90b058014655d))
* **llm:** stop retrying a self-inflicted request deadline ([#113](https://github.com/whykusanagi/celeste-cli/issues/113)) ([#122](https://github.com/whykusanagi/celeste-cli/issues/122)) ([1e21914](https://github.com/whykusanagi/celeste-cli/commit/1e219142de4f73e2eca58935e636594e96c7be90))
* **providers:** unpin Google API version from v1 and default to the -latest alias ([#123](https://github.com/whykusanagi/celeste-cli/issues/123)) ([4078dec](https://github.com/whykusanagi/celeste-cli/commit/4078dec00c38a95860dc12ed1f5ddb15cf34ba9b))

## [1.14.0](https://github.com/whykusanagi/celeste-cli/compare/v1.13.0...v1.14.0) (2026-07-15)

The large-file reliability release. Reading one big or minified file could poison
a whole session. This release closes that across the read path, the retry path,
and the write path, and makes the skills browser usable at scale.

### Skills browser

* Type-ahead search, pagination, and a full-description line for the selected
  skill. The filter ranks with the same BM25 index that powers tool discovery,
  and falls back to substring matching so partial words still hit. ([#102](https://github.com/whykusanagi/celeste-cli/issues/102))

### Large-file byte path

Three bugs compounded so a single oversized tool payload corrupted the session:

* **read_file** bounds the returned bytes on a line boundary (48 KiB default) and
  reports `total_bytes`, `returned_bytes`, and `next_offset_line` with a paging
  hint. A minified single line no longer returns the whole file for the 128 KiB
  history cap to mid-byte-truncate into garbage. ([#103](https://github.com/whykusanagi/celeste-cli/issues/103))
* **Retry trim.** Each LLM retry gets its own deadline instead of reusing the
  first attempt's expired one, so a timeout retry runs instead of failing
  instantly. Before each retry the trim shrinks the largest tool result
  copy-on-write, so an oversized payload never replays unchanged. ([#103](https://github.com/whykusanagi/celeste-cli/issues/103))
* **splice_file** moves a region between files by anchors or line ranges. The
  bytes copy on disk and the model never regenerates them, so relocating a large
  block cannot corrupt it in transit. `patch_file` refuses a literal over 16 KiB
  and points to `splice_file`. ([#103](https://github.com/whykusanagi/celeste-cli/issues/103))
* A capped read_file result plus its JSON wrapper tripped the retry trim and
  mangled its own metadata; the two byte budgets no longer overlap. ([#105](https://github.com/whykusanagi/celeste-cli/issues/105))

### CI

* Release binaries build with Go 1.26.5, which carries the crypto/tls fix for
  GO-2026-5856. ([#105](https://github.com/whykusanagi/celeste-cli/issues/105))

## [1.13.0](https://github.com/whykusanagi/celeste-cli/compare/v1.12.0...v1.13.0) (2026-07-11)

The MCP-connectivity release. Celeste installs itself into other MCP clients,
consumes the servers they already have, speaks the modern Streamable-HTTP
transport, and gains a set of TUI features around all of it.

### Features

#### MCP connectivity

* **`celeste mcp install`** — a self-locating installer. It resolves celeste's
  own absolute path and writes it into Claude Desktop, Claude Code, and Cursor
  MCP configs, so GUI clients that don't inherit your shell PATH can still launch
  it. Merges without touching your other servers, backs up to `<file>.bak`,
  refuses to write through a symlink, and supports `--dry-run` and `--client`
  (`claude-desktop` / `claude-code` / `cursor` / `celeste-cli` / `all`; `codex`
  prints a TOML block to paste). ([#98](https://github.com/whykusanagi/celeste-cli/pull/98))
* **Foreign / project config discovery** — celeste merges MCP servers you've
  already defined for Claude Code or Cursor (`~/.claude/mcp.json`,
  `~/.cursor/mcp.json`, project `.mcp.json`), gated behind an opt-in
  `"enabled": true` so nothing connects until you ask.
* **Runtime `/mcp` panel** — connect, disconnect, reconnect, or toggle a server
  without restarting (`c` / `d` / `r` / `space`). Actions run async, so an OAuth
  handshake never freezes the UI.
* **Streamable-HTTP transport** — set `"transport": "http"` to reach modern MCP
  servers over POST + SSE with the negotiated `MCP-Protocol-Version` header, no
  stdio bridge.
* **Protocol-version negotiation** — celeste accepts any supported MCP revision
  it's offered (2024-11-05 through 2025-06-18) instead of demanding an exact
  match.
* **`find_tools` dynamic discovery** — a BM25-ranked tool that surfaces hidden or
  external tools on demand. It turns on once the registered tool list crosses 40,
  keeping the prompt lean as MCP servers pile up.

#### TUI

* **`ask` tool** — the model can ask you a multiple-choice question mid-turn. An
  interactive picker (single- or multi-select) blocks the turn until you answer,
  and degrades to a clear error in headless / one-shot contexts.
* **Segmented status line** — git branch, dirty count, ahead/behind, project,
  model, effort, permission mode, session, and skill count in one bar; it narrows
  its segments below 80 columns.
* **Contextual key hints** per view and **boxed tool-call cards** (running / done
  / failed with elapsed time and a detail line), plus a consolidated bottom
  layout that hands the reclaimed rows back to the chat.

### Bug Fixes

* **deps:** bump github.com/anthropics/anthropic-sdk-go ([#94](https://github.com/whykusanagi/celeste-cli/pull/94))
* **deps:** bump golang.org/x/text ([#95](https://github.com/whykusanagi/celeste-cli/pull/95))
* **deps:** bump google.golang.org/genai from 1.62.0 to 1.63.0 ([#96](https://github.com/whykusanagi/celeste-cli/pull/96))
* **deps:** bump github.com/ipfs/go-cid from 0.6.1 to 0.6.2 ([#97](https://github.com/whykusanagi/celeste-cli/pull/97))
* **deps:** bump goldmark to v1.7.17 (GO-2026-5320) and CI Go to 1.26.5 (GO-2026-5856)

### CI / internal

* Run the cross-platform test matrix post-merge instead of on every PR ([#93](https://github.com/whykusanagi/celeste-cli/pull/93))
* Visual TUI + live-model CLI smoke tests wired into the release gate (`make smoke`) ([#100](https://github.com/whykusanagi/celeste-cli/pull/100))

## [1.12.0](https://github.com/whykusanagi/celeste-cli/compare/v1.11.2...v1.12.0) (2026-06-28)


### Features

* **config:** resolve default profile from a file flag, not hardcoded provider ([#92](https://github.com/whykusanagi/celeste-cli/issues/92)) ([4c9ca8f](https://github.com/whykusanagi/celeste-cli/commit/4c9ca8fd1a7ee9571cef3ee278cb86bd6c841eb6))


### Bug Fixes

* **deps:** bump github.com/anthropics/anthropic-sdk-go ([#88](https://github.com/whykusanagi/celeste-cli/issues/88)) ([a38dc35](https://github.com/whykusanagi/celeste-cli/commit/a38dc356205552c397102d6b32e0b0f36392d102))
* **deps:** bump github.com/tree-sitter/tree-sitter-php ([#89](https://github.com/whykusanagi/celeste-cli/issues/89)) ([c65921d](https://github.com/whykusanagi/celeste-cli/commit/c65921d59d20998b7d1ceaadf06ea2cae53710d7))
* **deps:** bump google.golang.org/genai from 1.39.0 to 1.62.0 ([#90](https://github.com/whykusanagi/celeste-cli/issues/90)) ([0302c87](https://github.com/whykusanagi/celeste-cli/commit/0302c87b1cee26bdd8e412d230c6d411e5e4f1ea))

## [1.11.2](https://github.com/whykusanagi/celeste-cli/compare/v1.11.1...v1.11.2) (2026-06-23)


### Bug Fixes

* honor -config in read commands; /model lists real provider models in TUI ([#82](https://github.com/whykusanagi/celeste-cli/issues/82)) ([cb7e4df](https://github.com/whykusanagi/celeste-cli/commit/cb7e4df9fa6ebc3467aea7f9666900f2a1432b3b))

## [1.11.1](https://github.com/whykusanagi/celeste-cli/compare/v1.11.0...v1.11.1) (2026-06-22)


### Bug Fixes

* **deps:** bump github.com/charmbracelet/bubbles from 0.21.0 to 1.0.0 ([#69](https://github.com/whykusanagi/celeste-cli/issues/69)) ([ae00976](https://github.com/whykusanagi/celeste-cli/commit/ae009764174b8c35c95f9766b791401f3bd5903b))
* **deps:** bump github.com/ethereum/go-ethereum from 1.17.0 to 1.17.4 ([#73](https://github.com/whykusanagi/celeste-cli/issues/73)) ([46a7ab7](https://github.com/whykusanagi/celeste-cli/commit/46a7ab7b9c5061bd9cc7c5a1f4e63904d8f9a7c7))
* **deps:** bump github.com/ipfs/go-cid from 0.6.0 to 0.6.1 ([#63](https://github.com/whykusanagi/celeste-cli/issues/63)) ([df46df3](https://github.com/whykusanagi/celeste-cli/commit/df46df3e5c1ae1621d219032c396f37d53d3e0f7))
* **deps:** bump github.com/tree-sitter/tree-sitter-python ([#67](https://github.com/whykusanagi/celeste-cli/issues/67)) ([3eb4644](https://github.com/whykusanagi/celeste-cli/commit/3eb4644733546439368ba84317317a1d3d9b40b2))
* **deps:** bump github.com/tree-sitter/tree-sitter-rust ([#72](https://github.com/whykusanagi/celeste-cli/issues/72)) ([6515c94](https://github.com/whykusanagi/celeste-cli/commit/6515c942dc9c575f345a081d4a26498b6ed73508))
* **deps:** bump modernc.org/sqlite from 1.48.1 to 1.53.0 ([#65](https://github.com/whykusanagi/celeste-cli/issues/65)) ([6505021](https://github.com/whykusanagi/celeste-cli/commit/65050215cc2596d4479062f11c51fe25ff18b813))
* **deps:** upgrade anthropic-sdk-go to v1.51.1; pin govulncheck past its generics panic ([#80](https://github.com/whykusanagi/celeste-cli/issues/80)) ([57c907e](https://github.com/whykusanagi/celeste-cli/commit/57c907e4dfa498c30b1ce8a9fd80633cffc9a89d))
* make agent RunID unique to stop checkpoint file collisions ([#78](https://github.com/whykusanagi/celeste-cli/issues/78)) ([57b27bd](https://github.com/whykusanagi/celeste-cli/commit/57b27bd436440d47dd15f4c0897673c6d7f1a758))
* **test:** deterministic MinHash seeds so codegraph ranking test isn't flaky ([#76](https://github.com/whykusanagi/celeste-cli/issues/76)) ([ef76768](https://github.com/whykusanagi/celeste-cli/commit/ef76768c9f947a8eadf9d70d48a773cbfd982a8d))

## [1.11.0](https://github.com/whykusanagi/celeste-cli/compare/v1.10.0...v1.11.0) (2026-06-22)


### Features

* add Sakana AI (Fugu) provider ([7de3e35](https://github.com/whykusanagi/celeste-cli/commit/7de3e35520d0cbe30508b6cb6c5c6a96edc77e43))
* add Sakana AI (Fugu) provider ([a859910](https://github.com/whykusanagi/celeste-cli/commit/a85991005aae1bd10ccb04d47eb76ee4a1de5a87))


### Bug Fixes

* address Fugu pre-release review ([7b9c742](https://github.com/whykusanagi/celeste-cli/commit/7b9c7423863f8e6e10688f0717253f031193d83e))
* cap tool calls before recording them so declared calls match results ([c4d5373](https://github.com/whykusanagi/celeste-cli/commit/c4d537376e4ef2cdba0ae193ca149d1936ac9730))
* **ci:** version tests read serverVersion constant; stop double-running CI per branch ([#71](https://github.com/whykusanagi/celeste-cli/issues/71)) ([ab8c2a4](https://github.com/whykusanagi/celeste-cli/commit/ab8c2a4038a071436d0cbde058a17215b4fdce9b))
* **deps:** bump the golang-x group across 1 directory with 2 updates ([#74](https://github.com/whykusanagi/celeste-cli/issues/74)) ([3a1475c](https://github.com/whykusanagi/celeste-cli/commit/3a1475c887072e30a474e4f31b6bb686bf3461be))
* make 'config --set-*' respect the -config &lt;name&gt; profile ([55dcfda](https://github.com/whykusanagi/celeste-cli/commit/55dcfdac5ab8603c34c1049a0dce104ef8aa7364))
* **security:** align GPG release verification with celeste-ops ([ed99a7f](https://github.com/whykusanagi/celeste-cli/commit/ed99a7f9b16fcbd018126eccf04dc28ee1961573))
* write TTS audio to the workspace, not the process cwd ([fa519a4](https://github.com/whykusanagi/celeste-cli/commit/fa519a48202ccaa23d01b43daf3106b879cccc77))

## [Unreleased]

### Hooks

- Lifecycle hooks (`hooks.json` and grimoire `## Hooks` sections) now load with
  explicit trust: your own `~/.celeste` files run automatically, but a
  project's `.celeste/hooks.json` or grimoire hooks need one-time approval
  (`celeste hooks list`, `celeste hooks trust`). Non-interactive runs never
  auto-approve; untrusted or changed hooks are skipped with a warning.
- Hooks now run **before** the permission prompt, not after, and each hook
  runs in its own source's project root rather than always the workspace.
- v1 (grimoire) hooks fail closed if their `CELESTE_TOOL_*` input had to be
  omitted for being oversized or containing NUL, and now also fail closed if
  they print more than 1 MiB to stdout, even at exit 0 — previously
  unbounded.
- See `docs/HOOKS.md` for the full hooks.json format, the v2 JSON protocol,
  and v1 migration notes.
- Agent runs now load hooks: `celeste agent`, the MCP server's `celeste` tool
  in `mode: "agent"`, `/agent` in the chat, subagents and `/orchestrate`
  lanes. Your global hooks run; untrusted repo hooks are skipped with a
  warning, because agent runs never prompt for trust. Approve them ahead of
  time with `celeste hooks trust`.
- A Stop hook's `deny` is now acted on in the chat, `celeste agent`, MCP
  agent mode and MCP chat: the run continues once with the hook's `reason`
  as the next instruction, and only while turns remain. Subagents,
  `/orchestrate` lanes and `/agent` in the chat skip SessionStart and Stop.
- A subagent started with `spawn_agent` fires SubagentStop when it finishes
  (`agent_id` is the ID `spawn_agent` returned, kept when it is resumed). A
  `deny` continues it once, like Stop. `/orchestrate` lanes and `/agent`
  fire neither.
- MCP chat (`celeste` tool, `mode: "chat"`) now loads hooks like agent
  runs: your global hooks run, and untrusted repo hooks are skipped with a
  warning returned in the result. Each call fires SessionStart (`startup`),
  and UserPromptSubmit sees the call's `prompt`: a `deny` refuses the call
  with an error result, and `additionalContext` is sent with the prompt.
- Agent compaction summaries fire PreCompact (trigger `auto`) and
  PostCompact (with the summary text). A PreCompact `deny` skips the summary
  and is reported as a warning.

### Agent runs

- Agent runs (including subagents, `/orchestrate` lanes, `/agent` and MCP
  agent mode) now stop on the same guards as MCP chat: the same tool calls
  3 turns in a row, the same tool results 6 turns in a row, or invalid tool
  arguments 3 turns in a row. The counters start over whenever the model
  replies without calling a tool, and when a run is resumed.
- Tools with their own timeout keep it in agent runs instead of the 45s
  default or `--tool-timeout`: `bash` 5m, `spawn_agent` 10m,
  `generate_speech` 5m, `audio_render` 2m.
- Concurrency-safe tool calls in one turn (reads, searches) run in parallel;
  results keep the order the model asked for them.
- Tool results over 128 KiB are saved to a private file; the model gets the
  start and end of the output and the file's path. Spilled results (in the
  chat UI too) are now written as 0600 files in 0700 directories.
- Agent runs now load your custom skills, MCP servers, memories and the
  code-graph summary. Past 40 tools, tool discovery turns on and MCP tools
  are hidden until the model finds them with `find_tools`. There are 38
  built-in tools, so three or more custom skills and MCP tools together
  (for example one MCP server with three tools) switch it on.
- Repo MCP servers (a workspace's `.mcp.json` or `.celeste/mcp.json`) don't
  start in non-interactive runs: `celeste agent`, MCP agent mode, `/agent`
  in the chat, subagents and `/orchestrate` lanes skip them with a warning.
  Move a server to a global config (`~/.celeste/mcp.json`,
  `~/.claude/mcp.json` or `~/.cursor/mcp.json`) to use it there. The chat
  itself still loads them.
- A cancelled or timed-out call to a tool from an MCP server now returns
  promptly, and a server that never answers no longer blocks that server's
  other tools for the rest of the session (#221).
- MCP agent mode now returns setup and hook warnings in the tool result,
  under a `## Warnings` heading.
- Subagents share one set of MCP servers, hooks and code graph per chat
  session instead of each starting its own (up to 15 s per subagent). The
  shared setup is rebuilt for the next subagent after you change
  permissions, hooks, hook trust, MCP configs or the home or workspace
  grimoire, or add or remove a skill; subagents already running finish on
  the old one. Each subagent also reloads your permissions, captures the
  git state, and brings the code graph up to date with files changed
  since the last update, waiting at most 2 s for it. The code-graph summary
  in its system prompt is from when the shared setup was built. A subagent
  in its own worktree still loads hooks, project context and the code
  graph for that worktree. Subagent setup and hook warnings now show in
  the chat, and subagent hooks get the chat session's `session_id`.
- `/orchestrate` asks before running tools your permission policy doesn't
  allow outright (writes, edits, shell commands), through the same prompt
  as the chat, shown over the orchestrator view. Before, every such call
  was silently denied. An orchestrator run without a prompt still denies
  them, and now says so.
- Esc and Ctrl+C now dismiss the permission prompt as a denial (and cancel
  the `ask` tool's question), so a run waiting on it, such as an
  `/orchestrate` lane or `/agent`, carries on. Esc on an empty input and
  Ctrl+C now also cancel a running `/orchestrate`, as they do `/agent`.
- `/orchestrate` lanes (primary, reviewer and each debate round) share one
  set of MCP servers, hooks and code graph per run instead of each starting
  its own. Their hooks see one `session_id` for the run,
  `orchestrator-<n>`, instead of one per lane.
- When the reviewer debate in `/orchestrate` fails, the run now shows
  "debate skipped" as a notice and carries on to the primary agent's
  result. Before, the chat treated it as the end of the run and never
  showed the result.
- In an `/orchestrate` lane configured with its own `primary_base_url` or
  `primary_api_key`, the primary's answers to the reviewer (the debate's
  defense turns) now go to that endpoint too. Before, they went to the main
  config's provider.
- "Always allow" and "Always deny" in the permission prompt of `/agent` and
  `/orchestrate` now save to `~/.celeste/permissions.json`, as in the chat:
  later lanes of the same run and later runs don't ask again. Before, the
  answer only applied to that one lane or run. Runs without a prompt
  (`celeste agent`, MCP, subagents) never write the file. A save now adds
  the rule to the file as it is on disk, so rules saved from the chat and
  from these runs no longer overwrite each other. A `permissions.json` that
  can't be read or parsed is never rewritten: the save fails with a warning
  and the file keeps your rules. Saves replace the file atomically (temp
  file and rename, keeping its mode; a new file is 0600), so a lane or
  another celeste starting mid-save never reads a half-written file.

### Chat

- The chat runs its tool calls on the same loop as agent runs and MCP chat.
  It no longer executes tools inside the UI's update loop, so the screen
  stays responsive while tools run.
- The default tool timeout in the chat is 45s (was 30s). Tools with their
  own timeout keep it.
- The chat stops a turn when the tools return the same results 6 turns in a
  row, as agent runs do. The identical-call guard (3) is unchanged. The turn
  cap (`max_tool_iterations`, default 25) now counts every model turn.
  When a turn stops on the cap or a guard, the status bar says "Stopped",
  and the cap's notice gives the number of model turns the loop ran.
- A failed tool call reaches the model as a JSON error with `tool` and
  `message` fields, the same shape agent runs use. The tool's card in the
  chat still shows `Error: <message>`.
- Esc cancels the running tools, not only the ones still queued. Every call
  still gets a result, so the conversation stays valid.
- Which tools run in parallel is decided by each tool, including MCP and
  custom tools, instead of a fixed list. The status bar shows
  `⚡ Executing: <name>` as each call starts, instead of
  `⚡ Executing N tools in parallel`.
- A message typed during a turn (a steer) is checked by your
  UserPromptSubmit hooks when it joins the conversation at the next tool
  step. A blocked steer is reported and dropped; the model still finishes
  the step it was on.
- While a Stop hook decides whether a turn may end, the status bar says
  "Running Stop hook…" instead of "Ready". A message typed then waits for
  the turn: it is sent when the turn ends, or joins the conversation if
  the hook asks the chat to continue.
- When pruning old tool results is not enough, the automatic summary starts
  when the turn ends instead of in the middle of it.
- Costs now include the tokens of tool-call turns.
- Tool results over 128 KiB are saved as `<call-id>-<n>.txt`, numbered
  across the whole chat session, so a repeated call ID no longer overwrites
  an earlier result.
- Whether tools are offered at all (off in NSFW mode and for a provider
  without function calling) is decided when you send a message and holds
  for the whole turn. Turning NSFW mode on or off from a tool changes it
  from your next message, not partway through the current one.
- Warnings and notices from starting the chat (an MCP server that fails to
  start, an invalid `permissions.json`, a git snapshot or code-graph update
  that timed out) are shown in the chat as well as on stderr.
- A permission prompt or `ask` question left open when its turn, `/agent`
  run or `/orchestrate` run ends is closed (as a denial or a cancel)
  instead of staying on screen and taking your keys.

### MCP chat (`celeste` tool, `mode: "chat"`)

- Runs on the same tool loop and setup as agent runs. Tool names, arguments
  and the response shape are unchanged, as are the 25-turn cap, the
  identical-call and no-progress guards (and their texts), and the stripping
  of unbacked "Audio saved" and "subagent spawned" claims.
- Now loads your custom skills, global MCP servers, the code-graph tools,
  project memories, the code-graph summary and the git state. Repo MCP
  configs are skipped with a warning, as in agent runs.
- The server sets this up once per workspace and reuses it across calls
  (up to 4 workspaces). It rebuilds it on the next call after you change
  permissions, hooks, hook trust, MCP configs or the home or workspace
  grimoire, or add or remove a skill. Anything else (an edit inside a skill,
  a grimoire in a parent directory) applies within 10 minutes, when it is
  rebuilt anyway. Each call still starts with a fresh conversation.
- Also starts the MCP servers in your home-level configs:
  `~/.celeste/mcp.json`, and now `~/.claude/mcp.json` and
  `~/.cursor/mcp.json`. If one of them lists `celeste serve`, each
  workspace's setup runs a child Celeste server (at most 4 at a time).
- Global tool hooks run on every tool call. Untrusted repo hooks are skipped
  with a warning. Warnings are appended to the result under a `## Warnings`
  heading, only when there are any; setup warnings appear on the call that
  set the workspace up.
- Concurrency-safe tool calls in one turn run in parallel. Every tool has a
  timeout: 45s, or its own (`bash` 5m, `generate_speech` 5m). Results over
  128 KiB are saved to a private file.
- A failed tool call reaches the model as
  `{"error": true, "message": …, "tool": …}` instead of the raw text.
- Within a call, `write_file`, `patch_file` and `splice_file` refuse to edit
  a file that changed since Celeste last read or wrote it ("read it again
  before editing"), and snapshot a file before writing it.
- Undo snapshots keep the 100 most recent: past that the oldest one (and
  its backup file) is dropped. Before, a workspace (or a long chat session)
  that reached 100 snapshots failed every later edit with "snapshot failed".
- Prunes old tool results when a call nears the model's context window,
  like agent runs (context_limit is honoured). Pruned results can be
  restored with `recall_tool_result`. MCP chat never writes a summary.

### Fixed
- MCP `celeste_code_search` honours `top_k` (capped at 100); it was ignored and every search returned up to 10 results (#209).
- `celeste providers` marks Venice's tool support as per model and Vertex's default model as unverified; `providers info local` shows a working `-config local` example (#151).
- A single unknown lowercase word (`celeste models`) now errors with a suggestion instead of being sent to the model (#151).
- The chat now offers tools on Venice exactly when the selected model supports them (checked against the live Venice catalog), instead of disabling tools for every Venice model (#151).
- `celeste chat`, single-message mode and `celeste agent` no longer refuse to start on an empty `api_key` when the config uses Google ADC (`google_use_adc`), a service-account file, or a keyless local endpoint; all three also now refuse a stale ADC/service-account flag left over from an earlier Vertex/Gemini setup once `--set-url` has repointed the profile at a different provider (#151).

### Added
- MCP `celeste_status` reports `grimoire` (loaded, sources), `project` (indexed, file/symbol/edge counts) and `session_cost` (tokens, USD, unpriced requests) (#210).

### Removed
- The `classic`/`claw` runtime mode: the `-mode` flag, `config --set-mode`, the `runtime_mode` key, and the `celeste-classic`/`celeste-claw` templates. Old configs migrate on load (#144). See MIGRATING-2.0.md.

### Changed
- `claw_max_tool_iterations` is now `max_tool_iterations` (`-max-tool-iterations`, `--set-max-tool-iterations`); the old key and flags still work with a warning (#144).

## [1.10.0] - 2026-06-03

### Added

- **Model router + capability guardrail (task e8775b91).** New `agent_model` config
  field: agent / orchestrate / subagent work uses `ResolveAgentModel()` (agent_model
  if set, else the chat model), so you can pin a reasoning/tool-capable model for
  agent work while keeping a cheap non-reasoning model for chat. The agent runner
  warns loudly when the chosen model doesn't support tool calling (it will flail /
  hallucinate in agent mode). `reconcileModel` migrates the grok-4-1-* trap on
  `agent_model` too. (`Options.Model` is the per-run override seam.)
- **Subagents + MCP agent mode auto-approve tools** so they can actually do work
  (write/commit/bash) headlessly — spawning / invoking is the approval. The
  interactive main agent stays `/confirm`-gated.
- **Corruption colors are sourced from the canonical corrupted-theme palette
  (task 7aa133c9).** New `cmd/celeste/tui/theme` package embeds `colors.json`
  (synced from corrupted-theme `src/data/colors.json` via `make sync-theme`);
  `streaming.go` consumes it via `theme.Hex(...)` instead of hardcoded hex, so the
  #00ffff/#ff0000 colors track the theme repo. Corruption phrase/glitch pools are
  intentionally left in code (animation-critical).
- **`/agents kill <id>` cancels a specific in-flight subagent (task 6ffb5a7c).** The
  manager now tracks a per-run cancel function (keyed by run id and task id) and
  exposes `Manager.Kill`; the TUI wires `/agents kill <id>` (with autocomplete and
  help text). Combined with the runtime ctx-honoring fix, this gives a manual escape
  hatch for a stuck subagent instead of having to kill the whole TUI.
- **Subagent resilience and orchestration.** Transient-error retry with backoff at the
  LLM client layer — 429 honors `Retry-After`, 5xx and network errors retry with capped
  exponential backoff, 4xx fails fast (#29). Background subagents with auto-transition
  after a threshold, exposed via the `spawn_agent` `background_after` param (#30).
  Inter-agent mailbox messaging with a `post_message` tool for spawn-time injection (#31).
  Opt-in git worktree isolation per subagent (`isolate_worktree` param), with serialized
  merges and sanitized worktree names (#32). Checkpoint persistence with `/agents resume`
  to continue a failed subagent from its last completed turn (#33).
- **TUI hard permission gate (#34).** Modal confirmation that blocks tool execution:
  reads auto-approve, writes require approval. Options for yes / no / yes-for-this-tool /
  yes-all-this-session, with rule persistence.
- **Codegraph accuracy improvements.** STUB detection now skips dunder methods (#42),
  `Protocol`/`ABC`/`@abstractmethod` members (#43); decorator `@syntax` calls (#44) and
  `@property.setter` assignments (#45) are captured as call edges; `include_tests` now
  matches top-level test dirs and pytest conventions (#46); two-pass `Build()` fixes
  dropped cross-file caller counts (#47).

### Changed

- **Default model is now `grok-4.20-0309-non-reasoning`** — reliable tool calling, zero
  reasoning-token burn, never routes to the cost-prohibitive grok-4.3.

### Fixed

- **Subagent/agent runtime no longer hangs uncancellably on a stuck tool (task 349f1f14).**
  A tool that ignores its context (e.g. a codegraph call spinning on the DB) used to
  block the turn loop forever — the per-tool timeout fired on the context but nothing
  observed it. Tool execution now runs under `runToolWithTimeout`, which returns at the
  deadline even if the tool keeps running, and the turn loop checks `ctx.Err()` between
  turns so an expired parent deadline (a subagent's overall timeout) stops it promptly.
  New `StatusCancelled` run status.
- **Codegraph operations honor context cancellation (task 349f1f14).** `SemanticSearch`,
  `Build`, and `Update` gained `*WithContext` variants that check `ctx` before and during
  their hot loops, so a `code_search`/`code_index` tool call stops at the tool deadline
  instead of scanning the whole corpus/repo. The `code_search` tool and the server index
  tools pass their request context through; non-ctx signatures are preserved as
  `context.Background()` wrappers.
- **Progress-aware repetition guard (task 8f02ed3d).** The chat/message loop now stops a
  loop where the model re-calls the same tool with slightly-varying args but gets
  byte-identical results turn after turn — a case the args-based guard missed. It keys on
  the result, not the args, so legitimate bulk work (each call producing a distinct
  result, e.g. a new mp3 file) is never blocked.
- **xAI streaming tool-call JSON corruption / TTS hallucination (#48).** Validate
  assembled tool-call JSON at stream finish, chat-mode error-handling parity with agent
  mode, block hallucinated `Audio saved:` claims when no file is written, distinguish
  missing vs empty TTS text, and cap consecutive invalid-args turns instead of retrying
  unbounded.
- **Model routing safety (#51).** Auto-migrate deprecated `grok-4-1-*` models (xAI
  silently routes them to grok-4.3 — the cost trap) and fill empty model on load; clamp
  a stale `context_limit` that exceeds the model window; correct `grok-build-0.1` to its
  real 256K window and pricing.
- **Repetition guard** in both the TUI and server message loops — the call signature now
  includes args, so it only trips on identical repeated calls and does not block bulk
  distinct work (e.g. batch TTS).
- **macOS install** no longer produces `zsh: killed` — `make install` builds to the
  destination and re-applies an ad-hoc code signature instead of `cp`-over-existing.
- **Theme color drift (#49)** — corruption cyan/red aligned to canonical corrupted-theme
  0.2.0 (`#00ffff` / `#ff0000`).
- **Quiet MCP startup** — per-tool registration logging is now gated behind
  `CELESTE_MCP_DEBUG`.

### Security

- **Hard permission gate (#34)** blocks tool execution pending explicit approval,
  closing the prompt-injection bypass of the previous soft `/confirm`.
- **Subagent worktree isolation (#32)** contains the blast radius of any single subagent.
- **grok-4-1-\* migration guard (#51)** prevents silent routing to the cost-prohibitive
  grok-4.3 model.

## [1.9.3] - 2026-04-21

### Added

- **Subagent orchestration with DAG dependencies.** Element-named agents (地火水光闇風)
  with auto-detected dependency chains from goal text. Agents that reference another
  agent's task_id automatically wait until the dependency completes before starting.
  Parallel dispatch for independent tasks, sequential execution for dependent chains.
- **Multi-language tree-sitter code graph.** Native Go parsers for 10 languages
  (Python, Rust, TypeScript, JavaScript, Java, C, C++, Ruby, PHP, TSX). 67-140%
  more symbols extracted vs regex. Node type mappings derived from code-review-graph.
- **Graph snapshots and change impact analysis.** `/index snapshot` saves graph state,
  `/index diff` compares against last snapshot, `/index impact` maps git diff to affected
  symbols with risk scoring and test gap detection. MCP tools: `code_impact`, `code_snapshot`.
- **Audio production pipeline.** ElevenLabs TTS (generate, speak, play, batch, history,
  download), sound effects generation, timeline-aware ffmpeg mixer with loop/delay/volume,
  `audio_render` project pipeline with Gantt chart visualization. Idempotent batch with
  timeout recovery. SSML tags auto-stripped (ElevenLabs v3 doesn't support them).
- **User identity system.** `/user` command with Kusanagi mode vs Summoner default.
  Prompt refreshes mid-session on identity change. Hidden LLM-visible directives.
- **Session picker panel.** Interactive paginated session browser (↑/↓/PgUp/PgDn/Enter/d)
  rendered inline in the TUI. Session resume loads full conversation history.
- **ElevenLabs voice management.** `/voice list`, `/voice set-key`, `/voice set-voice`
  commands. Config persisted to disk, loaded fresh on each tool call.
- **Confirm mode.** `/confirm` toggle with status bar indicator. Task execution prompt
  for sequential multi-step plan completion.
- **Subcommand typeahead.** Purple hints for `/index rebuild`, `/voice list`, etc.
  Escape clears input. Navigation keybinds in status bar.

### Changed

- **v3.0.0 persona sync.** `system_prompt` field used directly for v3.0.0 essence,
  v1.x structured assembly preserved as fallback. Fraternal twin gender clarified.
  Anti-confabulation appearance fix ("No tail. No wings.").
- **Tool timeout tiers.** 30s for reads, 5min for bash/TTS, 10min for subagents,
  2min for audio render. Prevents long-running tools from being killed.
- **Model changes persist to config.** `/model` and `/config set-model` write to
  `config.json` so the setting survives restarts.
- **Empty response detection.** Shows "(No response — try rephrasing)" instead of
  appearing frozen when LLM returns empty.
- **Tool progress auto-clear.** Completed entries clear when response finishes,
  not on next user message.
- **Ctrl+K shows on/off state** for skill call logs visibility.
- **Grimoire injection for MCP content tool.** `celeste_content` now reads `.grimoire`
  from CWD for project-specific rules.
- **Tick animation during tool execution.** Spinners and elapsed timers now animate
  continuously while tools run, without requiring keypress.

### Fixed

- Empty assistant bubble for tool-call-only responses.
- Session resume type assertion mismatch (`tui.SessionMessage` vs `config.SessionMessage`).
- Subagent partial result recovery on failure (returns last assistant response).
- Mix validation rejects stacked audio (all tracks at delay:0 with no loops).
- DAG drain context cancellation (was killing queued agents immediately).
- Stagger delay based on active agent count, not total-ever spawned.
- Nil guard on failed spawn preventing panic.

## [1.9.2] - 2026-04-17

### Added

- **Persisted LSH band table for sub-linear semantic search.** 64 bands × 2 elements from
  the 128-element MinHash signature, stored in a new `lsh_bands` SQLite table with an index
  on `(band_id, band_hash)`. At query time, `SemanticSearchWithOptions` computes the query's
  64 band hashes and retrieves candidate symbol IDs directly from the table — typically 0.1-1%
  of the corpus — then ranks only those candidates by exact Jaccard similarity. Falls back to
  brute-force for pre-LSH indexes that haven't been rebuilt.

  The 64×2 band configuration is empirically derived from the CODEGRAPH_LSH_RESEARCH.md
  validation: conventional 16×8 and 32×4 configs produce 0% recall on code search because the
  Jaccard similarity range for code queries (0.05-0.20) is far below document similarity
  (0.5-0.8). At grafana scale (77,420 symbols), validated at 20× speedup over brute-force
  (89ms → 4.3ms) by eliminating the full signature BLOB load.

- **TUI `/index rebuild` and `/index update` subcommands.** Previously `/index` was
  display-only — users had to exit the TUI and run `celeste index --rebuild` from the shell.
  Now the TUI can trigger a full or incremental re-index directly, with async execution and
  status reporting via the typing animation.

- **TUI `/config set-key`, `/config set-model`, `/config set-url` subcommands.** Users can
  now modify API key, model, and base URL from the TUI without exiting. Previously required
  shelling out to `celeste config --set-*`.

### Changed

- `SemanticSearchWithOptions` now uses LSH candidate lookup when `lsh_bands` data exists,
  brute-force fallback when it doesn't. BM25, RRF fusion, structural rerank, and path filter
  are all unchanged — LSH is a pre-filter that narrows the candidate pool without affecting
  downstream scoring.
- Users must rebuild their index once to populate LSH band data:
  `/index rebuild` in TUI or `celeste_index { operation: "rebuild" }` via MCP.

## [1.9.1] - 2026-04-14

### Fixed

- **TUI streaming tick-complete race truncated short first chunks.** When grok streamed an
  assistant reply that started with a 1-3 character first delta (e.g. `"O"`), the typing
  animation exhausted its buffer on the very first `TickMsg` (50ms later, `charsPerTick=3`),
  committed that single character to session history, and stopped the ticker. Subsequent SSE
  chunks appended to a zombie buffer with no active renderer — the TUI chat bubble froze at
  the first chunk, session persistence wrote the 1-char string to disk, and the next LLM
  request was sent with `content_len=1` for that assistant message, losing the rest of the
  response as conversation context.

  Root cause was in `cmd/celeste/tui/app.go` — the `TickMsg` handler used
  `typingPos == len(typingContent)` as the sole termination condition without checking whether
  the network stream had actually finished. Fix adds an `AppModel.streamDone bool` that tracks
  `EventMessageDone` receipt; the tick-complete branch now has three cases:

  | `typingPos` vs `len` | `streamDone` | Action |
  |---|---|---|
  | `pos < len` | any | advance + reschedule (unchanged) |
  | `pos == len` | `false` | **idle**: reschedule a no-op tick so any late chunk gets picked up |
  | `pos == len` | `true` | commit to session + stop |

  `StreamChunkMsg(IsFirst=true)` resets `streamDone=false`. `StreamDoneMsg` sets it to true.
  `AgentProgressResponse` (non-streaming agent reply path that reuses the typing animation)
  primes `streamDone=true` up front so its animation commits on catch-up instead of idling
  forever waiting for a done event the agent path never sends.

  Validation: four new regression tests in `cmd/celeste/tui/streaming_race_test.go` reproduce
  the exact scenario and pin the new invariants. Confirmed in a live TUI session against
  grok-4-1-fast with a 630-character response rendered correctly on the first try — no
  re-prompt needed, no trailing anomalies in `~/.celeste/logs/celeste_2026-04-13.log`.

### Details

- Changed files: `cmd/celeste/tui/app.go`, `cmd/celeste/tui/streaming_race_test.go` (new),
  `cmd/celeste/main.go` (version constant), `cmd/celeste/server/server.go` (version constant),
  matching test assertions.
- Scope: this is the **only** behavioral change in v1.9.1. Everything else from v1.9.0 is
  untouched.

## [1.9.0] - 2026-04-13

Major search-quality bundle and MCP architecture rework. Eleven commits, ten closed feature
tasks, empirical A/B validation archives under
[celeste-stopwords/results/](https://github.com/whykusanagi/celeste-stopwords/tree/main/results).

SPEC §5.3 acceptance criteria all met on the grafana benchmark:
`JQueryStatic` absent from Q3 top 10, aggregate relevance 22/50 → 35/50 (+59% absolute),
no TP regressions on Q2/Q5, stopword list < 500 entries, corpus Apache-2.0/MIT.

### Added

- **Direct codegraph MCP tools.** Five new first-class MCP tools that bypass the chat LLM
  entirely so Claude Code / Cursor / any MCP client can call the codegraph directly:
  - `celeste_index` (operations: `status`, `update`, `rebuild`) — indexing is explicit; the
    query tools never auto-reindex. Progress notifications (`notifications/progress`) stream
    back through the stdio transport when the client provides a `progressToken`.
  - `celeste_code_search` — semantic search with MinHash+BM25 fusion + structural rerank
  - `celeste_code_review` — structural code review findings as verbatim JSON
  - `celeste_code_graph` — symbol callers/callees/references
  - `celeste_code_symbols` — list all symbols in a file or package

  Per-workspace `*codegraph.Indexer` cached on the server, lazy-opened, released on shutdown.
  Results returned verbatim as MCP `ContentBlock`s with no chat-LLM summarization and no
  `max_tokens` ceiling.

- **BM25 fused ranking.** Per-symbol term frequency and per-corpus IDF persisted in two new
  SQLite tables (`symbol_tokens`, `token_stats`). Query time merges Jaccard and BM25 via
  Reciprocal Rank Fusion (k=60). `SearchResult` gains `BM25Score` + `MatchedTokens`. Q1
  ("authentication session token validate") flipped from 2/10 relevant to 8/10 relevant on
  grafana after this landed — IDF-weighted tiebreaking on session/token tokens lifted the
  real auth API functions above the noise.

- **Tree-sitter TypeScript parser** (behind `//go:build cgo`). Replaces the regex-based
  `GenericParser` for `.ts` and `.tsx` files when celeste is built with CGo enabled. Accurate
  `call_expression` edge resolution instead of the old `\bname(` body-scan heuristic. On
  content-control (21 TS files) the edge count drops 445 → 211 (real, not overcounted) and
  zero-edge top-10 warnings drop 3 → 0. Python and Rust stay on the regex parser for v1.9.0;
  tracked for v2.1.0 as #18 and #19.

  `parser_ts_stub.go` provides a `//go:build !cgo` fallback that delegates to `GenericParser`
  so pure-Go cross-compile release binaries still work — they just lose the tree-sitter
  improvement. Users building from source get the full experience automatically.

- **Structural feature rerank layer.** `StructuralReranker` rescores candidates using features
  the RRF fusion can't see: matched-token-ratio, log-normalized edge density, function/method
  kind boost, zero-edge penalty. Pure Go, zero dependencies. Exposed via a `Reranker` interface
  on `SemanticSearchOptions` so a future embedding-based reranker (local llama.cpp bridge,
  grok/xAI embeddings, ONNX) can drop in without touching the pipeline.

- **Stopwords runtime integration.** Embedded `celeste-stopwords` v1.0.0 (CC BY 4.0) applied
  at both index time and query time. Filters universal + per-language noise tokens from
  shingle sets before MinHash so common tokens like `get`/`set`/`error`/`string` don't consume
  signature slots. Includes a downstream patch removing `query` from the TypeScript set to
  avoid asymmetric filtering breaking Q3 on TS codebases, plus `TestStopWords_PreserveTokensNotStopped`
  regression guard.

- **Reasoning metadata on `SearchResult`.** `EdgeCount`, `PathFlags`, `ConfidenceWarnings`,
  `MatchedTokens` — downstream LLMs can audit every search result instead of trusting a
  single similarity number. Confidence warnings surface zero-edge interfaces, low-confidence
  scores, declaration-only types, and path-demotion reasons.

- **Path-based post-ranking filter.** Tier-partitions `test` / `mock` / `generated` /
  `vendored` / `declaration` results below clean-path results with explicit `[mock]` etc.
  flags. Q2 ("http request handler middleware") previously had 100% mock handlers dominating
  the top 10 on grafana; v1.9.0 tiers them below production handlers. If the query itself
  asks for test/mock code (`queryWantsTests`), the filter backs off to respect user intent.

- **MinHash seed persistence.** Replaced `hash/maphash` (opaque, unserializable) with seeded
  FNV-1a (serializable uint64 seeds). The 128 seed values are stored in a new `meta` SQLite
  table at first `Build()`, so a subsequent process loading the same index restores the same
  hash family and signatures stay comparable across process invocations. Without this, the
  `celeste serve` MCP bridge silently returned noise because every new server process rolled
  fresh random seeds that had no relationship to the signatures already stored in the database.

### Changed

- **Anthropic backend default `max_tokens` raised from 8192 → 32768.** The old 8192 ceiling
  truncated the chat-mode MCP path mid-response when a sub-tool returned a multi-KB JSON blob
  and the chat LLM was asked to echo it. Claude opus/sonnet 4.x support up to 64K output
  tokens; 32K is a 4× budget with no downside (only used tokens are billed).

- **`ShinglesForSymbol` signature.** Now takes `lang string` so the embedded stopwords
  filter can apply the per-language set alongside the universal set. Callers that don't know
  the file language should pass `""` to get universal-only filtering.

- **`SearchResult` struct.** Extended with `BM25Score`, `MatchedTokens`, `EdgeCount`,
  `PathFlags`, `ConfidenceWarnings`. `Similarity` (Jaccard) is unchanged — existing callers
  that only read `Symbol` + `Similarity` keep working.

- **MinHash signatures computed by celeste-cli < 1.9.0 are semantically stale.** Existing
  indexes still work for symbol lookups, edges, and keyword search, but semantic search
  accuracy improves if you rebuild:

  ```bash
  # Via the CLI
  celeste index --rebuild

  # Via the MCP bridge
  # call celeste_index { operation: "rebuild", workspace: "/path/to/project" }
  ```

### Fixed

- **`splitCamelCase` end-of-acronym rule now requires 3+ consecutive uppercase letters.** Two
  uppercase letters followed by a lowercase word is a PascalCase boundary, not an acronym edge.
  Previously, `JQueryStatic` decomposed into `["J", "Query", "Static"]`, which caused the
  `jQueryStatic` pollution problem documented in
  [celeste-stopwords Issue #1](https://github.com/whykusanagi/celeste-stopwords/blob/main/docs/KNOWN_QUALITY_ISSUES.md):
  searches for "query" would match `JQueryStatic` via the stray `query` token, and ~1,650 other
  identifiers exhibited the same bug across a 31-repo training corpus. `HTTPServer`,
  `HTMLToMarkdown`, `CSVParser`, and every other 3+ consecutive-uppercase acronym still splits
  correctly.

  Affected identifier families now atomize correctly:
  - `JQuery` / `JQueryStatic` / `JQueryElement` / `JQueryPromise`
  - `IFoo` / `IArguments` / `IPromise` / `ICache` / any `I`-prefixed TypeScript interface
  - `IPv4` / `IPv6` / `VNode` / `ETag` / `OAuth2` / `XDist`

- **Two direct-tool MCP schema mismatches.** `celeste_code_symbols` advertised a `name` field
  that the underlying builtin tool doesn't accept; `celeste_code_graph` advertised an `action`
  discriminator that doesn't exist. Both schemas now mirror the real builtin tool parameters
  1:1 — name-based lookups go through `celeste_code_search` instead.

### CI / Release

- **Go toolchain bumped 1.26.1 → 1.26.2** in `.github/workflows/ci.yml` and `release.yml`.
  1.26.1 stdlib has 5 CVEs (`crypto/x509` × 3, `crypto/tls`, `html/template`) fixed in 1.26.2;
  `govulncheck` now returns "No vulnerabilities found".
- **Release workflow pins `CGO_ENABLED=0`** for cross-platform binary builds. Released
  artifacts ship with the pure-Go `parser_ts_stub.go` fallback for portability; users who want
  the tree-sitter improvement can `go install` from source with CGo enabled. A proper CGo
  cross-toolchain release workflow (zig cc or native-runner matrix) is queued for v2.1.0.
- **Dockerfile builder adds `build-base`** and sets `CGO_ENABLED=1` on the test-compile step
  so the codegraph test binary can link the tree-sitter C runtime. Runtime container still
  uses `CGO_ENABLED=0`; only test compilation needs the C toolchain.

### Details

- 14 commits on `feat/v1.9-quality-bundle` (PR #16) plus a schema fix direct to main plus
  PR #17 hardening `release.yml` for the tag build.
- Empirical validation archives under `celeste-stopwords/results/`:
  - `ab_test_TASK19_v1.9.0_grafana_app.txt` (path filter + reasoning metadata)
  - `ab_test_TASK20_v1.9.0_grafana_app.txt` (BM25 fused ranking)
  - `ab_test_TASK21_v1.9.0_grafana_app.txt` (stopwords runtime)
  - `ab_test_TASK24_rerank_content_control.txt` (structural rerank, shared-index A/B)
  - `ship_decision_v1.9.0.md` (full GO decision document)
- Known v2.1.0 follow-ups: #18 (tree-sitter Python), #19 (tree-sitter Rust), release workflow
  CGo cross-toolchain for pre-built TS parser.

## [1.8.0] - 2026-04-03

### Added
- **`.grimoire` Project Context**: Persona-themed project config files with auto-discovery, `@include` support, and `celeste init` auto-detection
- **Code Graph Index**: Structural code graph with Go AST parsing, regex extraction for other languages, SQLite storage via `modernc.org/sqlite`
- **Semantic Code Search**: MinHash over enriched shingles — concept-based search without embeddings or API calls
- **MCP Server Mode**: `celeste serve` exposes Celeste via stdio and authenticated SSE transports for Claude Code, Codex, or any MCP client
- **Git-Aware Context**: Startup snapshot injected into system prompt, plus `git_status` and `git_log` tools
- **Session Persistence**: JSONL append-only session logs with `celeste resume` and auto-resume
- **File Checkpointing**: Snapshots before writes, stale file detection, `/diff` session summary, `/undo` revert
- **Extended Thinking**: Provider-specific reasoning tokens (Claude, Gemini, xAI) with `/effort` command
- **Prompt Caching**: Static prefix / dynamic suffix structure for cache-friendly system prompts
- **Image Input**: Multimodal support — `read_file` detects images, base64-encodes for vision models
- **Web Search & Fetch**: DuckDuckGo search and URL-to-markdown tools (no API key required)
- **Cost Tracking**: Per-model pricing table, session cost accumulation, display in context bar
- **Hooks System**: Pre/post tool execution hooks defined in `.grimoire` with template variables
- **Plan Mode**: `/plan` enters read-only mode, writes plan file for user review, `/plan execute` runs it
- **Task Tracking**: `todo` tool for model self-management with TUI panel
- **Memory System**: Persistent learned knowledge at `~/.celeste/projects/`, heuristic extraction, staleness detection
- **Subagent Spawning**: `/spawn` and `spawn_agent` tool for foreground task delegation
- **Graceful Ctrl+C**: Single interrupt cancels current task, double exits. AbortSignal propagation through tools.
- **Build-time Version Injection**: Version, build tag, and commit SHA injected via ldflags in CI/CD

### Changed
- Version information now uses `var` instead of `const` for ldflags injection
- Release workflow injects commit SHA into binaries
- CI build artifacts include version + commit metadata

## [1.7.0] - 2026-03-31

### Added
- **Unified Tool Layer**: Single `Tool` interface replacing the old `skills/` package, with `tools/builtin/` for all implementations
- **Streaming Tool Executor**: Tools begin executing as LLM generates, with concurrent dispatch for read-only tools
- **Context Window Management**: Automatic token budget tracking, reactive/proactive compaction, tool result capping
- **Permission System**: Multi-layer allow/deny/ask rules with pattern matching, denial tracking, persistent config
- **MCP Client**: Model Context Protocol support with stdio/SSE transports for external tool servers
- **TUI Enhancements**: Tool progress indicators, context budget bar, permission prompts, MCP server panel

### Changed
- Replaced `cmd/celeste/skills/` package with `cmd/celeste/tools/` unified tool system
- All 23 built-in skills migrated to `tools/builtin/` with individual files
- All 6 dev tools migrated from `agent/dev_skills.go` to `tools/builtin/`
- LLM backends now support `SendMessageStreamEvents()` for granular streaming
- Agent runtime uses streaming events instead of sync batch execution
- Config token tracking delegates to new `context/` package

### Removed
- `cmd/celeste/skills/` package (replaced by `cmd/celeste/tools/`)
- `cmd/celeste/agent/dev_skills.go` (replaced by `tools/builtin/`)
- `cmd/celeste/llm/summarize.go` (replaced by `context/summarizer.go`)
- `cmd/celeste/config/context.go` token tracking (replaced by `context/budget.go`)

## [Unreleased - Pre-1.7]

### Added
- New autonomous `agent` command family for multi-turn task execution:
  - `celeste agent --goal ...`
  - `celeste agent --resume <run-id>`
  - `celeste agent --list-runs`
  - `celeste agent --eval <cases.json>`
- Agent runtime loop package (`cmd/celeste/agent`) with:
  - max-turn and max-tool safety controls
  - completion-marker controls (default `TASK_COMPLETE:`)
  - no-progress stop behavior
- Checkpointed long-horizon run persistence under `~/.celeste/agent/runs`.
- Agent-only development skills for coding/content workflows:
  - `dev_list_files`, `dev_read_file`, `dev_write_file`, `dev_search_files`, `dev_run_command`
- Eval harness for JSON-defined scenarios with pass/fail scoring.
- Phase 2 agent controls:
  - explicit planning phase with extracted plan steps
  - execution progress markers via `STEP_DONE: <n>`
  - verification gate via repeatable `--verify-cmd` commands and `--require-verify`
- Phase 3 agent deliverables:
  - per-run artifact bundles (`summary`, `run_state`, `plan`, `steps`, `verification`, optional git status/diff)
  - benchmark suite scaffolding via `celeste agent --benchmark <suite.json>`
  - optional benchmark JSON report export via `--benchmark-out`

### Testing
- Added new unit coverage for:
  - checkpoint save/load/list behavior
  - workspace path traversal guards
  - development skill execution paths
  - eval file parsing and result scoring

## [1.5.5] - 2026-02-27

### Fixed
- Restored tool definition delivery in chat mode so skill/function metadata is sent from the live LLM client instead of the TUI skills panel stub.
- Added multi-tool execution flow support: all tool calls returned in a single assistant turn are now executed and replied with matching `tool_call_id` results before continuation.
- Hardened tool argument parsing so malformed JSON arguments surface as explicit tool errors rather than silently falling back to empty argument maps.

### Changed
- Added schema validation for disk-loaded custom skills to reject malformed function definitions before they reach provider APIs.
- Hardened OpenAI/xAI tool serialization by skipping invalid tool payloads gracefully instead of sending malformed definitions.
- Improved Google/Vertex schema conversion compatibility for `required` fields across both `[]string` and `[]interface{}` input forms.

### Testing
- Added regression coverage for:
  - custom skill schema validation pass/fail paths
  - OpenAI and xAI tool serialization skip-on-error behavior
  - Google/Vertex `required` schema conversion compatibility
  - multi-tool TUI execution sequencing and single follow-up request semantics

## [1.5.4] - 2026-02-25

### Security
- Upgraded `go-ethereum` v1.16.8 → v1.17.0 to remediate GO-2026-4508 (DoS via malicious p2p message)

## [1.5.3] - 2026-02-25

### Added
- `upscale_image` tool call skill — upscale and enhance images via Venice.ai, now available as an LLM-callable tool in all modes (not limited to NSFW)
- `docs/plans/2026-02-24-clear-nsfw-upscale-design.md` — design doc for this release's changes
- `docs/plans/2026-02-24-clear-nsfw-upscale.md` — implementation plan used to build this release

### Changed
- `/clear` now performs a full session reset — clears chat history **and** starts a fresh session (equivalent to `/clear` + `/session new` in one command)
- Removed `upscale:` as an NSFW-mode media command; upscaling is now handled exclusively by the `upscale_image` tool call

## [1.5.2] - 2026-02-22

### Fixed
- `/tools` command from menu now correctly opens the skills browser instead of returning "Unknown command"
- `/nsfw` now toggles — typing it a second time disables NSFW mode (previously required `/safe` to exit)
- Context rot: UI notification messages (`role=system`) were being sent to the LLM on every request, bloating context with phantom system messages per session; LLM requests now only include user/assistant/tool messages
- Menu item selection now correctly executes the selected command (value-type `InputModel` mutation was being discarded)
- Skill browser selection now correctly populates the input field

### Changed
- Renamed project references from `CelesteCLI` to `Celeste CLI` across all documentation, scripts, and workflows

## [1.5.1] - 2026-02-19

### Added
- **Collections Support (xAI RAG)** - Upload custom documents for semantic search during chat
  - Create and manage collections via CLI and TUI
  - Upload documents (.md, .txt, .pdf, .html) up to 10MB each
  - Enable/disable collections for chat with active set management
  - Automatic semantic search integration with Grok models
  - 7 CLI commands: `create`, `list`, `upload`, `delete`, `enable`, `disable`, `show`
  - Interactive TUI: `/collections` command with navigation and toggle support
  - Management API client for xAI Collections API
  - High-level Collections Manager with config integration
  - Server-side RAG via xAI's built-in `collections_search` tool
  - Support for recursive directory uploads
  - Persistent configuration in `~/.celeste/config.json`
  - Document validation (format and size checking)

### Changed
- Extended `config.json` structure with Collections configuration:
  - Added `xai_management_api_key` field for Collections API authentication
  - Added `collections` object with enabled status and active collections list
  - Added `xai_features` object for xAI-specific feature flags
- Updated LLM backend to inject xAI built-in tools when collections enabled
- Enhanced TUI with collections view mode and `/collections` command
- Main application wired for Collections config propagation to LLM client

### Documentation
- Added `docs/COLLECTIONS.md` - Complete Collections user guide with:
  - Quick start tutorial for Collections setup
  - Full CLI commands reference with examples
  - TUI interface usage and keybindings
  - Best practices for organizing collections and documents
  - Troubleshooting guide for common issues
  - Advanced usage patterns (batch operations, git hooks, context switching)
  - API integration details and limitations
  - FAQ section covering common questions
- Updated `README.md` - Added Collections Support section and feature bullet
- Updated `docs/LLM_PROVIDERS.md` - Added Collections column to compatibility matrix showing xAI as only supported provider

## [1.4.0] - 2025-12-18

### Added
- **Wallet Security Monitoring** - Comprehensive wallet threat detection and alerting
  - Monitor multiple wallet addresses across networks (Ethereum, Polygon, Arbitrum, Optimism, Base)
  - Real-time polling every 5 minutes (configurable)
  - 6 wallet management operations: add, remove, list, check security, get alerts, acknowledge alerts
  - 4 threat detection types:
    - **Dust attacks** - Detect tiny value transfers (< 0.001 ETH) used for address poisoning
    - **NFT scams** - Flag unsolicited NFT transfers from unknown contracts
    - **Large transfers** - Alert on significant outgoing funds (> 1 ETH or > 10% of balance)
    - **Dangerous approvals** - Detect unlimited token approvals (2^256-1) and high-value approvals
  - Alert system with severity-based classification (critical, high, medium, low)
  - Persistent alert history stored in `~/.celeste/wallet_alerts.json`
  - Alert acknowledgment system to track reviewed threats
  - CLI commands for wallet management via `celeste skill wallet_security`
- **Background Monitoring Daemon** - Automatic wallet security monitoring
  - Run wallet checks in background at configurable intervals
  - Commands: `celeste wallet-monitor start/stop/status`
  - Fork to background process with PID file management
  - Graceful shutdown with SIGTERM handling
  - Configurable poll interval via `wallet_security_poll_interval`
  - Automatic logging of security events with timestamps
- **Token Approval Monitoring** - ERC20 approval event tracking
  - Monitor `Approval(address,address,uint256)` events via `eth_getLogs`
  - Detect unlimited approvals (max uint256 = 2^256-1)
  - Flag high-value approvals (> 1 million tokens)
  - Alert severity: HIGH for unlimited, MEDIUM for high-value
  - Track spender contracts and approved amounts
- **IPFS File Upload** - Binary file support for IPFS
  - Upload files via `--file_path` parameter
  - Support for all file types: images, PDFs, archives, audio/video, binaries
  - Automatic file size and name detection
  - Returns filename, size, type, and CID in response
  - Preserves original string content upload functionality
- **Wallet Security Storage**
  - `~/.celeste/wallet_security.json` - Monitored wallets configuration
  - `~/.celeste/wallet_alerts.json` - Security alerts history log
  - `~/.celeste/wallet_monitor.pid` - Daemon process ID
  - Automatic directory creation and file management
- **Enhanced Configuration**
  - Added `WalletSecuritySettingsConfig` with poll interval and alert level settings
  - Config fields: `wallet_security_enabled`, `wallet_security_poll_interval`, `wallet_security_alert_level`

### Changed
- Extended Alchemy integration for wallet security monitoring using `alchemy_getAssetTransfers` and `eth_getLogs` APIs
- Enhanced alert display system with severity-based styling (leveraging existing TUI components)
- Updated ConfigLoader interface with `GetWalletSecurityConfig()` method
- IPFS skill description updated to reflect file upload support

### Documentation
- Added `docs/WALLET_SECURITY.md` - Complete wallet security monitoring guide with:
  - Setup instructions for wallet monitoring
  - Threat detection patterns and explanations
  - Background daemon usage and configuration
  - Token approval monitoring details
  - Usage examples for all operations
- Updated `docs/IPFS_SETUP.md` - Added file upload documentation with examples for binary files

## [1.3.0] - 2025-12-18

### Added
- **IPFS Integration** - Decentralized content management
  - Upload and download content via IPFS (returns CID)
  - Pin management (pin, unpin, list pins)
  - Multi-provider support (Infura, Pinata, custom nodes)
  - Gateway URL generation for public access
  - Official go-ipfs-http-client library integration
- **Alchemy Blockchain API** - Comprehensive blockchain data access
  - Wallet operations: ETH/token balances, transaction history, asset transfers
  - Token data: Real-time metadata and comprehensive token information
  - NFT APIs: Query NFTs by owner, metadata, collection info
  - Transaction monitoring: Gas prices, transaction receipts, block information
  - Multi-network support: Ethereum, Arbitrum, Optimism, Polygon, Base (mainnet + testnets)
  - JSON-RPC interface with proper error handling
- **Blockchain Monitoring** - Real-time blockchain event tracking
  - Watch addresses for new transactions across multiple blocks
  - Get latest block information with transaction details
  - Query specific blocks by number (hex or decimal)
  - Asset transfer tracking (external, internal, ERC20, ERC721, ERC1155)
  - Network-specific monitoring with configurable poll intervals
- **Modern Crypto Utilities**
  - Ethereum address validation using go-ethereum (EIP-55 checksumming)
  - Wei ↔ Ether ↔ Gwei conversion helpers with big.Int precision
  - Production-ready rate limiting using golang.org/x/time/rate
  - Multi-network URL construction and validation
  - Chain ID support for all major networks
- **Enhanced Configuration System**
  - Network-specific settings for L2 support
  - Environment variable overrides for CI/CD (`CELESTE_IPFS_API_KEY`, `CELESTE_ALCHEMY_API_KEY`)
  - Flexible provider configuration (Infura, Pinata, custom endpoints)
  - Crypto-specific config fields in config.json and skills.json
  - ConfigLoader interface with GetIPFSConfig(), GetAlchemyConfig(), GetBlockmonConfig()

### Changed
- Upgraded to modern production-grade Go crypto libraries:
  - `github.com/ethereum/go-ethereum@v1.16.7` - Official Ethereum Go implementation
  - `github.com/ipfs/go-ipfs-http-client@v0.7.0` - Official IPFS HTTP client
  - `github.com/ipfs/go-cid@v0.6.0` - Content Identifier handling
  - `golang.org/x/time@v0.14.0` - Token bucket rate limiting
- Improved error handling for external API integrations
- Enhanced skills.json structure for crypto service configuration
- Better address normalization with proper checksum validation

### Documentation
- Added `docs/IPFS_SETUP.md` - Infura IPFS configuration guide
- Added `docs/ALCHEMY_SETUP.md` - Alchemy API setup and usage
- Added `docs/BLOCKCHAIN_MONITORING.md` - Real-time monitoring guide

## [1.1.0] - 2025-12-14

### Added
- **One-shot CLI commands** for all features (context, stats, export, session, config, skills)
  - Execute any command without entering TUI: `./celeste context`, `./celeste stats`
  - Direct skill execution: `./celeste skill <name> [--args]`
  - Comprehensive skill testing with `./celeste skill generate_uuid`, etc.
- **Context Management System**
  - Token usage tracking with input/output breakdown
  - Retroactive token calculation for session history
  - Context window monitoring and warnings
  - Auto-summarization when approaching limits
- **Enhanced Session Persistence**
  - Message persistence across sessions
  - Session metadata tracking (token counts, model info)
  - Improved session loading and restoration
- Interactive model selector with arrow key navigation
- Flickering corruption animation for stats dashboard
- GitHub Actions CI/CD pipeline
- Comprehensive test coverage
- Security vulnerability scanning
- Cross-platform build support

### Fixed
- **Token counting** - Now correctly displays input/output token breakdown
- **All 18 skills** - 100% functional from CLI one-shot commands:
  - Type conversion for numeric arguments (length, value, amount)
  - Parameter name corrections (encoded, text, from_timezone, etc.)
  - Weather skill accepts both string and numeric zip codes
- Session persistence and provider detection issues
- Code formatting issues
- Dependency version compatibility

### Changed
- Improved documentation structure
- Enhanced error handling
- Model selector with arrow key navigation
- Stats dashboard with corruption animation effects

### Documentation
- Added `ONESHOT_COMMANDS.md` - Complete CLI command reference
- Added `docs/TEST_RESULTS.md` - Test verification results for all skills
- Added corruption aesthetic validation guides
- Added brand system documentation (migrated to corrupted-theme package)

## [1.0.2] - 2025-12-03

### Added
- **Bubble Tea TUI**: Complete rewrite with flicker-free terminal UI
  - Scrollable chat viewport with PgUp/PgDown navigation
  - Input history with arrow key navigation
  - Real-time skills panel showing execution status
  - Corrupted theme styling (pink/purple aesthetic)
- **Named Configurations**: Multi-profile config support
  - `celeste -config openai chat` for OpenAI
  - `celeste -config grok chat` for xAI/Grok
  - Template system for quick config creation
- **Skills System**: OpenAI function calling support
  - Tarot reading (3-card and Celtic Cross)
  - NSFW mode (Venice.ai integration)
  - Content generation (Twitter, TikTok, YouTube, Discord)
  - Image generation (Venice.ai)
  - Weather lookup
  - Unit/timezone/currency converters
  - Hash/Base64/UUID/Password generators
  - QR code generation
  - Twitch live status checking
  - YouTube video lookup
  - Reminders and notes
- **Session Management**: Conversation persistence
  - Auto-save and resume sessions
  - Session listing and loading
  - Message history with timestamps
- **Simulated Typing**: Smooth text rendering
  - Configurable typing speed
  - Corruption effects during typing
  - Better UX for streamed responses

### Changed
- **Architecture**: Modular package structure
  - `cmd/Celeste/tui/` - Bubble Tea components
  - `cmd/Celeste/llm/` - LLM client
  - `cmd/Celeste/config/` - Configuration management
  - `cmd/Celeste/skills/` - Skills registry and execution
  - `cmd/Celeste/prompts/` - System prompts
- **Configuration**: JSON-based config system
  - Migrated from `.celesteAI` to `~/.celeste/config.json`
  - Separate `secrets.json` for sensitive data
  - Environment variable override support
- **Binary Name**: Renamed from `celestecli` to `Celeste`

### Removed
- Legacy main_old.go (3,481 lines)
- Old configuration format
- Deprecated Python utilities

### Fixed
- API key exposure in error messages
- Config file permission issues
- Session not saving in some scenarios
- Weather skill error handling

### Security
- Added SECURITY.md with vulnerability reporting process
- Implemented secret masking in config display
- Improved API key storage with separate secrets file
- Added .gitignore protection for sensitive files

## [2.0.0] - Previous Release

### Added
- Initial CLI implementation
- Basic LLM integration
- Configuration file support

## [1.0.0] - Initial Release

### Added
- Basic functionality
- Simple command-line interface

---

## Release Links

- [Unreleased](https://github.com/whykusanagi/celeste-cli/compare/v1.5.2...HEAD)
- [1.5.2](https://github.com/whykusanagi/celeste-cli/compare/v1.5.1...v1.5.2)
- [1.5.1](https://github.com/whykusanagi/celeste-cli/compare/v1.4.0...v1.5.1)
- [1.4.0](https://github.com/whykusanagi/celeste-cli/compare/v1.3.0...v1.4.0)
- [1.3.0](https://github.com/whykusanagi/celeste-cli/compare/v1.1.0...v1.3.0)
- [1.1.0](https://github.com/whykusanagi/celeste-cli/compare/v1.0.2...v1.1.0)
- [1.0.2](https://github.com/whykusanagi/celeste-cli/releases/tag/v1.0.2)
- [1.0.0](https://github.com/whykusanagi/celeste-cli/releases/tag/v1.0.0)

## How to Update

### From 0.x to 1.0+

The configuration format has changed:

**Old format** (`.celesteAI`):
```
api_key=sk-xxx
base_url=https://api.openai.com/v1
```

**New format** (`~/.celeste/config.json`):
```json
{
  "api_key": "",
  "base_url": "https://api.openai.com/v1",
  "model": "gpt-4o-mini",
  "timeout": 60,
  "skip_persona_prompt": false,
  "simulate_typing": true,
  "typing_speed": 40
}
```

**Migration steps**:
1. Backup your old config: `cp ~/.celesteAI ~/.celesteAI.backup`
2. Install new version: `make install`
3. Run config migration: `celeste config --show` (auto-migrates)
4. Verify settings: `celeste config --show`
5. Test: `celeste chat`

### Breaking Changes in 1.0+

- Command name changed from `celestecli` to `Celeste`
- Config file location changed to `~/.celeste/`
- Session format incompatible with 2.x (will create new sessions)
- Some command flags renamed for consistency

---

## Support

- **Issues**: [GitHub Issues](https://github.com/whykusanagi/celeste-cli/issues)
- **Security**: See [SECURITY.md](SECURITY.md)
- **Contributing**: See [CONTRIBUTING.md](CONTRIBUTING.md)
