-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

DROP INDEX IF EXISTS scene_log_dek_ref;

ALTER TABLE scene_log
    DROP COLUMN IF EXISTS dek_version,
    DROP COLUMN IF EXISTS dek_ref;
