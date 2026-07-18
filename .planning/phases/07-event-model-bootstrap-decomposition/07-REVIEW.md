---
phase: 07-event-model-bootstrap-decomposition
reviewed: 2026-07-18T18:15:39Z
depth: standard
files_reviewed: 82
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
  critical: 0
  warning: 2
  info: 1
  total: 3
status: issues_found
---

# Phase 07: Code Review Report

**Reviewed:** 2026-07-18T18:15:39Z
**Depth:** standard
**Files Reviewed:** 82 (full required-reading set; several test files skimmed only far enough to confirm they exercise the production code correctly, per "do not report issues in test files unless they affect test reliability")
**Status:** issues_found

## Summary

This is a genuinely fresh re-review of a large (11-plan, 9-wave) refactor: the
`core.Event`/`eventbus.Event` collapse, the `Subsystem.Start` → `Prepare`/`Activate`
split across all 17 production subsystems, and the gateway-boundary import
closure. I independently re-verified all four items the prior review's fixes
addressed (WR-01 Lua-host-leak-on-world-conn-failure, WR-02 canonical
`eventbus.NewEvent()` construction in `PluginEventEmitter.Emit`, WR-03
reaper `bootAt` stamped in `Run` not `NewReaper`, and the CR-01
`stopCoordinatorOnBootFailure` boot-failure cleanup path) — all four are
present, correctly reasoned, and match their documented rationale; I did not
just trust the commit messages, I read each fix's current code and traced
its call sites. I also traced the orchestrator's topological ordering
against every subsystem's live `DependsOn()` and cross-checked it against
`core_topo_order_test.go`'s pinned assertions by hand (not just by reading
the assertion) — the graph is acyclic and the pinned order matches what the
code actually produces.

The most substantive finding from this pass is a genuine gap in the new
Prepare/Activate contract itself: several subsystems' "already
prepared"/"already activated" guards are never reset by `Stop`, silently
defeating the retry semantics the `lifecycle.Subsystem` interface documents
as a MUST. This does not affect the current production boot path (which
calls `StartAll` exactly once and exits on failure), but it is a real,
concrete contract violation with a straightforward fix, and it is
inconsistent with several subsystems in the very same phase
(`eventbus.Subsystem`, `internal/eventbus/audit.Subsystem`,
`cluster.registry`) that got this right. A second, minor finding is an
inconsistent nil-guard pattern across three near-identical
publisher-wrapping constructors introduced in this phase.

I did not find any security vulnerabilities, data-loss risks, or crashes in
the reviewed files. No BLOCKER-severity findings.

## Warnings

### WR-01: Several subsystems' Prepare/Activate idempotency guards are never reset by Stop, silently defeating the documented retry contract

**File:** `internal/lifecycle/subsystem.go:95-99,119-121` (the contract), with concrete violations at:
- `internal/store/subsystem.go:53-56` (Prepare guard) / `:91-96` (Stop)
- `internal/access/setup/subsystem.go:79-82` (Prepare guard) / `:155-163` (Stop)
- `cmd/holomush/cryptowiring.go:283-286` (Prepare guard), `:881-884` (Activate guard) / `:913-947` (Stop)
- `internal/admin/socket/subsystem.go:156-163` (Activate guard) / `:205-210` (Stop)
- `internal/world/setup/relay_subsystem.go:86-89` (Prepare guard), `:119-122` (Activate guard) / `:138-155` (Stop)
- `internal/eventbus/crypto/dek/sweep.go:121-124` (Activate guard) / `:136-149` (Stop)

**Issue:** `lifecycle.Subsystem`'s doc comment (`internal/lifecycle/subsystem.go:95-99` for `Prepare`, `:119-121` for `Activate`) states both methods "MUST be idempotent... because the orchestrator does not promise to call Prepare exactly once — a failed Activate elsewhere in the sweep rolls back via Stop, and a caller may legitimately retry Prepare after fixing a transient failure." Every subsystem listed above implements the "idempotent" half as an early-return guard keyed on a field set during the first successful `Prepare`/`Activate` (e.g. `store.DatabaseSubsystem.Prepare`: `if s.eventStore != nil { return nil }`). But none of these subsystems' `Stop` methods reset that guarding field — `DatabaseSubsystem.Stop` calls `s.eventStore.Close()` but never sets `s.eventStore = nil`; `ABACSubsystem.Stop` calls `s.stack.Close()` but never sets `s.stack = nil`; `grpcSubsystem.Stop` calls `GracefulStop()`/closes the listener but never nils `s.grpcServer`/`s.listener`; `AdminSocketSubsystem.Stop` never nils `s.errCh`; `OutboxRelaySubsystem.Stop` never nils `s.relay`/`s.done`; `CheckpointSweepSubsystem.Stop` never nils `s.done`.

Consequently, if `StartAll` is ever retried on the same `Orchestrator` instance after a rollback (exactly the scenario the interface doc describes as legitimate), every one of these subsystems' `Prepare`/`Activate` would see its guard field still non-nil (pointing at an already-`Close()`d/stopped resource) and return `nil` immediately — the retry would silently report success while serving a closed DB pool, a torn-down ABAC stack, an unbound gRPC listener, a dead admin socket, or a stopped outbox relay, with no error surfaced anywhere.

Contrast with the subsystems in this same phase that get this right:
`internal/eventbus/subsystem.go:463-464` (`Stop` sets `s.conn = nil; s.js = nil`, matching the `Prepare` guard at `:118`), `internal/eventbus/audit/subsystem.go:450-479` (`Stop` explicitly clears `preparedProjection`/`partitionManager`/`worker` after drain, with an inline comment explaining exactly why), and `internal/cluster/heartbeat.go:154-175` (`Stop` explicitly nils `subAlive`/`subBye`/`subProbe`/`subPoison`/tickers/done-channels before returning).

This has no impact on the current production boot path (`cmd/holomush/core.go` calls `orch.StartAll(ctx)` exactly once and exits on failure — no in-tree retry path, as `internal/lifecycle/orchestrator.go:157-166` itself documents), so it is not a currently-triggerable production bug. It is, however, a real and concrete violation of an explicit MUST written in the very interface these subsystems implement, and a latent trap for any future retry path, test harness, or long-running admin tool that reuses an `Orchestrator`.

**Fix:** Either (a) reset the guarding field to its zero value in each affected `Stop`, mirroring the eventbus/audit/cluster pattern (e.g. `store.DatabaseSubsystem.Stop`: `s.eventStore = nil; s.pool = nil` after `Close()`; `grpcSubsystem.Stop`: `s.grpcServer = nil` after `GracefulStop()`/`Stop()`, `s.listener = nil` after `Close()`), or (b) narrow `internal/lifecycle/subsystem.go`'s doc comment to scope "MUST be idempotent" explicitly to "idempotent within a single `StartAll` sweep" (which is all the orchestrator's own `topoSort`-driven single-pass-per-subsystem execution actually requires) and drop the "a caller may legitimately retry Prepare" claim, so the documented contract matches what the code actually guarantees.

```go
// internal/store/subsystem.go — example of (a)
func (s *DatabaseSubsystem) Stop(_ context.Context) error {
	if s.eventStore != nil {
		s.eventStore.Close()
		s.eventStore = nil
		s.pool = nil
	}
	return nil
}
```

### WR-02: `sysbroadcast.NewBroadcaster` does not detect typed-nil `Publisher` values, unlike its two sibling constructors introduced in the same phase

**File:** `internal/sysbroadcast/broadcaster.go:38-46`

**Issue:** `NewBroadcaster` guards against a nil `Publisher` with a bare `pub == nil` check:

```go
func NewBroadcaster(pub eventbus.Publisher, gameID func() string) *Broadcaster {
	if pub == nil {
		panic("sysbroadcast.NewBroadcaster: nil Publisher")
	}
	...
}
```

This phase introduces two sibling constructors over the same `eventbus.Publisher`-shaped nil-checking problem — `presence.NewEmitter` (`internal/presence/emitter.go:54-62`) and `cluster.NewSubsystem`'s `isNilConn` helper (`internal/cluster/registry.go:303-320`) — both of which explicitly detect *typed*-nil interface values via `reflect.ValueOf(x).IsNil()` on the nilable-kind cases, with a doc comment explaining why the bare `== nil` check is insufficient: a nil concrete pointer wrapped in an interface is not itself `== nil` at the interface level, so `presence.NewEmitter`'s own doc calls this out explicitly ("Detects both untyped nil and typed-nil interface values... so callers truly fail fast at construction"). `sysbroadcast.NewBroadcaster` reintroduces exactly the gap `presence.NewEmitter` was written to close: a typed-nil concrete `Publisher` implementation passed to `NewBroadcaster` would pass the `pub == nil` check, defer the failure past construction, and panic (or nil-pointer-dereference) inside `Broadcast` instead — the same failure-mode the sibling constructor's doc comment says construction-time detection exists specifically to avoid.

Current callers (`cmd/holomush/sub_grpc.go`'s `sysbroadcast.NewBroadcaster(publisher, ...)`, `hostcap.NewSystemBroadcaster`) always pass a concrete non-nil `*RenderingPublisher`, so this is not exploitable today — but it is a real inconsistency between three constructors doing the same defensive job in the same PR, one of which explicitly documents the pattern the other two omit.

**Fix:** Reuse (or extract to a shared helper in `internal/eventbus`) the same `reflect`-based typed-nil detection `presence.isNilPublisher` and `cluster.isNilConn` already implement:

```go
func NewBroadcaster(pub eventbus.Publisher, gameID func() string) *Broadcaster {
	if pub == nil || isNilPublisher(pub) { // reuse presence's isNilPublisher (extract to a shared helper)
		panic("sysbroadcast.NewBroadcaster: nil Publisher")
	}
	...
}
```

## Info

### IN-01: `PluginSubsystem.Prepare` has no "already prepared" idempotency guard at all, unlike every sibling subsystem

**File:** `internal/plugin/setup/subsystem.go:173-179`

**Issue:** Every other subsystem reviewed in this phase implements one of two documented dispositions for repeat `Prepare` calls: an explicit early-return guard (`store.DatabaseSubsystem`, `ABACSubsystem`, `grpcSubsystem`, `eventbus.Subsystem`, `OutboxRelaySubsystem`), or an explicit "no guard needed — re-running is benign reassignment" comment (`WorldSubsystem`, `SessionSubsystem`, `TLSSubsystem`, `BootstrapSubsystem`). `PluginSubsystem.Prepare` has neither: it unconditionally resolves the plugins dir, builds a fresh `hostfunc` bridge, a fresh Lua host, a fresh service registry, and (on non-empty `DatabaseConnStr`) a fresh alias pool + schema provisioner + binary host + `Manager` + `LoadAll` — every time it is called, with no comment explaining whether re-running is intended to be safe/cheap. Given `goplugin.NewHost` launches subprocess-management goroutines and `Manager.LoadAll` re-runs bootstrap-alias-seeding side effects, a second `Prepare` call (the same cross-`StartAll`-retry scenario WR-01 discusses) would silently duplicate plugin-subprocess launch and re-run `LoadAll`'s side effects rather than short-circuiting or explicitly documenting why full re-construction is safe.

This is Info-severity because, unlike WR-01's subsystems, `PluginSubsystem` at least does real work on a retry rather than silently reporting stale success — the failure mode here is "wasteful/duplicated work", not "silent use of a closed resource".

**Fix:** Either add an explicit "already prepared" guard (`if s.manager != nil { return nil }`) mirroring the sibling subsystems that use one, or add a one-line doc comment explicitly stating why unconditional re-construction on retry is considered safe (matching the style of `WorldSubsystem`/`SessionSubsystem`/`BootstrapSubsystem`'s "no idempotency guard is needed" comments), so a future reader doesn't have to independently re-derive this.

---

_Reviewed: 2026-07-18T18:15:39Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
