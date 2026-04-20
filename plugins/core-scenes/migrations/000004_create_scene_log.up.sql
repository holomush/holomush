-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- F5 (holomush-1tvn.12): per-plugin audit storage.
--
-- The host's JetStream audit consumer ack-and-skips any subject matching
-- events.*.scene.> and dispatches those deliveries to this plugin's
-- PluginAuditService.AuditEvent RPC. That RPC INSERTs here.
--
-- Schema lives in the plugin's isolated search_path (plugin_core_scenes),
-- so the unqualified table name resolves correctly.
--
-- Columns mirror the host events_audit table so QueryHistory responses
-- can use the same proto wire format.

CREATE TABLE IF NOT EXISTS scene_log (
    id          BYTEA PRIMARY KEY,
    subject     TEXT NOT NULL,
    type        TEXT NOT NULL,
    timestamp   TIMESTAMPTZ NOT NULL,
    actor_kind  TEXT NOT NULL,
    actor_id    BYTEA,
    payload     BYTEA NOT NULL,
    schema_ver  SMALLINT NOT NULL,
    codec       TEXT NOT NULL,
    js_seq      BIGINT,
    inserted_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Composite (subject, id) index drives QueryHistory's cursor paging.
-- The ULID id is naturally time-ordered, so lexicographic ORDER BY id
-- gives chronological order within a subject.
CREATE INDEX IF NOT EXISTS scene_log_subject_id ON scene_log (subject, id);
