<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 35. Audit Log Source Column and No Decision Column

> [Back to Decision Index](../README.md)

**Review finding:** (a) The `access_audit_log` schema had both `decision` and
`effect` columns where `decision` was strictly derivable from `effect`. (b) The
schema had no way to distinguish whether an audit record came from the ABAC
engine, the static adapter, or shadow mode.

**Decision:** (a) Drop the `decision` column. The `effect` column alone
indicates the outcome: `allow` = allowed, `deny`/`default_deny` = denied. (b)
Add a `source TEXT NOT NULL DEFAULT 'admin'` column to `access_policies` to
track where policies originate (`admin`, `lock`, `seed`, `plugin`).

**Validation constraint:** Policies named `seed:*` MUST have `source='seed'`
and vice versa. Policies named `lock:*` MUST have `source='lock'` and vice
versa. Violations are rejected at creation time to prevent naming/source
inconsistencies.

**Rationale:** (a) Redundant columns in append-heavy audit tables waste storage
and create consistency risks. The `effect` column already encodes the decision.
(b) The `source` column enables filtering and lifecycle management — e.g.,
showing only admin-authored policies, or identifying lock-generated policies
for cleanup.
