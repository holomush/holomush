<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 105. LISTEN/NOTIFY Disconnection Recovery Strategy

> [Back to Decision Index](../README.md)

**Review finding (PR #69, Suggestion #4):** The LISTEN/NOTIFY disconnection
recovery behavior is specified in `05-storage-audit.md` but lacks a formal
design decision documenting the alternatives considered and rationale.

**Context:** The ABAC engine uses PostgreSQL LISTEN/NOTIFY to receive real-time
cache invalidation signals when policies change. If the LISTEN connection
drops (network partition, PostgreSQL restart, connection pool recycling), the
engine must handle the gap between disconnection and reconnection.

**Decision:** Use a stale-cache-with-threshold strategy:

1. **During disconnection:** Continue serving `Evaluate()` calls using the stale
   in-memory policy cache. Log a warning:
   `slog.Warn("policy cache may be stale, LISTEN/NOTIFY reconnecting")`.

2. **Staleness threshold (default: 30s):** When `time.Now() - last_cache_update`
   exceeds the configurable `cache_staleness_threshold`, fail-closed for all
   subjects by returning `EffectDefaultDeny`.

3. **On reconnection:** Perform a full policy reload (not incremental) before
   serving the next `Evaluate()` call. Missed notifications cannot be recovered
   individually.

4. **Manual override:** Administrators can force an immediate cache refresh via
   `policy reload` command during prolonged staleness windows.

**Alternatives considered:**

| Alternative                                  | Why rejected                                                                           |
| -------------------------------------------- | -------------------------------------------------------------------------------------- |
| Immediate fail-closed on disconnect          | Too aggressive for MUSH workloads; brief network blips would deny all access           |
| Polling fallback (periodic full reload)      | Adds DB load and complexity; 30s stale window is acceptable                            |
| Incremental recovery (replay missed NOTIFYs) | PostgreSQL does not buffer missed LISTEN notifications; impossible without WAL tailing |
| No recovery (require restart)                | Unacceptable for operational availability                                              |

**Rationale:**

1. **MUSH workload tolerance:** Brief policy staleness (<30s) is acceptable
   because MUSH policy changes are infrequent (admin-initiated, not
   per-request).

2. **Fail-safe after threshold:** The 30s threshold ensures that prolonged
   disconnections trigger fail-closed behavior, maintaining security at the
   cost of availability.

3. **Full reload simplicity:** A full reload on reconnection is simpler and
   more reliable than attempting to determine which notifications were missed.

4. **Observability:** The `abac_policy_cache_last_update` metric (Unix
   timestamp) enables operator alerting before the threshold is reached.

**Impact:**

- Spec: `05-storage-audit.md` lines 160-187 document the implementation
- Metric: `abac_policy_cache_last_update` gauge tracks cache freshness
- Config: `cache_staleness_threshold` (default 30s) is operator-configurable

**Related:** Decision #11 (Cache Invalidation), Decision #14 (No Database
Triggers)
