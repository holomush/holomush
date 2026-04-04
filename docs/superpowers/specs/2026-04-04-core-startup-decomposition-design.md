# Core Startup Decomposition Design

**Date:** 2026-04-04
**Status:** Draft
**Authors:** Sean, Claude Opus 4.6

## Problem Statement

`cmd/holomush/core.go` is a 1034-line file where `runCoreWithDeps()` handles
config validation, logging setup, database connection, TLS certificates,
ABAC construction, auth services, world service, plugin loading, bootstrap,
gRPC server creation, service registration, signal handling, and shutdown —
all in one function with local variable scoping.

This makes the startup sequence difficult to understand, test in isolation,
and extend. Adding a new subsystem requires reading 1000 lines of context
to find the right insertion point.

The lifecycle infrastructure (`internal/lifecycle`) from Phase 1 provides
the `Subsystem` interface, `Orchestrator`, and `ReadinessRegistry`. This
spec designs how to decompose `core.go` using those primitives.

## Goals

- Decompose `runCoreWithDeps()` into focused subsystem files
- Each subsystem lives in the package it manages (compiler-enforced boundaries)
- Two-phase init: construct with config → Start() initializes live resources
- Orchestrator manages dependency-ordered startup and reverse shutdown
- `core.go` becomes ~100 lines of wiring
- Preserve external contract (config flags, env vars, ports, proto APIs)

## Non-Goals

- Changing the gRPC API or proto definitions
- Changing config flag names or environment variables
- Adding new features or subsystems beyond what exists today
- Dynamic plugin loading (future work)

## RFC2119 Keywords

| Keyword      | Meaning                                    |
| ------------ | ------------------------------------------ |
| **MUST**     | Absolute requirement                       |
| **MUST NOT** | Absolute prohibition                       |
| **SHOULD**   | Recommended, may ignore with justification |
| **MAY**      | Optional                                   |

---

## Architecture

### Two-Phase Init

Subsystems use a two-phase initialization pattern:

**Phase 1 — Construction:** `core.go` creates each subsystem with its config
and references to dependency subsystems. No live resources are allocated.
Construction cannot fail (returns a struct, not an error).

**Phase 2 — Start:** The orchestrator calls `Start(ctx)` in topological
order. Each subsystem reads its dependencies' outputs via accessor methods,
creates live resources (pools, connections, goroutines), and stores them.

```go
// Phase 1: config + subsystem references
dbSub := store.NewSubsystem(store.SubsystemConfig{
    DatabaseURL: databaseURL,
})
abacSub := setup.NewSubsystem(setup.SubsystemConfig{
    Database: dbSub,
})

// Phase 2: orchestrator calls Start() in dependency order
// dbSub.Start(ctx)   — creates pool
// abacSub.Start(ctx) — calls dbSub.Pool() to get pool, builds ABAC stack
```

### Accessor Methods

Cross-subsystem references use typed accessor methods that return interfaces
where possible. Accessors MUST panic if called before `Start()` — this is a
programming error (dependency declaration bug), not a runtime condition.

```go
func (s *Subsystem) Pool() *pgxpool.Pool {
    if s.pool == nil {
        panic("store: Pool() called before Start()")
    }
    return s.pool
}
```

### Infrastructure vs. Subsystems

Infrastructure starts before the orchestrator and is not managed by it:

| Component          | Type           | Rationale                                  |
| ------------------ | -------------- | ------------------------------------------ |
| Logging/telemetry  | Infrastructure | Must be available for all subsystem logging |
| Database migration | Infrastructure | Schema must exist before subsystems start   |
| TLS certificates   | Infrastructure | Needed by gRPC subsystem at Start() time    |
| Observability      | Infrastructure | Liveness probe available during startup     |
| ReadinessRegistry  | Infrastructure | Created before subsystems, passed to them   |
| Signal handling    | Infrastructure | Runs after orchestrator starts              |

Subsystems are managed by the orchestrator:

| Subsystem | Package for wrapper | DependsOn |
| --------- | ------------------- | --------- |
| Database  | `internal/store`    | (none)    |
| ABAC      | `internal/access/setup` | Database |
| Auth      | `internal/auth`     | Database  |
| World     | `internal/world`    | Database, ABAC |
| Plugins   | `internal/plugin`   | Database  |
| Sessions  | `internal/session`  | Database  |
| Bootstrap | `internal/bootstrap`| ABAC, World, Plugins |
| gRPC      | `cmd/holomush`      | Bootstrap, Sessions, Auth |

---

## File Structure

### New Files

| File | Responsibility |
| ---- | -------------- |
| `internal/store/subsystem.go` | Database subsystem: pool, EventStore |
| `internal/access/setup/subsystem.go` | ABAC subsystem: BuildABACStack, poller |
| `internal/auth/subsystem.go` | Auth subsystem: player repo, reset repo, hasher |
| `internal/world/subsystem.go` | World subsystem: WorldService with repos |
| `internal/plugin/subsystem.go` | Plugin subsystem: Manager, hosts, core plugins |
| `internal/session/subsystem.go` | Session subsystem: SessionStore, reaper |
| `internal/bootstrap/subsystem.go` | Bootstrap subsystem: runner, seeds, settings |
| `cmd/holomush/sub_grpc.go` | gRPC subsystem: server, services, listener |

### Modified Files

| File | Change |
| ---- | ------ |
| `cmd/holomush/core.go` | Shrink to ~100 lines of infrastructure + wiring + signal loop |
| `cmd/holomush/deps.go` | Simplify or remove factory interfaces that subsystems internalize |

### Potentially Removed

| File | Condition |
| ---- | --------- |
| `cmd/holomush/deps.go` | If all factories move into subsystem configs |

---

## Dependency Graph

```text
Database
  ├── ABAC
  │     ├── World ──── Bootstrap
  │     └── Bootstrap
  ├── Auth ──── gRPC
  ├── Plugins ── Bootstrap
  ├── Sessions ── gRPC
  └── Bootstrap ── gRPC
```

The orchestrator topologically sorts this and starts in order:
`Database → ABAC → Auth → World → Plugins → Sessions → Bootstrap → gRPC`

(Auth, World, Plugins, Sessions are independent of each other and MAY start
concurrently.)

Shutdown is reverse: `gRPC → Bootstrap → Sessions → Plugins → World → Auth → ABAC → Database`

---

## Subsystem Details

### Database Subsystem (`internal/store/subsystem.go`)

**Config:**

- `DatabaseURL string`

**Start:**

- Create `pgxpool.Pool`
- Create `PostgresEventStore`

**Accessors:**

- `Pool() *pgxpool.Pool`
- `EventStore() *PostgresEventStore`

**Stop:**

- Close EventStore (closes pool)

**HealthReporter:** Yes — pool connectivity.

### ABAC Subsystem (`internal/access/setup/subsystem.go`)

**Config:**

- Reference to Database subsystem (for pool)
- Reference to ReadinessRegistry (for health registration)
- `SkipSeedMigrations bool`

**Start:**

- Call `dbSub.Pool()` to get pool
- Call `BuildABACStack(ctx, cfg)`
- Start PolicyPoller goroutine
- Register HealthTracker with ReadinessRegistry

**Accessors:**

- `Engine() types.AccessPolicyEngine`
- `PolicyStore() *policystore.PostgresStore`
- `PolicyInstaller() *plugins.PolicyInstaller`
- `PluginProvider() *attribute.PluginProvider`
- `HealthTracker() *lifecycle.HealthTracker`

**Stop:**

- Cancel poller context
- Close ABACStack

**HealthReporter:** Yes — via HealthTracker (already wired in Phase 1).

### Auth Subsystem (`internal/auth/subsystem.go`)

**Config:**

- Reference to Database subsystem

**Start:**

- Create player repository, password reset repository, player session store
- Create Argon2id hasher

**Accessors:**

- `PlayerRepo() PlayerRepository`
- `ResetRepo() PasswordResetRepository`
- `PlayerSessionStore() PlayerSessionStore`
- `Hasher() PasswordHasher`

**Stop:** No-op (stateless after init).

**HealthReporter:** No.

### World Subsystem (`internal/world/subsystem.go`)

**Config:**

- Reference to Database subsystem
- Reference to ABAC subsystem

**Start:**

- Create all repositories (location, exit, object, scene, character, property)
- Create Transactor
- Build WorldService

**Accessors:**

- `Service() *WorldService`
- `CharacterRepo() CharacterRepository`
- `LocationRepo() LocationRepository`
- `PropertyRepo() PropertyRepository`
- `Transactor() Transactor`

**Stop:** No-op (stateless coordinator).

**HealthReporter:** No.

### Plugin Subsystem (`internal/plugin/subsystem.go`)

**Config:**

- Reference to Database subsystem
- `DataDir string`
- Plugin registration config (core plugins to load)

**Start:**

- Create Lua host
- Create Manager with plugin directory
- Load all discovered plugins + core plugins
- Build command registry

**Accessors:**

- `Manager() *Manager`
- `CommandRegistry() *command.Registry`

**Stop:**

- Close Manager (unloads all plugins)

**HealthReporter:** Yes — aggregate plugin health (initially: loaded successfully).

### Session Subsystem (`internal/session/subsystem.go`)

**Config:**

- Reference to Database subsystem
- `SessionTTL time.Duration`
- `MaxHistory int`
- `ReaperInterval time.Duration`

**Start:**

- Create PostgresSessionStore
- Start reaper goroutine

**Accessors:**

- `Store() *PostgresSessionStore`

**Stop:**

- Cancel reaper context
- Close session store

**HealthReporter:** Yes — DB-backed store health.

### Bootstrap Subsystem (`internal/bootstrap/subsystem.go`)

**Config:**

- Reference to ABAC subsystem
- Reference to World subsystem
- Reference to Plugin subsystem
- `Setting string`
- `ResetSetting bool`
- `GameID string`

**Start:**

- Run bootstrap sequence (policy seeds, setting bootstrap, aliases)
- Record starting location from bootstrap metadata

**Accessors:**

- `StartLocationID() ulid.ULID`
- `GameID() string`

**Stop:** No-op (runs once).

**HealthReporter:** No.

### gRPC Subsystem (`cmd/holomush/sub_grpc.go`)

**Config:**

- References to all other subsystems (for service registration)
- `GRPCAddr string`
- `ControlAddr string`
- TLS credentials

**Start:**

- Create gRPC server with TLS
- Register core service, content service, guest service
- Create guest authenticator
- Bind TCP listener
- Start `grpcServer.Serve()` in goroutine
- Start control server

**Accessors:**

- `Server() *grpc.Server` (for graceful stop)

**Stop:**

- `grpcServer.GracefulStop()`
- Stop control server

**HealthReporter:** No (gRPC health is the readiness probe itself).

---

## What core.go Becomes

```go
func runCoreWithDeps(ctx context.Context, cfg *coreConfig, ...) error {
    // --- Infrastructure ---
    setupLogging(cfg)
    defer shutdownTelemetry()
    databaseURL := getDatabaseURL()
    runMigrations(databaseURL)
    ensureTLSCerts()
    registry := lifecycle.NewReadinessRegistry()
    startObservabilityServer(cfg.MetricsAddr, registry)

    // --- Subsystem construction (Phase 1) ---
    dbSub := store.NewSubsystem(...)
    abacSub := setup.NewSubsystem(dbSub, registry, ...)
    authSub := auth.NewSubsystem(dbSub)
    worldSub := world.NewSubsystem(dbSub, abacSub)
    pluginSub := plugin.NewSubsystem(dbSub, ...)
    sessionSub := session.NewSubsystem(dbSub, ...)
    bootstrapSub := bootstrap.NewSubsystem(abacSub, worldSub, pluginSub, ...)
    grpcSub := newGRPCSubsystem(bootstrapSub, sessionSub, ...)

    // --- Orchestrator (Phase 2) ---
    orch := lifecycle.NewOrchestrator()
    for _, sub := range []lifecycle.Subsystem{
        dbSub, abacSub, authSub, worldSub,
        pluginSub, sessionSub, bootstrapSub, grpcSub,
    } {
        orch.Register(sub)
    }
    if err := orch.StartAll(ctx); err != nil {
        return err
    }
    defer orch.StopAll(ctx)

    // --- Readiness gate ---
    readinessCtx, readinessCancel := context.WithTimeout(ctx, 30*time.Second)
    defer readinessCancel()
    if err := registry.WaitReady(readinessCtx); err != nil {
        return fmt.Errorf("startup timeout: %w", err)
    }
    slog.Info("core process ready", ...)

    // --- Signal loop ---
    return waitForSignal(ctx)
}
```

---

## Testing Strategy

### Unit Tests

Each subsystem wrapper gets its own test file (`subsystem_test.go`) in the
same package. Tests verify:

- `Start()` creates expected resources
- Accessor panics before `Start()`
- `Stop()` cleans up resources
- `DependsOn()` returns correct dependencies
- `ID()` returns correct SubsystemID

### Integration Tests

The existing integration tests (`test/integration/`) MUST continue to pass.
They test end-to-end behavior through the gRPC API, which is unchanged.

### core_test.go

The existing `cmd/holomush/core_test.go` tests `runCoreWithDeps()` with
mock dependencies. These tests MUST be updated to work with the new
structure. The `CoreDeps` pattern may simplify since subsystems internalize
their own factory logic.

---

## Migration Path

This is a pure structural refactor. No external behavior changes. The
implementation order:

1. Create subsystem wrappers (one file per package, implementing Subsystem)
2. Verify each subsystem compiles and its tests pass independently
3. Rewrite `core.go` to use subsystems + orchestrator
4. Update `core_test.go`
5. Run full test suite (`task pr-prep`)

Steps 1-2 can be done incrementally — each subsystem wrapper is independent
until step 3 wires them all together.
