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

## Sessions and agent checkpoints

| 1.x | 2.0 |
|---|---|
| A saved session or agent run (`celeste resume`, `celeste agent --resume`) | Loads unchanged, except that a tool result over 128 KiB is cut to 128 KiB (start and end kept) when it loads, and the next save stores the cut version. If you need the full text of such a result, copy the session file (under `~/.celeste/sessions`) or the run checkpoint (under `~/.celeste/agent/runs`) before resuming it in 2.0. |
| Tool results between 64 KiB and 128 KiB | Sent whole. 1.x cut every tool result to 64 KiB on each request. |
