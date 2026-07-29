---
phase: 05-world-model-integrity-fixes-m2-m12
plan: 03
subsystem: database
tags: [postgres, pgx, optimistic-concurrency, cas, model-03, world-model, characters, objects]

# Dependency graph
requires:
  - phase: 05-01
    provides: "version column (migration 000049), Version struct field, world.ErrConcurrentEdit + CodeConcurrentEdit (WORLD_CONCURRENT_EDIT)"
  - phase: 05-02
    provides: "shared classifyCASZeroRow / querierFromCtx / primaryDeltaVersioned helpers + the re-entrant withTx seam + the location/exit CAS pattern to mirror"
  - phase: 05-14
    provides: "re-entrant withTx/execerFromCtx seam, redesigned repo write signatures returning (*wmodel.MutationDelta, error), Delete/Move/UpdateLocation expectedVersion params, object-Move enrollment on the ambient executor"
provides:
  - "Version-predicated CAS Update/Delete/UpdateLocation for the character aggregate with the shared two-outcome zero-row classifier"
  - "Version-predicated CAS Update/Delete/Move for the object aggregate (Move is now a guarded containment write)"
  - "Canonical version-scanning CharacterRepository.ListByPlayer (round-6 R6-1) — the in-boundary list the 05-16 guest reaper's CAS Delete needs"
  - "CharRepoAdapter.ListByPlayer delegates to the version-scanning repo (round-6 R6-1/R6-3) — no production character-list feeding a write returns Version==0"
  - "All four world aggregates now carry the MODEL-03 guard"
affects: [05-04, 05-10, 05-11, 05-16, world-outbox, resilience-m12]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Version-predicated CAS: UPDATE ... version = version + 1 WHERE id = $1 [AND version = $expected] RETURNING version"
    - "Guarded Move: FOR UPDATE lock reads + checks version, the containment UPDATE increments it (row already locked, no zero-row window)"
    - "Version-scanning list (ListByPlayer) as the canonical source of the expected version a later guarded delete needs"

key-files:
  created: []
  modified:
    - "internal/world/postgres/character_repo.go — CAS Update/Delete/UpdateLocation, version scans, canonical version-scanning ListByPlayer, versioned deltas + struct refresh"
    - "internal/world/postgres/object_repo.go — CAS Update/Delete, guarded Move (version-predicated), version scans in Get + 3 list SELECTs, versioned deltas + struct refresh"
    - "internal/bootstrap/setup/adapters.go — CharRepoAdapter.ListByPlayer delegates to the version-scanning repo; ListAll documented directory-only; dropped unused pgnanos import"
    - "internal/world/postgres/helpers.go — removed the now-unused primaryDelta helper (all four aggregates use primaryDeltaVersioned)"
    - "internal/world/postgres/character_repo_test.go — Update/Move/Delete version-guard, ListByPlayer, adapter version-scan integration tests"
    - "internal/world/postgres/object_repo_test.go — Update/Move/Delete version-guard integration tests"

key-decisions:
  - "expectedVersion/Version == 0 remains an unversioned (id-only) write so existing world.Service callers (which pass 0 today) stay green; version-threading is 05-04/05-10/05-11."
  - "Object Move is a guarded version-predicated write: the object row is already locked FOR UPDATE (the pre-existing TOCTOU lock), so the version is read + checked there and the containment UPDATE increments it — no separate zero-row classifier is needed for Move (unlike Update, whose predicate lives in the WHERE clause)."
  - "CharacterRepository.ListByPlayer is the canonical version-bearing list; CharRepoAdapter.ListByPlayer delegates to it (keeps world reads in the boundary). ListAll stays directory-only (id+name) and must NOT back a guarded delete (round-6 R6-3)."
  - "MODEL-03 stays Pending: this plan completes the guard for all four aggregates, but the requirement also needs version-threaded RMW callers + the two-replica surfaced-conflict verification (05-04). Marking it now would be a false-green."

patterns-established:
  - "Guarded write skeleton reused verbatim from 05-02 (withTx + version-predicated CAS + classifyCASZeroRow) for characters/objects."
  - "Version-scanning list as the source of a guarded delete's expected version (reaper path)."

requirements-completed: []

coverage:
  - id: D1
    description: "Character Update/UpdateLocation are version-predicated CAS: a stale-version write surfaces WORLD_CONCURRENT_EDIT (row untouched); an absent row surfaces CHARACTER_NOT_FOUND; a successful write increments version by 1 and refreshes the struct."
    requirement: "MODEL-03"
    verification:
      - kind: integration
        ref: "internal/world/postgres/character_repo_test.go#TestCharacterRepository_UpdateVersionGuard"
        status: pass
      - kind: integration
        ref: "internal/world/postgres/character_repo_test.go#TestCharacterRepository_MoveVersionGuard"
        status: pass
    human_judgment: false
  - id: D2
    description: "Character Delete is a version-predicated CAS (FOR UPDATE lock + expectedVersion check): stale -> WORLD_CONCURRENT_EDIT, absent -> CHARACTER_NOT_FOUND, version-matched -> tombstone delta."
    requirement: "MODEL-03"
    verification:
      - kind: integration
        ref: "internal/world/postgres/character_repo_test.go#TestCharacterRepository_DeleteVersionGuard"
        status: pass
    human_judgment: false
  - id: D3
    description: "Canonical version-scanning CharacterRepository.ListByPlayer returns each character with the STORED version (listed version == stored version), so a subsequent CAS Delete keyed on the listed version succeeds (round-6 R6-1, the guest-reaper source)."
    requirement: "MODEL-03"
    verification:
      - kind: integration
        ref: "internal/world/postgres/character_repo_test.go#TestCharacterRepository_ListByPlayer"
        status: pass
    human_judgment: false
  - id: D4
    description: "The production auth-facing CharRepoAdapter.ListByPlayer scans version (delegates to the world repo) so no production character-list feeding a later write/delete returns Version==0 (round-6 R6-1/R6-3)."
    requirement: "MODEL-03"
    verification:
      - kind: integration
        ref: "internal/world/postgres/character_repo_test.go#TestCharRepoAdapter_ListByPlayerScansVersion"
        status: pass
    human_judgment: false
  - id: D5
    description: "Object Update/Delete are version-predicated CAS (stale -> WORLD_CONCURRENT_EDIT, absent -> OBJECT_NOT_FOUND); a successful update increments version by 1 and refreshes the struct."
    requirement: "MODEL-03"
    verification:
      - kind: integration
        ref: "internal/world/postgres/object_repo_test.go#TestObjectRepository_UpdateVersionGuard"
        status: pass
      - kind: integration
        ref: "internal/world/postgres/object_repo_test.go#TestObjectRepository_DeleteVersionGuard"
        status: pass
    human_judgment: false
  - id: D6
    description: "Object Move is a guarded version-predicated containment write: a stale move surfaces WORLD_CONCURRENT_EDIT and does NOT relocate the object; a successful move increments version and its delta carries before/after versions. Move stays enrolled in the ambient executor (T-05-08)."
    requirement: "MODEL-03"
    verification:
      - kind: integration
        ref: "internal/world/postgres/object_repo_test.go#TestObjectRepository_MoveVersionGuard"
        status: pass
    human_judgment: false
  - id: D7
    description: "No guarded write in character_repo.go/object_repo.go uses r.pool.Exec directly — every write routes through withTx/execerFromCtx (grep gate) so it enrolls in an ambient transaction (T-05-08)."
    requirement: "MODEL-03"
    verification:
      - kind: other
        ref: "rg -n 'r\\.pool\\.Exec' internal/world/postgres/character_repo.go internal/world/postgres/object_repo.go => NONE"
        status: pass
    human_judgment: false

# Metrics
duration: ~40m
completed: 2026-07-12
status: complete
---

# Phase 5 Plan 03: Character + Object Version-Guard CAS Summary

**Completes the MODEL-03 optimistic-concurrency guard for the remaining two world aggregates — characters and objects — by mirroring the 05-02 location/exit CAS pattern (version-predicated Update/Delete/Move + shared two-outcome zero-row classifier + versioned MutationDeltas), and closes the round-6 R6-1/R6-3 version-scan gap with a canonical version-scanning `ListByPlayer` on the world repo that the auth adapter delegates to.**

## Performance

- **Duration:** ~40 min
- **Tasks:** 3
- **Files modified:** 6 (2 repos, 1 adapter, 1 helpers cleanup, 2 test files)

## Accomplishments

- Character `Update` and the character-move write `UpdateLocation` are now version-predicated CAS (`... version = version + 1 WHERE id = $1 [AND version = $expected] RETURNING version`); a zero-row result runs the shared `classifyCASZeroRow` on the caller's connection (`WORLD_CONCURRENT_EDIT` vs `CHARACTER_NOT_FOUND`). `Delete` locks the row `FOR UPDATE`, reads + checks the version, then deletes.
- Object `Update`/`Delete` mirror the character guard. Object `Move` — already enrolled on the ambient executor in 05-14 — became a guarded write: its pre-existing `FOR UPDATE` object lock now reads + checks the version and the containment `UPDATE` increments it, so a stale move surfaces `WORLD_CONCURRENT_EDIT` and does not relocate the object.
- Every read path scans `version` into the struct; every successful write refreshes the struct `Version` (finding 12) and returns a `primaryDeltaVersioned` whose before/after versions match the row transition.
- Added the canonical version-scanning `CharacterRepository.ListByPlayer` (round-6 R6-1): the in-boundary list whose `SELECT` includes `version`, so the 05-16 guest reaper's CAS `Delete(ctx, id, char.Version)` matches the stored `version DEFAULT 1` instead of permanently conflicting on `Version==0`.
- Repointed the production auth-facing `CharRepoAdapter.ListByPlayer` (round-6 R6-1/R6-3) to delegate to that version-scanning repo method, so no production character-list feeding a later guarded write returns `Version==0`. Documented `ListAll` as directory-only (id+name) — it must NOT back a guarded delete.
- All four world aggregates (locations/exits in 05-02, characters/objects here) now carry the MODEL-03 guard.

## Task Commits

Each task followed the TDD RED → GREEN gate:

1. **Task 1: Character repo CAS + ListByPlayer**
   - `21ea7360c` (test) — character version-guard CAS + ListByPlayer RED tests
   - `d4d97d4fd` (feat) — CAS Update/Delete/UpdateLocation + version scans + ListByPlayer
2. **Task 2: Object repo CAS + guarded Move**
   - `da6ba7b99` (test) — object version-guard CAS (incl. Move) RED tests
   - `51240d5a5` (feat) — CAS Update/Delete + guarded Move + version scans
3. **Task 3: Version-scan inventory (CharRepoAdapter.ListByPlayer)**
   - `747e3936f` (test) — adapter version-scan RED assertion
   - `6ae74f578` (feat) — adapter delegates to the version-scanning repo
   - `7b554665c` (refactor) — drop now-unused `primaryDelta` helper (lint cleanup)

## Files Created/Modified

- `internal/world/postgres/character_repo.go` — CAS `Update`/`Delete`/`UpdateLocation`, `version` in read scans, versioned deltas + struct refresh, canonical `ListByPlayer`.
- `internal/world/postgres/object_repo.go` — CAS `Update`/`Delete`, guarded `Move`, `version` in `Get` + the three list SELECTs, versioned deltas + struct refresh.
- `internal/bootstrap/setup/adapters.go` — `CharRepoAdapter.ListByPlayer` → delegates to `charRepo.ListByPlayer`; `ListAll` documented directory-only; removed unused `pgnanos` import.
- `internal/world/postgres/helpers.go` — removed the unused `primaryDelta` (all four aggregates use `primaryDeltaVersioned`).
- `internal/world/postgres/character_repo_test.go` — Update/Move/Delete version-guard, `ListByPlayer`, and adapter version-scan integration tests.
- `internal/world/postgres/object_repo_test.go` — Update/Move/Delete version-guard integration tests.

## Decisions Made

- **`expectedVersion`/`Version == 0` stays unversioned** so production `world.Service` callers passing `0` today remain green; version-threading is 05-04/05-10/05-11.
- **Object Move needs no separate zero-row classifier.** Unlike `Update` (whose version predicate lives in the `WHERE` clause and can silently affect zero rows), `Move` already holds the object row `FOR UPDATE` (the pre-existing TOCTOU lock), so the version is read + compared under that lock and the containment `UPDATE` is a safe id-predicated write. The stale case is caught deterministically before the write.
- **`CharacterRepository.ListByPlayer` is the canonical version-bearing list**; the auth adapter delegates to it (keeps world reads in the boundary). `ListAll` (id+name directory picker) stays version-blind by design and is documented as not a guarded-delete feeder (round-6 R6-3).
- **MODEL-03 left `Pending`**: the guard now covers all four aggregates, but the requirement also spans version-threaded RMW callers + the two-replica surfaced-conflict verification (05-04).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Dead code] Removed the now-unused `primaryDelta` helper**
- **Found during:** post-implementation `task lint` (unused check).
- **Issue:** With characters/objects migrated to `primaryDeltaVersioned` (locations/exits already were in 05-02), the unversioned `primaryDelta` helper in `helpers.go` had no remaining callers — `task lint` failed with `func primaryDelta is unused`.
- **Fix:** Deleted the helper (grep-confirmed zero callers across `internal/`).
- **Files modified:** `internal/world/postgres/helpers.go`
- **Verification:** `task lint` exit 0.
- **Committed in:** `7b554665c`

**2. [Rule 3 - Blocking] Dropped the unused `pgnanos` import from `adapters.go`**
- **Found during:** Task 3 (replacing the inline `ListByPlayer` scan with delegation).
- **Issue:** The old inline scan was the last `pgnanos` user in `adapters.go`; delegating left the import unused (compile error).
- **Fix:** Removed the import.
- **Files modified:** `internal/bootstrap/setup/adapters.go`
- **Committed in:** `6ae74f578` (Task 3 feat commit).

---

**Total deviations:** 2 auto-fixed (1 dead-code, 1 blocking). **Impact:** both are mechanical consequences of completing the guard; no scope creep.

## Issues Encountered

- **Same cross-plan interaction as 05-02** (`test/integration/resilience/m12_lastwritewins_test.go`): because reads now populate `Version` and writes are version-predicated, the M12 RMW reproduction will surface `WORLD_CONCURRENT_EDIT` once callers thread versions — flipping that spec and threading versions through the RMW service callers is **explicitly owned by plan 05-04**. The resilience suite is nightly/opt-in (`HOLOMUSH_RUN_QUARANTINED=1`) and skipped in the gating path; no action taken here (out of scope).

## Verification

- `task test:int -- ./internal/world/postgres/` — 263 tests pass (includes the new character/object CAS + Move + ListByPlayer + adapter specs).
- `task test:int -- ./internal/bootstrap/... ./internal/world/... ./test/integration/world/...` — 1253 tests pass (downstream consumers of the repos + the auth adapter).
- `task test` — 10193 unit tests pass (3 skipped, pre-existing).
- `task lint` — exit 0.
- Grep gate: `rg -n 'r\.pool\.Exec' internal/world/postgres/character_repo.go internal/world/postgres/object_repo.go` → NONE.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- The MODEL-03 CAS mechanism is now complete for all four world aggregates, returning populated `MutationDelta`s ready for the outbox manifest (05-10/05-11) and delta-parity (05-12).
- The canonical version-scanning `ListByPlayer` is the version source the 05-16 guest reaper's CAS `Delete` needs (D-06).
- Plan 05-04 threads the read version through the RMW service callers (MoveCharacter/MoveObject/update/delete) and flips the M12 resilience spec to assert the surfaced conflict, then can mark MODEL-03 complete.

## Self-Check: PASSED

- All 6 modified source/test files verified present on disk.
- All 7 commits (6 task + 1 lint-cleanup refactor) verified in git history.
- `task lint` / `task test` / `task test:int` all green; grep gate clean.

---
*Phase: 05-world-model-integrity-fixes-m2-m12*
*Completed: 2026-07-12*
