# Sandbox

Celeste can run the model's `bash` commands inside the operating system's sandbox, so a command can write only to the workspace, the temp directories and your build caches, and optionally cannot reach the network. Reads are not restricted.

| OS | Sandbox |
|---|---|
| macOS | A seatbelt profile, run with `/usr/bin/sandbox-exec`. |
| Linux | [bubblewrap](https://github.com/containers/bubblewrap) (`bwrap`), when it is installed and can create user namespaces. |
| Windows | None. Commands run with the command denylist only. |

The sandbox is **off by default in 2.0**. Turn it on in `~/.celeste/config.json`:

```json
{
  "sandbox": { "enabled": true }
}
```

This holds whichever profile is active: a named profile (`-config <name>`, or a `config.<name>.json` with `"default": true`) inherits the `sandbox` settings of `~/.celeste/config.json`. A profile can set its own `sandbox` object; each key it sets (`enabled`, `network`, `writable`) replaces config.json's, and the keys it leaves out still come from config.json.

It applies to the `bash` tool in every mode: the chat, `celeste agent`, subagents and MCP chat. It does not apply to commands you wrote yourself: hooks, custom JSON tools in `~/.celeste/skills` and `--verify-cmd` run as before. The command denylist (`sudo`, `rm -rf /` and the rest) still checks every `bash` command first, sandbox or not.

Every `bash` command runs in its own process group, and a timeout or cancel kills the whole group, with or without the sandbox. A sandboxed command also runs in its own session, without celeste's terminal, so it cannot type into celeste's prompts.

## What is writable

- the workspace;
- the git directories of the repository the workspace is in, when they are outside it: a linked worktree's (`git worktree add`, which is also how isolated subagents run) git dir and the repository's shared `.git`, or the `.git` above a workspace that is a subdirectory of a repository. Without them `git add` and `git commit` fail. Like the workspace's own `.git`, this includes `.git/hooks` and `.git/config`;
- the temp directories: `$TMPDIR` (or the system default), `/tmp` and, on macOS, `/private/tmp`;
- your user cache directory (`~/Library/Caches` on macOS, `$XDG_CACHE_HOME` or `~/.cache` on Linux), which holds Go's build cache;
- these build caches, when they exist: `~/go/pkg` (for each `$GOPATH` entry when it is set: its `pkg`; plus `$GOMODCACHE` when set), `~/.cargo` (or `$CARGO_HOME`), `~/.gradle` (or `$GRADLE_USER_HOME`), `~/.npm` and `~/.m2/repository`. The Cargo and Gradle homes are writable whole, since Cargo takes its lock there and the Gradle wrapper and daemon live beside the caches; that includes `~/.cargo/bin` and Gradle's init scripts. These locations are read from celeste's environment, not from `go env`'s config file;
- on macOS, `/dev/null`, `/dev/tty` and `/dev/fd/*`. On Linux the sandbox has its own `/dev`, and `/run` is an empty temporary directory (the directory `/etc/resolv.conf` points into there, systemd's, NetworkManager's or resolvconf's, stays visible read-only, so DNS keeps working).

Everything else is read-only to the command. Paths are compared after resolving symlinks.

## Settings

The `sandbox` object takes three keys, in `~/.celeste/config.json` (yours, also for named profiles, which can override it key by key in their own `config.<name>.json`) and in a workspace's `.celeste/config.json` (the repository's):

| Key | Meaning |
|---|---|
| `"enabled"` | `true` runs `bash` in the sandbox, `false` without it. |
| `"writable"` | More writable directories. A relative path is relative to the workspace; `~/` is your home directory. |
| `"network"` | `false` cuts the network (local unix sockets still work on macOS). Default `true`. |

### Adding a cache directory

When a build writes somewhere else, the failed command's error says so:

```
blocked by celeste's sandbox (seatbelt): writes are limited to the workspace, temp and cache directories; add the directory to "sandbox.writable" in .celeste/config.json, or set "sandbox.enabled": false there (that file must be trusted: celeste hooks trust)
```

Add the directory, for every project in `~/.celeste/config.json`:

```json
{
  "sandbox": { "enabled": true, "writable": ["~/.cache/my-tool", "/opt/build-cache"] }
}
```

or for one project in its `.celeste/config.json`:

```json
{
  "sandbox": { "writable": ["../shared-out"] }
}
```

### Turning it off for one workspace

```json
{
  "sandbox": { "enabled": false }
}
```

in the workspace's `.celeste/config.json`.

### Cutting the network

```json
{
  "sandbox": { "network": false }
}
```

A command that fails to resolve or reach a host then gets a hint that the network is off.

## A repository's settings need your trust to loosen

A cloned repository could ship a `.celeste/config.json` that turns the sandbox off. So a repository's file applies like this:

- **Tightening applies always:** `"enabled": true` and `"network": false`.
- **Loosening applies only once you trust the file:** `"enabled": false`, `"network": true` and `"writable"`.

Trust works as it does for repo hooks (see [HOOKS.md](HOOKS.md)): the approval is the file's path plus a hash of its `sandbox` object, stored in `~/.celeste/trusted.json`, and any edit to the object asks again. The chat asks on the terminal when it starts. `celeste agent`, subagents and MCP chat never ask: they skip an untrusted loosening with a warning that names `celeste hooks trust`. Approve it ahead of time:

```bash
celeste hooks list                              # shows .celeste/config.json#sandbox and its status
celeste hooks trust                             # approve this directory's sources (asks y/N)
celeste hooks trust --yes .celeste/config.json  # just the sandbox settings, without asking
```

An isolated subagent's worktree lane sits under the workspace and has its own copy of `.celeste/config.json`. When its `sandbox` object is the same as the one the parent run trusted, the lane reuses that trust; when it differs (the lane's branch changed it), it needs its own and is skipped with a warning like any other. A workspace outside the parent's never inherits trust.

A `.celeste/config.json` that is a symlink (or a FIFO or device) is not read at all: celeste warns that it is ignoring the workspace's sandbox settings, so none of them apply, the tightening ones included. One in a symlinked `.celeste` directory is read, so its tightening settings apply, but it is never trusted. Your own `~/.celeste/config.json` needs no trust.

## When no sandbox is available

With `"enabled": true` and no sandbox, `bash` commands still run, with the denylist only:

- **Linux without a usable bubblewrap** (not installed, or unprivileged user namespaces are disabled, as in many containers and CI runners): one warning per process, `bash runs without a sandbox: install bubblewrap (bwrap) to limit writes to the workspace; the command denylist still applies.` Install it with your package manager (`apt install bubblewrap`, `dnf install bubblewrap`, `pacman -S bubblewrap`).
- **macOS where `sandbox-exec` cannot apply a profile** (celeste itself running inside another sandbox): one warning per process.
- **Windows:** one log line; Windows has no sandbox in celeste.

## Limits

- Reads are not restricted: a command can read any file you can.
- On macOS, unix sockets are not restricted, and on Linux neither are sockets outside `/run`; a command that can reach a privileged daemon's socket (Docker's, for example) can act through it.
- Running celeste with your home directory as the workspace makes all of it writable, `~/.celeste` included.
- The sandbox covers the `bash` tool. Celeste's own file tools (`write_file`, `patch_file`, `splice_file`) already write only inside the workspace.
