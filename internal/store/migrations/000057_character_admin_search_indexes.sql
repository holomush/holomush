-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- +goose Up

-- 000057 — trigram indexes for the admin character search (D-106, ADMIN-03).
--
-- The admin `characters` section searches a SUBSTRING of two columns and only
-- two: characters.normalized_name (the stored normal form of 01-SPEC §6.1.3)
-- and the joined players.username. Both predicates are unanchored ILIKE, which
-- no B-tree can serve, so each searched column gets a GIN trigram index.
--
-- ## The extension is already a hard dependency, and is deliberately not
-- re-declared here
--
-- 000001_baseline.sql:17 creates pg_trgm and :110,:136,:159 already build three
-- gin_trgm_ops indexes on it. Any database that can run migration 1 therefore
-- has the extension. Repeating the CREATE here would read as a NEW deployment
-- requirement introduced by this release, which it is not.
--
-- ## Plain CREATE INDEX, not the concurrent form
--
-- goose wraps each migration in a transaction. The concurrent form cannot run
-- inside one, so choosing it would force this file to opt out of the
-- transaction — surrendering atomicity for two indexes on tables of this size,
-- where the exclusive lock is measured in milliseconds. The trade is not worth
-- making here.

CREATE INDEX IF NOT EXISTS idx_characters_normalized_name_trgm
    ON characters USING gin (normalized_name gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_players_username_trgm
    ON players USING gin (username gin_trgm_ops);

-- +goose Down

-- Reverse order: the players index first, then the characters index. Neither
-- drop is conditional on the other, but reversing keeps the file readable as
-- the inverse of its own Up block.

DROP INDEX IF EXISTS idx_players_username_trgm;

DROP INDEX IF EXISTS idx_characters_normalized_name_trgm;
