# Plan mode

Plan mode lets Celeste look around before she changes anything. While it is
on she has only read-only tools; she ends it by submitting a plan, and the
plan runs only after you approve it.

## Commands

| Command | What it does |
|---------|--------------|
| `/plan` | Turn plan mode on. The status line shows `PLAN`. |
| `/plan <goal>` | Turn plan mode on and send `<goal>` as your prompt. |
| `/plan off` | Leave plan mode without a plan. |
| `/plan show` | Show the approved plan with each step's todo status. |
| `celeste plan` | The same as `/plan show`, from the shell. |

Plan mode is chat-only. `celeste agent`, MCP chat and agent runs, and
subagents never have it, and the `submit_plan` tool exists only in the chat.

## While plan mode is on

- Each request offers only read-only tools (`read_file`, `list_files`,
  `search`, `git_status`, `git_log`, the `code_*` tools, `web_fetch`, `ask`
  and the like) plus `submit_plan`. MCP tools are offered only when their
  server is trusted and marks them read-only (see the MCP section of the
  README).
- A call to any other tool, even one the model saw earlier, is refused with
  "plan mode is on: only read-only tools until the plan is approved (/plan off
  to leave)" before it runs. This does not depend on the permission mode:
  trust mode and always-allow rules do not let a write through. Allowed
  calls still go through hooks and permission checks as usual.
- Each prompt you send is preceded by a hidden instruction asking Celeste
  to investigate and then call `submit_plan` with concrete, ordered steps.

## Approving a plan

`submit_plan` takes an optional `goal` (the `/plan <goal>` text when left
out) and 1 to 30 steps, each a `title` and an optional `detail`. You get the
plan in a question with two answers:

- **Keep planning** (or Esc): nothing is saved, plan mode stays on, and
  Celeste revises the plan. This is the pre-selected answer, so Enter on
  its own never approves.
- **Approve and start** (move to it with ↓, `j` or `2`, then Enter): the
  plan is saved to `.celeste/plan.json`, each step becomes an item in the
  workspace todo list (`.celeste/tasks.json`), plan mode ends, and Celeste
  starts with step 1 in the same turn, with all tools again.

The approval's result lists each step with the todo id it became and the
two `todo` calls that keep it current: `{"action":"update","id":<id>,"status":"in_progress"}`
when a step starts and `"status":"done"` when it is finished. If three tool
turns pass with no change to the plan's todo items, Celeste gets a hidden
reminder naming the open steps and their ids. It never fires while plan
mode is on, comes at most twice per approved plan, and stops once every
step is done or its todo item removed, when you `/clear`, or when you start,
resume or clear a session. It lasts only for the current process: a resumed
session's plan gets no reminders.

While the question is open, typing goes nowhere: the first key that is not
one of the question's keys (a letter, `/`, a paste) puts the cursor back on
**Keep planning** and the footer says "Typing is ignored here. Choose an
option: ↑/↓ then Enter". Letters stay text until you press an arrow or
Enter, and that first Enter only clears the hint, so typing `/plan off` and
Enter into it does not answer. Retype the command once the question is
closed.

`.celeste/plan.json` holds `goal`, `steps` (`title`, `detail`, `todo_id`)
and `approved_at`. A new approved plan replaces it. `/plan show` and
`celeste plan` mark each step `[x]` done, `[>]` in progress, `[ ]` pending, or
`[-]` when its todo item was removed.

## From 1.x

In 1.x, `/plan <goal>` asked the model to write a markdown checklist to
`.celeste/plan.md`, and `celeste plan` looked for that file and a few others
(`PLAN.md`, `plan.md`, ...). Now `celeste plan` shows `.celeste/plan.json`; when
there is none but a `.celeste/plan.md` exists, it shows that file with a note.
`/plan cancel` is gone: use `/plan off`.
