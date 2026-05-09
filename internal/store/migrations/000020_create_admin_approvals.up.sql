-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Phase 5 sub-epic D: admin_approvals table for dual-control approval rows.
-- Idempotent (project rule per CLAUDE.md): IF NOT EXISTS guards.

CREATE TABLE IF NOT EXISTS admin_approvals (
    request_id              BYTEA PRIMARY KEY,         -- 16-byte ULID
    primary_player_id       BYTEA NOT NULL,
    op_kind                 TEXT NOT NULL,             -- "rekey" | "admin_read_stream"
    op_args_hash            BYTEA NOT NULL,            -- 32-byte SHA-256
    expires_at              TIMESTAMPTZ NOT NULL,
    approved_at             TIMESTAMPTZ NULL,
    approved_by_player_id   BYTEA NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_admin_approvals_pending
    ON admin_approvals (request_id)
    WHERE approved_at IS NULL;
