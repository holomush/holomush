<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 53. Audit WAL Best-Effort Semantics

> [Back to Decision Index](../README.md)

**Review finding:** The spec said denial audit logs "MUST be written
synchronously" with a WAL fallback if the DB write fails, but also specified
graceful degradation (increment counter and drop) when both DB and WAL fail.
These requirements contradict each other — MUST-audit is incompatible with
graceful degradation on dual failure.

**Decision:** Change denial audit writes to SHOULD (best-effort). If both the
database and WAL file are unavailable, log to stderr and increment
`abac_audit_failures_total{reason="db_write_failed"}`. Accept that some denial
audit entries may be lost during catastrophic failures. Additionally,
standardize the WAL file path to `$XDG_STATE_HOME/holomush/audit-wal.jsonl`
(XDG_STATE_HOME is semantically correct for transient state) and consolidate
the duplicate WAL descriptions in the spec into a single section.

**Security requirement (S7 - holomush-5k1.353):** WAL fallback for denial and
system_bypass audit events MUST be used when the primary write fails. The SHOULD
applies only to allow events. Denial/bypass event loss creates an unacceptable
security blind spot.

**Rationale:** A pragmatic approach that preserves audit logging during normal
operation while avoiding the impossible contract of guaranteed writes during
infrastructure failure. The stderr fallback ensures operators can still observe
failures through system-level log aggregation. XDG_STATE_HOME is the correct
XDG directory for state data that is not essential to preserve across reinstalls.

**Cross-reference:** Main spec, Audit Log Configuration section; bead
`holomush-3hdt`.
