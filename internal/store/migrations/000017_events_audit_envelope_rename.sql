-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- +goose Up

-- Phase 3d: rename events_audit.payload to envelope.
--
-- The column has always stored the marshaled Event proto envelope bytes
-- (per publisher.go:295,302 — proto.Marshal(envelope) → msg.Data).
-- The original "payload" name is a misnomer: Event.payload is one nested
-- field within the envelope, not the column's contents. This rename
-- clarifies semantics for cold-tier readers and SQL tooling.
--
-- ALTER TABLE ... RENAME COLUMN is metadata-only — no row-level work.
-- Idempotent guard (project rule per CLAUDE.md / AGENTS.md "Every
-- database migration MUST be idempotent"): only rename when the source
-- column is present and the destination is absent, so reruns and
-- partially-reconciled environments stay safe.

-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'public'
      AND table_name = 'events_audit'
      AND column_name = 'payload'
  ) AND NOT EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'public'
      AND table_name = 'events_audit'
      AND column_name = 'envelope'
  ) THEN
    ALTER TABLE events_audit RENAME COLUMN payload TO envelope;
  END IF;
END $$;
-- +goose StatementEnd

-- +goose Down

-- Reverse the Phase 3d rename. Column reverts to its original name.
-- Idempotent symmetric to the up migration: only rename when 'envelope'
-- is present and 'payload' is absent.

-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'public'
      AND table_name = 'events_audit'
      AND column_name = 'envelope'
  ) AND NOT EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'public'
      AND table_name = 'events_audit'
      AND column_name = 'payload'
  ) THEN
    ALTER TABLE events_audit RENAME COLUMN envelope TO payload;
  END IF;
END $$;
-- +goose StatementEnd
