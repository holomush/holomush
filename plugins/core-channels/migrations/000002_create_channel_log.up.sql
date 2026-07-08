-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Per-plugin audit storage for channel events.
--
-- The host's JetStream audit consumer ack-and-skips any subject matching
-- events.*.channel.> and dispatches those deliveries to this plugin's
-- PluginAuditService.AuditEvent RPC (served by plan 01-06). That RPC INSERTs
-- here. QueryHistory for channel subjects routes to the plugin's
-- PluginAuditService.QueryHistory.
--
-- Columns mirror the host events_audit table (and scene_log) so QueryHistory
-- responses reuse the same proto wire format — MINUS dek_ref / dek_version:
-- channel events are PLAINTEXT (D-04, sensitivity: never). There are no
-- per-event DEK columns because channels declare no crypto.emits.

CREATE TABLE IF NOT EXISTS channel_log (
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

-- Composite (subject, id) index drives QueryHistory's cursor paging. The ULID
-- id is naturally time-ordered, so lexicographic ORDER BY id gives
-- chronological order within a subject.
CREATE INDEX IF NOT EXISTS channel_log_subject_id ON channel_log (subject, id);
