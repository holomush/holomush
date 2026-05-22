-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Convert events_audit and crypto_keys timestamp columns from TIMESTAMPTZ
-- to BIGINT (epoch nanoseconds, UTC). See:
--   docs/superpowers/specs/2026-05-22-nanosecond-timestamps-design.md
-- INV-TS-1, INV-TS-4, INV-TS-5.

-- Drop TIMESTAMPTZ defaults before type conversion; PostgreSQL cannot
-- auto-cast TIMESTAMPTZ defaults when changing column type to BIGINT.
ALTER TABLE events_audit
    ALTER COLUMN inserted_at DROP DEFAULT;

ALTER TABLE events_audit
    ALTER COLUMN timestamp
        TYPE BIGINT USING (EXTRACT(EPOCH FROM timestamp) * 1e9)::BIGINT,
    ALTER COLUMN inserted_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM inserted_at) * 1e9)::BIGINT;

ALTER TABLE events_audit
    ALTER COLUMN inserted_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;

ALTER TABLE crypto_keys
    ALTER COLUMN created_at DROP DEFAULT;

ALTER TABLE crypto_keys
    ALTER COLUMN created_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM created_at) * 1e9)::BIGINT,
    ALTER COLUMN rotated_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM rotated_at) * 1e9)::BIGINT,
    ALTER COLUMN destroyed_at
        TYPE BIGINT USING (EXTRACT(EPOCH FROM destroyed_at) * 1e9)::BIGINT;

ALTER TABLE crypto_keys
    ALTER COLUMN created_at
        SET DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT;
