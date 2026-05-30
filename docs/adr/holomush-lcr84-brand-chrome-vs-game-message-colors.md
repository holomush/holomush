<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Software Brand Governs Chrome Tokens Only; Message Colors Are Game-World Content

**Date:** 2026-05-29
**Status:** Accepted
**Decision:** holomush-lcr84
**Deciders:** HoloMUSH Contributors
**Related:** holomush-9ektq

## Context

The web client uses two CSS custom-property namespaces: `--color-*` for UI chrome (background, accents, borders, status) and `--mush-*` for in-game message text (say/pose/system/ooc/…). The 2026-05-28 software brand refresh (`.claude/rules/branding.md`) defined the holographic-terminal identity — cyan accents (`#3dd6f7`/`#1565c0`) on ink, with amber (`#ffb300`) reserved as the cursor only (INV-1). branding.md INV-6 already states the brand is "software/platform only — never the game world / default setting," but the boundary at the *token* level was implicit, and the web client predated the brand entirely (brown/amber, amber-as-accent).

During the web rebrand (holomush-9ektq) the question surfaced concretely: do the 11 `--mush-*` message colors get brand-locked to the cyan palette? The user clarified INV-6's intent — it decouples the software brand from the game/sandbox brand; it is not a ban on a given aesthetic.

## Decision

The software brand palette governs **only** the `--color-*` chrome namespace. The `--mush-*` message-color tokens are game-world content with **no parity obligation** to the software brand: the game/sandbox may adopt any message palette (warm, cyan, custom) independently and copy themes across the boundary without synchronization. INV-1 (amber = cursor only) applies **exclusively** to `--color-*` accent tokens.

This is enforced mechanically: the INV-1 brand-guard test (`themeStore.test.ts`) asserts cyan-dominance and amber-absence on `--color-primary`/`accent`/`ring`/`scrollback.indicator` for the `default-*` themes only — it never inspects `--mush-*`, and it never runs against the `warm-*` themes.

## Alternatives Considered

### A — Decouple `--mush-*` from the brand, no parity obligation (chosen)

Game-message text is authored content, not platform chrome. Decoupling lets warm alternate themes use amber for `--mush-system` without violating the cursor-only rule, and lets game designers/operators define message palettes freely. Cost: the boundary must be documented so future contributors don't re-couple the namespaces.

### B — Brand-lock `--mush-*` to the same cyan palette as chrome

Visual consistency and one palette to maintain, but it forces a parity obligation on game-world presentation, makes the warm-dark theme's amber `--mush-system` a brand violation, and conflates platform identity with authored content. Rejected.

## Rationale

- Brand rules exist so the *platform UI* is recognizable; message text is game content, a different ownership domain.
- The warm alternate themes (holomush-9ektq D3) require amber in `--mush-system`; decoupling makes that coherent rather than a violation.
- Aligns with and operationalizes branding.md INV-6 at the token-namespace level.
- Narrows the INV-1 enforcement surface to a well-defined token subset, making the brand-guard test precise.

## Consequences

**Positive:** operators and game designers define message palettes freely; warm themes are coherent; INV-1 enforcement is a narrow, testable subset.

**Negative:** contributors must learn which namespace governs brand vs content; the boundary lives in docs + the guard test, not in the type system.

**Neutral:** the brand-guard test checks `--color-*` accents only — a direct mechanical reflection of this boundary.

## Implementation

See `docs/superpowers/plans/2026-05-29-web-client-brand-rebrand-contrast.md` Task 3 (INV-1 brand-guard test scoped to `--color-*` on `default-*` themes) and Tasks 4–5 (message `--mush-*` values chosen as game-world content, not brand-locked).

## References

- Spec: `docs/superpowers/specs/2026-05-29-web-client-brand-rebrand-contrast-design.md` §Decisions D4, §Invariants INV-1
- `.claude/rules/branding.md` INV-1, INV-6
- `web/src/lib/stores/themeStore.ts` `MUSH_TOKENS` (the namespace split)
