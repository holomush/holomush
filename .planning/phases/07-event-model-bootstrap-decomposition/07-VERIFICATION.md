---
phase: 07-event-model-bootstrap-decomposition
verified: 2026-07-18T05:36:36Z
status: passed
score: 10/10 must-haves verified
behavior_unverified: 0
overrides_applied: 0
---

# Phase 7: Event-Model & Bootstrap Decomposition Verification Report

**Phase Goal:** Collapse the parallel Event models (coordinated with the ADR) and unify process bootstrap on `lifecycle.Orchestrator`.
**Verified:** 2026-07-18T05:36:36Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | `core.Event`/`core.NewEvent`/`core.EventAppender` deleted; `eventbus.Event` is the sole Event representation | ✓ VERIFIED | `rg '\bcore\.Event\b\|\bcore\.NewEvent\b\|\bcore\.EventAppender\b'` → 0 hits repo-wide. `internal/core/event.go` retains only `ActorKind`/`SystemActorULID`/`ActorSystemID` (no `Event` type). `CLAUDE.md:146` + `.claude/rules/event-conventions.md:49` explicitly document `eventbus.Event` as the single representation. |
| 2 | `WithEventStore` replaced by `WithEventPublisher`; CoreServer still publishes command_response/command_error | ✓ VERIFIED | `rg 'WithEventStore'` → 0 hits. `internal/grpc/server.go:259 func WithEventPublisher(pub eventbus.Publisher, gameID GameIDProvider)`; wired at `cmd/holomush/sub_grpc.go:609`. |
| 3 | Actor-bridge collapse: three hand-copied `core.Actor→eventbus.Actor` bridges (`coreToBusActor`, `harnessCoreToBusActor`, `busEventAppenderAdapter`) collapsed to the single pre-existing `plugins.coreActorToEventbusActor`; CoreServer's system actor is a direct typed literal, not a fourth bridge | ✓ VERIFIED | `rg -c 'func coreActorToEventbusActor' internal/plugin/event_emitter.go` → 1; zero occurrences of the three retired bridge names anywhere in the tree. `internal/presence/emitter.go`'s domain-scoped `mapActor` (07-05, a different, earlier, and explicitly-documented concern per its own doc comment) is not one of the three collapsed bridges and is out of this truth's scope. |
| 4 | Plugin-history pagination Seq bug fixed; both Lua and binary runtimes carry the real JetStream `Seq` (no hardcoded zero); `hostv1.Event` unchanged (8 fields, no Seq) | ✓ VERIFIED (behaviorally) | Ran the actual integration tests: `task test:int -- -run 'TestReplayTailPaginationAdvancesAcrossPagesOnQuietStream\|TestReplayTailPaginationAdvancesWhenULIDOrderDisagreesWithSeqOrder' ./cmd/holomush/...` → PASS (2/2, 5.6s). `task test -- -run TestHostV1EventFieldCensusExcludesSequence ./internal/plugin/hostcap/...` → PASS. Code: `internal/plugin/hostfunc/stdlib_focus.go:468,493` and `internal/plugin/hostcap/servers.go:901` both build `cursor.HostCursor{Seq: e.Seq, ...}` from the real event, not a literal 0. `pkg/plugin/event.go:73-85` `Event` struct has exactly 8 fields, no Seq. |
| 5 | Process bootstrap runs through `lifecycle.Orchestrator` — two-sweep Prepare/then-Activate over topo order is the sole ordering authority; zero subsystem `Start` calls exist outside it | ✓ VERIFIED | `internal/lifecycle/orchestrator.go:54-120` `StartAll` runs sweep 1 (all `Prepare`) then sweep 2 (all `Activate`) over `topoSort()` order. `rg 'func \(s \*\w+Subsystem\) Start\('` → 0 hits (old `Start` method fully removed). All 17 production subsystems (Database, TLS, ABAC, Auth, World, Sessions, Plugins, Bootstrap, CryptoChainVerifier, EventBus, Cluster, AuditProjection, CryptoPolicy, GRPC, AdminSocket, RekeyCheckpointSweep, OutboxRelay) implement `Prepare`+`Activate`, confirmed by grep across each subsystem file. `cmd/holomush/core.go:826-869` registers all 17 with `orch.Register` and calls `orch.StartAll(ctx)` exactly once; `orch.StopAll` is deferred inside a closure that builds its own 5s timeout at shutdown time (LOW-7 — not evaluated at boot). |
| 6 | Startup/shutdown behavior unchanged — a KEK-wired full boot (admin authenticate/rekey E2E) still passes end-to-end under the two-sweep orchestrator | ✓ VERIFIED (behaviorally) | Ran `task test:int -- -run TestAdminAuthenticateE2E ./cmd/holomush/...` → PASS (11.8s) — the full-stack Ginkgo E2E spec ("Admin Authenticate Lifecycle... covers happy-path, admin-reset, and lockout against one running server") boots the real process with a KEK configured, exercises the admin socket, and shuts it down cleanly. Also ran `task test:int -- -run 'TestBootstrapPassesWithCleanEventsAudit\|TestBootstrapFailsWithSyntheticOrphan\|TestBootstrapFailsWithOrphanInUnpartitioned\|TestBootstrapPassesWhenUnpartitionedAbsent' ./internal/bootstrap/setup/...` → PASS (4/4) confirming the bootstrap orphan boot gate still refuses on a real orphaned row after its move to `internal/bootstrap/setup`. |
| 7 | The bootstrap orphan boot gate moved (not duplicated) from `cmd/holomush` to `internal/bootstrap/setup`; zero references to `runBootstrapOrphanCheck` remain under `cmd/holomush` | ✓ VERIFIED | `rg 'runBootstrapOrphanCheck'` → all 6 hits are in `internal/bootstrap/setup/{subsystem.go,orphan_check_internal_test.go}`; zero hits under `cmd/holomush/`. |
| 8 | No package outside `cmd/holomush` names the `cryptoWiring` type | ✓ VERIFIED | `rg 'cryptoWiring' --type go -g '!cmd/holomush/**'` → exactly one hit, a comment reference in `internal/eventbus/subsystem.go:83`, not a type usage. |
| 9 | `internal/web` / `internal/telnet` import only protocol-translation dependencies — the gateway-boundary rule (INV-EVENTBUS-1) passes with zero violations | ✓ VERIFIED | `go list -deps ./internal/telnet/...` and `./internal/web/...` piped through a forbidden-package filter (`world\|access\|store\|plugin\|eventbus\|auth\|command\|core\|session\|grpc`) → 0 matches, both. Ran the actual gate tests: `task test -- -run 'TestGatewayImportsAreOnlyProtocolTranslation\|TestGatewayTransitiveClosureExcludesDomainPackages\|TestClosureOracleDetectsStoreInsideInternalGrpc\|TestClosureOracleWalksTransitivelyNotVacuously' ./cmd/holomush/...` → PASS (6/6). `internal/eventvocab` (introduced by 07-02 to unblock this) has zero internal (holomush) dependencies — confirmed via `go list -deps`. |
| 10 | INV-EVENTBUS-1 is bound to tests that genuinely assert it (both the direct-import test and the closure test) | ✓ VERIFIED | `docs/architecture/invariants.yaml:2342-2353` — `binding: bound`, `asserted_by: [cmd/holomush/gateway_imports_test.go, cmd/holomush/gateway_closure_test.go]`, summary enumerates the same 10 forbidden packages as `gatewayForbiddenPackages` in `gateway_imports_test.go:146-163`. Ran `task test -- -run 'TestEveryRegistryInvariantHasBinding\|TestProvenanceGuard\|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/...` → PASS (7 tests). |

**Score:** 10/10 truths verified (0 present-but-behavior-unverified)

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/grpcclient/client.go` | gRPC client leaf, 07-01 | ✓ VERIFIED | exists, populated |
| `internal/eventvocab/eventvocab.go` | event-type vocabulary leaf, 07-02 | ✓ VERIFIED | exists; `go list -deps` returns itself alone among holomush packages; wire strings match exactly (arrive, leave, system, move, command_response, command_error, location_state, exit_update, session_ended) |
| `internal/ulidgen/ulidgen.go`, `internal/cmdparse/cmdparse.go`, `internal/sessionlease/sessionlease.go` | gateway value leaves, 07-03 | ✓ VERIFIED | all exist with tests |
| `cmd/holomush/gateway_closure_test.go` | transitive-closure gate, 07-04 | ✓ VERIFIED | exists; passes; `gatewayForbiddenPackages` is the single shared list read by both gates |
| `internal/presence/emitter.go`, `internal/presence/session_ended.go` | presence extraction, 07-05 | ✓ VERIFIED | exist; `internal/auth` declares its own consumer-defined interface and does not import `internal/presence` |
| `internal/sysbroadcast/broadcaster.go` | broadcast builder, 07-06 | ✓ VERIFIED | exists; `internal/command` still does not reach `internal/eventbus` (confirmed no import) |
| `CLAUDE.md`, `.claude/rules/event-conventions.md` | rule amendments, 07-07 | ✓ VERIFIED | both updated; no reference to a deleted symbol |
| `cmd/holomush/plugin_replaytail_pagination_integration_test.go`, `internal/plugin/hostcap/hostv1_no_seq_test.go` | pagination TDD tests, 07-08 | ✓ VERIFIED | both exist; both pass when executed |
| `internal/tls/subsystem.go` | TLS subsystem, 07-09 | ✓ VERIFIED | exists (package `tlscerts`); registered as `SubsystemTLS` |
| `internal/lifecycle/subsystem.go`, `internal/lifecycle/orchestrator.go` | Prepare/Activate interface + two-sweep orchestrator, 07-10/07-11 | ✓ VERIFIED | interface has `Prepare`/`Activate`, no `Start`; orchestrator runs two full sweeps |
| `cmd/holomush/core_topo_order_test.go` | live topo-order proof, 07-10 | ✓ VERIFIED | exists; passes as part of the `TestCoreTopoOrder`/`TestProductionSubsystemsIncludes*` run |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|----|--------|---------|
| `internal/telnet/gateway_handler.go` | `internal/grpcclient` | error translation | ✓ WIRED | `grpcclient.TranslateSubscribeErr` referenced; no `internal/grpc` import in telnet |
| `internal/telnet` arrive/leave render switch | `internal/eventvocab` | wire-type constants | ✓ WIRED | `gateway_handler.go:1249,1251` reads `eventvocab.EventTypeArrive`/`EventTypeLeave` |
| `core.NewULID` | `internal/ulidgen.New` | forwarding | ✓ WIRED | single entropy source per 07-03 |
| `internal/auth` | `internal/presence` (consumer-defined interface) | no direct import | ✓ WIRED | build is the oracle; no cycle |
| `hostcap.NewSystemBroadcaster` | `internal/sysbroadcast.Broadcaster` | wraps, pins `core.SystemBroadcastSubject` | ✓ WIRED | confirmed in 07-06 summary + code |
| `CoreServer` | `eventbus.Publisher` via `WithEventPublisher` | replaces `WithEventStore` | ✓ WIRED | `cmd/holomush/sub_grpc.go:609` |
| `orch.StartAll` | all 17 subsystems' `Prepare` then `Activate` | topo-sorted two-sweep | ✓ WIRED | `cmd/holomush/core.go:826-869` |
| `grpcSubsystem`/`socket.AdminSocketSubsystem` | `SubsystemCryptoChainVerifier` | `DependsOn` | ✓ WIRED | `cmd/holomush/sub_grpc.go:213-226` includes `SubsystemCryptoChainVerifier`, `SubsystemAuditProjection`, `SubsystemTLS` |
| `internal/web`/`internal/telnet` | (protocol translation only) | gateway-boundary gate | ✓ WIRED | `go list -deps` clean; gate tests pass |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Gateway boundary gates (direct-import + transitive-closure) | `task test -- -run 'TestGatewayImportsAreOnlyProtocolTranslation\|TestGatewayTransitiveClosureExcludesDomainPackages\|TestClosureOracleDetectsStoreInsideInternalGrpc\|TestClosureOracleWalksTransitivelyNotVacuously' ./cmd/holomush/...` | 6 tests pass | ✓ PASS |
| `hostv1.Event` has no Seq field (8-field census) | `task test -- -run TestHostV1EventFieldCensusExcludesSequence ./internal/plugin/hostcap/...` | 1 test passes | ✓ PASS |
| Plugin history pagination bug fix (quiet stream + inverted lex order) | `task test:int -- -run 'TestReplayTailPaginationAdvancesAcrossPagesOnQuietStream\|TestReplayTailPaginationAdvancesWhenULIDOrderDisagreesWithSeqOrder' ./cmd/holomush/...` | 2 tests pass (5.6s, Docker) | ✓ PASS |
| Orchestrator / topo-order / production-subsystems-registered tests | `task test -- -run 'TestOrchestrator\|TestTopoOrder\|TestProductionSubsystemsIncludes\|TestCoreTopoOrder' ./internal/lifecycle/... ./cmd/holomush/...` | 16 tests pass | ✓ PASS |
| Bootstrap orphan gate still refuses on a real orphaned row, post-move | `task test:int -- -run 'TestBootstrapPassesWithCleanEventsAudit\|TestBootstrapFailsWithSyntheticOrphan\|TestBootstrapFailsWithOrphanInUnpartitioned\|TestBootstrapPassesWhenUnpartitionedAbsent' ./internal/bootstrap/setup/...` | 4 tests pass (2.0s, Docker) | ✓ PASS |
| Full KEK-wired boot E2E (admin authenticate/reset/lockout) under two-sweep orchestrator | `task test:int -- -run TestAdminAuthenticateE2E ./cmd/holomush/...` | 1 Ginkgo suite passes (11.8s, Docker) | ✓ PASS |
| Invariant registry: INV-EVENTBUS-1 binding is genuine, not fabricated | `task test -- -run 'TestEveryRegistryInvariantHasBinding\|TestProvenanceGuard\|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/...` | 7 tests pass | ✓ PASS |
| Full `go build` (Go binary + web) | `task build` | succeeds | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|------------|--------------|--------|----------|
| ARCH-03 | 07-09, 07-10, 07-11 | Process bootstrap migrated onto `lifecycle.Orchestrator`, unifying subsystem start/stop ordering | ✓ SATISFIED | Two-sweep Prepare/Activate orchestrator; 17/17 subsystems migrated; zero out-of-band `Start`; KEK-wired E2E boot passes |
| ARCH-04 | 07-02, 07-05, 07-06, 07-07, 07-08 | `core.Event`/`eventbus.Event` collapsed to a single representation | ✓ SATISFIED | `core.Event`/`NewEvent`/`EventAppender` deleted repo-wide; `eventbus.Event` sole representation; actor-bridge collapse verified; pagination fix passes |
| ARCH-05 | 07-01, 07-02, 07-03, 07-04 | Gateway-boundary import violations removed | ✓ SATISFIED | `go list -deps` clean for `internal/web`/`internal/telnet`; INV-EVENTBUS-1 bound and passing |

No orphaned requirements — `.planning/REQUIREMENTS.md` maps only ARCH-03/04/05 to Phase 7, and all three are marked Complete and covered above.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `cmd/holomush/gateway.go:258` | pre-existing `TODO(grpc-telnet)` | TODO | ℹ️ Info | Predates this phase (introduced in commit `2178e349b`); the 07-01 commit only moved surrounding code. Not a debt marker introduced by this phase; no TBD/FIXME/XXX found. |
| `internal/grpc/auth_handlers.go:242` | pre-existing `TODO` (connID threading) | TODO | ℹ️ Info | Pre-existing, unrelated to this phase's scope. |
| `internal/testsupport/natstest/scoped.go:29` | gosec G101 false positive on a test-only NATS credential | (resolved) | ℹ️ Info | Logged in `deferred-items.md` during 07-10 (out of that plan's scope), then fixed in the phase's final commit `90dfa5b00` (`//nolint:gosec` with justification, per repo convention) — no longer an open item. |

No TBD/FIXME/XXX markers found in any file touched by this phase's plans. No blocking anti-patterns.

### Human Verification Required

None. Every must-have truth was verified either by static analysis (grep/`go list -deps`) confirmed against actual code, or by executing the actual named test (unit or integration, including three Docker-backed integration runs: pagination fix, bootstrap orphan gate, and the full KEK-wired admin-authenticate E2E boot).

### Gaps Summary

No gaps. All three ROADMAP success criteria are independently confirmed against the running/compiled codebase, not just SUMMARY.md narrative:

1. **Single Event representation** — `core.Event`, `core.NewEvent`, `core.EventAppender` do not exist anywhere in the tree (verified by repo-wide grep returning zero hits); `eventbus.Event` is the only Event type, all callers migrated (CoreServer via `WithEventPublisher`, plugin history via `[]eventbus.Event`, the three duplicate actor bridges collapsed to one). The plugin-history pagination Seq bug (a correctness fix bundled into this consolidation) is behaviorally proven fixed by an executed integration test.
2. **Bootstrap unified on `lifecycle.Orchestrator`** — the interface itself now enforces a two-sweep Prepare-then-Activate barrier across all 17 production subsystems; zero subsystem `Start` calls exist outside `orch.StartAll`; a full KEK-wired admin-authenticate/rekey E2E boot was executed against the new orchestrator and passed.
3. **Gateway boundary clean** — `go list -deps` on `internal/web` and `internal/telnet` shows zero forbidden domain-package dependencies, and the INV-EVENTBUS-1 gate (both the direct-import test and the transitive-closure test) passes when executed.

---

_Verified: 2026-07-18T05:36:36Z_
_Verifier: Claude (gsd-verifier)_
