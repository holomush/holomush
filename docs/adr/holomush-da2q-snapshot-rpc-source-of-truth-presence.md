<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Snapshot RPC as Source of Truth for Current-State Presence

**Date:** 2026-05-19
**Status:** Accepted
**Decision:** holomush-da2q
**Deciders:** HoloMUSH Contributors

## Context

The web terminal populated presence by replaying `arrive`/`leave` events from the Subscribe stream
(`web/src/routes/(authed)/terminal/+page.svelte:381-401`). When a session B joins a location after
session A is already present, A's `arrive` event predates B's `LocationArrivedAt` privacy floor
(I-PRIV-1, established by the history-scope-privacy epic `holomush-iwzt`) and is correctly
filtered out by the Tier 2 filter-at-delivery in `internal/grpc/server.go::dispatchDelivery`.

The privacy filter is working as designed; the presence-via-event-replay UX is the bug. Event
replay cannot recover what a correct privacy filter suppresses, and the test
`web/e2e/terminal.spec.ts:136` ("presence list shows self and other connections") only passes
today because the Tier 2 filter is effectively a no-op in production (`holomush-ofpi`). Once
`holomush-iwzt.15` lands, the test fails for the correct privacy reason — proving the presence
model needs to change.

## Decision

Presence MUST be populated by a current-state query (`CoreService.ListFocusPresence`) at Subscribe
open. Subscribe `arrive`/`leave` events apply additively with idempotent semantics keyed by
`character_id`. The snapshot is the source of truth for "who is here now"; the event stream is
the source of truth for changes.

This is the canonical pattern for current-state surfaces that must coexist with privacy-filtered
event streams. Future per-stream presence RPCs MAY layer on top of the same `session.Store` and
ABAC machinery; the wire shape introduced here is forward-compatible.

## Rationale

- **Event replay cannot reconstruct state suppressed by a correct privacy filter.** The two
  concerns are structurally incompatible. The snapshot bypasses the temporal floor because it
  answers "who is here right now," not "what happened in history" — a categorical, not
  quantitative, distinction. (See ADR `holomush-o46k` for the privacy boundary.)
- **Idempotent `Map<characterID>` semantics resolve any race by construction.** Whether a session
  appears in the snapshot AND in the buffered Subscribe stream (when both ran in parallel), or in
  the snapshot but already left before the event drain, the `Set` semantics produce the right
  answer with no special-case race resolution.
- **The pattern generalises.** Any future current-state surface (scene occupants, follower lists,
  etc.) that must coexist with privacy-filtered streams will hit the same problem and benefit from
  the same shape.

## Alternatives Considered

- **Per-stream presence RPC.** Caller specifies which stream context to query; server returns
  members for that specific stream. More granular and orthogonal but premature: the immediate UX
  needs one list, scene context isn't yet defined, and the underlying primitive is the same.
  Deferred as an incremental future addition; the response wire shape (`PresenceContext` enum,
  `context_id` field) stays forward-compatible.
- **Continue deriving presence from event replay.** Structurally broken once the Tier 2 filter is
  active. Cannot reconstruct what was correctly suppressed.

## Consequences

**Positive**

- Presence is correct the moment Subscribe opens, regardless of when prior sessions arrived.
- The backfill→presence code path is removed entirely, simplifying the client
  (`web/src/routes/(authed)/terminal/+page.svelte:380-394` block deletion; resolves the
  `holomush-1tvn.15` TODO).
- Establishes a reusable pattern for future current-state RPCs.

**Negative**

- New `CoreService.ListFocusPresence` RPC, gateway proxy, and ABAC seed entry required.
- Two parallel round-trips at Subscribe open (backfill + snapshot).

**Neutral**

- Subscribe `arrive`/`leave` events remain authoritative for changes; only initial state moves to
  the snapshot. The event stream is unchanged.

## References

- Spec: `docs/superpowers/specs/2026-05-19-presence-snapshot-design.md` (§1 Problem, §2 D-8
  client model, §5.2 flow ordering)
- Plan: `docs/superpowers/plans/2026-05-19-presence-snapshot.md`
- Parent bead: `holomush-5b2j`
- Related ADR: `holomush-o46k` (snapshot exempt from I-PRIV-1 floor)
- Related ADR: `holomush-lp65` (`list_presence` ABAC action)
- Related: `holomush-iwzt.15` (Tier 2 filter-at-delivery), `holomush-1tvn.15` (resolved TODO)
