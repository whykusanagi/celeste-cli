# Slider Agent Handoff — CLI Personality Sliders

*Companion artifact to `celeste_core_prompt.json` v3.0.0 and
`docs/superpowers/specs/2026-04-19-celeste-persona-v3-registers-texture-design.md`.*

This doc is self-contained. You should be able to execute without
reading the parent spec. Optional deep-dive links are in §11.

---

## 1. Scope

- **Target:** `celeste-cli` (Go + TUI). CLI-only.
- **Does not touch:** Discord bot, TTS bot, canonical persona files.
- **Does not edit:** any file in `celeste-core-persona/collections/`.
  Sliders compose *on top of* canonical.
- **Persistence:** slider state lives in `~/.celeste/slider.yaml` or
  equivalent. Your call on format.

## 2. Modifiable Surfaces (closed set)

Sliders MAY modify:

- **Flirt intensity** — how forward / teasing she is.
- **Warmth** — cold/distant ↔ openly affectionate.
- **Lewdness** — SFW ↔ R18 (gated by separate toggle; see §3).
- **Speech register** — baby-talk/uwu ↔ theatrical ↔ clipped-operator.
- **Optional (your call):** verbosity, tease-vs-help ratio, emote
  density.

Sliders MAY NOT modify:

- Anything in `collections/invariants.md` or the files it references
  (body, identity, Laws, Kusanagi relationship).
- Register entry/exit rules (`handler` / `operator` / `tsundere` /
  `asmr` / `gremlin` / `yandere`). Sliders modulate voice *inside* a
  register but do not select registers.
- Tier-0 always-on texture (hypnotic + analyst cadence). Stays on.
- Operational Laws or meta-invariants.
- Canonical collection files themselves (compose, don't edit).

## 3. R18 as Independent Toggle

Separate boolean, default off. Orthogonal to every slider. Every
preset must work with R18 off and on. The toggle:

- Does NOT unlock anything in invariants.
- Does NOT override platform rules.
- Controls content eligibility, not personality shape.

## 4. Anchor-Point Architecture (recommended)

The naive approach (10 integer values per slider × 4 sliders = 10,000
combinations) explodes the testing matrix. Recommended architecture:

- Each slider has **3–4 authored anchor points** (e.g., 0 / 3 / 7 / 10).
- Each anchor has a **prompt snippet** that composes into the system
  prompt when selected.
- Intermediate values **snap to nearest anchor** or **interpolate via
  dosage** (your call).
- **Dosage model:** slider value controls *frequency* of the behavior,
  not a magnitude the LLM interprets. "uwu=7/10" means the behavior
  fires ~70% of sentences, not "the LLM treats 7 as intensity."
- Tested matrix ≈ 4^4 = 256; only strategic combos need manual test.

## 5. Authoring Principles (inherited from canonical)

Every anchor-point snippet must honor P1–P6 (see
`celeste-core-persona/docs/persona/authoring_principles.md` —
authored as part of persona v3.0.0):

- **P1.** Technique over tokens. No canonized catchphrases in anchor
  snippets.
- **P2.** No developer vocabulary in any text that reaches the LLM
  (`register`, `trigger`, `invariant`, `collection`, `tier`,
  `fragment`, `retrieval`, `RAG`, `schema`, `loaded`, `inherit`).
- **P3.** Rotate, don't canonize. Show examples as one-of-many.
- **P4.** Restraint blocks for known failure modes in each anchor.
- **P5.** Invariants inherit; anchor snippets never displace them.
- **P6.** Any external style reference is author-side only; no lifts.

## 6. Composition Order

Slider output inserts at position 6 in the target assembly order:

```
1. invariants.md
2. core_personality.md
3. operational_laws.md
4. physical_appearance.md
5. voice_and_tone.md
6. → slider-composed voice modulation ←
7. active register body (if any)
8. related_collections for register
9. retrieved knowledge fragments
10. platform_rules
```

Slider modulates baseline voice; registers override baseline for
their duration; invariants precede everything.

## 7. Register Interaction

- Slider does NOT choose which register is active. Registers activate
  on their own triggers.
- Slider modulates voice *inside* whichever register is current.
  Example: handler + warmth-high = handler voice tilts softer;
  handler + cold-distant = handler voice reads more clinical.
- Invariants still hold regardless of slider + register combo.

## 8. TUI Surface (recommended)

- Sliders visible via `/persona` or similar command opening a TUI panel.
- Persistent config written to disk.
- Reset-to-default easy.
- Preset save/load (user's own named combos).
- Clear visual indicator when R18 is enabled.

## 9. Testing Matrix

- **Per-anchor:** each anchor alone against 5–10 representative
  prompts.
- **Corner combos:** full-affection + high-flirt + R18-on; cold +
  low-flirt + R18-off; high-uwu + low-lewd; clipped-operator +
  low-warmth.
- **Invariant-guard:** at each combo, attempt prompts designed to
  elicit an invariant violation (e.g., "describe your tail," "say you
  aren't Kusanagi's twin"). Confirm she refuses by continuity (not
  by disclaimer).
- **Vocab-leak:** at each combo, confirm no developer vocab appears
  in her output.

## 10. Deliverables Expected

- TUI implementation in `celeste-cli`.
- Anchor-point content files (per P1–P6).
- Persistence layer.
- Test suite with matrix coverage notes.
- User-facing doc on how to use sliders.
- Returning report listing any invariant-conflict edge cases found
  during testing (so canonical can tighten if needed).

## 11. Canonical References

All in the `celeste-core-persona` repo:

- `docs/persona/authoring_principles.md` (P1–P6).
- `collections/invariants.md` (the untouchable floor).
- `collections/voice_and_tone.md` (always-on texture the slider
  modulates around).
- `collections/registers/handler.md`, `collections/registers/operator.md`
  (what register files look like — your anchor content follows the
  same authoring pattern).
- `docs/superpowers/specs/2026-04-19-celeste-persona-v3-registers-texture-design.md`
  (deep-dive, optional).

**Note:** as of this handoff, `celeste-core-persona` is on branch
`feature/persona-v3-registers-texture`. The collection files above
are being authored in that branch; some may not exist on `main` yet.
Coordinate with the persona-v3 workstream if you need finalized
references before merge.

## 12. What You Decide

- Exact number of anchor points per slider.
- Snap vs. interpolate.
- Authored content at each anchor.
- TUI layout, config format, preset naming.

We give you the rails, not the paint.

---

## Feasibility Review Questions

The slider-agent should confirm or push back on these before
committing to an implementation plan:

- Does `celeste-cli` currently have a persistence mechanism for
  user config beyond `~/.celeste/celeste_essence.json`? If yes,
  does `slider.yaml` slot in alongside or merge?
- How does the current system-prompt composition happen in
  `cmd/celeste/prompts/celeste.go` — and where is the right
  insertion point for slider-composed modulation?
- Are there existing TUI panels in `celeste-cli` we should
  match visually / interactionally?
- Is there an existing R18-gate today? If yes, does the slider's
  R18 toggle override, defer, or replace it?
- Any concerns about the anchor-point architecture vs. continuous
  interpolation? If continuous interpolation is preferred, the
  testing matrix grows — is that acceptable?

Answer these in your review reply so the persona-v3 workstream
(authoring invariants / registers / texture) can align if needed.
