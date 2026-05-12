-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors
-- Add phase3_rows_rewritten column to crypto_rekey_checkpoints so the Phase 3
-- row count is persisted and available to Phase 7 for the RekeyAuditPayload
-- (holomush-jxo8.7.54). NOT NULL DEFAULT 0 gives existing rows a sensible value.

ALTER TABLE crypto_rekey_checkpoints
    ADD COLUMN IF NOT EXISTS phase3_rows_rewritten int NOT NULL DEFAULT 0;
