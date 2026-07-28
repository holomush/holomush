---
phase: 09-test-quality-code-health-sweep
plan: 05
subsystem: store-schema
tags: [migration, index, postgres, presence, performance, reversibility]
requires:
  - "09-01 (wave-1 gate convention only — NOT a functional dependency; see Premise Verification)"
provides:
  - "idx_sessions_location_id — the presence / who-is-here query's filter column is indexed"
  - "a round-trip migration reversibility spec that the up-only integration lane cannot make"
  - "latestMigrationVersion constant, so the next migration gets one clear failure instead of three opaque ones"
affects:
  - "every ListActiveByLocation call (presence, location follow, session reaper)"
  - "internal/store migration version assertions (three pre-existing sites hardcoded 52)"
tech-stack:
  added: []
  patterns:
    - "keyed pg_indexes lookup by index NAME + indexdef column assertion, never a count of indexes on the table"
    - "round-trip migration spec: step down, assert absent, step up, assert present, step back, assert absent, reapply"
    - "negative control executed during development to prove the reversibility assertion is falsifiable"
key-files:
  created:
    - internal/store/migrations/000053_sessions_location_index.up.sql
    - internal/store/migrations/000053_sessions_location_index.down.sql
    - internal/store/migrations_sessions_location_index_integration_test.go
  modified:
    - internal/store/migrate_test.go
    - internal/store/migrate_integration_test.go
decisions:
  - "D-17/D-19 implemented as specified: migration 000053, plain idempotent CREATE INDEX IF NOT EXISTS + paired DROP INDEX IF EXISTS, no concurrent build"
  - "Index named idx_sessions_location_id, following the 000008 idx_sessions_player_session_id convention rather than inventing one"
  - "Plain (non-partial) index rather than one matching the query's full predicate — location_id is the selective term and the plain form also serves the other location-filtered session paths"
  - "Rewrote the up migration's rationale comment to avoid the literal CONCURRENTLY keyword so the plan's prohibition check is honestly satisfied rather than tripped by prose about the decision"
  - "Introduced latestMigrationVersion in migrate_integration_test.go instead of a second bare 52 -> 53 literal edit"
  - "Kept migrate_test.go's pending-migration census as an explicit literal list — deriving it from the embedded FS would make it tautological against the helper it tests"
  - "Did NOT assert the query PLAN: on an empty test table PostgreSQL correctly prefers a sequential scan regardless of the index, so an EXPLAIN assertion would be vacuous or require a synthetic large seed"
  - "QUAL-05 left Pending — this plan delivers 1 of its 5 enumerated Medium-cluster items"
metrics:
  duration: ~25min
  tasks: 1
  files: 5
  completed: 2026-07-26
status: complete
---

# Phase 09 Plan 05: Sessions Location Index Summary

Migration `000053` adds `idx_sessions_location_id`, closing the unindexed filter column behind
`ListActiveByLocation` (#4796), and ships a round-trip spec that proves the paired down migration
actually drops it — an assertion the ordinary integration lane structurally cannot make.

## What Changed

| Change | Commit |
| ------ | ------ |
| Migration 000053 up/down pair, round-trip integration spec, and three pre-existing latest-version assertions bumped | `37cd87f57` |

Final migration number: **000053**. Index name: **`idx_sessions_location_id`**.

## Premise Verification

Three of the four preceding plans in this phase carried a falsified premise, so every claim in
this plan was checked before it was trusted. All of them held:

| Plan claim | Verdict | Evidence |
| ---------- | ------- | -------- |
| `sessions.location_id` has no index across all 52 migrations | **TRUE** | `rg 'ON sessions' internal/store/migrations/` returns exactly three hits — `character_id` (partial unique), `status` (partial), `player_session_id`. None covers `location_id`. |
| 000052 is the highest number on disk | **TRUE** | Directory listing; 000053 was free and was used. |
| Three indexes already exist on the table (so a count-based assertion is unsafe) | **TRUE** | Same three hits above — this is why every assertion is keyed on the index name. |
| Issue #4796 is about this | **TRUE** | Open; title `data: sessions.location_id is unindexed — presence / who's-here query`; its AC is "add the index in a migration". |
| The presence query filters on the column | **TRUE** | `session_store.go:748` — `WHERE location_id = $1 AND status = 'active' AND grid_present = true`. |
| `depends_on: ["09-01"]` is a gate convention, not functional | **TRUE** | Nothing here reads a coverage number or touches `.codecov.yml`. |

One stale repo learning was also disproven: `.claude/rules/references/plan-review-learnings.md` states
`task test:int` ignores `--` package args. It does **not** — `Taskfile.yaml:189` interpolates
`{{.CLI_ARGS | default "./..."}}`, so `task test:int -- ./internal/store/...` scoped correctly. The
plan's verify command was sound.

## The Falsifiability Work (phase-9 defect class)

The plan's central prohibition is that `task test:int` alone cannot evidence reversibility, because
the migrator only ever moves UP during normal startup — a down migration that silently no-ops passes
the entire lane. Two things were done about it.

**1. The round-trip spec.** `migrations_sessions_location_index_integration_test.go` migrates the full
chain, steps back to 52, asserts the index is **absent** (a built-in negative control proving later
assertions observe *this* migration and not a pre-existing index), steps forward one, asserts the
index is **present** via a keyed `pg_indexes` lookup on its name *and* that its `indexdef` contains
`location_id`, steps back one, asserts it is **absent**, then reapplies. The version number is bound
to this plan's files by asserting `store.MigrationName(53) == "000053_sessions_location_index"`, so a
future renumber cannot silently redirect the round trip at some other migration while every
assertion still passes.

**2. The negative control was actually executed.** The down migration's body was emptied to a
comment-only file and the spec re-run:

```
[FAILED] index must not exist before migration 53 runs
Expected
    <string>: CREATE INDEX idx_sessions_location_id ON public.sessions USING btree (location_id)
to be empty
```

`task test:int` exited non-zero (go-task wrapper 201). The real down migration was then restored and
the suite re-run green. Two details worth recording:

- The failure landed on the **step-back-to-52** assertion, not the later one — a no-op down leaves
  the index in place from the initial `Up()`, which is precisely the silent-no-op shape the
  prohibition names.
- The sibling idempotency spec **passed** under the broken down migration. On its own it would not
  have caught a no-op down. Only the round trip does.

Per the plan's prohibition, no assertion anywhere counts indexes on the `sessions` table; every one
is keyed on the index name, and the presence assertion additionally pins the column.

## Deviations from Plan

### [Rule 3 - Blocking] Three pre-existing tests hardcoded "latest migration = 52"

- **Found during:** Task 1, first integration run.
- **Issue:** Adding migration 000053 broke three assertions that encode the head version as a
  literal — `migrate_test.go`'s pending-migration census and its "empty at latest version" test, and
  `migrate_integration_test.go`'s FullCycle spec (at two points, post-`Up()` and post-re-apply).
  Directly caused by this change, so in scope.
- **Fix:** Extended the census list to include 53, bumped the mock's latest version to 53, and
  replaced both FullCycle literals with a single named `latestMigrationVersion` constant carrying a
  comment telling the next migration author to bump it. The next migration now gets one clear failure
  at one documented place instead of three opaque numeric mismatches.
- **Deliberately not "fixed" further:** the census list was left as an explicit literal rather than
  derived from the embedded filesystem. Deriving it from `allMigrationVersions()` — the same helper
  `PendingMigrations()` uses internally — would make the assertion tautological and blind to a
  migration file that went missing, which is the one thing that census exists to catch.
- **Files modified:** `internal/store/migrate_test.go`, `internal/store/migrate_integration_test.go`
- **Commit:** `37cd87f57`

### [Rule 3 - Blocking] Acceptance check tripped by the rationale comment

- **Found during:** Task 1, acceptance-criteria checks.
- **Issue:** `rg -ci 'concurrently' internal/store/migrations/000053_*.sql` matched. Not a concurrent
  index build — the up migration's comment explaining *why* a concurrent build was rejected used the
  literal keyword.
- **Fix:** Reworded the comment to describe "PostgreSQL's non-blocking index-build form" and keep the
  full rationale (cannot run inside the runner's transaction; no precedent in 52 migrations; adopting
  it means adding non-transactional runner support, a new capability rather than a new migration).
  The check now returns no matches, and the guard was proven still capable of matching by running it
  against a scratch file containing `CREATE INDEX CONCURRENTLY` — exit 0 with a count, versus exit 1
  on the real files.
- **Files modified:** `internal/store/migrations/000053_sessions_location_index.up.sql`
- **Commit:** `37cd87f57`

## Acceptance Criteria

| Criterion | Result |
| --------- | ------ |
| Both files exist, `000053` prefix, matching snake_case suffix | PASS |
| Two-line SPDX Apache-2.0 header in SQL comment form on each | PASS |
| `rg -c 'IF NOT EXISTS' …up.sql` == 1; `rg -c 'IF EXISTS' …down.sql` == 1 | PASS (1, 1) |
| `rg -ci 'concurrently' …*.sql` no matches | PASS (exit 1; guard proven falsifiable by control) |
| `rg -ci 'room' …*.sql` no matches | PASS (exit 1) |
| Up migration names #4796 and states the query pattern | PASS |
| `task test:int -- ./internal/store/...` exits 0 | PASS (206 tests) |
| Committed round-trip test: down, assert absent, up, assert present by name, back, assert absent, reapply | PASS |
| Round-trip test FAILS when the down body is emptied | PASS — executed, failure quoted above, reverted |
| SUMMARY records the database was left reapplied | PASS — see below |
| `task lint` exits 0 | PASS |

**Database left reapplied:** the round-trip spec's final step is `migrator.Up()`, followed by an
assertion that the index is present again, so the database ends at head schema. (Each spec also runs
against its own `testutil.RawDatabase` instance, so the reapply is belt-and-braces rather than
load-bearing for suite isolation.)

## Verification

| Gate | Result |
| ---- | ------ |
| `task test -- ./internal/store/...` | exit 0 — 181 tests |
| `task test:int -- ./internal/store/...` | exit 0 — 206 tests |
| `task test:int` negative control (down body emptied) | exit non-zero — **as required** |
| `task lint` | exit 0 |
| `task fmt` | exit 0, no residual diff |

All pass/fail judgements are taken from exit codes, not from matching strings in output.

## Known Stubs

None.

## Scope Note — QUAL-05

QUAL-05 enumerates five arch-review Medium-cluster items. Delivered so far: the ABAC empty-string
sentinels (09-03), the secure-cookie default (09-04), and the `sessions.location_id` index (this
plan). The silent audit-emitter drop and the DEK read-cache remain. **QUAL-05 stays `Pending`** —
this plan's artifacts demonstrate one item, not the requirement.

## Not Done (deliberate)

Issue #4796's AC has a second clause: "verify the presence query plan." No `EXPLAIN` assertion was
added. On an empty or tiny test table PostgreSQL's planner correctly prefers a sequential scan
whether or not the index exists, so such an assertion would either be vacuous or require seeding a
synthetically large `sessions` table purely to coerce the planner — a fixture whose row count, not
the schema, would be the thing under test. What is asserted instead is falsifiable and stable: the
index exists, on the right table, covering the right column. Plan-plan verification of the live query
plan belongs against a realistically-sized database, not this migration's spec.

## Self-Check: PASSED

- `internal/store/migrations/000053_sessions_location_index.up.sql` — FOUND
- `internal/store/migrations/000053_sessions_location_index.down.sql` — FOUND
- `internal/store/migrations_sessions_location_index_integration_test.go` — FOUND
- commit `37cd87f57` — FOUND
