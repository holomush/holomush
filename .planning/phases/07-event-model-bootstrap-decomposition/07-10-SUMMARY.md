---
phase: 07-event-model-bootstrap-decomposition
plan: 10
subsystem: infra
tags: [go, lifecycle-orchestrator, bootstrap, topological-sort, graceful-shutdown, testing]

requires:
  - phase: 07-event-model-bootstrap-decomposition
    provides: "07-09 (D-12 Wave A: five eager pre-starts eliminated, TLS registered, memoized cryptoWiring builder, chain.VerifierSubsystem.DependsOn grown to include EventBus)"
provides:
  - "Orchestrator.StopAll — deadline-aware, honors ctx.Done() against a Stop that ignores its own ctx, logs abandoned subsystems at error level"
  - "StartAll's rollback path — runs on a fresh bounded context, never the (possibly already-cancelled) startup ctx"
  - "eventbus.Subsystem.DependsOn's doc comment — records why the dep set is exactly [Database] and why the CryptoChainVerifier reverse edge is forbidden forever"
  - "grpcSubsystem.DependsOn() — gains SubsystemAuditProjection (T-07-50)"
  - "cmd/holomush/core_topo_order_test.go — TestProductionSubsystemsTopologicalStartOrderIsPinned + TestProductionSubsystemGraphIsAcyclic, both reading every production subsystem's DependsOn() live"
affects: [07-11]

tech-stack:
  added: []
  patterns:
    - "Dep-carrying recording stub sourced from a REAL, zero-value-constructed production subsystem type, so a pinned topological order is never a hand-copied (and therefore driftable) list"
    - "Deadline-aware teardown: per-goroutine Stop raced against ctx.Done() via a buffered one-shot result channel, so an abandoned goroutine's eventual send never blocks"

key-files:
  created:
    - cmd/holomush/core_topo_order_test.go
    - .planning/phases/07-event-model-bootstrap-decomposition/deferred-items.md
  modified:
    - internal/lifecycle/orchestrator.go
    - internal/lifecycle/orchestrator_test.go
    - cmd/holomush/core.go
    - internal/eventbus/subsystem.go
    - cmd/holomush/sub_grpc.go
    - cmd/holomush/sub_grpc_test.go
    - cmd/holomush/gateway_imports_test.go

key-decisions:
  - "MEDIUM-11 closed by comment-deletion + topo-order pin, NOT by adding the eventbus->CryptoChainVerifier edge rev 4 shipped — that edge closes EventBus -> CryptoChainVerifier -> EventBus, a cycle topoSort refuses at boot (the verifier's handler set is built from the bus's own publisher, so the dependency direction cannot be reversed)."
  - "eventbus.Subsystem.DependsOn() stays exactly [SubsystemDatabase] (07-09's round-7 GameIDProvider edge) — pinned by an exact-set unit test, not a grep for the forbidden constant, since the doc comment legitimately names the constant it forbids."
  - "StartAll's rollback path moved to a fresh context.WithTimeout(context.Background(), 5s) instead of the startup ctx, in THIS plan rather than deferred to 07-11 — otherwise the newly deadline-aware StopAll would abandon rollback cleanup on any cancelled boot."
  - "Task 3's topological-order pin observes StartAll's behavior over dep-carrying recording stubs whose (id, deps) are read live from real, zero-value-constructed production subsystem types — never through the unexported topoSort method (unreachable from package main) and never via a hand-copied dependency list (the exact defect class that hid rev 4's cycle)."

requirements-completed: [ARCH-03]

duration: ~45min
completed: 2026-07-18
status: complete
---

# Phase 7 Plan 10: D-14 ride-alongs — bounded shutdown, MEDIUM-11 comment fix, AuditProjection edge, topo-order pin Summary

**`StopAll` now honors a deadline (and races an abandoned Stop instead of blocking forever); the false "verifier runs before EventBus" comment is deleted and replaced with a real `core_topo_order_test.go` pin of the actual 17-subsystem topological order (bus-before-verifier, verifier-before-{grpc,admin}, database-before-eventbus); gRPC now declares its `AuditProjection` dependency; and a dedicated acyclicity test — demonstrated RED against rev 4's exact `EventBus -> CryptoChainVerifier -> EventBus` cycle — proves the real post-07-09 production graph has no cycle.**

## Performance

- **Duration:** ~45min
- **Completed:** 2026-07-18
- **Tasks:** 4 (Task 1 and Task 2 each one commit; Tasks 3+4 share one file and one commit per the plan's design)
- **Files modified:** 7 (+ 1 new deferred-items.md doc)

## Accomplishments

- **`Orchestrator.StopAll` (LOW-7) now honors a deadline.** Before each `Stop`, it checks `ctx.Err()`; each `Stop` runs in its own goroutine racing a **buffered** (`make(chan error, 1)`) one-shot channel against `ctx.Done()`, so a subsystem whose `Stop` ignores its own ctx and hangs past the deadline cannot block shutdown indefinitely, and the abandoned goroutine's eventual send never blocks forever. Subsystems still unstopped at the deadline are logged at error level (T-07-52/T-07-53) via a new `logAbandonedSubsystems` helper.
- **`StartAll`'s rollback path now runs on a fresh bounded context** (`context.WithTimeout(context.Background(), 5*time.Second)`), never the startup ctx — landed in THIS plan (not deferred to 07-11, per the settlement's explicit warning): once `StopAll` is deadline-aware, handing it an already-cancelled startup ctx (e.g. a SIGINT mid-boot) would abandon every rollback `Stop` instantly.
- **`core.go`'s production shutdown defer** now builds its 5s timeout **inside** the deferred closure (matching the existing telemetry/observability shutdown idiom), never as a direct `defer orch.StopAll(shutdownCtx)` — Go evaluates deferred call arguments at registration time, so that construct would start the timer at boot and hand `StopAll` an already-dead context at actual shutdown.
- **MEDIUM-11 closed by comment-deletion + topo-order pin**, per the settlement's explicit authorization — NOT by adding the reverse edge rev 4 shipped (`eventbus.Subsystem.DependsOn() = [SubsystemCryptoChainVerifier]`), which combined with 07-09's verifier→EventBus edge closes an unbootable cycle. `eventbus.Subsystem.DependsOn()`'s doc comment now records, with file:line citations (`core.go`'s `auditPublisher` construction, `readstream_wiring.go:118`), why the dependency set stays exactly `[Database]` and why the reverse edge is forbidden forever. The false `// runs before EventBus (chain integrity check)` comment on `cryptoChainVerifierSub` in `productionSubsystems` is deleted and replaced with a comment stating the TRUE order and pointing at Task 3's pin as the asserted artifact.
- **`grpcSubsystem.DependsOn()` gains `SubsystemAuditProjection`** (T-07-50): without this edge, gRPC could begin serving before the host audit projection is up, so events served in that window would not be durably audited (a repudiation gap). Task 2's own unit test (`TestGRPCSubsystemDependsOnExpectedSubsystems`) was updated to assert the exact, grown dependency set via `assert.ElementsMatch`.
- **`cmd/holomush/core_topo_order_test.go`** (Tasks 3+4, one new file): constructs all 17 production subsystem types with zero-value configs (none allocate or touch live resources, per 07-09's D-12 Wave A), reads each one's real `DependsOn()` live, and drives a real `lifecycle.Orchestrator.StartAll` over dep-carrying recording stubs sourced from that live graph — `topoSort` itself is an unexported method unreachable from `package main`, so every property is observed through the public `StartAll` seam with zero new exported ordering surface (D-10).
  - `TestProductionSubsystemsTopologicalStartOrderIsPinned` pins the exact 17-element start sequence (Database, TLS, ABAC, Auth, Sessions, EventBus, World, Cluster, CryptoChainVerifier, OutboxRelay, Plugins, AdminSocket, Bootstrap, AuditProjection, GRPC, CryptoPolicy, RekeyCheckpointSweep) plus six named orderings: database-first, database-before-eventbus, bus-before-verifier, auditprojection-before-grpc, verifier-before-grpc, and verifier-before-admin.
  - `TestProductionSubsystemGraphIsAcyclic` proves the real production graph has no dangling edges and no cycle: `StartAll` returns no error (printing every subsystem's `ID -> DependsOn` on failure so a cycle is readable from test output alone), and the recorded start count equals the registered count (catching a `topoSort` that ever silently stopped erroring on a cycle).
  - `TestProductionSubsystemGraphAcyclicityCatchesRev4Cycle` is a permanent regression guard reconstructing rev 4's exact cycle in a local copy of the live graph (never in production code) and asserting `StartAll` rejects it.
- **`gateway_imports_test.go`'s `coreOnlyFiles` allowlist** gained `core_topo_order_test.go` (Rule 1 auto-fix): the new file legitimately constructs the core process's own production subsystems, so it imports the same domain packages `core.go` does — it is not gateway code.

## Task Commits

1. **Task 1: LOW-7 — StopAll honors a deadline** — `ba0021edf` (fix)
2. **Task 2: MEDIUM-11 and the missing AuditProjection edge** — `18a140bd6` (fix)
3. **Tasks 3+4: Pin the topological start order + prove acyclicity** — `77cb43fef` (test)

**Plan metadata:** committed alongside this SUMMARY.

## Files Created/Modified

- `internal/lifecycle/orchestrator.go` — deadline-aware `StopAll`, rollback fresh-ctx fix, `logAbandonedSubsystems` helper
- `internal/lifecycle/orchestrator_test.go` — 4 new tests (deadline-honoring x2, abandoned-subsystem logging, rollback-with-cancelled-startup-ctx)
- `cmd/holomush/core.go` — shutdown defer closure shape; `productionSubsystems`'s stale ordering comments deleted/corrected
- `internal/eventbus/subsystem.go` — `DependsOn()` doc comment extended (forbids the reverse edge, cites the forcing call chain)
- `cmd/holomush/sub_grpc.go` — `DependsOn()` gains `SubsystemAuditProjection`
- `cmd/holomush/sub_grpc_test.go` — exact-set assertion updated for the new edge
- `cmd/holomush/gateway_imports_test.go` — `coreOnlyFiles` allowlist entry for the new test file
- `cmd/holomush/core_topo_order_test.go` — new file: the topological-order pin + acyclicity proof (Tasks 3+4)
- `.planning/phases/07-event-model-bootstrap-decomposition/deferred-items.md` — new file: logs one out-of-scope pre-existing lint finding (see Issues Encountered)

## Decisions Made

See `key-decisions` frontmatter.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `TestGatewayImportsAreOnlyProtocolTranslation` failed on the new test file's domain imports**
- **Found during:** `task test -- ./cmd/holomush/` after adding `core_topo_order_test.go`
- **Issue:** The new test constructs real production subsystem types (ABAC, Auth, EventBus, audit, chain, dek, plugin/setup, session/setup, store, world/setup) to read their `DependsOn()` live — the gateway boundary gate flagged these as forbidden domain-package imports for a `cmd/holomush` file not in `coreOnlyFiles`.
- **Fix:** Added `core_topo_order_test.go` to `gateway_imports_test.go`'s `coreOnlyFiles` allowlist with a comment explaining why (it constructs the core process's own subsystems, not gateway code).
- **Files modified:** `cmd/holomush/gateway_imports_test.go`
- **Verification:** `task test -- ./cmd/holomush/` passes
- **Committed in:** `77cb43fef`

**2. [govet shadow] Renamed a closure-local `cancel` to `stopCancel`**
- **Found during:** `task lint` after Task 1's core.go edit
- **Issue:** The new shutdown closure's `stopCtx, cancel := context.WithTimeout(...)` shadowed an outer `cancel` already in scope a few lines above (from an earlier `context.WithCancel` used by the admin-socket error monitor).
- **Fix:** Renamed the closure-local variable to `stopCancel`.
- **Files modified:** `cmd/holomush/core.go`
- **Committed in:** `ba0021edf`

**Total deviations:** 2 auto-fixed (Rule 1 bug + a lint-driven rename), both necessary for `task test`/`task lint` to pass cleanly. No behavior outside the plan's stated scope was touched.

## Issues Encountered

- **Task 3's prescribed RED-proof procedure does not reproduce for the current graph, and this is a genuine structural fact worth recording rather than a defect.** The plan's action text says: "temporarily remove `lifecycle.SubsystemAuditProjection` from `grpcSubsystem.DependsOn()` (Task 2's real edge) and confirm the pin fails with gRPC moving ahead of the audit projection." I performed exactly this removal and re-ran `TestProductionSubsystemsTopologicalStartOrderIsPinned`: **it still passed, with the recorded order byte-for-byte unchanged.** Tracing Kahn's algorithm by hand explains why: `GRPC` and `AuditProjection` both become ready in the SAME round (both are gated by `Plugins`, indirectly via `Bootstrap` for GRPC and directly for AuditProjection), and within that round `Bootstrap` (SubsystemID 7) sorts ahead of `AuditProjection` (SubsystemID 10) — so `Bootstrap` is dequeued and processed first, which is what actually makes `GRPC` ready (via the `Bootstrap` edge, independent of the `AuditProjection` edge). But `AuditProjection` was *already sitting in the FIFO queue* (enqueued in the same batch as `Bootstrap`, ahead of `GRPC`'s later insertion), so it is dequeued and appears in the recorded order *before* `GRPC` regardless of whether the direct `AuditProjection -> GRPC` edge exists. Removing the edge is therefore a no-op for THIS specific graph shape — it's masked by a redundant, transitively-earlier path through `Bootstrap`.
  - This does **not** mean the edge is unnecessary or untested: it is a real, required correctness edge (T-07-50 — a repudiation-gap mitigation), and Task 2's own exact-set unit test (`TestGRPCSubsystemDependsOnExpectedSubsystems`, using `assert.ElementsMatch`) fails immediately if the edge is removed, since exact-set equality (unlike topological position) is not subject to this redundancy.
  - I verified the topo-order test file is not vacuously green in general: `TestProductionSubsystemGraphAcyclicityCatchesRev4Cycle` (and a manual RED reproduction of rev 4's exact cycle in `eventbus.Subsystem.DependsOn`, reverted before commit) genuinely fails with a readable diagnosis when a real regression (the forbidden cycle) is introduced — confirming the harness can and does catch regressions; this particular single-edge perturbation just happens to be absorbed by the graph's existing redundancy.
  - No code or test changes were made in response to this finding beyond documenting it here — the pinned order and its six named assertions remain correct statements about the live graph, and adding more assertions to force sensitivity to this specific edge would duplicate coverage `sub_grpc_test.go` already provides.
- **One pre-existing, out-of-scope `gosec` finding** (`internal/testsupport/natstest/scoped.go:29`, a 07-09-introduced smoke/dev credential placeholder) keeps `task lint` from exiting 0 overall. Logged to `deferred-items.md` per the scope-boundary rule rather than fixed, since the file is untouched by any of this plan's four tasks.
- **One flaky/transient integration test** (`internal/testsupport/natstest.TestNatstestConnHandsOutIndependentPerReplicaConnections`, an `i/o timeout` dialing a testcontainer) failed on the first `task test:int` run (before Task 2's edits landed) and passed cleanly on immediate re-run in isolation, and again in the final whole-repo `task test:int` run (10688 tests, 0 failures) — consistent with the project's documented "transient testcontainer drop under load" pattern, not a regression from this plan's changes.

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- **07-11 (Wave B, deferrable per D-12)** can build its `Prepare`/`Activate` barrier on top of this plan's now-honest bootstrap ordering: the topo-order pin (`core_topo_order_test.go`) gives 07-11 a concrete regression guard for the exact sequence its barrier work must preserve or intentionally re-derive.
- All four of this plan's `must_haves.truths` are satisfied and test-verified: `StopAll` returns within its deadline against a hanging `Stop`; the documented boot order matches what `topoSort` actually produces (pinned, not asserted in prose); the real post-07-09 production graph is proven acyclic by a test reading every edge live; gRPC declares its `AuditProjection` dependency; the chain verifier starts before both external domain surfaces (pinned by named topo assertions); and `StartAll`'s rollback path stops already-started subsystems even when the startup ctx is already cancelled.

---
*Phase: 07-event-model-bootstrap-decomposition*
*Completed: 2026-07-18*

## Self-Check: PASSED

- All key files verified present on disk: `internal/lifecycle/orchestrator.go`, `internal/lifecycle/orchestrator_test.go`, `cmd/holomush/core.go`, `internal/eventbus/subsystem.go`, `cmd/holomush/sub_grpc.go`, `cmd/holomush/sub_grpc_test.go`, `cmd/holomush/gateway_imports_test.go`, `cmd/holomush/core_topo_order_test.go`, `.planning/phases/07-event-model-bootstrap-decomposition/deferred-items.md`, this SUMMARY.
- All three commit hashes (`ba0021edf`, `18a140bd6`, `77cb43fef`) verified present in `git log --oneline --all`.
