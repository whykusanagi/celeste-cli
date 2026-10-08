# Permissions

Celeste checks every tool call against your permission policy before the
tool runs. The policy lives in `~/.celeste/permissions.json`. Choosing
**Always allow** or **Always deny** in the chat's permission prompt adds a
rule to that file. The ACP editor prompt's "Always allow" is the exception:
it lasts for the session only.

## The file

```json
{
  "mode": "default",
  "always_allow": [
    {"tool_pattern": "read_file", "decision": "allow"},
    {"tool_pattern": "bash(git status*)", "decision": "allow"}
  ],
  "always_deny": [
    {"tool_pattern": "bash(sudo *)", "decision": "deny"},
    {"tool_pattern": "write_file(secrets/*)", "decision": "deny"}
  ],
  "pattern_rules": [
    {"tool_pattern": "read_file(*.env)", "decision": "deny"},
    {"tool_pattern": "write_file(docs/*)", "decision": "ask"}
  ]
}
```

With no file, celeste uses the defaults: `default` mode, with `read_file`,
`list_files` and `search` allowed and `bash(sudo *)` and `bash(su *)` denied.

## How a call is decided

The steps run in this order, and the first one that decides wins:

1. **Protected files.** A shell command that names the hook or trust files
   is denied.
2. **`always_deny`**: any matching rule denies, whatever the mode.
3. **`always_allow`**: any matching rule allows.
4. **`pattern_rules`**: the first matching rule's decision (`allow`, `deny`
   or `ask`) applies.
5. **The mode.** `trust` allows everything, `strict` asks for everything,
   and `default` allows read-only tools and asks for the rest.

In `default` mode the read-only auto-allow is the last step, so a pattern
rule's `deny` or `ask` also covers read-only tools such as `read_file`.

A PreToolUse hook can turn an allowed call into a prompt, but it can never
skip your rules. See [HOOKS.md](HOOKS.md).

## Rule patterns

`tool_pattern` is a tool name (`write_file`), `*` for every tool, or a tool
name with an argument glob in parentheses (`bash(git *)`). `input_pattern`,
when set, is a glob matched against the call's whole input as JSON.

### Which argument a glob matches

An argument glob is matched against the field the tool acts on:

| Tool | Field |
|---|---|
| `bash` | `command` |
| `read_file`, `write_file`, `patch_file`, `list_files`, `search`, `git_log` | `path` |
| `web_fetch` | `url` |
| `web_search`, `find_tools` | `query` |
| any other tool | the first of `command`, `path`, `content`, `pattern`, `url` or `query` its schema declares, or else its only string parameter |

Only fields the tool declares count. A field the model adds that the tool
never reads cannot satisfy an allow rule or get a call past a deny rule.
For a call that leaves out the tool's field, and for a tool that has no such
field (`splice_file`, which takes `source` and `dest`, is one), an
argument-scoped `deny` or `ask` rule still applies and an argument-scoped
`allow` rule never does.

### Paths

A path is cleaned before it is matched (`public/./css` becomes
`public/css`, and `public/../secrets/key` becomes `secrets/key`), so `..`
cannot move a path into or out of a rule's directory. An allow rule never
matches a path that still climbs out of the workspace after cleaning
(`../x`). `*` matches across directories: `src/*` covers everything under
`src/`.

### Commands

A command rule covers a single command:

- An `allow` rule never matches a command line that chains, pipes,
  substitutes or redirects (`;`, `&`, `&&`, `|`, `||`, newlines,
  backquotes, `$(...)`, parentheses, `<`, `>`). `bash(git *)` allows
  `git status --short` but asks for `git status && make`.
- A `deny` or `ask` rule matches when any one command in the line matches.
  `bash(sudo *)` denies `make && sudo make install`.

## The permission prompt

The prompt shows the call's primary argument in full (the whole `bash`
command, the whole path), followed by every other argument as `key: value`.
Only a value longer than 4 KiB is cut, and the cut is marked with how many
characters are hidden.

## Only offered tools run

A turn runs only the tools it offered the model. A call to a tool of
another runtime mode, a tool hidden until `find_tools` activates it, or a
tool the chat's plan mode filtered out is refused before any hook or
permission check runs.

## Subagents

`spawn_agent`'s `workspace` must be the parent's workspace or a directory
inside it, with symlinks resolved on both sides. Subagents are
auto-approved, so this keeps their file tools and sandbox inside what the
parent was given. A killed subagent stays failed even when it was just
finishing, and its isolated worktree is not merged.

## Without a home directory

When the home directory cannot be resolved (`HOME` unset in some CI and
service environments), celeste uses the default policy and saves no rules.
It also loads no custom skills, home-level hooks, home-level MCP servers or
stream rules. It never reads those files from the current directory instead.
