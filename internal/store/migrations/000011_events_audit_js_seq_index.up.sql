-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- 000011 — index events_audit on (subject, js_seq) for the new history
-- pagination contract (see docs/superpowers/specs/2026-04-21-cold-tier-js-seq-pagination-design.md §5).
-- The events_audit.js_seq column has been NOT NULL since 000009; no data
-- backfill is required.

CREATE INDEX IF NOT EXISTS events_audit_subject_js_seq
  ON events_audit (subject, js_seq);
