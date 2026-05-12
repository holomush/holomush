-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors
--
-- Drop the FK constraints from crypto_rekey_checkpoints.{new_dek_id,old_dek_id}
-- to crypto_keys(id). Matches the existing precedent for events_audit.dek_ref
-- (no FK to crypto_keys) and prevents an inherited operational dead-end for
-- future DEK hard-cleanup scripts.
--
-- Rationale (holomush-jxo8.7.48):
--
-- Phase 6 of the Rekey lifecycle SOFT-deletes DEK rows (destroyed_at = NOW(),
-- row stays). The FKs from crypto_rekey_checkpoints would not block soft-delete
-- but WOULD block any future hard-cleanup tool (e.g. DELETE FROM crypto_keys
-- WHERE destroyed_at < now() - interval '90 days'). The blocked cleanup forces
-- either (i) lockstep hard-deletion of audit-trail checkpoint rows (data loss
-- on the operator record of a rekey), (ii) a strict archival ordering between
-- checkpoint and DEK pruning (operational coupling), or (iii) a production
-- foot-gun where the cleanup script errors at runtime.
--
-- Following the same shape as events_audit.dek_ref (no FK), application
-- invariants enforce referential integrity:
--
--   - Phase 1 (RunPhase1Fresh) verifies old DEK exists at INSERT time
--     (crypto_rekey_checkpoints.old_dek_id NOT NULL + Phase 1 selects the
--     active DEK row before opening the checkpoint).
--   - Phase 2 (RunPhase2) inserts the new DEK row and stamps new_dek_id on
--     the checkpoint atomically.
--   - Future hard-cleanup scripts can prune old DEK rows freely; checkpoint
--     rows referencing them retain the bigint id as a historical record.
--
-- See docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md §3.6
-- "no-prod-shape-for-undeployed" — design the prod policy now, not later.
--
-- Constraint names are PostgreSQL defaults (table_column_fkey). IF EXISTS so
-- the migration applies cleanly to fresh DBs where 000031 may not yet have
-- run (defensive — the migration chain is sequential, but idempotency is
-- the convention).

ALTER TABLE crypto_rekey_checkpoints
    DROP CONSTRAINT IF EXISTS crypto_rekey_checkpoints_new_dek_id_fkey;

ALTER TABLE crypto_rekey_checkpoints
    DROP CONSTRAINT IF EXISTS crypto_rekey_checkpoints_old_dek_id_fkey;
