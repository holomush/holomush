<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Per-Session Attach Intervals on SessionInfo for Multi-Session Continuity

**Date:** 2026-05-17
**Status:** Accepted
**Decision:** holomush-rc8b
**Deciders:** HoloMUSH Contributors

## Context

HoloMUSH characters may have multiple concurrent sessions: web tab plus telnet, two devices, web plus iOS. The history-scope privacy invariant (`holomush-iwzt`) is principled at the character level — "a character sees stream history only when actively present" — but the implementation must decide whether to track attach intervals per character (one timestamp per character row) or per session (one timestamp per session row).

The design-reviewer round 2 initially recommended storing `LocationArrivedAt` on the `world.Character` row because it eliminated a session-store sync question. Subsequent analysis of multi-session scenarios revealed that approach breaks legitimate continuity: if a web session drops while telnet stays connected, on web reconnect the character was continuously present via telnet, yet a character-row floor that re-stamps on every session attach would retroactively shrink telnet's visibility — wrong.

## Decision

**Store `LocationArrivedAt` on `SessionInfo` (per-session), not on `world.Character` (per-character).** Each session has its own floor, independent of other sessions for the same character. Query-time floor is read from the requesting session's row only — no aggregation across sessions required.

## Rationale

**Multi-session correctness.** A character continuously attached via at least one session retains that session's visibility. A dropping-then-reconnecting session loses only its own visibility, not the character's overall continuity. The per-session model is honest about per-session attach intervals rather than collapsing to a single per-character timestamp.

**Per-session is the natural granularity.** Sessions already carry transport-level lifecycle (`StatusActive`, `StatusIdle`, `StatusDetached`). A timestamp anchored to lifecycle transitions belongs on the same entity as the lifecycle itself. Character rows do not track per-attach state and adding it there would create a new coupling between world model and session lifecycle.

**No aggregation cost at query time.** Floor is a scalar read from the same session row already loaded for authorization. Per-character aggregation would require enumerating active sessions for the character and computing `MIN` — both query latency cost and complexity cost, both contributing to the connect-to-live latency concern tracked at `holomush-87qu`.

## Alternatives Considered

**A. `world.Character.LocationArrivedAt`, updated on every session create/reattach/move.** Rejected: breaks multi-session continuity. Web reattach would steal telnet's visibility.

**B. Hybrid: `character.last_move_time` (written on move only) plus per-session attach intervals, with query-time `MIN` aggregation across active sessions.** Rejected: more storage and more code paths; the simpler per-session model handles every case the user articulated.

**C (selected): `SessionInfo.LocationArrivedAt` per-session, no character-row column.**

## Consequences

- **MUST** synchronize `session.LocationID` and `session.LocationArrivedAt` on character move across all active sessions for the character (see ADR `holomush-kmac` — session-store sync hook).
- **MUST** update `LocationArrivedAt` on three lifecycle transitions: fresh `SelectCharacter` (create), `SelectCharacter` reattach, `Subscribe.ReattachCAS` (transport reattach).
- **MUST NOT** update `LocationArrivedAt` on idle status changes — idle is still "actively attached" per the privacy principle.
- Each session's floor is independent; debugging multi-session privacy issues requires inspecting per-session state in the session store, not a single character row.
- Schema: `sessions.location_arrived_at TIMESTAMPTZ NOT NULL DEFAULT NOW()` added in Phase 1.

## References

- Spec: `docs/superpowers/specs/2026-05-17-history-scope-privacy-design.md` §2 (multi-session worked example), §4.1, §5, I-PRIV-4
- Bead: `holomush-iwzt`
- Related ADRs: `holomush-kmac` (character-move session-sync hook — the prerequisite)
- Code: `internal/session/session.go:154-158` (SessionInfo struct)
