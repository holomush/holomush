<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# F1 deep-dive — WHY event sourcing isn't in place (archaeology)

Prompted by the reviewer's (user's) challenge: F1 is bigger than "correct the docs" — the real question is *why* the architecture diverges from its stated principle, and whether that was a decision or drift. This note records the archaeology that answers it.

## What the evidence shows

**World state (locations, exits, characters, objects) was always CRUD — event sourcing for the world model was never built.**

- `internal/store/postgres.go:33-34` — `PostgresEventStore` doc comment: *"Event append/replay is handled by the JetStream event bus (F7+)."* The PG store today holds only `holomush_system_info`/game_id — not an event table.
- `cmd/holomush/sub_grpc.go:768-770` — `busEventAppender`: *"F7 removes the PG events table and the EventWriter; all host-engine Append calls go straight to the bus."*
- `internal/world/event_store_adapter.go:31-47` — the world's `EventEmitter.Emit` builds a `core.NewEvent(...)` and calls `store.Append(...)`. This is an **append-only notification/audit log**, one-directional. There is no read-back that reconstructs a location or character from events.
- The JetStream design doc (`docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md`) lists the F-series deletions — `EventWriter`, `cursor_lock.go`, `replay.go`, `internal/grpc/replay.go` (§150, §599, §1451). Every one of those lived in the **gRPC `Subscribe` client-catch-up path** (§91: "simplify the gRPC Subscribe handler, replay…"), i.e. *event-log replay for reconnecting clients*, not world-state rebuild.

**No world-state rebuild path has ever existed.** `git log -S EventStore` and `git log --grep "rebuild world|derive state|projection"` across all history surface only: the event-*log* store (MemoryEventStore/EventWriter/replay, later moved to `coretest`), the w9ml actor-ULID work, and the F7 JetStream cutover. **Not one commit reconstructs world state by replaying events.** The audit projection (`internal/eventbus/audit/projection.go`) writes `events_audit` — it never writes world tables.

## What this means (the reframe)

There are three candidate explanations; the evidence picks the third:

1. ~~Event sourcing was built for world state, then regressed by careless (AI-assisted) coding.~~ **Not supported** — no commit ever built world-state-from-events; the replay that was removed was client-catch-up, not state derivation.
2. ~~World state CRUD was a considered decision.~~ **Not supported** — there is **no ADR** in 197 `docs/adr/` files recording a CRUD-vs-event-sourcing choice for the world model.
3. **The stated architectural *principle* ("event sourcing — current state is derived from event replay", CLAUDE.md/architecture.md/coding-standards.md/index.mdx) was never implemented for the world model, and the gap was never surfaced or decided.** The event *bus/log* half (append, JetStream replay for clients, audit) is real and well-built; the event-*sourcing-of-state* half was asserted as a foundation and quietly never built. This is **architectural drift by default**, not a decision — exactly the reviewer's instinct, made precise.

The user's "lazy/poor AI-assisted coding" hypothesis is directionally right in spirit (no one reconciled principle to reality) but the specific mechanism is *"a foundational principle stated in the design docs was never realized for the world model, and no decision ever recorded the divergence"* — not a build-then-break regression. Intent (was it laziness, an aspirational design doc, or a deliberate-but-unrecorded pragmatic call?) cannot be proven from git; the ADR investigation should establish it.

## Why it's not cosmetic (severity)

The CRUD-not-event-sourced reality is the **root cause** of two other findings:

- **D1-M2 (dual-write non-atomicity):** because state is the source of truth and events are a *post-commit notification*, a NATS blip loses the notification while the DB change persists (`move_succeeded:true`). Under true event sourcing the event *is* the write, so this class can't occur.
- **D1-M12 (last-write-wins, no version guard):** CRUD writes have no event-ordered concurrency control; under an event-sourced model the append sequence is the ordering. This is the concurrency-corruption risk the §7 blind-spot follow-up (I11) targets.

So "which architecture is real" is not a doc question — it determines whether those correctness gaps are incidental bugs (fixable in CRUD with optimistic locking + transactional outbox) or symptoms of a missing foundation (fixable by building the foundation). **That decision is the finding.**

## The reframed action (replaces "correct the docs")

**Investigate + decide, then reconcile — an ADR-driving investigation, not a doc patch:**

1. Establish intent: was world-state event sourcing ever meant to be real (interview/roadmap archaeology), or was "event sourcing" always shorthand for "event-driven with an audit log"?
2. Decide (ADR) between:
   - **(A) Build it** — introduce a real projection/rebuild path (or a transactional outbox + event-first writes) if replayability, auditable state reconstruction, or the two-replica correctness guarantees are actually wanted. Larger effort; fixes M2 + M12 at the root.
   - **(B) Formally adopt CRUD-canonical** — event log is notification/audit only; add optimistic-concurrency + a transactional outbox for M2/M12; and **downgrade the "event sourcing" principle everywhere** (6 doc sites incl. the public marketing page) to "event-driven with an append-only audit log."
3. Whichever is chosen, capture the ADR (the artifact whose absence *is* the drift) and correct the docs to match.

Recommended severity: **High (architecture integrity)** — not because docs are wrong, but because a foundational principle is unimplemented and undecided, and that gap is the root of real correctness findings.
