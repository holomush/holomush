-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors
DROP TABLE IF EXISTS bootstrap_metadata;
CREATE TABLE bootstrap_metadata (
    key   text PRIMARY KEY,
    value text NOT NULL
);
