<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# `foreground` and `input` Are Themed `ThemeColors` Keys, Not `@theme`-Only Statics

**Date:** 2026-05-29
**Status:** Accepted
**Decision:** holomush-poxub
**Deciders:** HoloMUSH Contributors
**Related:** holomush-9ektq, holomush-lcr84

## Context

The web client's runtime theming works by mapping a theme JSON's keys to `--color-*`/`--mush-*` custom properties (`themeToCssVars()`) applied inline on `.app-root`; the Tailwind v4 `@theme` block in `web/src/app.css` carries build-time static defaults. `--color-foreground` and `--color-input` existed **only** as `@theme` statics — there was no `foreground` or `input` key in the `ThemeColors` type or the theme JSONs, so they could not be overridden per theme at runtime. Body text and input borders therefore rendered the same color regardless of the selected theme — a latent bug exposed once the rebrand added a light brand theme (which needs a dark foreground where the dark theme needs a light one).

## Decision

`foreground` and `input` are promoted to full `ThemeColors` keys, present in every theme JSON and emitted by `themeToCssVars()` as `--color-foreground` / `--color-input`, overriding the `@theme` statics at runtime. This codifies a general rule: **any token whose value differs between themes MUST be a `ThemeColors` key**, not an `@theme`-only static. The `@theme` statics remain as pre-hydration/SSR/unstyled-fallback defaults.

## Alternatives Considered

### A — Promote `foreground` and `input` to `ThemeColors` keys (chosen)

Runtime theme switching correctly overrides body text and input borders; consistent with every other theme-variant token; `themeToCssVars()` already maps any non-MUSH key to `--color-<key>`, so promotion needs only the type + JSON edits, no store-logic change. `pnpm check` enforces completeness — a missing key in any theme JSON is a type error.

### B — Leave them as `@theme` statics only

No type/JSON churn, but light and dark would share one foreground/input color — broken runtime theming for body text and input borders on light themes. Rejected: it is the bug, not a viable option.

## Rationale

- A single build-time static cannot serve both a light and a dark foreground; the value is inherently theme-variant, which is exactly what `ThemeColors` is for.
- `ThemeColors` is the contract for runtime-themeable tokens; the rule "theme-variant ⇒ `ThemeColors` key" makes that contract complete and gives future contributors a clear test for where a new token belongs.
- The mapping is automatic in `themeToCssVars()` (`themeStore.ts:83-93`), so the cost is type + data, not logic.

## Consequences

**Positive:** all four themes render correct body-text/input-border colors; `pnpm check` mechanically enforces JSON completeness.

**Negative:** all theme JSONs and the `ThemeColors` type must stay in sync; every new theme must supply these keys (and the other new tokens `cursor`, `scrollback.replayed`).

**Neutral:** the `@theme` statics persist as fallback defaults for non-themed/unhydrated contexts.

## Implementation

See `docs/superpowers/plans/2026-05-29-web-client-brand-rebrand-contrast.md` Task 1 (add keys to `ThemeColors`) and Tasks 2/4/5 (populate all theme JSONs); INV-6 token-completeness test in Task 3.

## References

- Spec: `docs/superpowers/specs/2026-05-29-web-client-brand-rebrand-contrast-design.md` §Token specification (reconciliation note), §Invariants INV-6
- `web/src/lib/stores/themeStore.ts:83-93` (`themeToCssVars` non-MUSH auto-map)
- `web/CLAUDE.md` "Theme System" → "Adding a New Theme Token"
