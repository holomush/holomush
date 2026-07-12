---
gsd_state_version: 1.0
milestone: v0.12
milestone_name: Foundation Hardening
current_phase: 05
current_phase_name: world-model-integrity-fixes-m2-m12
status: executing
stopped_at: Completed 05-04-PLAN.md (version-threaded RMW + M12 spec flip; MODEL-03 complete)
last_updated: "2026-07-12T21:56:57.479Z"
last_activity: 2026-07-12
last_activity_desc: Phase 05 execution started
progress:
  total_phases: 6
  completed_phases: 1
  total_plans: 20
  completed_plans: 11
  percent: 17
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-07-07)

**Core value:** Players can play HoloMUSH end-to-end (create characters, communicate, roleplay in scenes)
through either telnet or the web client, with every access-control decision default-deny and every plugin
trusted identically.
**Current focus:** Phase 05 — world-model-integrity-fixes-m2-m12

## Current Position

Phase: 05 (world-model-integrity-fixes-m2-m12) — EXECUTING
Plan: 8 of 16
Status: Ready to execute
Last activity: 2026-07-12 — Phase 05 execution started

## Performance Metrics

**Velocity:**

- Total plans completed: 30
- Average duration: N/A (no plans executed yet under this GSD roadmap)
- Total execution time: 0 hours

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 01 | 10 | - | - |
| 02 | 7 | - | - |
| 03 | 9 | - | - |
| 04 | 4 | - | - |

**Recent Trend:**

- Last 5 plans: N/A
- Trend: N/A

*Updated after each plan completion*
| Phase 01 P01 | 11 | 2 tasks | 7 files |
| Phase 01 P02 | 95min | 3 tasks | 24 files |
| Phase 01-channels-subsystem P03 | 40min | 3 tasks | 11 files |
| Phase 01 P04 | 40min | 2 tasks | 5 files |
| Phase 01 P05 | 55min | 2 tasks | 4 files |
| Phase 01 P06 | 55min | 2 tasks | 9 files |
| Phase 01 P05b | 70min | 2 tasks | 7 files |
| Phase 01 P07 | 75min | 2 tasks | 9 files |
| Phase 01 P08 | 55min | 2 tasks | 6 files |
| Phase 01 P09 | 150min | 3 tasks | 11 files |
| Phase 02 P01 | 20m | 3 tasks | 6 files |
| Phase 02 P02 | ~15m | 2 tasks | 4 files |
| Phase 02 P03 | 20m | 4 tasks | 11 files |
| Phase 02 P06 | ~40m | 4 tasks | 11 files |
| Phase 02 P04 | ~35m | 3 tasks | 5 files |
| Phase 02 P05 | 55m | 3 tasks | 28 files |
| Phase 02 P07 | ~35m | 3 tasks | 5 files |
| Phase 03 P01 | ~35m | 2 tasks | 4 files |
| Phase 03 P02 | 20m | 2 tasks | 5 files |
| Phase 03 P03 | 40m | 2 tasks | 4 files |
| Phase 03 P05 | ~70m | 3 tasks | 5 files |
| Phase 03 P07 | 50m | 3 tasks | 9 files |
| Phase 03 P09 | 20min | 2 tasks | 1 files |
| Phase 04 P01 | 40min | 2 tasks | 7 files |
| Phase 04 P02 | 35min | 2 tasks | 3 files |
| Phase 04 P03 | ~55min | 2 tasks | 3 files |
| Phase 04 P04 | ~90min | 3 tasks | 2 files |
| Phase 05 P01 | 20m | 3 tasks | 9 files |
| Phase 05 P14 | 45min | 3 tasks | 48 files |
| Phase 05 P02 | 45m | 2 tasks | 6 files |
| Phase 05 P03 | ~40m | 3 tasks | 5 files |
| Phase 05 P04 | 45m | 2 tasks | 6 files |
| Phase 05 P05 | 55min | 3 tasks | 11 files |
| Phase 05 P06 | 75min | 3 tasks | 18 files |

## Accumulated Context

### Decisions

Full decision log lives in PROJECT.md "Key Decisions" (v0.11 phase-level decisions were folded in at
milestone close; per-plan detail is archived in `milestones/v0.11-phases/`). No decisions accumulated for
the next milestone yet.

- [Phase 04]: M12 last-write-wins world corruption REPRODUCED deterministically (D-06): a stale full-row UPDATE silently reverts a committed rename, both writers returning nil (04-02)
- [Phase 04]: Success-criterion #1 four chaos dimensions all green; replica restart recovers canonical state from the DB, not event replay (04-02)
- [Phase 04]: M2 dual-write window characterized (D-07): MoveCharacter commits then emits post-commit; on broker flap the caller sees move_succeeded=true while notification delivery is decoupled from the result
- [Phase 04]: Production world.Service wires NO EventEmitter — the move-notification leg is dead code today (pinned by a spec)
- [Phase 04]: MODEL-01 decided: Option B — CRUD-canonical + optimistic concurrency + transactional outbox in the panel-ratified strengthened shape (consensus one-pager NORMATIVE); Phase 5 implements MODEL-03 version guard + MODEL-04 ordered atomic feed — Human decider (Sean Brandt) chose under future-state-first framing after a two-round three-model panel unanimously ratified the strengthened B shape; the ordered complete world-change feed is the platform's extensibility contract; evolvability inverts under event sourcing pre-1.0; coverage rot countered structurally (compile-time seam + census meta-test + delta-parity)
- [Phase 04]: INV-WORLD-ATOMIC-FEED/-DELTA-PARITY/-FEED-ORDER/-WRITER-BOUNDARY named in the ADR; registration/binding deferred to Phase 5's spec per .claude/rules/invariants.md
- [Phase ?]: Phase 5 slice-1 foundation: version INTEGER NOT NULL DEFAULT 1 on locations/exits/characters/objects (migration 000049); Version int on the four world structs; WORLD_CONCURRENT_EDIT/ErrConcurrentEdit as the single typed conflict signal (D-02/MODEL-03).
- [Phase ?]: [Phase 05]: MODEL-03 CAS mechanism for locations+exits (05-02): version-predicated Update/Delete + a locked follow-up read (same-connection via re-entrant withTx) classifying a zero-row result into TWO outcomes — existing-row-version-moved -> WORLD_CONCURRENT_EDIT, absent -> NOT_FOUND (a committed concurrent delete is correctly observed as not-found).
- [Phase ?]: [Phase 05]: expectedVersion/Version==0 stays an unversioned (id-only) write so existing world.Service delete/update callers (which pass 0 today) remain green; the guard fires only when a caller threads a read version >0 (version-threading is plan 05-04).
- [Phase ?]: [Phase 05]: location DELETE locks the parent row FOR UPDATE BEFORE preselecting FK-cascaded exits (round-6 R6-4) — the parent lock conflicts with the FK key-share lock a child-exit INSERT needs, fencing the child-insert phantom; an interleave integration test binds INV-WORLD-2 delta-parity adversarially.
- [Phase ?]: 05-04: RMW version threading was already end-to-end after 05-02/05-03 via struct.Version transport plus deepest-oops-code; Task 1 added pinning tests with no production change
- [Phase ?]: 05-04: M12 command-race specs serialize through HandleCommand, so the surfaced conflict is proven deterministically at the service level (spec 1 location + new spec 4 object)
- [Phase ?]: [Phase 05]: 05-05 MODEL-04 outbox foundation (slice 2): migration 000050 lands outbox (event_id PK dedup + (game_id,epoch,feed_position) UNIQUE gap-free) + world_feed_counter (locked per-game next_position/epoch + durable lease_generation) + world_genesis_checkpoint + SPLIT world_consumer_receipts/world_consumer_watermarks.
- [Phase ?]: [Phase 05]: 05-05 WriteIntent (internal/world/postgres writer boundary) is sole owner of storage-stamped envelope fields (round-3 blocker #1): allocates epoch/feed_position from the locked FOR UPDATE counter, finalizes via pure wmodel.Finalize, persists one outbox row via execerFromCtx (same tx), returns the finalized Envelope; types in wmodel leaf; WORLD_FEED_LOCK_TIMEOUT bounds a stuck lock.
- [Phase ?]: [Phase 05]: 05-05 always-run INV-WORLD-1 integration test proves a REAL world row + its envelope commit-or-roll-back together (rollback/commit/forced-duplicate-event_id); binding annotation added in 05-12.
- [Phase ?]: [Phase 05]: 05-06 mutate(ctx, intent, write-closure) compile-time write-requires-envelope seam — closure identifies+executes the operation (round-5 finding 1), writer repos private to executor, package world imports neither outbox nor postgres (round-2 cycle fix); injected world.OutboxWriter owns epoch/position+finalization (round-3 blocker #1).
- [Phase ?]: [Phase 05]: 05-06 MoveCharacter is first through the same-tx outbox; post-commit emit path (events.go/EmitMoveEvent/go-retry) DELETED folding WR-01 (D-03); post-commit movement-hook failure = operational degradation (log+metric, command success), move_succeeded=true fail-after-commit path deleted (round-5 finding 3); M2 dual-write window CLOSED (proven by rewritten resilience spec).

### Pending Todos

None yet.

### Blockers/Concerns

- Forums (Epic 11, `holomush-djj`) has no design yet — blocks any Forums-integration forward work
- Discord integration (Epic 12): Channels prerequisite shipped in v0.11; still blocked on an OAuth substrate that does not yet exist
- 259/334 registered invariants are `binding: pending` (concentrated in INV-CRYPTO and INV-SCENE) — tracked
  epic `holomush-hz0v4`, not a blocker, but phases touching crypto/scenes should bind relevant invariants as
  part of their own definition of done

### Quick Tasks Completed

| # | Description | Date | Commit | Directory |
|---|-------------|------|--------|-----------|
| 260709-sqg | Fix holomush-9hygy — convert core-channels migrations TIMESTAMPTZ→BIGINT epoch-ns (lint:no-timestamptz ship blocker) | 2026-07-10 | 1284ba341 | [260709-sqg-…](./quick/260709-sqg-fix-bead-holomush-9hygy-convert-core-cha/) |
| 260711-hg1 | GH-4785 (F2): cap gateway ConnectRPC request-body size (`WithReadMaxBytes` 4 MiB + `ReadTimeout`) to prevent unauthenticated OOM | 2026-07-11 | 0e3806ebf | [260711-hg1-…](./quick/260711-hg1-gh-4785-cap-gateway-connectrpc-request-b/) |

## Deferred Items

Items acknowledged and carried forward from the ingest, not part of this roadmap:

| Category | Item | Status | Deferred At |
|----------|------|--------|-------------|
| Social-spaces | Forums integration (Epic 11) | No design yet | Ingest 2026-07-07 |
| Social-spaces | Discord/Slack bridging + OAuth linking (Epic 12) | Blocked on Channels + OAuth substrate | Ingest 2026-07-07 |
| Web portal | Non-scene web surfaces (building/world editing, admin UI) | Directional theme goal, not yet spec'd | Ingest 2026-07-07 |

## Session Continuity

Last session: 2026-07-12T21:56:02.057Z
PROJECT.md / REQUIREMENTS.md / ROADMAP.md / STATE.md written and committed (PR #4811).
Stopped at: Completed 05-04-PLAN.md (version-threaded RMW + M12 spec flip; MODEL-03 complete)
Resume file: None

## Operator Next Steps

- Merge PR #4811 (milestone-init planning docs).
- Ship the F2 gateway DoS cap (#4785) as a `/gsd-quick` fix — the immediate opener.
- Then `/gsd-plan-phase 4` (or `/gsd-discuss-phase 4`) — the F1 resilience pass + event-sourcing-vs-CRUD ADR.
