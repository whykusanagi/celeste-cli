# Celeste CLI Roadmap

*Last updated: October 2026, for 2.0.*

2.0 is the release that brings celeste level with the agentic coding CLIs it is
compared against in [COMPARISON.md](COMPARISON.md) ([#176](https://github.com/whykusanagi/celeste-cli/issues/176)),
while keeping what none of them ship: the code graph with structural review, and
MCP-server mode. What changed for existing users is in
[MIGRATING-2.0.md](../MIGRATING-2.0.md); the details of each item are in
[CHANGELOG.md](../CHANGELOG.md).

This page promises no dates. "Next" lists what 2.0 deferred and where each item
came from; an item moves when someone picks it up.

## 2.0 (shipped)

On `main`, to be tagged v2.0.0. One line per workstream.

- **One loop.** The chat, `celeste agent`, MCP chat and subagents run on the same
  tool loop, with the same caps, permissions, hooks, MCP servers and memories in
  every mode; concurrency-safe tools run in parallel.
- **Hooks v2 and trust.** JSON over stdin/stdout with `additionalContext`;
  `PreToolUse`, `PostToolUse`, `SessionStart`, `UserPromptSubmit`, `PreCompact`,
  `PostCompact`, `Stop` and `SubagentStop`; a repository's hooks run only after
  `celeste hooks trust`. See [HOOKS.md](HOOKS.md).
- **Provider blocks.** Where a backend returns its own reply format (Anthropic
  thinking blocks, OpenAI Responses reasoning items), it is kept byte for byte in
  history, session files and agent checkpoints.
- **Checkpoints, `/undo`, `/rewind`, `revert`.** File checkpoints on disk per
  session; `/undo`, `/diff`, `/rewind [n]`, `/fork` and `celeste revert`.
- **Compaction.** A ladder that scales with the context window: prune old tool
  results (never one the model has not seen), optional Jev pruning, then a
  summary that carries an authoritative state block (todos, changed files, the
  voice rule) ([#174](https://github.com/whykusanagi/celeste-cli/issues/174),
  [#234](https://github.com/whykusanagi/celeste-cli/issues/234),
  [#200](https://github.com/whykusanagi/celeste-cli/issues/200)). The OpenAI
  backend probes for server compaction; using it is in "Next".
- **Thinking replay.** Anthropic thinking blocks and OpenAI reasoning items are
  sent back on later turns, also after `celeste resume`
  ([#192](https://github.com/whykusanagi/celeste-cli/issues/192)).
- **Steering and stream rules.** Regex rules on the streaming reply and on tool
  arguments, a watchdog ballot, a completion gate, and opt-in Jev pruning,
  gating and routing ([#175](https://github.com/whykusanagi/celeste-cli/issues/175)).
  See [STEERING.md](STEERING.md).
- **Persona profiles.** The persona ships encrypted in official builds, at four
  levels (full, spine, lite, off); the persona steps down to fit the context
  window, and on a small window so do the tool definitions: a core set with
  short descriptions ([#310](https://github.com/whykusanagi/celeste-cli/issues/310)); explore and review
  subagents run with it off; the blind persona back-test passed;
  `celeste persona verify` ([#173](https://github.com/whykusanagi/celeste-cli/issues/173)).
  On Anthropic the persona is its own cached system block, so `/user` and
  the rest of the dynamic prompt re-write only what follows it
  ([#309](https://github.com/whykusanagi/celeste-cli/issues/309)).
- **Responses API.** The `openai` provider uses OpenAI's Responses API, with a
  Chat Completions fallback for endpoints that lack it.
- **AGENTS.md and context files.** `AGENTS.md` and `CLAUDE.md` are read up to the
  git root in every mode, under the grimoire; celeste no longer writes
  `.grimoire` into a project; `/init` and `celeste init --agents` write them on
  request.
- **Edits.** `patch_file` takes several edits per call, all or nothing; a
  whitespace-tolerant fallback returns a diff; writes are atomic; an existing file
  must be read before it is edited.
- **Shell and sandbox.** One shell runner with process-tree kill and an output
  cap for `bash`, custom tools and `--verify-cmd`. Sandbox, opt-in for 2.0:
  seatbelt on macOS and bubblewrap on Linux, with `"network": false`. See
  [SANDBOX.md](SANDBOX.md).
- **MCP client hardening.** External tools never replace a registered one;
  `readOnlyHint` is honoured only from servers marked trusted in a home-level
  config.
- **Sessions.** Sessions record their workspace and list this project's first;
  tool calls are persisted.
- **Typed subagents.** `spawn_agent` takes `explore`, `general` or `review`, each
  with its own tool set, model and persona level, and returns a validated
  `{summary, findings, files}` result. See [SUBAGENTS.md](SUBAGENTS.md).
- **Plan mode.** `/plan` restricts the chat to read-only tools until the model
  submits a plan with `submit_plan`; approving it saves the plan, adds a todo per
  step and continues with every tool; `celeste plan` shows it. See
  [PLAN_MODE.md](PLAN_MODE.md).
- **ACP.** `celeste acp` is an Agent Client Protocol agent for Zed and JetBrains:
  prompts, streamed replies, tool calls, the editor's permission prompt and
  cancel; editor threads reopen with `session/load`, and an untrusted
  repository hook file is approved through the editor's permission prompt.
- **Images.** `read_file` fits images to each provider's limits or refuses them
  with the limit named ([#239](https://github.com/whykusanagi/celeste-cli/issues/239)).
- **Surface cleanup.** The classic/claw runtime mode and `skip_persona_prompt`
  are gone; local endpoints need no key; `celeste update` fetches the official
  signed release.

## Next

Each item names where it was deferred from. "The 2.0 design" means the 2.0
release design's out-of-scope list; "the 2.0 plans" means the "Not in" section
of the workstream plan named.

### Compaction

- [ ] Provider server compaction: Anthropic server compaction and OpenAI
      `/responses/compact` as a ladder rung ([#199](https://github.com/whykusanagi/celeste-cli/issues/199),
      rung 2 of [#174](https://github.com/whykusanagi/celeste-cli/issues/174)).
- [ ] Threshold and background compaction (the 2.0 plans: compaction).
- [x] Tool definitions that fit small context windows: a core set with short
      descriptions when the full set would crowd out the history, and a local
      server's reported window instead of the 8,192 guess ([#310](https://github.com/whykusanagi/celeste-cli/issues/310)).

### Context, skills and commands

- [ ] Agent Skills: `SKILL.md` directories in `~/.celeste/skills/*/` and
      `.celeste/skills/`, name and description in the prompt, body on demand; the
      JSON shell "skills" become commands (Agent Skills [#302](https://github.com/whykusanagi/celeste-cli/issues/302); JSON skills to
      `~/.celeste/commands/` [#303](https://github.com/whykusanagi/celeste-cli/issues/303)).
      `~/.celeste/skills/*.json` keeps loading until the move is done.
- [ ] Repository-local commands (the 2.0 plans: context and skills).

### Edits and diagnostics

- [ ] LSP diagnostics after writes, starting with gopls and the TypeScript
      server ([#304](https://github.com/whykusanagi/celeste-cli/issues/304)); more
      language servers after that (the 2.0 plans: edits and LSP).
- [ ] Evaluate hash-anchored (hashline) edits for weaker and local models
      ([#305](https://github.com/whykusanagi/celeste-cli/issues/305)).

### Sandbox

- [ ] Windows sandbox (the 2.0 plans: sandbox). Windows runs `bash` with the
      denylist only.
- [ ] Sandbox on by default. Turning it on is a breaking change, so it waits for
      a major version.
- [ ] Per-command network allowlists; sandboxing hooks and MCP servers (the 2.0
      plans: sandbox).

### Sessions and memory

- [ ] A `recall_memory` tool, so the model can read a memory's body (today it
      sees the memory index), and a memory store keyed on the git root rather
      than the workspace ([#306](https://github.com/whykusanagi/celeste-cli/issues/306)).

### ACP

- [ ] The editor's `fs/*` and `terminal/*` methods, session modes, and `http`/`sse`
      MCP servers passed by the editor (the 2.0 plans: ACP).
- [ ] Share or lazily start MCP servers across editor sessions ([#311](https://github.com/whykusanagi/celeste-cli/issues/311)).

### Persona

- [ ] `persona_lore(query)`: a chat-only lore lookup, BM25 first, semantic
      retrieval later. 2.0 does not ship it
      ([#173](https://github.com/whykusanagi/celeste-cli/issues/173)).

### Providers

- [ ] OpenAI `previous_response_id` server-side state (the 2.0 design).
- [ ] Gemini `ThoughtSignature` folded into provider blocks (the 2.0 design).
- [ ] Anthropic cache tokens shown in `/costs` and the logs ([#312](https://github.com/whykusanagi/celeste-cli/issues/312)).

### Cleanup

- [ ] One token estimator and one set of context thresholds
      ([#307](https://github.com/whykusanagi/celeste-cli/issues/307)).
- [ ] Split `tui/app.go`'s `update()` into one file per message type, with slash
      commands in their own files ([#308](https://github.com/whykusanagi/celeste-cli/issues/308)).

### Code graph

- [ ] Automatic stale-index detection with a rebuild prompt.
- [ ] A pluggable local reranker (llama.cpp bridge or ONNX) behind the existing
      `Reranker` interface, with no cloud dependency.

### Not planned

- A plugin marketplace or a web UI (the 2.0 design).
