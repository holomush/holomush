# Operations Guide

This document covers operational monitoring and maintenance for HoloMUSH.

## PostgreSQL Extensions

HoloMUSH uses several PostgreSQL extensions for enhanced functionality:

| Extension            | Purpose                       | Required |
| -------------------- | ----------------------------- | -------- |
| `pg_trgm`            | Fuzzy text matching for exits | Yes      |
| `pg_stat_statements` | Query performance monitoring  | Optional |

## Query Performance Monitoring

### Prerequisites

`pg_stat_statements` requires configuration at PostgreSQL startup:

```ini
# postgresql.conf
shared_preload_libraries = 'pg_stat_statements'
pg_stat_statements.track = all
```

### Common Monitoring Queries

**Top 10 slowest queries by total time:**

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

**Queries with highest I/O:**

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

**Most frequent queries:**

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

**Reset statistics:**

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

## LISTEN/NOTIFY Monitoring

HoloMUSH uses PostgreSQL LISTEN/NOTIFY for real-time event streaming.

**Active listeners:**

```sql
SELECT pid, datname, usename, application_name, state, query
FROM pg_stat_activity
WHERE query LIKE 'LISTEN%' OR wait_event_type = 'Client';
```

**Recent notifications (requires logging):**

Enable `log_statement = 'all'` to see NOTIFY calls in PostgreSQL logs.

## Connection Pool Monitoring

**Active connections:**

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

**Connection counts by state:**

```sql
SELECT state, count(*)
FROM pg_stat_activity
WHERE datname = 'holomush'
GROUP BY state;
```

## Index Usage

**Unused indexes:**

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

**Index hit ratios:**

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

**Table sizes:**

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

## Vacuum and Maintenance

**Tables needing vacuum:**

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
