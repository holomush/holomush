<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Single Combined SessionConnectionMutator Under One Store-Lock Acquisition

**Date:** 2026-05-21
**Status:** Accepted
**Decision:** holomush-8new
**Deciders:** HoloMUSH Contributors

## Context

Phase 5 operations mutate two fields together:

- `session.Info.PresentingFocus` (per-session) — the reconnect-restoration target.
- `session.Connection.FocusKey` (per-connection) — the per-conn focus pointer added by Phase 5.

The existing `FocusMutator` (sentinel-protected type at `internal/session/session.go:91-132`) covers only `Info.FocusMemberships` + `Info.PresentingFocus`; its callback signature cannot reach `Connection.FocusKey`.

Three rounds of design review surfaced concurrency hazards in early formulations:

- **Round 2** flagged a read-then-write race: validation against `FocusMemberships` was done *outside* the store's lock, so a concurrent `LeaveFocus` could remove membership between the validation step and the focus write.
- **Round 3** flagged a torn-state risk: when two separate mutator invocations were chained (`UpdateConnection` then `UpdateFocusMemberships`), an external observer could see `Connection.FocusKey` updated while `Info.PresentingFocus` was still stale.

Three structural responses were considered:

1. **Coordinator-level mutex** wrapping two existing mutator calls. Would introduce a new lock primitive alongside the existing store-side per-session lock, doubling the locking surface.
2. **Single combined mutator** whose callback receives both `Info` and `Connection` snapshots and returns updates to both under one Store-lock acquisition. Reuses the existing locking infrastructure; sentinel pattern parallels `FocusMutator`.
3. **Push atomicity down to the database layer via `SERIALIZABLE` transactions** and accept higher abort rates and weaker MemStore semantics.

## Decision

Introduce `SessionConnectionMutator` (with the same sentinel-construction pattern as `FocusMutator`) whose callback signature is:

```go
func(info Info, conn Connection) (nextInfo Info, nextConn Connection, err error)
```

The `Store` interface gains `UpdateSessionConnection(ctx, sessionID, connectionID, mutator) error`, which runs the callback under one lock acquisition. **MemStore** reuses its existing store-wide `sync.RWMutex` (`internal/session/memstore.go:18`). **Postgres** opens a single transaction that locks the `sessions` row first via `FOR UPDATE`, then the `session_connections` row via `FOR UPDATE` (canonical order to prevent deadlock between concurrent `UpdateSessionConnection` calls on the same session for different connections — pinned by INV-P5-14's 50-iteration regression test).

No Coordinator-level lock is introduced. No database isolation-level escalation is needed. Both `MemStore` and Postgres impls narrow the write set to `{Info.PresentingFocus, Connection.FocusKey}` for parity (Postgres UPDATEs those two columns specifically; MemStore assigns those two fields by name rather than full-struct overwrite).

## Rationale

The existing `FocusMutator` (`internal/session/session.go:91-132`) already establishes the sentinel-protected mutator pattern. Mirroring that shape for `SessionConnectionMutator` keeps the substrate vocabulary internally consistent: I-6 server-authoritative mutation is enforced the same way for both per-session focus state (FocusMutator) and per-Connection focus state (SessionConnectionMutator) — only `internal/grpc/focus` can construct either.

The single-mutator-under-one-lock pattern is the minimum mechanism that closes both flaws round-2 and round-3 reviews surfaced: validation against `FocusMemberships` must be inside the lock (closes read-validate-write race), and `Connection.FocusKey` + `Info.PresentingFocus` writes must be inside the same lock (closes torn-state race). A single Store-side method satisfies both with no Coordinator-level locking infrastructure.

The Postgres canonical lock order (sessions row before session_connections row) follows from the simplest available rule: lock the row whose write happens conceptually "first" (PresentingFocus is the session-level decision that's downstream of FocusMembership existence; Connection.FocusKey is the per-conn application). A 50-iteration deadlock-detector regression test makes the discipline auditable.

## Alternatives Considered

**Coordinator-level mutex wrapping two existing mutator calls.** Use `FocusMutator` for `PresentingFocus` and a new `ConnectionMutator` for `Connection.FocusKey`, with a Coordinator-side mutex bracketing both. Rejected because (a) it introduces a new lock primitive alongside the existing store-side per-session lock — two locks to keep coherent under future evolution; (b) external observers calling `Get(sessionID)` between the two locked sections could see torn state, the very flaw the round-3 review flagged; (c) the Coordinator becomes a stateful object holding lock state, complicating its lifecycle.

**Push atomicity to the database via `SERIALIZABLE` transactions.** Postgres `SERIALIZABLE` isolation would prevent any phantom reads or write skew between the two-row mutation. Rejected because (a) `SERIALIZABLE` raises abort rates under concurrency and forces callers to implement retry loops, (b) MemStore would have to invent serialization-conflict semantics that don't naturally exist for an in-memory store, breaking MemStore↔Postgres parity, and (c) it solves a stronger problem than Phase 5 needs — Phase 5 needs two-field write atomicity, not full serializable scheduling.

## Consequences

**Positive:**

- Validation against `FocusMemberships` happens inside the locked callback, so the read-validate-write window is structurally closed. INV-P5-1 cannot be violated by interleaved reads.
- No external observer can see `Connection.FocusKey` updated while `Info.PresentingFocus` is stale, or vice versa. INV-P5-7 (operation-atomicity) holds at the type level.
- The Coordinator stays lock-free. I-6 (server-authoritative mutation) is preserved via the sentinel pattern — `SessionConnectionMutator` can only be constructed via `NewSessionConnectionMutator` and is only called from `internal/grpc/focus`.
- The canonical Postgres lock order (sessions row first, then session_connections row) is now a documented, test-pinned discipline. INV-P5-14's deadlock-detector regression test races 50×2 goroutines on different conns within one session and confirms no deadlock under the canonical order.

**Negative:**

- A third mutator type joins the substrate vocabulary alongside `FocusMutator`. Plan authors must pick the right one — `FocusMutator` for `FocusMemberships`/`PresentingFocus`-only mutations (existing `JoinFocus` / `LeaveFocus`), `SessionConnectionMutator` for the new combined Phase 5 operations. Choosing wrong produces a compile error (sentinel mismatch), so it is not a runtime hazard, but it is cognitive load.
- The narrow-write semantics (only `{PresentingFocus, FocusKey}` are persisted) is a contract carried by comment + parity between MemStore and Postgres, not by the type system. A future mutator that modifies `Info.LocationID` or `Connection.Streams` inside the callback would have its writes silently dropped. This is by design but is a latent footgun for future maintainers.

**Neutral:**

- The canonical Postgres lock order is now load-bearing for any future write path that touches both `sessions` and `session_connections`. New writers must acquire in the same order or risk deadlock.

## Source

- Spec: `docs/superpowers/specs/2026-05-21-scenes-phase-5-focus-model-and-multi-connection-visibility-design.md` §3 D7, §3 D11, §5.1.1, §10 INV-P5-7, INV-P5-14
- Plan: `docs/superpowers/plans/2026-05-21-scenes-phase-5-focus-model-and-multi-connection-visibility.md` Phase A (T3, T5, T6)
- Existing pattern: `FocusMutator` at `internal/session/session.go:91-132`; `UpdateFocusMemberships` at `internal/store/session_store.go:664-755`
