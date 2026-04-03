# Database Migrations

HoloMUSH uses [golang-migrate](https://github.com/golang-migrate/migrate) with
embedded SQL files for schema management. Migrations live in
`internal/store/migrations/` and are compiled into the binary via `embed.FS`.

## Baseline

The schema starts from a single baseline migration (`000001_baseline`) that
creates every table needed for the 0.1 release. There is no upgrade path from
pre-0.1 databases. If you need to evolve the schema, add a new migration file
after the baseline.

## Rules

These rules use RFC 2119 keywords (MUST, SHOULD, MAY).

| Rule | Description |
| ---- | ----------- |
| MUST use sequential numbering | `000002_`, `000003_`, etc. |
| MUST provide up and down | Every migration needs both `.up.sql` and `.down.sql` |
| MUST be idempotent | Use `IF NOT EXISTS`, `IF EXISTS`, `ON CONFLICT DO NOTHING` |
| MUST NOT use triggers or functions | All logic lives in Go; PostgreSQL is storage only |
| MUST NOT modify the baseline | Add new migrations instead of editing `000001_baseline` |
| SHOULD keep migrations small | One logical change per migration |
| SHOULD add comments | Explain why, not what |

## Creating a New Migration

1. Pick the next sequence number (check the highest existing file).

2. Create two files:

```text
internal/store/migrations/000002_add_foo.up.sql
internal/store/migrations/000002_add_foo.down.sql
```

3. Add the SPDX license header to both files.

4. Write idempotent SQL in the up migration.

5. Write the reverse in the down migration (the down migration MUST fully undo
   the up migration so that round-trip tests pass).

6. Run the test suite:

```bash
task test && task lint && task test:int
```

## CLI Commands

The `migrate` subcommand manages schema versions:

```bash
holomush migrate            # Apply all pending migrations
holomush migrate up         # Same as above
holomush migrate down       # Roll back one migration
holomush migrate down --all # Roll back all migrations
holomush migrate status     # Show current version and dirty state
holomush migrate version    # Print version number only
holomush migrate force N    # Force version (recovery only)
```

Add `--dry-run` to `up` or `down` to preview without applying.

## Testing

Unit tests in `migrate_embed_test.go` verify that every migration has both an
up and a down file and follows the naming convention. The integration test in
`migrate_integration_test.go` performs a full round trip: up, verify tables,
down, verify empty, up again. Any new migration that breaks the round trip will
fail CI.

## Schema Partitions

The `access_audit_log` table is partitioned by timestamp range. The migration
creates the parent table definition but does not create partitions. The server
creates partitions at bootstrap time. If you add a partitioned table, follow the
same pattern: define the table in the migration, create partitions in Go.
