---
title: "Operations"
---

This guide covers monitoring and maintaining HoloMUSH in production.

## Health Checks

Both core and gateway expose health endpoints.

### Endpoints

| Endpoint             | Description             |
| -------------------- | ----------------------- |
| `/healthz/liveness`  | Process is alive        |
| `/healthz/readiness` | Ready to accept traffic |

**Core (default port 9100):**

```bash
curl http://localhost:9100/healthz/readiness
```

**Gateway (default port 9101):**

```bash
curl http://localhost:9101/healthz/readiness
```

### Status Command

Check overall system health:

```bash
holomush status
```

This queries both core and gateway health endpoints via gRPC.

## Prometheus Metrics

HoloMUSH exposes Prometheus metrics at `/metrics`.

### Available Metrics

**Connections and requests:**

| Metric                                    | Type      | Labels            | Description                                        |
| ----------------------------------------- | --------- | ----------------- | -------------------------------------------------- |
| `holomush_connections_total`              | Counter   | `type`            | Total connections (telnet, web)                    |
| `holomush_requests_total`                | Counter   | `type`, `status`  | Total requests by type and outcome                 |

**Commands:**

| Metric                                    | Type      | Labels                | Description                                        |
| ----------------------------------------- | --------- | --------------------- | -------------------------------------------------- |
| `holomush_command_executions_total`       | Counter   | `command`, `source`, `status` | Command executions by name, source, and status (success, error, not_found, permission_denied, rate_limited) |
| `holomush_command_duration_seconds`       | Histogram | `command`, `source`   | Command execution latency                          |
| `holomush_command_output_failures_total`  | Counter   | `command`             | Failed to deliver command output to session        |
| `holomush_command_rate_limited_total`     | Counter   | `command`             | Commands rejected by rate limiter                  |
| `holomush_alias_expansions_total`        | Counter   | `alias`               | Alias expansion count by alias name                |
| `holomush_alias_rollback_failures_total` | Counter   |                       | Alias rollback failures (requires manual fix)      |

**Engine and resilience:**

| Metric                                    | Type      | Labels            | Description                                        |
| ----------------------------------------- | --------- | ----------------- | -------------------------------------------------- |
| `holomush_engine_failures_total`         | Counter   | `operation`       | Engine operation failures (access checks, capabilities) |
| `holomush_circuit_breaker_trips_total`   | Counter   | `handler`         | Circuit breaker activations per command handler     |
| `holomush_circuit_breaker_skipped_total` | Counter   | `handler`         | Sessions skipped due to open circuit breaker        |
| `holomush_ratelimiter_sessions`          | Gauge     |                   | Current number of tracked rate-limit sessions       |

Go runtime and process metrics (`go_*`, `process_*`) are also exported automatically.

### Scrape Configuration

```yaml
scrape_configs:
  - job_name: holomush-core
    static_configs:
      - targets: ["localhost:9100"]

  - job_name: holomush-gateway
    static_configs:
      - targets: ["localhost:9101"]
```

## PostgreSQL Extensions

HoloMUSH requires and optionally uses these PostgreSQL extensions:

| Extension            | Purpose                       | Required |
| -------------------- | ----------------------------- | -------- |
| `pg_trgm`            | Fuzzy text matching for exits | Yes      |
| `pg_stat_statements` | Query performance monitoring  | Optional |

### Enable pg_stat_statements

For query performance monitoring, enable `pg_stat_statements`:

```ini
# postgresql.conf
shared_preload_libraries = 'pg_stat_statements'
pg_stat_statements.track = all
```

Restart PostgreSQL after configuration changes.

## Query Performance Monitoring

### Slowest Queries

Find queries consuming the most time:

```sql
SELECT
    calls,
    round(total_exec_time::numeric, 2) as total_ms,
    round(mean_exec_time::numeric, 2) as mean_ms,
    round(stddev_exec_time::numeric, 2) as stddev_ms,
    rows,
    query
FROM pg_stat_statements
ORDER BY total_exec_time DESC
LIMIT 10;
```

### High I/O Queries

Identify queries causing disk reads:

```sql
SELECT
    calls,
    shared_blks_hit + shared_blks_read as total_blocks,
    round(100.0 * shared_blks_hit / nullif(shared_blks_hit + shared_blks_read, 0), 2) as cache_hit_pct,
    query
FROM pg_stat_statements
WHERE shared_blks_hit + shared_blks_read > 0
ORDER BY shared_blks_read DESC
LIMIT 10;
```

### Most Frequent Queries

Find queries called most often:

```sql
SELECT
    calls,
    round(total_exec_time::numeric, 2) as total_ms,
    round(mean_exec_time::numeric, 2) as mean_ms,
    query
FROM pg_stat_statements
ORDER BY calls DESC
LIMIT 10;
```

### Reset Statistics

Clear accumulated statistics:

```sql
SELECT pg_stat_statements_reset();
```

### Key Metrics

| Metric             | Description                  | Alert Threshold  |
| ------------------ | ---------------------------- | ---------------- |
| `mean_exec_time`   | Average query execution time | > 100ms          |
| `stddev_exec_time` | Variance in execution time   | High variance    |
| `rows`             | Total rows returned          | Depends on query |
| `shared_blks_read` | Blocks read from disk        | High = slow      |

## Connection Monitoring

### Active Connections

View current database connections:

```sql
SELECT
    datname,
    usename,
    application_name,
    client_addr,
    state,
    query_start,
    now() - query_start as query_duration
FROM pg_stat_activity
WHERE datname = 'holomush'
ORDER BY query_start;
```

### Connection Counts

Connections grouped by state:

```sql
SELECT state, count(*)
FROM pg_stat_activity
WHERE datname = 'holomush'
GROUP BY state;
```

### JetStream Consumer Health

HoloMUSH uses an embedded NATS JetStream server for real-time event delivery.
Check consumer state via the NATS monitoring port (default: disabled; set
`event_bus.monitor_port` in config to enable):

```bash
# List stream info (requires nats CLI and monitoring port enabled)
nats stream info EVENTS --server nats://localhost:<monitor_port>

# Check audit projection consumer lag
nats consumer info EVENTS host_audit_projection
```

Prometheus metric `audit_projection_lag_seconds` alerts at > 5s lag. The
embedded server does not open a network port by default (`DontListen: true`).

## Index Health

### Unused Indexes

Find indexes that may be candidates for removal:

```sql
SELECT
    schemaname,
    relname as table_name,
    indexrelname as index_name,
    idx_scan,
    pg_size_pretty(pg_relation_size(indexrelid)) as index_size
FROM pg_stat_user_indexes
WHERE idx_scan = 0
ORDER BY pg_relation_size(indexrelid) DESC;
```

### Index Usage

Verify indexes are being used:

```sql
SELECT
    relname as table_name,
    round(100.0 * idx_scan / nullif(seq_scan + idx_scan, 0), 2) as index_usage_pct,
    seq_scan,
    idx_scan
FROM pg_stat_user_tables
WHERE seq_scan + idx_scan > 0
ORDER BY seq_scan DESC;
```

## Table Statistics

### Table Sizes

Monitor table and index sizes:

```sql
SELECT
    relname as table_name,
    pg_size_pretty(pg_total_relation_size(relid)) as total_size,
    pg_size_pretty(pg_relation_size(relid)) as table_size,
    pg_size_pretty(pg_indexes_size(relid)) as index_size,
    n_live_tup as live_rows,
    n_dead_tup as dead_rows
FROM pg_stat_user_tables
ORDER BY pg_total_relation_size(relid) DESC;
```

### Tables Needing Vacuum

Identify tables with dead tuples:

```sql
SELECT
    relname,
    n_dead_tup,
    last_vacuum,
    last_autovacuum
FROM pg_stat_user_tables
WHERE n_dead_tup > 1000
ORDER BY n_dead_tup DESC;
```

## Backup and Recovery

### PostgreSQL Backups

Use `pg_dump` for logical backups:

```bash
pg_dump -U holomush -h localhost holomush > backup.sql
```

For production, consider:

- Continuous archiving with WAL shipping
- Point-in-time recovery (PITR)
- Streaming replication for high availability

### Restore

Restore from a logical backup:

```bash
psql -U holomush -h localhost holomush < backup.sql
```

## Log Management

### Log Locations

| Component    | Location                        |
| ------------ | ------------------------------- |
| Core logs    | stdout (use log aggregation)    |
| Gateway logs | stdout (use log aggregation)    |
| PostgreSQL   | `/var/log/postgresql/` (varies) |

### Log Aggregation

For production, aggregate logs with:

- **Loki** - Grafana's log aggregation
- **Elasticsearch** - Full-text search
- **CloudWatch** - AWS managed logging

### Docker Logging

View container logs:

```bash
docker compose logs -f core
docker compose logs -f gateway
```

## Troubleshooting

### Connection Issues

**Symptom:** Gateway cannot connect to core.

**Check:**

1. Core is running: `holomush status`
2. Network connectivity: `nc -zv localhost 9000`
3. Firewall rules allow traffic
4. TLS certificates are valid

### Database Connection Failed

**Symptom:** Core fails with database errors.

**Check:**

1. PostgreSQL is running: `pg_isready -h localhost`
2. `DATABASE_URL` is correct
3. User has permissions: `\du` in psql
4. Database exists: `\l` in psql

### High Memory Usage

**Possible causes:**

- Too many active sessions
- Large query result sets
- Plugin memory leaks

**Investigate:**

```bash
# Check active sessions
curl http://localhost:9100/metrics | grep sessions_active

# Check PostgreSQL connections
psql -c "SELECT count(*) FROM pg_stat_activity WHERE datname='holomush';"
```

### Slow Queries

**Investigate:**

1. Enable `pg_stat_statements` (see above)
2. Check for missing indexes
3. Analyze query plans with `EXPLAIN ANALYZE`

```sql
-- Replace <ulid> with an actual location ULID from your database
EXPLAIN ANALYZE SELECT * FROM events WHERE stream = 'location:<ulid>' ORDER BY id DESC LIMIT 100;
```

### Alias Database-Cache Inconsistency

**Symptom:** Log message with `severity=critical` containing
"alias rollback failed - database-cache inconsistency".

**Monitoring:** Alert on the `holomush_alias_rollback_failures_total` metric.

```yaml
# Prometheus alert rule
- alert: AliasRollbackFailure
  expr: increase(holomush_alias_rollback_failures_total[5m]) > 0
  for: 0m
  labels:
    severity: critical
  annotations:
    summary: "Alias database-cache inconsistency detected"
    description: "A rollback failure has left aliases in an inconsistent state."
```

**Cause:** During alias creation, the database write succeeded but the cache
update failed (e.g., circular reference detected). The subsequent rollback
(database delete) also failed, leaving the alias in the database but not in
the cache.

**Impact:**

- The alias exists in the database but is not loaded into cache
- On server restart, the alias will be loaded and may cause issues
- The alias may conflict with other aliases or commands

**Recovery:**

1. **Identify the problematic alias** from the log message (check `alias` and
   `player_id` fields)

2. **For player aliases**, delete from database:

    ```sql
   DELETE FROM player_aliases
   WHERE player_id = '<player_id>' AND alias = '<alias_name>';
    ```

3. **For system aliases**, delete from database:

    ```sql
   DELETE FROM system_aliases WHERE alias = '<alias_name>';
    ```

4. **Verify cleanup:**

    ```sql
   -- Check player aliases
   SELECT * FROM player_aliases WHERE alias = '<alias_name>';

   -- Check system aliases
   SELECT * FROM system_aliases WHERE alias = '<alias_name>';
    ```

5. **No restart required** - the cache does not contain the alias, so removing
   it from the database prevents it from being loaded on the next restart.

**Prevention:** This failure typically occurs when database connectivity is
degraded. Monitor database connection health and ensure proper connection
pooling configuration.

## Next Steps

- [Configuration](/operating/configuration/) - Adjust server settings
- [Installation](/operating/installation/) - Deployment options
