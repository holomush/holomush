<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-sv1ei; do not edit manually; use `/adr update holomush-sv1ei` -->

# Resolve scene roster names host-side via facade, not plugin

**Date:** 2026-06-21
**Status:** Accepted
**Decision:** holomush-sv1ei
**Deciders:** Sean Brandt

## Context

Three web scene surfaces rendered character ULIDs instead of display names because the `core-scenes` plugin stubs `ParticipantInfo.CharacterName = id`. Two plugin-side resolution approaches were evaluated and rejected before settling on facade-side resolution mirroring the established `ListFocusPresence` pattern. The decision constrains where all future scene roster enrichment lives and why plugin-side resolution is structurally unsafe. Originating bug: holomush-5rh.25; design: holomush-vdy2z.

## Decision

Scene roster display names MUST be resolved host-side in the `GetSceneForViewer` facade (`internal/grpc`) via `characterNameResolver.Names()`, after the plugin's collection-level ABAC gate has already authorized the roster. Plugin-side resolution is structurally prohibited because both available transport paths are gated by constraints incompatible with cross-location scene membership.

## Rationale

- The `seed:player-character-colocation` guard on `WorldService.GetCharacter` silently degrades cross-location roster reads to ULIDs, reproducing the bug under a different name.
- The binary plugin host intentionally exposes no `world.query` host capability (`WorldQuerier() -> nil`); Lua-only by design. Plugin-side resolution would require opening a new trust surface (retired epic holomush-q42fh).
- The facade already holds an authorized roster post-`resolveAndGate`; name enrichment of an authorized set is downstream display, not a new ABAC decision — identical reasoning to `ListFocusPresence`.
- Resolution in `internal/grpc` (not the web BFF) preserves the gateway-boundary invariant: the BFF proxies, the facade computes.
- Best-effort semantics (ULID fallback on miss) prevent resolver failures from surfacing as RPC errors.

## Alternatives Considered

- **Host-side resolution in facade via characterNameResolver (chosen):** mirrors the proven ListFocusPresence pattern — one collection-level ABAC gate, then a `characterNameResolver.Names()` batch with no per-character ABAC; the resolver sees an already-authorized set so the co-location seed is irrelevant.
- **Plugin resolves via world.query host capability (rejected):** the binary plugin host intentionally exposes no `world.query` surface (`WorldQuerier() -> nil`, Lua-only); would re-open deliberately-retired epic holomush-q42fh and violate plugin-runtime-symmetry.
- **Plugin resolves via WorldService.GetCharacter (rejected):** gated by `seed:player-character-colocation`, which denies reads of non-co-located characters; scene participants routinely span locations, so roster reads would silently fall back to ULIDs — indistinguishable from the original bug.

## Consequences

- Positive: cross-location scene participants (the common case) get display names; the pattern is reusable for future roster-like surfaces; no new ABAC attributes or policies required.
- Negative: `SceneAccessServer` gains a `characterNameResolver` dependency + a wiring change; the telnet roster remains unresolved (separate follow-up, out of scope).
- Neutral: ULID fallback is preserved on resolver miss / deleted character (no regression); the pose author name (#3) is resolved differently — the dispatcher stamps `req.CharacterName` at command dispatch, no resolution needed.
