# ABAC Stack Wiring Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire the complete ABAC engine stack into the production startup path — from PolicyCache through Engine to world.Service and plugin.Manager.

**Architecture:** A new `BuildABACStack` function in `internal/access/setup.go` constructs all ABAC dependencies in order, returning an `ABACStack` struct. Consumers (`world.Service`, `hostfunc.Functions`, `plugin.Manager`) pull what they need. A two-phase PluginRegistry resolution breaks the circular dependency between Engine and Manager. A new `PgListener` implements live cache invalidation via PostgreSQL LISTEN/NOTIFY.

**Tech Stack:** Go 1.23, pgx v5, pgxpool/stdlib, PostgreSQL LISTEN/NOTIFY, testify

**Spec:** `docs/specs/2026-03-15-abac-stack-wiring-design.md`

---

## File Structure

### New Files

| File | Responsibility |
| ---- | -------------- |
| `internal/access/setup.go` | `ABACStack`, `ABACConfig`, `BuildABACStack`, `noopSessionResolver` |
| `internal/access/setup_test.go` | Tests for BuildABACStack construction and error paths |
| `internal/access/policy/pglistener.go` | `PgListener` — pg_notify Listener with internal reconnect |
| `internal/access/policy/pglistener_test.go` | Unit test for PgListener (mock pgx conn) |

### Modified Files

| File | Changes |
| ---- | ------- |
| `internal/access/policy/attribute/plugin_provider.go` | Add `SetRegistry` method |
| `internal/access/policy/attribute/plugin_provider_test.go` | Test for `SetRegistry` |
| `internal/plugin/manager.go` | Add `IsPluginLoaded` method |
| `internal/plugin/manager_test.go` | Test `IsPluginLoaded` |
| `cmd/holomush/core.go` | Call `BuildABACStack`, wire world.Service + plugins |
| `cmd/holomush/deps.go` | Add ABAC-related fields to `CoreDeps` |

---

## Chunk 1: Foundation — PluginProvider.SetRegistry + Manager.IsPluginLoaded

### Task 1: Add `SetRegistry` to PluginProvider

**Files:**

- Modify: `internal/access/policy/attribute/plugin_provider.go`
- Modify: `internal/access/policy/attribute/plugin_provider_test.go`

- [ ] **Step 1: Write the failing test**

In `plugin_provider_test.go`, add:

```go
func TestPluginProvider_SetRegistry(t *testing.T) {
	p := NewPluginProvider(nil)

	// Before SetRegistry: returns nil for any plugin
	attrs, err := p.ResolveSubject(context.Background(), "echo-bot")
	require.NoError(t, err)
	assert.Nil(t, attrs, "nil registry should deny")

	// Set registry
	registry := &mockPluginRegistry{loaded: map[string]bool{"echo-bot": true}}
	p.SetRegistry(registry)

	// After SetRegistry: loaded plugin returns attrs
	attrs, err = p.ResolveSubject(context.Background(), "echo-bot")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"name": "echo-bot"}, attrs)

	// Unloaded plugin still returns nil
	attrs, err = p.ResolveSubject(context.Background(), "unknown")
	require.NoError(t, err)
	assert.Nil(t, attrs)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestPluginProvider_SetRegistry ./internal/access/policy/attribute/`
Expected: FAIL — `SetRegistry` undefined

- [ ] **Step 3: Implement SetRegistry**

In `plugin_provider.go`, add:

```go
// SetRegistry sets the plugin registry for two-phase initialization.
// This is called after plugin.Manager is constructed to break the circular
// dependency between Engine and Manager. Safe to call during startup before
// any concurrent evaluations.
func (p *PluginProvider) SetRegistry(r PluginRegistry) {
	p.registry = r
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestPluginProvider ./internal/access/policy/attribute/`
Expected: PASS

- [ ] **Step 5: Commit**

```text
feat(access/attribute): add SetRegistry for two-phase PluginProvider init
```

### Task 2: Add `IsPluginLoaded` to Manager

**Files:**

- Modify: `internal/plugin/manager.go`

- [ ] **Step 1: Write the failing test**

In a test file (find existing manager test or create `manager_test.go`):

```go
func TestManager_IsPluginLoaded(t *testing.T) {
	m := plugins.NewManager("/nonexistent")

	assert.False(t, m.IsPluginLoaded("echo-bot"), "no plugins loaded yet")
}
```

Note: testing the positive case (loaded == true) requires loading a plugin which is complex. The simple test verifies the method exists and returns false for empty state. The existing integration tests exercise the loaded path.

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestManager_IsPluginLoaded ./internal/plugin/`
Expected: FAIL — `IsPluginLoaded` undefined

- [ ] **Step 3: Implement IsPluginLoaded**

In `manager.go`, add:

```go
// IsPluginLoaded returns true if the named plugin is currently loaded.
// Implements attribute.PluginRegistry for ABAC attribute resolution.
func (m *Manager) IsPluginLoaded(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.loaded[name]
	return ok
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestManager_IsPluginLoaded ./internal/plugin/`
Expected: PASS

- [ ] **Step 5: Verify compile-time interface satisfaction**

Add to `manager.go` (or its test):

```go
var _ attribute.PluginRegistry = (*Manager)(nil)
```

- [ ] **Step 6: Commit**

```text
feat(plugin): Manager implements attribute.PluginRegistry
```

---

## Chunk 2: PgListener

### Task 3: Create PgListener

**Files:**

- Create: `internal/access/policy/pglistener.go`
- Create: `internal/access/policy/pglistener_test.go`

- [ ] **Step 1: Write the test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy"
)

func TestPgListener_ImplementsInterface(t *testing.T) {
	var _ policy.Listener = (*policy.PgListener)(nil)
}

func TestPgListener_CancelsOnContextDone(t *testing.T) {
	// PgListener with invalid connStr — will fail to connect
	// but should respect context cancellation
	listener := policy.NewPgListener("postgres://invalid:5432/nonexistent")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	ch, err := listener.Listen(ctx)
	require.NoError(t, err)

	// Channel should close when context expires
	select {
	case _, ok := <-ch:
		assert.False(t, ok, "channel should close on context cancellation")
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for channel close")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestPgListener ./internal/access/policy/`
Expected: FAIL — `PgListener` undefined

- [ ] **Step 3: Implement PgListener**

Create `internal/access/policy/pglistener.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package policy

import (
	"context"
	"log/slog"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
)

// PgListener implements Listener using a dedicated PostgreSQL connection
// for LISTEN/NOTIFY. It internally reconnects with exponential backoff on
// connection failure, keeping the output channel open.
type PgListener struct {
	connStr string
}

// NewPgListener creates a listener that connects to PostgreSQL using connStr.
// The connection is dedicated (not from a pool) to avoid holding pool slots.
func NewPgListener(connStr string) *PgListener {
	return &PgListener{connStr: connStr}
}

// Listen returns a channel that emits pg_notify payloads for the
// "policy_changed" channel. The channel closes only when ctx is cancelled.
// Connection failures are handled internally with exponential backoff.
func (l *PgListener) Listen(ctx context.Context) (<-chan string, error) {
	ch := make(chan string, 16)

	go l.listenLoop(ctx, ch)

	return ch, nil
}

func (l *PgListener) listenLoop(ctx context.Context, ch chan<- string) {
	defer close(ch)

	const (
		initialBackoff = 100 * time.Millisecond
		maxBackoff     = 30 * time.Second
		backoffFactor  = 2.0
	)

	backoff := initialBackoff
	for {
		if ctx.Err() != nil {
			return
		}

		conn, err := pgx.Connect(ctx, l.connStr)
		if err != nil {
			slog.Warn("pg_notify listener: connect failed, retrying",
				"error", err, "backoff", backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = time.Duration(math.Min(
				float64(backoff)*backoffFactor,
				float64(maxBackoff),
			))
			continue
		}

		// Reset backoff on successful connect
		backoff = initialBackoff

		_, err = conn.Exec(ctx, "LISTEN policy_changed")
		if err != nil {
			slog.Warn("pg_notify listener: LISTEN failed", "error", err)
			conn.Close(ctx)
			continue
		}

		slog.Info("pg_notify listener: connected and listening")

		// Read notifications until error or context cancellation
		for {
			notification, err := conn.WaitForNotification(ctx)
			if err != nil {
				if ctx.Err() != nil {
					conn.Close(ctx)
					return
				}
				slog.Warn("pg_notify listener: notification error, reconnecting",
					"error", err)
				conn.Close(ctx)
				break // reconnect
			}

			select {
			case ch <- notification.Payload:
			case <-ctx.Done():
				conn.Close(ctx)
				return
			}
		}
	}
}
```

- [ ] **Step 4: Run tests**

Run: `task test -- -run TestPgListener ./internal/access/policy/`
Expected: PASS

- [ ] **Step 5: Commit**

```text
feat(policy): add PgListener for LISTEN/NOTIFY cache invalidation
```

---

## Chunk 3: BuildABACStack + noopSessionResolver

### Task 4: Create ABACStack, ABACConfig, noopSessionResolver, BuildABACStack

**Files:**

- Create: `internal/access/setup.go`
- Create: `internal/access/setup_test.go`

- [ ] **Step 1: Write the test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
)

func TestNoopSessionResolver_ReturnsInvalid(t *testing.T) {
	r := access.NewNoopSessionResolver()

	_, err := r.ResolveSession(context.Background(), "test-session")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not yet implemented")
}
```

Note: `TestBuildABACStack` requires a real PostgreSQL connection (for PolicyStore + audit writer). It should be an integration test. The unit test above validates the no-op resolver.

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestNoopSessionResolver ./internal/access/`
Expected: FAIL — types undefined

- [ ] **Step 3: Implement setup.go**

Create `internal/access/setup.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package access

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/policy"
	"github.com/holomush/holomush/internal/access/policy/attribute"
	"github.com/holomush/holomush/internal/access/policy/audit"
	policystore "github.com/holomush/holomush/internal/access/policy/store"
	"github.com/holomush/holomush/internal/access/policy/types"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/world"
)

// ABACStack holds all constructed ABAC components for consumption by
// the server's subsystems (world.Service, hostfunc, plugin.Manager).
type ABACStack struct {
	Engine          types.AccessPolicyEngine
	Cache           *policy.Cache
	PolicyStore     policystore.PolicyStore
	Resolver        *attribute.Resolver
	AuditLogger     *audit.Logger
	PolicyInstaller *plugins.PolicyInstaller
	PluginProvider  *attribute.PluginProvider

	// sqlDB is the database/sql bridge for audit — must be closed on shutdown
	sqlDB *sql.DB
}

// Close cleans up resources. Call during shutdown after the engine is no
// longer receiving evaluations.
func (s *ABACStack) Close() error {
	if s.AuditLogger != nil {
		s.AuditLogger.Close()
	}
	if s.sqlDB != nil {
		return s.sqlDB.Close()
	}
	return nil
}

// ABACConfig provides external dependencies that BuildABACStack cannot
// create on its own.
type ABACConfig struct {
	Pool          *pgxpool.Pool
	CharacterRepo world.CharacterRepository
	AuditMode     audit.Mode
}

// BuildABACStack constructs the complete ABAC engine stack in dependency
// order. The returned stack is ready for use except that
// PluginProvider.SetRegistry must be called after plugin.Manager is
// constructed (two-phase initialization for circular dependency resolution).
func BuildABACStack(ctx context.Context, cfg ABACConfig) (*ABACStack, error) {
	if cfg.AuditMode == "" {
		cfg.AuditMode = audit.ModeDenialsOnly
	}

	// 1. Policy store
	ps := policystore.NewPostgresStore(cfg.Pool)

	// 2-3. Schema + compiler
	schema := types.NewAttributeSchema()
	compiler := policy.NewCompiler(schema)

	// 4-5. Cache + initial load
	cache := policy.NewCache(ps, compiler)
	if err := cache.Reload(ctx); err != nil {
		return nil, oops.In("abac_setup").Wrapf(err, "initial policy cache reload")
	}

	// 6-9. Attribute resolver with providers
	schemaReg := attribute.NewSchemaRegistry()
	resolver := attribute.NewResolver(schemaReg)

	if cfg.CharacterRepo != nil {
		charProvider := attribute.NewCharacterProvider(cfg.CharacterRepo, nil)
		if err := resolver.RegisterProvider(charProvider); err != nil {
			return nil, oops.In("abac_setup").Wrapf(err, "register character provider")
		}
	}

	pluginProvider := attribute.NewPluginProvider(nil) // nil registry — set later via SetRegistry
	if err := resolver.RegisterProvider(pluginProvider); err != nil {
		return nil, oops.In("abac_setup").Wrapf(err, "register plugin provider")
	}

	// 10-11. sql.DB bridge for audit writer
	sqlDB := stdlib.OpenDBFromPool(cfg.Pool)
	if err := sqlDB.PingContext(ctx); err != nil {
		sqlDB.Close()
		return nil, oops.In("abac_setup").Wrapf(err, "audit DB ping")
	}

	// 12-14. Audit logger
	writer := audit.NewPostgresWriter(sqlDB)
	auditLogger := audit.NewLogger(cfg.AuditMode, writer, "")
	if err := auditLogger.ReplayWAL(ctx); err != nil {
		slog.Warn("audit WAL replay failed (non-fatal)", "error", err)
	}

	// 15-16. Engine
	sessionRes := &noopSessionResolver{}
	engine := policy.NewEngine(resolver, cache, sessionRes, auditLogger)

	// 17. Policy installer
	installer := plugins.NewPolicyInstaller(ps)

	return &ABACStack{
		Engine:          engine,
		Cache:           cache,
		PolicyStore:     ps,
		Resolver:        resolver,
		AuditLogger:     auditLogger,
		PolicyInstaller: installer,
		PluginProvider:  pluginProvider,
		sqlDB:           sqlDB,
	}, nil
}

// noopSessionResolver fails closed for all session resolution requests.
type noopSessionResolver struct{}

// NewNoopSessionResolver creates a session resolver that rejects all sessions.
// Exported for testing.
func NewNoopSessionResolver() *noopSessionResolver {
	return &noopSessionResolver{}
}

func (n *noopSessionResolver) ResolveSession(_ context.Context, sessionID string) (string, error) {
	return "", oops.Code("SESSION_INVALID").
		With("session", sessionID).
		Errorf("session resolution not yet implemented")
}
```

- [ ] **Step 4: Run tests**

Run: `task test -- -run TestNoopSessionResolver ./internal/access/`
Expected: PASS

- [ ] **Step 5: Run build**

Run: `task build`
Expected: PASS — verifies all imports and types resolve

- [ ] **Step 6: Commit**

```text
feat(access): add BuildABACStack for production engine wiring
```

---

## Chunk 4: Wire into core.go

### Task 5: Wire ABACStack into startup

**Files:**

- Modify: `cmd/holomush/core.go`

This task requires careful reading of `core.go` to find the exact insertion
point. The ABAC stack MUST be built after `PolicyBootstrapper` runs and
before the gRPC server starts accepting requests.

- [ ] **Step 1: Read core.go to find insertion point**

Find where `deps.PolicyBootstrapper(ctx, ...)` is called. The ABAC stack
construction goes immediately after that.

- [ ] **Step 2: Add ABAC stack construction**

After the PolicyBootstrapper call, add:

```go
// Build ABAC engine stack
abacStack, err := access.BuildABACStack(ctx, access.ABACConfig{
    Pool:          pool,
    CharacterRepo: postgres.NewCharacterRepository(pool),
    AuditMode:     audit.ModeDenialsOnly,
})
if err != nil {
    return oops.Code("ABAC_SETUP_FAILED").Wrap(err)
}
defer abacStack.Close()

// Start live policy cache invalidation
listener := policy.NewPgListener(cfg.databaseURL)
go abacStack.Cache.StartWithListener(ctx, listener)
```

- [ ] **Step 3: Add world.Service construction**

```go
worldService := world.NewService(world.ServiceConfig{
    LocationRepo:  postgres.NewLocationRepository(pool),
    ExitRepo:      postgres.NewExitRepository(pool),
    ObjectRepo:    postgres.NewObjectRepository(pool),
    SceneRepo:     postgres.NewSceneRepository(pool),
    CharacterRepo: postgres.NewCharacterRepository(pool),
    PropertyRepo:  postgres.NewPropertyRepository(pool),
    Engine:        abacStack.Engine,
    EventEmitter:  nil, // TODO: wire when event emitter is available
    Transactor:    postgres.NewTransactor(pool),
})
```

- [ ] **Step 4: Add plugin stack construction**

```go
hostFuncs := hostfunc.New(nil, // KV store not yet available
    hostfunc.WithEngine(abacStack.Engine),
    hostfunc.WithWorldService(worldService),
)
luaHost := pluginlua.NewHostWithFunctions(hostFuncs)
pluginManager := plugins.NewManager(pluginsDir,
    plugins.WithLuaHost(luaHost),
    plugins.WithPolicyInstaller(abacStack.PolicyInstaller),
)

// Complete circular dependency
abacStack.PluginProvider.SetRegistry(pluginManager)

// Load all discovered plugins
if err := pluginManager.LoadAll(ctx); err != nil {
    slog.Error("failed to load plugins", "error", err)
    // Non-fatal — server can run without plugins
}
defer pluginManager.Close(ctx)
```

- [ ] **Step 5: Add required imports**

Add imports for `access`, `policy`, `audit`, `hostfunc`, `pluginlua`, `plugins`,
`postgres` (world), etc.

- [ ] **Step 6: Build and verify**

Run: `task build`
Expected: PASS

Run: `task lint:go`
Expected: 0 issues

- [ ] **Step 7: Commit**

```text
feat(cmd): wire ABAC engine stack into production startup

Constructs full ABAC stack after policy bootstrap: cache, resolver,
providers, audit logger, engine, world.Service, plugin.Manager.
PgListener provides live cache invalidation via pg_notify.
```

---

## Chunk 5: Tests + Verification

### Task 6: Add integration test for boot sequence

**Files:**

- Create: `test/integration/access/setup_test.go` (or similar)

This test requires PostgreSQL and should use `//go:build integration`.

- [ ] **Step 1: Write integration test**

```go
//go:build integration

package access_integration_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/audit"
	// testcontainers or shared test pool setup
)

func TestBuildABACStack_Production(t *testing.T) {
	// Use testcontainers or shared test pool
	pool := setupTestPool(t)

	stack, err := access.BuildABACStack(context.Background(), access.ABACConfig{
		Pool:      pool,
		AuditMode: audit.ModeMinimal,
	})
	require.NoError(t, err)
	defer stack.Close()

	require.NotNil(t, stack.Engine)
	require.NotNil(t, stack.Cache)
	require.NotNil(t, stack.PolicyStore)
	require.NotNil(t, stack.Resolver)
	require.NotNil(t, stack.AuditLogger)
	require.NotNil(t, stack.PolicyInstaller)
	require.NotNil(t, stack.PluginProvider)
}
```

- [ ] **Step 2: Run (if test infra available)**

Run: `task test -- -tags=integration -run TestBuildABACStack_Production ./test/integration/access/`

- [ ] **Step 3: Commit**

```text
test(access): add integration test for BuildABACStack
```

### Task 7: Final verification

- [ ] **Step 1: Run full test suite**

Run: `task test`
Expected: 0 failures

- [ ] **Step 2: Run linter**

Run: `task lint`
Expected: 0 issues

- [ ] **Step 3: Run build**

Run: `task build`
Expected: clean build

- [ ] **Step 4: Verify no regressions**

Run: `task test -- -count=1 ./internal/access/... ./internal/plugin/... ./cmd/holomush/`
Expected: all pass

---

## Post-Implementation Checklist

- [ ] All unit tests pass: `task test`
- [ ] Linter clean: `task lint`
- [ ] Build succeeds: `task build`
- [ ] `BuildABACStack` constructs all 17 steps in spec order
- [ ] `PgListener` reconnects internally (channel stays open)
- [ ] `PluginProvider.SetRegistry` called before `Manager.LoadAll`
- [ ] `AuditLogger.Close` deferred after construction
- [ ] `sqlDB.PingContext` validates audit DB connection
- [ ] `noopSessionResolver` returns `SESSION_INVALID` code
- [ ] Shutdown order matches spec §9
