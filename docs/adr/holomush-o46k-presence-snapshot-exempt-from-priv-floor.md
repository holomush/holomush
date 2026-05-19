<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Current-State Presence Snapshot Exempt from I-PRIV-1 Temporal Floor

**Date:** 2026-05-19
**Status:** Accepted
**Decision:** holomush-o46k
**Deciders:** HoloMUSH Contributors

## Context

I-PRIV-1 (history-scope-privacy spec, `docs/superpowers/specs/2026-05-17-history-scope-privacy-design.md`)
gates which **events** a session may receive based on a per-session `LocationArrivedAt` floor.
The history-scope-privacy spec treats this floor as universal for event delivery, with both
`QueryStreamHistory` and `Subscribe` enforcing it via Tier 1 (NATS `DeliverByStartTimePolicy`) and
Tier 2 (in-process filter-at-delivery, ADR `holomush-ghpx`).

The new `CoreService.ListFocusPresence` RPC (ADR `holomush-da2q`) answers "who is here right now"
— a current-state fact identical to what any person in the room can observe by looking around.
Subjecting this snapshot to the same temporal floor would produce the wrong answer: new arrivals
would see an empty room until prior occupants individually crossed the floor threshold (which
they never will, because they were already there). Presence would never converge.

## Decision

The presence snapshot RPC is **exempt** from the I-PRIV-1 temporal floor. `PresenceEntry` MUST
contain exactly three fields — `character_id`, `character_name`, `state` — with **no
timestamps**, no `arrived_at_ms`, no first-seen / last-active duration data. The carve-out is
scoped narrowly to set membership and current state.

## Rationale

- **Current-state membership and duration-of-presence are different privacy categories.**
  I-PRIV-1's temporal floor protects the latter (how long someone has been here). The former
  (who is here) is already disclosed to any co-located character via the existing
  same-location `read` permission on the character resource. The snapshot exposes nothing new
  beyond what the event stream would disclose under non-filtered conditions.
- **Minimal-field exposure closes the side-channel.** Without timestamps, the snapshot cannot
  leak duration-of-presence even indirectly. A reader sees the set; they cannot reconstruct
  who arrived first. Invariants I-PRES-2 (floor exemption) and I-PRES-7 (exactly three
  fields, no timestamps) together encode the boundary.
- **Privacy model remains coherent.** The temporal floor still governs *all* event delivery
  through `Subscribe` and `QueryStreamHistory`; the snapshot is a narrowly-scoped exemption
  that doesn't touch either path.

## Alternatives Considered

- **Apply the temporal floor to the snapshot.** Wrong answer: a new arrival who just crossed
  the floor would see the room as empty even though others are visibly present. Defeats the
  purpose of presence and is inconsistent with the in-character observation that everyone in
  the room can see each other.
- **Include `arrived_at_ms` in `PresenceEntry`.** Convenient for UX (sort-by-arrival,
  duration display) but `arrived_at_ms` is exactly the duration-of-presence data the temporal
  floor is designed to protect. Including it would re-open the leak through the snapshot
  side-channel.

## Consequences

**Positive**

- Presence is always accurate for new arrivals regardless of their `LocationArrivedAt` floor.
- The privacy model stays explicit and auditable: the carve-out is named (I-PRES-2 / I-PRES-7),
  scoped (exactly the snapshot RPC), and enforced (no timestamp fields on the wire).

**Negative**

- The history-scope-privacy spec must carry a one-line cross-reference at I-PRIV-1 documenting
  the carve-out — split between two specs is necessary because the carve-out is conceptually
  about both.
- Future contributors must understand that two privacy rules (I-PRIV-1 and I-PRES-2/7) interact
  at the snapshot boundary.

**Neutral**

- Duration-of-presence UX (e.g., "joined 5 min ago") remains a deferred, separately
  privacy-reviewed decision. If it's needed in the future, it requires a new ADR.

## References

- Spec: `docs/superpowers/specs/2026-05-19-presence-snapshot-design.md` (§2 D-4, §6
  cross-reference plan, §7 I-PRES-2, I-PRES-7)
- Related spec: `docs/superpowers/specs/2026-05-17-history-scope-privacy-design.md`
  (I-PRIV-1..8)
- Related ADR: `holomush-da2q` (snapshot RPC as source of truth)
- Related ADR: `holomush-ghpx` (Tier 2 filter-at-delivery, the gate this exempts the snapshot from)
- Parent bead: `holomush-5b2j`
