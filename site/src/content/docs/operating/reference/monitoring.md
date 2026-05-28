---
title: "Monitoring reference"
---

Lookup catalogs for monitoring HoloMUSH: health-check endpoints, the Prometheus
metric set, required PostgreSQL extensions, query-performance metrics, and log
locations. For the procedures that use these, see
[Operations](/operating/how-to/operations/).

## Health-check endpoints

Both core (default port 9100) and gateway (default port 9101) expose:

| Endpoint             | Description             |
| -------------------- | ----------------------- |
| `/healthz/liveness`  | Process is alive        |
| `/healthz/readiness` | Ready to accept traffic |

## Prometheus metrics

HoloMUSH exposes Prometheus metrics at `/metrics`.

**Connections and requests:**

| Metric                       | Type    | Labels           | Description                        |
| ---------------------------- | ------- | ---------------- | ---------------------------------- |
| `holomush_connections_total` | Counter | `type`           | Total connections (telnet, web)    |
| `holomush_requests_total`    | Counter | `type`, `status` | Total requests by type and outcome |

**Commands:**

| Metric                                   | Type      | Labels                        | Description                                                                                                  |
| ---------------------------------------- | --------- | ----------------------------- | ------------------------------------------------------------------------------------------------------------ |
| `holomush_command_executions_total`      | Counter   | `command`, `source`, `status` | Command executions by name, source, and status (success, error, not_found, permission_denied, rate_limited) |
| `holomush_command_duration_seconds`      | Histogram | `command`, `source`           | Command execution latency                                                                                    |
| `holomush_command_output_failures_total` | Counter   | `command`                     | Failed to deliver command output to session                                                                  |
| `holomush_command_rate_limited_total`    | Counter   | `command`                     | Commands rejected by rate limiter                                                                            |
| `holomush_alias_expansions_total`        | Counter   | `alias`                       | Alias expansion count by alias name                                                                          |
| `holomush_alias_rollback_failures_total` | Counter   |                               | Alias rollback failures (requires manual fix)                                                                |

**Engine and resilience:**

| Metric                                   | Type    | Labels      | Description                                             |
| ---------------------------------------- | ------- | ----------- | ------------------------------------------------------- |
| `holomush_engine_failures_total`         | Counter | `operation` | Engine operation failures (access checks, capabilities) |
| `holomush_circuit_breaker_trips_total`   | Counter | `handler`   | Circuit breaker activations per command handler         |
| `holomush_circuit_breaker_skipped_total` | Counter | `handler`   | Sessions skipped due to open circuit breaker            |
| `holomush_ratelimiter_sessions`          | Gauge   |             | Current number of tracked rate-limit sessions           |

Go runtime and process metrics (`go_*`, `process_*`) are also exported automatically.
The `audit_projection_lag_seconds` metric alerts at > 5s JetStream audit lag.

## PostgreSQL extensions

| Extension            | Purpose                       | Required |
| -------------------- | ----------------------------- | -------- |
| `pg_trgm`            | Fuzzy text matching for exits | Yes      |
| `pg_stat_statements` | Query performance monitoring  | Optional |

## Query-performance metrics

These `pg_stat_statements` columns drive the query-performance procedures:

| Metric             | Description                  | Alert Threshold  |
| ------------------ | ---------------------------- | ---------------- |
| `mean_exec_time`   | Average query execution time | > 100ms          |
| `stddev_exec_time` | Variance in execution time   | High variance    |
| `rows`             | Total rows returned          | Depends on query |
| `shared_blks_read` | Blocks read from disk        | High = slow      |

## Log locations

| Component    | Location                        |
| ------------ | ------------------------------- |
| Core logs    | stdout (use log aggregation)    |
| Gateway logs | stdout (use log aggregation)    |
| PostgreSQL   | `/var/log/postgresql/` (varies) |
