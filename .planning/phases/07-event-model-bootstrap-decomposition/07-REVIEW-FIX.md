---
phase: 07-event-model-bootstrap-decomposition
fixed_at: 2026-07-18T06:10:00Z
review_path: .planning/phases/07-event-model-bootstrap-decomposition/07-REVIEW.md
iteration: 1
findings_in_scope: 4
fixed: 4
skipped: 0
status: all_fixed
---

# Phase 7: Code Review Fix Report

**Fixed at:** 2026-07-18T06:10:00Z
**Source review:** .planning/phases/07-event-model-bootstrap-decomposition/07-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope: 4 (1 Critical, 3 Warnings)
- Fixed: 4
- Skipped: 0

## Fixed Issues

### CR-01: `invalidation.Coordinator` leaks on any Prepare failure between `CryptoChainVerifier` and `grpcSubsystem`

**Files modified:** `cmd/holomush/core.go`, `cmd/holomush/cryptowiring.go`, `cmd/holomush/cryptowiring_test.go`
**Commit:** `2a3b15631`
**Applied fix:** Chose the review's option (b) — an orchestrator-independent
cleanup keyed on whether the Coordinator was actually constructed+started,
not on any one subsystem's own Prepare outcome. Added
`stopCoordinatorOnBootFailure(ctx, holder *coordHolder)` in
`cryptowiring.go`, called unconditionally from `runCoreWithDeps` on every
`orch.StartAll` failure (previously the code just `return orchErr`'d with
no cleanup at all). The helper no-ops when `holder` or `holder.coord` is
nil, and Stops the Coordinator with a 5s timeout otherwise, logging (not
propagating) any Stop error. This is mutually exclusive with
`grpcSubsystem.Stop`'s existing happy-path Coordinator-Stop (only reached
via the deferred `orch.StopAll` after a StartAll success), so there is no
double-stop or race — `invalidation.Coordinator.Stop` is idempotent in any
case. Updated two doc comments in `cryptowiring.go` that previously
implied `grpcSubsystem.Stop` was the *sole* cleanup owner.

Added `TestStopCoordinatorOnBootFailure` (4 subtests, `cryptowiring_test.go`)
using a minimal `fakeCoordinator` test double satisfying the 3-method
`invalidation.Coordinator` interface: verifies Stop is called when a
coordinator was started, and that the helper is a safe no-op when the
coordinator was never started (nil `holder.coord`, e.g. no KEK) or when
`holder` itself is nil. A full end-to-end regression (a real `StartAll`
that fails after the Coordinator has started, asserting via goleak that
its goroutines/subscriptions are gone) was not added — constructing that
scenario needs a real EventBus + cluster.Registry + KEK-backed
`dek.Manager`, which is full-integration-harness territory (per the
07-11 precedent cited in the review) rather than a cheap unit test; the
unit-level tests above directly exercise the new cleanup logic instead.

### WR-01: `PluginSubsystem.Prepare`'s world-conn error path bypasses `cleanupOnError`, leaking the Lua host

**Files modified:** `internal/plugin/setup/subsystem.go`
**Commit:** `0d0ffda52`
**Applied fix:** Applied the review's suggested fix verbatim in shape:
on `newWorldInProcessConn`'s error path (before `cleanupOnError` is even
defined later in the function), close `s.luaHost` inline if non-nil and
nil it out, before returning `WORLD_INPROCESS_CONN_FAILED`. Updated
`Stop`'s doc comment, which previously overclaimed that `cleanupOnError`
alone accounted for every pre-manager error path — it now correctly notes
this one specific path releases its own resource in-body.

No new regression test added: `newWorldInProcessConn`'s only error path
is `plugins.NewInProcessConn` returning a non-nil error, which only
happens when its `*grpc.Server` argument is nil — but the call site
always constructs a fresh `grpc.NewServer()` first, so this path has no
cheap, deterministic public-API injection point (unlike WR-01's sibling
fault-injection test `TestPluginSubsystemPrepareFailureAfterHostConstructionLeavesNoSweeperGoroutine`,
which injects via `VerbRegistry: nil`). Existing tests in
`internal/plugin/setup` (18 tests, including the goleak-based sweeper-leak
test) continue to pass unmodified.

### WR-02: `PluginEventEmitter.Emit` constructs a raw `eventbus.Event{}` literal instead of `eventbus.NewEvent()`

**Files modified:** `internal/plugin/event_emitter.go`
**Commit:** `4d6bdbad3`
**Applied fix:** Replaced the raw `eventbus.Event{...}` literal with
`eventbus.NewEvent(sub, typ, busActor, payload)` followed by
`event.Sensitive = sensitive`, exactly matching the review's suggested
fix and `NewEvent`'s own doc-comment-prescribed override pattern. Removed
the now-unused `time` import (the literal's `time.Now().UTC()` call was
the only user of that package in the file); `core` remains used elsewhere
in the file. `task lint:go` on `internal/plugin/...` reports 0 issues.
This file is named in CLAUDE.md's Pre-Push Review Gates table
(`internal/plugin/event_emitter.go::Emit`) as requiring `crypto-reviewer`
sign-off — the orchestrator should run that gate after these fixes land.

### WR-03: Session reaper's `bootAt` is captured at construction (Prepare) rather than when `Run()` actually starts consuming it (Activate)

**Files modified:** `internal/session/reaper.go`, `internal/session/reaper_test.go`
**Commit:** `652e2587d`
**Applied fix:** Moved the `r.bootAt = config.Now()` stamp from
`NewReaper` (construction) to the top of `Run()`, per the review's first
suggested option. `grpcSubsystem.Activate` calls
`go s.sessionReaper.Run(s.reaperCtx)` directly (no intermediate
`MarkBoot()`-style call site to wire), so this required no changes to
`cmd/holomush/sub_grpc.go`. Existing reaper tests (`reaper_test.go`,
`reaper_lease_test.go`) call `Run` immediately after `NewReaper` with a
fixed fake clock, so their behavior is unaffected.

Added `TestReaperCapturesBootAtWhenRunStartsNotWhenConstructed`
(`reaper_test.go`): asserts `config.Now()` is never called during
`NewReaper` and is only invoked once `Run` starts the sweep loop, using
an atomic call counter — a direct, cheap regression test for the fix
(no Docker/testcontainer dependency, unlike the sibling lease-sweep
tests in the same package).

## Skipped Issues

None — all findings were fixed.

## Verification

Run inside an isolated worktree (`gsd-reviewfix/07-7388`, branched from
`gsd/phase-07-event-model-bootstrap-decomposition`), one commit per
finding, each verified individually with `task build` + a scoped
`task test` before committing. After all four fixes landed:

- `task build`: passes.
- `task test` (full suite): **10285 tests passed, 0 failed, 4 skipped**
  (skips are pre-existing quarantine/opt-in markers unrelated to this
  phase).
- `task test:int` (full suite): **10705 tests passed, 0 failed, 7 skipped**
  (skips are pre-existing quarantine/nightly-opt-in markers unrelated to
  this phase).
- `task lint:go` on every touched package: 0 issues.

No logic-classified findings in this batch required a
"requires human verification" downgrade — CR-01, WR-01, and WR-03 are all
lifecycle/resource-cleanup fixes with direct unit-test coverage of the new
code paths (or, for WR-01, coverage of the surrounding invariant via
existing tests); WR-02 is a mechanical single-construction-path
substitution verified by the existing emitter test suite plus a clean
lint pass.

**Follow-up for the orchestrator:** WR-02 touches
`internal/plugin/event_emitter.go::Emit`, which CLAUDE.md's Pre-Push
Review Gates table names as requiring `crypto-reviewer` sign-off before
push/PR.

---

_Fixed: 2026-07-18T06:10:00Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
