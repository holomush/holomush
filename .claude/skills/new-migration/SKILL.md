---
name: new-migration
description: Create a new database migration as a single goose file with Up and Down sections, following project conventions
disable-model-invocation: true
---

# New Migration

Create a PostgreSQL migration using goose conventions: **one file per version**,
carrying both directions.

## Usage

```text
/new-migration <name>
```

Where `<name>` is a snake_case description (e.g., `add_player_inventory`).

## Steps

1. **Pick the next version number.** List `internal/store/migrations/` and take
   the highest `NNNNNN_` prefix plus one, padded to **six** digits:

   ```bash
   ls internal/store/migrations/*.sql | tail -1
   ```

   There is no generator task. goose's own `create` command numbers sequential
   migrations with five digits, which does not match this project's six-digit
   convention (pinned by `internal/store/migrate_embed_test.go`), so the file is
   written by hand from the template below.

2. **Create one file** at `internal/store/migrations/NNNNNN_<name>.sql` from this
   template:

   ```sql
   -- SPDX-License-Identifier: Apache-2.0
   -- Copyright 2026 HoloMUSH Contributors

   -- +goose Up

   -- +goose Down
   ```

3. **Populate the `-- +goose Up` section** with the requested schema changes.
   Follow these conventions:
   - Use `IF NOT EXISTS` for `CREATE TABLE`, `CREATE INDEX`
   - Use `NOT NULL` with sensible defaults
   - Use `ULID` (CHAR(26)) for entity IDs, matching the Go `ulid.ULID` type
   - Use `BIGINT` epoch-nanoseconds for all timestamps — **never** `TIMESTAMPTZ`
     or `TIMESTAMP` (INV-STORE-1; `task lint:no-timestamptz` rejects them):

     ```sql
     created_at BIGINT NOT NULL DEFAULT (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT
     ```

   - Add appropriate indexes for foreign keys and query patterns
   - Add `COMMENT ON TABLE/COLUMN` for non-obvious fields

4. **Populate the `-- +goose Down` section** in the same file with the exact
   reverse:
   - `DROP TABLE IF EXISTS` in reverse order of creation
   - `DROP INDEX IF EXISTS` for any standalone indexes
   - The down section MUST cleanly reverse the up section

5. **Wrap any dollar-quoted body.** goose splits statements on semicolons line by
   line, so a `DO $$ … END $$;` block must be bracketed:

   ```sql
   -- +goose StatementBegin
   DO $$
   BEGIN
     IF to_regclass('public.foo') IS NOT NULL THEN
       ALTER TABLE public.foo ADD COLUMN IF NOT EXISTS label TEXT;
     END IF;
   END $$;
   -- +goose StatementEnd
   ```

   Without the wrapper Postgres rejects the torn fragment with
   `unterminated dollar-quoted string`. `task test` runs
   `TestEveryDollarQuotedMigrationBodyIsWrappedInStatementBeginEnd`, which names
   any unwrapped body by file and line.

6. **Verify** the migration compiles into the embed and passes the format guards:

   ```bash
   task build && task test -- ./internal/store/
   ```

7. **Show the user** the migration number and the file path for review.

## Conventions

- Migration names: `snake_case`, descriptive (e.g., `add_scene_tags`,
  `alter_objects_add_properties`)
- Foreign keys: always name them explicitly (`CONSTRAINT fk_<table>_<column>`)
- Indexes: `idx_<table>_<column(s)>`
- goose already wraps each migration in a transaction, so the body must not open
  one of its own. If a statement cannot run inside a transaction (e.g.
  `CREATE INDEX CONCURRENTLY`), put `-- +goose NO TRANSACTION` at the top of the
  file instead — and accept that the migration is then no longer atomic.
- goose reads **any** `--` comment containing the annotation prefix as an
  annotation and hard-errors on an unrecognised one, so do not mention that
  prefix in passing inside a migration comment.
- Existing migrations are in `internal/store/migrations/` — check them for
  patterns; `000052_events_audit_partition.sql` is the reference for wrapped
  blocks.
- Full rules: `.claude/rules/database-migrations.md`
