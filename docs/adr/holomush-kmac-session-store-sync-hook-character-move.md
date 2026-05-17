<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Session-Store Sync Hook on Character Move

**Date:** 2026-05-17
**Status:** Accepted
**Decision:** holomush-kmac
**Deciders:** HoloMUSH Contributors

## Context

Grounding during the `holomush-iwzt` history-scope design discovered that no code path syncs `sessions.location_id` to `characters.location_id` on character move. `world.Service.MoveCharacter` (`internal/world/service.go:759-790`) updates the character row and emits an `EventTypeMove` event. The Subscribe-side `locationFollower` (`internal/grpc/location_follow.go:60-130`) consumes the event per-Subscribe-stream and switches the bus filter, but it operates on per-stream in-memory state only — it never writes the session store.

As a result, the session-store entry is stale relative to the character row after any move. Confirmed via `rg "UpdateLocation"` across `internal/` — no path writes `sessions.location_id` on move.

The privacy fix (`holomush-iwzt`) requires per-session `LocationArrivedAt` to update atomically with `LocationID` on move. The session-store sync hook is a prerequisite for the privacy fix; it also corrects a latent drift that may already affect other consumers reading `session.LocationID`.

## Decision

**Add an explicit session-store sync hook to the character-move path.** Define `session.Store.UpdateLocationOnMove(ctx, characterID, newLocationID, arrivedAt) error` that atomically updates `LocationID` and `LocationArrivedAt` for all `Active`/`Idle` sessions belonging to the character (single transaction). `world.Service.MoveCharacter` invokes the hook immediately after `characterRepo.UpdateLocation` and before the move-event emit.

The wiring approach (the plan stage selects between the two options below) preserves the gateway/world layering invariant:

- **Preferred:** a `MovementHook` interface implemented by core-server (which holds both `world.Service` and `session.Store` dependencies). Keeps `world.Service` ignorant of sessions.
- **Alternative:** direct `session.Store` injection into `world.Service`. Simpler but introduces cross-layer coupling.

## Rationale

**Privacy fix prerequisite.** Without the sync hook, `SessionInfo.LocationArrivedAt` cannot be set at the moment of character move — silently leaking history at the moment the per-session floor is most relevant.

**Latent drift correction.** Any consumer reading `session.LocationID` to make a routing or policy decision has, until this fix, observed a value that could be stale relative to the character's actual location. The plan stage MUST audit existing readers; the design-reviewer round 3 verified the drift via grep across `internal/`.

**Atomic invariant.** At the moment a Move event reaches any subscriber, every session in the store MUST already reflect the new `LocationID` and `LocationArrivedAt`. The hook fires before the event emit to maintain this ordering. Asynchronous reconciliation (e.g., consuming the move event in a separate handler) would create a window during which a session's stored `LocationID` lags the character's.

## Alternatives Considered

**A. Asynchronous reconciliation via move-event consumer.** Rejected: race window between event emit and sync write would leak through `QueryStreamHistory` using the still-stale `session.LocationID`.

**B. Direct `session.Store` injection into `world.Service`.** Considered: simplest implementation, but violates the gateway/world layering invariant — `world.Service` knowing about sessions creates coupling that constrains future world-service evolution.

**C (selected): `MovementHook` interface implemented by core-server.** Preserves layering; core-server already holds both dependencies; same pattern useful for future world mutations that need session-store side effects.

## Consequences

- **MUST** update sessions atomically with character-row update in the same transaction context, or in a tightly-following synchronous call before the event emit.
- **MUST** audit current readers of `session.LocationID` to confirm none rely on the previously-stale value.
- **MUST NOT** treat the sync as best-effort — fire-and-forget would re-introduce the original drift.
- Adds a new architectural seam (`MovementHook`) that future world mutations may also want to use.
- Phase 1 of the privacy fix is privacy-leak-neutral but is NOT semantically inert: this drift correction is a positive change that may surface previously-masked bugs in `session.LocationID` consumers.

## References

- Spec: `docs/superpowers/specs/2026-05-17-history-scope-privacy-design.md` §5.1, §7 Phase 1
- Bead: `holomush-iwzt`
- Related ADRs: `holomush-rc8b` (per-session attach intervals — depends on this fix)
- Code: `internal/world/service.go:759-790`, `internal/grpc/location_follow.go:60-130`, `internal/session/store.go`
