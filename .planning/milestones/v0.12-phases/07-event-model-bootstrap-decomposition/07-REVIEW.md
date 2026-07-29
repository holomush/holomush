---
phase: 07-event-model-bootstrap-decomposition
reviewed: 2026-07-18T20:01:46Z
depth: standard
files_reviewed: 133
files_reviewed_list:
  - .claude/rules/event-conventions.md
  - cmd/holomush/core.go
  - cmd/holomush/core_topo_order_test.go
  - cmd/holomush/cryptowiring.go
  - cmd/holomush/cryptowiring_test.go
  - cmd/holomush/deps.go
  - cmd/holomush/deps_test.go
  - cmd/holomush/gateway.go
  - cmd/holomush/gateway_closure_test.go
  - cmd/holomush/gateway_imports_test.go
  - cmd/holomush/plugin_replaytail_pagination_integration_test.go
  - cmd/holomush/sub_grpc.go
  - cmd/holomush/sub_grpc_adapters_test.go
  - cmd/holomush/sub_grpc_test.go
  - docs/architecture/invariants.md
  - docs/architecture/invariants.yaml
  - internal/access/setup/crypto_operator_validation.go
  - internal/access/setup/crypto_operator_validation_internal_test.go
  - internal/access/setup/subsystem.go
  - internal/admin/policy/subsystem.go
  - internal/admin/socket/subsystem.go
  - internal/auth/auth_service.go
  - internal/auth/auth_service_test.go
  - internal/auth/setup/subsystem.go
  - internal/bootstrap/setup/orphan_check_internal_test.go
  - internal/bootstrap/setup/subsystem.go
  - internal/cluster/heartbeat.go
  - internal/cluster/registry.go
  - internal/cluster/registry_internal_test.go
  - internal/cmdparse/cmdparse.go
  - internal/cmdparse/cmdparse_test.go
  - internal/command/dispatcher_test.go
  - internal/command/handlers/shutdown_test.go
  - internal/command/types.go
  - internal/command/types_test.go
  - internal/core/event.go
  - internal/core/ulid.go
  - internal/core/ulid_test.go
  - internal/eventbus/audit/chain/verifier_subsystem.go
  - internal/eventbus/audit/dlq.go
  - internal/eventbus/audit/subsystem.go
  - internal/eventbus/audit/subsystem_prepare_retry_integration_test.go
  - internal/eventbus/bus.go
  - internal/eventbus/config.go
  - internal/eventbus/crypto/dek/sweep.go
  - internal/eventbus/subsystem.go
  - internal/eventvocab/eventvocab.go
  - internal/eventvocab/eventvocab_test.go
  - internal/grpc/auth_handlers.go
  - internal/grpc/auth_handlers_test.go
  - internal/grpc/dispatcher_test.go
  - internal/grpc/location_follow.go
  - internal/grpc/pipeline_rendering_test.go
  - internal/grpc/server.go
  - internal/grpc/server_helpers_test.go
  - internal/grpc/test_helpers_test.go
  - internal/grpcclient/client.go
  - internal/grpcclient/client_test.go
  - internal/lifecycle/orchestrator.go
  - internal/lifecycle/orchestrator_test.go
  - internal/lifecycle/registry.go
  - internal/lifecycle/subsystem.go
  - internal/plugin/event_emitter.go
  - internal/plugin/goplugin/host_service_test.go
  - internal/plugin/host.go
  - internal/plugin/hostcap/hostv1_no_seq_test.go
  - internal/plugin/hostcap/servers.go
  - internal/plugin/hostcap/streamhistory_test.go
  - internal/plugin/hostcap/system_broadcaster.go
  - internal/plugin/hostcap/system_broadcaster_test.go
  - internal/plugin/hostfunc/stdlib_focus.go
  - internal/plugin/hostfunc/stdlib_focus_test.go
  - internal/plugin/hostfunc/streamauth.go
  - internal/plugin/hostfunc/streamauth_test.go
  - internal/plugin/setup/subsystem.go
  - internal/plugin/setup/system_broadcaster_test.go
  - internal/presence/emitter.go
  - internal/presence/emitter_test.go
  - internal/presence/session_ended.go
  - internal/presence/session_ended_test.go
  - internal/session/reaper.go
  - internal/session/setup/subsystem.go
  - internal/sessionlease/sessionlease.go
  - internal/sessionlease/sessionlease_test.go
  - internal/store/subsystem.go
  - internal/sysbroadcast/broadcaster.go
  - internal/sysbroadcast/broadcaster_test.go
  - internal/telnet/gateway_handler.go
  - internal/telnet/gateway_handler_test.go
  - internal/telnet/limits.go
  - internal/testsupport/integrationtest/harness.go
  - internal/testsupport/natstest/scoped.go
  - internal/tls/subsystem.go
  - internal/tls/subsystem_test.go
  - internal/ulidgen/ulidgen.go
  - internal/ulidgen/ulidgen_internal_test.go
  - internal/ulidgen/ulidgen_test.go
  - internal/web/handler.go
  - internal/web/handler_test.go
  - internal/web/scene_handlers_test.go
  - internal/web/translate_test.go
  - internal/world/setup/relay_subsystem.go
  - internal/world/setup/subsystem.go
  - test/integration/auth/auth_suite_test.go
  - test/integration/auth/multi_tab_test.go
  - test/integration/command/ratelimit_integration_test.go
  - test/integration/phase1_5_test.go
  - test/integration/pluginparity/session_admin_broadcast_test.go
findings:
  critical: 2
  warning: 1
  info: 0
  total: 3
status: issues_found
---

# Phase 07: Code Review Report (iteration 3 — final)

**Reviewed:** 2026-07-18T20:01:46Z
**Depth:** standard
**Files Reviewed:** 133
**Status:** issues_found

## Summary

This is the third and final iteration of the review/fix loop. I re-verified all four fixes
from the prior round by reading the current code directly (not trusting the fix report):

1. **`CheckpointSweepSubsystem` (`internal/eventbus/crypto/dek/sweep.go`)** — `Stop` correctly
   captures `done := s.done` into a local before nilling the field, and `loop` now takes `done
   chan struct{}` as an explicit parameter rather than reading `s.done` from inside the
   goroutine. This is the correct fix for the class of bug it addresses (a goroutine reading a
   mutable field that `Stop` concurrently nils). Verified sound.
2. **`ReadinessRegistry.Unregister` (`internal/lifecycle/registry.go`)** — correctly guarded
   with the registry's own mutex, a genuine no-op on a missing key, called unconditionally from
   both `ABACSubsystem.Stop` and `PluginSubsystem.Stop`. Verified sound; also traced the actual
   production interaction it enables (`grpcSubsystem.Stop` resetting `CoordHolder.coord`, see
   below) and confirmed it closes the intended double-Register panic path.
3. **`grpcSubsystem.Stop` resets `CoordHolder.coord`** (`cmd/holomush/sub_grpc.go`) — verified
   this is not just cosmetic: it is load-bearing for `stopCoordinatorOnBootFailure`
   (`cmd/holomush/cryptowiring.go`) to correctly detect "already stopped by the orchestrator's
   own rollback" and skip a second `Coordinator.Stop()` call on the normal (non-abandoned)
   rollback path. See the WR finding below for a residual race in the *abandoned*-Stop variant
   of this same interaction that neither fix addresses.
4. **`eventbus.IsNilPublisher`** (`internal/eventbus/bus.go`) — correctly extracted, used
   identically by `internal/presence/emitter.go` and `internal/sysbroadcast/broadcaster.go`.
   Verified sound.

Having verified the four prior fixes, I did a fresh adversarial sweep specifically for
**siblings of the exact bug class WR-01 fixed in `sweep.go`**: a background goroutine that
reads a subsystem's own mutable field directly (instead of an explicit parameter/local capture
taken at launch time), racing a `Stop()` that concurrently resets that same field to nil. I
found two more, unfixed instances of precisely this pattern in files that are in scope for this
phase, plus one narrower interaction-level race enabled by combining two of the fixes above with
the orchestrator's own documented "abandon a slow Stop and move on" behavior. All three are
reported below with reasoning for why they were not caught by the existing (green,
`-race`-enabled) test suite, and concrete fixes mirroring the pattern `sweep.go`'s fix already
established correctly. All other subsystems checked for this same pattern
(`internal/cluster/heartbeat.go`, `internal/eventbus/audit/subsystem.go`,
`internal/admin/socket/subsystem.go`, `internal/access/setup/subsystem.go`,
`internal/plugin/setup/subsystem.go`, `internal/session/reaper.go`) use either a mutex or a
proper explicit-parameter capture and are sound.

## Critical Issues

### CR-01: `Subsystem.Stop` (eventbus) races its own background `WaitForShutdown` goroutine on `s.server`

**File:** `internal/eventbus/subsystem.go:448-462`

**Issue:** `Stop` launches a background goroutine that reads the subsystem's own mutable
`s.server` field directly, then falls through to `s.server = nil` when its own `select`
resolves on `ctx.Done()` instead of `<-done`:

```go
if s.server != nil {
    s.server.Shutdown()
    done := make(chan struct{})
    go func() {
        s.server.WaitForShutdown()   // reads s.server directly — not captured
        close(done)
    }()
    select {
    case <-done:
    case <-ctx.Done():
        // Fall through — server teardown continues in the background.
    }
    s.server = nil                   // races the goroutine's read above
}
```

This is the exact class of bug WR-01 fixed in `internal/eventbus/crypto/dek/sweep.go`'s
`loop()` (that fix's own doc comment describes precisely this hazard: "a `defer close(s.done)`
could evaluate the field AFTER Stop's ... nils it, panicking ..."). Here the risk is worse: if
the `ctx.Done()` branch fires before the launched goroutine has actually started executing its
first statement (plausible when `ctx` has very little budget left — e.g. `StopAll`'s reverse-order
walk sharing one 5s budget across many subsystems, or `orch.rollback`'s fresh-but-still-bounded
5s context), the main `Stop()` goroutine sets `s.server = nil` while the background goroutine is
about to read that same field. `(*server.Server).WaitForShutdown()`
(`github.com/nats-io/nats-server/v2/server`) immediately dereferences `s.shutdownComplete`
(verified: `func (s *Server) WaitForShutdown() { <-s.shutdownComplete }`), so a nil receiver
there is an unrecovered nil-pointer panic in a background goroutine — which crashes the whole
process, not just the shutdown path. Independent of whether the nil-deref actually fires on any
given run, this is also a genuine unsynchronized data race on `s.server` per Go's memory model
(one goroutine writes, another reads, no lock, no channel handoff of the value) — it is only
absent from existing `-race` runs because current tests always give `Stop` a context with ample
budget, so `<-done` always wins the select before `s.server = nil` executes.

**Fix:** capture the server as a local before launching the goroutine, mirroring the `sweep.go`
fix (thread state in, don't read the mutable field from inside the goroutine):

```go
if s.server != nil {
    srv := s.server
    srv.Shutdown()
    done := make(chan struct{})
    go func(srv *server.Server, done chan struct{}) {
        srv.WaitForShutdown()
        close(done)
    }(srv, done)
    select {
    case <-done:
    case <-ctx.Done():
    }
    s.server = nil
}
```

### CR-02: `OutboxRelaySubsystem.Activate` races `Stop` on `s.done` (and `s.relay`) via `defer close(s.done)`

**File:** `internal/world/setup/relay_subsystem.go:119-134` (goroutine at 127-130), `Stop` at 143-165

**Issue:** `Activate` launches the drain goroutine with the exact anti-pattern the `sweep.go`
fix eliminated — reading the mutable `s.done` field (and `s.relay`) directly inside the
goroutine, rather than capturing them as parameters at launch:

```go
s.done = make(chan struct{})
go func() {
    defer close(s.done)              // reads s.done at defer-registration time (goroutine start)
    _ = s.relay.Run(runCtx)          // reads s.relay at call time
}()
```

`defer close(s.done)`'s argument (`s.done`) is evaluated when the defer statement executes —
i.e. essentially the goroutine's first statement — not when the deferred call actually runs.
`Stop` (same file, lines 152-158) does:

```go
if s.done != nil {
    select {
    case <-s.done:
    case <-time.After(relayStopTimeout):
    }
    s.done = nil
}
```

If `Stop` runs concurrently with a not-yet-scheduled instance of the `Activate` goroutine (the
race window is the interval between the `go func(){...}()` statement returning and the goroutine
actually being scheduled), `Stop` can observe `s.done` non-nil, wait out `relayStopTimeout`, and
set `s.done = nil` — all before the goroutine's `defer close(s.done)` line has executed. When
that goroutine is finally scheduled, it evaluates `s.done` as `nil` and defers `close(nil)`,
which panics with "close of nil channel" when `s.relay.Run(runCtx)` eventually returns. The same
`s.relay` field is subject to an analogous read-after-reset race against `Stop`'s
`s.relay = nil` (line 161). Not observed in the current test suite because
`TestOutboxRelaySubsystemRepeatedActivateLaunchesOnlyOneDrainGoroutine` only exercises repeated
`Activate`, never an `Activate` racing a concurrent `Stop`.

**Fix:** capture `done` (and, ideally, `relay`) as locals before the `go` statement and thread
them in as parameters, exactly like `sweep.go`'s `loop(ctx, done)`:

```go
done := make(chan struct{})
s.done = done
relay := s.relay
go func(done chan struct{}, relay *outbox.Relay) {
    defer close(done)
    _ = relay.Run(runCtx) //nolint:errcheck // Run returns only the ctx-cancellation reason on Stop
}(done, relay)
```

## Warnings

### WR-01: `CoordHolder.coord` unsynchronized across an abandoned `grpcSubsystem.Stop` and `stopCoordinatorOnBootFailure`

**File:** `cmd/holomush/sub_grpc.go:954-964`, `cmd/holomush/cryptowiring.go:483-492`,
`internal/lifecycle/orchestrator.go:176-207`

**Issue:** The prior-round fix (`grpcSubsystem.Stop` resetting `CoordHolder.coord = nil`)
correctly closes the double-`Coordinator.Stop()` gap for the *normal* (non-abandoned) rollback
path — verified by tracing `orch.StartAll` → `o.rollback` → `o.StopAll` → `grpcSubsystem.Stop`
(which now nils `coord`) → `core.go`'s own unconditional `stopCoordinatorOnBootFailure` call,
which now correctly no-ops because `holder.coord` is already nil.

However, `orchestrator.go`'s own documented behavior (`StopAll`, lines 176-207) is to run each
subsystem's `Stop` in its own goroutine and *abandon* it — without waiting further — if the
orchestrator's own `ctx` expires first, moving on / returning while that `Stop` call keeps
running in the background. `coordHolder` (`cmd/holomush/crypto_rekey_wiring.go:99-101`) is a bare
struct with no mutex around `coord`. If `grpcSubsystem.Stop`'s `GracefulStop()` (or the reaper
teardown ahead of it) is slow enough that `orch.rollback`'s 5s bounded context expires before
`grpcSubsystem.Stop` reaches its `CoordHolder.coord = nil` line, `StopAll` returns (abandoning
that in-flight `Stop` goroutine) while `orch.StartAll` itself returns the Prepare/Activate error
to `core.go`, which immediately calls `stopCoordinatorOnBootFailure(ctx, coordHolderPtr)` on the
*same* `coordHolder` from the *main* goroutine — reading and potentially writing `holder.coord`
concurrently with the still-running, abandoned `grpcSubsystem.Stop` goroutine's own read/write of
the same field. This is an unsynchronized concurrent read/write (a data race) and can also result
in `invalidation.Coordinator.Stop()` being invoked twice concurrently (its own `Stop`,
`internal/eventbus/crypto/invalidation/coordinator.go:83-92`, also mutates `c.sub` without a
lock, so a concurrent double-Stop there is itself unsynchronized). The comment in
`cryptowiring.go:479-482` claiming "there is no double-stop or race" is true for the
*non-abandoned* path but does not account for the orchestrator's own documented abandonment
behavior when a boot-failure rollback also runs slow.

This requires two coincident, low-probability conditions (a Prepare/Activate failure late in
topological order, and a slow-enough `grpcSubsystem.Stop` to blow the rollback's 5s budget), so
it is narrower than CR-01/CR-02, but it is a real, provable gap in the same "lifecycle guard
reset" family this iteration was scoped to re-check, so it is reported rather than silently
dropped.

**Fix:** guard `coordHolder.coord` with a small mutex (`sync.Mutex`) and have both
`grpcSubsystem.Stop`'s reset and `stopCoordinatorOnBootFailure`'s read+call go through a
lock-protected accessor method (e.g. `coordHolder.takeAndStop(ctx)` that atomically reads, nils,
and stops under the lock) so at most one caller ever observes a non-nil `coord` and invokes
`Stop()` on it.

---

_Reviewed: 2026-07-18T20:01:46Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
