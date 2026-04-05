# Core Lifecycle & Policy Cache Redesign

**Date:** 2026-04-04
**Status:** Draft
**Authors:** Sean, Claude Opus 4.6

## Problem Statement

The core server has two related deficiencies:

1. **The policy cache invalidation model is fundamentally flawed.** The cache uses a
   30-second staleness timer that conflates "time since last reload" with "notification
   channel health." In steady state, no policies change after seed — so no LISTEN/NOTIFY
   events fire, the timer expires, and all requests are denied with "policy cache stale."
   This makes the game unplayable within 30 seconds of startup.

2. **The core server lacks a startup state machine.** The gRPC listener binds and accepts
   connections before all subsystems are initialized. The readiness probe is hardcoded to
   `true`. There is no formal lifecycle management — subsystems are started via ad-hoc
   goroutines and defers with no dependency ordering or health aggregation.

## Goals

- Replace the broken staleness-based cache model with a correct health tier system
- Introduce a formal subsystem lifecycle with dependency-ordered startup and shutdown
- Gate traffic acceptance on aggregate subsystem readiness
- Make the readiness probe reflect actual system health (startup and runtime)
- Remove the LISTEN/NOTIFY mechanism in favor of polling + direct invalidation
- Provide a reusable health tracking pattern for future subsystems

## Non-Goals

- Gateway startup coordination (gateway retries gRPC connection naturally)
- Dynamic plugin loading/hot-reload (future work; interfaces MUST NOT preclude it)
- Full ABAC engine redesign (only the cache health integration changes)
- Multi-instance cache coordination (future; polling handles it naturally)

## RFC2119 Keywords

| Keyword          | Meaning                                    |
| ---------------- | ------------------------------------------ |
| **MUST**         | Absolute requirement                       |
| **MUST NOT**     | Absolute prohibition                       |
| **SHOULD**       | Recommended, may ignore with justification |
| **SHOULD NOT**   | Not recommended                            |
| **MAY**          | Optional                                   |

---

## Architecture Overview

The design has four components with separated concerns:

| Component           | Responsibility                                         | Package                  |
| ------------------- | ------------------------------------------------------ | ------------------------ |
| **PolicyCache**     | Store and serve compiled policy snapshots               | `internal/access/policy` |
| **PolicyPoller**    | Detect external policy changes via periodic DB query    | `internal/access/policy` |
| **HealthTracker**   | Track operational health tiers for any subsystem        | `internal/lifecycle`     |
| **ReadinessRegistry** | Aggregate subsystem health for startup gating         | `internal/lifecycle`     |

### Component Relationships

```text
┌─────────────────────────────────────────────────────┐
│                    Core Server                       │
│                                                      │
│  ┌──────────────┐    reload()    ┌──────────────┐   │
│  │ PolicyPoller  │──────────────▶│ PolicyCache   │   │
│  │ (periodic DB  │               │ (snapshot +   │   │
│  │  check)       │               │  compile)     │   │
│  └──────┬───────┘               └──────┬────────┘   │
│         │                               │            │
│         │ success/failure               │ success/   │
│         │                               │ failure    │
│         ▼                               ▼            │
│  ┌─────────────────────────────────────────────┐    │
│  │           HealthTracker (ABAC)               │    │
│  │  Warm ──▶ Degraded ──▶ Stale ──▶ Dead       │    │
│  └──────────────────┬──────────────────────────┘    │
│                     │ HealthReporter                 │
│                     ▼                                │
│  ┌─────────────────────────────────────────────┐    │
│  │          ReadinessRegistry                   │    │
│  │  ☑ ABAC PolicyCache    ── warm               │    │
│  │  ☑ Database             ── connected          │    │
│  │  ☑ Plugin Stack         ── loaded             │    │
│  └──────────────────┬──────────────────────────┘    │
│                     │ AllReady()                      │
│                     ▼                                │
│  ┌─────────────────────────────────────────────┐    │
│  │  gRPC Listener (binds only when ready)       │    │
│  │  /healthz/readiness (reflects AllReady)       │    │
│  └─────────────────────────────────────────────┘    │
│                                                      │
│  In-process policy mutations (store layer)           │
│  ──▶ cache.Invalidate() (direct push, fast)          │
└─────────────────────────────────────────────────────┘
```

### Invalidation Model

Two independent paths, both application-layer (no database triggers per ADR #14):

1. **Active push (fast path):** When code mutates policies through the store layer,
   it calls `cache.Invalidate()` directly. This triggers an immediate `Reload()`.
   No pg_notify needed for same-process changes.

2. **Periodic poll (safety net):** A background `PolicyPoller` queries
   `SELECT MAX(updated_at) FROM access_policies` every 10 seconds. If the
   timestamp has changed since the last poll, it calls `cache.Reload()`. This
   catches manual DB edits, multi-instance deployments, and missed in-process
   invalidations.

The LISTEN/NOTIFY mechanism (`PgListener`, `Cache.StartWithListener()`,
`Cache.listenLoop()`, and the `Listener` interface) is removed entirely.

---

## Subsystem Lifecycle

### SubsystemID

All subsystem identifiers MUST be typed constants, not strings:

```go
type SubsystemID int

const (
    SubsystemDatabase   SubsystemID = iota
    SubsystemTLS
    SubsystemABAC
    SubsystemAuth
    SubsystemWorld
    SubsystemPlugins
    SubsystemSessions
    SubsystemBootstrap
    SubsystemGRPC
)
```

New subsystems MUST add a constant to this enum. The `String()` method SHOULD be
generated via `go generate` + stringer for human-readable logging.

### Subsystem Interface

All top-level server components MUST implement `Subsystem`:

```go
type Subsystem interface {
    ID()        SubsystemID
    DependsOn() []SubsystemID
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
}
```

- `Start()` MUST be idempotent — calling it twice is a no-op, not an error.
- `Stop()` MUST be idempotent and MUST NOT block indefinitely.
- `DependsOn()` declares dependencies by `SubsystemID`. The server topologically
  sorts subsystems and starts them in dependency order.
- Subsystems with no dependency relationship MAY be started concurrently.

### Dependency Resolution

At startup, the server MUST:

1. Collect all registered `Subsystem` instances.
2. Perform topological sort on the dependency graph (Kahn's algorithm).
3. Detect cycles — a cycle is a fatal startup error.
4. Start subsystems in topological order. Subsystems at the same depth in the graph
   MAY be started concurrently.
5. On shutdown, stop subsystems in reverse topological order.

### What Is (and Is Not) a Subsystem

| Component          | Subsystem? | HealthReporter? | Rationale                                      |
| ------------------ | ---------- | --------------- | ---------------------------------------------- |
| Database pool      | Yes        | Yes             | Ongoing connection, health matters              |
| ABAC stack         | Yes        | Yes             | Cache, poller, ongoing state                    |
| Auth services      | Yes        | No              | Stateless after init (hasher, repos)            |
| WorldService       | Yes        | No              | Stateless coordinator over repos                |
| PluginStack        | Yes        | Yes             | Loaded plugins, optional per-plugin health      |
| SessionStore       | Yes        | Yes             | DB-backed, reaper goroutine                     |
| Bootstrap runner   | Yes        | No              | Runs once at startup, done                      |
| TLS manager        | Yes        | No              | Generates certs once, stateless after           |
| Attribute resolver | No         | No              | Internal to ABAC stack                          |
| Auth hasher        | No         | No              | Pure function, internal dependency              |
| gRPC server        | Yes        | No              | Terminal subsystem, started last                |

Internal components of a subsystem MUST report health through their parent's
`HealthReporter`, not independently. The `ReadinessRegistry` tracks top-level
subsystems only.

### Plugin Health Reporting

The `PluginStack` subsystem reports aggregate health. Individual plugins MAY
implement `HealthReporter`:

```go
if reporter, ok := plugin.(HealthReporter); ok {
    pluginStack.trackHealth(plugin.Name(), reporter)
}
```

Plugins that do not implement `HealthReporter` are assumed healthy after
successful `Load()`. This keeps simple Lua plugins zero-overhead while allowing
complex plugins (future: Discord, external API connectors) to participate in
health tracking.

When dynamic plugin loading is implemented (future), the plugin `Manager` MUST
register/deregister plugin health reporters during `Load()`/`Unload()`.

---

## Health Tier System

### Tier Definitions

| Tier         | Serves Requests? | Readiness Probe | Description                                       |
| ------------ | ---------------- | --------------- | ------------------------------------------------- |
| **Warm**     | Yes, normally    | Ready (200)     | Normal operation. Cache valid, poller healthy.     |
| **Degraded** | Yes, last-known-good | Ready (200) | Poll failure or compilation error. Serving from last-known-good snapshot. Metrics bumped. |
| **Stale**    | No, fail-closed  | Not ready (503) | Grace period expired while still failing. Snapshot may be outdated. |
| **Dead**     | No, fail-closed  | Not ready (503) | Unrecoverable. Max retries exhausted or corruption detected. Server initiates graceful shutdown. |

### State Transitions

```text
                    ┌─────────────────────┐
                    │      STARTUP        │
                    │  (no snapshot yet)   │
                    └─────────┬───────────┘
                              │
                        initial Reload()
                          succeeds
                              │
                              ▼
         ┌───────────────────────────────────────┐
   ┌────▶│            🟢 WARM                     │◀────┐
   │     │  Serving normally, poller healthy       │     │
   │     └─────────────┬─────────────────────────┘     │
   │                   │                                 │
   │             poll failure /                    successful
   │             compilation error                 reload
   │                   │                                 │
   │                   ▼                                 │
   │     ┌───────────────────────────────────────┐     │
   │     │          🟡 DEGRADED                   │─────┘
   │     │  Serving last-known-good snapshot      │
   │     └─────────────┬─────────────────────────┘
   │                   │
   │             grace period expires
   │             (still failing)
   │                   │
   │                   ▼
   │     ┌───────────────────────────────────────┐
   └─────│          🟠 STALE                      │──────┐
         │  Fail-closed, deny non-system          │      │
         │  Still attempting recovery              │      │
         └─────────────┬─────────────────────────┘      │
                       │                                  │
                 max retries exhausted /             recovery
                 corruption detected                 succeeds
                       │                                  │
                       ▼                             (Stale→Warm
         ┌───────────────────────────────────────┐   only)
         │          🔴 DEAD                       │
         │  Fail-closed, initiating shutdown      │
         │  NO recovery from this state           │
         └───────────────────────────────────────┘
```

### Transition Thresholds

| Transition        | Trigger                                 | Default              |
| ----------------- | --------------------------------------- | -------------------- |
| Warm → Degraded   | First poll failure or compilation error  | 1 failure            |
| Degraded → Stale  | Continuous failures exceeding grace      | 60 seconds           |
| Stale → Dead      | Max consecutive failures exhausted       | 30 failures (~5 min) |
| Any → Warm        | Successful reload (except from Dead)     | 1 success            |

All thresholds MUST be configurable. Dead is terminal — no recovery without
operator restart.

### Degradation Behavior

- **Degraded:** Serve from last-known-good snapshot. Bump
  `abac_health_tier{tier="degraded"}` metric. Log at WARN.
- **Stale:** Deny all non-system requests via `engine.EnterDegradedMode()`.
  Log at ERROR. Readiness probe returns 503.
- **Dead:** Same as Stale, plus initiate graceful server shutdown. An operator
  MUST investigate and restart.

### Database Unavailability

Database unavailability is fatal beyond the grace period. The rationale:

- Events cannot persist (event sourcing is broken)
- Sessions cannot resolve (authentication is broken)
- World state queries fail (game is non-functional)
- Policy changes cannot be detected (security posture unknown)

The `PolicyPoller` failing is the canary — it detects DB unavailability before
game operations fail. The health tier escalation (Degraded → Stale → Dead)
provides a grace period for transient blips, but sustained DB loss MUST result
in shutdown.

---

## Policy Cache Changes

### Retained

- `Snapshot` type — immutable, read-only policy view
- `CachedPolicy` type — stored + compiled pair
- `Cache.Snapshot()` — returns current snapshot (copy)
- `Cache.Reload(ctx)` — fetch, compile, swap atomically
- `Compiler` — DSL compilation (unchanged)

### Added

- `Cache.Invalidate(ctx)` — direct push method for in-process mutations.
  Triggers an immediate `Reload()`. Called by the store layer after successful
  policy mutations (Create, Update, Delete, ReplaceBySource).

### Removed

- `Cache.IsStale()` — replaced by HealthTracker tier checks
- `Cache.lastUpdate` atomic — no longer needed
- `Cache.stalenessThreshold` config — replaced by health tier config
- `Cache.StartWithListener()` — replaced by PolicyPoller
- `Cache.listenLoop()` — replaced by PolicyPoller
- `Cache.Start()` (unimplemented stub) — removed
- `Cache.Wait()` / `Cache.Stop()` — lifecycle managed by Subsystem interface
- `Listener` interface — removed
- `PgListener` type — removed
- `WithStalenessThreshold` option — removed
- `WithReconnectConfig` option — removed

### PolicyPoller

```go
type PolicyPoller struct {
    store       PolicyVersionQuerier
    cache       *Cache
    tracker     *HealthTracker
    interval    time.Duration
    lastUpdated time.Time // MAX(updated_at) from previous poll
}
```

- Queries `SELECT MAX(updated_at) FROM access_policies` on each poll interval.
  The `access_policies` table has `updated_at TIMESTAMPTZ NOT NULL DEFAULT now()`
  which is updated on every Create/Update/Delete operation.
- If `MAX(updated_at)` differs from `lastUpdated`, calls `cache.Reload(ctx)`.
- On successful reload: `tracker.RecordSuccess()`.
- On poll or reload failure: `tracker.RecordFailure(reason)`.
- Default interval: 10 seconds. MUST be configurable.

### Store Layer Changes

The policy store (`internal/access/policy/store/postgres.go`) currently calls
`pg_notify('policy_changed', ...)` inside Create/Update/Delete/ReplaceBySource
transactions. These calls MUST be removed. In their place, the store MUST call
`cache.Invalidate(ctx)` after successful transaction commit.

The store MUST accept the cache via a setter or constructor option, not a direct
dependency — the cache may not exist during store construction (dependency ordering).

---

## Startup Sequence

### Phases

```text
Phase 1: INFRASTRUCTURE
  ✓ Parse config, validate environment
  ✓ Initialize logging + telemetry
  ✓ Connect to PostgreSQL
  ✓ Run schema migrations
  ✓ Start observability server (liveness probe NOW available)
     └─ /healthz/liveness → 200 (process alive)
     └─ /healthz/readiness → 503 (not ready yet)

Phase 2: SUBSYSTEM INITIALIZATION (dependency-ordered)
  ✓ Topological sort of registered Subsystems
  ✓ Start each in order (parallel where independent)
  ✓ Each Start() either returns nil (success) or error (fatal)
  ✓ Subsystems implementing HealthReporter register with ReadinessRegistry

Phase 3: READINESS GATE
  ✓ registry.WaitReady(ctx) blocks until AllReady() or timeout
  ✓ Timeout (default 30s) is a fatal startup error
  ✓ /healthz/readiness → 200

Phase 4: ACCEPT TRAFFIC
  ✓ Bind gRPC TCP listener
  ✓ grpcServer.Serve() — NOW accepting connections
  ✓ Log: "core process ready"

Phase 5: RUNTIME (steady state)
  PolicyPoller runs every 10s
  HealthTracker monitors tier transitions
  /healthz/readiness flips to 503 if aggregate health degrades
```

### Key Changes from Current Startup

| Aspect             | Current                           | New                                      |
| ------------------ | --------------------------------- | ---------------------------------------- |
| gRPC listener      | Binds during init, serves early   | Binds after AllReady()                   |
| Readiness probe    | Hardcoded `true`                  | Reflects aggregate subsystem health      |
| Liveness probe     | Always 200                        | Unchanged (always 200)                   |
| Observability      | Starts late (Phase D)             | Starts early (Phase 1)                   |
| Cache freshness    | 30s staleness timer (broken)      | HealthTracker tiers                      |
| LISTEN/NOTIFY      | Dedicated PG connection           | Removed — replaced by poller             |
| Shutdown ordering  | Ad-hoc defers                     | Reverse topological order                |
| Startup timeout    | None (hangs indefinitely)         | Configurable (default 30s)               |
| Subsystem ordering | Implicit in code layout           | Explicit dependency graph                |

### Observability Server Timing

The observability server MUST start in Phase 1 (before subsystem init). This
ensures:

- Liveness probe available during migration, ABAC setup, bootstrap
- Kubernetes/Docker sees the container as responsive during slow startup
- Metrics available to diagnose startup failures
- Readiness probe tracks initialization progress (503 → 200)

---

## ABAC Engine Changes

### Removed: Inline Staleness Check

The `IsStale()` check in `Engine.Evaluate()` (Step 6b) is removed. The engine
no longer makes health decisions inline during evaluation.

### Retained: Degraded Mode

The existing `Engine.EnterDegradedMode()` / `ClearDegradedMode()` mechanism is
retained unchanged. It already correctly denies all non-system requests when
active (Step 3 of Evaluate).

### Integration

The `HealthTracker` bridges the poller/cache and the engine:

```text
PolicyPoller
  ├─ on success → cache.Reload() → tracker.RecordSuccess()
  └─ on failure → tracker.RecordFailure(reason)

In-process mutations (store layer)
  └─ cache.Invalidate() → cache.Reload() → tracker.RecordSuccess()

HealthTracker tier transitions
  ├─ → Stale:  engine.EnterDegradedMode("policy cache stale")
  ├─ → Dead:   engine.EnterDegradedMode("policy cache dead")
  │            + initiate graceful shutdown
  └─ → Warm:   engine.ClearDegradedMode()
```

The engine's evaluation path is unchanged — it trusts its degraded flag, which
is now set by the HealthTracker instead of the inline staleness check.

---

## Interfaces Summary

### `internal/lifecycle` Package

```go
// SubsystemID is a compile-time-safe typed identifier.
type SubsystemID int

// Subsystem is a top-level server component with lifecycle management.
type Subsystem interface {
    ID()        SubsystemID
    DependsOn() []SubsystemID
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
}

// HealthTier represents operational health levels.
type HealthTier int

const (
    HealthWarm     HealthTier = iota // Normal operation
    HealthDegraded                    // Issues detected, serving last-known-good
    HealthStale                       // Extended failure, fail-closed
    HealthDead                        // Unrecoverable, shutdown imminent
)

// HealthStatus is the health report from a subsystem.
type HealthStatus struct {
    Tier   HealthTier
    Reason string
    Since  time.Time
}

// HealthReporter is implemented by subsystems with ongoing runtime state.
type HealthReporter interface {
    HealthStatus() HealthStatus
}

// ReadinessRegistry aggregates subsystem health.
type ReadinessRegistry struct { ... }

func (r *ReadinessRegistry) Register(id SubsystemID, hr HealthReporter)
func (r *ReadinessRegistry) AllReady() bool
func (r *ReadinessRegistry) Status() map[SubsystemID]HealthStatus
func (r *ReadinessRegistry) WaitReady(ctx context.Context) error

// HealthTracker manages tier transitions (generic, reusable).
type HealthTracker struct { ... }

type TrackerConfig struct {
    GracePeriod   time.Duration // time in Degraded before → Stale (default: 60s)
    MaxFailures   int           // failures before Stale → Dead (default: 30)
}

func NewHealthTracker(cfg TrackerConfig) *HealthTracker
func (ht *HealthTracker) RecordSuccess()
func (ht *HealthTracker) RecordFailure(reason string)
func (ht *HealthTracker) HealthStatus() HealthStatus // implements HealthReporter
```

---

## Metrics

| Metric                                  | Type    | Description                                |
| --------------------------------------- | ------- | ------------------------------------------ |
| `lifecycle_health_tier`                 | Gauge   | Current tier per subsystem (label: subsystem) |
| `lifecycle_health_transitions_total`    | Counter | Tier transitions (labels: subsystem, from, to) |
| `lifecycle_startup_duration_seconds`    | Histogram | Time from process start to AllReady()     |
| `abac_policy_poller_polls_total`        | Counter | Total poll attempts                        |
| `abac_policy_poller_changes_detected`   | Counter | Polls that found changes                   |
| `abac_policy_poller_errors_total`       | Counter | Poll failures                              |
| `abac_policy_cache_reloads_total`       | Counter | Cache reload attempts (labels: trigger=poll\|invalidate) |
| `abac_policy_cache_reload_errors_total` | Counter | Failed reloads                             |
| `abac_policy_cache_snapshot_age_seconds`| Gauge   | Age of current snapshot                    |

Existing metrics (`abac_engine_degraded_mode`, `abac_policy_cache_last_update`)
MUST be preserved or migrated to the new metric names with deprecation notices.

---

## Testing Strategy

### Unit Tests

- **HealthTracker:** Tier transitions, threshold behavior, terminal Dead state,
  concurrent RecordSuccess/RecordFailure safety.
- **ReadinessRegistry:** Register/AllReady/WaitReady, timeout behavior,
  dynamic health changes.
- **PolicyPoller:** Change detection, cache reload triggering, failure counting,
  interval timing.
- **PolicyCache:** Reload, Invalidate, Snapshot atomicity (existing tests
  adapted — remove staleness tests).
- **Topological sort:** Correct ordering, cycle detection, parallel grouping.

### Integration Tests

- **Startup sequence:** Subsystems start in correct order, gRPC not available
  before AllReady().
- **Health degradation:** Poller failure escalates tiers correctly, engine
  enters degraded mode at Stale.
- **Recovery:** Successful reload after degradation returns to Warm, engine
  clears degraded mode.
- **Dead state:** Max failures trigger graceful shutdown.

### E2E Tests

- **Guest login + say/pose:** The exact scenario that exposed this bug. Guest
  connects, waits for readiness, issues commands successfully.

---

## Implementation Status

### Phase 1: Policy Cache Fix — COMPLETE

All Phase 1 work is implemented and verified (`task pr-prep` green):

1. ✅ `internal/lifecycle` package: SubsystemID, Subsystem, HealthTier,
   HealthTracker, ReadinessRegistry, Orchestrator
2. ✅ PolicyCache refactored: IsStale removed, Invalidate() added
3. ✅ PolicyPoller: periodic MAX(updated\_at) replaces LISTEN/NOTIFY
4. ✅ HealthTracker wired to Engine.EnterDegradedMode via OnTierChange
5. ✅ PgListener removed, store uses onMutate → cache.Invalidate()
6. ✅ ReadinessRegistry wired to observability readiness probe

### Phase 2: Core Startup Decomposition — DESIGNED

Phase 2 decomposes `cmd/holomush/core.go` into subsystem wrappers using
the lifecycle infrastructure from Phase 1. The detailed design is in a
separate spec:

**See:** `docs/superpowers/specs/2026-04-04-core-startup-decomposition-design.md`

Key decisions made during Phase 2 design:

- Subsystem wrappers live in the packages they manage (compiler-enforced boundaries)
- Two-phase init: construct with config → Start() initializes live resources
- Observability server stays as infrastructure (not a subsystem)
- gRPC subsystem is the only wrapper in `cmd/holomush/`

---

## Related Issues

- `holomush-pogi` — Auto-reload stale policy cache (superseded by this design)
- Readiness probe hardcoded to `true` (fixed by Phase 1)
- Observability server starts too late (addressed in Phase 2 design)
