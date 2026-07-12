---
phase: 05-world-model-integrity-fixes-m2-m12
plan: 02
subsystem: database
tags: [postgres, pgx, optimistic-concurrency, cas, model-03, world-model, outbox]

# Dependency graph
requires:
  - phase: 05-01
    provides: "version column (migration 000049), Version struct field, world.ErrConcurrentEdit + CodeConcurrentEdit (WORLD_CONCURRENT_EDIT)"
  - phase: 05-14
    provides: "re-entrant withTx/execerFromCtx seam, redesigned repo write signatures returning (*wmodel.MutationDelta, error), Delete(ctx,id,expectedVersion), wmodel.MutationDelta/AffectedAggregate"
provides:
  - "Version-predicated CAS Update/Delete for the location aggregate with a locked follow-up zero-row classifier"
  - "Version-predicated CAS Update/Delete for the exit aggregate mirroring the location repo"
  - "Shared zero-row classifier (classifyCASZeroRow) — two-outcome: WORLD_CONCURRENT_EDIT vs NOT_FOUND"
  - "Location DELETE cascade-delta with parent-lock-first ordering (round-6 R6-4 child-insert phantom fence)"
  - "MutationDelta before/after version population + struct Version refresh on write"
affects: [05-03, 05-04, 05-10, 05-11, 05-12, world-outbox, resilience-m12]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Version-predicated CAS: UPDATE ... version = version + 1 WHERE id = $1 AND version = $expected RETURNING version"
    - "Two-outcome zero-row classifier via a locked follow-up read on the caller's connection (same-tx, never a second pool borrow)"
    - "Parent-lock-first delete ordering to fence an FK child-insert phantom before preselecting cascaded children"

key-files:
  created: []
  modified:
    - "internal/world/postgres/location_repo.go — CAS Update/Delete + preselectCascadedExits + version scans"
    - "internal/world/postgres/exit_repo.go — CAS Update/Delete + bidirectional cascade delta + version scans"
    - "internal/world/postgres/helpers.go — classifyCASZeroRow, querierFromCtx, primaryDeltaVersioned"
    - "internal/world/postgres/scene_repo.go — thread version column through the shared location scanner"
    - "internal/world/postgres/location_repo_test.go — CAS/cascade/interleave/pool-size-1 integration tests"
    - "internal/world/postgres/exit_repo_test.go — CAS + bidirectional-delta integration tests"

key-decisions:
  - "expectedVersion/Version == 0 is an unversioned (id-only) write so existing world.Service callers (which pass 0 today) stay green; the guard fires only when a caller threads a read version > 0 (version-threading is plan 05-04)."
  - "The zero-row classifier is TWO outcomes only (round-5 Codex MEDIUM): a committed concurrent delete is observed as an absent row and correctly reported NOT_FOUND — no three-way distinction (the ID-only API carries no existence token)."
  - "Location DELETE locks the parent row FOR UPDATE BEFORE preselecting FK-cascaded exits (round-6 R6-4) to fence the child-insert phantom; the parent FOR UPDATE lock conflicts with the FK key-share lock a child-exit INSERT must acquire."
  - "MODEL-03 is NOT marked complete: it spans locations/exits (this plan) + characters/objects (05-03) + version-threaded RMW/two-replica verification (05-04)."

patterns-established:
  - "Guarded write skeleton: wrap CAS + classifier in re-entrant withTx so the follow-up read reuses the caller's connection (pool-size-1 safe, finding 14)."
  - "Cascade delta: preselect FK-cascaded children under lock and return them as tombstone AffectedAggregates so the outbox manifest matches (INV-WORLD-2 delta-parity)."

requirements-completed: []

coverage:
  - id: D1
    description: "Location Update/Delete are version-predicated CAS: a stale-version write surfaces WORLD_CONCURRENT_EDIT (row untouched); an absent row surfaces LOCATION_NOT_FOUND."
    requirement: "MODEL-03"
    verification:
      - kind: integration
        ref: "internal/world/postgres/location_repo_test.go#TestLocationRepository_UpdateVersionGuard"
        status: pass
      - kind: integration
        ref: "internal/world/postgres/location_repo_test.go#TestLocationRepository_DeleteVersionGuard"
        status: pass
    human_judgment: false
  - id: D2
    description: "A successful location update increments the stored version by 1 and refreshes the struct; the MutationDelta before/after versions match the transition; Get/List populate Version."
    requirement: "MODEL-03"
    verification:
      - kind: integration
        ref: "internal/world/postgres/location_repo_test.go#TestLocationRepository_UpdateVersionGuard/successful_update_increments_version_by_1_and_refreshes_struct"
        status: pass
    human_judgment: false
  - id: D3
    description: "A location DELETE returns a MutationDelta whose Affected list carries every FK-cascaded exit as a tombstone (INV-WORLD-2 delta-parity, finding 4)."
    requirement: "MODEL-03"
    verification:
      - kind: integration
        ref: "internal/world/postgres/location_repo_test.go#TestLocationRepository_DeleteCascadeDelta"
        status: pass
    human_judgment: false
  - id: D4
    description: "Adversarial interleave (round-6 R6-4): with the deletion tx holding the parent FOR UPDATE lock, a concurrent referencing-exit INSERT blocks then fails once the parent is gone; the manifest equals every deleted exit (no phantom child escapes)."
    requirement: "MODEL-03"
    verification:
      - kind: integration
        ref: "internal/world/postgres/location_repo_test.go#TestLocationRepository_DeleteCascadeInterleave"
        status: pass
    human_judgment: false
  - id: D5
    description: "Under a pool constrained to size 1, the zero-row classifier's locked follow-up read reuses the caller's connection and resolves to WORLD_CONCURRENT_EDIT rather than deadlocking (finding 14)."
    requirement: "MODEL-03"
    verification:
      - kind: integration
        ref: "internal/world/postgres/location_repo_test.go#TestLocationRepository_ZeroRowClassifierNoDeadlockPoolSize1"
        status: pass
    human_judgment: false
  - id: D6
    description: "Exit Update/Delete are version-predicated CAS mirroring the location repo (stale -> WORLD_CONCURRENT_EDIT, absent -> EXIT_NOT_FOUND); a bidirectional Create/Delete delta carries the reverse/cascaded return exit (finding 7); bidirectional cascade stays in one transaction."
    requirement: "MODEL-03"
    verification:
      - kind: integration
        ref: "internal/world/postgres/exit_repo_test.go#TestExitRepository_UpdateVersionGuard"
        status: pass
      - kind: integration
        ref: "internal/world/postgres/exit_repo_test.go#TestExitRepository_DeleteVersionGuard"
        status: pass
      - kind: integration
        ref: "internal/world/postgres/exit_repo_test.go#TestExitRepository_BidirectionalDelta"
        status: pass
    human_judgment: false
  - id: D7
    description: "No guarded write in location_repo.go/exit_repo.go uses r.pool.Exec directly — every write routes through execerFromCtx/withTx (grep gate) so it enrolls in an ambient transaction."
    requirement: "MODEL-03"
    verification:
      - kind: other
        ref: "grep -n 'r.pool.Exec' internal/world/postgres/location_repo.go internal/world/postgres/exit_repo.go => NONE"
        status: pass
    human_judgment: false

# Metrics
duration: ~50m
completed: 2026-07-12
status: complete
---

# Phase 5 Plan 02: Location + Exit Version-Guard CAS Summary

**MODEL-03 optimistic-concurrency mechanism for two of the four world aggregates: version-predicated CAS Update/Delete with a locked same-connection follow-up read that classifies a zero-row result into WORLD_CONCURRENT_EDIT vs NOT_FOUND, plus cascade-aware MutationDeltas and a round-6 R6-4 parent-lock-first phantom fence.**

## Performance

- **Duration:** ~50 min
- **Started:** 2026-07-12T15:59:04-04:00 (first task commit)
- **Completed:** 2026-07-12T20:10:00Z
- **Tasks:** 2
- **Files modified:** 6

## Accomplishments

- Location `Update`/`Delete` are now version-predicated CAS (`... version = version + 1 WHERE id = $1 AND version = $expected RETURNING version`); a zero-row result runs a locked follow-up read on the caller's connection (via the re-entrant `withTx` seam) that classifies it into exactly two outcomes — existing-row-version-moved → `WORLD_CONCURRENT_EDIT`, absent → `LOCATION_NOT_FOUND`.
- Exit `Update`/`Delete` mirror the location repo; bidirectional Create/Delete deltas carry the reverse/cascaded return exit (finding 7); the bidirectional cascade and non-severe `BidirectionalCleanupResult` surfacing are preserved.
- Read paths populate the struct `Version`; a successful write refreshes it and returns a `MutationDelta` whose before/after versions match the row transition (finding 12).
- Location `DELETE` locks the parent row `FOR UPDATE` FIRST, then preselects every FK-cascaded exit (`from_location_id` OR `to_location_id`) as tombstone `AffectedAggregate`s — the parent lock conflicts with the FK key-share lock a child-exit INSERT needs, fencing the child-insert phantom (round-6 R6-4). An adversarial interleave test binds INV-WORLD-2 delta-parity.
- Extracted shared helpers (`classifyCASZeroRow`, `querierFromCtx`, `primaryDeltaVersioned`) to `helpers.go`; one code path per zero-row outcome across both repos.

## Task Commits

Each task followed the TDD RED → GREEN gate:

1. **Task 1: Location repo CAS + zero-row classifier**
   - `e8e85170d` (test) — version-guard CAS RED tests
   - `24e90a8e3` (feat) — CAS + classifier + cascade delta + parent-lock-first delete
2. **Task 2: Exit repo CAS + zero-row classifier**
   - `1e4a29d06` (test) — version-guard CAS RED tests
   - `80d119f8e` (feat) — CAS + classifier + bidirectional cascade delta

## Files Created/Modified

- `internal/world/postgres/location_repo.go` — version-predicated CAS Update/Delete, `preselectCascadedExits`, version columns in all read scans, version refresh + versioned deltas.
- `internal/world/postgres/exit_repo.go` — version-predicated CAS Update/Delete, version columns in all read scans, bidirectional Create/Delete delta population, version check on the FOR UPDATE row read.
- `internal/world/postgres/helpers.go` — `classifyCASZeroRow` (two-outcome), `querierFromCtx`, `primaryDeltaVersioned`.
- `internal/world/postgres/scene_repo.go` — threaded the new `version` column through `GetScenesFor`'s SELECT so the shared location scanner's destinations match.
- `internal/world/postgres/location_repo_test.go` — Update/Delete version-guard, cascade delta, R6-4 interleave, pool-size-1 no-deadlock, version-populated reads.
- `internal/world/postgres/exit_repo_test.go` — Update/Delete version-guard, bidirectional Create/Delete delta.

## Decisions Made

- **`expectedVersion`/`Version == 0` is an unversioned (id-only) write.** Production `world.Service` delete/update callers (`service.go:244/276/357/378`) pass `0` today, and `service.go` is not in this plan's scope. Treating `0` as "no optimistic-concurrency expectation" keeps those paths green while the guard fires whenever a caller threads a read version `> 0`. Version-threading through the RMW callers is explicitly plan 05-04.
- **Two-outcome classifier, not three** (round-5 Codex MEDIUM): the ID-only delete API carries no existence token, so a concurrent delete that already committed is observed as an absent row and correctly reported not-found. No round-4 "prior in-tx existence evidence" is attempted.
- **Parent-lock-first delete ordering** (round-6 R6-4): a bare preselect of matching exits would lock only already-existing rows, leaving a child-insert phantom. Locking the parent `FOR UPDATE` first closes the window because the FK key-share lock a child-exit INSERT needs conflicts with `FOR UPDATE`.
- **MODEL-03 left `Pending`**: it requires all four aggregates (05-03 adds characters/objects) plus the two-replica surfaced-conflict verification (05-04). Marking it now would be a false-green.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Threaded the version column through `scene_repo.GetScenesFor`'s shared-scanner SELECT**
- **Found during:** Task 1 (location read-scan change)
- **Issue:** Adding `version` to the shared `scanLocations`/`scanLocationRow` destinations broke `SceneRepository.GetScenesFor`, whose 9-column SELECT then mismatched the 10 scan destinations (`number of field descriptions must equal number of destinations, got 9 and 10`).
- **Fix:** Added `l.version` to the `GetScenesFor` SELECT column list.
- **Files modified:** `internal/world/postgres/scene_repo.go`
- **Verification:** `TestSceneRepository_GetScenesFor` passes; full `internal/world/postgres` integration package green (235 tests).
- **Committed in:** `24e90a8e3` (Task 1 feat commit)

---

**Total deviations:** 1 auto-fixed (1 blocking). **Impact:** necessary to keep the shared location scanner consistent; no scope creep. (`scene_repo.go`'s vestigial `public.scene_participants` surface is slated for removal by D-07 in a different plan; this change only keeps its read path compiling.)

## Issues Encountered

- **Cross-plan interaction with `test/integration/resilience/m12_lastwritewins_test.go`:** because `Get` now populates `Version`, the M12 resilience spec's read-modify-write reproduction would surface `WORLD_CONCURRENT_EDIT` where it currently asserts silent last-write-wins. Flipping that spec (and threading versions through the RMW service callers) is **explicitly owned by plan 05-04** (its `files_modified` lists that test; my plan's does not). The resilience suite is nightly/opt-in (`HOLOMUSH_RUN_QUARANTINED=1`) and skipped in the gating path, and the phase lands as one PR (D-04), so consistency is restored on the phase branch before the PR. No action taken here — out of scope.

## Verification

- `task test:int -- ./internal/world/postgres/` — 235 tests pass (includes the new CAS/cascade/interleave/pool-size-1 specs).
- `task test:int -- ./test/integration/world/... ./internal/world/...` — 1170 tests pass (downstream consumers of the repos).
- `task test` — 10193 unit tests pass.
- `task lint` — exit 0.
- Grep gates: no `r.pool.Exec` write-path hits in `location_repo.go`/`exit_repo.go`; the location `Delete` parent lock (`SELECT version FROM locations WHERE id = $1 FOR UPDATE`) precedes the exit preselect.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- The MODEL-03 CAS mechanism is in place for locations + exits, returning populated `MutationDelta`s ready for the outbox manifest (05-10/05-11) and delta-parity (05-12).
- Plan 05-03 applies the identical guard to characters + objects (the shared `classifyCASZeroRow`/`querierFromCtx`/`primaryDeltaVersioned` helpers are ready to reuse).
- Plan 05-04 threads the read version through the RMW service callers and flips the M12 resilience spec to assert the surfaced conflict.

---
*Phase: 05-world-model-integrity-fixes-m2-m12*
*Completed: 2026-07-12*
