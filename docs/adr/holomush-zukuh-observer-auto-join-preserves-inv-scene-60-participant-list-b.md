<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-zukuh; do not edit manually; use `/adr update holomush-zukuh` -->

# Observer Auto-Join Preserves INV-SCENE-60 Participant-List Boundary

**Date:** 2026-06-07
**Status:** Accepted
**Decision:** holomush-zukuh
**Deciders:** Sean Brandt (seanb4t)

## Context

The spec needed a "watch" mechanism for open scenes without breaking INV-SCENE-60, which makes the participant list the sole privacy boundary for scene-log reads (plugin-code-enforced; ABAC never in the path — see decision holomush-c8a9, which this PRESERVES). An earlier iteration proposed a spectate-focus mode that would read-only-focus a scene without participant-list membership.

## Decision

"Watch" on an open scene calls JoinScene with role=observer, gated by a plugin-code-enforced visibility==open check evaluated BEFORE the ABAC spectate action (INV-SCENE-61). Observers are real participants: visible in the roster, kickable, excluded from emit/pose-order/publish-votes by role gates. INV-SCENE-60 is unchanged.

## Rationale

- The participant list as sole privacy boundary is the load-bearing invariant of the scene privacy model; weakening it would require auditing every downstream consumer.
- Watchers as real members means the focus coordinator, filter-at-delivery, and replay-tail machinery are untouched — correctness by construction.
- Observers are socially visible (RP consent norm) and moderatable, matching MUSH conventions.
- Telnet `scene watch` parity is free: same JoinScene path.

## Alternatives Considered

**Spectate-focus without membership** — rejected. No schema change and invisible watchers, but violates INV-SCENE-60 (a non-participant receives scene events), and the coordinator + filter-at-delivery would need a parallel membership-lite trust path.

**Observer auto-join (chosen)** — invariant holds verbatim; costs a plugin migration widening the scene_participants role CHECK and role-aware gates at emit, pose order, publish votes, idle nudge.

## Consequences

- Positive: INV-SCENE-60 unchanged; no new streaming trust paths; telnet parity structural; observers get scene_activity badges like any member.
- Negative: plugin migration required (role CHECK constraint + ParticipantRoleObserver constant); every participant-keyed surface must be role-audited (publish-vote roster already filters owner/member structurally).
- Neutral: observer→player upgrade is a thin path on the existing join flow.

Related (NOT superseded): holomush-c8a9 "Enforce Scene Privacy at Plugin Code, Not ABAC Engine" — this decision deliberately preserves it. Spec: 2026-06-07-web-portal-scenes-design.md §3 D6, §7. Bead: holomush-5rh.8.
