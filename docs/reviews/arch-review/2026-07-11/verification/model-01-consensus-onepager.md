<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->

# MODEL-01 Consensus One-Pager — CRUD-Canonical + Version Guard + Ordered Atomic Feed

**Status:** panel-ratified shape of Option B. This page defines the B the panel endorses; the A/B decision itself remains the human decider's.

## Decision shape

World state (locations, exits, characters, objects) stays **canonical in PostgreSQL**. Every world mutation is protected by **optimistic concurrency** and atomically publishes to an **ordered, complete, schema-governed world-change feed** via a transactional outbox. The feed — not the rows — is the platform's extensibility contract. Event sourcing is not adopted for world state; the per-aggregate dial stays open for genuinely log-native future domains (economy ledger, NPC memory).

## Core mechanics

1. **Version guard (M12).** `version INTEGER NOT NULL DEFAULT 1` on locations, exits, characters, objects (exits verified to have racing writers). Writes are version-predicated CAS `UPDATE … WHERE id=$1 AND version=$2`; deletes are equally version-predicated (`DELETE … WHERE id=$1 AND version=$2`). Any zero-row result is classified by a locked follow-up read in the same transaction (conflict vs concurrent-delete vs not-found — deterministic, no diagnosis race).
2. **Atomic feed (M2).** Each successful, externally visible world command commits **exactly one semantic envelope** to the outbox **in the same transaction** as its state change: `{event_id (ULID), feed_position, kind, schema_version, actor, causation/correlation, primary aggregate, affected-aggregates manifest with before/after versions, payload}`. Payloads are **intent-level, new-values-only** (erasure-safe; history privacy reduces to audit retention). Deletes emit one tombstone per aggregate; cascades are subsumed by the command's single envelope. Failed/no-op commands emit nothing. The post-commit emit path (`EmitMoveEvent`, `EVENT_EMITTER_MISSING`) is deleted outright.
3. **Feed ordering.** `feed_position` is allocated transactionally from a locked per-game counter (commit-ordered, gap-free — not `BIGSERIAL`). The counter deliberately serializes world writes per game — an accepted throughput ceiling at MUSH write rates; do NOT revert to insert-time allocation without re-deriving the commit-order proof. A single leased relay publishes strictly in position order (`Nats-Msg-Id` = event ULID; JetStream `DupeWindow` + durable consumer-side dedup beyond the window), woken by `LISTEN/NOTIFY` with a periodic sweep. Poison envelopes halt-and-alert (DLQ posture mirrors the audit projection); rows are marked published after PubAck and pruned.

## Enforcement (the drift that caused this ADR must be non-recurrable)

- **Compile-time:** the guarded write seam requires the envelope — `mutate(ctx, entity, expectedVersion, envelope)`; an envelope-less write is a type error.
- **Runtime:** the wrapper enforces envelope cardinality; `INV-WORLD-ATOMIC-FEED`, `INV-WORLD-DELTA-PARITY` (manifest provably matches the committed delta — presence is not enough), `INV-WORLD-FEED-ORDER`, `INV-WORLD-WRITER-BOUNDARY` (no world-table writes outside the mutation wrapper; admin repairs AND migrations/backfills either emit normal mutations or advance the feed epoch) — all registered and bound per the invariant registry.
- **Static:** census meta-test enumerates every write command → declared envelope kind; lint fence forbids raw world-table SQL outside `internal/world/postgres`.
- **Empirical:** the two-replica resilience suite (M12/M2 reproductions) is the permanent regression gate, extended with fault-injection (relay crash around PubAck, dual relay, duplicate delivery, broker downtime) and per-aggregate race tests.

## Consistency, UX, lifecycle

- **Conflicts surface strictly** as typed `WORLD_CONCURRENT_EDIT` (telnet message + web retry affordance; mapping table ships in the taxonomy spec). **No automatic retry in the first slice**; compare-before-retry semantics (retry only if the original field is unchanged, re-run `checkAccess` + validation) may be added later, narrowly, if telemetry justifies it.
- **Bootstrap & retention:** consumers bootstrap from a consistent DB snapshot + feed watermark, then tail (OPS-02 bounds retention — no infinite-replay assumption; reconstruction claims are bounded by retention). Genesis snapshot events are emitted once at cutover to give the feed a defined origin. A feed **epoch/reset** procedure covers DB restores and backfills.
- **Schema governance:** the ~15–20-type taxonomy is a versioned, CI-validated schema registry (per-type payload schemas; `App-Schema-Version`), explicitly designated the **ARCH-04 (Phase 7) input** so the unified event model adopts these schemas.

## Honestly forgone

Byte-derivable canonical state, true time-travel/world-forking of the canonical world, and proof that history derives state. Docs downgrade the "event sourcing" claim at ~6 MODEL-02 sites in the same phase the mechanism lands. Relay lag makes the feed temporally secondary to the DB (bounded by wakeup contract + lag alerting).

## Phase 5 first slice (ordered)

1. Version columns + guarded repos + strict conflict surfacing; resilience M12 specs flip to assert surfaced conflicts.
2. Outbox + envelope + positioned relay; `MoveCharacter` end-to-end (the empirically characterized window) + a reference idempotent consumer and bootstrap harness — **zero product projections in Phase 5**. Relay lag/halt alerting lands in THIS slice (a halted ordered feed is otherwise silent).
3. Taxonomy schema registry + census meta-test + invariants registered; then mechanical emission rollout across the remaining commands; genesis emission at cutover.
4. MODEL-02 doc downgrade, same phase.

**Panel:** Antigravity (Gemini 3.1 Pro High), Codex (gpt-5.6-sol max), Fable (Fable 5), orchestrator (Fable 5) — ratified unanimously on 2026-07-11 (pass 1: Antigravity AGREE, Fable AGREE + 2 notes folded in, Codex OBJECT with 2 blocking edits; pass 2 after amendments: all AGREE). Trail: [model-01-panel-round2.md](model-01-panel-round2.md).
