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

Every other key in your config file keeps its value. The MCP `celeste` tool's
`mode` argument (`chat` / `agent`) is unrelated and unchanged.
