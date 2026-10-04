# Steering

Celeste can watch a run as it streams and steer it: stream rules, a
watchdog, a completion gate for agent runs, and optional TypeSafe Jev
judgements. Every mechanism is off or in shadow mode by default (shadow:
it decides and logs, but changes nothing) and fails open: any error,
timeout or missing key leaves the run as it would have been.

## Stream rules

A stream rule is a regex on the model's streamed reply or on one argument
of a tool call, and a reminder that joins the conversation only when the
rule fires. A rule costs no context until then.

`stream_rules` in your config: `shadow` (default), `on`, or `off`.

### Where rules come from

1. The built-ins (below).
2. `~/.celeste/rules/*.md`, one rule per file; the file name is the rule
   name.
3. The project grimoire's `## Stream Rules` section, one `### <name>`
   subsection per rule. This section is never sent to the model.

A project grimoire's rules can stop replies, re-run turns (which costs
requests) and add instructions the model follows, so they are trusted the
way repo hooks are: by content, in the same trust store. The chat asks you
once when it starts. Agent runs, MCP calls and subagents load only rules
you have already trusted, and skip the rest with one warning; approve them
ahead of time with `celeste hooks trust` (and see them with
`celeste hooks list`). Editing the section asks again. The built-ins,
`~/.celeste/rules` and your global `~/.celeste/grimoire.md` need no trust.

A later rule replaces an earlier one with the same name, and
`enabled: false` turns one off:

    ---
    enabled: false
    ---

A file that does not parse is skipped with a warning when the session
starts.

### Format

    ---
    condition: '\bTODO\b'
    scope: tool_args:write_file.content, tool_args:patch_file.new_string
    action: append
    repeat: after-gap:5
    ---
    Do not leave TODO markers in files; finish the code or say what is missing.

- `condition`: a Go (RE2) regular expression. Quotes around it are
  removed; backslashes are kept as written. Use `(?i)` for case-insensitive.
  Matching takes time in proportion to the text, never more. On the
  streamed reply a match is looked for in the last 4 KB, so a match longer
  than that can be missed. A rule file over 64 KiB is skipped.
- `scope`: `text` (the reply as it streams; the default), `thinking`
  (accepted, but thinking-scope rules are not wired to the model's
  reasoning yet, so such a rule is skipped with a warning), or `tool_args:<tool>.<field>` (one argument of
  that tool's calls, checked when the call is complete and before it
  runs). Several scopes are separated by commas. The reply is matched as
  it streams, a batch at a time (every 256 bytes or so, at each newline,
  and once more when the reply ends), so `$` (and `\z`) can match at the
  end of a batch, not only at the end of the reply or of a line; use
  `(?m)` and a newline in the pattern to anchor to line ends.
- `action`:
  - `interrupt`: stop the reply, add the reminder, and run the turn again.
    For a tool-argument rule the turn's calls never run. A turn is re-run
    at most twice; after that the reply goes through.
  - `append` (default): add the reminder after the turn's tool results,
    before the next request. After a final reply it waits for your next
    message.
  - `queue`: add the reminder before the next run (your next message, or
    the agent's next step).
- `repeat`: `once` per session (default), or `after-gap:N`: again only
  after N more requests.

The reminder reaches the model as a hidden `<system-reminder>` message.
The chat shows a short line when a rule stops a reply. A dropped reply
is never kept in the conversation, but the provider billed it, so it
counts in the session cost.

What a rule knows about the session (for example that `generate_speech`
really ran, which silences `unbacked-audio-claim`, or that `repeat: once`
already fired) lasts for the session: the whole chat, one agent run, or
a single MCP `celeste` call in chat mode, where each call starts fresh.

### Built-in rules

| Rule | Fires on | Action |
|---|---|---|
| `persona-voice-in-files` | pet names, emotes or stylised spelling in `write_file` / `patch_file` content, outside fenced code and `>` quotes, in a file not under a `docs` directory or a `persona` path (a `~` is ignored in dotfiles, shell scripts and TeX) | append |
| `unbacked-audio-claim` | "Audio saved:" in a reply when no text-to-speech call has succeeded | interrupt |
| `task-complete-before-verify` | a line that starts with `TASK_COMPLETE` (not a mention mid-sentence, nor `TASK_COMPLETED`) when a file changed after the last command ran (not in agent runs with verification commands, which check the work themselves) | interrupt |
| `destructive-bash` | `git push --force` / `-f` (not `--force-with-lease`), or a recursive forced `rm` (`-rf`, `-r -f`, `--recursive --force`) in a `bash` command, unless every path it removes is under `build`, `dist`, `node_modules`, `target`, `.cache`, `out` or `coverage` inside the project; it reads quoting, paths (`/bin/rm`), subshells, `sh -c`, `eval` and `ssh host '…'`, and fires again on later requests. Not covered: `find … -delete`, `xargs rm`, or scripts the command runs | interrupt |
| `three-strikes` | a strike or warning ladder in a reply | append |

MCP chat (`celeste serve`) also replaces an unbacked "Audio saved:" claim
in its result, in every mode.

## Watchdog

Every 3 requests, the watchdog asks a fixed ballot about the last few
turns: how on track the run is (1–10), and whether it is looping,
drifting from the goal, letting its persona voice into files, claiming
success it has not checked, or running destructive commands it was not
asked for.

`watchdog`: `off` (default), `shadow` (asked and logged), `on`.
`oracle` picks who answers:

- `heuristic` (default): no model call. It can tell looping, voice in
  files, unchecked success claims and destructive commands from the
  transcript, but has no opinion on being on track or drifting.
- `llm`: your `small_model`, or your main model when no `small_model` is
  set.
- `jev`: TypeSafe's Jev (`TYPESAFE_API_KEY` or `~/.celeste/typesafe.key`).

A model-backed oracle that fails, has no key, or takes more than 2.5
seconds is replaced by the heuristic for that ballot.

An answer of 0.70 or more acts; 0.30 to 0.70 is logged only.

- **Blocker** (a destructive command the goal did not ask for): stops the
  reply in flight and re-runs it with the warning.
- **Concern** (off track, looping, drifting, unchecked success): a warning
  before the next request after the tools run.
- **Nit** (voice in files): added to the next warning or reminder, once.

At most one blocker or concern every 3 requests. The ballot runs in the
background and never delays a request. When an agent run or an MCP call
ends or is cancelled, a ballot still running is cancelled and its answer
ignored. In the chat the watchdog spans the conversation, but a new
prompt drops warnings about the previous one that have not been given
yet, and the answer of a ballot still running about it. In an agent run,
the completion gate asks about the final reply, so the background ballot
skips it.

## Completion gate (agent runs)

`completion_gate`: `shadow` (default) or `on`. With `on`, an agent run
completes only when its reply starts or ends with a line beginning with
the word `TASK_COMPLETE` (a mention mid-sentence, or `TASK_COMPLETED`,
does not count). Progress-marker lines such as `STEP_DONE: 1` before it
do not count as the first line, and reasoning a local server left in the
reply, up to a `</think>` line, is skipped. When the watchdog is on, the gate also asks the ballot
before it accepts the completion and sends the run back if the ballot
finds an unchecked claim of success. It sends a run back at most once,
and never a run with verification commands (the runtime checks those
itself). In shadow mode the old check decides and the log says where the
gate would have differed.

## Jev (TypeSafe)

Jev is TypeSafe's decision model. It answers typed yes/no, choice and
score questions in a fraction of a second. Celeste can ask it to judge
three things; each is off by default and has its own key:

| Key | What Jev judges | `on` does |
|---|---|---|
| `jev_prune` | which old tool results the run still needs | the least-needed are pruned first when the context fills |
| `jev_gate` | whether a tool call is destructive, sends data out, or goes beyond what you asked | asks you before an otherwise-allowed call (in headless runs, denies it with the reason); it never allows anything |
| `jev_route` | which kind of work an `/orchestrate` goal is | picks the lane |

`shadow` asks and logs what it would do without acting. Any error, a
missing key, or no answer within 2.5 seconds falls back to celeste's own
rules. `oracle: jev` also lets Jev answer the watchdog's ballot.

Details:

- `jev_prune: on` asks Jev inside the loop, before the request, in the
  chat, agent runs and MCP chat. `/context compact` stays in shadow even
  with `on`: it runs in the UI loop, which never waits on the network.
- `jev_gate` looks only at calls your permission policy already allows,
  and only at tools that change something, plus `web_fetch` (a URL can
  carry data out). A call the policy denies or already asks about is left
  alone. Without a key, a destructive bash command (as the
  `destructive-bash` rule reads it) is still flagged.
- `jev_route` changes only which lane `/orchestrate` uses; see
  [ROUTING.md](ROUTING.md#orchestrate-lanes).

Set it up with:

    celeste config --init jev

It saves your TypeSafe key to `~/.celeste/typesafe.key` (readable only by
you; `TYPESAFE_API_KEY` works too; a pasted key is not echoed) and sets
the three keys to `shadow` in the profile. A key already `on` stays `on`.

## Third parties

Stream rules, the heuristic oracle and the completion gate's marker check
run on your machine and send nothing anywhere. Data leaves your machine
only when you turn on one of these:

- `jev_prune`: excerpts of old tool results with their arguments, the goal and the latest
  user message, to TypeSafe.
- `jev_gate`: the pending tool call (its name and arguments, cut to 2,000
  bytes) and your request (the agent goal, the MCP prompt, or the chat's
  latest prompt), to TypeSafe.
- `jev_route`: the `/orchestrate` goal, to TypeSafe.
- `watchdog` (`shadow` or `on`) with `oracle: jev` or `oracle: llm`:
  every ballot, including the one the completion gate asks, sends the
  ballot's questions and this state to TypeSafe (`jev`) or to your
  model provider (`llm`):
  - the goal (the agent goal, the MCP prompt, or the chat's latest
    prompt);
  - the last 4 assistant replies;
  - for each of their tool calls: the tool name, its arguments (each
    value cut to 600 bytes), an excerpt of its result (800 bytes) and
    whether it failed.

Before anything is sent, secrets and keys are replaced with
`[REDACTED]`, and file paths are rewritten: a path inside the workspace
becomes relative to it, and any other absolute or `~` path becomes
`<path>`.

Redaction is best-effort. Delete `~/.celeste/typesafe.key` (and unset
`TYPESAFE_API_KEY`) to turn Jev off everywhere.

## Status

`celeste_status` (MCP) reports:

- `completions`: how many of this server's model calls (MCP chat, agent
  runs, `celeste_content`) succeeded and failed, with `last_error` while
  the latest one failed. `health` is `degraded` while the latest one
  failed and `ok` again after one succeeds; a cancelled call counts as
  neither.
- `oracle`: the `oracle` mode, the three `jev_*` modes, and the calls,
  fallbacks, hit rate and latency of model-backed judgements, in total
  and per use (`ballot`, `gate`, `route`, `prune`).
- `rules`: the `stream_rules` and `watchdog` modes and how often each rule
  fired, acted, or only logged (shadow).

The counters are per server process, like `session_cost`.
