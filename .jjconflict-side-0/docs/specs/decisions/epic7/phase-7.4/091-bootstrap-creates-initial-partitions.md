<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 91. Bootstrap Creates Initial Audit Partitions

> [Back to Decision Index](../README.md)

**Question:** Should initial audit log partitions be created via SQL migration or
at bootstrap?

**Context:** The original plan (T2) specified a Go migration file
(`000016_access_audit_log_partitions.go`) for dynamic partition creation. However,
`golang-migrate` expects `.up.sql`/`.down.sql` files — a standalone Go file won't
execute automatically. Without initial partitions, PostgreSQL rejects INSERTs into
the unpartitioned parent table.

Two options were considered:

- **Option A:** Static SQL partitions in `.up.sql` (simpler, creates N fixed
  months)
- **Option B:** Move initial partition creation to the bootstrap sequence (T23)

**Decision:** Option B — initial partition creation happens at bootstrap (T23), not
in SQL migrations. The `.up.sql` migration creates only the partitioned table
structure (DDL). The bootstrap sequence creates initial partitions dynamically based
on the current date.

**Rationale:** Bootstrap already manages runtime initialization (seed policies,
cache warming). Partition creation is date-dependent and benefits from dynamic
calculation rather than static SQL. This also aligns partition creation and
partition maintenance (T19b) under the same runtime subsystem, avoiding split
responsibility between migrations and runtime.

**Cross-reference:** Review finding C2 (PR #69); T2 (Phase 7.1); T23 (Phase 7.4);
T19b (Phase 7.3 — audit retention/partition management); ADR #91 (this decision).
