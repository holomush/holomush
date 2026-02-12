-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

CREATE TABLE access_audit_log (
    id               TEXT NOT NULL,
    timestamp        TIMESTAMPTZ NOT NULL DEFAULT now(),
    subject          TEXT NOT NULL,
    original_subject TEXT,           -- Session subject before resolution to character (NULL if no resolution)
    action           TEXT NOT NULL,
    resource         TEXT NOT NULL,
    effect           TEXT NOT NULL CHECK (effect IN ('allow', 'deny', 'default_deny', 'system_bypass')),
    policy_id        TEXT,
    policy_name      TEXT,
    attributes       JSONB,
    error_message    TEXT,
    provider_errors  JSONB,
    duration_us      INTEGER,
    -- Composite PK required for partitioned tables (ADR #111).
    -- PostgreSQL requires the partition key (timestamp) in any primary key.
    -- See: docs/specs/decisions/epic7/phase-7.1/111-audit-log-composite-pk-partitioning.md
    PRIMARY KEY (id, timestamp)
) PARTITION BY RANGE (timestamp);

-- Initial partitions created at bootstrap (T23, Phase 7.4) per ADR #91.
-- PostgreSQL rejects INSERTs into unpartitioned parent tables, so the server
-- MUST create at least one partition before any audit log writes.

CREATE INDEX idx_audit_log_timestamp ON access_audit_log USING BRIN (timestamp)
    WITH (pages_per_range = 128);
CREATE INDEX idx_audit_log_subject ON access_audit_log(subject, timestamp DESC);
CREATE INDEX idx_audit_log_denied ON access_audit_log(effect, timestamp DESC)
    WHERE effect IN ('deny', 'default_deny');
