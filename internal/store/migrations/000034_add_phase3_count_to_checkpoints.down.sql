-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors
-- Reverse: remove phase3_rows_rewritten column from crypto_rekey_checkpoints.

ALTER TABLE crypto_rekey_checkpoints
    DROP COLUMN IF EXISTS phase3_rows_rewritten;
