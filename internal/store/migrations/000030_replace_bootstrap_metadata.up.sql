-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors
-- Replace bootstrap_metadata schema to key on (chain_name, scope_key)
-- instead of legacy (key) — generalizes from policy_set-specific shape
-- to the chain-agnostic auditchain primitive.
--
-- Project rule: no production deployments → DROP+CREATE is acceptable.
-- See docs/superpowers/specs/2026-05-10-event-payload-crypto-phase5-sub-epic-e-design.md §3.6.

DROP TABLE IF EXISTS bootstrap_metadata;

CREATE TABLE IF NOT EXISTS bootstrap_metadata (
    chain_name      text        NOT NULL,
    scope_key       text        NOT NULL,
    initialized_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (chain_name, scope_key)
);
