-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- MODEL-03 (Phase 5, slice 1): add the optimistic-concurrency version column to
-- the four world tables (locations, exits, characters, objects). This is the
-- schema foundation for version-predicated CAS writes/deletes
-- (... WHERE id = $1 AND version = $2) that close last-write-wins (M12, #4798).
--
-- Mirrors the in-schema precedent access_policies.version
-- (000001_baseline.up.sql:269): INTEGER NOT NULL DEFAULT 1. DEFAULT 1 backfills
-- every existing row atomically, so no data-migration step is required and no
-- NOT NULL-without-default lock storm occurs. ADD COLUMN IF NOT EXISTS keeps the
-- migration idempotent and safe to re-run.
--
-- entity_properties is intentionally NOT touched (one-pager §1): the version
-- guard covers only the four core world tables.

ALTER TABLE locations ADD COLUMN IF NOT EXISTS version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE exits ADD COLUMN IF NOT EXISTS version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE characters ADD COLUMN IF NOT EXISTS version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE objects ADD COLUMN IF NOT EXISTS version INTEGER NOT NULL DEFAULT 1;
