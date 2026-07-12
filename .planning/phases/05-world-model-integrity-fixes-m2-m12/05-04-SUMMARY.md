---
phase: 05-world-model-integrity-fixes-m2-m12
plan: 04
subsystem: world-model
tags: [optimistic-concurrency, model-03, world-model, rmw, resilience, m12, version-guard]

# Dependency graph
requires:
  - phase: 05-01
    provides: "version column + Version struct field + world.ErrConcurrentEdit / CodeConcurrentEdit (WORLD_CONCURRENT_EDIT)"
  - phase: 05-02
    provides: "location/exit version-predicated CAS + reads populate struct.Version + Update consumes struct.Version as expectedVersion"
  - phase: 05-03
    provides: "character/object version-predicated CAS + version-scanning reads for all four aggregates"
provides:
  - "Regression-pinning unit tests that the RMW callers (entity_mutator SetName/SetDescription, world.Service Update*) thread the read version and surface WORLD_CONCURRENT_EDIT unchanged (D-02)"
  - "The M12 resilience regression gate flipped to assert the surfaced conflict (was: both writers return nil, one write silently lost)"
  - "A per-aggregate (object) two-replica race spec proving the guard rejects the stale writer for a non-location aggregate"
  - "Slice 1 (MODEL-03) complete: concurrent writers cannot silently lose an update, verified under two-replica deployment"
affects: [05-10, 05-11, 05-12, resilience-m12]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "RMW version threading is automatic via the shared struct pointer: read populates struct.Version, the same struct flows read->mutate->write, Update consumes struct.Version as expectedVersion"
    - "WORLD_CONCURRENT_EDIT survives a generic *_UPDATE_FAILED context wrap because oops.Code() returns the DEEPEST code in the chain (getDeepestErrorCode) — errutil.AssertErrorCode asserts that deepest code"
    - "Deterministic two-replica conflict proof at the service level (control the interleave); command-race specs assert integrity only (HandleCommand serializes the reads)"

key-files:
  created:
    - ".planning/phases/05-world-model-integrity-fixes-m2-m12/05-04-SUMMARY.md"
  modified:
    - "internal/property/entity_mutator_test.go — RMW version-threading + conflict-surfacing pins (location + object)"
    - "internal/world/service_test.go — UpdateLocation/UpdateObject/UpdateCharacterDescription conflict-surfacing + version-threading pins"
    - "test/integration/resilience/m12_lastwritewins_test.go — flipped M12 spec: surfaced conflict + new per-aggregate object race spec"
    - "test/integration/resilience/chaos_helpers_test.go — corrected the now-false 'unguarded full-row UPDATE' comment in newWorldService"
    - "internal/world/postgres/location_repo.go — gofmt normalization only (uncommitted 05-02 fmt output)"
    - "internal/world/postgres/object_repo.go — gofmt normalization only (uncommitted 05-03 fmt output)"

key-decisions:
  - "No production code change was needed for version threading or conflict propagation: 05-02/05-03 made reads populate struct.Version and Update consume it, and the same struct pointer flows read->mutate->write through entity_mutator -> world.Mutator(*world.Service) -> repo. Task 1 delivered pinning tests, not a code change."
  - "WORLD_CONCURRENT_EDIT propagates unchanged (D-02) via oops deepest-code semantics: the service's *_UPDATE_FAILED wrap does not mask the conflict because oops.Code() returns the deepest code (samber/oops@v1.22.0 getDeepestErrorCode), which is what errutil.AssertErrorCode checks. No explicit re-propagation branch was added (it would be untestable with the repo's own assertion helper)."
  - "The M12 command-race specs (concurrent-describe, cross-field) serialize through HandleCommand, so they surface 0 conflicts at the command layer; they now assert INTEGRITY (no silent overwrite) and report a natural-window conflict count. The deterministic surfaced-conflict proof lives in the service-level specs 1 (location) and 4 (new, object), where the interleave is controlled."
  - "MODEL-03 is now Complete: the guard covers all four aggregates (05-02/05-03), the RMW callers thread the read version (pinned here), and the two-replica surfaced-conflict verification PASSES (M12 suite flipped + green)."

patterns-established:
  - "Service-level deterministic interleave (both replicas read same version -> one commits -> the stale writer is rejected with WORLD_CONCURRENT_EDIT, committed value survives) is the reliable two-replica conflict proof; command races prove integrity only."

requirements-completed: [MODEL-03]

coverage:
  - id: D1
    description: "entity_mutator SetName/SetDescription (location + object) thread the read-time struct.Version into the guarded write, not a zeroed/re-read version."
    requirement: "MODEL-03"
    verification:
      - kind: unit
        ref: "internal/property/entity_mutator_test.go#TestLocationEntityMutator_SetName_ThreadsReadVersion"
        status: pass
      - kind: unit
        ref: "internal/property/entity_mutator_test.go#TestObjectEntityMutator_SetName_ThreadsReadVersion"
        status: pass
    human_judgment: false
  - id: D2
    description: "A stale RMW write surfaces WORLD_CONCURRENT_EDIT unchanged at the service boundary (D-02) for locations, objects, and UpdateCharacterDescription."
    requirement: "MODEL-03"
    verification:
      - kind: unit
        ref: "internal/world/service_test.go#TestWorldService_UpdateLocation/surfaces_WORLD_CONCURRENT_EDIT_unchanged_on_a_stale_write"
        status: pass
      - kind: unit
        ref: "internal/world/service_test.go#TestWorldService_UpdateObject/surfaces_WORLD_CONCURRENT_EDIT_unchanged_on_a_stale_write"
        status: pass
      - kind: unit
        ref: "internal/world/service_test.go#TestWorldService_UpdateCharacterDescription/surfaces_WORLD_CONCURRENT_EDIT_unchanged_on_a_stale_write"
        status: pass
    human_judgment: false
  - id: D3
    description: "UpdateCharacterDescription threads the version read at the start of the RMW into the guarded character update (no re-read after mutation)."
    requirement: "MODEL-03"
    verification:
      - kind: unit
        ref: "internal/world/service_test.go#TestWorldService_UpdateCharacterDescription/threads_the_read_version_into_the_guarded_write"
        status: pass
    human_judgment: false
  - id: D4
    description: "Two-replica deterministic interleave on a location: the stale writer is rejected with WORLD_CONCURRENT_EDIT and the committed rename survives (no silent revert)."
    requirement: "MODEL-03"
    verification:
      - kind: integration
        ref: "test/integration/resilience/m12_lastwritewins_test.go#deterministic interleave (HOLOMUSH_RUN_QUARANTINED=1)"
        status: pass
    human_judgment: false
  - id: D5
    description: "Two-replica deterministic interleave on a non-location aggregate (object): the guard rejects the stale writer with WORLD_CONCURRENT_EDIT and A's rename survives."
    requirement: "MODEL-03"
    verification:
      - kind: integration
        ref: "test/integration/resilience/m12_lastwritewins_test.go#per-aggregate guard object (HOLOMUSH_RUN_QUARANTINED=1)"
        status: pass
    human_judgment: false
  - id: D6
    description: "Under real concurrent `describe here` commands and a command-vs-service cross-field race, there is zero silent overwrite; the surviving value is always a genuine committed write."
    requirement: "MODEL-03"
    verification:
      - kind: integration
        ref: "test/integration/resilience/m12_lastwritewins_test.go#concurrent describe + cross-field race (HOLOMUSH_RUN_QUARANTINED=1)"
        status: pass
    human_judgment: false

# Metrics
duration: ~45m
completed: 2026-07-12
status: complete
---

# Phase 5 Plan 04: Version-Threaded RMW + M12 Resilience Flip Summary

**Closes the MODEL-03 loop: the read-modify-write callers thread the read version into the guarded write so a concurrent writer between the read and the write is rejected with WORLD_CONCURRENT_EDIT instead of silently overwriting, and the standing M12 last-write-wins resilience gate is flipped from asserting the corruption to asserting the surfaced conflict — proven under two replicas / one broker / one shared DB. MODEL-03 is now Complete.**

## Performance

- **Duration:** ~45 min
- **Tasks:** 2
- **Files modified:** 4 test files (+2 repo files, gofmt-only)

## Accomplishments

- **Version threading was already end-to-end** after 05-02/05-03: reads populate `struct.Version`, `Update` consumes it as `expectedVersion`, and the same struct pointer flows `read -> mutate one field -> write` through `entity_mutator` (`SetName`/`SetDescription`) and `world.Service.UpdateCharacterDescription`. `world.Mutator` is satisfied by `*world.Service`, so no hop reconstructs the entity or drops `Version`. Task 1 therefore delivered **regression-pinning tests** rather than a production change (see Deviations).
- **WORLD_CONCURRENT_EDIT propagates unchanged (D-02)** despite the service's generic `*_UPDATE_FAILED` context wrap: `samber/oops@v1.22.0` `oops.Code()` returns the **deepest** code in the chain (`getDeepestErrorCode`), which is exactly what `errutil.AssertErrorCode` (and `oops.AsOops(err).Code()`) reads. Unit tests pin this for `UpdateLocation`/`UpdateObject`/`UpdateCharacterDescription`.
- **Flipped the M12 resilience spec** (`m12_lastwritewins_test.go`) from the corruption assertion (both writers return nil, one write silently lost) to the guarded reality:
  - **Deterministic interleave (location, service level):** both replicas read the same version, A commits, B's stale write is REJECTED with `WORLD_CONCURRENT_EDIT` and A's rename SURVIVES.
  - **New per-aggregate spec (object, service level):** the guard rejects the stale writer for a non-location aggregate too.
  - **Command races (concurrent-describe, cross-field):** assert no silent overwrite and surface any command-level conflict; they report a natural-window conflict count.
- Corrected the now-false "unguarded full-row UPDATE" comment in `newWorldService`.
- **MODEL-03 marked Complete** (REQUIREMENTS.md + traceability): the guard covers all four aggregates, the RMW callers thread the read version, and the two-replica surfaced-conflict verification passes.

## Task Commits

1. **Task 1: Thread the read version through the RMW callers (TDD)**
   - `67ee6fccf` (test) — RMW version-threading + WORLD_CONCURRENT_EDIT surfacing pins (property + world). No production change was required (behavior pre-existed from 05-02/05-03; see Deviations).
   - `2dc5b0b40` (style) — committed uncommitted gofmt normalization of the 05-02/05-03 repo arg slices (keeps `fmt:check` green).
2. **Task 2: Flip the M12 resilience spec + add a per-aggregate race spec**
   - `b82315efd` (test) — flipped M12 spec to the surfaced conflict + new object per-aggregate race spec + corrected helper comment.

## Files Created/Modified

- `internal/property/entity_mutator_test.go` — fakes + tests: SetName/SetDescription thread the read version; SetDescription surfaces WORLD_CONCURRENT_EDIT (location + object).
- `internal/world/service_test.go` — UpdateLocation/UpdateObject/UpdateCharacterDescription conflict-surfacing + a MatchedBy version-threading assertion for UpdateCharacterDescription.
- `test/integration/resilience/m12_lastwritewins_test.go` — 4 specs: deterministic interleave (location), concurrent-describe integrity, cross-field integrity, per-aggregate object conflict; verdict lines describe the surfaced conflict.
- `test/integration/resilience/chaos_helpers_test.go` — corrected the newWorldService doc comment (guard active, not "unguarded").
- `internal/world/postgres/location_repo.go`, `internal/world/postgres/object_repo.go` — gofmt normalization only (no behavior change).

## Decisions Made

- **No production code change for Task 1.** The plan's premise ("today they pass 0, the guard never fires") predates 05-02/05-03's struct-transport design. After those plans, the RMW path threads a real read version (>= 1) automatically through the shared struct pointer, and the conflict code survives the service wrap via oops deepest-code semantics. The honest TDD outcome is pinning tests that pass immediately because the behavior is already correct; fabricating a redundant `loc.Version = loc.Version` or an explicit re-propagation branch (untestable with the repo's own `errutil.AssertErrorCode`) would be noise. This is documented rather than hidden.
- **Command-race specs assert integrity, not a per-round conflict.** Empirically the two `describe here` commands serialize through `HandleCommand` (each command's read+guarded-write completes as a unit relative to the other), so the command layer surfaces 0 conflicts. An initial `conflicts >= 1` hard assertion failed for exactly this reason; it was relaxed to assert no-silent-overwrite + report the natural-window count. The deterministic surfaced-conflict proof lives in the service-level specs (1: location, 4: object), where the interleave is controlled — this fully satisfies the must-have ("asserts a surfaced conflict where it previously asserted both return nil").
- **MODEL-03 Complete.** All three legs are now in place (four-aggregate guard, version-threaded RMW, two-replica surfaced-conflict verification).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Stale comment] Corrected the "unguarded full-row UPDATE" comment in `newWorldService`**
- **Found during:** Task 2.
- **Issue:** `chaos_helpers_test.go`'s `newWorldService` doc still described the pre-guard reality ("identical unguarded full-row UPDATE (location_repo.go:73)"), now false.
- **Fix:** Updated to "version-predicated guarded CAS Update ... now that the guard closes last-write-wins."
- **Committed in:** `b82315efd`.

**2. [Rule 3 - Blocking, fmt output] Committed uncommitted gofmt normalization of the location/object repo arg slices**
- **Found during:** `task fmt` after Task 1.
- **Issue:** 05-02/05-03 left uncommitted gofmt output (slice-literal reflow) in `location_repo.go`/`object_repo.go`; leaving it would fail `fmt:check` in CI on the phase branch.
- **Fix:** Committed the fmt normalization as a dedicated `style` commit (`2dc5b0b40`), keeping the Task 1 test commit clean.

### Plan-premise deviation (documented, not auto-fixed)

**3. Task 1 required no production change.** The plan's `<action>` asked to "ensure SetName/SetDescription capture the entity's Version ... and pass it through" and marked the task `tdd="true"`. The behavior already existed after 05-02/05-03 (struct-transport threading + deepest-oops-code conflict propagation), verified empirically: the pinning tests passed on the first run with no source change. Task 1 was delivered as test coverage that pins the behavior against regression (the M12 corruption path). All Task 1 acceptance criteria are met by these tests.

---

**Total deviations:** 2 auto-fixed (1 stale comment, 1 fmt output) + 1 documented plan-premise deviation. **Impact:** no scope creep; the plan's goal (RMW threads the read version; M12 asserts the surfaced conflict; MODEL-03 complete) is fully achieved.

## Verification

- `task test` — 10202 unit tests pass (3 pre-existing skips; +9 new tests/subtests vs the 05-03 baseline).
- `task lint` — exit 0.
- `task test:int` — 10569 tests pass (the resilience suite correctly self-skips without the quarantine env; #4791).
- **M12 resilience gate (the D-05 proof), run exactly as the plan specifies:**
  `HOLOMUSH_RUN_QUARANTINED=1 task test:int -- -run TestWorldModelResilience ./test/integration/resilience/` — **PASS** (all 4 M12 specs green). Verdicts:
  - deterministic-interleave: B's stale write surfaced WORLD_CONCURRENT_EDIT; A's rename survived (B rejected, no silent revert).
  - per-aggregate-object: guard rejected B's stale object write with WORLD_CONCURRENT_EDIT; A's rename survived.
  - concurrent-describe: N=50 rounds, zero silent overwrites (dispatch serialized; not a refutation).
  - cross-field-race: 0 of N=100 natural-window races silently resurrected a field (window never interleaved; deterministic proofs in specs 1 & 4 stand).

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- **MODEL-03 is Complete** — Slice 1 of the world-model integrity work is done: concurrent writers cannot silently lose an update, proven under two-replica deployment.
- The guarded repos return populated `MutationDelta`s ready for the MODEL-04 outbox manifest (05-10/05-11) and delta-parity (05-12); nothing in this plan blocks them.
- The conflict-surfacing UX slice (telnet message / web retry affordance for WORLD_CONCURRENT_EDIT) remains deferred (D-02).

## Self-Check: PASSED

- All 4 modified test files + 2 gofmt-only repo files verified present on disk.
- All 3 task commits verified in git history (67ee6fccf, 2dc5b0b40, b82315efd).
- `task test` / `task lint` / `task test:int` all green; the quarantined M12 suite PASSES with WORLD_CONCURRENT_EDIT surfaced.

---
*Phase: 05-world-model-integrity-fixes-m2-m12*
*Completed: 2026-07-12*
