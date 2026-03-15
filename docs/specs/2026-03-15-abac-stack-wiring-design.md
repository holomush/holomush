# ABAC Stack Wiring Design

**Status:** Draft  
**Date:** 2026-03-15  
**Bead:** holomush-ur6c

## Problem

The ABAC engine and all its dependencies are fully implemented and tested in
isolation but have zero connections to the production startup path. `core.go`
constructs only the core event engine and gRPC server. Nothing in the ABAC
stack -- PolicyCache, AttributeResolver, SessionResolver, AuditLogger, Engine,
world.Service, plugin.Manager -- is instantiated at startup.

## Design

### Principles

- Separate `ABACStack` struct keeps ABAC wiring isolated from core wiring
- Single construction function with clear dependency order
- Consumers pull what they need from the returned struct
- All background goroutines MUST have their cleanup deferred immediately

### 1. ABACStack Struct

New file: `internal/access/setup.go`

```go
type ABACStack struct {
    Engine          types.AccessPolicyEngine
    Cache           *policy.Cache
    PolicyStore     store.PolicyStore
    Resolver        *attribute.Resolver
    AuditLogger     *audit.Logger
    PolicyInstaller *plugins.PolicyInstaller
}
```

### 2. ABACConfig

```go
type ABACConfig struct {
    Pool          *pgxpool.Pool
    CharacterRepo world.CharacterRepository
    AuditMode     audit.Mode    // defaults to audit.ModeDenialsOnly
    ConnStr       string        // for pg_notify listener (dedicated connection)
}
```

Note: `PluginRegistry` is NOT in ABACConfig. See section 5 for the two-phase
approach that resolves the circular dependency.

### 3. Construction Function

```go
func BuildABACStack(ctx context.Context, cfg ABACConfig) (*ABACStack, error)
```

**Exact construction order (MUST be followed):**

```text
                    +-- PolicyBootstrapper runs BEFORE this function --+
                    |  (seeds DB, creates audit partitions)            |
                    +--------------------------------------------------+

 1. policyStore  = policystore.NewPostgresStore(pool)
 2. schema       = types.NewAttributeSchema()
 3. compiler     = policy.NewCompiler(schema)
 4. cache        = policy.NewCache(policyStore, compiler)
 5. cache.Reload(ctx)                    -- populates from DB (seeds must exist)
 6. schemaReg    = attribute.NewSchemaRegistry()
 7. resolver     = attribute.NewResolver(schemaReg)
 8. resolver.RegisterProvider(NewCharacterProvider(charRepo, nil))
 9. resolver.RegisterProvider(NewPluginProvider(nil))  -- nil registry, see section 5
10. sqlDB        = stdlib.OpenDBFromPool(pool)
11. sqlDB.PingContext(ctx)               -- validate before passing to audit
12. writer       = audit.NewPostgresWriter(sqlDB)
13. auditLogger  = audit.NewLogger(mode, writer, "")
14. auditLogger.ReplayWAL(ctx)           -- drain entries from prior crash
    defer auditLogger.Close()            -- MUST defer immediately
15. sessionRes   = noopSessionResolver{}
16. engine       = policy.NewEngine(resolver, cache, sessionRes, auditLogger)
17. installer    = plugins.NewPolicyInstaller(policyStore)
```

**Critical ordering constraints:**

- PolicyBootstrapper MUST complete before `cache.Reload(ctx)` (step 5), or
  the cache loads an empty policy set and everything is default-deny.
- `sqlDB.PingContext` (step 11) MUST succeed before `NewPostgresWriter`
  (step 12), since the writer starts a background goroutine immediately.
- `auditLogger.Close()` MUST be deferred right after construction (step 13).

### 4. pg_notify Listener

New file: `internal/access/policy/pglistener.go`

```go
type PgListener struct {
    connStr string
}

func NewPgListener(connStr string) *PgListener

func (l *PgListener) Listen(ctx context.Context) (<-chan string, error)
```

Uses a dedicated pgx connection (not from the pool) with
`conn.WaitForNotification` in a loop. On connection failure, the listener
MUST reconnect internally with exponential backoff and re-issue
`LISTEN policy_changed`. The channel closes only when ctx is cancelled.

This is critical: `cache.StartWithListener` calls `listener.Listen` once
and reads from the channel. If the channel closes unexpectedly (network
failure), the cache `listenLoop` exits and stops refreshing. The listener
MUST hide reconnection from the cache -- the channel stays open, reconnection
happens behind it.

**Startup sequence after BuildABACStack returns:**

```go
listener := policy.NewPgListener(cfg.ConnStr)
go stack.Cache.StartWithListener(ctx, listener)  // BEFORE manager.LoadAll
```

### 5. Circular Dependency: PluginRegistry

The dependency graph has a cycle:

```text
Engine -> Resolver -> PluginProvider -> PluginRegistry(Manager)
Manager -> hostfunc.Functions -> Engine
```

**Resolution: Two-phase initialization.**

Phase 1 (`BuildABACStack`): Construct `PluginProvider` with `nil` registry.
During this window, `ResolveSubject` returns `nil, nil` for all plugin
subjects -- attributes are invisible, conditions fail, policies deny.

Phase 2 (after Manager construction): Call `pluginProvider.SetRegistry(manager)`.
This MUST happen before `manager.LoadAll(ctx)`.

**Why this is safe:** No plugin ABAC evaluations occur during the setup window.
Evaluations only happen when plugins call host functions, which requires
`manager.LoadAll()` to have run first. The window between `BuildABACStack`
and `SetRegistry` has zero evaluations.

```go
// PluginProvider gains:
func (p *PluginProvider) SetRegistry(r PluginRegistry) {
    p.registry = r  // safe: no concurrent evaluations during startup
}
```

The `PluginProvider` SHOULD log at Debug level in `ResolveSubject` when the
registry is nil or the plugin is not found, for observability during debugging.

### 6. SessionResolver

No-op implementation, fails closed:

```go
type noopSessionResolver struct{}

func (n noopSessionResolver) ResolveSession(
    _ context.Context, sessionID string,
) (string, error) {
    return "", oops.Code("SESSION_INVALID").
        With("session", sessionID).
        Errorf("session resolution not yet implemented")
}
```

Replaced with a real adapter when the web auth layer ships.

### 7. PluginRegistry on Manager

`plugin.Manager` MUST implement `attribute.PluginRegistry`:

```go
func (m *Manager) IsPluginLoaded(name string) bool {
    m.mu.RLock()
    defer m.mu.RUnlock()
    _, ok := m.loaded[name]
    return ok
}
```

### 8. Wiring in core.go

Full boot sequence in `runCore`:

```go
// 1. PolicyBootstrapper already ran (existing code)

// 2. Build ABAC stack
stack, err := access.BuildABACStack(ctx, access.ABACConfig{
    Pool:          pool,
    CharacterRepo: worldStore.Characters(),
    AuditMode:     audit.ModeDenialsOnly,
    ConnStr:       cfg.databaseURL,
})
defer stack.AuditLogger.Close()

// 3. Start live cache invalidation BEFORE loading plugins
listener := policy.NewPgListener(cfg.databaseURL)
go stack.Cache.StartWithListener(ctx, listener)

// 4. Build world.Service with engine
worldService := world.NewService(world.ServiceConfig{
    Engine: stack.Engine,
    // ... repos from worldStore ...
})

// 5. Build plugin stack
kvStore := worldStore.KV()  // or nil if not yet available
hostFuncs := hostfunc.New(kvStore,
    hostfunc.WithEngine(stack.Engine),
    hostfunc.WithWorldService(worldService),
    hostfunc.WithCommandRegistry(commandRegistry),
)
luaHost := pluginlua.NewHostWithFunctions(hostFuncs)
pluginManager := plugins.NewManager(pluginsDir,
    plugins.WithLuaHost(luaHost),
    plugins.WithPolicyInstaller(stack.PolicyInstaller),
)

// 6. Complete circular dependency
stack.Resolver.PluginProvider().SetRegistry(pluginManager)

// 7. Load plugins (policies installed, cache picks up via pg_notify)
pluginManager.LoadAll(ctx)
```

**KVStore:** `hostfunc.New` requires a `KVStore` as its first argument.
This comes from the existing PostgreSQL store layer. If not yet available,
pass `nil` -- hostfunc handles nil kvStore gracefully.

**WorldService repos:** `ServiceConfig` requires LocationRepo, ExitRepo,
ObjectRepo, SceneRepo, CharacterRepo, PropertyRepo, EventEmitter, and
Transactor. These all come from `store.PostgresWorldStore` accessor methods.
The implementation plan will detail exact accessor calls.

### 9. Shutdown Order

```text
1. Cancel ctx           -- stops listener, cache loop, gRPC server
2. pluginManager.Close  -- unloads plugins, removes policies
3. cache stops          -- no more reloads
4. auditLogger.Close    -- drains buffered entries, closes WAL
5. sqlDB.Close          -- closes audit DB bridge
6. pool.Close           -- closes all pg connections
```

### 10. Tests

- `TestBuildABACStack` -- constructs with test doubles, verifies all fields non-nil
- `TestBuildABACStack_CacheReloadError` -- DB error propagated
- `TestBuildABACStack_AuditPingError` -- bad pool detected early
- `TestPgListener_ReceivesNotification` -- integration test (build-tagged)
- `TestPgListener_ReconnectsOnFailure` -- integration test
- `TestNoopSessionResolver_ReturnsInvalid` -- fail-closed behavior
- `TestManagerIsPluginLoaded` -- Manager implements PluginRegistry
- `TestBootSequence_PluginsSeePolicies` -- end-to-end: build stack, start
  listener, load plugins, verify engine permits plugin operations

## Files Changed

| File | Change |
|------|--------|
| `internal/access/setup.go` | New -- ABACStack, ABACConfig, BuildABACStack |
| `internal/access/setup_test.go` | New -- construction + sequence tests |
| `internal/access/policy/pglistener.go` | New -- pg_notify Listener with reconnect |
| `internal/access/policy/pglistener_test.go` | New -- integration tests |
| `internal/access/policy/attribute/plugin_provider.go` | Add SetRegistry method |
| `internal/plugin/manager.go` | Add IsPluginLoaded method |
| `cmd/holomush/core.go` | Wire ABACStack into startup |
| `cmd/holomush/deps.go` | Add ABACConfig fields |

## Out of Scope

- Real SessionResolver (needs web auth layer)
- RoleResolver for CharacterProvider (needs role system)
- Prometheus metrics for cache staleness (observability phase)
