---
phase: 07-event-model-bootstrap-decomposition
reviewed: 2026-07-18T00:00:00Z
depth: standard
files_reviewed: 45
files_reviewed_list:
  - .claude/rules/event-conventions.md
  - cmd/holomush/core.go
  - cmd/holomush/core_topo_order_test.go
  - cmd/holomush/cryptowiring.go
  - cmd/holomush/deps.go
  - cmd/holomush/gateway.go
  - cmd/holomush/gateway_closure_test.go
  - cmd/holomush/gateway_imports_test.go
  - cmd/holomush/sub_grpc.go
  - internal/access/setup/crypto_operator_validation.go
  - internal/access/setup/subsystem.go
  - internal/admin/policy/subsystem.go
  - internal/admin/socket/subsystem.go
  - internal/auth/setup/subsystem.go
  - internal/bootstrap/setup/subsystem.go
  - internal/cluster/heartbeat.go
  - internal/cluster/registry.go
  - internal/cmdparse/cmdparse.go
  - internal/cmdparse/cmdparse_test.go
  - internal/command/types.go (diff)
  - internal/core/event.go
  - internal/core/ulid.go
  - internal/eventbus/audit/chain/verifier_subsystem.go
  - internal/eventbus/audit/dlq.go
  - internal/eventbus/audit/subsystem.go
  - internal/eventbus/config.go
  - internal/eventbus/crypto/dek/sweep.go
  - internal/eventbus/subsystem.go
  - internal/eventvocab/eventvocab.go
  - internal/grpc/location_follow.go (diff)
  - internal/grpc/server.go (diff, stat only)
  - internal/idgen/id.go
  - internal/lifecycle/orchestrator.go
  - internal/lifecycle/subsystem.go
  - internal/plugin/event_emitter.go
  - internal/plugin/host.go (diff)
  - internal/plugin/hostcap/servers.go (diff)
  - internal/plugin/hostcap/system_broadcaster.go
  - internal/plugin/hostfunc/stdlib_focus.go (diff)
  - internal/plugin/setup/subsystem.go
  - internal/plugin/setup/world_conn.go
  - internal/presence/emitter.go
  - internal/presence/session_ended.go
  - internal/session/reaper.go
  - internal/session/setup/subsystem.go
  - internal/sessionlease/sessionlease.go
  - internal/store/subsystem.go
  - internal/sysbroadcast/broadcaster.go
  - internal/telnet/gateway_handler.go (diff)
  - internal/telnet/limits.go
  - internal/tls/subsystem.go
  - internal/ulidgen/ulidgen.go
  - internal/web/handler.go (diff, stat only)
  - internal/world/setup/relay_subsystem.go
  - internal/world/setup/subsystem.go
findings:
  critical: 1
  warning: 3
  info: 0
  total: 4
status: issues_found
---

# Phase 7: Code Review Report

**Reviewed:** 2026-07-18T00:00:00Z
**Depth:** standard
**Files Reviewed:** 45 read/diffed of the ~85 named in required_reading (see note below)
**Status:** issues_found

## Summary

This is a very large (198-file), carefully-documented refactor. Every Prepare/Activate
split I inspected (Database, TLS, ABAC, Auth, World, Sessions, Plugins, Bootstrap,
EventBus, OutboxRelay, AuditProjection, CryptoChainVerifier, CryptoPolicy,
CheckpointSweep, AdminSocket, cluster.Registry, grpcSubsystem) individually honors the
documented D-13.3 boundary (acquire-only in Prepare, domain-traffic/external-bind only
in Activate), and the orchestrator's rollback/StopAll mechanics (fresh rollback context,
buffered per-Stop channel, preparedOrder-is-superset-of-activatedOrder) are sound and
well-reasoned in isolation. The gateway-boundary tests (`gateway_imports_test.go`,
`gateway_closure_test.go`) are a genuinely strong enforcement mechanism with a real
positive control, not a tautology.

However, adversarial review found one cross-subsystem correctness gap in the rollback
path that the per-subsystem-in-isolation review missed: the `invalidation.Coordinator`
started as a side effect of the shared `cryptoWiring` builder is only ever stopped by
`grpcSubsystem.Stop`, but the builder's *first caller* in the real topological order is
`CryptoChainVerifier` (earlier than `grpcSubsystem`) — so a Prepare failure in
`CryptoChainVerifier` itself (or any other wiring consumer between it and gRPC) after
the Coordinator has started leaves it running forever, because `grpcSubsystem`'s own
`Prepare`/`Stop` are never reached. Three further, lower-severity findings round out the
report (a leaked Lua host on one Prepare error path, a raw `eventbus.Event{}` literal
that bypasses the project's own single-construction-path invariant, and a boot-grace
window whose start point silently moved earlier under the two-sweep split).

**Coverage note:** given the scale of this phase (85 named files, several 1000+-line
diffs), some files were reviewed via targeted `git diff` against the phase base rather
than a full re-read (marked `(diff)` above); a few very large gateway-side files
(`internal/grpc/server.go`, `internal/web/handler.go`) were only diff-stat'd given no
findings surfaced in the directly-related files they call into. No file in the required
list was skipped outright, but review depth is not uniform across all ~85 files.

## Critical Issues

### CR-01: `invalidation.Coordinator` leaks on any Prepare failure between `CryptoChainVerifier` and `grpcSubsystem`

**File:** `cmd/holomush/cryptowiring.go:335-380`, `cmd/holomush/sub_grpc.go:936-946`, `internal/eventbus/audit/chain/verifier_subsystem.go:102-131`

**Issue:** The `invalidation.Coordinator` (a live component with its own NATS
subscriptions for cross-replica DEK-cache invalidation) is constructed **and started**
inside `buildCryptoWiring` (`cryptowiring.go:349-380`) as a side effect of the *first*
subsystem in topological order that resolves the shared, memoized `cryptoWiringFn`.
`grpcSubsystem` is the only subsystem whose `Stop` knows how to tear the Coordinator
down (`sub_grpc.go:941-945`, gated on `s.cfg.CoordHolder.coord != nil`) — this replaced
a former `defer` in `runCore` that unconditionally ran regardless of where boot failed.

But `grpcSubsystem` is *not* the first caller of `cryptoWiringFn` in the real
production graph: `chain.VerifierSubsystem` (`SubsystemCryptoChainVerifier`) also holds
a provider (`RepoProvider`) backed by the same builder, and the pinned real topological
order (`cmd/holomush/core_topo_order_test.go`'s
`TestProductionSubsystemsTopologicalStartOrderIsPinned`) places
`SubsystemCryptoChainVerifier` at index 8, five subsystems *before*
`SubsystemGRPC` at index 14. `chain.VerifierSubsystem.Prepare` calls
`s.cfg.RepoProvider()` **first** (line 104-110, triggering the full wiring build,
including the Coordinator's construction+`Start()`), and only *afterward* runs
`ValidateRegistration` and `s.verifier.VerifyAll` for every registered chain handler
(lines 118-129) — either of which can fail (a broken hash chain is exactly
`INV-CRYPTO-102`'s designed failure mode).

If `VerifyAll` (or `ValidateRegistration`) fails, `VerifierSubsystem.Prepare` returns a
non-nil error. `Orchestrator.StartAll` then calls `rollback` → `StopAll`, which walks
`preparedOrder` in reverse and calls `Stop` on every subsystem whose `Prepare` was
*invoked* — but `grpcSubsystem.Prepare` was **never invoked** (topological order never
reached it), so `grpcSubsystem` is not in `preparedOrder` and its `Stop` — the only code
path that reads `CoordHolder.coord` — never runs. The already-started
`invalidation.Coordinator` (goroutines + NATS subscriptions) is never stopped.

The same gap applies to any Prepare failure in `AdminSocketSubsystem`,
`OutboxRelaySubsystem`, `PluginSubsystem`, `BootstrapSubsystem`, or
`AuditProjection.Subsystem` (all of which sit between index 8 and index 14 in the
pinned order) *after* the wiring has already been resolved by `CryptoChainVerifier`.

This is precisely the failure mode `cryptowiring.go`'s own doc comment
(`cryptoWiringInputs.CoordHolder`, lines 100-118) half-acknowledges — it discusses why
starting the Coordinator inside Prepare (rather than Activate) is safe for the
D-13.0 barrier, but does not address that the *cleanup* owner (`grpcSubsystem`) can be
topologically unreachable when the *builder* (triggered by an earlier consumer) fails
downstream of Coordinator construction.

**Impact:** In production a failed `StartAll` aborts the process shortly after, so the
leaked goroutines/subscriptions are bounded by process exit — but this is exactly the
kind of gap that bites in integration tests (repeated `StartAll`/rollback cycles in one
test binary, e.g. a chain-integrity regression test) and violates the subsystem's own
stated contract that `grpcSubsystem` "owns the Coordinator's lifecycle" when
`grpcSubsystem` may never even be constructed-into-Prepare in the failing run.

**Fix:** Give the Coordinator's lifecycle to something that is guaranteed to be in
`preparedOrder` whenever it was started — e.g. stash `CoordHolder` cleanup inside
`buildCryptoWiring`'s own error/rollback path is not enough (a *later* failure, not the
builder's own, is the trigger); instead, either (a) have *every* wiring-consumer
subsystem's `Stop` check-and-stop `CoordHolder.coord` (idempotent `Stop` already exists
on `invalidation.Coordinator`, so this is safe to duplicate), or (b) register the
Coordinator's stop as an orchestrator-independent deferred cleanup that runs whenever
`resolveCryptoWiring`'s `once.Do` actually fired, not gated on any one subsystem's own
successful Prepare.

## Warnings

### WR-01: `PluginSubsystem.Prepare`'s world-conn error path bypasses `cleanupOnError`, leaking the Lua host

**File:** `internal/plugin/setup/subsystem.go:219-247`

**Issue:** `s.luaHost = luaHost` is assigned at line 238 (Lua host fully constructed).
Immediately after, `newWorldInProcessConn` is called (line 244); on error, the function
returns directly (`return oops.Code("WORLD_INPROCESS_CONN_FAILED")...`, line 246) —
**before** `cleanupOnError` is even defined in the function body (its definition starts
at line 272). Every *other* pre-manager error path in this function correctly calls
`cleanupOnError()` (which closes `binaryHost`, `s.luaHost`, and `s.worldConn`), and the
subsystem's own doc comment for `Stop` explicitly relies on this: "every pre-manager
error path inside Prepare already routes through cleanupOnError, making Stop's
`s.manager == nil` early return a TRUE no-op." That claim is false for this one path:
`Stop()` sees `s.manager == nil` and no-ops, so the already-constructed `s.luaHost`
(with its state-factory pool) is never closed.

**Fix:** Move the `newWorldInProcessConn` call (and the `s.registry` construction it
depends on) above the Lua-host construction, or hoist `cleanupOnError`'s definition
above this call site and invoke it on this error path too:

```go
worldConn, worldConnErr := newWorldInProcessConn(s.cfg.World.Service())
if worldConnErr != nil {
    if s.luaHost != nil {
        _ = s.luaHost.Close(ctx) //nolint:errcheck // best-effort cleanup
        s.luaHost = nil
    }
    return oops.Code("WORLD_INPROCESS_CONN_FAILED").Wrap(worldConnErr)
}
```

### WR-02: `PluginEventEmitter.Emit` constructs a raw `eventbus.Event{}` literal instead of `eventbus.NewEvent()`

**File:** `internal/plugin/event_emitter.go:191-199`

**Issue:** `eventbus/types.go`'s `NewEvent` doc comment states it is "post-ARCH-04, the
ONLY construction path for eventbus.Event values that will be published," and
`internal/idgen/id.go` independently documents: "eventbus.Event{} struct literals must
use eventbus.NewEvent() ... never construct an Event literal with a manually-supplied
ID." `PluginEventEmitter.Emit` — the single publish path for every plugin emit,
arguably the highest-traffic event construction site in the whole system — does exactly
that:

```go
event := eventbus.Event{
    ID:        core.NewULID(),
    Subject:   sub,
    Type:      typ,
    Timestamp: time.Now().UTC(),
    Actor:     busActor,
    Payload:   payload,
    Sensitive: sensitive,
}
```

It happens to use the correct generator (`core.NewULID()`, not `idgen.New()`), so
today's dedup/ordering behavior is not broken. But it has already drifted from
`NewEvent` in one detail — `time.Now().UTC()` vs. `NewEvent`'s bare `time.Now()` — and,
more importantly, bypasses the single-construction-path invariant this refactor's own
conventions establish: any future change to `NewEvent`'s stamping (added validation,
a new defaulted field) will silently not apply here.

**Fix:** Build the base event via `eventbus.NewEvent(sub, typ, busActor, payload)` and
set `Sensitive` afterward, matching the pattern `NewEvent`'s own doc comment
prescribes ("Callers that need to override specific fields after construction ...
MUST still use NewEvent for the base value").

### WR-03: Session reaper's `bootAt` is captured at construction (Prepare), not at `Run()` (Activate)

**File:** `cmd/holomush/sub_grpc.go:805-813`, `internal/session/reaper.go:52-63`

**Issue:** `session.NewReaper(...)` — which stamps `r.bootAt = config.Now()` inside its
constructor (`reaper.go:61`) — is called from `grpcSubsystem.Prepare` (`sub_grpc.go:810`,
step 10, "launch deferred to Activate — row 16"). The reaper's `Run()` loop, which is
the thing that actually *consults* `bootAt` to suppress the lease sweep for
`BootGrace` (`reaper.go:76`), is not launched until `grpcSubsystem.Activate`
(`sub_grpc.go:886`). Under the two-sweep orchestrator, `grpcSubsystem.Activate` runs
only after every earlier-topo-order subsystem's own `Activate` has completed (13
subsystems, per the pinned order in `core_topo_order_test.go`) — so the wall-clock
instant `bootAt` records is measurably earlier than the instant the reaper loop
actually starts checking it. This silently shrinks the effective `BootGrace` window
below the configured value (default 60s, floor-enforced at 2× the 15s gateway refresh
cadence in `parseSessionConfig`) by whatever time the tail of Prepare + the rest of
Activate takes.

In the pre-refactor single-phase `Start`, `NewReaper` and `go reaper.Run(...)` ran back
to back with no intervening barrier, so `bootAt` and the loop's actual start were
effectively simultaneous. This is a genuine (if currently small — likely sub-second in
practice) semantic drift introduced by the Prepare/Activate split, and the drift grows
with every future subsystem inserted between `SubsystemCryptoChainVerifier` and
`SubsystemGRPC` in topological order.

**Fix:** Capture `bootAt` in `Run()` (or add a `Reaper.MarkBoot()` called from
`Activate` immediately before `go s.sessionReaper.Run(...)`) rather than in the
constructor, so the grace window is measured from when the reaper actually starts
protecting sessions, not from when it was merely constructed.

---

_Reviewed: 2026-07-18T00:00:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
