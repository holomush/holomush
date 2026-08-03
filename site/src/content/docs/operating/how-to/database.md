---
title: "Database Management"
---

HoloMUSH uses PostgreSQL as its primary data store. This guide covers database
setup, migrations, and maintenance.

## Prerequisites

- PostgreSQL 18 or later
- A database user with CREATE privileges
- The `pg_trgm` extension (for fuzzy search)

## Database Migrations

HoloMUSH uses [goose](https://github.com/pressly/goose) for schema management.
Migrations are embedded in the binary and run automatically on startup, or can
be managed manually. Applied versions are recorded in the `goose_db_version`
table.

### Running Migrations

```bash
# Apply all pending migrations
holomush migrate up

# Preview migrations without executing
holomush migrate up --dry-run

# Check current migration status
holomush migrate status

# Rollback one migration
holomush migrate down
```

There is no version-forcing command. goose applies a migration's body and writes
its `goose_db_version` row inside the **same transaction**, so a migration either
applies and is recorded or does neither. The half-applied, half-recorded state
that a force command existed to repair cannot arise.

### Automatic Migrations

By default, HoloMUSH runs migrations automatically on startup. This can be
controlled with environment variables:

```bash
# Disable automatic migrations
HOLOMUSH_DB_AUTO_MIGRATE=false

# Run migrations manually
holomush migrate up
```

### Creating New Migrations

For creating new migration files, see the [Contributing Guide](/contributing/reference/coding-standards/).

### First boot after the goose cutover

Databases whose bookkeeping was written by the previous migration tooling
(golang-migrate, table `schema_migrations`) are adopted **automatically** the
first time the new binary runs `holomush migrate up` — including the startup
auto-migration. There is no script to run and no flag to set.

Inside a single transaction, guarded by a Postgres advisory lock, the adopt gate:

1. Seeds `goose_db_version` with one row per migration the old ledger recorded as
   applied, in ascending version order, **plus goose's own version-0 bootstrap
   row** — so a fully-migrated database ends up with **45 rows** (44 migrations
   at `version_id > 0`, and the version-0 row).
2. Renames `schema_migrations` to `schema_migrations_pre_goose`, keeping the
   forensic record of the version cut over from.

This write is **unattended and one-way**. There is no undo command; the only
rollback is the surgical procedure in
[Restoring a Postgres Backup](/operating/how-to/sandbox/sandbox-restore/), and it
is valid for one deploy window only. **Rehearse the cutover first** using the
pre-deploy rehearsal in that same document — the adopt gate trusts the recorded
version and performs no schema verification, so a rehearsal against restored real
data is the only place schema drift would surface.

If the old ledger is marked **dirty**, adopt refuses and aborts boot rather than
recording a partially-applied migration as applied. Resolve the dirty version
with the previous tooling before deploying the new binary.

#### `migrate status` before the cutover reports version 0

On a database that has not yet been adopted, `holomush migrate status` and
`holomush migrate version` report **version 0 with 44 pending migrations** —
every migration appearing unapplied. This is expected, not data loss: adopt runs
only from `migrate up`, so goose's ledger is still empty when a read-only verb
inspects it. The first `holomush migrate up` (or the startup auto-migration)
corrects the reading.

Running `status` first is **safe**. It creates an empty ledger as a side effect,
and the adopt gate seeds that ledger rather than mistaking it for a completed
cutover. A diagnostic never triggers the irreversible write.

> The legacy `scripts/bootstrap-migrations.sql` is superseded and no longer
> runnable. Do not look for a hand-run adopt path — the automatic gate above is
> the only one.

## Connection Configuration

Configure the database connection via environment variables:

| Variable       | Description                    | Default  |
| -------------- | ------------------------------ | -------- |
| `DATABASE_URL` | Full PostgreSQL connection URL | Required |

**Example connection URL:**

```bash
DATABASE_URL="postgres://holomush:secret@localhost:5432/holomush?sslmode=require"
```

## Backup and Recovery

### Creating Backups

```bash
# Full database dump
pg_dump -Fc holomush > holomush_$(date +%Y%m%d_%H%M%S).dump

# Schema only
pg_dump -Fc --schema-only holomush > holomush_schema.dump

# Data only
pg_dump -Fc --data-only holomush > holomush_data.dump
```

### Restoring from Backup

```bash
# Restore full backup
pg_restore -d holomush holomush_backup.dump

# Restore to new database
createdb holomush_restored
pg_restore -d holomush_restored holomush_backup.dump
```

## Troubleshooting

### A Migration Failed Partway Through

It did not. goose runs each migration's statements and the write of its
`goose_db_version` row in one transaction, so a failing migration rolls back
whole and is not recorded. Fix the migration (or the database condition it
tripped over) and re-run `holomush migrate up`; the failed version is still
pending.

The one exception is a migration explicitly marked `-- +goose NO TRANSACTION`,
which some Postgres statements require (for example `CREATE INDEX CONCURRENTLY`).
Those are not atomic by construction. Inspect the schema and re-run.

```bash
# See which versions are applied and which are pending
holomush migrate status
```

### Connection Issues

Check PostgreSQL is running and accessible:

```bash
psql $DATABASE_URL -c "SELECT 1"
```

Verify the `pg_trgm` extension is available:

```sql
CREATE EXTENSION IF NOT EXISTS pg_trgm;
```

## Error Codes Reference

Migration commands return structured error codes to help diagnose issues.

### Migration Errors

| Code                       | Meaning                                      | Common Causes                                  | Remediation                                                    |
| -------------------------- | -------------------------------------------- | ---------------------------------------------- | -------------------------------------------------------------- |
| `MIGRATION_SOURCE_FAILED`  | Failed to read embedded migration files      | Corrupted binary, missing migrations           | Rebuild the binary                                             |
| `MIGRATION_INIT_FAILED`    | Failed to connect to database for migrations | Invalid DATABASE_URL, database offline         | Check connection string, verify PostgreSQL is running          |
| `MIGRATION_UP_FAILED`      | Failed to apply pending migrations           | SQL syntax error, constraint violation         | Fix the migration SQL or the blocking condition and re-run `migrate up`; the failed version rolled back and is still pending |
| `MIGRATION_DOWN_FAILED`    | Failed to rollback migrations                | SQL error in down migration, missing table     | Check down migration SQL, verify schema state                  |
| `MIGRATION_STEPS_FAILED`   | Failed to apply/rollback specific steps      | Same as UP/DOWN errors                         | Check specific migration file                                  |
| `MIGRATION_VERSION_FAILED` | Failed to read current version               | Database connection lost, `goose_db_version` unreadable | Check connection and that the migration user can read `goose_db_version` |
| `MIGRATION_CLOSE_FAILED`   | Failed to close migrator cleanly             | Connection already closed                      | Usually safe to ignore                                         |

### Cutover (Adopt) Errors

These fire only during the one-shot adoption of a pre-goose database, from
`holomush migrate up` (including the startup auto-migration). None of them can
leave the bookkeeping half-written: the whole adopt runs in one transaction.

| Code                            | Meaning                                                | Common Causes                                            | Remediation                                                                                                                                          |
| ------------------------------- | ------------------------------------------------------ | -------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| `MIGRATION_ADOPT_REFUSED_DIRTY` | The old ledger is marked dirty; adopt refused to seed  | A previous migration failed partway under the old tooling | The database was left dirty by the **old** tooling. Resolve that version with the old binary before deploying this one — the new binary deliberately refuses rather than recording a partially-applied migration as applied. The error names the version. |
| `MIGRATION_ADOPT_LOCK_FAILED`   | Could not acquire the advisory lock guarding the adopt | Connection lost, another replica holding the lock         | Retry. If it persists, check for a stuck session holding a Postgres advisory lock.                                                                   |
| `MIGRATION_ADOPT_PROBE_FAILED`  | Could not read the existing bookkeeping tables         | Connection lost, insufficient privileges                  | Verify the migration user can read `schema_migrations` and `goose_db_version`                                                                        |
| `MIGRATION_ADOPT_SEED_FAILED`   | Could not write the seeded `goose_db_version` rows     | Connection lost, insufficient privileges                  | Nothing was written — the transaction rolled back. Fix the cause and re-run `migrate up`.                                                             |
| `MIGRATION_ADOPT_RENAME_FAILED` | Could not rename `schema_migrations`                   | A `schema_migrations_pre_goose` table already exists      | Nothing was written. Inspect both tables; a pre-existing `schema_migrations_pre_goose` means an adopt was already attempted.                          |

### CLI Errors

| Code                             | Meaning                                    | Common Causes                                | Remediation                                    |
| -------------------------------- | ------------------------------------------ | -------------------------------------------- | ---------------------------------------------- |
| `MIGRATION_VERSION_CHECK_FAILED` | Migration applied but version check failed | Database connection dropped during operation | Run `migrate status` to verify actual state    |
| `CONFIG_INVALID`                 | Missing required configuration             | DATABASE_URL not set                         | Set DATABASE_URL environment variable          |

### Reading Error Output

Errors include context to help diagnose issues:

```text
MIGRATION_UP_FAILED: migration failed
  operation: apply migration 7
  error: pq: relation "objects" already exists
```

The nested context shows:

- **Error code**: Quick identification of failure type
- **Operation**: What the system was trying to do
- **Error**: Underlying database or system error
