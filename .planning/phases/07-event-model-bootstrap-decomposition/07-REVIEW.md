---
phase: 07-event-model-bootstrap-decomposition
reviewed: 2026-07-18T18:51:00Z
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
  warning: 3
  info: 1
  total: 4
status: issues_found
---

# Phase 07: Code Review Report

**Reviewed:** 2026-07-18T18:51:00Z
**Depth:** standard
**Files Reviewed:** 82
**Status:** issues_found

## Summary

This is iteration 2 of the review/fix loop. A fixer landed three commits
addressing the prior round's WR-01 (Prepare/Activate idempotency guards
never reset by `Stop` across `DatabaseSubsystem`, `ABACSubsystem`,
`grpcSubsystem`, `AdminSocketSubsystem`, `OutboxRelaySubsystem`,
`CheckpointSweepSubsystem`), WR-02 (`sysbroadcast.NewBroadcaster`'s missing
typed-nil `Publisher` detection), and IN-01 (`PluginSubsystem.Prepare`'s
missing idempotency guard). I re-read all three fixes' current code
(not the commit messages) and traced every guarded field to its
construction site and its `Stop`-side reset:

- **WR-02 (typed-nil Publisher)**: verified correct and byte-for-byte
  consistent with `presence.isNilPublisher` and `cluster.isNilConn` — same
  `reflect.Kind` switch, same panic discipline, new test
  (`TestNewBroadcasterPanicsOnTypedNilPublisher`) exercises it. No issues.
- **WR-01 (guard resets)**: `DatabaseSubsystem`, `ABACSubsystem`,
  `AdminSocketSubsystem`, and `OutboxRelaySubsystem`'s `Stop` methods
  correctly and unconditionally reset every guarding field. `grpcSubsystem`
  correctly resets `grpcServer`/`listener`/`reaperCancel`/`reaperCtx` — but
  see WR-03 below for a field it does NOT reset in the same function.
  `CheckpointSweepSubsystem`'s fix is **incomplete** — see WR-01 below: one
  of its two `Stop` return branches skips the reset the other branch
  performs, an inconsistency the sibling fixes in the same commit do not
  have.
- **IN-01 (Plugin guard)**: the guard field (`s.manager`) is set
  mid-`Prepare` and the orchestrator's own `preparedOrder`-before-call
  design (`internal/lifecycle/orchestrator.go:71-75`) guarantees `Stop`
  always runs on a subsystem whose `Prepare` was invoked, even on partial
  failure, so the "guard set before Prepare fully completes" shape is not
  by itself exploitable. However, combining the now-functional retry path
  this fix (and WR-01) enables with a **pre-existing** call these two
  subsystems make in `Prepare` — `ReadinessRegistry.Register`, which
  panics on a duplicate `SubsystemID` and has no corresponding
  `Unregister` — produces a genuine new failure mode on a legitimate
  retry. See WR-02 below.

Net: the three targeted fixes are directionally correct and none of them
regress anything exercised by the current single-boot production path
(`cmd/holomush/core.go` calls `StartAll` exactly once). But two of the
three fixes are **incomplete** in ways that matter for the exact retry
contract they were written to satisfy, and I traced a genuine, previously
undetected interaction between the fixes and pre-existing code
(`ReadinessRegistry.Register`'s panic-on-duplicate) that a legitimate
retry would now hit. All three new findings share the same
not-currently-triggerable caveat as the original WR-01 (no in-tree retry
path exists today), so I am keeping them at Warning severity for
consistency with how the prior round scored this exact class of issue —
but they should be fixed before any retry-capable caller (an admin tool,
a test harness reusing an `Orchestrator`) is introduced.

## Warnings

### WR-01: `CheckpointSweepSubsystem.Stop`'s ctx-timeout branch does not reset the guard it just failed to wait for, inconsistent with its own happy-path branch and every other Stop in this codebase

**File:** `internal/eventbus/crypto/dek/sweep.go:139-154`

**Issue:** The WR-01 fix added `s.done = nil` to the happy-path branch of `Stop`, but the sibling `ctx.Done()` (timeout) branch was left untouched:

```go
func (s *CheckpointSweepSubsystem) Stop(ctx context.Context) error {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.done == nil {
		return nil
	}
	select {
	case <-s.done:
		s.done = nil
		return nil
	case <-ctx.Done():
		return oops.Code("DEK_REKEY_SWEEP_STOP_TIMEOUT").Wrap(ctx.Err())
	}
}
```

If `Stop` is called with a context whose deadline elapses before the
background loop drains (exactly the scenario `oops.Code("DEK_REKEY_SWEEP_STOP_TIMEOUT")`
exists to report), `s.done` is left non-nil. `Activate`'s idempotency guard
is `if s.done != nil { return nil }` (line 122) — so a subsequent legitimate
retry of `Activate` (the same scenario this whole fix round exists to
support) would silently no-op, believing the checkpoint sweep is already
running, when in fact the prior loop may have already fully exited (its
`defer close(s.done)` still fires on the channel object captured at
`Activate`, line 158) or may still be winding down. Either way, no new tick
loop launches and the guard gives no signal that anything is wrong.

This is a real regression against the very contract this iteration's fix
commit exists to establish, and it contradicts the pattern used by every
other subsystem fixed in the same commit (`DatabaseSubsystem`,
`ABACSubsystem`, `AdminSocketSubsystem`, `OutboxRelaySubsystem` all reset
their guard fields unconditionally, regardless of the return path) — and
by the two subsystems this whole approach was modeled on:
`internal/eventbus/subsystem.go:448-464` sets `s.server = nil` /
`s.conn = nil` / `s.js = nil` unconditionally, AFTER the `select` block,
specifically so the `ctx.Done()` fallthrough branch ("Fall through — server
teardown continues in the background") still resets state; and
`internal/eventbus/audit/subsystem.go:472-479` sets `s.worker = nil` /
`s.preparedProjection = nil` / `s.partitionManager = nil` unconditionally
before checking whether `drain(ctx)` returned an error.

**Fix:** Move the reset out of the `case <-s.done:` branch so it runs on both paths, mirroring the two cited reference implementations:

```go
func (s *CheckpointSweepSubsystem) Stop(ctx context.Context) error {
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.done == nil {
		return nil
	}
	done := s.done
	s.done = nil
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return oops.Code("DEK_REKEY_SWEEP_STOP_TIMEOUT").Wrap(ctx.Err())
	}
}
```

### WR-02: WR-01/IN-01's now-functional Prepare retry reaches `ReadinessRegistry.Register` a second time for `ABACSubsystem` and `PluginSubsystem`, which panics (no `Unregister` exists)

**Files:**
- `internal/access/setup/subsystem.go:79-82` (guard), `:125-127` (Register call), `:159-166` (Stop, now resets the guard)
- `internal/plugin/setup/subsystem.go:178-180` (guard, added by IN-01), `:473-475` (Register call), `:539-570` (Stop, now resets the guard)
- `internal/lifecycle/registry.go:29-37` (`Register` panics on duplicate `SubsystemID`; no `Unregister` method exists on `ReadinessRegistry`)

**Issue:** Before this iteration's fixes, `ABACSubsystem.Stop` never reset `s.stack`, and `PluginSubsystem.Prepare` had no guard at all. Combined, a retried `Prepare` on either subsystem was previously either a silent no-op (ABAC, pre-WR-01) or a genuinely wasteful-but-safe full rebuild (Plugin, pre-IN-01) — in both cases, a second call to `s.cfg.Registry.Register(...)` never happened, because ABAC's old code never reached the guarded-off Prepare body again on retry, and Plugin's old code had never gone through a guard-enforced "first success" boundary that a second call would collide with.

Now that both subsystems reset their guard field in `Stop` (WR-01 for ABAC, IN-01 for Plugin), a legitimate retry sequence — `Prepare` succeeds → some other subsystem's `Activate` fails elsewhere in the sweep → orchestrator rolls back via `Stop` (which resets the guard) → caller retries `StartAll` — now genuinely re-enters the full `Prepare` body on the second attempt, including:

```go
// internal/access/setup/subsystem.go:125-127
if s.cfg.Registry != nil {
	s.cfg.Registry.Register(lifecycle.SubsystemABAC, stack.HealthTracker)
}
```
```go
// internal/plugin/setup/subsystem.go:473-475
if s.cfg.Registry != nil {
	s.cfg.Registry.Register(lifecycle.SubsystemPlugins, s)
}
```

`ReadinessRegistry.Register` (`internal/lifecycle/registry.go:33-35`) panics unconditionally on a duplicate `SubsystemID`:

```go
if _, exists := r.reporters[id]; exists {
	panic("lifecycle: duplicate registration for subsystem " + id.String())
}
```

Neither subsystem's `Stop` unregisters from `s.cfg.Registry`, and `ReadinessRegistry` exposes no `Unregister` method at all — there is no way to undo the first registration. So the retry this round's fixes were written to enable now reaches a **hard panic** on the second `Prepare`, for exactly the two subsystems whose `Stop` methods this round modified to reset their guards. This is a strictly worse failure mode than the WR-01 finding it replaces (silent stale-resource reuse vs. a process panic), even though — like the original WR-01 — it shares the same "not currently triggerable" caveat: production's `cmd/holomush/core.go` calls `StartAll` exactly once and exits on failure, so no in-tree caller retries an `Orchestrator` today. I confirmed no other touched subsystem calls `Registry.Register` inside `Prepare` (`internal/eventbus/subsystem.go`, `internal/eventbus/audit/subsystem.go`, `internal/cluster/heartbeat.go`, `internal/tls/subsystem.go` do not), so this is specifically confined to the two subsystems this round's fixes modified.

**Fix:** Either (a) add an `Unregister(id SubsystemID)` method to `ReadinessRegistry` and call it from both subsystems' `Stop`, or (b) make `Register` idempotent for the same `(id, reporter)` pair re-registered after a `Stop` (e.g., allow overwrite, since `Stop` already tore down the old reporter), or (c) narrow the `lifecycle.Subsystem` interface doc's "a caller may legitimately retry Prepare" claim to explicitly exclude subsystems that register with a `ReadinessRegistry`, until (a) or (b) lands. (a) matches the existing idiom most closely:

```go
// internal/lifecycle/registry.go
func (r *ReadinessRegistry) Unregister(id SubsystemID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.reporters, id)
}
```
```go
// internal/access/setup/subsystem.go — Stop
if s.cfg.Registry != nil {
	s.cfg.Registry.Unregister(lifecycle.SubsystemABAC)
}
```

### WR-03: `grpcSubsystem.Stop` resets four Prepare/Activate-owned fields but leaves `CoordHolder.coord` unreset, inconsistent with the sibling resets added in the same function by this round's fix

**File:** `cmd/holomush/sub_grpc.go:917-951`, `cmd/holomush/cryptowiring.go:335-387`

**Issue:** This round's WR-01 fix added resets for `s.grpcServer`, `s.reaperCancel`, `s.reaperCtx`, and `s.listener` inside `grpcSubsystem.Stop` — but the same function also unconditionally calls `s.cfg.CoordHolder.coord.Stop(ctx)` a few lines later (lines 949-951) without ever setting `s.cfg.CoordHolder.coord = nil`:

```go
if s.cfg.CoordHolder != nil && s.cfg.CoordHolder.coord != nil {
	if stopErr := s.cfg.CoordHolder.coord.Stop(ctx); stopErr != nil {
		slog.WarnContext(ctx, "invalidation.Coordinator stop error", "error", stopErr)
	}
}
return nil
```

`CoordHolder.coord` is set exactly once, inside `buildCryptoWiring` (`cmd/holomush/cryptowiring.go:379`), which itself runs inside a `sync.Once`-memoized closure (`resolveCryptoWiring`, `cmd/holomush/cryptowiring.go:141-156` — "the `sync.Once` body runs AT MOST ONCE per process"). Since a retried `grpcSubsystem.Prepare` calls the same memoized `resolveCryptoWiring()` closure, it will NOT re-run `buildCryptoWiring` and will NOT reassign `CoordHolder.coord` — the field keeps pointing at the coordinator instance that was already `Stop()`-ed in the previous attempt. A retry therefore "succeeds" at `Prepare`/`Activate` while silently leaving the invalidation-fan-out path bound to an already-drained `coordinator` (its own `Stop` is idempotent and won't panic on a second call, but `RequestInvalidation`'s cluster fan-out never resumes because `Start` is never called again either).

This is a narrower issue than WR-01/WR-02 above (the crypto-wiring memoization was already a known, deliberately-documented one-shot-per-process design predating this phase's fixes — see the comment at `cryptowiring.go:344-353`), but it is still a genuine inconsistency introduced by this round's specific fix: every other field this same `Stop` function touches was reset to restore the "fresh state on retry" guarantee the fix's own doc comment claims ("resets... to nil so a legitimate retry of Prepare/Activate after Stop reacquires fresh state"), while `CoordHolder.coord` — set and read in the same function — was not, even though it is exactly the kind of "already-torn-down handle" WR-01 exists to guard against.

**Fix:** At minimum, reset `s.cfg.CoordHolder.coord = nil` alongside the other fields in `Stop` so the staleness is at least visible/nil rather than a live pointer to a stopped coordinator; note in the doc comment that this alone does not make the Coordinator retry-safe (the memoized builder would still need to become retry-aware to actually reconstruct it — out of scope for this fix but should not be silently implied by the "reacquires fresh state" doc comment as currently written).

## Info

### IN-01: Three near-identical `reflect`-based typed-nil detection helpers now exist in three packages instead of one shared helper, as the prior round's WR-02 finding recommended

**Files:** `internal/sysbroadcast/broadcaster.go:51-65` (new, this round), `internal/presence/emitter.go:64-75`, `internal/cluster/registry.go:303-320`

**Issue:** The prior round's WR-02 finding explicitly suggested "reuse (or extract to a shared helper in `internal/eventbus`)" the typed-nil detection pattern. The landed fix instead added a third copy — `sysbroadcast.isNilPublisher` — byte-for-byte identical in body to `presence.isNilPublisher`, both operating on `eventbus.Publisher`. This is correct and consistent (see Summary), but it is the less-preferred of the two options the prior finding offered, and now there are two verbatim-duplicate `isNilPublisher` functions (plus a third `isNilConn` variant differing only by parameter type) doing the same reflection dance in three different packages.

**Fix:** Extract a single generic (or `eventbus.Publisher`-typed) helper — e.g. `eventbus.IsNilPublisher(pub eventbus.Publisher) bool` — and have `presence` and `sysbroadcast` both call it, eliminating one of the two duplicate copies. `cluster.isNilConn` operates on a different interface (`natsconn.Conn`) so it would remain separate unless a generic `reflect`-based nil-check helper is introduced instead.

---

_Reviewed: 2026-07-18T18:51:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
