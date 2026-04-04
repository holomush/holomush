# Core Startup Decomposition Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Decompose the monolithic `cmd/holomush/core.go` (1034 lines) into focused subsystem files using the lifecycle infrastructure from Phase 1.

**Architecture:** Each subsystem wrapper lives in the package it manages. Two-phase init: construct with config references, Start() initializes live resources. The Orchestrator starts subsystems in topological order. `core.go` becomes ~100 lines of infrastructure + wiring + signal loop.

**Tech Stack:** Go 1.23, existing lifecycle package (`internal/lifecycle`), existing `CoreDeps` test injection pattern

**Spec:** `docs/superpowers/specs/2026-04-04-core-startup-decomposition-design.md`
**Prerequisite:** Phase 1 of `docs/superpowers/plans/2026-04-04-core-lifecycle-and-policy-cache.md` (complete)

---

## File Structure

### New Files

| File                                     | Responsibility                                     |
| ---------------------------------------- | -------------------------------------------------- |
| `internal/store/subsystem.go`            | Database subsystem: pool, EventStore               |
| `internal/store/subsystem_test.go`       | Database subsystem tests                           |
| `internal/access/setup/subsystem.go`     | ABAC subsystem: BuildABACStack, poller             |
| `internal/access/setup/subsystem_test.go`| ABAC subsystem tests                               |
| `internal/auth/subsystem.go`             | Auth subsystem: services, repos, hasher            |
| `internal/auth/subsystem_test.go`        | Auth subsystem tests                               |
| `internal/world/subsystem.go`            | World subsystem: WorldService with repos           |
| `internal/world/subsystem_test.go`       | World subsystem tests                              |
| `internal/plugin/subsystem.go`           | Plugin subsystem: Manager, hosts, core plugins     |
| `internal/plugin/subsystem_test.go`      | Plugin subsystem tests                             |
| `internal/session/subsystem.go`          | Session subsystem: SessionStore, reaper            |
| `internal/session/subsystem_test.go`     | Session subsystem tests                            |
| `internal/bootstrap/subsystem.go`        | Bootstrap subsystem: runner, seeds, settings       |
| `internal/bootstrap/subsystem_test.go`   | Bootstrap subsystem tests                          |
| `cmd/holomush/sub_grpc.go`              | gRPC subsystem: server, services, listener         |

### Modified Files

| File                    | Change                                              |
| ----------------------- | --------------------------------------------------- |
| `cmd/holomush/core.go`  | Shrink to infrastructure + wiring + signal loop     |
| `cmd/holomush/deps.go`  | Simplify — subsystems internalize their factories   |

### Extraction Map

This shows what code moves from `core.go` into each subsystem's `Start()`:

| Subsystem | Source lines in core.go | What moves                          |
| --------- | ----------------------- | ----------------------------------- |
| Database  | 245-284                 | EventStore creation, game ID init   |
| ABAC      | 324-348                 | BuildABACStack, poller start        |
| Auth      | 362-393                 | Auth repos, services, hasher        |
| World     | 350-360                 | World repos, WorldService           |
| Plugins   | 408-492                 | Plugin hosts, manager, core plugins |
| Sessions  | 406, 734-776            | SessionStore, reaper                |
| Bootstrap | 289-292, 395-642        | Runner, all bootstrappers, RunAll   |
| gRPC      | 535-767                 | Server, services, listener, serve   |

---

## Conventions

**All subsystem files follow this pattern:**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package <pkgname>

import "github.com/holomush/holomush/internal/lifecycle"

type SubsystemConfig struct { /* config + dependency refs */ }
type Subsystem struct { /* config + initialized state */ }

func NewSubsystem(cfg SubsystemConfig) *Subsystem { /* store config, no live resources */ }
func (s *Subsystem) ID() lifecycle.SubsystemID { return lifecycle.Subsystem<Name> }
func (s *Subsystem) DependsOn() []lifecycle.SubsystemID { return []lifecycle.SubsystemID{...} }
func (s *Subsystem) Start(ctx context.Context) error { /* create live resources */ }
func (s *Subsystem) Stop(ctx context.Context) error { /* clean up */ }
func (s *Subsystem) <Accessor>() <Type> { /* panic if not started */ }
```

**Accessor panic pattern:**

```go
func (s *Subsystem) Pool() *pgxpool.Pool {
    if s.pool == nil {
        panic("store: Pool() called before Start()")
    }
    return s.pool
}
```

**Testing convention:** Tests use ACE naming (Action, Condition, Expectation).
Example: `TestSubsystemStartCreatesPool`, `TestSubsystemPoolPanicsBeforeStart`.

**Commits:** Use jj: `JJ_EDITOR=true jj --no-pager new -m "message"` to create
a new change for each task, keeping the stacked commit approach.

**Build/test commands:**

- `task test -- ./<package>/` for package tests
- `task test` for full suite
- `task lint` for lint
- `task build` for build verification

---

## Task 1: Database Subsystem

**Files:**

- Create: `internal/store/subsystem.go`
- Create: `internal/store/subsystem_test.go`

- [ ] **Step 1: Write failing test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/internal/store"
)

func TestDatabaseSubsystemID(t *testing.T) {
	sub := store.NewSubsystem(store.SubsystemConfig{})
	assert.Equal(t, lifecycle.SubsystemDatabase, sub.ID())
}

func TestDatabaseSubsystemDependsOnNothing(t *testing.T) {
	sub := store.NewSubsystem(store.SubsystemConfig{})
	assert.Empty(t, sub.DependsOn())
}

func TestDatabaseSubsystemPoolPanicsBeforeStart(t *testing.T) {
	sub := store.NewSubsystem(store.SubsystemConfig{})
	assert.Panics(t, func() { sub.Pool() })
}

func TestDatabaseSubsystemEventStorePanicsBeforeStart(t *testing.T) {
	sub := store.NewSubsystem(store.SubsystemConfig{})
	assert.Panics(t, func() { sub.EventStore() })
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- ./internal/store/`
Expected: Compilation errors — SubsystemConfig doesn't exist.

- [ ] **Step 3: Implement database subsystem**

Create `internal/store/subsystem.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/lifecycle"
)

// SubsystemConfig configures the database subsystem.
type SubsystemConfig struct {
	// DatabaseURL is the PostgreSQL connection string.
	DatabaseURL string

	// EventStoreFactory creates an event store. If nil, uses NewPostgresEventStore.
	EventStoreFactory func(ctx context.Context, url string) (*PostgresEventStore, error)
}

// Subsystem manages the database connection pool and event store.
type Subsystem struct {
	cfg        SubsystemConfig
	eventStore *PostgresEventStore
	pool       *pgxpool.Pool
	gameID     string
}

// NewSubsystem creates a database subsystem. No live resources are allocated.
func NewSubsystem(cfg SubsystemConfig) *Subsystem {
	return &Subsystem{cfg: cfg}
}

// ID returns SubsystemDatabase.
func (s *Subsystem) ID() lifecycle.SubsystemID { return lifecycle.SubsystemDatabase }

// DependsOn returns nil — database has no dependencies.
func (s *Subsystem) DependsOn() []lifecycle.SubsystemID { return nil }

// Start connects to the database, creates the event store, and initializes
// the game ID.
func (s *Subsystem) Start(ctx context.Context) error {
	factory := s.cfg.EventStoreFactory
	if factory == nil {
		factory = func(ctx context.Context, url string) (*PostgresEventStore, error) {
			return NewPostgresEventStore(ctx, url)
		}
	}

	es, err := factory(ctx, s.cfg.DatabaseURL)
	if err != nil {
		return oops.Code("DB_CONNECT_FAILED").Wrap(err)
	}
	s.eventStore = es
	s.pool = es.Pool()

	gameID, err := es.InitGameID(ctx)
	if err != nil {
		es.Close()
		return oops.Code("GAME_ID_INIT_FAILED").Wrap(err)
	}
	s.gameID = gameID

	slog.Info("database subsystem started", "game_id", gameID)
	return nil
}

// Stop closes the event store and its connection pool.
func (s *Subsystem) Stop(_ context.Context) error {
	if s.eventStore != nil {
		s.eventStore.Close()
	}
	return nil
}

// Pool returns the database connection pool. Panics if called before Start().
func (s *Subsystem) Pool() *pgxpool.Pool {
	if s.pool == nil {
		panic("store: Pool() called before Start()")
	}
	return s.pool
}

// EventStore returns the PostgresEventStore. Panics if called before Start().
func (s *Subsystem) EventStore() *PostgresEventStore {
	if s.eventStore == nil {
		panic("store: EventStore() called before Start()")
	}
	return s.eventStore
}

// GameID returns the initialized game ID. Panics if called before Start().
func (s *Subsystem) GameID() string {
	if s.gameID == "" {
		panic("store: GameID() called before Start()")
	}
	return s.gameID
}
```

- [ ] **Step 4: Run tests**

Run: `task test -- ./internal/store/`
Expected: All tests pass (new subsystem tests + existing store tests).

- [ ] **Step 5: Run lint**

Run: `task lint`
Expected: Clean.

- [ ] **Step 6: Commit**

```bash
JJ_EDITOR=true jj --no-pager new -m "feat(store): add database subsystem wrapper"
```

---

## Task 2: ABAC Subsystem

**Files:**

- Create: `internal/access/setup/subsystem.go`
- Create: `internal/access/setup/subsystem_test.go`

- [ ] **Step 1: Write failing test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/access/setup"
	"github.com/holomush/holomush/internal/lifecycle"
)

func TestABACSubsystemID(t *testing.T) {
	sub := setup.NewSubsystem(setup.SubsystemConfig{})
	assert.Equal(t, lifecycle.SubsystemABAC, sub.ID())
}

func TestABACSubsystemDependsOnDatabase(t *testing.T) {
	sub := setup.NewSubsystem(setup.SubsystemConfig{})
	assert.Equal(t, []lifecycle.SubsystemID{lifecycle.SubsystemDatabase}, sub.DependsOn())
}

func TestABACSubsystemEnginePanicsBeforeStart(t *testing.T) {
	sub := setup.NewSubsystem(setup.SubsystemConfig{})
	assert.Panics(t, func() { sub.Engine() })
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- ./internal/access/setup/`
Expected: Compilation errors.

- [ ] **Step 3: Implement ABAC subsystem**

Create `internal/access/setup/subsystem.go`. The `Start()` method:

1. Gets the pool from the database subsystem via a `PoolProvider` interface
2. Creates the role store
3. Calls `BuildABACStack(ctx, ABACConfig{...})`
4. Registers the health tracker with the ReadinessRegistry
5. Starts the PolicyPoller goroutine

The subsystem needs a `PoolProvider` interface to avoid importing `internal/store`
directly (which would create a cross-package concrete dependency):

```go
// PoolProvider is implemented by the database subsystem.
type PoolProvider interface {
    Pool() *pgxpool.Pool
}
```

Key accessors: `Engine()`, `PolicyStore()`, `PolicyInstaller()`, `PluginProvider()`,
`HealthTracker()`, `RoleStore()`.

**Extract from core.go lines 324-348:** BuildABACStack call, poller goroutine start.

The `SubsystemConfig` should include:

- `DB PoolProvider` — database subsystem reference
- `Registry *lifecycle.ReadinessRegistry` — for health tracker registration
- `SkipSeedMigrations bool`
- `AuditMode audit.Mode` (default: `audit.ModeDenialsOnly`)

- [ ] **Step 4: Run tests**

Run: `task test -- ./internal/access/setup/`
Expected: All tests pass.

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager new -m "feat(access): add ABAC subsystem wrapper"
```

---

## Task 3: Auth Subsystem

**Files:**

- Create: `internal/auth/subsystem.go`
- Create: `internal/auth/subsystem_test.go`

- [ ] **Step 1: Write failing test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/lifecycle"
)

func TestAuthSubsystemID(t *testing.T) {
	sub := auth.NewSubsystem(auth.SubsystemConfig{})
	assert.Equal(t, lifecycle.SubsystemAuth, sub.ID())
}

func TestAuthSubsystemDependsOnDatabase(t *testing.T) {
	sub := auth.NewSubsystem(auth.SubsystemConfig{})
	assert.Equal(t, []lifecycle.SubsystemID{lifecycle.SubsystemDatabase}, sub.DependsOn())
}

func TestAuthSubsystemServicePanicsBeforeStart(t *testing.T) {
	sub := auth.NewSubsystem(auth.SubsystemConfig{})
	assert.Panics(t, func() { sub.AuthService() })
}
```

- [ ] **Step 2: Implement auth subsystem**

Create `internal/auth/subsystem.go`. The `Start()` method:

1. Gets the pool from the database subsystem via `PoolProvider`
2. Creates player repo, password reset repo, player session store
3. Creates Argon2id hasher
4. Creates AuthService, PasswordResetService, CharacterService

**Extract from core.go lines 362-393.**

Key accessors: `AuthService()`, `ResetService()`, `CharacterService()`,
`PlayerRepo()`, `PlayerSessionStore()`, `Hasher()`, `CharacterRepo()`.

Note: The `authCharRepoAdapter` and `authLocRepoAdapter` types currently in
`core.go` should move into the auth subsystem since they bridge auth and world.

- [ ] **Step 3: Run tests and commit**

Run: `task test -- ./internal/auth/`

```bash
JJ_EDITOR=true jj --no-pager new -m "feat(auth): add auth subsystem wrapper"
```

---

## Task 4: World Subsystem

**Files:**

- Create: `internal/world/subsystem.go`
- Create: `internal/world/subsystem_test.go`

- [ ] **Step 1: Write failing test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/lifecycle"
	"github.com/holomush/holomush/internal/world"
)

func TestWorldSubsystemID(t *testing.T) {
	sub := world.NewSubsystem(world.SubsystemConfig{})
	assert.Equal(t, lifecycle.SubsystemWorld, sub.ID())
}

func TestWorldSubsystemDependsOnDatabaseAndABAC(t *testing.T) {
	sub := world.NewSubsystem(world.SubsystemConfig{})
	deps := sub.DependsOn()
	assert.Contains(t, deps, lifecycle.SubsystemDatabase)
	assert.Contains(t, deps, lifecycle.SubsystemABAC)
}

func TestWorldSubsystemServicePanicsBeforeStart(t *testing.T) {
	sub := world.NewSubsystem(world.SubsystemConfig{})
	assert.Panics(t, func() { sub.Service() })
}
```

- [ ] **Step 2: Implement world subsystem**

Create `internal/world/subsystem.go`. The `Start()` method:

1. Gets pool from database subsystem
2. Gets ABAC engine from ABAC subsystem
3. Creates all world postgres repositories
4. Creates Transactor
5. Builds WorldService

**Extract from core.go lines 350-360.**

Key accessors: `Service()`, `Transactor()`.

The `SubsystemConfig` needs:

- `DB PoolProvider`
- `ABAC EngineProvider` — interface with `Engine() types.AccessPolicyEngine`

- [ ] **Step 3: Run tests and commit**

```bash
JJ_EDITOR=true jj --no-pager new -m "feat(world): add world subsystem wrapper"
```

---

## Task 5: Plugin Subsystem

**Files:**

- Create: `internal/plugin/subsystem.go`
- Create: `internal/plugin/subsystem_test.go`

- [ ] **Step 1: Write failing test and implement**

Create `internal/plugin/subsystem.go`. The `Start()` method:

1. Resolves plugin directory (from config or XDG)
2. Creates hostfunc bridge, Lua host, ServiceProxy
3. Creates LocalPluginHost, registers core plugin handlers
4. Wraps hosts with OTel instrumentation
5. Creates Manager, registers hosts
6. Loads all discovered plugins

**Extract from core.go lines 408-492.**

The `SubsystemConfig` needs:

- `DB PoolProvider`
- `ABAC` — for engine, policy installer, plugin provider
- `World` — for world service
- `Sessions` — for session store access in hostfuncs
- `DataDir string`
- `Events` — for ServiceProxy

Key accessors: `Manager()`, `CommandRegistry()` (created here, used by gRPC).

Note: The command registry (`command.NewRegistry()`, `handlers.RegisterAll()`,
`handlers.RegisterAdmin()`) currently lives in the gRPC block (core.go lines
553-561). This is a judgment call: does command registration belong in the
plugin subsystem or the gRPC subsystem? It belongs with plugins since plugins
register commands into the registry. The gRPC subsystem reads the registry
(via the dispatcher) but doesn't populate it.

- [ ] **Step 2: Run tests and commit**

```bash
JJ_EDITOR=true jj --no-pager new -m "feat(plugin): add plugin subsystem wrapper"
```

---

## Task 6: Session Subsystem

**Files:**

- Create: `internal/session/subsystem.go`
- Create: `internal/session/subsystem_test.go`

- [ ] **Step 1: Write failing test and implement**

Create `internal/session/subsystem.go`. The `Start()` method:

1. Gets pool from database subsystem
2. Creates PostgresSessionStore
3. Creates session Reaper (but does not start the goroutine — the Reaper
   needs the core engine's HandleDisconnect, which isn't available until
   gRPC subsystem starts; the reaper goroutine is started in Stop-phase-reverse
   or by the gRPC subsystem)

Actually, re-reading core.go lines 734-776: the session reaper is created
inside the gRPC block because it needs `engine.HandleDisconnect` and
`guestAuth.ReleaseGuest`. The reaper's `OnExpired` callback has live
dependencies on the core engine and guest authenticator.

**Revised approach:** The session subsystem creates the SessionStore only.
The reaper stays in the gRPC subsystem (or is created by it) since it has
dependencies on the core engine and guest authenticator that don't exist
until gRPC start time.

The `SubsystemConfig` needs:

- `DB PoolProvider`
- `SessionTTL time.Duration`
- `MaxHistory int`

Key accessors: `Store()`.

- [ ] **Step 2: Run tests and commit**

```bash
JJ_EDITOR=true jj --no-pager new -m "feat(session): add session subsystem wrapper"
```

---

## Task 7: Bootstrap Subsystem

**Files:**

- Create: `internal/bootstrap/subsystem.go`
- Create: `internal/bootstrap/subsystem_test.go`

- [ ] **Step 1: Write failing test and implement**

Create `internal/bootstrap/subsystem.go`. The `Start()` method:

1. Creates the BootstrapRunner
2. Registers policy bootstrapper (priority 200)
3. Registers admin bootstrapper (priority 400) — needs auth deps
4. Registers setting bootstrapper (priority 300) — needs plugin manager for discovery
5. Registers alias bootstrapper (priority 500) — needs alias repo + cache
6. Runs all bootstrappers via `RunAll(ctx)`
7. Resolves starting location from metadata
8. Creates alias cache and repo (used by gRPC subsystem later)

This is the most complex subsystem because it orchestrates multiple
bootstrappers with dependencies on ABAC, World, Plugins, and Auth.

The `SubsystemConfig` needs:

- `DB PoolProvider`
- `ABAC` — for policy bootstrapper, engine
- `World` — for world service, transactor
- `Plugins` — for plugin manager (setting discovery, command registration)
- `Auth` — for admin bootstrapper deps (player repo, character service, hasher)
- `Setting string`
- `ResetSetting bool`
- `SkipSeedMigrations bool`
- `GameConfig` — for disabled commands

Key accessors: `StartLocationID()`, `AliasCache()`, `AliasRepo()`,
`CommandRegistry()`.

Wait — command registry creation and command registration happens across
bootstrap and plugin subsystems. Let me reconsider:

The command registry is created, handlers are registered, aliases are
bootstrapped, plugin commands are registered, disabled commands are
unregistered — this all happens in core.go lines 553-685. This sequence
spans plugin discovery, bootstrap, and command wiring. It naturally belongs
in the bootstrap subsystem since it runs during bootstrap and its outputs
(registry, alias cache) are consumed by gRPC.

- [ ] **Step 2: Run tests and commit**

```bash
JJ_EDITOR=true jj --no-pager new -m "feat(bootstrap): add bootstrap subsystem wrapper"
```

---

## Task 8: gRPC Subsystem

**Files:**

- Create: `cmd/holomush/sub_grpc.go`

- [ ] **Step 1: Implement gRPC subsystem**

Create `cmd/holomush/sub_grpc.go`. The `Start()` method:

1. Creates core.Engine from EventStore
2. Creates gRPC server with TLS credentials
3. Creates guest authenticator (using start location from bootstrap)
4. Creates guest service
5. Creates command dispatcher
6. Creates command services
7. Completes ServiceProxy late bindings
8. Creates CoreServer, registers with gRPC
9. Creates ContentService, registers with gRPC
10. Creates session reaper, starts goroutine
11. Starts guest reaper goroutine
12. Binds TCP listener
13. Starts `grpcServer.Serve()` in goroutine

This subsystem depends on ALL other subsystems because it wires the
final gRPC server with services that touch everything.

The `SubsystemConfig` needs references to all other subsystems.

Key accessor: `Server()` (for graceful stop).

`Stop()`:

1. `grpcServer.GracefulStop()`
2. Cancel reaper contexts
3. Close listener

- [ ] **Step 2: Run build**

Run: `task build`
Expected: Clean build.

- [ ] **Step 3: Commit**

```bash
JJ_EDITOR=true jj --no-pager new -m "feat(grpc): add gRPC subsystem wrapper"
```

---

## Task 9: Rewrite core.go

**Files:**

- Modify: `cmd/holomush/core.go`
- Modify: `cmd/holomush/deps.go`

This is the integration task. Replace `runCoreWithDeps` with:

- [ ] **Step 1: Rewrite runCoreWithDeps**

The new function structure:

```go
func runCoreWithDeps(ctx context.Context, cfg *coreConfig, gameConfig config.GameConfig, cmd *cobra.Command, deps *CoreDeps) error {
    if deps == nil {
        deps = &CoreDeps{}
    }
    setDefaults(deps)

    if err := cfg.Validate(); err != nil {
        return err
    }

    // --- Infrastructure ---
    level, err := resolveLogLevel(cmd)
    if err != nil {
        return err
    }
    logging.SetDefault("holomush-core", version, cfg.LogFormat, level)

    telemetryShutdown, telErr := telemetry.Init(ctx, "holomush-core", version)
    if telErr != nil {
        return oops.Code("TELEMETRY_INIT_FAILED").Wrap(telErr)
    }
    defer func() {
        shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        if shutdownErr := telemetryShutdown(shutdownCtx); shutdownErr != nil {
            slog.Warn("telemetry shutdown error", "error", shutdownErr)
        }
    }()

    slog.Info("starting core process", "grpc_addr", cfg.GRPCAddr, "log_format", cfg.LogFormat)

    databaseURL := deps.DatabaseURLGetter()
    if databaseURL == "" {
        return oops.Code("CONFIG_INVALID").Errorf("DATABASE_URL environment variable is required")
    }

    // Run schema migrations
    migrationBoot := bootstrap.NewMigrationBootstrapper(databaseURL, deps.MigratorFactory, deps.AutoMigrateGetter())
    if migErr := migrationBoot.Bootstrap(ctx, nil, ""); migErr != nil {
        return migErr
    }

    // TLS certificates
    certsDir, err := deps.CertsDirGetter()
    if err != nil {
        return oops.Code("CERTS_DIR_FAILED").Wrap(err)
    }
    tlsConfig, err := deps.TLSCertEnsurer(certsDir, cfg.GameID)
    if err != nil {
        return oops.Code("TLS_SETUP_FAILED").Wrap(err)
    }

    // Readiness registry + observability server (infrastructure, not subsystems)
    registry := lifecycle.NewReadinessRegistry()

    var obsServer ObservabilityServer
    if cfg.MetricsAddr != "" {
        obsServer = deps.ObservabilityServerFactory(cfg.MetricsAddr, registry.AllReady)
        obsServer.MustRegister(command.CommandExecutions, command.CommandDuration, command.AliasExpansions)
        obsErrChan, obsErr := obsServer.Start()
        if obsErr != nil {
            return oops.Code("OBSERVABILITY_START_FAILED").Wrap(obsErr)
        }
        defer func() {
            shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
            defer cancel()
            if stopErr := obsServer.Stop(shutdownCtx); stopErr != nil {
                slog.Warn("error stopping observability server", "error", stopErr)
            }
        }()
        go monitorServerErrors(ctx, cancel, obsErrChan, "observability")
    }

    // --- Subsystem construction (Phase 1: config only) ---
    dbSub := store.NewSubsystem(store.SubsystemConfig{
        DatabaseURL: databaseURL,
    })
    abacSub := setup.NewSubsystem(setup.SubsystemConfig{
        DB:       dbSub,
        Registry: registry,
    })
    authSub := auth.NewSubsystem(auth.SubsystemConfig{
        DB: dbSub,
    })
    worldSub := world.NewSubsystem(world.SubsystemConfig{
        DB:   dbSub,
        ABAC: abacSub,
    })
    pluginSub := plugin.NewSubsystem(plugin.SubsystemConfig{
        DB:      dbSub,
        ABAC:    abacSub,
        World:   worldSub,
        DataDir: cfg.DataDir,
    })
    sessionSub := session.NewSubsystem(session.SubsystemConfig{
        DB:         dbSub,
        SessionTTL: sessionTTL,
        MaxHistory: cfg.SessionMaxHistory,
    })
    bootstrapSub := bootstrap.NewSubsystem(bootstrap.SubsystemConfig{
        DB:      dbSub,
        ABAC:    abacSub,
        World:   worldSub,
        Plugins: pluginSub,
        Auth:    authSub,
        Setting: cfg.Setting,
        // ...
    })
    grpcSub := newGRPCSubsystem(grpcSubsystemConfig{
        DB:        dbSub,
        ABAC:      abacSub,
        Auth:      authSub,
        World:     worldSub,
        Plugins:   pluginSub,
        Sessions:  sessionSub,
        Bootstrap: bootstrapSub,
        GRPCAddr:  cfg.GRPCAddr,
        TLSConfig: tlsConfig,
        // ...
    })

    // --- Orchestrator (Phase 2: start in dependency order) ---
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
        for id, status := range registry.Status() {
            if !status.Tier.IsReady() {
                slog.Error("subsystem not ready", "subsystem", id.String(),
                    "tier", status.Tier.String(), "reason", status.Reason)
            }
        }
        return fmt.Errorf("startup timeout: %w", err)
    }

    // --- Control server + signal loop ---
    // (control server stays as infrastructure)
    controlTLSConfig, tlsErr := deps.ControlTLSLoader(certsDir, "core")
    // ... (same as current)

    cmd.Println("Core process started")
    slog.Info("core process ready", "game_id", dbSub.GameID(), "grpc_addr", cfg.GRPCAddr)

    // Wait for shutdown signal or error
    // ... (same signal handling as current)
}
```

- [ ] **Step 2: Simplify deps.go**

Remove factory interfaces that are now internalized by subsystems:

- `EventStoreFactory` → moved into `store.SubsystemConfig`
- `PolicyBootstrapper` → moved into `bootstrap.SubsystemConfig`

Keep:

- `CertsDirGetter`, `TLSCertEnsurer` — still infrastructure
- `ControlTLSLoader`, `ControlServerFactory` — still infrastructure
- `ObservabilityServerFactory` — still infrastructure
- `DatabaseURLGetter`, `MigratorFactory`, `AutoMigrateGetter` — still infrastructure

- [ ] **Step 3: Move helper types out of core.go**

Move `bootstrapTransactor`, `authCharRepoAdapter`, `authLocRepoAdapter` from
`core.go` to the subsystem that uses them (bootstrap or auth).

Keep `ensureTLSCerts`, `fileExists`, `monitorServerErrors`, `parseAutoMigrate`,
`runAutoMigration` in `core.go` — they're infrastructure helpers.

- [ ] **Step 4: Run full test suite**

Run: `task test`
Expected: All tests pass.

Run: `task lint`
Expected: Clean.

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager new -m "refactor(core): replace monolithic startup with subsystem orchestrator"
```

---

## Task 10: Update core\_test.go

**Files:**

- Modify: `cmd/holomush/core_test.go`

- [ ] **Step 1: Update tests for new structure**

The existing `core_test.go` tests `runCoreWithDeps` with mock dependencies.
Update these tests to work with the new subsystem-based startup.

The key changes:

- `CoreDeps` has fewer fields (factories moved to subsystem configs)
- Tests that mock specific subsystem behavior should provide subsystem
  configs with mock factories instead of `CoreDeps` overrides
- Integration behavior is unchanged — tests that start the full server
  should still work

- [ ] **Step 2: Run tests**

Run: `task test -- ./cmd/holomush/`
Expected: All tests pass.

- [ ] **Step 3: Commit**

```bash
JJ_EDITOR=true jj --no-pager new -m "test(core): update core tests for subsystem-based startup"
```

---

## Task 11: Final verification

- [ ] **Step 1: Run pr-prep**

Run: `task pr-prep`
Expected: All checks pass (lint, format, schema, license, unit, integration, E2E).

- [ ] **Step 2: Verify core.go line count**

Run: `wc -l cmd/holomush/core.go`
Expected: ~150-250 lines (down from 1034).

- [ ] **Step 3: Verify subsystem files exist**

Run: `ls internal/*/subsystem.go internal/access/setup/subsystem.go cmd/holomush/sub_grpc.go`
Expected: 8 files listed.

---

## Post-Implementation Checklist

- [ ] All unit tests pass (`task test`)
- [ ] All integration tests pass (`task test:int`)
- [ ] E2E tests pass (48/48)
- [ ] `task pr-prep` passes with zero failures
- [ ] `core.go` is under 250 lines
- [ ] Each subsystem file is under 200 lines
- [ ] No subsystem directly imports another subsystem's package
  (they use `PoolProvider`/`EngineProvider` interfaces)
- [ ] Manual smoke test: guest say/pose works
