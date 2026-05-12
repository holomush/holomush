-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors
--
-- Reverse: restore the original FK constraints from
-- crypto_rekey_checkpoints.{new_dek_id,old_dek_id} to crypto_keys(id).
--
-- Note: this rollback only succeeds if every existing checkpoint row's
-- new_dek_id (when non-NULL) and old_dek_id reference a still-extant
-- crypto_keys.id. After production deployment, hard-deleted DEK rows
-- referenced by old checkpoint rows would make this rollback fail. That
-- is an explicit downside of the down migration; the up direction is the
-- intended steady-state.

ALTER TABLE crypto_rekey_checkpoints
    ADD CONSTRAINT crypto_rekey_checkpoints_new_dek_id_fkey
        FOREIGN KEY (new_dek_id) REFERENCES crypto_keys(id);

ALTER TABLE crypto_rekey_checkpoints
    ADD CONSTRAINT crypto_rekey_checkpoints_old_dek_id_fkey
        FOREIGN KEY (old_dek_id) REFERENCES crypto_keys(id);
