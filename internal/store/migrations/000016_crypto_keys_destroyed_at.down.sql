-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

DROP INDEX IF EXISTS crypto_keys_active_idx;

ALTER TABLE crypto_keys
    DROP COLUMN IF EXISTS destroyed_at;
