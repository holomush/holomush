-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Revert 000050_world_outbox.up.sql: drop the five outbox-foundation tables in
-- reverse creation order with DROP TABLE IF EXISTS so the revert is idempotent
-- and cleanly restores the pre-migration schema. The partial index
-- idx_outbox_unpublished is dropped implicitly with its outbox table.

DROP TABLE IF EXISTS world_consumer_watermarks;
DROP TABLE IF EXISTS world_consumer_receipts;
DROP TABLE IF EXISTS world_genesis_checkpoint;
DROP TABLE IF EXISTS world_feed_counter;
DROP TABLE IF EXISTS outbox;
