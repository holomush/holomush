---
phase: 05-world-model-integrity-fixes-m2-m12
plan: 01
subsystem: database
tags: [postgres, migrations, optimistic-concurrency, oops, world-model, cas]

# Dependency graph
requires:
  - phase: 04
    provides: existing world model (Location/Exit/Character/Object structs, migrations 000001-000048)
provides:
  - "Migration 000049: version INTEGER NOT NULL DEFAULT 1 on locations/exits/characters/objects"
  - "Version int field on world.Location/Exit/Character/Object structs"
  - "world.ErrConcurrentEdit sentinel + world.CodeConcurrentEdit (WORLD_CONCURRENT_EDIT) typed conflict signal"
affects: [05-02-guarded-location-exit-repos, 05-03-guarded-character-object-repos, 05-05-outbox, 05-06-write-executor, 05-14-transaction-foundation, 05-15-character-genesis, 05-16-character-reaping]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Optimistic-concurrency version column (mirrors access_policies.version precedent)"
    - "Single typed conflict signal via oops.Code + sentinel wrap"

key-files:
  created:
    - internal/store/migrations/000049_world_version_guard.up.sql
    - internal/store/migrations/000049_world_version_guard.down.sql
  modified:
    - internal/world/location.go
    - internal/world/exit.go
    - internal/world/character.go
    - internal/world/object.go
    - internal/world/errors.go
    - internal/world/errors_test.go
    - internal/store/migrate_test.go

key-decisions:
  - "version column mirrors access_policies.version: INTEGER NOT NULL DEFAULT 1 (DEFAULT 1 backfills existing rows atomically, no data-migration step)"
  - "entity_properties intentionally NOT versioned (one-pager §1 — four core world tables only)"
  - "Version field is inert this plan: constructors do not hand-set it; read scans + post-write refresh populate it in 05-02/05-03"
  - "WORLD_CONCURRENT_EDIT is the single typed conflict signal, propagated unchanged at world.Service (D-02 — no UX mapping in Phase 5); distinct from ErrNotFound"

patterns-established:
  - "Version guard struct field: exported Version int carries read version into a guarded CAS write and is refreshed to the committed version after write"
  - "Typed conflict error: exported sentinel + string code constant, code-asserted via errutil.AssertErrorCode"

requirements-completed: []  # MODEL-03 is ADVANCED (foundation only), NOT complete — the guarded CAS repos that satisfy it land in 05-02/05-03.

coverage:
  - id: D1
    description: "Migration 000049 adds version INTEGER NOT NULL DEFAULT 1 to locations/exits/characters/objects and reverts cleanly"
    requirement: "MODEL-03"
    verification:
      - kind: integration
        ref: "task test:int -- -run 'TestMigrat|Migration' ./internal/store/ (57 tests, up/down applies 000049)"
        status: pass
      - kind: unit
        ref: "internal/store/migrate_test.go#TestMigratorPendingMigrationsReturnsEmptyAtLatestVersion"
        status: pass
    human_judgment: false
  - id: D2
    description: "Version int field on the four world structs"
    requirement: "MODEL-03"
    verification:
      - kind: unit
        ref: "task test -- ./internal/world/ (962 tests, compiles with Version field)"
        status: pass
    human_judgment: false
  - id: D3
    description: "WORLD_CONCURRENT_EDIT typed conflict signal (ErrConcurrentEdit + CodeConcurrentEdit), distinct from ErrNotFound"
    requirement: "MODEL-03"
    verification:
      - kind: unit
        ref: "internal/world/errors_test.go#TestErrConcurrentEdit_IsTheTypedConflictSignal"
        status: pass
    human_judgment: false

# Metrics
duration: 20min
completed: 2026-07-12
status: complete
---

# Phase 5 Plan 01: World Version-Guard Foundation Summary

**Migration 000049 adds `version INTEGER NOT NULL DEFAULT 1` to the four world tables, `Version int` lands on Location/Exit/Character/Object, and `WORLD_CONCURRENT_EDIT` becomes the phase's single typed conflict signal — the interface-first foundation every slice-1 repo/executor plan builds against.**

## Performance

- **Duration:** ~20 min
- **Completed:** 2026-07-12
- **Tasks:** 3
- **Files modified:** 9 (2 created migrations, 7 modified)

## Accomplishments
- Paired idempotent migration 000049 adds an optimistic-concurrency `version` column (NOT NULL DEFAULT 1, `ADD COLUMN IF NOT EXISTS`) to `locations`, `exits`, `characters`, `objects`; the down reverts in reverse order (`DROP COLUMN IF EXISTS`). `entity_properties` intentionally untouched.
- `Version int` added to the four world structs, documented as the read-version carrier for a guarded CAS write and the field the repo refreshes post-write. Constructors do not hand-set it.
- `world.ErrConcurrentEdit` sentinel + `world.CodeConcurrentEdit` (`WORLD_CONCURRENT_EDIT`) code constant — the single typed conflict signal, code-asserted and proven distinct from `ErrNotFound`.

## Task Commits

1. **Task 1: Migration 000049 — version column on four world tables** - `01af336c0` (feat)
2. **Task 2: Version field on the four world structs** - `5eb72c9b1` (feat)
3. **Task 3: WORLD_CONCURRENT_EDIT typed error code (TDD)** - `acfeba2f0` (test, RED) → `8a5f610f3` (feat, GREEN)
4. **Deviation: migration fixture update for 000049** - `51b7725f0` (test)

## Files Created/Modified
- `internal/store/migrations/000049_world_version_guard.up.sql` - Adds `version` to the four world tables
- `internal/store/migrations/000049_world_version_guard.down.sql` - Reverts the column add
- `internal/world/location.go` / `exit.go` / `character.go` / `object.go` - `Version int` field
- `internal/world/errors.go` - `ErrConcurrentEdit` + `CodeConcurrentEdit`
- `internal/world/errors_test.go` - Code-identity + distinctness assertions
- `internal/store/migrate_test.go` - PendingMigrations fixtures updated for 000049

## Decisions Made
- `version INTEGER NOT NULL DEFAULT 1` mirrors the in-schema `access_policies.version` precedent; DEFAULT 1 backfills atomically (no data-migration, no lock storm).
- `entity_properties` is NOT versioned (one-pager §1) — the guard covers only the four core world tables.
- `Version` stays inert in this plan; population (read scans + post-write refresh) is owned by 05-02/05-03.
- `WORLD_CONCURRENT_EDIT` surfaces at the `world.Service` boundary unchanged (D-02); no UX/retry mapping in Phase 5.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Migration-set fixtures did not include 000049**
- **Found during:** Task 1 verification (running the store migration integration suite)
- **Issue:** `internal/store/migrate_test.go` `TestMigratorPendingMigrations*` hardcode the known migration version set (latest = 48). Adding migration 000049 shifted the count, failing both fixtures.
- **Fix:** Appended `49` to the expected pending list and bumped the "latest version" fixture from 48 → 49; updated the accompanying comment.
- **Files modified:** internal/store/migrate_test.go
- **Verification:** `task test -- -run TestMigratorPendingMigrations ./internal/store/` (3 tests) + `task test:int -- -run 'TestMigrat|Migration' ./internal/store/` (57 tests) both green.
- **Committed in:** `51b7725f0`

---

**Total deviations:** 1 auto-fixed (1 bug — a migration-set fixture directly caused by Task 1's new migration).
**Impact on plan:** Necessary to keep the migration fixtures truthful; no scope creep.

## Issues Encountered
None beyond the fixture deviation above.

## Requirement Status
- **MODEL-03** is ADVANCED, not complete. This plan lays the schema column + struct field + conflict code; the version-predicated CAS writes/deletes that actually close last-write-wins land in 05-02/05-03. `requirements-completed` is intentionally empty so MODEL-03 is not marked done prematurely.

## Verification
- `task test -- ./internal/world/` — 962 tests pass (structs + error compile and assert).
- `task test:int -- -run 'TestMigrat|Migration' ./internal/store/` — 57 tests pass; migration 000049 applies and reverts cleanly.
- `task lint` — exit 0 (green).

## Next Phase Readiness
- The `version` column, `Version` struct field, and `WORLD_CONCURRENT_EDIT` code are the contracts 05-02 (guarded location/exit repos) and 05-03 (guarded character/object repos) build against; both can proceed.
- No blockers.

## Self-Check: PASSED
- All created files present (migration up/down, errors.go, SUMMARY.md).
- All task commits present: 01af336c0, 5eb72c9b1, acfeba2f0, 8a5f610f3, 51b7725f0.

---
*Phase: 05-world-model-integrity-fixes-m2-m12*
*Completed: 2026-07-12*
