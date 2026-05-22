-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- DOWN MIGRATION IS PRECISION-LOSSY. Recovers TIMESTAMPTZ semantics but
-- truncates ns → µs. No backfill of pre-down-migration data is provided.

-- Drop BIGINT defaults before type reversion; PostgreSQL cannot auto-cast
-- BIGINT defaults when reverting column type to TIMESTAMPTZ.
ALTER TABLE events_audit
    ALTER COLUMN inserted_at DROP DEFAULT;

ALTER TABLE events_audit
    ALTER COLUMN timestamp
        TYPE TIMESTAMPTZ USING to_timestamp(timestamp::double precision / 1e9),
    ALTER COLUMN inserted_at
        TYPE TIMESTAMPTZ USING to_timestamp(inserted_at::double precision / 1e9);

ALTER TABLE events_audit
    ALTER COLUMN inserted_at SET DEFAULT now();

ALTER TABLE crypto_keys
    ALTER COLUMN created_at DROP DEFAULT;

ALTER TABLE crypto_keys
    ALTER COLUMN created_at
        TYPE TIMESTAMPTZ USING to_timestamp(created_at::double precision / 1e9),
    ALTER COLUMN rotated_at
        TYPE TIMESTAMPTZ USING to_timestamp(rotated_at::double precision / 1e9),
    ALTER COLUMN destroyed_at
        TYPE TIMESTAMPTZ USING to_timestamp(destroyed_at::double precision / 1e9);

ALTER TABLE crypto_keys
    ALTER COLUMN created_at SET DEFAULT now();
