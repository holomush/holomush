---
phase: 05-world-model-integrity-fixes-m2-m12
plan: 10
subsystem: world-model / transactional outbox (mechanical emission rollout — location/exit/object)
tags: [outbox, mutate-seam, envelope, taxonomy, INV-WORLD-4, delta-parity, MODEL-04, D-01]
requires: [05-05, 05-06, 05-09, 05-14]
provides:
  - "world.Service.{CreateLocation,UpdateLocation,DeleteLocation,CreateExit,UpdateExit,DeleteExit,CreateObject,UpdateObject,DeleteObject,MoveObject} routed through the mutate() seam — one taxonomy-declared envelope per successful command in the same tx"
  - "worldMutator per-operation closure-builders for location/exit/object writes (own the location/exit/object/property writers)"
  - "internal/world/payloads.go new-values-only envelope payload builders (Build{Location,Exit,Object,Tombstone,ObjectMove}Payload) + local kind constants mirroring the taxonomy"
affects: [05-11, 05-12]
tech-stack:
  added: []
  patterns: [write-requires-envelope-seam, closure-identifies-operation, delta-parity-manifest, new-values-only-payload, local-kind-constants-mirror-taxonomy]
key-files:
  created: []
  modified:
    - internal/world/service.go
    - internal/world/mutator.go
    - internal/world/payloads.go
    - internal/world/payloads_test.go
    - internal/world/service_test.go
    - internal/world/postgres/cascade_delete_test.go
    - test/integration/resilience/m12_lastwritewins_test.go
decisions:
  - "All 10 location/exit/object write commands now route through mutate(); each emits exactly ONE taxonomy-declared envelope in the SAME transaction as its state change (INV-WORLD-4). The envelope's affected-aggregates manifest is finalized by the writer FROM the repo's returned MutationDelta (cascaded exits from the location delete, the reverse exit from a bidirectional exit delete), never reconstructed from command inputs (Codex finding 7)."
  - "A delete emits ONE tombstone per command; the cascade is subsumed in the manifest (not one envelope per cascaded row). A non-severe bidirectional exit cleanup still commits + emits, then surfaces the notice post-commit; a failed/no-op command emits nothing."
  - "Update/Move commands surface WORLD_CONCURRENT_EDIT unchanged on a stale write (D-02 — no auto-retry), matching the MoveCharacter/UpdateCharacterPreferences precedent."
  - "Object kind constants + payload builders are DUPLICATED as local constants in package world rather than imported from internal/world/outbox — the world→outbox import edge is forbidden (test/meta/world_import_graph_test.go); the 05-11 census asserts the command↔kind bijection across the boundary."
  - "The M12 cross-field-race resilience spec was fixed (Rule 1): the per-game feed-counter FOR UPDATE lock globally serializes the world-write phase, so the slow describe command path deterministically loses its full-row version CAS to a concurrent direct UpdateLocation. describe swallows the conflict into a user status, so the spec now determines describe's landing by read-back, not errA. Bisect-confirmed the routing change caused it."
metrics:
  duration: ~120min
  tasks: 2
  files: 7
  completed: 2026-07-13
status: complete
---

# Phase 5 Plan 10: Location/Exit/Object Write Commands Through the Outbox Summary

The first half of the D-01 full mechanical-emission rollout: convert the ten
location, exit, and object write commands from "emits nothing / dead emit path"
to "exactly one declared-kind envelope per successful command, committed in the
same transaction as the state change" (INV-WORLD-4). MoveCharacter (05-06) is the
reference; character/scene/property + the coverage census land in 05-11.

## What was built

**Task 1 — location + exit write commands (TDD).**
`CreateLocation`, `UpdateLocation`, `DeleteLocation`, `CreateExit`, `UpdateExit`,
`DeleteExit` now route through the `mutate(ctx, intent, write-closure)` seam. The
`worldMutator` gained the location/exit writers plus the property writer (for the
same-tx delete cascade) and a per-operation closure-builder for each command. Each
command: checks access + validates (unchanged), builds a new-values-only
`EnvelopeIntent` of its taxonomy-declared kind, and calls the executor — whose
`OutboxWriter.WriteIntent` finalizes the affected-aggregates manifest FROM the
repo's returned `wmodel.MutationDelta`. So a `location_deleted` tombstone carries
the DB-cascaded exit tombstones the 05-02 repo preselected under lock, and an
`exit_deleted` tombstone carries the reverse exit — INV-WORLD-2 delta-parity, not
reconstructed from inputs. A non-severe bidirectional exit cleanup (reverse exit
already gone) still commits + emits, then logs the notice post-commit; a severe
cleanup rolls the tx back with no envelope.

**Task 2 — object write commands (TDD).**
`CreateObject`, `UpdateObject`, `DeleteObject`, `MoveObject` route through the seam
with their taxonomy-declared kinds. `CreateObject` and `MoveObject` previously used
the post-commit emit path deleted in 05-06; they now emit through the outbox.
`MoveObject` reads the object first so its `object_moved` payload carries the
pre-move source containment; `DeleteObject` emits one tombstone with the property
cascade in-tx. New payload builders (`BuildObjectPayload`, `BuildObjectMovePayload`)
in `payloads.go`.

**Payloads + taxonomy binding.** `internal/world/payloads.go` gained the
intent-level, new-values-only payload builders for all four aggregates plus the
shared tombstone. Local `kind*` string constants in package `world` mirror the
`internal/world/outbox/taxonomy.go` kind strings exactly — package `world` MUST NOT
import `internal/world/outbox` (the forbidden import edge that would re-form the
round-2/round-3 cycle); the 05-11 census meta-test asserts the command↔kind
bijection across the boundary. `payloads_test.go` gained direct builder tests.

## Verification

- `task test -- ./internal/world/` — 864 tests, exit 0.
- `task test:int -- -run 'Location|Exit' ./internal/world/...` and
  `-run 'Object' ./internal/world/...` — green (399 / 179 tests); the two postgres
  cascade-delete integration tests were wired with a real
  `postgres.NewOutboxStore` OutboxWriter.
- `task test:int -- -run zzzNoMatch ./...` — every integration package compiles
  under `-tags=integration`.
- `HOLOMUSH_RUN_QUARANTINED=1 task test:int -- -run TestWorldModelResilience` — the
  full M2 dual-write + M12 last-write-wins + outbox fault-injection suite green;
  the M2 specs exercise the new UpdateLocation/CreateExit/UpdateExit/CreateObject/
  UpdateObject routing through the REAL transactional outbox and confirm the
  state+envelope commit atomically.
- `task test` (full unit suite) — 10169 tests, 4 skipped (pre-existing), exit 0.
- `task lint` — exit 0. `task build` — exit 0.
- Raw-world-SQL fence (`TestNoRawWorldSQLOutsideWriterBoundary`) stays green — every
  new write routes through the sanctioned `internal/world/postgres` writer boundary.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] M12 cross-field-race resilience spec broke under the new write serialization**
- **Found during:** post-Task-2 diligence run of the D-05 opt-in resilience suite.
- **Issue:** The M12 `cross-field race` spec races a slow `describe here` command
  (which does a SetProperty → GetLocation → `UpdateLocation` RMW inside the plugin
  host) against a fast direct `svcB.UpdateLocation`. Once UpdateLocation routes
  through the outbox, the per-game `world_feed_counter` FOR UPDATE lock globally
  serializes the write phase and widens the read→write conflict window, so the slow
  describe path deterministically loses its full-row version CAS with
  WORLD_CONCURRENT_EDIT. The describe COMMAND swallows that conflict into a
  user-facing status ("Unable to set description"), so it does NOT surface through
  `SendCommand` — but the spec assumed `errA==nil` meant describe landed, tripping
  its "both-succeeded" branch.
- **Root-cause confirmation:** bisected — reverted the three production files to
  HEAD~1 (pre-05-10) and the suite PASSED; restored and it FAILED. The routing
  change is the cause; the serialization itself is correct-by-design
  (INV-WORLD-ATOMIC-FEED gap-free ordering).
- **Fix:** the spec now determines describe's landing by READ-BACK (not `errA`) and
  counts a describe loss as a conflict, while KEEPING the anti-silent-loss guarantee
  on the service path (`errB==nil ⇒ rename landed`) and the anti-resurrection
  guarantee (`errB!=nil ⇒ rename not landed`). No production behavior change.
- **Files modified:** test/integration/resilience/m12_lastwritewins_test.go.
- **Commit:** 3bfaabb18

**2. [Rule 3 - Blocking] Two postgres cascade-delete integration tests needed a real OutboxWriter**
- **Issue:** `TestWorldService_DeleteLocation_Integration` /
  `TestWorldService_DeleteObject_Integration` construct a real world.Service and now
  route the delete through mutate() — they errored with "world write executor not
  configured".
- **Fix:** injected `OutboxWriter: postgres.NewOutboxStore(testPool)` (the same
  writer production uses).
- **Files modified:** internal/world/postgres/cascade_delete_test.go.
- **Commit:** 881b36cb1 (location) / 3bfaabb18 (object).

**3. [Rule 2 - Missing critical] Update/Move commands now surface WORLD_CONCURRENT_EDIT unchanged**
- **Issue:** the pre-existing UpdateLocation/UpdateExit path masked a concurrent-edit
  conflict as a generic `*_UPDATE_FAILED` at the top level (the typed code only
  survived deeper in the oops chain). Routing through the seam is the right moment to
  surface it truthfully.
- **Fix:** each Update/Move command maps `ErrConcurrentEdit` → top-level
  `WORLD_CONCURRENT_EDIT` (D-02 — no auto-retry), matching the
  UpdateCharacterPreferences (05-09) precedent. Chain-walking `AssertErrorCode`
  callers are unaffected.

### Scope / tracking notes

- **Requirement `MODEL-04` NOT marked complete.** This plan is the FIRST HALF of the
  D-01 rollout; MODEL-04's full emission coverage + the coverage census + the
  INV-WORLD-4 binding complete in 05-11/05-12. Marking it complete here would be
  inaccurate — deferred to phase completion / the verifier (mirrors 05-09's deferral
  of MODEL-03/04).
- **Plan counter.** `state.advance-plan` advanced the coarse Current-Plan counter to
  12; the sequential orchestrator owns wave ordering, so the counter may lead the
  actual 05-10 completion — noted for reconciliation (same caveat as 05-09).

## TDD Gate Compliance

Per the phase's established cadence, each task is a single atomic commit with the
RED/GREEN cycle followed internally: for both `tdd="true"` tasks the failing tests
were authored and observed RED before implementation (Task 1: 52 failures; Task 2:
31 failures — commands requiring the newly-mandatory executor), then GREEN before
commit. No intermediate commit ships a broken build. Commit types: `feat` (both
tasks).

## Known Stubs

None that block the plan goal. This is the first half of the rollout; the remaining
character/scene/property commands, the coverage-census meta-test (structurally
forbidding an un-migrated command — no allow-list), and the INV-WORLD-4 binding land
in 05-11/05-12, the plan's explicit forward boundary.

## Threat Flags

None. No new network endpoint, auth path, or trust-boundary schema change beyond the
plan's `<threat_model>` (T-05-29/30/31 addressed): cardinality is asserted per
command (exactly one envelope), the manifest is finalized from the repo delta
(delta-parity, bound by INV-WORLD-2 in 05-12), and payloads are intent-level
new-values-only (erasure-safe, no secrets).

## Self-Check: PASSED

- FOUND: internal/world/service.go (10 commands routed), internal/world/mutator.go
  (per-op methods + writers), internal/world/payloads.go (builders)
- FOUND commits: 881b36cb1 (Task 1 — location + exit), 3bfaabb18 (Task 2 — object + M12 fix)
- GREEN: task test (10169, exit 0), task test:int Location|Exit|Object + full integration
  compile-check + resilience suite, task lint (0), task build (0)
- VERIFIED: raw-world-SQL fence stays green (all writes route through the sanctioned writer boundary)
