<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-sfxte; do not edit manually; use `/adr update holomush-sfxte` -->

# Hook focus-cache invalidation at UpdateSessionConnection

**Date:** 2026-07-08
**Status:** Accepted
**Decision:** holomush-sfxte
**Deciders:** Sean Brandt

## Context

The dispatch hot path reads a connection's focus per pose/say/ooc/emit via a synchronous Postgres GetConnection. Adding a cache (holomush-wm0fi) requires invalidation on every focus write. Three coordinator entry points can mutate a live connection's Connection.FocusKey — SetConnectionFocus (set_connection_focus.go:91), AutoFocusOnJoin (auto_focus_on_join.go:149), and RestoreConnectionFocus (restore_connection_focus.go:58) — but all three write inside a mutator passed to session.Store.UpdateSessionConnection, which holds the sole `UPDATE ... SET focus_key` SQL (session_store.go:1206). The wm0fi bead's original framing proposed hooking SetConnectionFocus alone; grounding the write paths showed that misses two of the three live-focus-change paths.

## Decision

Cache invalidation for live-connection focus changes is anchored exclusively to session.Store.UpdateSessionConnection (via a caching store decorator), not to individual coordinator RPCs.

## Rationale

- UpdateSessionConnection is the only method containing the UPDATE...SET focus_key statement; all three coordinator paths funnel through it, so hooking it is complete by construction.
- A storage-layer hook is structurally proof against a future coordinator path being added without updating cache invalidation.

## Alternatives Considered

- **Hook SetConnectionFocus only (bead's original framing) (rejected):** matches the bead title and is one obvious call site, but misses AutoFocusOnJoin and RestoreConnectionFocus — 2 of 3 live focus-change paths would leave stale cache entries uninvalidated.
- **Hook session.Store.UpdateSessionConnection — the sole SQL chokepoint (chosen):** catches all three coordinator paths by construction; one invalidation site instead of N.

## Consequences

- Positive: new coordinator write paths automatically get correct invalidation with no call-site changes.
- Negative: ties the cache decorator's correctness guarantee to store-layer placement, an open placement question deferred to the plan.
- Neutral: connection creation/removal (class B) is treated as best-effort reclaim, a policy separate from this chokepoint.
