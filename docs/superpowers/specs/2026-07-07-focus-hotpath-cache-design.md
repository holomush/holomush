<!--
SPDX-License-Identifier: Apache-2.0
Copyright 2026 HoloMUSH Contributors
-->

# Focus-Redirect Hot-Path Cache Design

- **Bead:** holomush-wm0fi
- **Status:** READY (design-reviewer round 2, 2026-07-07; round 1 NOT READY addressed)
- **Date:** 2026-07-07
- **Theme:** social-spaces
- **Supersedes / builds on:** PR #4574 (focus-redirect introduction), PR #4585
  (holomush-uprtc, INV-SCENE-67 fail-closed focus read)

The keywords **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** in
this document are to be interpreted as described in RFC 2119.

## Overview

`Dispatcher.maybeRedirectForFocus` (`internal/command/dispatcher.go:362`) reads a
connection's focus kind on **every** dispatch of the redirect-candidate verbs
`pose` / `say` / `ooc` / `emit` — the highest-frequency command class in the
game. The read chain is:

```text
maybeRedirectForFocus (dispatcher.go:371)
  → FocusReader.ConnectionFocusKind (focus_reader.go:42)
    → session.Store.GetConnection            ← uncached Postgres PK SELECT
```

`GetConnection` is documented as an `O(1)` primary-key `SELECT` (`session.go:460`)
and is a plain, non-locking `SELECT` (`internal/store/session_store.go:992`), but
it is still a Postgres round-trip. Before PR #4574 introduced focus routing, the
dispatch path for these verbs was **DB-free**. The regression: one synchronous
database read per ambient-verb command, on the hottest path in the server.

This design adds the missing cache-aside layer so the hot path serves focus from
memory, while preserving the fail-closed correctness INV-SCENE-67 established (a
focus-read failure MUST abort dispatch, never route scene-private content to the
grid).

### Where focus is written — the correctness chokepoint

Grounding the write paths shows the bead's proposed framing ("invalidate vs
`SetConnectionFocus`") is both **too narrow and misdirected**. There are two
distinct classes of write:

**(A) Live focus change on an existing connection — the correctness-critical
path.** Three coordinator entry points mutate a live connection's
`Connection.FocusKey`:

| Coordinator path         | Site                                              |
| ------------------------ | ------------------------------------------------- |
| `SetConnectionFocus`     | `internal/grpc/focus/set_connection_focus.go:91`  |
| `AutoFocusOnJoin`        | `internal/grpc/focus/auto_focus_on_join.go:149`   |
| `RestoreConnectionFocus` | `internal/grpc/focus/restore_connection_focus.go:58` |

All three write `conn.FocusKey` **inside a mutator** passed to
`Store.UpdateSessionConnection`; at SQL, the only `UPDATE … SET focus_key`
statement is inside that single method (`internal/store/session_store.go:1206`).
So **`UpdateSessionConnection` is the single chokepoint through which a live
connection's focus ever changes.** Hooking `SetConnectionFocus` alone (the bead's
suggestion) would miss `AutoFocusOnJoin` and `RestoreConnectionFocus`; hooking
`UpdateSessionConnection` catches all three by construction.

**(B) Connection creation / removal — a memory-hygiene concern, not a correctness
one.** `AddConnection` inserts a row that MAY carry an initial focus
(`internal/store/session_store.go:558`). Removal clears focus by dropping the row:
`RemoveConnection`, `RemoveConnectionAndCount`, `DeleteByCharacter`, **and**
`Store.Delete` (session delete) — the last cascades to all the session's
`session_connections` rows via `session_connections.session_id … ON DELETE
CASCADE` (`internal/store/migrations/000001_baseline.up.sql:228`) and is on the
mainline self-quit (`internal/grpc/server.go:513`) and admin-boot
(`internal/grpc/server.go:577`) paths.

The key observation for class (B): **a removed connection never dispatches
again** (its transport is gone), so a cache entry left behind for it can never be
*served* to a live dispatch. A missed removal-eviction is therefore a bounded
memory leak, **not** a stale-serve correctness bug. This is reinforced
structurally: `HandleCommand` gates every dispatch through
`auth.ValidateSessionOwnership` (`internal/auth/session_ownership.go:34-89`),
which `sessions.Get`s the session and fails closed with `SESSION_NOT_FOUND`
*before* `maybeRedirectForFocus` runs (`internal/grpc/server.go:379-410`) — so
once a session is deleted, none of its connections can reach the cache at all. This lets the design require
*correctness* eviction on exactly one method (`UpdateSessionConnection`) and treat
every create/remove path as best-effort eviction backstopped by an LRU size cap
(§ Architecture). This is the central correction from design-review round 1.

## Goals

- **G1.** `pose` / `say` / `ooc` / `emit` dispatch performs **no** synchronous DB
  round-trip for focus on a cache hit.
- **G2 (correctness).** No stale focus kind is ever served to a **live** connection
  after that connection's focus has been changed and committed. Guaranteed by
  invalidating on `UpdateSessionConnection` (class A) plus a generation-guarded
  populate that closes the cache-aside repopulation race (§ Correctness). This is
  INV-SCENE-69.
- **G3 (fail-closed).** Fail-closed focus semantics (INV-SCENE-67) are preserved: a
  genuine focus-read failure still aborts dispatch and MUST NOT be masked by the
  cache.
- **G4 (bounded memory).** Cache memory is bounded regardless of whether every
  removal path evicts, via an LRU size cap. Removal paths evict best-effort for
  prompt reclaim.
- **G5.** The cache and its invalidation are unit-testable in isolation, without
  Postgres.

## Non-goals

- **N1.** Multi-core-process / horizontally-scaled core. Single-core-process is a
  **documented invariant**
  (`docs/superpowers/specs/2026-05-30-session-liveness-and-gateway-survival-design.md:67-69`
  → `docs/plans/2026-04-07-cursor-lock-finding-1-closure.md`). This design assumes
  it. Cross-replica cache invalidation is reserved for the future `INV-CLUSTER`
  scope and is explicitly out of scope.
- **N2.** Caching any `Connection` field other than the derived `FocusKind`.
- **N3.** Changing the focus write paths, the `FocusReader` contract, or the
  fail-closed dispatch behavior. This is a read-path optimization only.
- **N4.** A general-purpose `session.Store` read cache. Only the focus projection
  is cached.

## Design chosen — caching `session.Store` decorator (`FocusKind`-scoped)

A `ConnectionFocusCache` shared by two collaborators, constructed once at
CoreServer setup:

1. A `cachingSessionStore` decorator (implements `session.Store`) that evicts on
   focus-affecting store methods, then delegates. Eviction on
   `UpdateSessionConnection` is correctness-required (class A); eviction on the
   create/remove methods (class B) is best-effort reclaim.
2. A cache-first `FocusReader` that reads through the cache (miss → underlying
   `GetConnection` → derive kind → generation-guarded populate).

Both hold the same `*ConnectionFocusCache` pointer — the only coupling; no
cross-package eviction wiring.

Two alternatives were considered and rejected (grounded):

- **Design B — reuse `SessionStreamRegistry`.** Rejected: its per-connection entry
  exists only while the live Subscribe transport is open (`server.go:896`), is not
  seeded with focus at registration, and leaves a dispatch-without-live-Subscribe
  gap needing a DB fallback — so it neither cleanly eliminates the read nor keeps
  the invariant in the right layer.
- **Design C — reuse the already-loaded session `Info`.** Rejected:
  `Info.PresentingFocus` is per-session and D9-gated
  (`set_connection_focus.go:97`), not the per-connection `Connection.FocusKey`
  (INV-SCENE-15) redirect needs; substituting it reintroduces the routing-bug class
  INV-SCENE-67 just closed.

## Architecture

### `ConnectionFocusCache`

Owns two maps under one `sync.Mutex` (a generation guard needs a full `Lock` on
populate, so a plain `Mutex` is simpler than `RWMutex` and the critical sections
are tiny), plus an LRU eviction list:

```text
entries  map[ulid.ULID]focusEntry     // focusEntry{ kind session.FocusKind }
epoch    map[ulid.ULID]uint64         // bumped on every Evict(connID)
lru      *list.List                   // recency; cap = configurable (default e.g. 50k)
```

Methods:

- `Peek(connID) (kind, hit bool)` — cache read.
- `beginLoad(connID) uint64` — returns the current generation counter; captured by
  the reader *before* it does the DB read.
- `commitLoad(connID, kind, g uint64)` — stores `kind` **only if** the generation
  counter is unchanged since `beginLoad` (no eviction happened in the window);
  otherwise drops the value. Applies the size cap when inserting.
- `Evict(connID)` — invalidates the entry and bumps the generation counter.

**Generation-guard implementation choice (plan decides).** The guard MAY be
per-key (an `epoch map[connID]uint64` bumped only on that key's `Evict`) or a
**single global counter** bumped on every `Evict`. The global counter is simpler
and strictly more conservative — it drops a populate whenever *any* key was
evicted during the load window, never fewer — at the cost of rare
false-invalidations under eviction churn (negligible at human-driven
focus-change rates). Both close the race identically; the plan selects one.
Likewise the size cap MAY be access-order (LRU) or insertion-order (FIFO) — a
backstop against leaked entries, not a correctness mechanism (§ G4), so precision
is not required.

### `cachingSessionStore` (decorator over `session.Store`)

- `UpdateSessionConnection(...)`: **delegate first; on success, `cache.Evict(connID)`**
  (invalidate-after-successful-write — a rolled-back write leaves the cache
  untouched). Correctness-required (class A).
- `AddConnection`, `RemoveConnection`, `RemoveConnectionAndCount`: best-effort
  `Evict(connID)` after delegating.
- `DeleteByCharacter`, `Delete` (session-keyed / char-keyed, cascade removals):
  best-effort — enumerate the affected connections via `ListConnectionsBySession`
  before delegating and evict each, OR rely on the LRU cap as backstop when
  enumeration is not worth a pre-read. Because these are class (B) removals, a miss
  is bounded memory, not stale-serve (G4).

### `cachingFocusReader` (implements `FocusReader`)

Holds the `*ConnectionFocusCache` and the underlying `connectionGetter`.

## Read path (hot)

```text
ConnectionFocusKind(connID):
  if kind, hit := cache.Peek(connID); hit { return kind, nil }          // no DB
  g := cache.beginLoad(connID)
  conn, err := store.GetConnection(connID)
  switch {
    err == nil:                     kind := deriveKind(conn); cache.commitLoad(connID, kind, g); return kind, nil
    code(err) == CONNECTION_NOT_FOUND: cache.commitLoad(connID, "" /*grid*/, g); return "", nil   // resolved no-focus
    default:                        return "", err        // genuine failure: DO NOT cache → dispatch fails closed (INV-SCENE-67)
  }
```

## Correctness

### The generation guard closes the cache-aside repopulation race

Design-review round 1 identified the classic cache-aside hazard: with a plain
(non-locking) `GetConnection`, a reader can miss, read the **pre-write** value,
and populate the cache **after** an eviction fired — leaving a stale entry that
survives until the next focus change. Evict-ordering alone (before/after the
write) does not close it.

The generation guard closes it: the reader captures `epoch[connID]` *before* its
DB read (`beginLoad`) and stores the result *only if the epoch is unchanged*
(`commitLoad`). Any `Evict` between capture and store bumps the epoch, so a
populate carrying a possibly-stale value is dropped. Combined with
invalidate-after-successful-write on `UpdateSessionConnection`, the guarantee is:

> Every committed focus change on a live connection bumps that connection's epoch;
> any concurrent read that observed the pre-change value fails its epoch check and
> does not cache it. Therefore no stale focus kind is ever cached for, and served
> to, a live connection after its focus has been committed. (INV-SCENE-69.)

The reader still *returns* whatever value it read for its own in-flight dispatch;
a dispatch that races a concurrent focus change may observe either the pre- or
post-change focus, which is legitimate (the two operations are concurrent). The
invariant governs *future* dispatches, which read fresh.

### Fail-closed preservation (INV-SCENE-67)

`commitLoad` runs **only** on a successful `GetConnection` (including the
`CONNECTION_NOT_FOUND` → grid mapping, a genuine resolved no-focus state per
`focus_reader.go:45-47`). A genuine read error (any non-`CONNECTION_NOT_FOUND`
oops code) propagates unchanged and is never cached, so the dispatcher fails
closed exactly as today.

### Grid vs. gone

`FocusKind("")` means grid focus; a removed connection also resolves to `""` via
`CONNECTION_NOT_FOUND`. Both are safe to cache as grid: a removed connection does
not dispatch, and ULIDs are never reused, so no stale entry can be misattributed
to a new connection.

## Invariant

- **INV-SCENE-69 (registered; `binding: pending`):** *No stale connection focus kind is served on the
  dispatch hot path: after any committed change to a live connection's
  `Connection.FocusKey`, the next focus read for that connection observes the new
  value. Enforced by (a) eviction on every `session.Store.UpdateSessionConnection`
  — the sole path that changes a live connection's focus — and (b) a
  generation-guarded populate that discards any read whose value predates a
  concurrent eviction.*
  - **Scope rationale:** filed under `INV-SCENE` as the direct sibling of the
    focus-read fail-closed invariant **INV-SCENE-67** (`invariants.yaml:3339`),
    whose correctness this optimization must preserve. (INV-SCENE-68 is unrelated —
    scene-info ABAC read gating.) If the decorator lands purely in session/store
    files, an `INV-SESSION`-scoped id would be the alternative — confirmed against
    the declared boundary during implementation. INV-SCENE-69 is the next free
    scene slot and is registered in `invariants.yaml` (`binding: pending`).
  - Ships `binding: pending`; bound once a test asserts both clauses (eviction on
    `UpdateSessionConnection` **and** the generation-guard drop).
  - Note: bounded-memory reclaim (G4) is an **operational** property, not part of
    this correctness invariant.

## Testing

- **Unit — `ConnectionFocusCache`:** `Peek`/`beginLoad`/`commitLoad`/`Evict`; the
  **generation-guard race**: `beginLoad` → `Evict` (concurrent) → `commitLoad`
  drops the value (assert no stale entry remains); LRU cap evicts least-recent when
  over cap. Run under `-race`.
- **Unit — `cachingFocusReader`:** hit serves without touching a mock store; miss
  populates; `CONNECTION_NOT_FOUND` → grid, cached; genuine read error → NOT
  cached, error propagated (fail-closed).
- **Unit — `cachingSessionStore`:** `UpdateSessionConnection` evicts **after** a
  successful delegate and does **not** evict when the delegate errors; create/remove
  methods best-effort evict; `Delete`/`DeleteByCharacter` evict the enumerated
  connections (or document reliance on the LRU backstop).
- **Integration:** one observable end-to-end test — prime the cache with grid
  focus (a pre-join pose that routes to the location stream), then `scene join`
  (AutoFocusOnJoin writes `Connection.FocusKey` via `UpdateSessionConnection`),
  then a post-join pose must route to the scene IC stream, proving the committed
  focus change evicted the stale grid entry. Since all three class-A coordinator
  paths (`SetConnectionFocus`, `AutoFocusOnJoin`, `RestoreConnectionFocus`) funnel
  through the same `UpdateSessionConnection` chokepoint — and the decorator unit
  test proves that chokepoint evicts regardless of caller — one path exercised
  end-to-end plus the caller-independent unit proof is complete; per-path
  integration duplication is unnecessary.
- **Invariant binding:** the test asserting both INV-SCENE-69 clauses carries
  `// Verifies: INV-SCENE-69`.

## Observability

Emit `focus_cache_hit` / `focus_cache_miss` counters (mirroring the existing
dispatch metric pattern) so effectiveness is measurable; a hit-rate near 1 on the
ambient verbs confirms the regression is closed. Optionally emit the current cache
size for LRU-cap tuning.

## Open questions for the plan

1. Decorator placement: `internal/command` vs `internal/store` (import-cycle check
   between `session.Store`, the cache type, and the `FocusReader` seam).
2. Invariant scope: `INV-SCENE-69` (registered) vs an `INV-SESSION`-scoped id —
   confirm by the declared boundary of the files that carry the guarantee.
3. LRU cap default and whether it is configurable; whether `Delete`/`DeleteByCharacter`
   do the `ListConnectionsBySession` pre-read or lean on the LRU backstop.
4. **Hot-path lock contention (design-review round 2).** `Peek` takes the full
   `sync.Mutex` because the LRU recency update mutates shared state, so every
   `pose`/`say`/`ooc`/`emit` dispatch serializes on one mutex across all
   connections. G1's premise is *removing* a hot-path bottleneck — benchmark under
   realistic concurrent-dispatch load, and if the single mutex serializes the hot
   path, consider an approximate/clock (second-chance) LRU that reads under an
   `RWMutex.RLock`, or sharded locks.
5. **`DeleteByCharacter` key mismatch (round 2).** It is keyed by `characterID`,
   not `sessionID`, so the pre-read path needs a `FindByCharacter` →
   `ListConnectionsBySession` hop; the plan decides pre-read vs LRU backstop.
6. **Residual risk note (round 2, pre-existing — not introduced here).**
   `ValidateSessionOwnership` validates *session* ownership, not connection-ID
   ownership (it never checks `req.ConnectionId` against the session's connection
   list). A client supplying a foreign/stale `connection_id` under its own valid
   session could hit a not-yet-evicted, not-yet-LRU-capped stale entry rather than
   an immediate `CONNECTION_NOT_FOUND`→grid. This is not new exposure (connection
   IDs are unvalidated today, and the redirected target command re-authorizes
   against the caller's real character/session), but the plan's risk section
   SHOULD note it rather than leave it silent.
<!-- adr-capture: sha256=5b2df6b953f13a9d; session=cli; ts=2026-07-08T01:20:00Z; adrs=holomush-sfxte,holomush-cfqxp,holomush-sz877 -->
