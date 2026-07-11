<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->

# World-State Model: Event Sourcing vs CRUD-Canonical

**Date:** 2026-07-11
**Status:** Proposed
**Decision:** holomush-i4784
**Deciders:** Sean Brandt

## Context

HoloMUSH's design docs (CLAUDE.md, architecture.md, coding-standards.md, the public index.mdx) state event sourcing as a foundational principle — "current state is derived from event replay." The F1 archaeology ([`f1-eventsourcing-why.md`](../reviews/arch-review/2026-07-11/verification/f1-eventsourcing-why.md), issue #4784) establishes that this was never true for the **world model**: locations, exits, characters, and objects have always been direct-write CRUD in Postgres. No world-state rebuild path has ever existed — `git log -S EventStore` and `git log --grep "rebuild world|derive state|projection"` surface only the event-*log* store, the w9ml actor-ULID work, and the F7 JetStream cutover; **not one commit reconstructs world state by replaying events.** The "replay" removed in the F-series deletions (`EventWriter`, `cursor_lock.go`, `replay.go`, `internal/grpc/replay.go`) lived entirely in the gRPC `Subscribe` **client catch-up** path, not in state derivation. The audit projection (`internal/eventbus/audit/projection.go`) writes `events_audit`, never the world tables.

No ADR has ever recorded a CRUD-vs-event-sourcing choice for the world model across 197 existing records. The event *bus/log* half (append, JetStream replay for clients, audit) is real and well-built; the event-*sourcing-of-state* half was asserted as a foundation and quietly never built. This is **architectural drift by default** — a stated principle unrealized for the world model, with no decision ever recording the divergence. The absence of this ADR *is* the finding; this ADR closes it.

The CRUD-not-event-sourced reality is the root cause of two correctness findings whose empirical characterization is recorded in [`f1-resilience-verdict.md`](../reviews/arch-review/2026-07-11/verification/f1-resilience-verdict.md) (OPS-05, issue #4791) — the two-replica resilience harness output. Rather than duplicate it, this ADR links it and summarizes the load-bearing verdicts:

- **M12 — last-write-wins, no version guard: REPRODUCED deterministically.** Two replicas racing a write to the same location row produce a silent lost update: one replica's stale full-row `UPDATE` reverts a field the other already committed, and both writers return `nil` — no conflict is ever surfaced. Proven by an explicit-interleave mechanism spec (always reproduces) and reinforced by a command-fidelity race (N=50, every round loses one write with zero conflict signals). The world tables carry no version column, so no optimistic-concurrency check is even possible today (issue #4798).
- **M2 — dual-write non-atomicity: window CHARACTERIZED.** `MoveCharacter` commits the character row first and emits the move notification post-commit; the two are not atomic. With a wired emitter and a broker frozen mid-move, the DB commit persists, the caller receives an emit-failure error carrying `move_succeeded=true`, and the notification's delivery is decoupled from that error (delivered late and out-of-band this run; loseable outright on other timings). The caller cannot know whether the notification landed — that ambiguity is the non-atomicity window.
- **Production wires no emitter at all.** `internal/world/setup/subsystem.go` constructs the production `world.Service` with no `EventEmitter`, so today the entire move-notification leg is dead code: every move reports `EVENT_EMITTER_MISSING` with `move_succeeded=true` while the DB commits unconditionally.
- **Restart recovery is a DB read, not a replay.** A restarted replica boots cleanly against the existing EVENTS stream and serves pre-restart state straight from the shared database; no replay runs at boot.

This ADR decides which world-state model HoloMUSH commits to and therefore which concrete mechanism Phase 5 (MODEL-03/MODEL-04) implements. Phase 5's scope, the OPS-02 retention interaction, and the Phase 7 ARCH-04 event-model collapse all take their shape from this choice.

## Intent (D-02)

Was world-state event sourcing ever meant to be real? The F1 archaeology's conclusion is that **intent cannot be proven from git.** No commit ever built world-state-from-events, and no ADR ever recorded a considered CRUD decision — the evidence supports neither "built then regressed" nor "deliberately chose CRUD." The best-supported reading is that the stated architectural principle functioned as shorthand for "event-driven with an append-only audit log," and the gap between principle and implementation for the world model was never surfaced or decided. The missing ADR is itself the finding; recording the decision here closes the drift. This ADR does not promise further excavation beyond the archaeology already performed — it formalizes the decision the divergence always needed.

## Decision

PENDING — resolved by decision checkpoint (Task 2).

## Rationale

PENDING — resolved by decision checkpoint (Task 2).

## Alternatives Considered

Per D-01 this decision is genuinely open: the F1 doc's original lean toward option B was overridden during discuss-phase, and both options are costed here from the verified inputs in `04-RESEARCH.md` with equal weight and no starting lean.

### Option A — build real event sourcing for world state

The event *is* the write: world state becomes a projection derived from an append-only event log, and every mutation validates then appends rather than issuing a direct row `UPDATE`. What this actually entails, costed against the current code:

1. **Event coverage inversion (~4 → ~15-20 types).** Today only move/examine/object_create/object_give emit (`internal/world/events.go`); Create/Update/Delete of locations, exits, characters, objects, and all property writes emit nothing. Real event sourcing needs an event per mutation — ~15-20 new event types with payload schemas — plus inverting every `world.Service` write method from validate→UPDATE to validate→append→project.
2. **The emit pipeline is unwired, not merely under-extended.** Production `world.Service` has no `EventEmitter` (`setup/subsystem.go`); `NewEventStoreAdapter` has zero production callers. Option A **builds** the pipeline, it does not extend a working one.
3. **No salvageable rebuild substrate.** The F-series deletions were gRPC `Subscribe` client-catch-up, not state rebuild. There is nothing to revive — the projection/rebuild path is greenfield.
4. **Retention conflict with OPS-02.** The JetStream EVENTS stream defaults to a 30-day `StreamMaxAge`; the durable tier `events_audit` is unbounded today and **OPS-02 (Phase 6) exists precisely to bound it** with the RetentionWorker. An event-sourced source of truth requires permanent event retention (or snapshot + compaction machinery), in direct tension with the same milestone's OPS-02. Choosing A means resolving this retention conflict in the ADR.
5. **Projection/rebuild machinery.** Projection consumers that write the world tables (the audit projection never does), idempotent replay (dedup by event ULID via `Nats-Msg-Id`), per-entity ordering (JetStream per-stream seq owns ordering; world events would span `location.<id>` / `character.<id>` subjects), a genesis snapshot migrating current CRUD state into the log, and a rebuild CLI/verify job.
6. **Option A does not automatically dissolve M12.** Command validation still reads state (ABAC `checkAccess` + existence checks run pre-write in every service method). Without an optimistic command/aggregate-version protocol, the lost-update simply moves to the validation read — the guard changes location, it does not vanish.
7. **ARCH-04 coupling.** The parallel `core.Event` / `eventbus.Event` models must collapse (ARCH-04, Phase 7). Option A makes the unified event model **state-load-bearing**; option B keeps it a notification/audit concern.
8. **Effort class.** Given the above, A is a multi-phase build (new write protocol + projections + retention/snapshot + migration + rebuild tooling). REQUIREMENTS.md "Out of Scope" already stipulates any large event-sourcing build is a follow-on milestone. The ADR can still *choose* A — Phase 5 would then implement a named **foundation slice** (a first projected entity family + the write-inversion seam), with the full build deferred to a follow-on milestone.
9. **What A uniquely buys.** Derivable/replayable state, auditable reconstruction, time-travel debugging, and structural elimination of the M2 class — because the event *is* the write, there is no post-commit notification to lose. This realizes the originally stated architectural principle.

### Option B — CRUD-canonical + optimistic concurrency + transactional outbox

State stays canonical in Postgres; the event log is formally a notification/audit concern. Two compensating controls close the findings:

1. **Optimistic concurrency for M12 (→ Phase 5 MODEL-03).** Add `version INTEGER NOT NULL DEFAULT 1` to locations, characters, objects, and exits (idempotent, paired down-migration per `.claude/rules/database-migrations.md`). Repos change to `UPDATE … SET …, version = version + 1 WHERE id = $1 AND version = $2`, with `RETURNING` used to distinguish not-found from a version conflict deterministically (avoiding the differentiator-SELECT race). In-schema precedent exists: `access_policies.version INTEGER NOT NULL DEFAULT 1` plus `access_policy_versions`.
2. **Version threading through callers.** The read-modify-write sites — `entity_mutator.go` (name/description × location/object/character) and `UpdateCharacterDescription` — carry the read version into the guarded `UPDATE`; the `Location`/`Object`/`Character` structs gain a `Version` field. A conflict-surfacing behavior must be named (a bounded retry loop vs a user-visible "concurrent edit" error) — the finalized ADR records which.
3. **Transactional outbox for M2 (→ Phase 5 MODEL-04).** No outbox exists today. An `outbox` table is written in the SAME transaction as the entity write, using the existing `Transactor.InTransaction` seam (already used by `DeleteLocation`); a relay claims rows and publishes to JetStream with `Nats-Msg-Id` = event ULID for dedup (via the existing `DupeWindow`), with cleanup coordinated through the OPS-02 RetentionWorker machinery. Because most world writes emit nothing today, MODEL-04's real work is "wire emissions through the outbox where notifications are wanted" — the finalized ADR scopes which events the outbox carries.
4. **What B forgoes.** State replay/rebuild — permanently, for world state. The "event sourcing" doc principle is formally downgraded at MODEL-02's ~6 sites (including the public marketing page) to "event-driven with an append-only audit log."

## Decision framework

Both options scored against evidence-backed criteria. Scores are observations from the linked evidence and the verified cost inputs, not a recommendation.

| Criterion | Option A (event sourcing) | Option B (CRUD + guard + outbox) |
| --------- | ------------------------- | -------------------------------- |
| Closes M12 (lost update) | Indirectly — needs an added optimistic command/aggregate-version protocol; ordering from append seq does not cover the validation read | Directly — version-guarded CAS `UPDATE` with `RETURNING` conflict detection |
| Closes M2 (dual-write) | Structurally — the event is the write, so no post-commit notification exists to lose | Directly — outbox write is in the same transaction as the entity write; relay dedups on publish |
| Migration + effort class | Multi-phase build (taxonomy, write inversion, projections, genesis snapshot, rebuild tooling); full build is a follow-on milestone; Phase 5 delivers a foundation slice | Bounded inside v0.12: version columns + guarded repos + outbox table + relay; in-schema precedent exists |
| Retention posture vs OPS-02 | Conflicts — a source-of-truth log needs permanent retention or snapshot/compaction, tension with OPS-02's bounding worker | Compatible — events remain bounded; outbox cleanup reuses OPS-02 machinery |
| Replayability value | High — derivable/replayable state, auditable reconstruction, time-travel debugging | None for world state — recovery stays a DB read |
| ARCH-04 impact | Makes the unified event model state-load-bearing | Keeps the unified event model a notification/audit concern |
| Operational complexity | Higher — projection consumers, rebuild/verify jobs, snapshot lifecycle | Lower — one relay process + version bookkeeping; no projection tier |

## Consequences

PENDING — resolved by decision checkpoint (Task 2). The finalized ADR will record the Phase 5 input contract (MODEL-02 doc corrections; MODEL-03/MODEL-04 implementation), the authorization-placement guarantee (`world.Service` `checkAccess` stays pre-write under both options; no outbox relay or projection consumer bypasses it), the ARCH-04 (Phase 7) coordination note, that the resilience suite stands as the regression check for the Phase 5 guard (D-05), and the explicit deferral of INV-* registration/binding to Phase 5's spec per `.claude/rules/invariants.md` (this ADR names the mechanism; Phase 5 mints and binds any invariants).

## Cross-references

- **Issue #4784** — MODEL-01 (this decision).
- **Issue #4791** — OPS-05 (resilience harness; the evidence input).
- **Issue #4798** — M12 last-write-wins finding.
- [`f1-eventsourcing-why.md`](../reviews/arch-review/2026-07-11/verification/f1-eventsourcing-why.md) — the archaeology of *why* the world model is CRUD-not-event-sourced.
- [`f1-resilience-verdict.md`](../reviews/arch-review/2026-07-11/verification/f1-resilience-verdict.md) — the empirical M12/M2 verdict this ADR is grounded in.
