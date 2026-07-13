---
phase: 05-world-model-integrity-fixes-m2-m12
plan: 14
subsystem: world-model / repository layer
tags: [transaction, repository, mutation-delta, wmodel, refactor, foundation]
requires: [05-01]
provides:
  - "internal/world/wmodel leaf package (MutationDelta / AffectedAggregate)"
  - "re-entrant Transactor.InTransaction + withTx seam"
  - "world write repo API returning (*wmodel.MutationDelta, error) + expectedVersion params"
  - "read-only reader interface views for the compile-time write fence"
  - "build:all Taskfile target"
affects: [05-02, 05-03, 05-05, 05-06, 05-09, 05-10, 05-11, 05-12, 05-15]
tech-stack:
  added: []
  patterns: [re-entrant-transaction-seam, cycle-neutral-leaf-package, mutation-delta-contract]
key-files:
  created:
    - internal/world/wmodel/mutation_delta.go
    - internal/world/wmodel/mutation_delta_test.go
    - internal/world/postgres/delta_test_helpers_test.go
  modified:
    - internal/world/postgres/transactor.go
    - internal/world/postgres/helpers.go
    - internal/world/repository.go
    - internal/world/service.go
    - internal/world/postgres/{location,exit,character,object,scene}_repo.go
    - internal/world/worldtest/mock_*.go
    - Taskfile.yaml
decisions:
  - "D-07 resolved by REMOVAL: the vestigial world scene-participant write surface deleted (no production caller)"
  - "05-14 returns a primary-only MutationDelta; expectedVersion accepted+ignored (predicate lands in 05-02/05-03)"
  - "auth.CharacterRepository unchanged via delta-discarding CharRepoAdapter bridge (05-15 removes it)"
metrics:
  duration: ~45min
  tasks: 3
  files: 48
  completed: 2026-07-12
status: complete
---

# Phase 5 Plan 14: World-Model Transaction & Repository Foundation Summary

Behavior-preserving repository-layer refactor that lands the three source-grounded prerequisites (Codex Agreed-Concern A) the version-guard (MODEL-03) and outbox (MODEL-04) plans build on: a re-entrant transaction seam, a cycle-neutral `MutationDelta` contract carried by every world write, and the removal of the vestigial scene-participant write surface — proven atomic by a whole-tree compile in both build modes.

## What was built

**Task 1 — re-entrant transaction seam.** `Transactor.InTransaction` now reuses an ambient transaction when one is present in context (no second `pool.Begin`; the outermost call owns commit/rollback), and only begins+commits a new transaction when none is present — behavior-identical to before at the top level. A package-level `withTx(ctx, pool, fn)` helper carries the same re-entrant semantics for repo methods. RED integration tests prove nested-share rollback, nested-error propagation, and `withTx` reuse/top-level-commit. This closes the non-reentrant-double-Begin and connection-reuse blockers.

**Task 2 — writes enroll in the ambient tx.** `ExitRepository.Create/Delete` and `ObjectRepository.Move` were moved off their own `r.pool.Begin` onto `withTx`; `PropertyRepository.Create/Update` switched from `r.pool.Exec` to `execerFromCtx`. Every retained world write can now commit atomically with a caller-owned transaction (the future outbox row). Multi-statement semantics preserved exactly (bidirectional exit cascade + `FOR UPDATE`, object move nesting/circular rules, property visibility defaults). Exit `Delete`'s non-severe cleanup notice is surfaced after commit rather than from the closure (which would roll back).

**Task 3 — wmodel contract + write API redesign + D-07 removal + blast-radius sweep.**
- New leaf package `internal/world/wmodel` holds `MutationDelta` + `AffectedAggregate` (before/after versions + tombstone flag); a leaf-guard test asserts it imports none of `world`/`postgres`/`outbox`, so the round-1 import cycles cannot form.
- The four world write interfaces now return `(*wmodel.MutationDelta, error)`; `Delete` gains `expectedVersion`; `Object.Move` and `Character.UpdateLocation` gain `expectedVersion` (the round-2 CAS-parameter gap). Impls return a primary-only delta; `expectedVersion` is accepted and ignored (predicate lands in 05-02/05-03).
- Read-only reader views (`LocationReader`/`ExitReader`/`ObjectReader`/`CharacterReader`/`SceneReader`) added for the 05-06/05-11 compile-time write fence.
- **Round-5 D-07:** the vestigial world scene-participant WRITE surface removed after a grep confirmed only `_test.go` referenced it — `world.Service.Add/RemoveSceneParticipant` and `SceneRepository.Add/RemoveParticipant` (+ their `scene_participants` writes) deleted; reads (`ListParticipants`/`GetScenesFor`) and the `public.scene_participants` table KEPT (physical DROP deferred to #4815); `plugins/core-scenes` untouched. `mock_SceneRepository` regenerated without the write methods.
- `build:all` Taskfile target added (`go build ./...`) for the whole-tree wave gate.
- Every blast-radius caller updated (world service + tests, access-attribute hand-rolled mocks, integration harness, `test/integration/world` + `auth`, regenerated worldtest mocks). `auth.CharacterRepository` stays `Create(...) error` via the delta-discarding `CharRepoAdapter` bridge.

## Verification

The atomic-wave proof is the whole-tree compile in both build modes plus the full test runs — not the hand list:
- `task build:all` — green (import cycles are compile errors; wmodel is cycle-neutral).
- `task test` (unscoped) — green, 10193 tests (catches the access-attribute old-signature mocks).
- `task test:int` (unscoped) — green, 10510 tests / 6 skipped (catches harness.go, `multi_tab_test.go` LocationRepository.Create calls, auth suite, every suite).
- `task lint` — green.
- Grep gates: no `r.pool.Begin` in exit/object; no write-path `r.pool.Exec` in property; `rg AddSceneParticipant|RemoveSceneParticipant|func .*(Add|Remove)Participant internal/world/` returns only the removal comment.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Pre-existing migrator version assertion (from sibling plan 05-01)**
- **Found during:** Task 3 whole-tree `task test:int` gate.
- **Issue:** `internal/store/migrate_integration_test.go` asserted migrator version `48` after applying all migrations, but sibling plan **05-01** (commit `01af336c0`) added migration `000049` (world version columns), making the latest version `49`. 05-01 updated the migration but not this assertion, leaving a red gate on the branch independent of this plan's changes.
- **Fix:** Updated both assertions `48 → 49`. `expectedTables` unchanged (000049 adds columns, not a table). Verified `internal/store` integration suite green.
- **Scope note:** This file is not in this plan's `files_modified` and the defect was not caused by this plan; it was fixed because it blocked the mandatory whole-tree `task test:int` gate and is a trivially-correct same-phase sibling oversight. Logged for visibility.
- **Files modified:** internal/store/migrate_integration_test.go
- **Commit:** 62127eb66

Everything else executed as written. The whole-tree `test:int` gate performed its intended function — it caught the round-4-named smoke-targets (`test/integration/auth/multi_tab_test.go` `Expect(env.locRepo.Create(...))` calls compiled but were semantically wrong until wrapped with the `delErr` bridge) that a scoped compile would have missed.

## Known Stubs

None that block the plan goal. The primary-only delta, ignored `expectedVersion`, and absent cascade `Affected` population are intentional and documented — the version predicate lands in 05-02/05-03, cascade delta population in 05-10/05-11, and the reader/write-fence wiring in 05-06/05-11. These are the plan's explicit "no behavior change here" boundary, not incomplete work.

## Self-Check: PASSED

- FOUND: internal/world/wmodel/mutation_delta.go
- FOUND: internal/world/postgres/delta_test_helpers_test.go
- FOUND commit f21bbbd83 (Task 1), 1d399c23a (Task 2), 62127eb66 (Task 3)
- `task build:all` + `task test` + `task test:int` + `task lint` all green.
