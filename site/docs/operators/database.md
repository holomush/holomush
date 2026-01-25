# Database Management

HoloMUSH uses PostgreSQL as its primary data store. This guide covers database
setup, migrations, and maintenance.

## Prerequisites

- PostgreSQL 14 or later
- A database user with CREATE privileges
- The `pg_trgm` extension (for fuzzy search)

## Database Migrations

HoloMUSH uses [golang-migrate](https://github.com/golang-migrate/migrate) for
schema management. Migrations are embedded in the binary and run automatically
on startup, or can be managed manually.

### Running Migrations

```bash
# Apply all pending migrations
task migrate

# Preview migrations without executing
task migrate -- --dry-run

# Check current migration status
task migrate:status

# Rollback one migration
task migrate:down

# Force version (for dirty state recovery)
task migrate:force -- 7
```

### Automatic Migrations

By default, HoloMUSH runs migrations automatically on startup. This can be
controlled with environment variables:

```bash
# Disable automatic migrations
HOLOMUSH_DB_AUTO_MIGRATE=false

# Run migrations manually
task migrate
```

### Creating New Migrations

For development, create new migration files:

```bash
# Install migrate CLI (one-time)
go install -tags 'pgx5' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# Create migration files
task migrate:create -- descriptive_name
# Creates: NNNNNN_descriptive_name.up.sql and NNNNNN_descriptive_name.down.sql
```

**Migration file conventions:**

- Up migrations apply changes (`.up.sql`)
- Down migrations revert changes (`.down.sql`)
- Files are numbered sequentially
- Each migration should be atomic and reversible

### Bootstrapping Existing Databases

If migrating from an older HoloMUSH version with an existing schema:

```bash
psql -d holomush -f scripts/bootstrap-migrations.sql
```

This marks all migrations as applied without running them.

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

### Dirty Migration State

If a migration fails partway through, the database may be in a "dirty" state:

```bash
# Check current state
task migrate:status

# If dirty, fix manually then force version
task migrate:force -- 6  # Force to version 6
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

| Code                       | Meaning                                      | Common Causes                                       | Remediation                                                          |
| -------------------------- | -------------------------------------------- | --------------------------------------------------- | -------------------------------------------------------------------- |
| `MIGRATION_SOURCE_FAILED`  | Failed to read embedded migration files      | Corrupted binary, missing migrations                | Rebuild the binary                                                   |
| `MIGRATION_INIT_FAILED`    | Failed to connect to database for migrations | Invalid DATABASE_URL, database offline              | Check connection string, verify PostgreSQL is running                |
| `MIGRATION_UP_FAILED`      | Failed to apply pending migrations           | SQL syntax error, constraint violation, dirty state | Check migration SQL, resolve conflicts, use `migrate force` if dirty |
| `MIGRATION_DOWN_FAILED`    | Failed to rollback migrations                | SQL error in down migration, missing table          | Check down migration SQL, verify schema state                        |
| `MIGRATION_STEPS_FAILED`   | Failed to apply/rollback specific steps      | Same as UP/DOWN errors                              | Check specific migration file                                        |
| `MIGRATION_VERSION_FAILED` | Failed to read current version               | Database connection lost, schema_migrations missing | Check connection, run bootstrap if needed                            |
| `MIGRATION_FORCE_FAILED`   | Failed to force-set version                  | Database connection lost                            | Check connection                                                     |
| `MIGRATION_CLOSE_FAILED`   | Failed to close migrator cleanly             | Connection already closed                           | Usually safe to ignore                                               |

### CLI Errors

| Code                             | Meaning                                    | Common Causes                                | Remediation                                    |
| -------------------------------- | ------------------------------------------ | -------------------------------------------- | ---------------------------------------------- |
| `MIGRATION_VERSION_CHECK_FAILED` | Migration applied but version check failed | Database connection dropped during operation | Run `migrate status` to verify actual state    |
| `CONFIG_INVALID`                 | Missing required configuration             | DATABASE_URL not set                         | Set DATABASE_URL environment variable          |
| `INVALID_VERSION`                | Invalid version number for force command   | Non-integer input, negative number           | Use positive integer (e.g., `migrate force 6`) |

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
