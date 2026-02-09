<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

## Policy Storage

### Schema

All `id` columns use ULID format, consistent with project conventions.

```sql
CREATE TABLE access_policies (
    id           TEXT PRIMARY KEY,           -- ULID
    name         TEXT NOT NULL UNIQUE,
    description  TEXT,
    effect       TEXT NOT NULL CHECK (effect IN ('permit', 'forbid')),
    source       TEXT NOT NULL DEFAULT 'admin'
                 CHECK (source IN ('seed', 'lock', 'admin', 'plugin')),
    dsl_text     TEXT NOT NULL,
    compiled_ast JSONB NOT NULL,             -- Pre-parsed AST from PolicyCompiler
    enabled      BOOLEAN NOT NULL DEFAULT true,
    seed_version INTEGER DEFAULT NULL,       -- NULL for operator-created policies, integer for seed policies (tracks upgrade lineage)
    created_by   TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    version      INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX idx_policies_enabled ON access_policies(enabled) WHERE enabled = true;

CREATE TABLE access_policy_versions (
    id          TEXT PRIMARY KEY,           -- ULID
    policy_id   TEXT NOT NULL REFERENCES access_policies(id) ON DELETE CASCADE,
    version     INTEGER NOT NULL,
    dsl_text    TEXT NOT NULL,
    changed_by  TEXT NOT NULL,
    changed_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    change_note TEXT,
    UNIQUE(policy_id, version)
);

-- Phase 7.1 MUST create this table with monthly range partitioning.
-- Retrofitting partitioning onto an existing table requires exclusive locks
-- and full table rewrites — at 10M rows/day in `all` mode, this becomes
-- impractical within days. Partition-drop purging is also far more efficient
-- than row-by-row DELETE. See "Audit Log Retention" section for partition
-- management (creation, detachment, and purging).
CREATE TABLE access_audit_log (
    id               TEXT PRIMARY KEY,         -- ULID
    timestamp        TIMESTAMPTZ NOT NULL DEFAULT now(),
    subject          TEXT NOT NULL,
    original_subject TEXT,                     -- Session subject before resolution to character (NULL if no resolution occurred)
    action           TEXT NOT NULL,
    resource         TEXT NOT NULL,
    effect           TEXT NOT NULL CHECK (effect IN ('allow', 'deny', 'default_deny', 'system_bypass')),
    policy_id        TEXT,
    policy_name      TEXT,
    attributes       JSONB,
    error_message    TEXT,
    provider_errors  JSONB,                   -- e.g., [{"namespace": "reputation", "error": "connection refused", "timestamp": "2026-02-06T12:00:00Z", "duration_us": 1500}]
    duration_us      INTEGER                  -- evaluation duration in microseconds (for performance debugging)
                                              -- NOTE: duration_us is total Evaluate() wall time; provider_errors[].duration_us is individual provider execution time.
                                              -- Sum of provider durations may be less than total due to overhead (attribute resolution, policy matching, audit logging).
);

-- Essential indexes only. The effect column doubles as the decision indicator:
-- allow = allowed, deny/default_deny = denied. No separate decision column needed.
-- BRIN index on timestamp: Audit logs are append-only with natural time ordering,
-- making BRIN ~1-2% the size of B-tree for time-range scans. Subject and effect
-- indexes remain B-tree as their values are not correlated with physical row order.
CREATE INDEX idx_audit_log_timestamp ON access_audit_log USING BRIN (timestamp) WITH (pages_per_range = 128);
CREATE INDEX idx_audit_log_subject ON access_audit_log(subject, timestamp DESC);
CREATE INDEX idx_audit_log_denied ON access_audit_log(effect, timestamp DESC)
    WHERE effect IN ('deny', 'default_deny');

CREATE TABLE entity_properties (
    id            TEXT PRIMARY KEY,         -- ULID
    parent_type   TEXT NOT NULL,
    parent_id     TEXT NOT NULL,
    name          TEXT NOT NULL,
    value         TEXT,                     -- NULL permitted for flag-style properties (name-only)
    owner         TEXT,
    visibility    TEXT NOT NULL DEFAULT 'public'
                  CHECK (visibility IN ('public', 'private', 'restricted', 'system', 'admin')),
    flags         JSONB DEFAULT '[]',
    visible_to    JSONB DEFAULT NULL,
    excluded_from JSONB DEFAULT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(parent_type, parent_id, name),
    CONSTRAINT visibility_restricted_requires_lists
        CHECK (visibility != 'restricted'
            OR (visible_to IS NOT NULL AND excluded_from IS NOT NULL)),
    CONSTRAINT visibility_non_restricted_nulls_lists
        CHECK (visibility = 'restricted'
            OR (visible_to IS NULL AND excluded_from IS NULL))
);

CREATE INDEX idx_properties_parent ON entity_properties(parent_type, parent_id);
CREATE INDEX idx_properties_owner ON entity_properties(owner) WHERE owner IS NOT NULL;
```

**Implementation note:** The `updated_at` column has no database trigger. The
Go property store MUST explicitly set `updated_at = now()` in all UPDATE
queries.

**Property lifecycle on parent deletion:** The `entity_properties` table uses
polymorphic references (`parent_type`, `parent_id`) without foreign key
constraints. To prevent orphaned properties, `WorldService` MUST delete child
properties when deleting parent entities:

- `WorldService.DeleteCharacter()` → `PropertyRepository.DeleteByParent("character", charID)`
- `WorldService.DeleteObject()` → `PropertyRepository.DeleteByParent("object", objID)`
- `WorldService.DeleteLocation()` → `PropertyRepository.DeleteByParent("location", locID)`

These deletions MUST occur in the same database transaction as the parent
entity deletion. `PropertyRepository.DeleteByParent(parentType, parentID)`
performs `DELETE FROM entity_properties WHERE parent_type = $1 AND parent_id = $2`.

**Orphan detection and cleanup:** Because `entity_properties` uses
polymorphic parent references without FK constraints, orphaned properties
can accumulate if a deletion path is added without calling
`DeleteByParent()`. Implementation **MUST** include the following
defense-in-depth measures:

1. **Background cleanup goroutine:** A goroutine running on a configurable
   timer (default: daily) **MUST** detect orphaned properties — rows where
   `parent_id` does not match any existing entity of the declared
   `parent_type`. Detected orphans are logged at WARN level on first
   discovery. After a configurable grace period (default: 24 hours, configured
   via server YAML `world.orphan_grace_period` as a duration string like
   `"24h"` or `"48h"`), orphans that persist across two consecutive runs
   **MUST** be actively deleted with a batch `DELETE` and logged at INFO level
   with the count of removed rows. The grace period prevents deleting
   properties whose parent is being recreated (e.g., during a migration or
   restore).

2. **Startup integrity check:** On server startup, the engine **MUST**
   count orphaned properties. If the count exceeds a configurable
   threshold (default: 100), the server **MUST** log at ERROR level but
   continue starting (not fail-fast) to avoid blocking recovery. The
   threshold alerts operators to systematic deletion bugs before
   orphans accumulate to problematic levels.

3. **Integration test coverage:** Each entity deletion path **MUST** have
   a corresponding integration test that verifies child properties are
   deleted in the same transaction.

This is Go-level cascading — no database triggers or FK constraints are
used, consistent with the project's "all logic in Go" constraint.

### Cache Invalidation

The Go policy store sends `pg_notify('policy_changed', policyID)` in the same
transaction as any policy CRUD operation. The engine subscribes to this channel
and reloads its in-memory policy cache on notification. No database triggers are
used — all notification logic lives in Go application code.

The engine uses `pgx.Conn.Listen()` which requires a dedicated persistent
connection outside the connection pool. On connection loss, the engine MUST:

1. Reconnect with exponential backoff (initial: 100ms, multiplier: 2x, max:
   30s, indefinite retries)
2. Re-subscribe to the `policy_changed` channel
3. Perform a full policy reload before serving the next `Evaluate()` call
   (missed notifications cannot be recovered)

During reconnection, `Evaluate()` uses the stale in-memory cache and logs a
warning: `slog.Warn("policy cache may be stale, LISTEN/NOTIFY reconnecting")`.
This is acceptable for MUSH workloads where a brief stale window (<30s) is
tolerable.

**Cache staleness threshold:** To limit the risk of serving stale policy
decisions during prolonged reconnection windows, the engine **MUST** support a
configurable `cache_staleness_threshold` (default: 30s). When the time since
the last successful cache update exceeds this threshold, the engine **MUST**
fail-closed for all subjects by returning `EffectDefaultDeny` without
evaluating policies. The engine **MUST** expose a Prometheus gauge
`policy_cache_last_update` (Unix timestamp) that is updated on every successful
cache reload. Operators **SHOULD** configure alerting when
`time.Now() - policy_cache_last_update > cache_staleness_threshold` to detect
prolonged LISTEN/NOTIFY disconnections before access is denied. Once the
LISTEN/NOTIFY connection is restored and a full reload completes, normal
evaluation resumes automatically. Administrators needing immediate access during
prolonged staleness **MUST** use the `policy reload` command (see Policy
Management Commands) to manually force a cache refresh, or use direct CLI/database
access outside the policy engine.

**Security requirement (S5 - holomush-5k1.346):** An out-of-band cache reload
mechanism MUST exist that bypasses ABAC authorization. Bypass MUST be
restricted to local/system callers only. Tests MUST verify recovery from stale
cache state.

**Rationale for 30s default:** The staleness threshold must exceed the
maximum expected reconnection backoff to avoid triggering fail-closed during
brief network disruptions. The `pgconn` reconnection backoff schedule is:
100ms, 200ms, 400ms, 800ms, 1600ms, 3200ms (cumulative: 6.3s before first
successful reconnect attempt). A 30s threshold provides ~5x headroom for
retries while still limiting the window of stale policy exposure. Operators
experiencing frequent network blips **MAY** increase this threshold further,
accepting a longer stale window in exchange for reduced fail-closed events.

### Audit Log Serialization

The `effect` column in `access_audit_log` maps to the Go `Effect` enum:

| Go Constant          | DB Value          |
| -------------------- | ----------------- |
| `EffectAllow`        | `"allow"`         |
| `EffectDeny`         | `"deny"`          |
| `EffectDefaultDeny`  | `"default_deny"`  |
| `EffectSystemBypass` | `"system_bypass"` |

The `effect` column is the sole decision indicator — there is no separate
`decision` column. `allow` means the request was allowed; `deny` or
`default_deny` means it was denied.

The `Effect` type MUST serialize to the string values in the mapping table
above, not to `iota` integer values. Implementation SHOULD define a
`String() string` method on `Effect` that returns the DB-compatible string.

### Policy Version Records

A version record is created in `access_policy_versions` only when `dsl_text`
changes (via `policy edit`). Toggling `enabled` via `policy enable`/`disable` or
updating `description` modifies the main `access_policies` row directly without
creating a version record. The `version` column on `access_policies` increments
only on DSL changes.

**Policy rollback:** Implementation SHOULD include a `policy rollback <name>
<version>` admin command that restores a previous version's DSL text and
creates a new version record for the rollback. This avoids requiring admins
to manually reconstruct old policy text from the history output.

### Audit Log Configuration

The audit logger supports three modes, configurable via server settings:

```go
type AuditMode string

const (
    AuditOff        AuditMode = "off"          // System bypasses only
    AuditDenialsOnly AuditMode = "denials_only" // Log deny + default_deny only
    AuditAll        AuditMode = "all"           // Log all decisions
)

type AuditConfig struct {
    Mode           AuditMode     // Default: AuditDenialsOnly
    RetainDenials  time.Duration // Default: 90 days
    RetainAllows   time.Duration // Default: 7 days (only relevant when Mode=all)
    PurgeInterval  time.Duration // Default: 24 hours
}
```

| Mode           | What is logged            | Typical use case                      |
| -------------- | ------------------------- | ------------------------------------- |
| `off`          | System bypasses + denials | Development, performance (allows off) |
| `denials_only` | Deny + default_deny       | Production default                    |
| `all`          | All decisions incl. allow | Debugging, compliance audit           |

The default mode is `denials_only` — this balances operational visibility with
storage efficiency.

**Volume estimates:** At 200 concurrent users, typical MUSH activity generates
~0.15 commands/sec/user with ~4 `Evaluate()` calls per command (command
permission, location check, property reads). This produces ~120 checks/sec
total, or ~10M audit records/day in `all` mode. Each record includes a JSONB
`attributes` snapshot (~500 bytes). At 7-day `RetainAllows` retention, `all`
mode accumulates ~70M rows (~35GB). `denials_only` mode produces a small
fraction of this (most checks result in allows). The 7-day allow retention
and 24-hour purge interval are important to enforce in `all` mode to prevent
unbounded growth.

**System bypass auditing:** System subject bypasses **MUST** be logged with
`effect = "system_bypass"` in **all** audit modes (`all`, `denials_only`, and
`off`). This ensures bugs or compromised system contexts are never invisible in
the audit trail, regardless of the configured audit mode.

**Security requirement (S3):** Denial records (`deny` and `default_deny`)
MUST be logged regardless of the configured audit mode. All modes (`off`,
`denials_only`, `all`) log denials. The `off` mode suppresses only `allow`
records. Tests MUST verify that denial logging occurs in all modes.

**Synchronous denial audits:** Denial events (`deny` and `default_deny`) **SHOULD**
be written synchronously to PostgreSQL before `Evaluate()` returns. This prevents
attackers from flooding the system to erase evidence of access violations. Allow
and system bypass events **MAY** be written asynchronously via a buffered channel
with batch writes. Synchronous writes add ~1-2ms latency per denial evaluation.

**Write-Ahead Log fallback:** If the synchronous database write fails (connection
unavailable, timeout, constraint violation), the audit logger **MUST** write
denial and system_bypass audit entries to a local Write-Ahead Log (WAL) file at
`$XDG_STATE_HOME/holomush/audit-wal.jsonl` (or `/var/lib/holomush/audit-wal.jsonl`
in production) for later replay. For allow events, the WAL fallback **SHOULD** be
used but **MAY** be skipped. Each line is a single JSON-encoded audit entry
matching the `access_audit_log` table schema. The WAL file is append-only and
written synchronously (O_APPEND | O_SYNC) to ensure durability.

**Security requirement (S7 - holomush-5k1.353):** WAL fallback for denial and
system_bypass audit events MUST be used (not SHOULD). If primary write fails
and WAL isn't used, denial/bypass events are lost.

The audit logger **MUST** expose a `ReplayWAL()` method that reads entries from
the WAL file, batch-inserts them to PostgreSQL, and truncates the file on success.
The server **SHOULD** call `ReplayWAL()` on startup and **MAY** call it
periodically (e.g., every 5 minutes) to drain the WAL during recovery from
transient database failures.

If both the database write and WAL write fail (catastrophic failure: disk full,
permissions error, filesystem unavailable), the audit logger **SHOULD** log the
failure to stderr at ERROR level, increment the counter metric
`abac_audit_failures_total{reason="wal_failed"}`, and drop the entry. This is
best-effort auditing during catastrophic failure — the `Evaluate()` decision is
returned successfully and audit logging does not block authorization.

If the WAL file grows beyond a configurable threshold (default: 10MB or 10,000
entries), the engine **SHOULD** log a warning. The engine **MAY** expose a
Prometheus gauge `abac_audit_wal_entries` to monitor backlog size. Operators
**SHOULD** monitor WAL file size and alert if it exceeds the threshold,
indicating prolonged database unavailability.

### Audit Metrics Reference

The ABAC audit system exports the following Prometheus metrics:

| Metric Name                 | Type    | Labels   | Description                                                                      |
| --------------------------- | ------- | -------- | -------------------------------------------------------------------------------- |
| `abac_audit_failures_total` | Counter | `reason` | Total audit write failures by reason (channel_full, db_write_failed, wal_failed) |
| `abac_audit_wal_entries`    | Gauge   | -        | Current number of entries in the audit WAL backlog                               |

**Failure reason labels:**

- `channel_full` - Async write channel was full, entry dropped (see ADR 052)
- `db_write_failed` - Database write failed and WAL unavailable (see ADR 053)
- `wal_failed` - Both database and WAL writes failed (catastrophic failure)

**Operational guidance:**

- Alert on `abac_audit_failures_total{reason="wal_failed"}` - indicates disk/filesystem issues
- Alert on `abac_audit_wal_entries > 1000` - indicates prolonged database unavailability
- Monitor `abac_audit_failures_total{reason="channel_full"}` rate - may indicate need for larger async buffer

### Audit Log Retention

Audit records MUST be purged by a periodic Go background job. The purge
interval and retention periods are configurable via `AuditConfig`:

- **Denials:** Retained for 90 days (default)
- **Allows:** Retained for 7 days (default, only relevant in `all` mode)
- **Purge interval:** Every 24 hours (default)

**Partitioning:** In `all` mode, the audit table reaches 10M rows on day one
(~120 checks/sec × 86,400s). Even in `denials_only` mode, at a 5% denial
rate, 10M rows is reached in ~20 days. Phase 7.1 MUST create the
`access_audit_log` table with monthly range partitioning on the `timestamp`
column from the start. Retrofitting partitioning onto a populated table
requires exclusive locks and full table rewrites — at multi-million row
scale, this locks the table for hours. Partition-drop purging is also orders
of magnitude faster than row-by-row `DELETE`.

```sql
CREATE TABLE access_audit_log (
    -- columns as defined above
) PARTITION BY RANGE (timestamp);

-- Create initial partitions (3 months ahead)
CREATE TABLE access_audit_log_2026_02 PARTITION OF access_audit_log
    FOR VALUES FROM ('2026-02-01') TO ('2026-03-01');
CREATE TABLE access_audit_log_2026_03 PARTITION OF access_audit_log
    FOR VALUES FROM ('2026-03-01') TO ('2026-04-01');
CREATE TABLE access_audit_log_2026_04 PARTITION OF access_audit_log
    FOR VALUES FROM ('2026-04-01') TO ('2026-05-01');
```

**Partition management:** A background goroutine **MUST** manage partition
lifecycle on a configurable schedule (default: daily):

1. **Create future partitions:** Ensure at least 2 months of future
   partitions exist. If creation fails (e.g., disk full, permissions),
   log at ERROR level — inserts to a missing partition cause PostgreSQL
   errors that the audit logger handles gracefully (logged, not fatal).
2. **Drop expired partitions:** Use `DETACH PARTITION` followed by `DROP
   TABLE` for partitions older than the retention period. `DETACH` first
   allows backup before permanent deletion if needed.
3. **Health check integration:** The health endpoint **SHOULD** report
   the number of available future partitions. Alert if fewer than 2
   future partitions exist.

The purge job **MUST** create future partitions and detach/drop expired
partitions rather than issuing row-by-row `DELETE` statements.

**Common audit investigation queries:**

```sql
-- Top 10 most denied resources (last 24h)
SELECT resource, COUNT(*) as denials
FROM access_audit_log
WHERE timestamp > NOW() - INTERVAL '24 hours'
  AND effect IN ('deny', 'default_deny')
GROUP BY resource
ORDER BY denials DESC
LIMIT 10;

-- Most frequently matched forbid policies (security hotspots)
SELECT policy_id, policy_name, COUNT(*) as matches
FROM access_audit_log
WHERE timestamp > NOW() - INTERVAL '7 days'
  AND effect = 'deny'
  AND policy_id IS NOT NULL
GROUP BY policy_id, policy_name
ORDER BY matches DESC
LIMIT 10;

-- Evaluation latency outliers (p99 > 10ms = 10000 microseconds)
SELECT subject, resource, action, duration_us
FROM access_audit_log
WHERE timestamp > NOW() - INTERVAL '1 hour'
  AND duration_us > 10000
ORDER BY duration_us DESC
LIMIT 20;

-- Provider timeout rate (attribute resolution failures)
SELECT subject, resource, COUNT(*) as timeout_count
FROM access_audit_log
WHERE timestamp > NOW() - INTERVAL '1 hour'
  AND error_message LIKE '%timeout%'
GROUP BY subject, resource
ORDER BY timeout_count DESC
LIMIT 10;

-- Access pattern by character (actions per character)
SELECT subject, action, COUNT(*) as count
FROM access_audit_log
WHERE timestamp > NOW() - INTERVAL '24 hours'
  AND subject LIKE 'character:%'
GROUP BY subject, action
ORDER BY count DESC
LIMIT 20;
```

### Visibility Defaults

See [Visibility Levels](03-property-model.md#visibility-levels) and [Dependency layering](03-property-model.md#property-model)
for the definitive visibility default rules. The Go property store
(`PropertyRepository`) enforces these defaults — not database triggers.
