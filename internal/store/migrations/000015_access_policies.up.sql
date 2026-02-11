-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

CREATE TABLE access_policies (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL UNIQUE,
    description  TEXT,
    effect       TEXT NOT NULL CHECK (effect IN ('permit', 'forbid')),
    source       TEXT NOT NULL DEFAULT 'admin'
                 CHECK (source IN ('seed', 'lock', 'admin', 'plugin')),
    dsl_text     TEXT NOT NULL,
    compiled_ast JSONB NOT NULL,
    enabled      BOOLEAN NOT NULL DEFAULT true,
    seed_version INTEGER DEFAULT NULL,
    created_by   TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    version      INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX idx_policies_enabled ON access_policies(enabled) WHERE enabled = true;

CREATE TABLE access_policy_versions (
    id          TEXT PRIMARY KEY,
    policy_id   TEXT NOT NULL REFERENCES access_policies(id) ON DELETE CASCADE,
    version     INTEGER NOT NULL,
    dsl_text    TEXT NOT NULL,
    changed_by  TEXT NOT NULL,
    changed_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    change_note TEXT,
    UNIQUE(policy_id, version)
);
