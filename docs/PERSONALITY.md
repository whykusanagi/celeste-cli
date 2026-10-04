# Celeste's persona

Celeste's personality is not open source. It is written in the private celeste-core-persona
corpus, built by celeste-persona-container, and shipped in this repository only as encrypted
files under `cmd/celeste/prompts/persona/`. Those files carry their own license: all rights
reserved, © whyKusanagi (see `cmd/celeste/prompts/persona/LICENSE`). The rest of celeste is MIT.

This page describes how the persona is built, chosen and verified. It does not reproduce it.

## Official releases and source builds

Official release binaries carry the key that decrypts the persona. celeste decrypts it in memory
at startup and never writes it to disk.

A `go install` build gets the official binary too. The first time it runs a command (any but
`help`, `version`, `update` and `persona`), it downloads the official
release binary of the same version, checks its GPG signature and checksums against the release
key built into celeste, replaces itself and carries on. Set `CELESTE_NO_AUTO_UPGRADE=1` to keep
the binary `go install` built. [VERIFY.md](../VERIFY.md) lists what is checked.

Every other build (from a checkout, a fork, a test binary) runs a minimal **public persona**:
one line saying who she is, the rule that she never claims an action a tool didn't report, and
the voice boundary rule below. It says so once at startup:

> Persona: this build runs Celeste's public persona. Her full persona ships only in official
> release binaries (github.com/whykusanagi/celeste-cli/releases).

If a release binary's persona can't be decrypted (a damaged download, say), celeste falls back to
the same public persona, logs why, and tells you to reinstall. It never stops working and never
sends encrypted bytes to a model.

`celeste persona verify` reports which one a binary runs. It prints
`official persona: core <commit>, key id <id>; full ~N, spine ~N, lite ~N, off ~N tokens` and
exits 0 when all four profiles decrypt and match `SOURCE.json`; otherwise it names the reason and
exits 1. It never downloads anything (nor do `celeste version` and `celeste help`), so on a
fresh `go install` build run `celeste update` first (it installs the official binary), then
`celeste persona verify`.

**What the encryption is for.** It is a gate and a statement of the license, not secrecy: every
official binary contains the key, and a determined person can extract it. Doing so doesn't grant
any rights to the persona.

## Profiles

| Profile | What it is for | Used by |
|---|---|---|
| `full` | the whole persona | the chat (TUI), `celeste message`, the MCP `celeste` tool in chat mode, `celeste_content`, `celeste acp` |
| `spine` | a shorter cut for working runs | `celeste agent`, `/agent`, the MCP `celeste` tool in agent mode, `general` subagents |
| `lite` | the smallest cut with her voice | small context windows (below) |
| `off` | the voice boundary rule only | the off level, below |

**The off level.** Lanes that report to celeste rather than talk to you run the off level: her
identity line, the honesty rule, then the `off` profile. That covers `explore` and `review`
subagents and every `/orchestrate` lane (the primary, the debate reviewer and the defense). The
off level is the same in official and source builds.

The persona is always on in chat and agent runs: 2.0 removed `skip_persona_prompt` (see
[MIGRATING-2.0.md](../MIGRATING-2.0.md)).

**Small windows.** A profile may take at most a quarter of the model's context window. When it
would take more, celeste steps down one level at a time (`full` → `spine` → `lite`). `lite` stays
while it fits in half the window; below that only the off level's identity, honesty rule and
voice boundary are kept, never nothing. The chat, `celeste agent` and `celeste message` say once
which profile they use; MCP responses only log it. A local model with no `context_limit` is
assumed to have 8,192 tokens, so chat runs on `lite` there. Set `context_limit` in your config to
the server's real window to get `full`.

Stepping down sizes the persona, not the whole request. The tool definitions need room too
(several thousand tokens in the chat), so on a very small window, the 8,192-token default
included, a request can still exceed the window and compaction starts. A fuller fix is planned
for 2.1 ([#310](https://github.com/whykusanagi/celeste-cli/issues/310)); until then, set `context_limit` to the server's real window.

Every profile ends with the **voice boundary**: her voice applies only to prose addressed to you.
Code, comments, commit messages, file contents and tool arguments are plain and professional.

The persona is the first, byte-stable part of the system prompt. Everything that changes per
request comes after it (sliders, user identity, the mode's rules, project context, git, memories
and the date), so provider prompt caches keep hitting.

## When she doesn't know

In the terminal she looks things up with the tools she has, or says she doesn't know. She never
claims a file was written or audio was saved unless a tool returned that result this turn. Both
rules are part of the official persona, and the second is in the public persona too.

## Sliders (`/persona`)

Flirt, warmth, speech style and lewdness run from 0 to 10, and each value snaps to an authored
anchor at 0, 3, 7 or 10. The slider block comes right after the persona and tunes how she sounds;
it never changes who she is or her core rules. Lewdness applies only with the separate R18 toggle
on. A saved preset renders exactly the same text as the same values set by hand. `spawn_agent`'s
`persona` argument overrides the sliders for one `general` subagent. The sliders work over the
public persona too. Authoring rules: [slider-agent-handoff.md](slider-agent-handoff.md).

There is no override file for the persona itself. `~/.celeste/celeste_essence.json` from 1.x is
no longer read.

## For the maintainer

- `make sync-persona` rebuilds the persona at the commits pinned in
  `cmd/celeste/prompts/persona/SOURCE.json` (in a temporary directory outside the repository) and
  writes only the ciphertext and `SOURCE.json`. Move a pin with `PERSONA_CORE_COMMIT=<sha>`. It
  needs both private checkouts and the key file in `~/.celeste/` (mode 0600).
- `make persona-dev` decrypts the profiles into a temporary directory for reading.
- `make persona-check` rebuilds and compares, then runs the key-gated tests. CI checks the
  ciphertext against `SOURCE.json` without a key.
- `make build` and `make install` include the key when the key file exists.
- Releases get the key from the `CELESTE_PERSONA_KEY` Actions secret. The release fails before
  anything is published if `celeste persona verify` fails on the built binary or any binary's
  build info names the key.
- To rotate the key: put a new 64-hex-character key in the secret and in the key file, run
  `make sync-persona` (every file re-seals because the key id changed), and merge before the next
  release.
