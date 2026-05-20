<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Denormalize Pose-Order Metadata Against scene_log Source of Truth

**Date:** 2026-05-19
**Status:** Accepted
**Decision:** holomush-r4th
**Deciders:** HoloMUSH Contributors

## Context

Scenes Phase 4 (`holomush-5rh.13`) introduces pose-order computation for the `GetPoseOrder` RPC and the `scene order` subcommand. Four pose-order modes are supported (`strict`, `3pr`, `5pr`, `free`); each requires per-participant timing data to determine eligibility and display order.

The scenes v2 spec section 4.2 specified:

> "Pose order is derived from the IC event stream — no separate state table."

This formulation matches the event-sourcing posture of the rest of the scene domain: scene_log is the audit projection of the IC event stream and is the canonical record of scene content. Deriving pose order from scene_log was conceptually clean — no risk of source-of-truth divergence.

However, the v2 spec was written before the substrate-contract era and did not anticipate the per-call cost. Deriving pose order on each `GetPoseOrder` invocation requires reading recent scene_pose rows from scene_log:

- For `strict` mode: replay all poses to determine current queue order.
- For `3pr` / `5pr` modes: for each participant, count "other characters' poses since this participant's last pose" — interleaving across the recent pose window.
- For long-running async scenes, the pose history can be hundreds or thousands of rows.

The cost is unbounded as scenes age. A scene running for weeks with hundreds of poses would require either a fixed `LIMIT` (broken correctness for large participant counts) or a full scan (latency disaster).

Additionally: the scene_log payload column carries ciphertext when `sensitivity: always` events (Phase 4 introduces these for `scene_pose` / `scene_say` / `scene_emit` / `scene_ooc`). Pose-order computation needs only metadata (`actor`, `timestamp`, `type`) — all plaintext columns. Reading scene_log for pose-order computation is structurally metadata-only, but the cost remains.

## Decision

Maintain denormalized pose-order metadata in three new schema columns, updated transactionally by the audit consumer on every `scene_pose` insertion:

- `scenes.total_pose_count INTEGER NOT NULL DEFAULT 0` — per-scene monotonic pose counter.
- `scene_participants.last_pose_at TIMESTAMPTZ NULL` — per-participant timestamp of most recent pose (NULL = never posed).
- `scene_participants.last_pose_seq INTEGER NULL` — per-participant value of `total_pose_count` at the moment of their last pose.

`GetPoseOrder` reads these columns via a single SQL join — `O(N participants)` per call, regardless of scene history depth.

The `scene_log` table remains the canonical source of truth. The maintained columns are a derived cache. Documented recovery SQL (spec §9.5) rebuilds the metadata from scene_log if drift is suspected:

```sql
UPDATE scenes s SET total_pose_count = (
    SELECT COUNT(*) FROM scene_log
    WHERE subject = 'events.' || $game || '.scene.' || s.id || '.ic'
      AND type = 'scene_pose'
);
-- (plus per-participant rebuild via window function — see spec §9.5)
```

The rebuild-equivalence property is pinned by INV-P4-8 (integration test exercises the recovery SQL after arbitrary pose history and asserts byte-identical metadata).

## Rationale

**Unbounded `O(H)` is architecturally untenable.** A scene running for weeks accumulates pose history that the read path cannot reasonably scan on every call. Even with optimistic indexing, the cost grows without bound as scenes age. Denormalized state with bounded read cost is the structural fix.

**The metadata is a function of scene_log, not a parallel source of truth.** The recovery SQL formalizes the relationship: `total_pose_count = COUNT(scene_log.type='scene_pose')`; per-participant `last_pose_at` and `last_pose_seq` are deterministic projections of the same rows. INV-P4-8's integration test makes the equivalence CI-enforced. The maintained columns cannot diverge from scene_log without operator intervention.

**Ciphertext is irrelevant to ordering.** Pose-order computation needs only `actor`, `timestamp`, and a monotonic sequence. All three are plaintext columns on scene_log (`actor` is the Phase 7 plugin SDK contract; `timestamp` is the event timestamp; the new `last_pose_seq` projects the natural ordering). The decision to denormalize does NOT introduce a crypto path dependency for pose-order reads — Phase 7's `metadata_only=true` semantics are preserved.

**Transactional INSERT+UPDATE is the right boundary.** INV-P4-10 pins the atomicity: the audit consumer either commits scene_log INSERT + both UPDATEs together, or rolls all three back. No partial states are visible to readers. The audit consumer is the natural durability boundary — JetStream delivery + plugin AuditEvent + scene_log persist is already the transactional unit; the metadata UPDATEs are folded inside.

## Alternatives Considered

**Option A: Derive pose order from scene_log on every call (v2 §4.2 original).**

| Aspect     | Assessment |
| ---------- | ---------- |
| Strengths  | No extra mutable state; pose order always reflects canonical truth; no rebuild procedure needed; pure event-sourcing posture |
| Weaknesses | `O(H)` per call unbounded as scenes age; full payload scan required even though payload is ciphertext-irrelevant for ordering; latency unacceptable for active scenes with hundreds of poses |

**Option B: Maintained metadata with rebuild contract (chosen).**

| Aspect     | Assessment |
| ---------- | ---------- |
| Strengths  | `O(N participants)` per call regardless of history depth; metadata columns are plaintext (no DEK dependency); recovery SQL documented for drift repair; INV-P4-8 pins rebuild-equivalence as a CI-enforced contract |
| Weaknesses | Introduces mutable denormalized state; requires atomic INSERT+UPDATE transaction per pose; operators must run recovery SQL after manual scene_log intervention |

**Option C: In-memory cache with on-read derivation.**

| Aspect     | Assessment |
| ---------- | ---------- |
| Strengths  | No schema changes; cache populated lazily on first call |
| Weaknesses | Cache invalidation on every pose emit; multi-process cache coherence (Phase 7 plugin SDK has plugin restart paths); plugin restart loses cache; latency on cache miss can still be `O(H)`; complexity for marginal gain |

## Consequences

**Positive:**

- `GetPoseOrder` is bounded by participant count, not scene age.
- Pose-order computation is a pure function over already-loaded rows — no additional DB roundtrips after the single SELECT.
- The maintained columns are plaintext metadata; no decryption path required for pose-order reads.
- Rebuild SQL provides an operator-runbook tool for drift repair.

**Negative:**

- Every `scene_pose` audit event requires a three-statement transaction (INSERT + 2 UPDATEs).
- Operators must run the recovery SQL after any manual intervention on scene_log (e.g., row deletion for retention compliance).
- The schema migration adds three columns; future schema changes must account for them.

**Neutral:**

- The `scene_log` table remains canonical; maintained columns are a derived cache. The decision does not change the source-of-truth posture, only the read-path cost.
- INV-P4-8 makes the rebuild-equivalence property a CI-enforced contract; the denormalization cannot silently diverge from the event log without test failure.

## References

- [Scenes Phase 4 Design](../superpowers/specs/2026-05-19-scenes-phase-4-streams-and-pose-order-design.md) §6, §9, §9.4, §9.5
- [Scenes v2 Design](../superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md) §4.2 (superseded for this concern)
- [Phase 7 Plugin SDK Design](../superpowers/specs/2026-05-13-event-payload-crypto-phase7-plugin-sdk-design.md) (plugin audit table shape; ciphertext irrelevance for ordering metadata)
- Bead: `holomush-5rh.13` (Phase 4 design)
