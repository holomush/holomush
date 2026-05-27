<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Cursor Lock — Deterministic Closure of Finding 1

**Tracking:** holomush-9ues
**Discovered from:** holomush-43nd / PR #198 (CodeRabbit thread on the renamed Finding 1 test)
**Status:** Planning → implementation

## Goal

Deterministically close the read-after-write race between `replayAndSend` (Send → UpdateCursors gap) and a concurrent `Subscribe` reading the cursor on reconnect. PR #198 mitigated the practical impact by ~10x (race window 1–10ms instead of unbounded) but did not eliminate the race.

## Decision: Option 2 (in-process per-session lock)

Evaluated against ticket options 1 (`SELECT FOR SHARE`), 3/4 (persist-before-Send), 5 (reconnect cookie), and a WAL/commit-ahead variant. Summary:

| Option | Verdict |
| --- | --- |
| 1 — `SELECT FOR SHARE` | Does not actually close the race. Postgres lock ordering allows the new Subscribe to acquire the shared lock *before* the old handler's UPDATE, read the stale cursor, and release — leaving the duplicate. Would only work if Old held the row lock across `Send`, which means a DB lock across network I/O — unacceptable. |
| 3/4 — persist-before-Send | Trades observable duplicates for **silent message loss** on `Send` failure. Strictly worse failure mode. |
| 5 — reconnect cookie | Architecturally clean but **requires client-side state**. Telnet adapter has no place to persist a per-stream last-seen ID across reconnects (a fresh terminal session has no prior state). Non-starter for the actual use case. |
| WAL / commit-ahead | Same atomicity problem as option 3 — the WAL entry must commit before or after `Send`, and either ordering reproduces option 3's silent loss or option 5's TOCTOU. The only WAL design that actually works requires full at-least-once delivery (client ACKs + retry queue), which is a multi-PR effort and out of scope. |
| **2 — per-session in-process lock** | **Chosen.** Deterministic, no proto change, no client change, no DB schema change. Locks in the single-process assumption — acceptable, since HoloMUSH was only ever going to run as a single core process. |

## Critical sections (minimum viable hold time)

The lock must cover the atomic operation "this event has been Sent AND the cursor reflects it." Any shorter critical section leaves a TOCTOU window where a concurrent reader observes the stale cursor.

| Path | Lock hold | Bound |
| --- | --- | --- |
| `replayAndSend` per event | from before `Send(ev)` to after `UpdateCursors(ev.ID)` | 1 Send + 1 DB UPDATE per event (~5–15ms typical) |
| `Subscribe` cursor read | around `sessionStore.Get(req.SessionId)` only | 1 DB SELECT |

**Per-event commit (not batch-end commit) is the shortest possible deterministic critical section.** The current PR #198 design commits the *batch's* last event ID once at the end of `replayAndSend`. Per-event commit replaces that with one CAS UPDATE per event. The trade is N round trips instead of 1 for batches > 1, but each contiguous lock-hold is bounded by *one* event's `Send + commit`, not the whole batch. A waiting concurrent Subscribe can slip in between events in the loop, not after the entire batch completes.

For typical live events (batch size 1), there is zero overhead vs. the current design. For initial replay after a long disconnect (batch size up to `maxReplay = 1000`), per-event commit adds N − 1 extra UPDATE statements. Each is a single-row JSONB merge with a CAS check — bounded and cheap.

## Lock structure

Per-session refcounted mutex map. Refcounting auto-cleans entries when no one holds the lock — no lifecycle hook plumbing required.

```go
// internal/grpc/cursor_lock.go (new file)

type sessionCursorLock struct {
    mu       sync.Mutex
    refCount int // protected by cursorLockMap.mu, NOT mu
}

type cursorLockMap struct {
    mu    sync.Mutex
    locks map[string]*sessionCursorLock
}

func (m *cursorLockMap) acquire(sessionID string) *sessionCursorLock // ++refCount, return lock
func (m *cursorLockMap) release(sessionID string)                    // --refCount, delete at 0
```

**Contract:** callers MUST `Unlock` the per-session `mu` BEFORE calling `release`. Defer LIFO ordering enforces this:

```go
lock := s.cursorLocks.acquire(sessionID)
defer s.cursorLocks.release(sessionID) // runs second
lock.mu.Lock()
defer lock.mu.Unlock()                 // runs first
```

## Test seam

A new optional hook on `CoreServer`:

```go
// cursorCommitHook, when non-nil, is called inside replayAndSend's
// per-event critical section AFTER Send returns and BEFORE UpdateCursors.
// Production leaves it nil. Tests use it to pause inside the critical
// section and drive a concurrent Subscribe to assert deterministic
// closure of Finding 1.
cursorCommitHook func(ctx context.Context, sessionID string, eventID ulid.ULID)
```

Wired via a new option:

```go
WithCursorCommitHook(hook func(context.Context, string, ulid.ULID)) CoreServerOption
```

## Acceptance criteria

- [x] Decision documented (this file).
- [ ] New deterministic integration test in `test/integration/session/` that:
  - Sets a `cursorCommitHook` to block A's critical section
  - Triggers a live event on session S via Subscribe A
  - Waits for the hook to fire (A is now blocked AFTER Send, BEFORE commit)
  - Opens Subscribe B for session S with `ReplayFromCursor=true`
  - Releases A
  - Drains B's replay phase
  - Asserts B did NOT receive the event A already received (no duplicate)
- [ ] No reliance on `GracefulStop` or wall-clock timing in the new test.
- [ ] Findings 2 and 3 tests in `session_persistence_integration_test.go` continue to pass unchanged.
- [ ] Existing unit tests for `replayAndSend` / `Subscribe` continue to pass.
- [ ] `task pr-prep` green.

## Files touched

- `internal/grpc/cursor_lock.go` (new)
- `internal/grpc/server.go` (CoreServer struct, NewCoreServer, replayAndSend, Subscribe, new option)
- `test/integration/session/cursor_lock_integration_test.go` (new) — keeps the new spec separate from `session_persistence_integration_test.go` to minimize merge surface

## Out of scope

- At-least-once delivery with ACKs (multi-PR effort).
- The reconnect-cookie proto change (would be additive but offers no benefit telnet can use).
- Cleaning up the small TCP-ACK silent-loss window that exists between `Send` returning and the TCP segment being acknowledged. PR #198 accepted this; this PR does not change it.
