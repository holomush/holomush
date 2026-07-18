---
phase: 07-event-model-bootstrap-decomposition
fixed_at: 2026-07-18T19:20:00Z
review_path: .planning/phases/07-event-model-bootstrap-decomposition/07-REVIEW.md
iteration: 2
findings_in_scope: 4
fixed: 4
skipped: 0
status: all_fixed
---

# Phase 07: Code Review Fix Report (iteration 2)

**Fixed at:** 2026-07-18T19:20:00Z
**Source review:** .planning/phases/07-event-model-bootstrap-decomposition/07-REVIEW.md
**Iteration:** 2

**Summary:**
- Findings in scope: 4 (0 Critical, 3 Warnings, 1 Info) — `fix_scope: all`
- Fixed: 4
- Skipped: 0

This round's findings were themselves surfaced by adversarially re-reviewing
iteration 1's fixes — two of the four (WR-02, and a regression inside WR-01's
own fix) are gaps in code this same fix-loop had just landed in the prior
iteration, one caught by the reviewer's own tracing and one caught by
`task test:int` during this round's own verification.

Commits were developed in an isolated scratch worktree (`gsd-reviewfix/07-72488`),
fully verified there (`task build`/`test`/`test:int`/`lint` all green), then
cherry-picked onto this branch — cherry-pick was chosen over a fast-forward
merge per explicit user direction, after two earlier incidents this session
where a fixer agent's own cleanup-tail heuristic mis-targeted the primary
`main` checkout instead of this worktree (both self-corrected immediately,
zero actual impact, independently verified via fresh `git fetch` against
`origin/main` both times). Cherry-pick avoids that entire class of risk since
it never moves a branch pointer in another checkout. Commit SHAs below are
the cherry-picked SHAs on this branch, not the original scratch-branch SHAs.

## Fixed Issues

### WR-01: `CheckpointSweepSubsystem.Stop`'s ctx-timeout branch didn't reset its guard, then that fix's own follow-up introduced a nil-channel panic caught by `task test:int`

**File modified:** `internal/eventbus/crypto/dek/sweep.go`
**Commits:** `0e79c76c0`, `b3d597572`

**Applied fix (first commit):** The prior iteration's fix reset `s.done = nil`
only in the happy-path `case <-s.done:` branch, leaving the `case <-ctx.Done():`
timeout branch untouched — so a `Stop` call that timed out left `s.done`
non-nil, causing a subsequent `Activate` retry to silently no-op on a stale
guard. Fixed by capturing `s.done` into a local `done` variable and resetting
`s.done = nil` unconditionally *before* the `select`, then selecting on the
local — mirroring `internal/eventbus/subsystem.go` and
`internal/eventbus/audit/subsystem.go`, both of which reset their guard
fields unconditionally regardless of select/drain outcome.

**Applied fix (second commit — self-caught regression):** `task test:int`
caught a genuine bug this first commit introduced: `CheckpointSweepSubsystem.loop`'s
`defer close(s.done)` reads the `s.done` **field** at defer-evaluation time,
not a value captured when the goroutine launched. Because `go s.loop(sctx)`
does not guarantee the goroutine runs before `Stop` returns, a fast `Stop`
call could nil `s.done` (the very fix above) before `loop`'s own defer
statement evaluated it, causing `close(nil)` to panic. Reproduced directly:
`TestDEKIntegration` panicked at `sweep.go:165` ("close of nil channel").
Fixed by threading the channel into `loop` as an explicit parameter,
captured synchronously at the `go` statement's argument-evaluation time
(before `Stop` can possibly run), so `loop` always closes the exact channel
it was launched with regardless of any later `s.done` field mutation.
Verified with 4 repeated `RACE=-race task test:int -- ./internal/eventbus/crypto/dek/...`
runs, all green.

### WR-02: `ABACSubsystem`/`PluginSubsystem`'s now-functional Prepare retry reaches `ReadinessRegistry.Register` a second time, which panics on duplicate registration

**Files modified:** `internal/lifecycle/registry.go`, `internal/access/setup/subsystem.go`, `internal/plugin/setup/subsystem.go`
**Commit:** `b96a86040`

**Applied fix:** Read `internal/lifecycle/registry.go` in full first,
confirming `Register` panics unconditionally on a duplicate `SubsystemID`
and that `ReadinessRegistry` has no `Unregister` method, and confirming the
locking discipline (`sync.RWMutex`: `Register`/the new `Unregister` take the
write lock via `mu.Lock()`; `AllReady`/`Status` take the read lock via
`mu.RLock()`) — a `delete` under the same write lock `Register` uses is safe
against concurrent reads with no additional discipline needed.

Grepped the whole tree for `Registry.Register(` production callsites
(`rg -n "Registry\.Register\(" --type go -g '!*_test.go'`) and confirmed
only `internal/access/setup/subsystem.go` (ABAC) and
`internal/plugin/setup/subsystem.go` (Plugin) call it inside `Prepare` — no
other subsystem touched by this or the prior round needed the same fix.

Chose option (a) from the review's three options: added
`ReadinessRegistry.Unregister(id SubsystemID)` (deletes from the `reporters`
map under the write lock; no-op if the id was never registered) and called
it from both `ABACSubsystem.Stop` and `PluginSubsystem.Stop`, alongside
their existing guard-field resets. `PluginSubsystem.Stop`'s call is placed
after the existing `s.manager == nil` early-return guard; since `s.manager`
is assigned strictly before `Registry.Register` is called in `Prepare`, a
non-nil `s.manager` at `Stop` time does not guarantee `Register` was
reached — safe regardless, since the new `Unregister` is a no-op on an
absent key.

### WR-03: `grpcSubsystem.Stop` resets four Prepare/Activate-owned fields but leaves `CoordHolder.coord` unreset

**File modified:** `cmd/holomush/sub_grpc.go`
**Commit:** `12fadf4d6`

**Applied fix:** Added `s.cfg.CoordHolder.coord = nil` immediately after
`coord.Stop(ctx)`, at minimum making the staleness visible (nil) rather than
leaving a live pointer to an already-drained `invalidation.Coordinator`. Per
the review's explicit caution, amended the `Stop` doc comment to state
plainly that this reset alone does **not** make the Coordinator retry-safe:
`cryptowiring.go`'s `resolveCryptoWiring` is a `sync.Once`-memoized closure
(a pre-existing, documented one-shot-per-process design predating this
phase) that will not reconstruct or restart a new Coordinator on a retried
`Prepare` — cluster invalidation fan-out simply does not resume after a
retry. That gap is explicitly called out as out of scope for this fix.

### IN-01: Three near-identical `reflect`-based typed-nil-detection helpers existed instead of one shared helper

**Files modified:** `internal/eventbus/bus.go`, `internal/presence/emitter.go`, `internal/sysbroadcast/broadcaster.go`
**Commit:** `d4b4d950a`

**Applied fix:** Extracted a single shared `eventbus.IsNilPublisher(pub Publisher) bool`
into `internal/eventbus/bus.go` (next to the `Publisher` interface it
operates on), carrying the same reflect-based `Chan/Func/Interface/Map/Pointer/Slice`
kind switch as both prior copies. Updated `presence.NewEmitter` and
`sysbroadcast.NewBroadcaster` to call `eventbus.IsNilPublisher(pub)` instead
of their local `isNilPublisher` helpers, deleting both duplicate function
bodies and their now-unused `reflect` imports. Left `cluster.isNilConn`
untouched — it operates on `natsconn.Conn`, a distinct interface, per the
review's fix note. Verified the remaining `isNilPublisher` mentions in
`sysbroadcast/broadcaster_test.go` and `presence/emitter_test.go` are stale
comments only (no code reference), so no test file needed updating.

## Skipped Issues

None — all findings were fixed.

## Verification

- `task build` — passes, verified at multiple intermediate states and on
  the final worktree after cherry-pick.
- Targeted `task test` on intermediate states (`./internal/eventbus/crypto/dek/...`,
  `./internal/lifecycle/...`, `./internal/access/setup/...`,
  `./internal/plugin/setup/...`, `./cmd/holomush/...`, `./internal/eventbus/...`,
  `./internal/presence/...`, `./internal/sysbroadcast/...`, `./internal/cluster/...`) —
  all passed at each step.
- Full `task test` on the final five-commit state: **10286 tests passed, 0
  failed, 4 skipped** (pre-existing quarantine/opt-in markers, unrelated to
  this phase).
- Full `task test:int` on the final five-commit state: **10706 tests
  passed, 0 failed, 7 skipped** (pre-existing quarantine/nightly-opt-in
  markers, unrelated to this phase) — this run is what caught the WR-01
  nil-channel regression described above, confirming the integration gate's
  value for exactly this class of lifecycle-timing bug.
- `task lint`: clean, 0 issues.
- Re-verified independently after cherry-picking onto this branch: `task build` passes.

## Notes

- Two of the four findings in this round (WR-01's nil-channel panic,
  WR-02's registry panic) are themselves regressions introduced by *this
  same fix-loop's* iteration-1 changes, caught by a genuinely adversarial
  re-review and by the fixer's own `task test:int` run rather than assumed
  away. This is the intended behavior of the `--auto` iteration loop — each
  pass re-verifies the previous pass's work rather than trusting it.
- All four findings were fixed exactly as diagnosed by the review — no
  finding required deviating from the review's suggested fix shape.
- No findings were skipped.

---

_Fixed: 2026-07-18T19:20:00Z_
_Fixer: Claude (gsd-code-fixer) + orchestrator (cherry-pick + report finalization, per explicit user direction on the merge mechanism)_
_Iteration: 2_
