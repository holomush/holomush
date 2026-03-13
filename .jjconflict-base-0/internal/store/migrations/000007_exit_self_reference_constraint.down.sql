-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

ALTER TABLE exits DROP CONSTRAINT IF EXISTS chk_not_self_referential;
