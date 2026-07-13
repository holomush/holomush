-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- D-06 anti-TOCTOU substrate (Phase 5, plan 05-16, round-6 R6-2): a durable
-- reaping-state flag on the players table. The guest character-reaping service
-- MARKS a guest player reaping (sets reaping_at) BEFORE enumerating its
-- characters; the character-genesis service (05-15) reads this column
-- SELECT reaping_at ... FOR UPDATE at the start of its creation transaction and
-- REJECTS creation (PLAYER_REAPING) for a marked player. The mark UPDATE and the
-- genesis FOR UPDATE read contend on the SAME players row, so they serialize
-- across connections — closing the snapshot-then-delete window where a character
-- created after enumeration would be FK-cascade-deleted with no tombstone.
--
-- reaping_at is a nullable BIGINT epoch-ns (the repo-wide time-column
-- convention) — NULL means "not reaping", a non-NULL value is the epoch-ns of
-- the reaping mark. Nullable, so no backfill is required. ADD COLUMN IF NOT
-- EXISTS keeps the migration idempotent (re-run safe).
ALTER TABLE players ADD COLUMN IF NOT EXISTS reaping_at BIGINT;
