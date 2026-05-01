-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors
--
-- Adds two nullable columns + a partial index for sensitive event lookups.
-- NULL on both columns is the correct representation for codec=identity
-- rows (cleartext events have no DEK). No foreign key to crypto_keys —
-- Rekey destroys old crypto_keys rows by design (master spec §4.7).

ALTER TABLE events_audit
    ADD COLUMN IF NOT EXISTS dek_ref     BIGINT,
    ADD COLUMN IF NOT EXISTS dek_version INTEGER;

CREATE INDEX IF NOT EXISTS events_audit_dek_ref
    ON events_audit (dek_ref)
    WHERE dek_ref IS NOT NULL;
