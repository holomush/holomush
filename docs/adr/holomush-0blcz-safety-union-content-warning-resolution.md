<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Safety-Union Resolution for Content-Warning Block Settings

**Status:** Accepted
**Decision:** holomush-0blcz
**Design bead:** holomush-iokti
**Date:** 2026-05-30

## Context

Content-warning **block** preferences (the categories a player never wants shown)
exist at GAME, PLAYER, and CHARACTER scope. The host settings `Chain`
(`internal/settings/chain.go`) resolves scalar settings **first-match-wins** —
the most-specific scope that has a value wins and shadows the rest.

For a safety-sensitive block list, first-match-wins is hazardous: a character-scope
list that omits a category would silently *discard* a player-level boundary,
exposing the human to content they explicitly blocked. The blocked set must be
**monotone** — adding a block at any scope may only enlarge it.

## Decision

`core-scenes` resolves `content.cw_block` as the **union** of the GAME, PLAYER,
and CHARACTER scope lists, read via three explicit single-scope `GetSetting`
calls and unioned plugin-side. The substrate `Chain`'s first-match-wins semantic
is deliberately **not** used for this setting. The substrate is unchanged; the
union is the safety-aware consumer's own composition over single-scope primitives.

A `SETTING_SCOPE_CHAINED` RPC mode (host-side chain merge) is **not** provided.

## Rationale

- Content warnings are a safety feature; the blocked set must be monotone under
  scope composition (INV-6). Override-by-specificity inverts that property.
- The chained RPC mode's first-match-wins result is simply *wrong* for this
  consumer — so it would be dead, misleading surface even if built.
- Keeping the union in the consumer keeps the substrate generic: a single-scope
  read primitive serves both first-match (host `Chain`) and union (safety)
  composition without the substrate privileging either.

## Alternatives Considered

- **First-match-wins (substrate `Chain` semantic).** Rejected: a character-scope
  block omitting a category discards the player's boundary for it; safety is not
  monotone under override.
- **`SETTING_SCOPE_CHAINED` RPC mode.** Rejected: returns first-match-wins (wrong
  semantic for the only consumer); a single `principal_id` cannot resolve
  character→player without a host-side join; dead wire surface.

## Consequences

- **Positive:** player-level boundaries are inviolable regardless of
  character-scope configuration; substrate `Chain` semantics and existing
  consumers are untouched; no `SETTING_SCOPE_CHAINED` dead surface.
- **Negative:** three `GetSetting` reads per board query (mitigated — these are
  cheap local DB reads, not cross-service RPCs); the union pattern is plugin-local,
  so future safety-setting consumers must apply it deliberately.
- **Neutral:** the divergence from `Chain` is intentional and recorded in INV-6,
  not an inconsistency.

## References

- Spec: `docs/superpowers/specs/2026-05-29-scenes-phase-8-board-content-warnings-design.md` §3.4, INV-6
- Related: `holomush-74ib4` (owner-partitioned settings substrate the reads run against)
- Design bead: `holomush-iokti`
