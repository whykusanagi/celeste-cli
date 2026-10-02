# Subagents

The chat model delegates work with `spawn_agent`. A subagent is an agent
run of its own (its own tool registry, messages and checkpoint) that shares
the chat's MCP clients, hooks and code graph. Spawning one is the approval:
subagents run headless, in trust mode, and deny rules still apply. A
subagent cannot spawn subagents.

The `spawn_agent` parameters are listed in the README; this page covers
subagent types and what a subagent hands back.

## Types

`spawn_agent` takes `type`. Without one, the subagent is `general`. Any
other value is an error that lists the three types.

| Type | Tools | Persona | Model |
|------|-------|---------|-------|
| `explore` | Every read-only tool except `spawn_agent` and `post_message`, plus `submit_result`. No `bash`, no file writes, no `todo` or `save_memory`, and no MCP tools (celeste cannot tell whether an MCP tool changes anything). | off | `small_model` (falls back to `model`) |
| `review` | `read_file`, `list_files`, `search`, `git_status`, `git_log`, every built-in `code_*` tool, and `submit_result` (a custom or MCP tool named `code_*` is not included) | off | `agent_model` (falls back to `model`) |
| `general` | Everything a subagent had before types existed, plus `submit_result` | on, with the `persona` slider override when given | `agent_model` (falls back to `model`) |

The type's tool set is applied to the subagent's own registry, so the model
is never offered the other tools, and a call to one fails as an unknown
tool. With the persona off, the system prompt has no persona, voice
boundary, user identity or sliders; the agent contract and project context
stay. A `persona` argument on an `explore` or `review` spawn is an error.

## Results

Every typed subagent finishes by calling `submit_result` and then replying
with `TASK_COMPLETE`. `submit_result` checks its input against this schema;
unknown keys are rejected:

```json
{
  "summary": "string, required, 1-4000 characters",
  "findings": [
    {
      "title": "string, required",
      "detail": "string",
      "severity": "info | low | medium | high",
      "file": "string",
      "line": 12
    }
  ],
  "files": ["string"]
}
```

`findings` holds at most 50 items, `line` is a whole number of 1 or more,
and `files` holds at most 200 strings. An invalid call gets back every
problem, each naming its field (for example `findings[0].title: required`),
and records nothing, so the model can fix it and call again. A later valid
call replaces an earlier one.

The parent sees one status line, then the result as indented JSON:

```
subagent 地 chi (explore): completed
{
  "summary": "Two places read the config without a lock.",
  "findings": [
    {
      "title": "unguarded read",
      "severity": "medium",
      "file": "config/load.go",
      "line": 88
    }
  ],
  "files": ["config/load.go"]
}
```

A subagent that ends without calling `submit_result` still returns that
shape: its final reply, with the `TASK_COMPLETE` line removed, as the
summary, empty `findings` and `files`, and
`"warning": "the subagent did not call submit_result"`. A subagent that
fails (out of turns, for instance) returns the same shape with why it
stopped as the warning: what it submitted, if it called `submit_result`,
or else its last reply as the summary.

The same JSON is the run's result in `/agents`, which shows each typed
run's type and the first line of its summary, and it is what a DAG
dependent receives as its dependency's result.
