# Per-Plugin PostgreSQL Role-Based Schema Isolation

**Bead:** holomush-fwan (SEC-01) | **Status:** Draft | **Date:** 2026-04-05

## Overview

The `SchemaProvisioner` creates per-plugin PostgreSQL schemas but passes the
server's own database credentials with only `search_path` changed. A plugin
can trivially `SET search_path TO public` and read/write all server data
including player credentials, session tokens, and other plugins' schemas.

This design adds per-plugin PostgreSQL roles with restricted privileges so
that each plugin can only access its own schema. The server's database role
gains `CREATEROLE` to manage plugin roles at runtime. Passwords are ephemeral
(regenerated on every server start) and never persisted.

## RFC2119 Keywords

The keywords MUST, MUST NOT, SHOULD, SHOULD NOT, and MAY are used per RFC2119.

## Design Decisions

| #  | Decision                                    | Rationale                                                                                          |
| -- | ------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| D1 | Per-plugin PostgreSQL roles, not shared      | Each plugin gets its own restricted role. Shared roles allow cross-plugin data access.              |
| D2 | Ephemeral passwords (regenerated on startup) | Plugins are go-plugin subprocesses that start/stop with the server. No persistent secret storage.  |
| D3 | Server role requires CREATEROLE              | Minimal privilege escalation. Server can create/alter/drop roles but cannot bypass access controls. |
| D4 | Future migration path to cert auth           | Existing CA infrastructure (`internal/tls`) supports client cert generation. Password auth now, cert auth later. Interface structured for clean swap. |
| D5 | Test infrastructure uses non-superuser role  | Production parity. Testing with superuser masks permission bugs this feature is designed to prevent. |
| D6 | E2E guard check, not concurrent projects     | E2E tests are heavyweight. Concurrent runs fight over Docker resources. Fail fast is honest.       |

## 1. Role Naming Convention

Plugin roles follow the pattern `holomush_plugin_<sanitized_name>`, using the
same sanitization as `pluginSchemaName()` (hyphens become underscores).

| Plugin name    | Role name                      | Schema name          |
| -------------- | ------------------------------ | -------------------- |
| `core-scenes`  | `holomush_plugin_core_scenes`  | `plugin_core_scenes` |
| `discord`      | `holomush_plugin_discord`      | `plugin_discord`     |
| `dnd5e-system` | `holomush_plugin_dnd5e_system` | `plugin_dnd5e_system`|

New helper: `pluginRoleName(name string) string` returns
`"holomush_plugin_" + strings.ReplaceAll(name, "-", "_")`.

## 2. Password Generation

Passwords are 32 bytes (256 bits) from `crypto/rand`, base64url-encoded
(43 characters). Generated fresh per `ProvisionSchema()` call. Never written
to disk, database, or logs.

256 bits exceeds NIST symmetric key recommendations (128 bits) and is
quantum-resistant under Grover's algorithm. For an ephemeral password that
exists only in server process memory, this is more than sufficient.

New helper: `generatePassword() (string, error)`.

## 3. Startup Validation

`SchemaProvisioner.Init()` MUST validate that the server's database role has
the `CREATEROLE` privilege before proceeding:

```sql
SELECT rolcreaterole FROM pg_roles WHERE rolname = current_user
```

If the check returns `false`, `Init()` MUST return error code
`SCHEMA_INSUFFICIENT_PRIVILEGES` with a message explaining:

> Server database role "%s" lacks CREATEROLE privilege. Run: ALTER ROLE %s CREATEROLE

If the role is not found in `pg_roles`, `Init()` MUST return error code
`SCHEMA_ROLE_NOT_FOUND`.

## 4. Provision Sequence

`ProvisionSchema(ctx, pluginName)` executes the following steps. All DDL runs
on the admin pool (server's own connection with `CREATEROLE`).

### 4.1 Role Creation (idempotent, Go logic)

```go
// Step 1: Check if role exists
var exists bool
err := pool.QueryRow(ctx,
    "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname = $1)",
    roleName,
).Scan(&exists)

// Step 2a: Create if new
// CREATE ROLE holomush_plugin_core_scenes LOGIN PASSWORD '<ephemeral>'

// Step 2b: Refresh password if existing
// ALTER ROLE holomush_plugin_core_scenes PASSWORD '<ephemeral>'
```

Role names are sanitized via `pgx.Identifier` to prevent SQL injection.
Passwords are passed as string literals (not query parameters) because
`CREATE ROLE ... PASSWORD` does not support `$1` placeholders in PostgreSQL.
The password is base64url-encoded (alphanumeric + `-_=`), eliminating
injection risk from the password value itself.

### 4.2 Schema Creation

```sql
CREATE SCHEMA IF NOT EXISTS plugin_core_scenes;
ALTER SCHEMA plugin_core_scenes OWNER TO holomush_plugin_core_scenes;
```

`ALTER SCHEMA ... OWNER TO` is required for idempotency because
`CREATE SCHEMA IF NOT EXISTS ... AUTHORIZATION` ignores the `AUTHORIZATION`
clause when the schema already exists.

### 4.3 Isolation Grants

```sql
-- Plugin can use its own schema
GRANT USAGE, CREATE ON SCHEMA plugin_core_scenes TO holomush_plugin_core_scenes;

-- Block public schema access
REVOKE ALL ON SCHEMA public FROM holomush_plugin_core_scenes;
```

Revoking `USAGE` on the `public` schema is sufficient to block all object
access within it. Without schema-level `USAGE`, the role cannot access any
tables, sequences, or functions inside that schema, even if object-level
grants exist.

Cross-plugin isolation is implicit: plugin roles are never granted access to
other `plugin_*` schemas.

### 4.4 Connection String

The returned connection string uses the plugin role's credentials:

```text
postgres://holomush_plugin_core_scenes:<password>@host:port/dbname?search_path=plugin_core_scenes
```

Built by replacing user and password in the base connection string. The
existing `scopedConnString` helper is extended into `pluginConnString` that
also swaps credentials.

## 5. Teardown (Purge)

The admin `plugin purge <name>` command executes:

```sql
DROP OWNED BY holomush_plugin_core_scenes;
DROP ROLE IF EXISTS holomush_plugin_core_scenes;
```

`DROP OWNED BY` cascades through everything the role owns: schema, tables,
sequences, indexes. The role can then be dropped cleanly.

**Unload** (server shutdown, `plugin disable`) does NOT touch the role or
schema. Data persists across restarts. The next startup regenerates the
password via `ALTER ROLE ... PASSWORD`.

New method: `PurgeSchema(ctx context.Context, pluginName string) error`.

## 6. API Surface Changes

### SchemaProvisioner

| Method                        | Current behavior                    | New behavior                                                           |
| ----------------------------- | ----------------------------------- | ---------------------------------------------------------------------- |
| `Init(ctx)`                   | Opens admin pool, pings             | + validates `CREATEROLE` privilege                                     |
| `ProvisionSchema(ctx, name)`  | Creates schema, returns conn string | + creates/refreshes role, sets ownership, revokes public, returns plugin-credentialed conn string |
| `PurgeSchema(ctx, name)`      | *does not exist*                    | `DROP OWNED BY` + `DROP ROLE`                                          |
| `Close()`                     | Closes pool                         | No change                                                              |

The external signature of `ProvisionSchema` is unchanged:
`(context.Context, string) -> (string, error)`. Callers are unaffected.

### New Internal Helpers

| Helper                                              | Purpose                                                |
| --------------------------------------------------- | ------------------------------------------------------ |
| `pluginRoleName(name string) string`                | `"holomush_plugin_" + sanitize(name)`                  |
| `generatePassword() (string, error)`                | 32 bytes `crypto/rand`, base64url                      |
| `pluginConnString(base, schema, role, pw) (string, error)` | Extends `scopedConnString` to swap user/password |

## 7. Test Infrastructure Hardening

### 7.1 Non-Superuser Server Role

All test environments MUST use a non-superuser `holomush` role with
`CREATEROLE`, matching production behavior. Testing with superuser masks
permission bugs.

**Init script** (`docker/postgres/init-role.sql`):

```sql
CREATE ROLE holomush LOGIN PASSWORD 'holomush' CREATEROLE;
CREATE DATABASE holomush OWNER holomush;
GRANT ALL ON DATABASE holomush TO holomush;
CREATE DATABASE holomush_test OWNER holomush;
GRANT ALL ON DATABASE holomush_test TO holomush;
```

### 7.2 Compose Changes

`compose.yaml` postgres service:

- Remove `POSTGRES_USER` and `POSTGRES_PASSWORD` (defaults to `postgres`
  superuser for container bootstrap only)
- Add volume mount:
  `./docker/postgres/init-role.sql:/docker-entrypoint-initdb.d/01-init-role.sql`
- Connection strings remain `postgres://holomush:holomush@...` (unchanged)
- `pg_isready` healthcheck changes to `-U postgres` (the container superuser)

`compose.e2e.yaml` inherits the base postgres service; no additional changes.

### 7.3 Testcontainer Changes

Each integration test suite switches from:

```go
postgres.WithUsername("holomush"),
postgres.WithPassword("holomush"),
postgres.WithDatabase("holomush_test"),
```

to:

```go
postgres.WithDatabase("postgres"),
postgres.WithUsername("postgres"),
postgres.WithPassword("postgres"),
postgres.WithInitScripts("testdata/init-role.sql"),
```

Tests then connect as `holomush:holomush@.../holomush_test`.

A shared test helper `testutil.StartPostgres(ctx) (container, connString,
error)` centralizes this pattern across the ~8 integration test suites.

### 7.4 E2E Concurrency Guard

`task test:e2e` MUST check for an already-running `holomush-e2e` compose
project before starting. If containers exist, it MUST fail fast with:

> E2E infrastructure already running (project: holomush-e2e). Stop it with:
> docker compose -p holomush-e2e down -v

Implementation: `docker compose -p holomush-e2e ps -q` returns container IDs
if the project exists. A non-empty result triggers the error.

## 8. SchemaProvisioner Integration Tests

All tests use testcontainers with the non-superuser `holomush` role.

| Test                               | Description                                                                                      |
| ---------------------------------- | ------------------------------------------------------------------------------------------------ |
| Provision creates role and schema  | `ProvisionSchema` → verify role in `pg_roles`, schema in `pg_namespace`, schema owner matches    |
| Plugin can use its own schema      | Connect as plugin role → `CREATE TABLE` → `INSERT` → `SELECT` succeeds                          |
| Plugin cannot access public schema | Connect as plugin role → `SET search_path TO public` → `SELECT` fails with permission denied     |
| Cross-plugin isolation             | Provision two plugins → plugin A cannot `SELECT` from plugin B's schema                          |
| Idempotent provision               | Call `ProvisionSchema` twice → no error, password refreshed, existing tables intact               |
| Purge removes role and schema      | Provision → create tables → `PurgeSchema` → verify role gone, schema gone                        |
| Missing CREATEROLE fails startup   | Create restricted role without `CREATEROLE` → `Init()` returns `SCHEMA_INSUFFICIENT_PRIVILEGES`  |

## 9. Future: Certificate Authentication

The existing CA infrastructure (`internal/tls`) supports
`GenerateClientCert(ca, name)` and `LoadClientTLS(certsDir, name, gameID)`.
A future migration to PostgreSQL `cert` auth would:

1. Generate a client certificate per plugin role via the existing CA
2. Configure PostgreSQL HBA for `cert` auth on plugin roles
3. Pass cert/key paths instead of passwords in connection strings

The `SchemaProvisioner` is structured so the credential strategy (password
generation vs. cert issuance) is isolated in helpers. The role creation,
grant/revoke, and teardown SQL remains identical regardless of auth method.

This migration is tracked separately and is not part of this spec's scope.
