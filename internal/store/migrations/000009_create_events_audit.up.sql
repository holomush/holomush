-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

CREATE TABLE IF NOT EXISTS events_audit (
    id           BYTEA       PRIMARY KEY,
    subject      TEXT        NOT NULL,
    type         TEXT        NOT NULL,
    timestamp    TIMESTAMPTZ NOT NULL,
    actor_kind   TEXT        NOT NULL,
    actor_id     BYTEA,
    payload      BYTEA       NOT NULL,
    schema_ver   SMALLINT    NOT NULL,
    codec        TEXT        NOT NULL,
    js_seq       BIGINT      NOT NULL,
    inserted_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS events_audit_subject_id  ON events_audit (subject, id);
CREATE INDEX IF NOT EXISTS events_audit_subject_ts  ON events_audit (subject, timestamp);
CREATE INDEX IF NOT EXISTS events_audit_subject_pat ON events_audit (subject text_pattern_ops);
