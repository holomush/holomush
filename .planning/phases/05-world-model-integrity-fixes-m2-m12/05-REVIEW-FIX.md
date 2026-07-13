---
phase: 05-world-model-integrity-fixes-m2-m12
fixed_at: 2026-07-13T17:03:42Z
review_path: .planning/phases/05-world-model-integrity-fixes-m2-m12/05-REVIEW.md
iteration: 1
findings_in_scope: 6
fixed: 5
skipped: 1
status: partial
---

# Phase 5: Code Review Fix Report

**Fixed at:** 2026-07-13T17:03:42Z
**Source review:** .planning/phases/05-world-model-integrity-fixes-m2-m12/05-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope: 6 (Critical + Warning + Info)
- Fixed: 5
- Skipped: 1

Verification: `task build` green; `task test -- ./internal/world/...` green (905
tests, `-race`); `go vet -tags=integration` green over the three touched world
packages (integration-tagged tests compile against the new fail-closed guard).

## Fixed Issues

### CR-01: Outbox LISTEN waker never resets a dead connection → permanent busy-loop

**Files modified:** `internal/world/setup/relay_subsystem.go`
**Commit:** 20c32ab14
**Applied fix:** In `outboxWaker.Wait`, on any `WaitForNotification` error, take
`w.mu`, and if `w.conn` is still the same pinned connection, `Release()` it and
set `w.conn = nil` so the next `Wait` re-acquires a live connection and
re-`LISTEN`s. This makes the post-acquire death path self-heal, matching the
acquire/LISTEN failure paths that already left `w.conn == nil`. Releasing on a
benign ctx-deadline is cheap (re-acquires next tick).
**Note (human verification recommended):** liveness/concurrency correctness
change — syntax + unit tests pass but the failover self-heal is best confirmed
against a real Postgres restart in integration/E2E.

### WR-01: Relay `Drain` held the state mutex across the network publish, blocking `Halted()`

**Files modified:** `internal/world/outbox/relay.go`
**Commit:** 58fa04f89
**Applied fix:** Introduced a separate lightweight `haltMu` protecting only the
`halted`/`haltPosition` health-signal fields, leaving `mu` to serialize drains
and protect the lease. `Halted()`, the new `haltState()` snapshot, and
`halt()`/`clearHalt()` now use `haltMu`; `Drain` reads halt state via
`haltState()` under the short lock. Ordering and one-event-per-position are
preserved — `mu` is still held across the publish, so nothing about the strict
`(epoch, feed_position)` drain changed; only the alert probe was decoupled.
**Note (human verification recommended):** lock-decomposition change — worth a
human read to confirm the halt-state ↔ drain interaction is race-free.

### WR-02: `publishOne` retried transient failures in a tight loop with no backoff

**Files modified:** `internal/world/outbox/relay.go`
**Commit:** 58fa04f89
**Applied fix:** Added `sleepWithBackoff`/`backoffDelay`: exponential backoff
(`50ms` base, capped at `2s`) with equal (half-fixed + half-random) jitter drawn
from `crypto/rand` (never `math/rand`, per repo convention), between transient
retry attempts (never after the last attempt). The sleep is `ctx`-aware and
returns `cancelErr(ctx)` on cancel so `Stop` unwinds promptly.

### IN-01: Unreachable post-loop permanent-error re-check in `publishOne`

**Files modified:** `internal/world/outbox/relay.go`
**Commit:** 58fa04f89
**Applied fix:** Removed the dead post-loop `if isPermanentPublishErr(lastErr)`
branch. A permanent error already returns `errPoison` from inside the loop, so
the only way out with `lastErr != nil` is a run of transient failures; the loop
now flows straight to the transient-error return, with a comment stating the
invariant.

### IN-02: `SET LOCAL lock_timeout` silently no-ops on the fallback-to-pool path

**Files modified:** `internal/world/postgres/feed_counter.go`
**Commit:** 2934bb467
**Applied fix:** Chose the fail-closed option from the review. `FeedCounter.Allocate`
now returns `oops.Code("WORLD_FEED_ALLOCATE_NO_TX")` when `txFromContext(ctx)`
is nil, refusing to allocate on the raw pool where `SET LOCAL` no-ops and the
`SELECT ... FOR UPDATE` + increment could split across connections (duplicate
`feed_position` risk). The genesis-store sites the review also cited
(`genesis_store.go:156-158`, `246-248`) already run inside `withTx`, so their
`execerFromCtx` always resolves to a tx — no change needed there. All existing
`Allocate` callers (production `outbox_store.go` and the integration tests) run
inside `Transactor.InTransaction`/`withTx`, so the guard does not regress them.

## Skipped Issues

### IN-03: Reference consumer is constructed but never runs a live consume loop

**File:** `internal/world/setup/relay_subsystem.go:94-97`
**Reason:** won't_fix — intentional per review; scoped out of Phase 5. The
reviewer explicitly flagged this as NOT a defect: the reference consumer is
deliberately wired without a live durable-JetStream consume loop, which lands in
05-11. No code change made; the reference consumer is left in place.
**Original issue:** `s.consumer` is built with a nil effect and exposed via
`Consumer()`, but nothing drives `Consumer.Apply` or calls `outbox.Bootstrap`,
so the watermark/receipt tables are unpopulated in Phase 5 (by design).

---

_Fixed: 2026-07-13T17:03:42Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
