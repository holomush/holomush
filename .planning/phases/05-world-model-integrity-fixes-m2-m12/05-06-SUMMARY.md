---
phase: 05-world-model-integrity-fixes-m2-m12
plan: 06
subsystem: world-model / transactional outbox
tags: [outbox, mutate-seam, envelope, MoveCharacter, movement-hook, M2, WR-01, MODEL-04]
requires: [05-05, 05-14]
provides:
  - "world.OutboxWriter interface + worldMutator write executor"
  - "mutate(ctx, intent, write-closure) compile-time write-requires-envelope seam"
  - "MoveCharacter routed through the same-tx outbox (first mover)"
  - "post-commit movement-hook = operational degradation (log + metric, command success)"
  - "holomush_movement_hook_failures_total metric"
  - "world stream-name builders relocated to internal/world/streams.go"
affects: [05-07, 05-08, 05-10, 05-11, 05-12]
tech-stack:
  added: []
  patterns: [write-requires-envelope-seam, closure-identifies-operation, injected-outbox-writer, post-commit-hook-degradation]
key-files:
  created:
    - internal/world/streams.go
    - internal/world/streams_test.go
  modified:
    - internal/world/mutator.go
    - internal/world/service.go
    - internal/world/service_test.go
    - internal/world/movement_hook.go
    - internal/world/movement_hook_test.go
    - internal/world/errors.go
    - internal/observability/server.go
    - .mockery.yaml
    - test/integration/resilience/chaos_helpers_test.go
    - test/integration/resilience/m2_dualwrite_test.go
    - docs/reviews/arch-review/2026-07-11/verification/f1-resilience-verdict.md
  deleted:
    - internal/world/events.go
    - internal/world/events_test.go
    - internal/world/event_store_adapter.go
    - internal/world/event_store_adapter_test.go
    - internal/world/worldtest/mock_EventEmitter.go
decisions:
  - "mutate() takes an EnvelopeIntent + a write CLOSURE (both non-optional); the closure identifies AND executes the operation (round-5 finding 1) — no intent.Kind dispatch; writer repos stay private to the executor"
  - "the injected world.OutboxWriter (postgres.OutboxStore) owns epoch/position + finalization; the executor never finalizes (round-3 blocker #1); package world imports neither outbox nor postgres (round-2 cycle fix)"
  - "post-commit movement-hook failure = operational degradation (log + metric, return success); the move_succeeded=true fail-after-commit path is deleted (round-5 finding 3)"
  - "documented consequence of a failed hook: session-derived location MAY LAG until re-sync (reconnect / explicit write) — NO automatic re-derivation in Phase 5 (round-6 Codex MEDIUM); a NON-quarantined regression test guards the anti-pattern (R6-6)"
  - "world-layer Examine{Location,Object,Character} commands removed (zero callers; examine is a read dropped from the world-change feed); the core-objects plugin owns the player-facing examine + object_examine event"
  - "the dot-relative stream-name builders are consumed by the gRPC subscribe/focus layer, so they were relocated to internal/world/streams.go rather than deleted with events.go"
metrics:
  duration: ~75min
  tasks: 3
  files: 18
  completed: 2026-07-12
status: complete
---

# Phase 5 Plan 06: MODEL-04 mutate() Seam + MoveCharacter Through the Outbox Summary

The pivotal slice-2 plan: introduce the compile-time write-requires-envelope
seam (`mutate(ctx, intent, write-closure)`), declare the `world.OutboxWriter`
interface the executor persists through, wire `MoveCharacter` end-to-end through
the same-transaction outbox, and delete the post-commit emit path outright —
folding WR-01 (D-03). The emit-path deletion, the first outbox mover, and the
resilience-test rewrite landed together (Pitfall 3).

## What was built

**Task 1 — the mutate() seam + MoveCharacter (TDD).**
`internal/world/mutator.go` gains the `world.OutboxWriter` interface
(`WriteIntent(ctx, intent, delta) (*wmodel.Envelope, error)`) and the
`worldMutator` write executor. `mutate(ctx, intent wmodel.EnvelopeIntent, write
func(ctx)(*wmodel.MutationDelta, error)) (*wmodel.MutationDelta, error)` runs, in
ONE re-entrant `Transactor.InTransaction`: (1) `delta := write(txCtx)` — the
closure runs the single version-guarded writer-repo method; (2)
`OutboxWriter.WriteIntent(txCtx, intent, delta)` — the injected writer allocates
epoch/position late, finalizes from the returned delta, persists the outbox row,
and returns the finalized envelope. Both parameters are non-optional (an
intent-less or closure-less call does not type-check), the operation is
identified by which per-operation method built the closure (no `intent.Kind`
dispatch), and the writer repos are private to the executor. Package `world`
imports neither `internal/world/outbox` nor `internal/world/postgres` — the seam
names only `wmodel` value types + the world-declared interface.

`MoveCharacter` builds a new-values-only `EnvelopeIntent` (game id, actor,
character-move payload) and routes through `worldMutator.moveCharacter`, threading
the character's read version as the CAS guard. The movement hook now fires
POST-commit; a hook failure is classified as operational degradation — logged
(`slog.WarnContext`) + `observability.RecordMovementHookFailure()` — and
`MoveCharacter` returns SUCCESS. The `move_succeeded=true` fail-after-commit path
is deleted. `MoveObject`'s emit was dropped (its outbox routing migrates in
05-10/05-11) and the unused world-layer `Examine*` commands were removed. A
NON-quarantined regression test
(`TestMoveCharacter_HookFailureIsOperationalDegradation`) proves a failing hook
leaves the move committed, the envelope emitted, and the command result success.

**Task 2 — delete the post-commit emit path (folds WR-01 / D-03).**
`internal/world/events.go` + `event_store_adapter.go` (and their `_test.go`)
deleted: `EmitMoveEvent`/`EmitExamineEvent`/`EmitObject*`, `emitWithRetry`, the
`EventEmitter`/`EventAppender` interfaces, `EVENT_EMITTER_MISSING` /
`EVENT_EMIT_FAILED`, the `sethvargo/go-retry` ~350ms ctx-expiry window (the WR-01
accident), and `ErrNoEventEmitter`. The still-needed dot-relative stream-name
builders (`LocationStream`/`CharacterStream`/`BroadcastLocationStream` — consumed
by the gRPC subscribe/focus layer, not the emit path) were relocated to the new
`internal/world/streams.go`. The orphaned world `EventEmitter` mockery entry +
generated mock were removed.

**Task 3 — M2 resilience rewrite + f1 verdict correction.**
`chaos_helpers_test.go`'s `newWorldService` now wires the `OutboxWriter` +
`GameID`; the deleted `newEmittingWorldService`/`worldBusAppender` machinery is
gone. `m2_dualwrite_test.go` asserts the new mechanism: a healthy-broker move and
a broker-frozen move each commit state + exactly one outbox envelope atomically
(no orphan either way), and the caller sees SUCCESS — the M2 dual-write window is
closed. The `f1-resilience-verdict.md` M2 Mechanism paragraph + citation chain
now describe the transactional outbox (the historical M2-VERDICT evidence lines
are retained as the characterized-window record).

## Examine-consumer audit (Task 2 requirement)

`rg -n 'examine' plugins/ internal/web/ web/`: the player-facing `examine`
command and its `core-objects:object_examine` event are owned by the
`core-objects` plugin (`main.lua` `register_emit_type("object_examine")`,
consumed by the web translate display registry) — an independent path. The
world-layer `Examine*` service methods (which emitted `EmitExamineEvent` to the
location stream) had ZERO production callers, so no consumer subscribed to the
world-emitted examine notification. Deletion is safe: examine is a read, dropped
from the world-change feed (RESEARCH Open Question 1). The plugin is untouched.

## Verification

- `task build:all` — green (import cycle would be a compile error; the seam is cycle-free).
- `task test -- ./internal/world/ ./internal/observability/` — green (871 tests).
- `task lint` — green (0 issues; fixed a govet shadow, an unparam return, and two wrapcheck sites in the seam).
- `task test:int -- -run 'zzzNoMatch' ./...` — every integration package compiles under `-tags=integration` (Pitfall 3: events.go deletion breaks no integration file).
- **M2 resilience spec RUN (D-05 opt-in gate):**
  `HOLOMUSH_RUN_QUARANTINED=1 task test:int -- -run TestWorldModelResilience ./test/integration/resilience/` — PASS (15.4s). Captured M2-VERDICT lines:
  - control: healthy broker — move committed AND exactly one move envelope committed in the SAME transaction.
  - flap-window: broker frozen mid-move — state + envelope committed in the SAME transaction, caller saw SUCCESS (no move_succeeded=true); the M2 non-atomicity window is CLOSED.
  - no-orphan: 2 committed move envelopes, every one resolving to the real character row — state and envelope are 1:1 atomic.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Stream-name builders are load-bearing for gRPC — relocated, not deleted**
- **Found during:** Task 2 (pre-delete importer audit).
- **Issue:** The plan's Task 2 framed `LocationStream`/`CharacterStream`/`BroadcastLocationStream` as relay-only ("relocate into the outbox/relay package, 05-07 consumes them"). A repo audit showed they are used NOW by the gRPC subscribe/focus layer (`internal/grpc/server.go`, `location_follow.go`, `list_session_streams.go`, `focus/restore.go`). Deleting them with `events.go` would break `task build:all`.
- **Fix:** Relocated the builders (verbatim) to a new `internal/world/streams.go` in package `world`, so `world.LocationStream` still resolves. `events.go` no longer exists (acceptance criterion met); the builders survive.
- **Files modified:** internal/world/streams.go (new), internal/world/streams_test.go (new).
- **Commit:** 86fb0c105

**2. [Rule 2 - Missing critical] GameID injection required for the intent**
- **Found during:** Task 1 (building the EnvelopeIntent).
- **Issue:** `wmodel.EnvelopeIntent.GameID` is required (keys the feed counter + outbox row), but `world.Service` had no game identity.
- **Fix:** Added `ServiceConfig.GameID` (defaults to `"main"`, matching the existing worldBusAppender fallback); the resilience harness passes `s.GameID()`. Subsystem wiring is 05-07 per the plan.
- **Files modified:** internal/world/service.go.
- **Commit:** b35061009

**3. [Rule 1 - Cleanup] Orphaned world EventEmitter mock + mockery entry**
- **Found during:** Task 2 (after deleting the EventEmitter interface).
- **Issue:** `internal/world/worldtest/mock_EventEmitter.go` + its `.mockery.yaml` entry targeted the now-deleted `world.EventEmitter`; `mockery` regeneration would fail and the committed mock would be a stale diff.
- **Fix:** Deleted the mock file and the `internal/world` `EventEmitter` mockery entry (the unrelated `internal/plugin` EventEmitter entry is untouched).
- **Files modified:** .mockery.yaml, internal/world/worldtest/mock_EventEmitter.go (deleted).
- **Commit:** 86fb0c105

### Scope note (within plan intent)

The plan's "Remove the s.eventEmitter field and every EmitMoveEvent call site"
was applied fully: `MoveObject` dropped its emit (envelope routing deferred to
05-10/05-11), and the three unused world-layer `Examine*` commands (whose only
behavior was the now-deleted examine emit and which had zero production callers)
were removed with their tests. This is the emit-path deletion the plan mandates;
`MoveObject` itself is KEPT for its 05-10/05-11 outbox migration.

## Known Stubs

None that block the plan goal. `MoveObject` performs a guarded write without an
envelope until its 05-10/05-11 outbox migration; the genuine compile-time
reader-view fence (Service holds no directly-callable write repo) is explicitly
completed in 05-11 after every command routes through `mutate()` — closing it
here would break compilation of the un-migrated commands (Codex finding 2). The
subsystem `OutboxWriter`/`GameID` wiring is 05-07 (production `world.Service`
logs a benign warn until then). These are the plan's explicit forward boundary.

## Threat Flags

None. No new network endpoint, auth path, or trust-boundary schema change was
introduced beyond the plan's `<threat_model>` (T-05-16/17/18/58 are addressed).

## Self-Check: PASSED

- FOUND: internal/world/streams.go, internal/world/streams_test.go, internal/world/mutator.go
- DELETED-OK: internal/world/events.go, event_store_adapter.go, worldtest/mock_EventEmitter.go
- FOUND commits: b35061009 (Task 1), 86fb0c105 (Task 2), f28e83ab5 (Task 3), 905c3dee3 (summary)
- `task build:all` + `task test -- ./internal/world/ ./internal/observability/` + `task lint` + integration compile (`./...`) + the M2 resilience suite all green.
