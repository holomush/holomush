---
phase: 05-world-model-integrity-fixes-m2-m12
reviewed: 2026-07-13T17:03:42Z
depth: standard
files_reviewed: 34
files_reviewed_list:
  - internal/world/outbox/relay.go
  - internal/world/outbox/store.go
  - internal/world/outbox/consumer.go
  - internal/world/outbox/genesis.go
  - internal/world/outbox/skip.go
  - internal/world/outbox/wire.go
  - internal/world/outbox/taxonomy.go
  - internal/world/outbox/metrics.go
  - internal/world/postgres/outbox_lease.go
  - internal/world/postgres/outbox_store.go
  - internal/world/postgres/consumer_checkpoint_store.go
  - internal/world/postgres/feed_counter.go
  - internal/world/postgres/genesis_store.go
  - internal/world/postgres/reaping_guard.go
  - internal/world/postgres/character_repo.go
  - internal/world/postgres/exit_repo.go
  - internal/world/postgres/location_repo.go
  - internal/world/postgres/binding_repo.go
  - internal/world/postgres/helpers.go
  - internal/world/postgres/transactor.go
  - internal/world/mutator.go
  - internal/world/service.go
  - internal/world/wmodel/envelope.go
  - internal/world/wmodel/mutation_delta.go
  - internal/world/setup/relay_subsystem.go
  - internal/world/setup/subsystem.go
  - internal/auth/character_genesis.go
  - internal/auth/character_reaping.go
  - internal/auth/character_service.go
  - internal/auth/guest_service.go
  - internal/auth/postgres/player_repo.go
  - internal/bootstrap/admin.go
  - internal/bootstrap/setup/adapters.go
  - cmd/holomush/outbox_admin.go
  - cmd/holomush/world_genesis.go
  - cmd/holomush/sub_grpc.go
  - internal/store/migrations/000049_world_version_guard.up.sql
  - internal/store/migrations/000049_world_version_guard.down.sql
  - internal/store/migrations/000050_world_outbox.up.sql
  - internal/store/migrations/000050_world_outbox.down.sql
  - internal/store/migrations/000051_player_reaping.up.sql
  - internal/store/migrations/000051_player_reaping.down.sql
  - internal/lifecycle/subsystem.go
  - internal/lifecycle/subsystemid_string.go
findings:
  critical: 1
  warning: 2
  info: 3
  total: 6
status: issues_found
---

# Phase 5: Code Review Report

**Reviewed:** 2026-07-13T17:03:42Z
**Depth:** standard
**Files Reviewed:** 34 (of the 131-file scope; the balance are generated mocks, tests, and docs given a light pass)
**Status:** issues_found

## Summary

Phase 5 delivers the world-model integrity substrate: a version-predicated CAS
guard (MODEL-03), a transactional outbox → ordered leased relay → idempotent
reference consumer (MODEL-04), and the atomic character genesis/reaping services
with anti-TOCTOU reaping serialization. The core concurrency machinery is
unusually well-constructed and I traced the highest-risk paths in depth:

- **CAS correctness** (`character_repo.go`, `exit_repo.go`, `location_repo.go`):
  the version-predicated `UPDATE ... WHERE id=$1 AND version=$2 RETURNING`,
  zero-row → locked-follow-up classification into `WORLD_CONCURRENT_EDIT` vs
  `CHARACTER_NOT_FOUND`, and the same-connection `withTx` re-entrancy are sound.
- **Lease fencing** (`outbox_lease.go`): the pinned-connection session advisory
  lock + durable `lease_generation` bump on acquire + generation compare on
  `MarkPublished` correctly rejects a stale holder's DB ack; `alive()` catches a
  dropped connection. At-least-once + `Nats-Msg-Id` dedup is honestly documented,
  not overclaimed.
- **Ordered relay + one-event-per-position** (`relay.go`, `consumer_checkpoint_store.go`):
  strict `(epoch, feed_position)` drain, halt-on-poison, contiguity-safe
  watermark advance with receipt-claim atomicity, and the skip-marker
  same-position republish all hold up.
- **Reaping anti-TOCTOU** (`reaping_guard.go`, `player_repo.go`,
  `character_reaping.go`): `MarkReaping` (UPDATE) and the genesis
  `SELECT reaping_at ... FOR UPDATE` contend on the same `players` row, closing
  the snapshot-then-delete window; per-character resumable tx scoping is correct.
- **Fail-closed wiring**: every new service constructor rejects nil deps and the
  composition roots (`sub_grpc.go`, `bootstrap/setup`) propagate errors.

The one Critical finding is a liveness defect in the relay's LISTEN waker that
turns a routine Postgres connection drop (failover, restart, proxy idle-kill)
into a permanent CPU/DB busy-loop. The remaining findings are robustness and
clarity issues.

## Critical Issues

### CR-01: Outbox LISTEN waker never resets a dead connection → permanent busy-loop on connection loss

**File:** `internal/world/setup/relay_subsystem.go:200-219`
**Issue:** `outboxWaker.Wait` acquires and pins a dedicated `LISTEN` connection
only inside the `if w.conn == nil` guard, then blocks on
`conn.Conn().WaitForNotification(ctx)`. When that pinned connection dies
(Postgres restart/failover, network blip, a proxy killing an idle LISTEN
session), `WaitForNotification` returns an error **immediately** — but `w.conn`
is never set back to `nil`. Every subsequent `Wait` call sees `w.conn != nil`,
skips re-acquisition, reuses the dead connection, and errors instantly again.

The relay's `Run` loop wraps `Wait` in `context.WithTimeout(ctx, SweepInterval)`
(`relay.go:155-161`), relying on that timeout to pace the sweep. But a dead
connection makes `Wait` return *before* the timeout, so the pacing is lost: the
loop degenerates to `Drain → Wait(instant error) → Drain → …` with zero delay.
Result: a permanent CPU busy-loop issuing a continuous storm of `NextUnpublished`
scans against the DB, which never self-heals until the subsystem is restarted.
The acquire/LISTEN failure paths (`relay_subsystem.go:203-212`) correctly leave
`w.conn == nil` and self-heal — only the post-acquire death path is unhandled,
making the bug asymmetric and easy to miss.

Connection loss is a routine production event (PG failover, rolling restart), so
this is a realistic self-inflicted availability failure, not a theoretical one.

**Fix:** On any `WaitForNotification` error, drop the pinned connection so the
next `Wait` re-acquires and re-`LISTEN`s:
```go
_, err := conn.Conn().WaitForNotification(ctx)
if err != nil {
    // Deadline/cancel OR a dead connection both land here. Release the pinned
    // conn so the next Wait re-acquires a live one; a healthy re-LISTEN restores
    // NOTIFY delivery instead of spinning on a dead session.
    w.mu.Lock()
    if w.conn == conn {
        w.conn.Release()
        w.conn = nil
    }
    w.mu.Unlock()
}
return err //nolint:wrapcheck // ctx/notify signal; relay re-drains regardless
```
(Releasing on a benign ctx-deadline just re-acquires next tick — cheap and
correct. Alternatively distinguish `ctx.Err() != nil` from a genuine conn error
and only reset on the latter.)

## Warnings

### WR-01: Relay `Drain` holds the state mutex across the network publish, blocking the halt-alert health probe

**File:** `internal/world/outbox/relay.go:178-244` (mutex acquired at 179-180) with `Halted()` at `internal/world/outbox/relay.go:97-101`
**Issue:** `Drain` locks `r.mu` for the entire pass — including
`r.cfg.Publisher.Publish(ctx, ev)` inside `publishOne` (relay.go:262), a network
round-trip to JetStream. `Halted()` also takes `r.mu`. `Halted()` /
`relayHaltPosition` is the operator's health/alert signal for a stalled feed, yet
it is exactly during a slow or hung publish (a broker in trouble) that a health
probe most needs to read state — and that is precisely when it will block behind
the publishing `Drain`. The observability signal degrades under the same
conditions it exists to surface.
**Fix:** Don't hold `r.mu` across the publish. Snapshot the halt state under a
short lock in `Halted()` (it already does), and narrow `Drain`'s critical
sections so the network publish runs outside the lock, or serialize drains with a
separate `drainMu` from the `haltMu` protecting the halt fields, so `Halted()`
never contends with an in-flight publish.

### WR-02: `publishOne` retries transient publish failures in a tight loop with no backoff

**File:** `internal/world/outbox/relay.go:257-269`
**Issue:** The retry loop calls `r.cfg.Publisher.Publish` up to
`MaxPublishAttempts` times back-to-back with no delay between attempts. When the
broker is degraded (the common transient case this loop exists to absorb), the
relay hammers it with immediate retries, adding load precisely when the broker is
struggling. In a resilience-focused phase this is a robustness gap, not merely a
micro-optimization. (The per-pass resume-on-next-sweep behavior does bound total
retries, but within a pass there is no pacing.)
**Fix:** Add a bounded backoff (e.g. exponential with jitter, capped) between
attempts, honoring `ctx` cancellation between sleeps so `Stop` still unwinds
promptly.

## Info

### IN-01: Unreachable post-loop permanent-error re-check in `publishOne`

**File:** `internal/world/outbox/relay.go:270-274`
**Issue:** Inside the retry loop, a permanent error already returns `errPoison`
(relay.go:266-268). The only way to exit the loop with `lastErr != nil` is a run
of *transient* failures, so the post-loop `if isPermanentPublishErr(lastErr)`
(relay.go:271-273) can never be true — dead code that suggests a control-flow
invariant the reader must re-derive.
**Fix:** Remove the redundant post-loop `isPermanentPublishErr` branch; the loop
body is the sole permanent-vs-transient classifier.

### IN-02: `SET LOCAL lock_timeout` silently no-ops on the fallback-to-pool path

**File:** `internal/world/postgres/feed_counter.go:64-67` (also `genesis_store.go:156-158`, `247-248`)
**Issue:** `Allocate` runs `SET LOCAL lock_timeout` via `execerFromCtx(ctx, pool)`.
`SET LOCAL` only affects the current transaction; if `Allocate` is ever invoked
without an ambient tx (the documented-but-real fallback to the raw pool), the
`SET LOCAL` runs on one pooled connection with no effect, and the subsequent
`SELECT ... FOR UPDATE` + increment may land on *different* pooled connections —
losing both the lock timeout and the atomicity of the position allocation
(risking a duplicate `feed_position`). Production always wraps `Allocate` in the
mutation tx, so this is latent, not live — but the fallback is a footgun for a
future caller.
**Fix:** Either fail closed when no ambient tx is present in `Allocate` (return a
coded error rather than silently degrading), or document with a compile-time/
runtime guard that `execerFromCtx` MUST resolve to a tx here.

### IN-03: Reference consumer is constructed but never runs a live consume loop

**File:** `internal/world/setup/relay_subsystem.go:94-97`
**Issue:** `s.consumer` is built with a nil effect and exposed via `Consumer()`,
but nothing in the wired subsystem drives `Consumer.Apply` or calls
`outbox.Bootstrap`, so `world_consumer_watermarks` / `world_consumer_receipts`
are never populated in Phase 5. This is intentionally scoped (the comment defers
the live loop), and the relay's publish path is fully live, so it is not a
defect — flagged only so downstream reviewers don't mistake the unpopulated
watermark tables for a bug when validating the feed.
**Fix:** None required for Phase 5; confirm the follow-up plan wires the consume
loop + `Bootstrap` before any real projection depends on the watermark.

---

_Reviewed: 2026-07-13T17:03:42Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
