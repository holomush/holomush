<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Publish-Vote Roster Frozen at Attempt Start into `published_scene_votes`, Not on `scene_participants`

**Date:** 2026-05-23
**Status:** Accepted
**Decision:** holomush-39a5f
**Deciders:** HoloMUSH Contributors
**Related:** [`holomush-jrefa`](holomush-jrefa-published-scenes-table-distinct-from-scene-log.md)

## Context

Phase 6 introduces a publish-vote mechanism. The Scenes v2 design (§1.3) declared a `PublishVote *bool` field on the Participant entity. Implementation surfaced two questions:

1. **Where does vote state live?** On the existing `scene_participants` row, or in a separate table?
2. **Who is eligible to vote, and when is the eligibility set?** The brainstorm question Q3 surfaced three options: roster frozen at scene-end into a dedicated table; roster = current participants at end time with vote state on participant rows; roster = anyone-ever-participated via soft-deletion of `scene_participants`.

Roster semantics depend on the data model: a frozen roster requires snapshotting at attempt-creation time and cannot be expressed naturally as a column on a live membership table.

Three approaches were considered:

- **Column on `scene_participants`** — add `publish_vote *bool` + `publish_vote_cast_at *time` columns. Vote state lives where membership lives. No snapshot — eligible voters are whoever has a row at scene-end time; leavers-after-end lose their vote when their row is deleted.
- **Soft-delete `scene_participants`** — add `left_at` column. Eligible voters at scene-end = all non-kicked owner/member participants, including those who voluntarily left. Heaviest change: alters the meaning of `scene_participants` for every existing reader.
- **Dedicated `published_scene_votes` table** — snapshot eligible voters at attempt creation. One row per (attempt × voter), carrying `vote *bool`, `voted_at`, `last_changed_at`. `scene_participants` untouched.

## Decision

A new `published_scene_votes` table holds vote state, one row per (attempt × voter). The roster is populated at attempt creation by snapshotting `scene_participants` rows with `role IN ('owner', 'member')` (NOT `'invited'`). After insertion, the roster is immutable for the attempt's lifetime: participants joining the scene later do not become voters for this attempt; participants leaving the scene later retain their vote eligibility.

`scene_participants` is not modified by Phase 6. Its existing schema, its membership semantics, and its access patterns are preserved.

`published_scene_votes.published_scene_id` carries a FK to `published_scenes(id) ON DELETE CASCADE`. This is the only FK in the Phase 6 schema. It is defense-in-depth: inert until an attempt-deletion or GC path exists, but structurally ensures vote rows can never outlive their parent attempt row. Cross-schema FKs to `public.characters` or `public.players` are impossible under plugin role isolation.

Phase 6 also adds a per-scene `max_publish_attempts` column to `scenes` (migration 000009) — this is unrelated to roster but co-located in the same Phase 6 schema work.

## Rationale

**Roster immutability is the load-bearing requirement.** INV-P6-1 mandates that the eligible-voter set is frozen at attempt creation. A leaver-after-end retains their vote (a publication they contributed to should not be derailed by their later departure); a leaver-before-end is excluded (they weren't there when the vote roster was decided). This semantic is impossible to express naturally as a column on a live `scene_participants` table — the row either exists (member) or it doesn't (left), and there's no per-attempt history.

**Coupling vote state with membership lifetime conflates unrelated concerns.** `scene_participants` rows track scene-level membership: who's in the scene right now, when did they join, what's their role. Vote state tracks per-attempt opinion: did this voter cast yes/no for THIS attempt, when, and is it sticky. The two lifecycles are independent: one voter might participate in multiple consecutive publication attempts on the same scene; the roster for each attempt is a separate snapshot. Cramming this into participant rows would require either (a) overwriting vote state per attempt (lose history), or (b) repeated columns per attempt (combinatorial explosion).

**Per-attempt isolation enables clean audit.** With separate vote rows per attempt, the system can answer "who voted yes on attempt 2 but no on attempt 3?" via simple SQL JOIN. With vote state on participant rows, that query is impossible without versioning the participant table.

**`scene_participants` stability matters.** Phase 4 (`5rh.13`) introduced maintained pose-order metadata on `scene_participants`. Phase 5 (`5rh.14`) introduced focus-membership relationships. ABAC attribute resolution (`principal.id in resource.scene.participants`) reads this table. Adding vote-state columns would increase row size for every membership row across every scene, and add the vote-state field to every reader's mental model. The Phase 6 work stays scoped: new table, new rows, no impact on existing participant readers.

## Alternatives Considered

**Option A: Dedicated `published_scene_votes` table, roster frozen at attempt creation (chosen).**

| Aspect | Assessment |
| ------ | ---------- |
| Strengths | Roster immutability is structural (insert-then-update-only); per-attempt isolation; voter lifecycle decoupled from membership lifecycle; `scene_participants` unmodified; vote history preserved across multiple attempts |
| Weaknesses | One additional table; the snapshot at attempt-creation time requires a transaction-coordinated INSERT (already needed for the publish-attempt row itself) |

**Option B: `publish_vote` + `publish_vote_cast_at` columns on `scene_participants`.**

| Aspect | Assessment |
| ------ | ---------- |
| Strengths | No new table; vote state lives where membership lives; SQL JOIN unnecessary for "who voted what" queries |
| Weaknesses | Cannot enforce roster immutability (leavers-after-end lose vote when their row is deleted); cannot support multiple attempts per scene without overwriting vote state; couples scene-membership lifetime with vote-window state; every reader of `scene_participants` carries the publish_vote field in their mental model |

**Option C: Soft-delete `scene_participants` (add `left_at` column).**

| Aspect | Assessment |
| ------ | ---------- |
| Strengths | Eligible voters at scene-end = all non-kicked owner/member rows; supports "anyone who ever participated" semantic; vote-state column on participant row still works |
| Weaknesses | Changes the meaning of `scene_participants` for every existing reader; every Phase 4/5 query that JOINs against participants must add `WHERE left_at IS NULL`; missed query is a security regression (leaked content to a left participant); biggest blast radius of the three options |

## Consequences

- **Roster immutability is structural.** INV-P6-1 ("rosters frozen at attempt creation and immutable for the attempt's lifetime") is enforced by the table's insert-only-then-update-only semantics: rows are inserted once at attempt creation, then only their `vote`/`voted_at`/`last_changed_at` fields change. No application-layer check is needed to prevent roster mutation.
- **Per-attempt isolation.** Multiple publication attempts on the same scene have independent roster rows. Each retry gets a fresh tally; vote history of prior attempts persists for audit.
- **Voter lifecycle decoupled from scene-membership lifecycle.** A voter can leave the scene (deleting their `scene_participants` row) without losing their vote eligibility on an in-flight publication attempt. This matches the user expectation that a vote, once cast, persists.
- **No `scene_participants` migration.** Phase 6 does not touch `scene_participants` schema. Every existing reader (focus model, Phase 5 multi-connection visibility, audit gates, ABAC `principal.id in resource.scene.participants` attribute resolution) continues to operate against the unchanged shape.
- **Invited rows excluded by predicate, not by table structure.** The roster snapshot at attempt creation filters `role IN ('owner', 'member')`. An invited character who later accepts (becoming `member`) does NOT retroactively become a voter for an attempt that started before their acceptance. This is correct under the spec's roster-immutability rule but is worth flagging — the contributing-vs-voting distinction lives in the snapshot filter.

## References

- [Scenes Phase 6 design spec](../superpowers/specs/2026-05-23-scenes-phase-6-logs-vote-privacy-design.md) §3.2, §3.3 (DDL), §4.1 (transitions), INV-P6-1
- [Scenes Phase 6 implementation plan](../superpowers/plans/2026-05-23-scenes-phase-6-logs-vote-privacy.md) Task A1 (DDL), Task A5 (CreatePublishAttempt seeds roster), Task A6 (CastVote)
- [Scenes v2 design](../superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md) §1.3 (superseded — `PublishVote *bool` field moved off Participant)
- Design bead `holomush-5rh.20` (brainstorm Q3 — voter eligibility and state location)
