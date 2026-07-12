---
phase: 05-world-model-integrity-fixes-m2-m12
plan: 05
subsystem: world-model / transactional outbox
tags: [outbox, migration, feed-counter, envelope, wmodel, atomicity, MODEL-04]
requires: [05-04, 05-14]
provides:
  - "migration 000050 — outbox + world_feed_counter + world_genesis_checkpoint + world_consumer_receipts + world_consumer_watermarks schema"
  - "wmodel.Envelope / wmodel.EnvelopeIntent value types + pure writer-only Finalize"
  - "postgres.FeedCounter — locked per-game feed_position+epoch allocator (bounded lock timeout)"
  - "postgres.OutboxStore.WriteIntent — same-tx allocate+finalize+persist+return"
  - "world.ErrFeedLockTimeout / CodeFeedLockTimeout sentinel"
  - "always-run INV-WORLD-1 state+envelope atomicity binding target"
affects: [05-06, 05-07, 05-11, 05-12, 05-15]
tech-stack:
  added: []
  patterns: [transactional-outbox, locked-per-game-counter, cycle-neutral-leaf-package, writer-owns-storage-fields]
key-files:
  created:
    - internal/store/migrations/000050_world_outbox.up.sql
    - internal/store/migrations/000050_world_outbox.down.sql
    - internal/world/wmodel/envelope.go
    - internal/world/wmodel/envelope_test.go
    - internal/world/postgres/feed_counter.go
    - internal/world/postgres/feed_counter_test.go
    - internal/world/postgres/outbox_store.go
    - internal/world/postgres/outbox_store_test.go
  modified:
    - internal/world/errors.go
    - internal/store/migrate_integration_test.go
    - internal/store/migrate_test.go
decisions:
  - "feed_position/epoch/manifest are owned exclusively by the WriteIntent writer (round-3 blocker #1); executors/commands pass (intent, delta) and receive the finalized Envelope back"
  - "Envelope/EnvelopeIntent live in the wmodel LEAF package (imports internal/core for NewULID — cycle-safe; core does not import world); leaf-guard test stays green"
  - "lock-timeout uses SET LOCAL lock_timeout inside the ambient tx; SQLSTATE 55P03 maps to WORLD_FEED_LOCK_TIMEOUT"
  - "event_id is the outbox PK (dedup key); (game_id, epoch, feed_position) UNIQUE enforces gap-free per-game order"
metrics:
  duration: ~55min
  tasks: 3
  files: 11
  completed: 2026-07-12
status: complete
---

# Phase 5 Plan 05: MODEL-04 Outbox Foundation (slice 2, interface-first) Summary

Interface-first transactional-outbox foundation: migration 000050 (outbox +
per-game feed counter + durable genesis checkpoint + the split consumer
receipts/watermarks store), the semantic `Envelope`/`EnvelopeIntent` value types
in the cycle-neutral `wmodel` leaf, and — at the `internal/world/postgres` writer
boundary — the locked per-game `feed_position`+epoch allocator and the
same-transaction `WriteIntent` that allocates, finalizes, persists, and returns
one envelope per command. Proven atomic by an always-run state+envelope
integration test that mutates a REAL world row.

## What was built

**Task 1 — migration 000050 (`type=auto`).** Paired idempotent
`000050_world_outbox.{up,down}.sql` creating five tables:
- `outbox` — `event_id` PK (the ULID = Nats-Msg-Id dedup key), `(game_id, epoch,
  feed_position)` UNIQUE (gap-free commit order per game/epoch), JSONB `affected`
  manifest + JSONB `payload`, nullable `published_at`, nullable
  `skip_marker_event_id` (round-4 A1), and a partial index on `published_at IS
  NULL` ordered `(game_id, epoch, feed_position)` for the relay scan.
- `world_feed_counter` — `game_id` PK, `next_position`, `epoch DEFAULT 1`, and
  `lease_generation BIGINT NOT NULL DEFAULT 0` (round-4 A2 — the durable relay
  lease-generation source-of-truth).
- `world_genesis_checkpoint` — PK `(game_id, epoch, aggregate_type,
  aggregate_id)`, the durable genesis identity that survives outbox pruning
  (round-3 MEDIUM).
- `world_consumer_receipts` (PK `(consumer_name, event_id)`) **and**
  `world_consumer_watermarks` (PK `(consumer_name, game_id)`, `epoch`+
  `feed_position`) — the SPLIT idempotency store (round-6 R6-5). The up-migration
  header documents the 05-07 monotonic UPSERT with the single lexicographic
  predicate + `ApplyOnce` contiguity guard.

All timestamps are BIGINT epoch-ns (INV-STORE-1); no triggers/functions; down
drops all five in reverse order. Migrator fixtures updated 49→50
(`migrate_test.go` pending list + latest-version case; `migrate_integration_test.go`
version assert + `expectedTables` +5 rows).

**Task 2 — value types (wmodel) + locked counter (postgres) (TDD).**
- `wmodel.EnvelopeIntent` carries a fresh `core.NewULID()` event id, an EXPLICIT
  caller-supplied `GameID` (round-9 R6-5), kind/schema/actor/causation/correlation/
  primary-aggregate/payload — and deliberately NO epoch/position/manifest.
  `NewEnvelopeIntent` is the sole mint site for the id (never `idgen`).
- `wmodel.Envelope` adds the storage-owned `Epoch`/`FeedPosition` + the
  `[]AffectedAggregate` manifest. The pure `Finalize(intent, delta, epoch, pos)`
  builds the manifest from the `MutationDelta` (primary + cascades, with
  before/after versions) and carries `GameID` through unchanged. Documented as
  writer-only (round-3 blocker #1).
- `postgres.FeedCounter.Allocate(ctx, gameID)` runs inside the ambient tx
  (`execerFromCtx`): `SET LOCAL lock_timeout`, upsert-init the counter row,
  `SELECT next_position, epoch ... FOR UPDATE`, increment. Returns `(epoch,
  position)`. A held lock surfaces `WORLD_FEED_LOCK_TIMEOUT` (SQLSTATE 55P03)
  rather than blocking. Integration tests prove gap-free monotonicity, FOR UPDATE
  serialization across 8 concurrent callers, and the bounded-timeout path.

**Task 3 — same-tx WriteIntent + INV-WORLD-1 atomicity (TDD).**
`postgres.OutboxStore.WriteIntent(ctx, intent, delta) (*wmodel.Envelope, error)`
allocates `(epoch, feed_position)` from the counter (late), finalizes via
`wmodel.Finalize`, inserts ONE outbox row through `execerFromCtx`, and returns the
finalized envelope. It never opens its own connection/tx. The always-run
INV-WORLD-1 integration test proves a REAL location row and its envelope commit
or roll back together three ways: rollback → neither survives; commit → both
survive; a forced outbox failure (duplicate `event_id`) after the state write →
the state change rolls back. A per-game round-trip test proves `game_id` survives
intent → outbox row → returned `Envelope` and that a different game allocates from
its own counter (position 1, not game A's).

## Verification

- `task test -- ./internal/world/wmodel/` — green (6 tests incl. the leaf-guard).
- `task test:int -- -run 'Counter|FeedCounter|Outbox|Writer' ./internal/world/postgres/` — green (10 tests).
- `task test:int -- ./internal/world/postgres/ ./internal/store/` — green (478 tests; no regression; migration applies+reverts).
- `task lint` — exit 0.
- Grep gates: no `idgen.New` in wmodel/outbox_store/feed_counter; no outbox write SQL under `internal/world/outbox` (no such pkg yet); no `BIGSERIAL`/`SERIAL` in 000050; `skip_marker_event_id` + `lease_generation` present.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Migrator fixtures hardcoded latest version 49**
- **Found during:** Task 1 migration integration run.
- **Issue:** `migrate_integration_test.go` (version assert + `expectedTables`) and
  `migrate_test.go` (`PendingMigrations` list + latest-version case) hardcoded 49;
  adding 000050 made them red. Expected per the plan/holomush_notes (05-01 added
  000049; this plan adds 000050).
- **Fix:** Bumped both to 50 and added the five new tables to `expectedTables`.
- **Files modified:** internal/store/migrate_integration_test.go, internal/store/migrate_test.go
- **Commit:** 947fe136c (Task 1)

**2. [Rule 1 - Doc] Comment literals tripped grep-based lint/verification gates**
- **Found during:** `task lint` (`lint:no-timestamptz`) and the verification grep gates.
- **Issue:** Doc comments in the migration and `envelope.go` contained the literal
  tokens `TIMESTAMPTZ`, `BIGSERIAL`, and `idgen.New()`, which are matched by the
  grep-based gates even inside comments.
- **Fix:** Reworded the comments to describe the constraint without the literal
  banned tokens; the constraints themselves are unchanged.
- **Files modified:** internal/store/migrations/000050_world_outbox.up.sql, internal/world/wmodel/envelope.go
- **Commit:** 947fe136c (BIGSERIAL), 3deb07731 (TIMESTAMPTZ + idgen rewords)

No architectural changes, no auth gates, no checkpoints. Everything else executed as written.

## Known Stubs

None that block the plan goal. The consumer receipts/watermarks tables and
`skip_marker_event_id`/`lease_generation` columns are provisioned here but written
by 05-07 (relay/consumer); `world_genesis_checkpoint` is written by 05-11
(genesis snapshot). The `// Verifies: INV-WORLD-1` annotation on the atomicity
test is added in 05-12 per the plan. These are the plan's explicit forward
boundary (interface-first slice), not incomplete work.

## Self-Check: PASSED

- FOUND: internal/store/migrations/000050_world_outbox.up.sql
- FOUND: internal/store/migrations/000050_world_outbox.down.sql
- FOUND: internal/world/wmodel/envelope.go
- FOUND: internal/world/postgres/feed_counter.go
- FOUND: internal/world/postgres/outbox_store.go
- FOUND commit 947fe136c (Task 1), 534816c65 (Task 2), 3deb07731 (Task 3)
- `task lint` + `task test -- ./internal/world/wmodel/` + `task test:int` (postgres+store) all green.
