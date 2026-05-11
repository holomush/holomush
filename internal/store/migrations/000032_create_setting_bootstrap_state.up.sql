-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors
-- Create setting_bootstrap_state for generic key-value bootstrap metadata
-- used by the content/setting bootstrap subsystem.
--
-- Migration 30 replaced bootstrap_metadata with the auditchain (chain_name, scope_key)
-- schema. This table provides the original (key, value) key-value store shape
-- for the SettingBootstrapper, keeping concerns separate from auditchain primitives.

CREATE TABLE IF NOT EXISTS setting_bootstrap_state (
    key        text        NOT NULL,
    value      text        NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (key)
);
