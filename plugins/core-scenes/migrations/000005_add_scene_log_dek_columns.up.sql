-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors
--
-- Phase 7 (holomush-1r0v): plugin-owned audit tables mirror events_audit
-- shape per master spec section 8.2. dek_ref BIGINT, dek_version INTEGER,
-- both nullable. Identity-codec rows have both NULL.

ALTER TABLE scene_log
    ADD COLUMN IF NOT EXISTS dek_ref     BIGINT,
    ADD COLUMN IF NOT EXISTS dek_version INTEGER;

CREATE INDEX IF NOT EXISTS scene_log_dek_ref
    ON scene_log (dek_ref)
    WHERE dek_ref IS NOT NULL;
