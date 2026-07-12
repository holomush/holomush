<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->

# MODEL-01 world-state decision — independent model panel (verbatim + synthesis)

Three independent AI models were given an identical brief on the MODEL-01 decision ([ADR holomush-i4784](../../../../adr/holomush-i4784-world-state-model-decision.md), issue #4784) at the Phase 4 plan 04-04 decision checkpoint (2026-07-11), before the human decider chose. This document preserves the brief's framing, each panelist's verbatim opinion, and the orchestrator's synthesis as durable decision inputs. The decision itself is recorded in the ADR, not here.

| Panelist | Model / configuration | Grounding |
| -------- | --------------------- | --------- |
| Antigravity | Gemini 3.1 Pro (High) via `agy --print` | Evidence documents embedded in the prompt (draft ADR, `f1-resilience-verdict.md`, `f1-eventsourcing-why.md`, `skeptic-d1-eventsourcing.md`) |
| Codex | `gpt-5.6-sol`, `model_reasoning_effort=max`, via `codex exec` (read-only sandbox in the repo) | Same embedded documents + optional repo verification |
| Fable | Fable 5 subagent (Claude Code) | Brief + repo research: CodeGraph index, probe, and direct reads of `internal/world/`, `internal/eventbus/` |

## Panel framing (verbatim, decider-directed)

The brief framed the decision with this directive, which overrides any lean inferable from the embedded documents:

> This is a TRUE open choice. The human decider has explicitly directed:
>
> 1. **Blast radius of change is NOT a driver.** Migration effort, implementation cost, "bounded inside v0.12," schedule fit, and how much code must change are NOT decision criteria. The team will pay what the right answer costs. Treat effort strictly as a planning footnote.
> 2. **The primary drivers are:** the desired FUTURE STATE of the architecture; the BEST SOLUTION for the problem space; EXTENSIBILITY; FLEXIBILITY; and the ABILITY TO EVOLVE the platform over years.
> 3. Some embedded documents score options on migration cost and schedule (e.g., a "Migration + effort class" table row, "bounded inside v0.12"). Consciously down-weight those rows — they were written under a different rubric than yours.

Each panelist answered the same seven questions: recommendation + confidence; why (problem-space grounded); 2–3 year end state; risks both ways; precise M12/M2 closure; Phase 5 first slice; and what the A/B framing misses. Hybrid/third paths were explicitly allowed.

## Opinion — Antigravity (Gemini 3.1 Pro High)

1. **RECOMMENDATION** — Option A — Real event sourcing for world state. High confidence.

2. **WHY** — A MUSH is not a standard B2B CRUD app; it is a collaborative, persistent timeline where history *is* the core product. If we optimize for the "now" (Option B), we are building a traditional web database. If we optimize for the timeline (Option A), we unlock the true future-state drivers of a roleplay platform.
   - **Extensibility:** Plugins (like NPC AI, complex economies, or weather systems) won't need to hook into monolithic Postgres transactions or poll for state changes. They simply subscribe to the event stream, replay from genesis to build their perfect bespoke read-models, and transition to live processing.
   - **Flexibility:** Schema evolution becomes trivial projection logic. Radically redesigning how `Locations` are stored won't require downtime or risky `ALTER TABLE` locks on production data; you simply write a V2 projector, replay the log, and point reads to the new table.
   - **Evolvability:** When the entire state is derivable from events, we structurally unlock massive product features: true time-travel for moderation disputes, cleanly "undoing" griefing, and allowing builders to fork the world stream to test a new city layout before merging it into the mainline timeline.
   - **Operational Reality:** Replicas no longer compete in a DB lock-contention bloodbath. The broker’s ordering becomes the absolute source of truth.

3. **END STATE** — In 2–3 years, the architecture is strictly segregated.
   - **Writes:** The core engine validates a command against an in-memory or fast Postgres projection, then appends the resulting event(s) to JetStream. The write is complete the millisecond the broker ACKs.
   - **Reads:** Reads never touch the write path. They hit highly-optimized, use-case-specific Postgres projections, spatial R-trees, or pure in-memory caches built strictly by tailing the broker.
   - **Recovery:** Booting a replica means loading the latest nightly snapshot and fast-forwarding JetStream to the present.
   - **Capabilities:** Time-travel debugging, exact scene reconstruction, offline analytics, and timeline branching are computationally cheap, natively supported features rather than expensive retrofits.

4. **RISKS** —
   - **Top risks of Option A:** Read-after-write eventual consistency (a client moves, the UI refreshes, the projection hasn't caught up, and they appear to be in the old room). It also creates severe tension with OPS-02; we must build robust snapshotting and compaction machinery because we can no longer simply age-out events.
   - **Top cost of rejecting Option B:** We pay a massive structural complexity tax—building projectors, snapshots, and rebuild tooling—for a system that could have achieved transactional integrity in a week with a Postgres version column and an outbox table. If time-travel and world-forking never materialize as product features, Option A was an expensive vanity architecture.

5. **M12/M2 CLOSURE** —
   - **M12 (Lost Update):** Option A does not magically fix M12 on its own, because validation still reads a projection that might be stale. We close it by introducing an **Optimistic Command Version protocol**. The client or command handler reads the current projection version, and the append to JetStream includes the `Nats-Expected-Last-Subject-Sequence` header. If two replicas race, the broker strictly serializes one, and the second append is rejected with a sequence mismatch. The validation read is entirely protected.
   - **M2 (Dual-Write Atomicity):** Structurally eliminated. The event *is* the write. There is no DB commit followed by an outbox or emit phase; the JetStream append is the only mutation. If the broker is unreachable, the write fails cleanly and entirely.

6. **PHASE 5 FIRST SLICE** — The Foundation Slice. We do not rewrite the world all at once.
   - Establish the permanent `world` JetStream stream and the Snapshot/Compaction lifecycle (resolving the OPS-02 retention conflict).
   - Invert exactly one core entity family (e.g., `Locations` and `Exits`).
   - Write a projector that consumes location events and updates the *existing* Postgres tables, so legacy reads still work transparently.
   - Execute a Genesis Snapshot to migrate the current CRUD locations into the new log.

7. **WHAT THE A/B FRAMING MISSES** — The graph transaction boundary. A MUSH world is a dense, highly interconnected graph. Moving a character touches the character, the old location, and the new location. The A/B framing dangerously assumes "per-entity ordering (JetStream per-stream seq... spanning `location.<id>` / `character.<id>` subjects)". Appending to multiple independent entity streams without a distributed transaction risks split-brain corruption if the broker drops an append mid-flight. Because a MUSH is relatively low-write-rate, we must use a **single, totally-ordered `world` stream** (partitioned by subject for consumers). Commands validate against the projection, then atomically append a batch of events to this single stream, guaranteeing absolute cross-entity integrity without complex sagas.

## Opinion — Codex (gpt-5.6-sol, reasoning max)

1. **RECOMMENDATION** — Choose **Option A: PostgreSQL-backed event sourcing for authoritative world aggregates, with synchronous projections and a transactional JetStream outbox**; confidence: **high**.

2. **WHY** — A MUSH’s durable product is an evolving shared history, not merely the latest rows. Three years from now, HoloMUSH will benefit from being able to explain how a world reached its current state: world-building changes, movement, ownership, moderation disputes, economies, NPC actions, and scene context all have temporal meaning.

Event sourcing gives the platform one enforceable extension seam. Plugins, weather systems, economies, and NPCs submit commands; the core authorizes and validates them; committed domain events are the only authoritative mutations. Plugins and clients consume stable facts without sharing database schemas or bypassing invariants. New search, graph, moderation, analytics, and AI-context projections can be built from existing history without changing the write model.

Option B fixes M12 and M2, but preserves two independently maintained descriptions of every mutation: the row update and its semantic notification. An outbox guarantees delivery of events developers remembered to create; it cannot guarantee complete or replay-sufficient event coverage. The current four emitted families versus roughly 15–20 mutation families demonstrates that this drift is structural, not hypothetical.

The authoritative journal SHOULD be PostgreSQL, not JetStream. JetStream remains the hot distribution layer; its retention policy and broker semantics should not define whether the world can be reconstructed. HoloMUSH’s relatively small, low-write world makes permanent domain history and periodic snapshots operationally reasonable.

3. **END STATE** — A command enters a single world command boundary carrying actor, causation, and expected aggregate version. The core performs ABAC and invariant checks, then atomically:

- Appends immutable, schema-versioned domain events to a PostgreSQL journal.
- Advances each affected aggregate’s version.
- Applies the events to synchronous current-state projections.
- Enqueues the same event envelopes for JetStream publication.

The journal has per-aggregate ordering plus a global position. The outbox relay publishes with the event ULID as `Nats-Msg-Id`; all consumers deduplicate by event ID and checkpoint source position.

Normal reads and restarts use PostgreSQL projections—event sourcing does not require replaying the world on every boot. Replay is used to rebuild, verify, create new projections, investigate history, and recover from projection defects. Snapshots accelerate aggregate loading and rebuilding without replacing the journal as the source of truth.

This makes historical inspection, compensating undo, moderation timelines, alternate read models, world forks, economy ledgers, and causally grounded NPC behavior substantially cheaper.

4. **RISKS** — The largest risks are permanent architectural complexity, not implementation effort:

- Bad event semantics become long-lived contracts. Events need ownership, schema versions, compatibility tests, and explicit upcaster or migration policy.
- Projection code becomes critical infrastructure. Idempotency, deterministic rebuilds, checkpoints, drift detection, and snapshot verification must be first-class.
- Event sourcing does not solve cross-aggregate invariants. Multi-aggregate commands need version vectors, ordered locking, or explicit workflows.
- Immutable history conflicts with privacy and erasure. Sensitive payloads need encryption boundaries, redaction events, and crypto-shredding rules; world-state retention must be separated from scene-content retention.
- Plugin-provided reducers cannot become unrecoverable dependencies. Authoritative event schemas and projectors need durable ownership and versioned packaging.

Rejecting B forfeits genuine simplicity: direct SQL repair, straightforward deletion, fewer moving parts, and easier ad hoc schema changes. B would be operationally adequate and much easier to teach. That simplicity is valuable, but it is the wrong optimization for a platform whose history is part of its product semantics.

5. **M12/M2 CLOSURE** — M12 closes through an explicit optimistic command-version protocol, not merely event ordering. Every mutation supplies the version it evaluated. The append transaction conditionally advances the stream head—for example, `UPDATE aggregate_heads ... WHERE version = expected_version`—and enforces uniqueness on `(aggregate_id, aggregate_version)`. Exactly one racing command commits; the other receives a typed conflict. Intent-specific events such as `LocationDescriptionChanged` also eliminate stale full-row replacement. Automatic retry is permitted only for commands proven safe to re-read, reauthorize, and re-evaluate; collaborative edits surface the conflict.

For commands touching several aggregates, the transaction checks every relevant expected version and acquires locks in a deterministic order.

M2 closes because the authoritative event, synchronous projection, and outbox record commit in one PostgreSQL transaction. Success means the world mutation and durable publication intent both exist. A broker outage leaves a retryable outbox row; it does not produce an ambiguous “state changed, event maybe published” result. The relay publishes at least once, JetStream deduplicates by event ULID, and consumers remain idempotent. World services no longer publish directly after committing.

6. **PHASE 5 FIRST SLICE** — Implement one complete vertical slice covering the two proven failures: migrate the entire `Location` and `Character` aggregate families—not just individual methods—to the new protocol.

Build the PostgreSQL journal, aggregate heads, versioned event envelope, transactional projector/outbox, relay, and rebuild verifier. Import existing rows as explicit genesis events. Route every Location and Character create/update/delete/move command through event append; make their repositories projection-only. Extend the plugin command boundary with expected versions. The acceptance suite must prove concurrent edits produce one success plus one conflict, broker loss leaves durable pending publication, duplicate delivery is harmless, and an empty projection database rebuilds to the same state and versions.

7. **WHAT THE A/B FRAMING MISSES** — It conflates four separate concerns: authoritative history, current-state projections, message transport, and audit retention. Choosing event sourcing does not make JetStream the database, require asynchronous user-visible reads, or require every HoloMUSH table to be event-sourced.

The boundary should be: authoritative world aggregates are event-sourced; their read models are disposable projections; JetStream is derived delivery infrastructure; sessions, caches, operational records, and unrelated configuration may remain CRUD. Plugins submit commands rather than arbitrary authoritative events. That boundary—not “events everywhere”—is what makes Option A sustainable.

## Opinion — Fable (Fable 5, repo + CodeGraph research)

**1. RECOMMENDATION**

Option B — CRUD-canonical + optimistic concurrency + transactional outbox — with one material strengthening: the outbox MUST carry the **complete** world-mutation taxonomy (~15–20 event types, every Create/Update/Delete/property write), making the event feed a first-class architectural contract rather than a compliance patch. Confidence: **high**.

**2. WHY**

I researched the code before opining. The write surface is small and perfectly chokepointed: ~16 write methods in `internal/world/service.go`, every repo write inside that one file (the skeptic verified 13 call sites, zero external writers), all behind pre-write ABAC `checkAccess`, with a working `Transactor.InTransaction` seam already used by the delete cascades. The event-bus substrate is genuinely excellent — JetStream publish with `Nats-Msg-Id` dedup, per-event DEK crypto with AAD, a DLQ-hardened audit projection, hot-JetStream→cold-Postgres history with downgrade fences. Both halves are strong. The question is only which is canonical for a small mutable spatial graph.

Three future-state arguments decide it against A, none of them cost arguments:

**Evolvability inverts under event sourcing.** Pre-1.0, the dominant change type is write-semantics churn — entity shapes, containment rules, property models will keep moving. Under B, that's a migration plus a repo edit. Under A, every historical event schema becomes load-bearing forever: upcasters, versioned payloads, replay-safety tests for each change, and (via ARCH-04) a unified event model that can never again be treated as merely a wire format. Event sourcing makes *adding read models* cheap and *changing write semantics* permanently expensive. A young platform should optimize for the opposite.

**A doesn't buy the correctness it advertises.** The draft ADR's own point 6 is the tell: validation reads (ABAC, existence, containment) precede the append, so A still requires an optimistic expected-version protocol — the M12 guard moves, it doesn't vanish. And a MUSH command loop is interactive: `move` then `look` must be consistent on either replica. Async projections break read-your-writes across two replicas (wait-for-seq or sticky routing machinery); synchronous projections rebuild B's transaction with more parts. A converges to "B plus a projection tier" from a correctness standpoint.

**The replay value lives in the wrong domain.** The genuinely log-native domain of a MUSH — narrative: says, poses, scenes — *already rides the bus*, encrypted, audited, replayable to clients, with scene playback via `scene_log`. World-state history (who renamed a room, when) is builder/moderation audit, fully served by a complete durable event feed plus the unbounded cold tier — without state being *derived* from it. Extensibility for plugins, weather, economy, NPC AI comes from a reliable, complete change feed to subscribe to; today's real gap is that world mutations are invisible on the bus (4 families emit, emitter unwired in production). Closing *that* is the extensibility win, and it's common to both options.

Operationally, the harness proved DB-read recovery boots clean and works. A replaces a proven recovery model with genesis snapshots, rebuild-verify jobs, projection-lag monitoring, and a retention inversion against OPS-02 — permanent complexity purchased for a domain with no consumer that needs derivable state. If any world event ever became `crypto.emits`-sensitive, a canonical log would also couple DEK lifecycle to state recoverability; as a feed, a crypto-lost event degrades audit, not the world.

**3. END STATE (2–3 years)**

Writes: every `world.Service` mutation is validate → guarded CAS `UPDATE … WHERE id=$1 AND version=$2 RETURNING` → outbox row in the same transaction. Commit means "state changed AND its event is durably queued" — one truth, no `move_succeeded=true` ambiguity. Reads: Postgres for current state (strongly consistent, read-your-writes free on N replicas); the WorldService/hostcap read chokepoint unchanged for both plugin runtimes. The bus carries the total ordered feed of world change; downstream projections (search indexes, world-map caches, analytics, NPC-AI perception feeds) are built as consumers next to the audit projection — cheap, disposable, rebuildable *from the feed*, never load-bearing for canonical state. Recovery: DB read, as today, proven. New capabilities that become cheap: any feature shaped "react to world change" (weather affecting descriptions, economy hooks, activity digests, moderation timelines), point-in-time forensic reconstruction from `events_audit`, and per-aggregate event sourcing for genuinely log-native future domains (an economy ledger, NPC memory) on the existing substrate — JetStream's `Nats-Expected-Last-Subject-Sequence` per-subject CAS is sitting there as the seam if one is ever wanted.

**4. RISKS**

Of B: (1) **Coverage rot** — the exact drift that caused this ADR: events that are "notification only" quietly stop being emitted for new writes. Mitigation is structural: an invariant (INV-registered, meta-tested) that every `world.Service` write method enqueues exactly one outbox event, plus the resilience suite as the standing regression. (2) Outbox relay is at-least-once; consumers must stay idempotent (dedup exists, but discipline is required). (3) Conflict-surfacing UX must be designed, not defaulted — a silent bounded-retry can re-hide concurrency. What is genuinely lost by rejecting A: byte-perfect derivable state and true time-travel of the canonical world; the elegance of one write model; and the public "events all the way down" claim must be honestly downgraded at all 6 doc sites. That loss is real but small in this domain — and it's a loss of *doctrine*, not of product capability, given a complete feed.

**5. M12/M2 CLOSURE**

M12: `version INTEGER NOT NULL DEFAULT 1` on locations/exits/characters/objects; repos switch to version-predicated CAS with `RETURNING` to distinguish not-found from conflict deterministically; `Version` threads through the structs, `UpdateCharacterDescription`, and the `entity_mutator.go` read-modify-write path (the proven M12 driver). Conflict behavior: surface `WORLD_CONCURRENT_EDIT` to the caller; permit one automatic re-read-and-retry only in `entity_mutator` single-field setters (field-disjoint edits merge safely; same-field conflicts surface). M2: outbox row written inside the same `InTransaction` as the entity write; relay publishes with `Nats-Msg-Id` = event ULID (existing `DupeWindow` dedups); delete the post-commit `EmitMoveEvent` path and the `EVENT_EMITTER_MISSING` dead code. The caller's result becomes truthful by construction; delivery is at-least-once, deduplicated. No additional protocol needed — unlike A, which would still need an expected-version append protocol on top.

**6. PHASE 5 FIRST SLICE**

(a) Version columns + guarded repos + conflict surfacing; flip the resilience suite's M12 specs to assert surfaced conflicts (regression green). (b) Outbox table + relay; route `MoveCharacter` — the one empirically characterized window — through it end-to-end. (c) Land the full event taxonomy as a spec artifact plus the write-coverage invariant (registered, meta-tested), then mechanically roll Create/Update/Delete emission through the outbox. (d) MODEL-02 doc downgrade at all 6 sites, same phase — the drift lesson is that docs and code must move together.

**7. WHAT THE A/B FRAMING MISSES**

The framing poses "events vs rows," but the load-bearing decision is **what the platform's extensibility contract is** — and the answer (a complete, reliable, ordered world-change feed) is identical under both options; A and B differ only in which side is canonical. Second, "event sourcing" is not one option but a per-aggregate dial: scenes are already the event-sourced aggregate in disguise, and the substrate supports promoting any future log-native domain without a global commitment — adopting A wholesale to "realize the stated principle" would be doctrine-driven, not domain-driven. Third, the root failure here was never CRUD — it was an unenforced binding between stated architecture and code. Whichever option is recorded, the ADR must ship with mechanical enforcement (the coverage invariant, the resilience suite), or B will rot exactly the way the event-sourcing claim did.

## Orchestrator synthesis (adjudication)

Written by the orchestrating agent (Fable 5, distinct dispatch from the Fable panelist) after reading all three opinions. The human decider read this alongside the verbatim opinions above; it is an input, not the decision.

### Vote and shape

2–1 for event sourcing on raw votes (Antigravity A, Codex A, Fable B) — but the two A votes disagree fundamentally on where the source of truth lives, and the two Postgres-transaction designs (Codex-A and Fable-B) are mechanically near-identical. The vote count alone is misleading.

### Argument quality

- **Antigravity** contributed one important insight and one disqualifying error. The insight: world mutations are graph-shaped (a move touches character + two locations), so per-entity subject ordering is dangerous for any A implementation — single-stream or multi-aggregate transactional ordering is required. The error: broker-as-source-of-truth. For an interactive MUD command loop, `move` then `look` hitting a lagging projection is a product-level correctness failure, and making JetStream retention semantics define world reconstructability is backwards. Both other panelists reject this shape. **Antigravity's A variant should be off the table regardless of the A/B outcome.**
- **Codex** delivered the strongest argument for A, and it is empirical: **coverage rot is structural under B** — "an outbox guarantees delivery of events developers remembered to create," and this repo already lived that failure (4 emitted families of ~15–20 mutation types, emitter unwired in production, docs claiming a principle nobody built). Under A, coverage cannot rot because the event is the write. Codex's shape is disciplined (Postgres journal, synchronous projections, JetStream derived, ES for world aggregates only, plugins submit commands not events) and its risk list is honest — including the erasure/privacy tension of immutable history in a codebase whose flagship feature is per-event encryption with crypto-shredding semantics.
- **Fable** delivered the strongest argument against A: **evolvability inverts**. Pre-1.0 the dominant change type is write-semantics churn, and A converts every historical event schema into a permanent replay-load-bearing contract (upcasters, versioned payloads, replay-safety tests, forever); B keeps schema churn a migration + repo edit. Fable also collapsed A's correctness case ("A converges to B plus a projection tier" — Codex's synchronous-projection design quietly concedes this) and made the clarifying domain observation: the log-native part of a MUSH — the narrative — is already event-sourced (scenes, encrypted, replayable); world state is a small mutable spatial graph whose history is moderation/builder audit.

### Unanimous across all three, regardless of verdict

1. The complete world-mutation event feed (~15–20 types) must exist and reliably emit — the feed is the platform's real extensibility contract, and closing today's gap (4 families, unwired emitter) is the win common to both options.
2. M12 requires an explicit optimistic version protocol under either option; event sourcing does not dissolve it.
3. State change and event durability must be atomic (one transaction, or the append is the write).
4. Enforcement must be mechanical (registered invariant + meta-test + the resilience suite), or either option rots the way the event-sourcing claim did. The root failure was never CRUD; it was an unenforced binding between stated architecture and code.

### The residual crux

Strip the shared mechanics away and the decision reduces to:

1. **Coverage by construction vs. coverage by enforcement.** A's guarantee is structurally stronger; B's is credible in this specific repo (unusually strong meta-test/invariant culture) but is a discipline dependency.
2. **Permanent event schemas as asset vs. liability.** Codex: the enforceable extension seam. Fable: a pre-1.0 tax on every future write-semantics change. Which effect dominates depends on platform maturity.
3. **Is derivable world state a product conviction?** Time-travel of the canonical world, world-forking, byte-perfect rebuild — only A buys these; B offers forensic reconstruction from the feed. (Fable's counter: under B, new projections bootstrap from current rows + tail the feed, so A's late-projection replay advantage mostly evaporates for world state.)

### Orchestrator lean

**B-strengthened (Fable's shape), medium-high confidence**, folding in two Codex contributions: record the four-concern boundary (authoritative history / current-state projections / transport / audit retention) in the ADR so a future per-aggregate ES adoption has a clean seam, and make the event taxonomy + write-coverage invariant non-negotiable ADR clauses rather than implementation details. Rationale under the decider's own drivers: the extensibility payoff (the complete feed) is identical under both options; the evolvability axis genuinely favors B pre-1.0; A's unique payoffs attach to the domain that least needs them while its permanent liabilities (replay-load-bearing schemas, projection infra as critical path, erasure tension with the crypto layer) attach to the platform's most churn-prone years. The event-sourcing soul of the platform lives in the narrative timeline, which is already built.

**What flips the lean to Codex-A:** if world-state-as-timeline is a product conviction — builders forking regions, staff time-traveling the grid, derivable world state as a shipped feature — then Codex's Postgres-journal shape is the safe way to build it, and the schema-contract tax is the fair price of a capability actually wanted. That is a product-vision question only the human decider can answer.
