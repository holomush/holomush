<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 111. Audit Log Composite Primary Key for Partitioning

> [Back to Decision Index](../README.md)

**Context:** The spec (05-storage-audit.md) defines `access_audit_log` with `id TEXT`
as the primary key. However, the table uses `PARTITION BY RANGE (timestamp)` for
time-based partitioning to enable efficient partition-drop purging of old audit logs.

**Problem:** PostgreSQL requires that any primary key on a partitioned table MUST
include the partition key. A simple `id TEXT PRIMARY KEY` constraint is rejected
by PostgreSQL with an error:

```text
ERROR: primary key constraint on partitioned table must include the partition key
```

**Decision:** Use a composite primary key `PRIMARY KEY (id, timestamp)` instead of
`id TEXT PRIMARY KEY`.

**Implementation:**

```sql
CREATE TABLE access_audit_log (
    id               TEXT NOT NULL,
    timestamp        TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- ... other columns ...
    -- Composite PK required for partitioned tables
    PRIMARY KEY (id, timestamp)
) PARTITION BY RANGE (timestamp);
```

**Rationale:**

1. **PostgreSQL constraint:** This is not optional — PostgreSQL enforces this at
   DDL execution time. The spec's original schema is technically invalid for a
   partitioned table.

2. **No semantic change:** The `id` column remains a ULID with embedded timestamp,
   ensuring global uniqueness. The composite PK does not change how applications
   query or insert data.

3. **Partition benefits:** Time-based partitioning enables:
   - Efficient `DROP PARTITION` for retention (vs. row-by-row DELETE)
   - BRIN index effectiveness on the timestamp column
   - Smaller index trees per partition

**Spec correction needed:** 05-storage-audit.md should be updated to reflect the
composite PK as the correct schema for partitioned audit tables.

**References:**

- PostgreSQL documentation: [Partitioned Tables](https://www.postgresql.org/docs/current/ddl-partitioning.html)
  — "A primary key or unique constraint on a partitioned table must include all
  the partition key columns."
