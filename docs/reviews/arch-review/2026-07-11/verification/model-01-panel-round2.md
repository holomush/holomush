<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->

# MODEL-01 panel — Round 2: stress-test of the B-leaning choice (verbatim + ratification trail)

Round 2 of the [MODEL-01 model panel](model-01-panel-opinions.md) (same three panelists, 2026-07-11). The human decider, leaning toward the Fable-B recommendation (CRUD-canonical + optimistic concurrency + full-taxonomy transactional outbox), directed the panel to work the choice over — whys, why-nots, gaps, strengths, additions, removals — and converge on a consensus one-pager all panelists and the orchestrator could sign. Each panelist answered a fixed 7-section critique brief (WHY RIGHT / WHY NOT / MISSING / GREAT / ADD / TAKE AWAY / CONSENSUS TEST); the Fable panelist was resumed with its Round-1 research context and explicitly instructed to red-team its own proposal, confronting six named underspecifications (payload shape, cross-entity ordering, coverage-invariant mechanics, exit versioning, delete cascades, conflict UX).

Outcome: all three panelists conditionally endorsed; their consensus conditions were compatible and were synthesized into [`model-01-consensus-onepager.md`](model-01-consensus-onepager.md), which was then ratified in two explicit AGREE/OBJECT passes (trail at the end of this document).

## Round-2 critique — Antigravity (Gemini 3.1 Pro High)

1. **WHY THIS IS RIGHT**
If the goal is to optimize for current-state read/write latency and read-your-writes consistency while avoiding the operational tax of projection rebuilds, B-strengthened is the pragmatic choice. It correctly isolates the primary value of event sourcing for this specific domain: the *event feed* is the product needed for extensibility (NPC AI, analytics, search), not the *derivation* of the canonical state itself. By elevating the outbox to a first-class, invariant-tested contract, it buys 80% of Event Sourcing's extensibility value for 20% of the architectural complexity.

2. **WHY NOT / RESIDUAL RISK**
We permanently foreclose zero-cost world branching, true time-travel moderation, and the elegance of a unified write model. The sharpest residual risk is temporal degradation: B treats the event stream as temporally secondary. The database commits, the user sees the UI change immediately, but the event sits in the outbox. If the relay lags or crashes, the "extensible" downstream platform operates on stale data, creating visible race conditions between human UX and system reactions (e.g., a player enters a room, but the NPC AI doesn't notice for 5 seconds).

3. **WHAT'S MISSING**

- **Graph Transaction Ordering**: A MUSH move touches three entities (character, old location, new location). If the `InTransaction` outbox insert creates multiple rows, what guarantees they are processed and appended to JetStream atomically or in causal order? The proposal is silent. **Fix:** Outbox rows must share a `transaction_id` and an intra-transaction `sequence`.
- **Relay Mechanism & Latency**: The proposal states the "relay publishes" but omits *how*. A simple polling loop adds unacceptable latency for interactive MUSH mechanics. **Fix:** Define the mechanism. The relay must use Postgres `LISTEN/NOTIFY` (or WAL tailing) for immediate wakeup, backed by a periodic sweep poll.
- **Outbox Lifecycle**: Does the outbox grow forever? **Fix:** The relay must `DELETE` outbox rows (or mark them `published_at`) within the same JetStream ACK loop, and a background mechanism must prune completed rows to prevent table bloat.

4. **WHAT'S GREAT**
The invariant-enforced coverage. The mandate that outbox emission is not a "best effort" developer convention, but a mathematically verified property of the `world.Service` via the registered meta-test. Do not water this down. Furthermore, the strict `version` column CAS with `RETURNING` must be protected—it is the only way to deterministically distinguish a concurrency conflict from a missing entity without race conditions.

5. **ADD**

- **Causal Transaction Grouping**: Add `tx_id UUID` and `tx_seq INT` to the outbox table and the resulting JetStream headers. Downstream consumers (like scene builders) must know that a character exiting Room A and entering Room B happened in the exact same database transaction, otherwise the character temporarily exists nowhere or in two places in the feed.
- **Relay Wakeup Contract**: Implement a Postgres `LISTEN/NOTIFY` trigger (`AFTER INSERT` on the outbox table) to wake the relay instantly, ensuring sub-10ms event publish latency.
- **Schema Enforcement**: The ~15-20 event types must be formalized in a strict schema registry (e.g., Protobuf or JSONSchema) and validated in CI. If the outbox is the platform's public contract, it cannot be unstructured, evolving JSON.

6. **TAKE AWAY**

- Drop the "one automatic re-read-and-retry permitted in `entity_mutator` single-field setters." This is premature optimization. Surface *all* `WORLD_CONCURRENT_EDIT` errors to the caller first. Build automatic merge-retry logic later only if UX telemetry proves field-disjoint collisions are actively harming the player experience.
- Defer the mechanical rollout of the full ~15-20 type taxonomy (Phase 5c). Route only `MoveCharacter` end-to-end through the outbox, let it soak in production, and observe the relay metrics before investing in the remaining payload mappers.

7. **CONSENSUS TEST**

I can endorse a consensus one-pager for Fable-B IF AND ONLY IF:

1. **Graph Ordering:** Outbox events are strictly grouped by a shared database transaction ID and sequence to solve the cross-entity ordering problem for downstream consumers.
2. **Latency Guarantee:** The relay mechanism is explicitly defined to support immediate wakeup (e.g., `LISTEN/NOTIFY`) rather than blind polling.
3. **Strict Surfacing:** In-process automatic retries are dropped from the first slice in favor of strict, universal conflict surfacing.

## Round-2 critique — Codex (gpt-5.6-sol, reasoning max)

1. **WHY THIS IS RIGHT** — B fits a young, mutable spatial graph whose operational truth is its current PostgreSQL state. It preserves straightforward reads, recovery, migrations, and repairs while closing M12 through explicit concurrency control and M2 through atomic publication intent. The strengthened feed captures the platform’s real extensibility need—reliable observation of every world change—without making historical reducers part of world availability. Narrative domains remain replayable where history is intrinsically valuable. Given the existing write chokepoint and transaction seam, this is a coherent architecture, not a compromise patch.

2. **WHY NOT / RESIDUAL RISK** — B permanently forfeits proof that retained history derives current world state; any later event-sourcing conversion begins with a genesis snapshot and cannot recover earlier history. More immediately, it retains two descriptions of each mutation: SQL effects and event semantics. “Exactly one event” detects omission, but not an incorrect type, incomplete payload, or false ordering. The proposal particularly underestimates composite operations: ULIDs, insertion order, and concurrent relays do not establish commit-ordered feed semantics.

3. **WHAT'S MISSING** —

- **Aggregate boundaries:** Define which versions a move, containment change, exit change, cascade, and property write advances. Every property mutation MUST advance its owning aggregate version. Multi-aggregate commands MUST validate all expected versions and lock/update aggregates in deterministic ID order.
- **Safe retry algorithm:** A reread alone makes same-field edits last-writer-wins. Retain the original field value; retry only if that field remains unchanged, and rerun authorization plus state-dependent validation before the single retry.
- **Composite feed semantics:** Emit one semantic command envelope containing all affected aggregates and their before/after versions. A move must not appear as unrelated partial changes.
- **Real ordering:** Add a transactional monotonic `feed_position`, allocated through a locked world-feed counter. Publish strictly by position through one elected relay. A poison event MUST halt and alert rather than be skipped.
- **CAS diagnosis:** Zero `RETURNING` rows cannot itself distinguish conflict from absence. Specify the locked existence/version read and how concurrent deletion is classified.
- **Envelope contract:** Define `event_id`, position, kind, schema version, actor, command/causation/correlation IDs, primary aggregate, affected aggregates, and payload. Deletes require useful tombstones. Define compatibility, redaction, and subject-access rules.
- **Bootstrap and retention:** OPS-02 means consumers cannot assume infinite replay. Specify consistent DB snapshot plus captured feed watermark, followed by consumption above that watermark. Define a feed epoch/reset procedure for DB restores and world-changing backfills.
- **Relay lifecycle:** Specify leasing, retry/backoff, PubAck-before-marking, stuck-row recovery, lag alerts, cleanup, and durable consumer deduplication beyond JetStream’s finite duplicate window.
- **Writer bypass:** Prevent production repository writes outside the world mutation boundary. Admin repairs and backfills must either emit normal mutations or advance the feed epoch and force projection rebootstrap.

4. **WHAT'S GREAT** —

- State mutation and outbox intent share one PostgreSQL transaction.
- Typed conflicts remain user-visible; automatic retry stays narrowly bounded.
- The complete taxonomy lands before mechanical emission rollout.
- The invariant registry, meta-test, and two-replica regression suite are mandatory architecture gates.
- Projections remain non-authoritative, and documentation loses the inaccurate event-sourcing claim immediately.
- `MoveCharacter` is the first full vertical slice through the proven failure window.

5. **ADD** —

- `INV-WORLD-ATOMIC-FEED`: every successful state-changing world command commits exactly one semantic envelope atomically; failed and no-op commands commit none.
- `INV-WORLD-DELTA-PARITY`: the envelope’s affected-aggregate manifest exactly matches the committed canonical before/after delta.
- `INV-WORLD-FEED-ORDER`: positions are unique, gap-free for committed commands, and published monotonically; composite commands occupy one position.
- `INV-WORLD-WRITER-BOUNDARY`: only the mutation transaction wrapper may invoke world repository writes.
- Make the wrapper enforce event cardinality at runtime; use the meta-test to enumerate write methods and their taxonomy mappings.
- Add fault-injection tests for every state/outbox commit boundary, relay crash before/after PubAck, duplicate delivery outside `DupeWindow`, dual relays, poison events, and broker downtime.
- Add race tests for every aggregate family, property writes, same-field versus disjoint retries, cascades, and multi-aggregate deadlock avoidance.
- Add contract tests proving each event’s payload and affected-version manifest, plus a snapshot-and-tail bootstrap test with concurrent writes.

6. **TAKE AWAY** — Replace “exactly one event per write method” with “exactly one envelope per successful externally visible command”; internal composition and cascades make method cardinality brittle. Do not build search, analytics, map, or NPC projections in Phase 5—ship only a reference idempotent consumer and bootstrap harness. Drop claims of indefinite forensic reconstruction unless retention explicitly guarantees it.

7. **CONSENSUS TEST** — I can endorse B if the one-pager requires: explicit aggregate/version-vector rules; compare-before-retry semantics; atomic composite envelopes; transactional global positions with ordered relay behavior; semantic delta-parity enforcement, not merely event presence; snapshot-plus-watermark bootstrap under bounded retention; durable consumer deduplication; and a controlled policy for repairs, migrations, and restores. With those amendments, B is an honest, enforceable canonical-CRUD architecture rather than notification events with better plumbing.

## Round-2 critique — Fable (resumed panelist, self-red-team)

**1. WHY THIS IS RIGHT**

The write surface is a perfect chokepoint (~16 methods, zero external writers, ABAC pre-write, a working `InTransaction` seam), which is exactly the topology where CAS + outbox is strong and projection machinery is redundant. The DB transaction is already the atomicity boundary for graph-shaped writes — the cascade in `DeleteLocation` proves it — so B gets cross-entity consistency for free where A must invent a single totally-ordered stream to recover it. It keeps read-your-writes on N replicas without wait-for-seq machinery, keeps write-semantics churn cheap pre-1.0, and keeps the proven DB-read recovery. The strengthening matters: the complete feed, not the rows, is the extensibility contract.

**2. WHY NOT / RESIDUAL RISK**

Codex's sharpest attack stands: an outbox only delivers events you remembered to create, and this repo *empirically* rots that way. A test-only invariant is too weak against it (tests were also the thing nobody wrote). Permanently foreclosed: byte-derivable canonical state; feed-based reconstruction is forensic best-effort, and without a defined origin the feed can never even claim completeness. Underestimated: the relay is a new quasi-singleton with lag, backpressure, and DLQ posture — operational surface the proposal hand-waved.

**3. WHAT'S MISSING** (gap → fix)

- **Payload shape unspecified.** Fix: intent-level deltas, not row snapshots. `entity_mutator` setters and `MoveCharacter` already know intent at the chokepoint (`MovePayload` precedent exists); whole-struct paths (`UpdateLocation`) emit `location.updated` with a changed-fields map diffed inside the write transaction against the version-V read. Payloads carry **new values only** (no `old`) — this also answers Codex's erasure burden: history-privacy reduces to audit retention, which OPS-02 already bounds. Schemas live in the taxonomy spec, versioned via the existing `App-Schema-Version` header; the spec is explicitly written as **ARCH-04's Phase-7 input** so the collapsed event model adopts these schemas rather than re-inventing them.
- **Cross-entity ordering unanswered (Antigravity).** Fix: `MoveCharacter` emits **one** event — `character.moved {character_id, from, to, version}` — not three; only the character row mutates (verified), and "notify the old room" is consumer-side fan-out, not authoritative events. Feed ordering: outbox rows get a monotonic `BIGSERIAL` sequence; a single relay (per game) publishes in sequence order and stamps the outbox seq in a header. Consumers filter by domain subject but can totally order across subjects by that seq. This is B's cheap answer to the graph-shaped-write concern A needed a global stream for.
- **Coverage invariant mechanics vague.** Fix below in ADD — it must be compile-time, not test-time.
- **Exit versioning questioned.** Verified: exits have real writers beyond world code — `internal/bootstrap/setting.go`, the building command path (`internal/command/types.go`), and the plugin `world_write` hostfunc. Two replicas' builders can race them. Exits stay versioned; they simply get no auto-retry (no `entity_mutator` path).
- **Delete semantics undefined.** Fix: one tombstone per aggregate — `location.deleted {id, version}` — the property cascade is subsumed, documented as "deletion subsumes child properties"; the tombstone is the subject's final event and consumers drop everything keyed to the entity.
- **Conflict UX undefined.** Fix: creates can't conflict (fresh IDs); update conflicts map at the command layer — telnet: "Someone else changed <entity> while you were editing; your change was not applied — re-run to apply against the latest"; web: typed `WORLD_CONCURRENT_EDIT` over ConnectRPC → retry affordance. A mapping table ships in the taxonomy spec. Auto-retry in `entity_mutator` MUST re-run `checkAccess` after re-read.

**4. WHAT'S GREAT** (do not water down)

The completeness contract as first-class architecture; the resilience suite as permanent regression gate; deleting the post-commit emit path outright (no "legacy emitter" coexistence); `RETURNING`-based conflict/not-found discrimination; projections explicitly disposable, never load-bearing; docs downgraded in the same phase as the mechanism lands.

**5. ADD**

1. **Make forgetting non-compilable.** Restructure the guarded write seam so a repo mutation *requires* an `OutboxEvent` argument inside the transaction (`mutate(ctx, entity, expectedVersion, event)`); an event-less write is a type error, not a test failure. This is the real answer to Codex's structural-rot attack.
2. **Census meta-test as backstop:** table-driven, enumerates the `Mutator`/write interface by reflection; each method executes and must yield exactly one outbox row of its declared type — a new write method fails until registered in the taxonomy. Plus a lint fence: no `UPDATE locations|characters|objects|exits` SQL outside `internal/world/postgres`.
3. **Genesis snapshot events at cutover:** one-time emission of `*.created`-shaped snapshot events for every existing entity (world is small), giving the feed a defined origin so "reconstructable from the feed" is true from day one, not "from whenever we shipped."
4. **Stamp post-write `version` in every event** — consumers get a per-entity ordering/idempotency key.
5. **Relay operations:** lag metric, alerting, DLQ posture mirroring the audit projection's (`TestProjectionNeverDropsWhenDLQPublishFails` precedent).

**6. TAKE AWAY**

Drop the speculative projection catalogue (search/world-map/NPC-perception) from the proposal — zero projections ship in Phase 5; the feed contract is the deliverable. Narrow auto-retry to `SetDescription`/`SetName` only, or defer it entirely from slice 1 and surface all conflicts first — retry semantics (reauthorization, merge rules) shouldn't complicate the guard's landing. Defer property-write taxonomy granularity (one `*.property_changed` type suffices initially; don't mint per-property types).

**7. CONSENSUS TEST**

I endorse given: (1) the compile-time write-requires-event seam + census backstop (ADD 1–2); (2) outbox-sequence total ordering with single-relay publish order documented as the feed's ordering contract; (3) intent-level, new-values-only payload schemas in a taxonomy spec explicitly designated the ARCH-04 input; (4) genesis snapshot events at cutover; (5) tombstone-subsumes-cascade delete semantics; (6) the conflict-UX mapping table. These are amendments, not redesigns — with them, B-strengthened is the strongest honest architecture for this platform.

## Ratification trail

**Pass 1 (draft one-pager):** Antigravity — AGREE. Fable — AGREE, two non-blocking notes (record the locked feed counter as a deliberate throughput ceiling; move relay lag/halt alerting into the relay slice). Codex — OBJECT, two blocking edits (version-predicated deletes with the same deterministic zero-row classification; migrations/backfills included alongside admin repairs in the writer-boundary/epoch policy).

**Amendments applied:** all four (Codex's two blocking edits verbatim; Fable's two notes folded in).

**Pass 2 (amended one-pager):** Antigravity — AGREE. Codex — AGREE. Fable — AGREE. Orchestrator — concurs. **Unanimous, 2026-07-11.**
