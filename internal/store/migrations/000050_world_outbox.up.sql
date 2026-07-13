-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- MODEL-04 (Phase 5, slice 2): the transactional-outbox foundation that closes
-- dual-write non-atomicity (M2, #4798-adjacent). Five tables, all created with
-- CREATE TABLE IF NOT EXISTS so the migration is idempotent and safe to re-run:
--
--   1. outbox                    — one durable envelope per successful command,
--                                  written in the SAME transaction as the state
--                                  change. feed_position is allocated from the
--                                  locked per-game world_feed_counter (NOT an
--                                  auto-increment / insert-time counter — the
--                                  commit-order proof depends on this), so it is
--                                  gap-free and commit-ordered per (game_id, epoch).
--   2. world_feed_counter        — the locked per-game position + epoch source
--                                  (SELECT ... FOR UPDATE) + the durable relay
--                                  lease_generation source-of-truth (round-4 A2).
--   3. world_genesis_checkpoint  — durable genesis identity that SURVIVES outbox
--                                  pruning (round-3 MEDIUM): genesis envelopes
--                                  use fresh ULIDs and outbox rows are pruned
--                                  after publish, so neither the event_id unique
--                                  nor the position unique can make a genesis
--                                  re-run idempotent. A same-epoch re-run
--                                  conflicts on this PK and skips before
--                                  allocating a position; an epoch advance
--                                  legitimately re-opens genesis.
--   4. world_consumer_receipts   — the per-event dedup key for the reference
--                                  idempotent consumer (05-07): one row per
--                                  applied event. JetStream's dedup window is
--                                  FINITE + configured (publisher.go:64/109,
--                                  subsystem.go:281), so an in-memory ULID set
--                                  cannot survive restart or a retry beyond the
--                                  window — this table does (round-5 finding 2).
--   5. world_consumer_watermarks — ONE row per (consumer, game): the resume
--                                  watermark, advanced by a monotonic UPSERT with
--                                  the single lexicographic predicate. Split from
--                                  receipts (round-6 R6-5): receipts are per-event
--                                  receipts; the watermark is per (consumer, game).
--
-- All timestamps are BIGINT epoch-ns (INV-STORE-1 / lint:no-timestamptz — NEW
-- tables must use BIGINT epoch-ns, not the zoned timestamp type). No triggers/
-- functions/procedures — every bit of logic (locked allocation,
-- monotonic-contiguous advance) lives in Go.

-- 1. outbox: one envelope per command, atomic with the state change.
CREATE TABLE IF NOT EXISTS outbox (
    -- event_id is the envelope ULID = Nats-Msg-Id dedup key; PK enforces the
    -- event_id UNIQUE constraint (a hand-minted or reused id collides here).
    event_id             TEXT NOT NULL PRIMARY KEY,
    game_id              TEXT NOT NULL,
    -- feed_position + epoch: gap-free commit order from the locked per-game
    -- counter. The (game_id, epoch, feed_position) UNIQUE makes any duplicate /
    -- insert-time allocation a hard DB error, not a silent gap.
    feed_position        BIGINT NOT NULL,
    epoch                BIGINT NOT NULL,
    kind                 TEXT NOT NULL,
    schema_version       INTEGER NOT NULL,
    actor                TEXT NOT NULL,
    causation_id         TEXT,
    correlation_id       TEXT,
    -- primary aggregate the command directly targeted.
    aggregate_id         TEXT NOT NULL,
    aggregate_type       TEXT NOT NULL,
    -- affected-aggregates manifest: before/after versions per touched aggregate,
    -- built from the repo's wmodel.MutationDelta (not command inputs).
    affected             JSONB NOT NULL,
    -- intent-level, new-values-only payload (erasure-safe; no secrets/DEK).
    payload              JSONB NOT NULL,
    -- published_at is NULL until the relay gets a PubAck; the partial index below
    -- scans NULLs in (epoch, feed_position) order per game.
    published_at         BIGINT,
    -- skip_marker_event_id: the STABLE event ULID of the operator skip marker for
    -- a poison row at this position, persisted BEFORE the marker is published so a
    -- crash-then-retry republishes the SAME Nats-Msg-Id (round-4 A1). NULL until
    -- an operator skip is initiated.
    skip_marker_event_id TEXT,
    created_at           BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT,
    CONSTRAINT outbox_game_epoch_position_unique UNIQUE (game_id, epoch, feed_position)
);

-- The relay's "next unpublished row in (epoch, position) order per game" scan.
CREATE INDEX IF NOT EXISTS idx_outbox_unpublished
    ON outbox (game_id, epoch, feed_position)
    WHERE published_at IS NULL;

-- 2. world_feed_counter: locked per-game position + epoch + lease generation.
CREATE TABLE IF NOT EXISTS world_feed_counter (
    game_id          TEXT NOT NULL PRIMARY KEY,
    -- next_position is the next feed_position to hand out; the allocator reads it
    -- FOR UPDATE, uses it, then increments (gap-free per game per epoch).
    next_position    BIGINT NOT NULL,
    -- epoch is the per-game current feed epoch a genesis/reset (05-11) advances so
    -- stale positions are never replayed after a restore/backfill.
    epoch            BIGINT NOT NULL DEFAULT 1,
    -- lease_generation is the durable, authoritative relay lease generation
    -- (round-4 A2): 05-07's AcquireLease bumps it and MarkPublished compares
    -- against it to reject a stale holder's DB ack.
    lease_generation BIGINT NOT NULL DEFAULT 0
);

-- 3. world_genesis_checkpoint: durable genesis identity that survives pruning.
CREATE TABLE IF NOT EXISTS world_genesis_checkpoint (
    game_id        TEXT NOT NULL,
    epoch          BIGINT NOT NULL,
    aggregate_type TEXT NOT NULL,
    aggregate_id   TEXT NOT NULL,
    created_at     BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT,
    PRIMARY KEY (game_id, epoch, aggregate_type, aggregate_id)
);

-- 4. world_consumer_receipts: per-event dedup key (one row per applied event).
CREATE TABLE IF NOT EXISTS world_consumer_receipts (
    consumer_name TEXT NOT NULL,
    event_id      TEXT NOT NULL,
    created_at    BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT,
    PRIMARY KEY (consumer_name, event_id)
);

-- 5. world_consumer_watermarks: ONE row per (consumer, game); monotonic resume
-- watermark. The 05-07 advance is an UPSERT
--   INSERT ... ON CONFLICT (consumer_name, game_id) DO UPDATE
--     SET epoch = EXCLUDED.epoch, feed_position = EXCLUDED.feed_position
--     WHERE epoch < $e OR (epoch = $e AND feed_position < $p)
-- — the SINGLE lexicographic predicate (round-9 grok MEDIUM), UPSERT so the FIRST
-- event for a (consumer, game) inserts (round-9 MEDIUM). ApplyOnce (05-07)
-- enforces CONTIGUITY on top so no un-applied position is ever skipped.
CREATE TABLE IF NOT EXISTS world_consumer_watermarks (
    consumer_name TEXT NOT NULL,
    game_id       TEXT NOT NULL,
    epoch         BIGINT NOT NULL,
    feed_position BIGINT NOT NULL,
    created_at    BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT,
    updated_at    BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT,
    PRIMARY KEY (consumer_name, game_id)
);
