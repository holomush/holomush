-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- +goose Up
-- Add justification column to crypto_rekey_checkpoints so that the
-- RunByRequestID explicit-resume path can rehydrate the operator's
-- original justification into the Phase 7 audit payload (holomush-jxo8.7.55).

ALTER TABLE crypto_rekey_checkpoints
    ADD COLUMN IF NOT EXISTS justification text NOT NULL DEFAULT '';

-- +goose Down
-- Reverse: remove justification column from crypto_rekey_checkpoints.

ALTER TABLE crypto_rekey_checkpoints
    DROP COLUMN IF EXISTS justification;
