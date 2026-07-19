---
phase: 07-event-model-bootstrap-decomposition
plan: 11
subsystem: infra
tags: [lifecycle, orchestrator, subsystem, boot, prepare-activate, cluster, plugin, audit, grpc, tls]

requires:
  - phase: 07-09
    provides: "D-12 Wave A — five eager subsystem starts eliminated, memoized cryptoWiring builder, TLS registered as a real subsystem"
  - phase: 07-10
    provides: "D-14 ride-alongs — StopAll deadline handling, topological start-order pin + acyclicity test"
provides:
  - "lifecycle.Subsystem interface split into Prepare/Activate/Stop (D-12 Wave B)"
  - "Orchestrator.StartAll as two full sweeps (Prepare-all, then Activate-all) with a structural acquire-before-serve barrier"
  - "All 17 production subsystems migrated to the new interface, each with a plan-settled Prepare/Activate/idempotency disposition"
  - "Executable property test proving no Activate precedes any Prepare over the real production dependency graph"
affects: [phase-07-goal-verification, future-subsystem-additions]

tech-stack:
  added: []
  patterns:
    - "Two-sweep orchestrator barrier (Prepare sweep, then Activate sweep) replacing per-edge DependsOn discipline as the acquire-before-serve enforcement mechanism"
    - "Phase-owned idempotency guards (a field the owning phase itself sets) instead of a single shared started flag"
    - "Per-policy-name completion map for multi-item Activate loops (crypto policy emit) instead of a single bool"

key-files:
  created:
    - internal/eventbus/audit/subsystem_prepare_retry_integration_test.go
  modified:
    - internal/lifecycle/subsystem.go
    - internal/lifecycle/orchestrator.go
    - internal/lifecycle/orchestrator_test.go
    - internal/store/subsystem.go
    - internal/tls/subsystem.go
    - internal/eventbus/subsystem.go
    - internal/access/setup/subsystem.go
    - internal/auth/setup/subsystem.go
    - internal/world/setup/subsystem.go
    - internal/world/setup/relay_subsystem.go
    - internal/session/setup/subsystem.go
    - internal/bootstrap/setup/subsystem.go
    - internal/plugin/setup/subsystem.go
    - internal/eventbus/audit/subsystem.go
    - internal/eventbus/audit/chain/verifier_subsystem.go
    - internal/eventbus/crypto/dek/sweep.go
    - internal/cluster/registry.go
    - internal/cluster/heartbeat.go
    - internal/admin/socket/subsystem.go
    - internal/admin/policy/subsystem.go
    - cmd/holomush/sub_grpc.go
    - cmd/holomush/core.go
    - cmd/holomush/cryptowiring.go

key-decisions:
  - "The Prepare/Activate barrier is scoped to externally-reachable DOMAIN surfaces and host-owned domain work loops, not 'anything running' — process-internal substrate (embedded NATS, DB pool, plugin subprocess launch) legitimately comes up in Prepare because peer subsystems acquire against it"
  - "invalidation.Coordinator's construction+Start() stays bundled inside the memoized cryptoWiring builder (unchanged from pre-07-11) rather than split so c.Start() runs specifically from grpcSubsystem's Activate — CryptoChainVerifier is topologically required to prepare before grpcSubsystem, so it (not necessarily grpcSubsystem) is often the actual first resolver of the memoized builder in the running system; the Coordinator's own pub/sub is process/cluster-internal invalidation signaling, not client-facing domain traffic, so confining its construction+Start to the Prepare sweep (which it always is) preserves D-13.0's guarantee even though it does not literally match the row-16 text's phrasing"
  - "Crypto-policy Activate idempotency is a per-policy-name completion map (emitted map[string]bool), not a single bool — a retry after a mid-loop failure re-emits only the not-yet-emitted names"
  - "Audit's prepared aggregate (preparedProjection, partitionManager, plus the lateInit-written cfg.Owners/pluginMgr) is captured and restorable on a Prepare failure; a prepared-only Stop clears in-memory state but intentionally retains the durable JetStream consumer server-side (idempotent CreateOrUpdateConsumer re-attaches on retry)"
  - "PluginSubsystem's cleanupOnError now closes both binaryHost and luaHost (nil-guarded) on every pre-manager Prepare failure path, making Stop's s.manager == nil early return a true no-op instead of a token-store-sweeper-goroutine leak"

requirements-completed: [ARCH-03]

coverage:
  - id: D1
    description: "lifecycle.Subsystem interface split into Prepare/Activate/Stop; Orchestrator runs two full topological sweeps with rollback on either phase"
    requirement: "ARCH-03"
    verification:
      - kind: unit
        ref: "internal/lifecycle/orchestrator_test.go#TestStartAllRunsAllPreparesBeforeAnyActivate"
        status: pass
      - kind: unit
        ref: "internal/lifecycle/orchestrator_test.go#TestOrchestratorRollbackStopsFailingSubsystemToo"
        status: pass
      - kind: unit
        ref: "internal/lifecycle/orchestrator_test.go#TestOrchestratorActivateFailureRollsBackEverythingPrepared"
        status: pass
    human_judgment: false
  - id: D2
    description: "All 17 production subsystems migrated to Prepare/Activate with a plan-settled per-subsystem disposition; every caller updated to call both phases"
    requirement: "ARCH-03"
    verification:
      - kind: unit
        ref: "task build (whole binary compiles)"
        status: pass
      - kind: unit
        ref: "task test (10279 tests)"
        status: pass
      - kind: integration
        ref: "task test:int (10699 tests)"
        status: pass
    human_judgment: false
  - id: D3
    description: "Executable property proving no Activate precedes any Prepare over the REAL production dependency graph — D-11's guarantee as a structural test, not per-subsystem inspection"
    requirement: "ARCH-03"
    verification:
      - kind: unit
        ref: "cmd/holomush/core_subsystems_test.go#TestStartAllActivatesNothingUntilEverySubsystemHasPrepared"
        status: pass
    human_judgment: false
  - id: D4
    description: "KEK-wired full boot (admin authenticate/rekey E2E) still passes end to end under the two-sweep orchestrator"
    requirement: "ARCH-03"
    verification:
      - kind: integration
        ref: "task test:int -- -run 'TestAdminAuthenticate|AdminRekey' ./cmd/holomush/"
        status: pass
    human_judgment: false

duration: ~4h
completed: 2026-07-18
status: complete
---

# Phase 07 Plan 11: Prepare/Activate Lifecycle Split Summary

**Split `lifecycle.Subsystem.Start` into `Prepare`/`Activate` across all 17 production subsystems and ~30 callers, so the orchestrator's two-sweep structure — not per-edge `DependsOn` discipline — makes "no subsystem serves before every subsystem has acquired" a structural, executable guarantee.**

## Performance

- **Duration:** ~4h
- **Completed:** 2026-07-18
- **Tasks:** 3 (squashed into one commit per the plan's round-11 unit-discipline directive)
- **Files modified:** 63, plus 1 new integration test file (64 total)

## Accomplishments

- `lifecycle.Subsystem` now declares `Prepare(ctx) error`, `Activate(ctx) error`, `Stop(ctx) error` — `Start` no longer exists on the interface.
- `Orchestrator.StartAll` runs two full topological sweeps (`preparedOrder`, `activatedOrder` replacing `startOrder`): every subsystem's `Prepare`, then every subsystem's `Activate`. Rollback fixed on both paths: the failing subsystem itself is now included in `Stop` (not just its predecessors), and rollback always runs on a fresh `context.Background()`-derived context, never the (possibly already-cancelled) startup ctx.
- All 17 production subsystems migrated per the plan's settled D-13.3 disposition table (reproduced below), each with its own idempotency verdict from D-13.2.
- `cluster.Registry` (the one interface-declared subsystem) split across three files: the interface (`registry.go`), the impl (`heartbeat.go` — provider resolution in `Prepare`, the whole locked subscribe/publish/goroutine critical section unsplit in `Activate`), and both `clustertest` callers.
- `PluginSubsystem`'s partial-Prepare leak (D-13.2 row 9, cross-AI round 9 BLOCKER) fixed: `cleanupOnError` now closes both `binaryHost` and `luaHost` on every pre-manager error path, proven by a goleak fault-injection test.
- `audit.Subsystem`'s prepared-vs-activated state split (row 10, rounds 7-9): `preparedProjection`/`partitionManager` fields, lateInit-restore-on-failure, and a prepared-only `Stop` path that clears in-memory state while retaining the durable JetStream consumer server-side.
- `grpcSubsystem` (row 16, the whole reason D-11 exists): both reapers now construct in `Prepare` and launch in `Activate`, proven by a focused test that neither runs after `Prepare` alone; `GuestReaperInterval` added as a config seam for that test's determinism.
- `admin/socket` (row 14): `Prepare` only constructs the `Server`; `Activate` guards `s.server == nil` (disabled mode) before calling the existing atomic `srv.Start()` — proven by a migrated two-phase disabled-mode test.
- `admin/policy` (row 15): Activate idempotency is a per-policy-name completion map, proven by an integration test that a mid-loop failure retries only the not-yet-emitted suffix.
- New executable property test (`TestStartAllActivatesNothingUntilEverySubsystemHasPrepared`) over the REAL production dependency graph asserts the index of the first `Activate` is greater than the index of the last `Prepare` — D-11's whole guarantee as one assertion, RED-proven against a temporary per-subsystem Prepare-then-Activate rewrite and reverted.
- Full verification green: `task build`, `task test` (10279 tests), `task test:int` (10699 tests, including the KEK-wired admin authenticate/rekey E2E suite), the topo-order pin, and `task lint:go`/`task lint` (clean except one pre-existing, already-deferred gosec finding unrelated to this plan).

## Task Commits

Per the plan's round-11 (rev 14) unit-discipline directive, Tasks 1–3 are ONE atomic execution unit: the interface split (Task 1) leaves 16 production impls and 2 test stubs uncompiled, and migrating all 17 impls (Task 2) still leaves 2 Start-only test stubs (`cmd/holomush/core_subsystems_test.go`'s `stubSubsystem`, `internal/eventbus/crypto/invalidation/coordinator_error_test.go`'s `stubRegistry`) uncompiled until Task 3. No intermediate commit can build. All three tasks' edits land in the single commit below.

1. **Tasks 1–3: split `Start` into `Prepare`/`Activate` across all 17 subsystems** - `ecbefb990` (refactor)

**Plan metadata:** committed alongside this SUMMARY (`docs(07-11): ...`)

## Files Created/Modified

63 modified + 1 new (64 total). Grouped by area:

**Lifecycle core:** `internal/lifecycle/subsystem.go`, `internal/lifecycle/orchestrator.go`, `internal/lifecycle/orchestrator_test.go`

**17 production subsystems:** `internal/store/subsystem.go`, `internal/tls/subsystem.go`, `internal/eventbus/subsystem.go`, `internal/access/setup/subsystem.go`, `internal/auth/setup/subsystem.go`, `internal/world/setup/subsystem.go`, `internal/world/setup/relay_subsystem.go`, `internal/session/setup/subsystem.go`, `internal/bootstrap/setup/subsystem.go`, `internal/plugin/setup/subsystem.go`, `internal/eventbus/audit/subsystem.go`, `internal/eventbus/audit/chain/verifier_subsystem.go`, `internal/eventbus/crypto/dek/sweep.go`, `internal/cluster/registry.go` + `internal/cluster/heartbeat.go`, `internal/admin/socket/subsystem.go`, `internal/admin/policy/subsystem.go`, `cmd/holomush/sub_grpc.go`, `internal/tls/subsystem.go`

**cmd/holomush wiring:** `cmd/holomush/core.go`, `cmd/holomush/cryptowiring.go`, `cmd/holomush/core_subsystems_test.go` (+ new property test), `cmd/holomush/core_topo_order_test.go` (depStubSubsystem migrated), `cmd/holomush/gateway_imports_test.go` (added `core_subsystems_test.go` to `coreOnlyFiles` — its new property test needs the same domain imports `core_topo_order_test.go` already has)

**cluster:** `internal/cluster/clustertest/harness.go`, `internal/cluster/clustertest/external.go`, `internal/cluster/registry_internal_test.go` (+ compile-time assertion)

**Test-support harnesses:** `internal/testsupport/integrationtest/{harness.go,plugins.go,real_abac.go}`, `internal/eventbus/eventbustest/embedded.go`

**~27 caller test files** (unit + integration) migrated `.Start(ctx)` → `.Prepare(ctx)` + `.Activate(ctx)` (or Prepare-only with an inline comment where the test deliberately drives only the fail-closed dependency check): `internal/admin/socket/{subsystem_test.go,subsystem_integration_test.go}`, `internal/admin/policy/{subsystem_test.go,subsystem_integration_test.go}`, `internal/auth/setup/subsystem_internal_test.go`, `internal/eventbus/{subsystem_test.go,subsystem_exporter_internal_test.go}`, `internal/eventbus/audit/{subsystem_test.go,projection_test.go,subsystem_boot_gate_integration_test.go,dlq_capture_integration_test.go}`, `internal/eventbus/audit/chain/verifier_subsystem_test.go`, `internal/eventbus/crypto/dek/sweep_integration_test.go`, `internal/tls/subsystem_test.go`, `internal/world/setup/relay_subsystem_test.go` (+ compile-time assertion, + repeated-Prepare/Activate tests with a real-but-unreachable `pgxpool.Pool` fake), `plugins/core-scenes/publish_snapshot_integration_test.go`, `test/integration/admin_policy_chain_test.go`, `test/integration/crypto/{e2e_test.go,emit_test.go,metadata_only_test.go,plugin_decrypt_test.go,readback_test.go}`, `test/integration/eventbus_e2e/{audit_drift_detector_test.go,audit_only_channel_test.go,js_storage_corruption_test.go,plugin_audit_isolation_test.go,plugin_audit_round_trip_test.go,plugin_crash_resilience_test.go,rendering_completeness_test.go,soak_test.go}`, `test/integration/eventbus_external/external_boot_test.go`, `test/integration/privacy/scene_history_readback_test.go`

**New:** `internal/eventbus/audit/subsystem_prepare_retry_integration_test.go` (row-10 Prepare-fail/retry stale-lateInit test)

## D-13.3 Disposition Table (as executed)

| # | Subsystem | `Prepare` actions | `Activate` actions | Idempotency verdict | Test |
|---|---|---|---|---|---|
| 1 | `store.DatabaseSubsystem` | Entire former `Start` body: pgxpool + event store + `InitGameID` | No-op | KEEP existing `s.eventStore != nil` guard; deleted pre-start rationale text | Existing coverage |
| 2 | `eventbus.Subsystem` | Entire body incl. `go s.server.Start()` (embedded server `DontListen:true`); Prometheus monitor bind is D-13.0 exception 1 | No-op — publishing happens through consumers | KEEP existing `s.conn != nil` guard | `TestSubsystemPrepareAfterPrepareIsNoop` |
| 3 | `setup.ABACSubsystem` | Build stack + crypto-operator validation + health registration | Poller goroutine | KEEP `s.stack != nil` on Prepare (pre-start text deleted, "duplicate poller" parenthetical kept + reworded); ADD `s.pollerCancel != nil` guard on Activate | Guard verified by reading |
| 4 | `setup.AuthSubsystem` | Entire body — construction only | No-op | KEEP existing guard | — |
| 5 | `setup.WorldSubsystem` | Entire body — acquire+wire | No-op | None needed — benign reassignment | — |
| 6 | `setup.OutboxRelaySubsystem` | Waker + relay + consumer construction | Drain-loop goroutine launch | ADD `s.relay != nil` (Prepare) + `s.done != nil` (Activate) guards | `TestOutboxRelaySubsystemRepeatedActivateLaunchesOnlyOneDrainGoroutine`, `TestOutboxRelaySubsystemRepeatedPrepareDoesNotRebuildRelay` |
| 7 | `setup.SessionSubsystem` | Store construction | Documented no-op (reaper is grpcSub-owned) | None needed | — |
| 8 | `setup.BootstrapSubsystem` | Entire body — one-shot seeding | No-op | None — re-run harmless | — |
| 9 | `setup.PluginSubsystem` | Entire body incl. binary subprocess launch + Lua loading | Documented no-op (delivery is synchronous, manager-owned) | `cleanupOnError` extended to close `binaryHost`+`luaHost` on every pre-manager error path | `TestPluginSubsystemPrepareFailureAfterHostConstructionLeavesNoSweeperGoroutine` (goleak) |
| 10 | `audit.Subsystem` | Boot gate (Backfill+EnsurePartitions) + durable consumer construction; lateInit capture/restore | `p.start` + plugin consumers + retention worker | ADD `preparedProjection`/`partitionManager` fields; Prepare guards on `preparedProjection`, Activate on `worker` (fails closed if unprepared) | `TestAuditSubsystemRepeatedPrepareDoesNotConstructSecondProjection`, `TestAuditSubsystemStopAfterPrepareOnlyIsCleanNoop`, `TestAuditSubsystemPrepareFailureAfterLateInitRestoresPreviousState` |
| 11 | `chain.VerifierSubsystem` | Entire body — the INV-CRYPTO-102 chain walk (a boot gate) | No-op | None needed | Existing tests migrated to Prepare-only (failure cases) / Prepare+Activate (success) |
| 12 | `dek.CheckpointSweepSubsystem` | Provider resolution only | Boot sweep + tick loop | ADD `s.done != nil` guard | `Sweep_RepeatedActivateRunsOnlyOneBootSweepAndTickLoop` |
| 13 | `cluster.registry` | Resolve+validate ConnProvider/ClusterIDProvider (round-6 BLOCKER: acquisition, not no-op) | Whole locked subscribe/publish/goroutine critical section, unsplit | KEEP existing `subAlive != nil` guard, now on Activate | Existing coverage |
| 14 | `socket.AdminSocketSubsystem` | `NewServer(Config{...})` only (disabled-mode early return) | `s.server == nil` guard (disabled mode) THEN atomic `srv.Start()` | ADD `s.server == nil` + `s.errCh != nil` guards | `TestAdminSocketSubsystemBothPhasesAreNoopWhenSocketPathEmpty`, `TestAdminSocketSubsystemActivateIsIdempotentWithFlock` |
| 15 | `policy.CryptoPolicySubsystem` | Resolve emit deps | Per-policy-name snapshot emit | ADD `emitted map[string]bool` (round-6: single bool is unsound for multi-policy configs) | `TestCryptoPolicySubsystemRepeatedActivateEmitsNothingNew` (unit) + integration retry-suffix tests |
| 16 | `grpcSubsystem` | Build server, wire emitter, construct Coordinator (via memoized builder — see Decision below), construct BOTH reapers + shared ctx/cancel | Launch both reaper goroutines FIRST, then bind listener + serve | KEEP `s.grpcServer != nil` (Prepare); ADD `s.listener != nil` (Activate) | Row-16 focused test (`cmd/holomush/sub_grpc_test.go`) proving neither reaper runs after Prepare alone |
| 17 | `tls.TLSSubsystem` | Entire body — filesystem cert generation | No-op | None — `EnsureCerts` self-idempotent | — |

## Decisions Made

1. **Barrier scope (D-13.0):** confirmed as-designed — process-internal substrate (embedded NATS, DB pool, plugin subprocess) legitimately comes up in `Prepare`; only externally-reachable DOMAIN surfaces (gRPC listener, `admin.sock`) and host-owned domain work loops are barred from `Prepare`.
2. **invalidation.Coordinator placement (deviation from the row-16 text, documented and grounded):** the plan's settled text says "construction → grpcSub's Prepare; `c.Start()` → grpcSub's Activate." Verified live that this is not literally achievable without a deeper refactor: the Coordinator's construction+`Start()` are bundled inside the memoized `cryptoWiring` builder, which multiple subsystems (chain verifier, admin socket, admin policy, dek sweep) each resolve via their own Provider closures during their own `Prepare`. `CryptoChainVerifier` is a hard `DependsOn` prerequisite of `grpcSubsystem` (pinned by `core_topo_order_test.go`'s `verifierIdx < grpcIdx`), so chain verifier's `Prepare` — not necessarily grpcSubsystem's — is frequently the actual first caller of the memoized builder in the running system; this was already true pre-07-11 and the "grpcSub is the first caller" comments in `cryptowiring.go`/`core.go` were already inaccurate before this plan. Rather than restructure the memoized builder to separate Coordinator construction from `Start()` (out of scope, and risky in a security-sensitive area), the Coordinator's construction+Start stays bundled and confined to the Prepare sweep — wherever it fires, it fires during Prepare, strictly before any Activate, which is what D-13.0's actual guarantee requires. The Coordinator's own pub/sub is process/cluster-internal invalidation signaling over the cluster's own already-established connection, not client-facing domain traffic — the same class of exception D-13.0 grants eventbus's whole Prepare body. Documented inline at `cryptoWiringInputs.CoordHolder`'s doc comment and in `cmd/holomush/core.go`.
3. **Row-15 idempotency:** per-policy-name completion map, not a single bool, because `PolicyNames` is a config-driven `[]string` and nothing enforces `len==1` — a single bool would either re-emit the successful prefix or suppress never-emitted policies on retry.
4. **Row-9 cleanup fix:** `cleanupOnError`'s closure captures `binaryHost` via a variable declared BEFORE the closure definition (not `:=` at the assignment site) so the closure observes the later assignment — the standard Go closure-over-variable idiom needed here because the three pre-manager error paths route through the SAME closure regardless of how far construction got.
5. **Relay repeated-Activate test fake:** `fakeRelayPool` hands out a REAL (but unreachable, `postgres://127.0.0.1:1/...`) `*pgxpool.Pool` rather than `nil` — `pgxpool.New` parses config without dialing, so `Acquire` (called by the relay's background goroutine) fails with a connection-refused error instead of a nil-pointer panic, letting the unit test observe Activate's idempotency guard without Docker.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `cmd/holomush/gateway_imports_test.go`'s `coreOnlyFiles` allowlist needed `core_subsystems_test.go`**
- **Found during:** Task 3, running `task test` after adding `TestStartAllActivatesNothingUntilEverySubsystemHasPrepared`
- **Issue:** The new property test constructs all 17 production subsystem types (the same pattern `core_topo_order_test.go` already uses, which IS on the allowlist) to read `DependsOn()` live; `TestGatewayImportsAreOnlyProtocolTranslation` failed because `core_subsystems_test.go` now imports the same domain packages but wasn't on the allowlist.
- **Fix:** Added `core_subsystems_test.go` to `coreOnlyFiles`, following the exact precedent and rationale already documented for `core_topo_order_test.go`.
- **Files modified:** `cmd/holomush/gateway_imports_test.go`
- **Verification:** `task test -- ./cmd/holomush/` green.
- **Committed in:** part of the single squashed commit.

**2. [Rule 1 - Bug] `assert.Same` on a channel value in the relay repeated-Activate test**
- **Found during:** Task 2, first `task test -- ./internal/world/setup/` run after writing the repeated-Activate test.
- **Issue:** `assert.Same(t, firstDone, s.done, ...)` panicked ("Both arguments must be pointers") — `s.done` is `chan struct{}`, not a pointer.
- **Fix:** Replaced with `assert.True(t, firstDone == s.done, ...)` (channels are directly comparable in Go).
- **Files modified:** `internal/world/setup/relay_subsystem_test.go`
- **Verification:** Test passes.
- **Committed in:** part of the single squashed commit.

**3. [Rule 1 - Bug] Relay repeated-Activate test's `fakeRelayPool` returning `nil` caused a background-goroutine panic**
- **Found during:** Task 2, same test run as #2 — after fixing the `assert.Same` issue, the test's background relay goroutine panicked with a nil-pointer SIGSEGV inside `pgxpool.(*Pool).Acquire` called on a nil `*pgxpool.Pool`.
- **Issue:** `Activate` launches a REAL background goroutine (`go s.relay.Run(runCtx)`) that immediately calls `AcquireLease`, which dereferences the pool; a nil pool panics rather than erroring.
- **Fix:** `fakeRelayPool` now constructs a real (but unreachable) `*pgxpool.Pool` via `pgxpool.New(ctx, "postgres://127.0.0.1:1/...")`, which parses config without dialing; `Acquire` then fails gracefully with a connection-refused error that the relay's `Run` loop logs and retries, rather than panicking.
- **Files modified:** `internal/world/setup/relay_subsystem_test.go`
- **Verification:** `task test -- ./internal/world/setup/` green, no panic.
- **Committed in:** part of the single squashed commit.

**4. [Rule 1 - Bug] Unnecessary `//nolint:staticcheck` directive flagged by `nolintlint`**
- **Found during:** `task lint:go` first run.
- **Issue:** The channel-identity `//nolint:staticcheck` comment added alongside fix #2 was flagged as unused — staticcheck doesn't fire on this comparison shape.
- **Fix:** Removed the directive.
- **Files modified:** `internal/world/setup/relay_subsystem_test.go`
- **Verification:** `task lint:go` clean (except the pre-existing, already-deferred gosec finding below).
- **Committed in:** part of the single squashed commit.

**5. [Rule 2 - Missing critical] Compile-time `lifecycle.Subsystem` assertions added for `world/setup.OutboxRelaySubsystem` and normalized for `plugin/setup.PluginSubsystem`**
- **Found during:** Task 3, verifying the plan's `rg -n 'var _ lifecycle\.Subsystem = ' internal/ cmd/holomush/ | wc -l` returns-17 acceptance criterion.
- **Issue:** `world/setup.OutboxRelaySubsystem` had no compile-time interface assertion at all (a genuine gap among the "7 missing" the plan named); `plugin/setup.PluginSubsystem`'s assertion existed but was written inside a `var (...)` block, so the plan's exact regex (`var _ lifecycle\.Subsystem = `) undercounted it.
- **Fix:** Added `var _ lifecycle.Subsystem = (*OutboxRelaySubsystem)(nil)` to `relay_subsystem_test.go`; split `plugin/setup/subsystem_test.go`'s grouped `var (...)` block into two standalone `var _ ... = ...` lines.
- **Files modified:** `internal/world/setup/relay_subsystem_test.go`, `internal/plugin/setup/subsystem_test.go`
- **Verification:** The grep now returns exactly 17.
- **Committed in:** part of the single squashed commit.

---

**Total deviations:** 5 auto-fixed (1 blocking-gate, 3 bugs, 1 missing-critical). All necessary for correctness/compile/lint; no scope creep.

## Issues Encountered

- **`go vet` was the compile oracle for the whole caller census.** Rather than trusting the plan's enumerated ~27-caller list as exhaustive up front, every migration pass was validated with `GOWORK=off go vet ./...` and `GOWORK=off go vet -tags=integration ./...` (both `task build`-equivalent for compile purposes but far faster to iterate). This caught every caller — including `gateway_imports_test.go`'s allowlist gap — without needing a second manual grep sweep.
- **The invalidation.Coordinator ownership question (Decision #2 above)** was the one place this plan's own settled text didn't survive contact with the live multi-consumer memoized-builder topology. Resolved by grounding the decision in D-13.0's actual guarantee (Prepare-sweep confinement, not "which named subsystem calls Start") rather than by attempting a deeper cryptoWiring refactor mid-plan.
- **`internal/testsupport/natstest/scoped.go`'s pre-existing gosec finding** (already logged in `deferred-items.md` during 07-10) is unrelated to this plan and untouched; `task lint:go`/`task lint` fail only on that one finding.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Phase 07's goal (event-model bootstrap decomposition, D-11/D-12/D-13/D-14) is now fully implemented: the orchestrator structurally enforces acquire-before-serve, closing the class of bug that let `grpcSubsystem.DependsOn()` once omit `AuditProjection`.
- Ready for phase-level regression gate and goal verification (`gsd-verifier`) — this was the final plan (Wave 9 of 9) in the phase.
- No blockers. The one pre-existing gosec finding in `internal/testsupport/natstest/scoped.go` remains tracked in `deferred-items.md` for whoever picks it up; it predates this plan and 07-10.

---
*Phase: 07-event-model-bootstrap-decomposition*
*Completed: 2026-07-18*

## Self-Check: PASSED

- `internal/lifecycle/subsystem.go` — FOUND
- `internal/eventbus/audit/subsystem_prepare_retry_integration_test.go` — FOUND
- Commit `ecbefb990` — FOUND in `git log --oneline --all`
