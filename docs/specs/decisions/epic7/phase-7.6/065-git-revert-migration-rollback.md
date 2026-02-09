<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 65. Git Revert as Migration Rollback Strategy

> [Back to Decision Index](../README.md)

**Question:** How do we roll back the migration from `StaticAccessControl` to
`AccessPolicyEngine` if serious issues are discovered after Task 28?

**Options Considered:**

1. **Feature flag** — Toggle between old and new authorization at runtime.
2. **Adapter layer** — Maintain backward-compatible wrapper that can fall back.
3. **Git revert** — Revert the migration commit(s) to restore original call sites.

**Decision:** Use `git revert` of the Task 28 migration commit(s) as the
rollback strategy. Since there is no adapter layer ([Decision #36](036-direct-replacement-no-adapter.md))
and no shadow mode ([Decision #37](037-no-shadow-mode.md)), the migration from
static access control to ABAC is a direct replacement. Reverting Task 28's
commit(s) restores all ~28 `AccessControl.Check()` call sites to their
pre-migration state.

**Rationale:** HoloMUSH has no production releases. Building feature flags or
adapter layers for rollback adds complexity for a system with no deployed users.
Git revert is simple, well-understood, and deterministic. Comprehensive test
coverage (>80% per package) serves as the primary safety net — issues should be
caught before merge, not after deployment. Each package migration commit can be
reverted independently if needed.

**Cross-reference:** [Decision #36](036-direct-replacement-no-adapter.md) (no
adapter), [Decision #37](037-no-shadow-mode.md) (no shadow mode), Phase 7.6
Task 28.

---

## Database Migration Rollback Procedure

**Context:** The ABAC implementation creates 3 database migration files in Phase
7.1 (Tasks 1-3) that introduce 4 new tables. If a critical issue is discovered
after Phase 7.1-7.5 deployment, operators need a documented procedure to reverse
both code and database changes.

### Migration Files

Phase 7.1 creates the following migrations (relative numbers assume `000014_aliases`
is the latest existing migration):

| Migration                  | Tables Created                              | Task             |
| -------------------------- | ------------------------------------------- | ---------------- |
| `000015_access_policies`   | `access_policies`, `access_policy_versions` | Phase 7.1 Task 1 |
| `000016_access_audit_log`  | `access_audit_log` (partitioned table)      | Phase 7.1 Task 2 |
| `000017_entity_properties` | `entity_properties`                         | Phase 7.1 Task 3 |

**Note:** Migration numbers may differ if other migrations merge before ABAC
implementation. Use the actual migration file names from your deployment.

### Rollback Strategy

**Code Rollback:** Use `git revert` to reverse Task 28 migration commits (Phase
7.6) as documented above. This restores `AccessControl.Check()` call sites.

**Database Rollback:** Run down migrations in reverse order to remove ABAC
tables. Down migrations MUST be run in reverse dependency order to avoid foreign
key constraint violations.

### Down-Migration Order

Run down migrations in **reverse order** of creation:

```bash
# Step 1: Verify current schema version
# (Implementation-specific: use your migration tool's status command)
# Example: migrate -database "postgres://..." -path db/migrations version

# Step 2: Back up existing data (CRITICAL)
pg_dump -h localhost -U holomush -d holomush \
  -t access_policies \
  -t access_policy_versions \
  -t access_audit_log \
  -t access_audit_log_* \
  -t entity_properties \
  --file=abac_backup_$(date +%Y%m%d_%H%M%S).sql

# Step 3: Run down migrations in reverse order
migrate -database "postgres://..." -path db/migrations down 1  # 000017 down
migrate -database "postgres://..." -path db/migrations down 1  # 000016 down
migrate -database "postgres://..." -path db/migrations down 1  # 000015 down

# Step 4: Verify rollback
psql -h localhost -U holomush -d holomush -c "\dt access_*"
psql -h localhost -U holomush -d holomush -c "\dt entity_properties"
# Expected: No ABAC tables present
```

### Down-Migration Steps Per File

**Migration 000017 (`entity_properties`) — Rollback First:**

```sql
-- internal/store/migrations/000017_entity_properties.down.sql
DROP TABLE IF EXISTS entity_properties;
```

**Rationale:** No foreign key dependencies. Safe to drop first.

**Migration 000016 (`access_audit_log`) — Rollback Second:**

```sql
-- internal/store/migrations/000016_access_audit_log.down.sql
DROP TABLE IF EXISTS access_audit_log;
```

**Rationale:** Partitioned table with child partitions. PostgreSQL automatically
drops child partitions (`access_audit_log_YYYY_MM`) when the parent is dropped.
No explicit partition cleanup needed in down migration.

**Data Loss:** Audit log records are deleted. Backup MUST be taken before rollback
(see Step 2 above).

**Migration 000015 (`access_policies`) — Rollback Last:**

```sql
-- internal/store/migrations/000015_access_policies.down.sql
DROP TABLE IF EXISTS access_policy_versions;
DROP TABLE IF EXISTS access_policies;
```

**Rationale:** `access_policy_versions` has a foreign key to `access_policies`.
Drop child table first, then parent. The down migration already handles this
correctly.

**Data Loss:** All policies and policy version history are deleted. Backup MUST
be taken before rollback.

### Pre-Rollback Data Backup Recommendations

Before running down migrations, operators MUST:

1. **Take a full database backup:**

   ```bash
   pg_dump -h localhost -U holomush -d holomush --file=full_backup_$(date +%Y%m%d_%H%M%S).sql
   ```

2. **Export ABAC tables separately for faster restore if rollback fails:**

   ```bash
   pg_dump -h localhost -U holomush -d holomush \
     -t access_policies \
     -t access_policy_versions \
     -t access_audit_log \
     -t access_audit_log_* \
     -t entity_properties \
     --file=abac_backup_$(date +%Y%m%d_%H%M%S).sql
   ```

3. **Export critical policies as JSON for manual recreation:**

   ```bash
   psql -h localhost -U holomush -d holomush -c \
     "COPY (SELECT id, name, dsl_text, effect, enabled, seed_version FROM access_policies) \
      TO STDOUT WITH CSV HEADER" > policies_export_$(date +%Y%m%d_%H%M%S).csv
   ```

4. **Verify backup integrity:**

   ```bash
   # Test restore to a temporary database
   createdb holomush_test_restore
   psql -h localhost -U holomush -d holomush_test_restore < abac_backup_*.sql
   dropdb holomush_test_restore
   ```

### Audit Log Partition Considerations

The `access_audit_log` table uses monthly range partitioning. Each partition is
a separate child table (`access_audit_log_2026_02`, `access_audit_log_2026_03`,
etc.). When `DROP TABLE access_audit_log` is executed, PostgreSQL automatically
drops all child partitions.

**Backup note:** `pg_dump` with `-t access_audit_log_*` captures all partitions.
Ensure the wildcard pattern is used in backups to avoid missing partition data.

### Rollback Testing

Before deploying ABAC to production, operators SHOULD:

1. **Test rollback in staging environment:**
   - Deploy Phase 7.1-7.5 to staging
   - Seed test policies and audit data
   - Run full rollback procedure (code + DB down migrations)
   - Verify system returns to pre-ABAC state

2. **Verify backup/restore cycle:**
   - Take ABAC table backup
   - Run down migrations
   - Restore from backup
   - Verify data integrity

3. **Document rollback duration:**
   - Measure time to run down migrations on production-scale data
   - Factor into incident response planning

### Recovery Path After Rollback

If rollback is executed due to a critical issue:

1. **Root cause analysis:** Identify the issue that triggered rollback (bug,
   performance, configuration error)
2. **Fix forward:** Address the issue in the codebase
3. **Re-migration:** Deploy corrected ABAC implementation with up migrations
4. **Policy restoration:** If policies were backed up, restore them after
   re-migration:

   ```bash
   psql -h localhost -U holomush -d holomush < abac_backup_YYYYMMDD_HHMMSS.sql
   ```

### When NOT to Roll Back

Rollback is a destructive operation. Consider alternatives first:

- **Performance issue:** Tune queries, add indexes, adjust cache settings
- **Policy misconfiguration:** Edit policies via admin commands, not rollback
- **Partial deployment:** If Task 28 migration is incomplete, fix forward by
  completing remaining packages
- **Test failures in CI:** Revert commits before merge, not after deployment

Rollback is appropriate for:

- **Critical security vulnerability** in ABAC engine logic
- **Data corruption** caused by ABAC migration
- **Cascading failures** preventing system recovery
- **Irrecoverable state** requiring fresh start

### Monitoring During Rollback

During rollback procedure, monitor:

1. **Database connection count:** Down migrations may briefly spike connections
2. **Disk usage:** Backup files consume additional disk space
3. **Query latency:** Large table drops may cause brief lock contention
4. **Application logs:** Verify no services are attempting ABAC operations during
   rollback

### Post-Rollback Verification

After completing rollback:

1. **Verify schema state:**

   ```bash
   psql -h localhost -U holomush -d holomush -c "\dt" | grep -E "access_|entity_properties"
   # Expected: No results (ABAC tables absent)
   ```

2. **Verify application startup:**

   ```bash
   # Start server with pre-ABAC code (post-git-revert)
   # Verify no ABAC-related errors in logs
   ```

3. **Run integration tests:**

   ```bash
   task test:integration
   # Verify legacy AccessControl.Check() calls work
   ```

4. **Verify user operations:**
   - Test character creation, location navigation, command execution
   - Confirm authorization works via legacy static evaluator
