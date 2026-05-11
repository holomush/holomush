-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors
-- Reverse: remove justification column from crypto_rekey_checkpoints.

ALTER TABLE crypto_rekey_checkpoints
    DROP COLUMN IF EXISTS justification;
