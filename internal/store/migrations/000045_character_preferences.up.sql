-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Phase 8: per-character preferences (owner-partitioned settings scope).
ALTER TABLE characters ADD COLUMN IF NOT EXISTS preferences JSONB NOT NULL DEFAULT '{}';
