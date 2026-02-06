# ADR 0016: PostgreSQL LISTEN/NOTIFY for Policy Cache Invalidation

**Date:** 2026-02-05
**Status:** Accepted
**Deciders:** HoloMUSH Contributors

## Context

The ABAC policy engine maintains an in-memory cache of compiled policies for fast
evaluation (<5ms p99 target). When an administrator creates, edits, enables, disables,
or deletes a policy, the in-memory cache must be updated to reflect the change. The
question is how the engine learns that a policy has changed.

HoloMUSH has a hard constraint: no database triggers or stored procedures. All logic
must live in Go application code. PostgreSQL is storage only.

**Critical constraint:** PostgreSQL LISTEN/NOTIFY is fire-and-forget with zero
buffering. Notifications sent while a listener is disconnected are permanently lost.
There is no replay mechanism, no delivery guarantee, and no way to recover missed
notifications. Any cache invalidation strategy using LISTEN/NOTIFY MUST handle
reconnection by performing a full reload of cached state.

### Options Considered

**Option A: Polling**

The engine periodically queries the policy table for changes (e.g., checking `updated_at`
timestamps every N seconds).

| Aspect     | Assessment                                                                                                                                                    |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Strengths  | Simple to implement; works with any PostgreSQL connection pool                                                                                                |
| Weaknesses | Latency proportional to poll interval; wasted queries when no changes occur; at 1s interval, up to 1s stale window; at 100ms interval, significant query load |

**Option B: Application-level pub/sub (Redis, NATS)**

Use an external message broker to notify the engine of policy changes.

| Aspect     | Assessment                                                                                               |
| ---------- | -------------------------------------------------------------------------------------------------------- |
| Strengths  | Low latency; scalable to multiple server instances                                                       |
| Weaknesses | Additional infrastructure dependency; operational complexity; overkill for single-server MUSH deployment |

**Option C: PostgreSQL LISTEN/NOTIFY (in Go application code)**

The Go policy store calls `pg_notify('policy_changed', policyID)` in the same transaction
as any policy CRUD operation. The engine subscribes to the `policy_changed` channel using
a dedicated PostgreSQL connection and reloads its cache on notification.

| Aspect     | Assessment                                                                                                                                                   |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Strengths  | Push-based (no polling overhead); built into PostgreSQL; no external dependencies; notification is transactional (sent only on commit); low latency (<100ms) |
| Weaknesses | Requires a dedicated persistent connection outside the pool; notification is fire-and-forget (no delivery guarantee on disconnect); single-server only       |

## Decision

**Option C: PostgreSQL LISTEN/NOTIFY in Go application code.**

As noted in Context, LISTEN/NOTIFY provides no delivery guarantees — notifications
are fire-and-forget with zero buffering, permanently lost during disconnection. The
reconnection protocol below handles this by performing a full reload on reconnect.

The Go policy store sends `pg_notify('policy_changed', policyID)` within the same
transaction as any Create, Update, or Delete operation on the `access_policies` table.
The engine subscribes to the `policy_changed` channel and reloads all enabled policies
into its in-memory cache on notification.

### Notification Flow

```text
Admin runs: policy edit faction-hq-access
    │
    ▼
PolicyStore.Update() — within transaction:
    1. UPDATE access_policies SET dsl_text=..., version=version+1
    2. INSERT INTO access_policy_versions (...)
    3. SELECT pg_notify('policy_changed', 'policy-ulid-here')
    4. COMMIT
    │
    ▼
Engine's LISTEN goroutine receives notification
    │
    ▼
Engine reloads all enabled policies from DB
    │
    ▼
Next Evaluate() uses updated policy cache
```

### Dedicated Connection

The engine uses `pgx.Conn.Listen()` which requires a dedicated persistent connection
**outside the connection pool**. This connection is used only for LISTEN — it cannot be
shared with query traffic. This is a PostgreSQL requirement: LISTEN subscriptions are
per-connection state that is lost if the connection is returned to a pool.

### Reconnection Protocol

On connection loss, the engine MUST:

1. Log a warning: `slog.Warn("policy cache may be stale, LISTEN/NOTIFY reconnecting")`
2. Reconnect with exponential backoff:
   - Initial delay: 100ms
   - Multiplier: 2x
   - Maximum delay: 30s
   - Retries: indefinite (the engine cannot function without cache updates)
3. Re-subscribe to the `policy_changed` channel
4. Perform a **full policy reload** before serving the next `Evaluate()` call

The full reload on reconnect is essential: notifications received during the disconnect
are lost (LISTEN/NOTIFY is fire-and-forget). The engine cannot know which policies changed
during the outage.

### Staleness During Reconnection

During reconnection, `Evaluate()` continues using the stale in-memory cache. The engine
does NOT block authorization on reconnection — a stale allow or deny for <30 seconds is
acceptable for MUSH workloads where the primary risk is a player seeing a slightly outdated
policy, not a financial transaction.

## Rationale

**No external dependencies:** PostgreSQL LISTEN/NOTIFY is built into the database
HoloMUSH already depends on. Adding Redis or NATS for a single-server deployment with
~200 users would be operational overhead without proportional benefit.

**Transactional consistency:** Because `pg_notify()` is called within the same transaction
as the policy CRUD operation, the notification is sent if and only if the change commits.
If the transaction rolls back, no notification is sent. This eliminates a class of
inconsistency where the cache is invalidated but the change didn't persist.

**Push-based efficiency:** At steady state, the LISTEN connection is idle — no queries,
no CPU, no network traffic. Notifications arrive only when policies change, which is
infrequent in a running MUSH (minutes to hours between policy edits). Polling at any
interval would waste resources for the vast majority of the time.

**No database triggers:** The `pg_notify()` call is explicit Go code in the policy store,
not a database trigger. This satisfies the project constraint that all logic lives in Go.

### Why Full Reload (Not Incremental)

The notification payload includes the policy ID, which could support incremental cache
updates (reload only the changed policy). However, full reload is simpler and sufficient:

- At <500 active policies and <50ms reload time, full reload is fast enough
- Incremental updates require handling create/delete/enable/disable state transitions
- Full reload is idempotent and self-healing — any cache corruption is fixed on next
  notification
- The complexity of incremental updates is not justified until policy counts grow
  significantly

### Future: Multi-Server

If HoloMUSH scales to multiple server instances, LISTEN/NOTIFY works across connections
to the same PostgreSQL instance. Each server's engine subscribes independently. No
changes to the notification mechanism are needed. Cross-instance PostgreSQL (read replicas)
would require a different approach (application-level pub/sub), but this is not in scope
for the current single-server architecture.

## Consequences

**Positive:**

- Push-based invalidation with <100ms latency (no polling delay)
- Transactional consistency — notification sent only on successful commit
- No external infrastructure dependencies beyond PostgreSQL
- All notification logic in Go application code (no database triggers)
- Works across multiple connections to the same PostgreSQL instance

**Negative:**

- Requires a dedicated persistent PostgreSQL connection outside the pool
- Notifications are fire-and-forget — lost during connection outages
- Full policy reload required on reconnection (cannot recover missed notifications)
- Stale cache during reconnection (<30s) — acceptable for MUSH, not for financial systems

**Neutral:**

- The dedicated LISTEN connection adds one persistent connection to the PostgreSQL
  connection count
- Exponential backoff on reconnection prevents thundering herd on database recovery
- The `policy_changed` channel name is a convention, not a PostgreSQL object — no schema
  migration needed to set it up

## References

- [Full ABAC Architecture Design — Cache Invalidation](../specs/2026-02-05-full-abac-design.md)
- [Design Decision #11: Cache Invalidation](../specs/2026-02-05-full-abac-design-decisions.md#11-cache-invalidation)
- [PostgreSQL LISTEN/NOTIFY Documentation](https://www.postgresql.org/docs/current/sql-notify.html)
