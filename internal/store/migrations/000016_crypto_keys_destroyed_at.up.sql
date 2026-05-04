-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors
--
-- Phase 3c (holomush-ojw1.3) Decision 4: soft-delete column on crypto_keys.
-- Replaces master spec §6.3's tombstone table. Production reads filter
-- destroyed_at IS NULL; forensic reads via Store.SelectAnyByID see
-- destroyed rows.

ALTER TABLE crypto_keys
    ADD COLUMN IF NOT EXISTS destroyed_at TIMESTAMPTZ NULL;

-- Partial index for the production read predicate (active rows only).
CREATE INDEX IF NOT EXISTS crypto_keys_active_idx
    ON crypto_keys (id)
    WHERE destroyed_at IS NULL;
