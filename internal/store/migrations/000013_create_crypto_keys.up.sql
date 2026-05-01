-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

CREATE TABLE IF NOT EXISTS crypto_keys (
    id              BIGSERIAL   PRIMARY KEY,
    context_type    TEXT        NOT NULL,
    context_id      TEXT        NOT NULL,
    version         INTEGER     NOT NULL,
    wrapped_dek     BYTEA       NOT NULL,
    wrap_provider   TEXT        NOT NULL,
    wrap_key_id     TEXT        NOT NULL,
    participants    JSONB       NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    rotated_at      TIMESTAMPTZ,
    superseded_by   BIGINT      REFERENCES crypto_keys(id),
    rekey_audit_id  BYTEA,
    UNIQUE (context_type, context_id, version)
);

CREATE INDEX IF NOT EXISTS crypto_keys_context
    ON crypto_keys (context_type, context_id);

CREATE INDEX IF NOT EXISTS crypto_keys_active
    ON crypto_keys (context_type, context_id)
    WHERE rotated_at IS NULL;
