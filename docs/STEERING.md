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
  (accepted, but no provider streams thinking yet, so such a rule is
  skipped with a warning), or `tool_args:<tool>.<field>` (one argument of
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
| `task-complete-before-verify` | `TASK_COMPLETE` when a file changed after the last command ran (not in agent runs with verification commands, which check the work themselves) | interrupt |
| `destructive-bash` | `git push --force` / `-f` (not `--force-with-lease`), or a recursive forced `rm` (`-rf`, `-r -f`, `--recursive --force`) in a `bash` command, unless every path it removes is under `build`, `dist`, `node_modules`, `target`, `.cache`, `out` or `coverage` inside the project; it reads quoting, paths (`/bin/rm`), subshells, `sh -c`, `eval` and `ssh host '…'`, and fires again on later requests. Not covered: `find … -delete`, `xargs rm`, or scripts the command runs | interrupt |
| `three-strikes` | a strike or warning ladder in a reply | append |

MCP chat (`celeste serve`) also replaces an unbacked "Audio saved:" claim
in its result, in every mode.

## Third parties

Stream rules run on your machine and send nothing anywhere. The optional
TypeSafe Jev judge (`jev_prune`, and further steering features in later
releases) is the only part of steering that sends anything off your
machine, and only when you turn it on.
