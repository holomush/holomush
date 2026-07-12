---
gsd_state_version: 1.0
milestone: v0.12
milestone_name: Foundation Hardening
current_phase: 5
current_phase_name: M2 / M12
status: "Phase 4 shipped — PR #4814"
stopped_at: Phase 5 context gathered
last_updated: "2026-07-12T01:17:29.319Z"
last_activity: 2026-07-12
last_activity_desc: Phase 05 planning complete
progress:
  total_phases: 6
  completed_phases: 1
  total_plans: 4
  completed_plans: 4
  percent: 17
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-07-07)

**Core value:** Players can play HoloMUSH end-to-end (create characters, communicate, roleplay in scenes)
through either telnet or the web client, with every access-control decision default-deny and every plugin
trusted identically.
**Current focus:** Phase 5 — World-Model Integrity Fixes (M2 / M12)

## Current Position

Phase: 5 — World-Model Integrity Fixes (M2 / M12)
Plan: Not started
Status: Phase 4 shipped — PR #4814
Last activity: 2026-07-12 — Phase 05 planning complete

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

Last session: 2026-07-12T00:31:21.752Z
PROJECT.md / REQUIREMENTS.md / ROADMAP.md / STATE.md written and committed (PR #4811).
Stopped at: Phase 5 context gathered
Resume file: .planning/phases/05-world-model-integrity-fixes-m2-m12/05-CONTEXT.md

## Operator Next Steps

- Merge PR #4811 (milestone-init planning docs).
- Ship the F2 gateway DoS cap (#4785) as a `/gsd-quick` fix — the immediate opener.
- Then `/gsd-plan-phase 4` (or `/gsd-discuss-phase 4`) — the F1 resilience pass + event-sourcing-vs-CRUD ADR.
