# Release Checklist

Reusable quality gate for releasing **celeste-cli** and **celesteops**. Work top
to bottom; most gates have a copy-paste command. CI enforces the starred (★) ones
too, but running them locally first avoids red pipelines.

> Convention: this repo releases via **release-please** + signed tag builds. You
> don't hand-tag or hand-bump versions. Merging the release-please PR cuts the
> tag, which triggers the signed multi-platform build.

---

## 0. PR queue hygiene (do this FIRST)
The security scan, `govulncheck`, and the binary smoke test only mean something if
they run against the dependencies you will actually ship. Clear the open-PR queue
BEFORE the gates below. Releasing over a backlog of pending dependency updates
means your security review validated stale modules.
```bash
gh pr list --state open --author app/dependabot   # dependency + CI-action bumps
gh pr list --state open                            # everything else
```
- [ ] Merge or close every pending **Dependabot** PR (dep + action bumps), or consciously defer one with a written reason. Do not ship a release with open dep bumps sitting in the queue.
- [ ] Any PR **stuck** (not draining)? Diagnose it BEFORE writing new code or cutting the release: behind `main` (rebase / `gh pr update-branch`), red on a **flaky test** (fix the flake at its root, do not just re-run), `go.sum` cascade (merge serially), or genuinely broken (fix on a branch or close). A stuck queue is a signal, not noise.
- [ ] After the queue is clean, re-run §1 (build) and §2 (govulncheck/security) so they reflect the **shipping** dependency set.

## 1. Code quality gates (must be green)
```bash
go build ./...                                   # ★ compiles, all targets
gofmt -l .            # ★ output MUST be empty   (CI fails on any listed file)
go vet ./...                                     # ★
golangci-lint run --timeout=5m                   # ★
go test ./... -race                              # ★ full suite, race detector
go mod tidy && go mod verify                     #   no unexpected go.mod/go.sum diff
```
- [ ] All of the above pass
- [ ] Coverage ≥ threshold (CI enforces ≥5%)
- [ ] No stray/debug files committed (`git status` clean; no scratch scripts, logs, binaries, `.mp3`/test artifacts)
- [ ] No leftover `TODO`/`FIXME`/`panic("unimplemented")` introduced by this change

## 2. Security
```bash
govulncheck ./...                                # ★ 0 vulns in CALLED code
git diff main...HEAD | grep -iE 'api[_-]?key|secret|token|password|PRIVATE KEY'   # expect no hits
```
- [ ] govulncheck clean (note: unreachable transitive vulns are acceptable, same as prior releases)
- [ ] No secrets in the diff (gitleaks runs in CI; committed public keys are fine)
- [ ] No live credentials in committed configs/tests
- [ ] **Rotate any API key used for testing** before/after release
- [ ] New outbound endpoints / new dependencies reviewed (supply chain)

## 3. Review
- [ ] Self or peer review of the full diff (`git diff main...HEAD`)
- [ ] **AI review (Fugu/Celeste)**: feed the code diff and triage findings, fix the valid ones:
  ```bash
  git diff main...HEAD -- '*.go' scripts > /tmp/rev.txt
  # prompt: "Review inline, no tools. Severity/file/one-line fix. Flag release blockers."
  celeste -config sakana agent --goal-file /tmp/rev_prompt.txt --planner=false --max-turns 1
  ```
- [ ] **Smoke-test the real binary** (`make build`), not just unit tests: run the top user flows for every changed feature (e.g. a model/provider: chat + tool call + the feature it touches)
- [ ] **`make smoke`** passes — automates the above: builds the binary, renders every TUI component to `test-output/tui/*.png` (gitignored), and drives new-feature CLI paths incl. **one live model call** (sakana/fugu) so model wiring is proven end-to-end. Requires `freeze` (`go install github.com/charmbracelet/freeze@latest`).
  - [ ] **Eyeball the TUI PNGs** in `test-output/tui/` — every visual component (status line, key hints, tool-call cards, ask prompt, `/mcp` panel) renders correctly. Regenerate anytime with `make tui-snapshots`.

## 4. Documentation
- [ ] README updated: feature lists, counts, examples
- [ ] CHANGELOG: release-please auto-generates from conventional commits; **verify it reads correctly**
- [ ] Version references consistent: no stale `vX.Y.Z` in docs; any "N <things>" count matches reality
- [ ] **Every new command / flag / feature has a doc home** — new subcommand or flag → README usage + the relevant `docs/*.md`; new capability → a runnable example. (Grep the diff for user-facing surface: `git diff main...HEAD -- cmd/ | grep -E 'case "|flag\.(String|Bool|Int)'`.)
- [ ] Breaking changes + migration notes called out
- [ ] **Setup/onboarding commands actually run**: execute them, don't assume (we have shipped broken setup hints before)
- [ ] **Zero-context doc validation** (superpowers:zero-context-validation): spawn a blind sub-agent given ONLY the install/setup docs and have it attempt the flow (e.g. "install celeste + register it as a Claude Desktop MCP server"). Any gap it reports is a real blindspot — fix the docs, re-test until it succeeds without assumptions.
- [ ] **De-slop new prose** (stop-slop): run new user-facing prose (README/docs sections, release notes) through stop-slop — active voice, specific, no adverbs / em-dashes / filler.

## 5. Versioning & release mechanics (release-please)
- [ ] Commit type sets the bump: `feat:` → minor, `fix:` → patch, `feat!:` / `BREAKING CHANGE:` → major
- [ ] Version constants left for release-please (don't hand-edit the `x-release-please-version` markers)
- [ ] Version-dependent tests assert against the version constant, not a literal (so the auto-bump doesn't break CI)
- [ ] Merge to `main` → review the release-please PR's generated CHANGELOG + version → merge it to tag

## 6. Signing & verification (celeste-* GPG)
- [ ] Release artifacts are GPG-signed (`checksums.txt.asc`, `manifest.json.asc`)
- [ ] The in-repo public key (`whykusanagi.asc`) includes the **current signing subkey**: releases are signed by a subkey GitHub/Keybase may not carry
  ```bash
  gpg --show-keys --with-subkey-fingerprint whykusanagi.asc | grep '\[S\]'   # must be present + unexpired
  ```
- [ ] `./scripts/verify.sh <artifact>` passes end-to-end → **"Good signature"**
- [ ] Primary fingerprint cross-checks against `https://github.com/whykusanagi.gpg`
- [ ] Multi-platform artifacts present (linux/darwin/windows × amd64/arm64)
- [ ] Install path works (`curl … | bash`, or `make install`)

## 7. Post-release
- [ ] **Dogfood the verify flow**: download the published release, run `verify.sh` → Good signature + checksum OK
- [ ] **Announce only AFTER the tag exists** (X post / blog: copy usually says "vX is live")
- [ ] Rotate temporary test credentials
- [ ] Update dependent repos / install docs / homebrew etc. if applicable
- [ ] Watch CI and the issue tracker for early breakage

---

## Project-specific notes
**celeste-cli**: adding a provider touches several enumerations; update ALL of them:
`README.md`, `docs/CAPABILITIES.md`, `docs/LLM_PROVIDERS.md`, `docs/PROVIDER_AUDIT_MATRIX.md`,
plus `providers/registry.go`, `providers/models.go`, `context/budget.go`, `costs/pricing.go`.
Codegraph/agent tools register per `--workspace`; smoke-test agent flows with a real workspace.

**celesteops**: ships a signed macOS app (currently ad-hoc, not Apple-notarized); keep the
quarantine-clear step in `VERIFY.md` accurate. Same GPG signing model as celeste-cli (repo key
is authoritative; GitHub serves the primary fingerprint as the trust anchor).

## Hard-won checks (things that actually bit us)
- Signing **subkey** not published to GitHub/Keybase → verify from the repo key, not external sources.
- `config --set-*` must target the `-config <name>` profile, not clobber the default.
- Tool-output writers (TTS, exports) must honor `--workspace`, not the process cwd.
- A turn must never declare more `tool_calls` than the tool results it returns (strict APIs 400).
- Don't ship a setup command in docs without running it first.
- Drain the PR queue (esp. Dependabot dep bumps) BEFORE the security scan + binary smoke test, or you're reviewing/shipping stale modules (v1.11.0 shipped with 12 pending dep PRs). See §0.
- A "stuck" PR queue usually means a flaky test or branch-protection serialization, not Dependabot being slow. Diagnose the root before assuming it'll drain on its own.
