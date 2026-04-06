# Plugin Architecture Phase 1: Infrastructure Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the infrastructure for proto-first plugin architecture — service registry, binary plugin host with service injection, storage SDK, and server-provided proto services — so that Phase 2 (core→Lua migration) and Phase 3 (scenes as binary plugin) can proceed.

**Architecture:** The service registry maps proto service names to `RegisteredService` entries wrapping `grpc.ClientConnInterface`. Server-internal services (world, sessions) are adapted to in-process gRPC connections and registered alongside plugin-provided services. Binary plugins receive required services as gRPC client connections during go-plugin handshake. The plugin manifest gains `requires`, `provides`, and `storage` fields. Dependency resolution uses topological sort (DAG) to determine load order.

**Tech Stack:** Go 1.25, hashicorp/go-plugin v1.7, protobuf/gRPC, pgx/v5, ConnectRPC

**Spec:** `docs/superpowers/specs/2026-04-05-plugin-architecture-rework-design.md`

**Scope:** Phase 1 only. Phases 2-4 (core→Lua migration, scenes as binary plugin, cleanup) get separate plans.

---

## File Structure

### New Files

| File | Responsibility |
|------|---------------|
| `internal/plugin/registry.go` | ServiceRegistry interface + implementation |
| `internal/plugin/registry_test.go` | Registry tests |
| `internal/plugin/registered_service.go` | RegisteredService type + HealthReporter interface |
| `internal/plugin/dependency.go` | DAG resolution from manifest requires/provides |
| `internal/plugin/dependency_test.go` | Dependency resolution tests |
| `internal/plugin/binary_host.go` | BinaryPluginHost — go-plugin host with service injection |
| `internal/plugin/binary_host_test.go` | Binary host tests |
| `internal/plugin/grpc_proxy.go` | gRPC proxy that forwards to plugin-provided services |
| `internal/plugin/grpc_proxy_test.go` | Proxy tests |
| `internal/plugin/inprocess_conn.go` | In-process gRPC adapter (wraps Go service as ClientConnInterface) |
| `internal/plugin/inprocess_conn_test.go` | Adapter tests |
| `pkg/plugin/storage/storage.go` | Plugin storage SDK (Connect, RunMigrations) |
| `pkg/plugin/storage/storage_test.go` | Storage SDK tests |
| `api/proto/holomush/plugin/v1/services.proto` | Extended plugin handshake proto with service injection |

### Modified Files

| File | Change |
|------|--------|
| `internal/plugin/manifest.go` | Add `Requires`, `Provides`, `Storage` fields to Manifest |
| `internal/plugin/manifest_test.go` | Tests for new fields |
| `internal/plugin/manager.go` | Integrate service registry, DAG resolution, binary host |
| `internal/plugin/manager_test.go` | Updated manager tests |
| `internal/plugin/setup/subsystem.go` | Wire service registry, register server services |

---

## Task 1: RegisteredService Type and HealthReporter

**Files:**

- Create: `internal/plugin/registered_service.go`
- Create: `internal/plugin/registered_service_test.go`

- [ ] **Step 1: Write failing test for RegisteredService**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRegisteredService(t *testing.T) {
	t.Run("stores service metadata alongside connection", func(t *testing.T) {
		svc := RegisteredService{
			Name:       "holomush.scene.v1.SceneService",
			PluginName: "core-scenes",
			PluginType: TypeBinary,
		}
		assert.Equal(t, "holomush.scene.v1.SceneService", svc.Name)
		assert.Equal(t, "core-scenes", svc.PluginName)
		assert.Equal(t, TypeBinary, svc.PluginType)
	})

	t.Run("server-internal service has empty plugin name", func(t *testing.T) {
		svc := RegisteredService{
			Name:       "holomush.world.v1.WorldService",
			PluginName: "",
			PluginType: typeServerInternal,
		}
		assert.True(t, svc.IsServerInternal())
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestRegisteredService ./internal/plugin/`

Expected: Compilation error — types not defined.

- [ ] **Step 3: Implement RegisteredService**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import "google.golang.org/grpc"

// typeServerInternal is used for services provided by the server itself.
const typeServerInternal Type = "server-internal"

// HealthReporter reports the health state of a service provider.
type HealthReporter interface {
	// Healthy returns true if the service is available.
	Healthy() bool
}

// RegisteredService represents a proto service registered in the service registry.
type RegisteredService struct {
	// Name is the fully qualified proto service name (e.g., "holomush.scene.v1.SceneService").
	Name string

	// Conn is the gRPC transport to the service implementation.
	Conn grpc.ClientConnInterface

	// PluginName identifies which plugin provides this service. Empty for server-internal services.
	PluginName string

	// PluginType is the type of plugin providing this service (binary, lua, or server-internal).
	PluginType Type

	// Health reports the provider's health state. May be nil if health checking is not supported.
	Health HealthReporter
}

// IsServerInternal returns true if this service is provided by the server, not a plugin.
func (s *RegisteredService) IsServerInternal() bool {
	return s.PluginType == typeServerInternal
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestRegisteredService ./internal/plugin/`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(plugin): add RegisteredService type and HealthReporter interface"
jj new
```

---

## Task 2: Service Registry

**Files:**

- Create: `internal/plugin/registry.go`
- Create: `internal/plugin/registry_test.go`

- [ ] **Step 1: Write failing tests**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceRegistry_Register(t *testing.T) {
	t.Run("registers a service and resolves it by name", func(t *testing.T) {
		reg := NewServiceRegistry()
		svc := RegisteredService{Name: "holomush.world.v1.WorldService", PluginName: "", PluginType: typeServerInternal}

		err := reg.Register(svc)
		require.NoError(t, err)

		resolved, err := reg.Resolve("holomush.world.v1.WorldService")
		require.NoError(t, err)
		assert.Equal(t, "holomush.world.v1.WorldService", resolved.Name)
	})

	t.Run("rejects duplicate service name", func(t *testing.T) {
		reg := NewServiceRegistry()
		svc := RegisteredService{Name: "holomush.world.v1.WorldService"}

		require.NoError(t, reg.Register(svc))
		err := reg.Register(svc)
		assert.Error(t, err)
	})
}

func TestServiceRegistry_Resolve(t *testing.T) {
	t.Run("returns error for unknown service", func(t *testing.T) {
		reg := NewServiceRegistry()
		_, err := reg.Resolve("holomush.fake.v1.FakeService")
		assert.Error(t, err)
	})
}

func TestServiceRegistry_Deregister(t *testing.T) {
	t.Run("removes a registered service", func(t *testing.T) {
		reg := NewServiceRegistry()
		svc := RegisteredService{Name: "holomush.world.v1.WorldService"}
		require.NoError(t, reg.Register(svc))

		err := reg.Deregister("holomush.world.v1.WorldService")
		require.NoError(t, err)

		_, err = reg.Resolve("holomush.world.v1.WorldService")
		assert.Error(t, err)
	})

	t.Run("returns error for unknown service", func(t *testing.T) {
		reg := NewServiceRegistry()
		err := reg.Deregister("holomush.fake.v1.FakeService")
		assert.Error(t, err)
	})
}

func TestServiceRegistry_List(t *testing.T) {
	t.Run("returns all registered services", func(t *testing.T) {
		reg := NewServiceRegistry()
		require.NoError(t, reg.Register(RegisteredService{Name: "svc-a"}))
		require.NoError(t, reg.Register(RegisteredService{Name: "svc-b"}))

		all := reg.List()
		assert.Len(t, all, 2)
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run TestServiceRegistry ./internal/plugin/`

Expected: Compilation errors.

- [ ] **Step 3: Implement ServiceRegistry**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"sync"

	"github.com/samber/oops"
)

// ServiceRegistry maps proto service names to their registered implementations.
// Thread-safe for concurrent registration and resolution.
type ServiceRegistry struct {
	mu       sync.RWMutex
	services map[string]RegisteredService
}

// NewServiceRegistry creates an empty service registry.
func NewServiceRegistry() *ServiceRegistry {
	return &ServiceRegistry{services: make(map[string]RegisteredService)}
}

// Register adds a service to the registry. Returns an error if the service name is already registered.
func (r *ServiceRegistry) Register(svc RegisteredService) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.services[svc.Name]; exists {
		return oops.Code("SERVICE_ALREADY_REGISTERED").
			With("service", svc.Name).
			Errorf("service %q is already registered", svc.Name)
	}
	r.services[svc.Name] = svc
	return nil
}

// Resolve looks up a service by fully qualified proto name.
func (r *ServiceRegistry) Resolve(name string) (*RegisteredService, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	svc, ok := r.services[name]
	if !ok {
		return nil, oops.Code("SERVICE_NOT_FOUND").
			With("service", name).
			Errorf("service %q is not registered", name)
	}
	return &svc, nil
}

// Deregister removes a service from the registry.
func (r *ServiceRegistry) Deregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.services[name]; !ok {
		return oops.Code("SERVICE_NOT_FOUND").
			With("service", name).
			Errorf("service %q is not registered", name)
	}
	delete(r.services, name)
	return nil
}

// List returns all registered services.
func (r *ServiceRegistry) List() []RegisteredService {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]RegisteredService, 0, len(r.services))
	for _, svc := range r.services {
		result = append(result, svc)
	}
	return result
}
```

- [ ] **Step 4: Run tests**

Run: `task test -- -run TestServiceRegistry ./internal/plugin/`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(plugin): add ServiceRegistry for proto service resolution"
jj new
```

---

## Task 3: Manifest Schema Extensions

**Files:**

- Modify: `internal/plugin/manifest.go`
- Modify: `internal/plugin/manifest_test.go`

- [ ] **Step 1: Write failing tests for new manifest fields**

Add to `manifest_test.go`:

```go
func TestManifestRequiresProvides(t *testing.T) {
	t.Run("parses requires and provides fields", func(t *testing.T) {
		data := []byte(`
name: test-plugin
version: 1.0.0
type: binary
requires:
  - holomush.world.v1.WorldService
provides:
  - holomush.scene.v1.SceneService
binary-plugin:
  executable: ./plugin
commands:
  - name: test
    help: test command
`)
		m, err := ParseManifest(data)
		require.NoError(t, err)
		assert.Equal(t, []string{"holomush.world.v1.WorldService"}, m.Requires)
		assert.Equal(t, []string{"holomush.scene.v1.SceneService"}, m.Provides)
	})

	t.Run("rejects provides on lua plugins", func(t *testing.T) {
		data := []byte(`
name: test-plugin
version: 1.0.0
type: lua
provides:
  - holomush.scene.v1.SceneService
lua-plugin:
  entry: main.lua
commands:
  - name: test
    help: test command
`)
		_, err := ParseManifest(data)
		assert.Error(t, err)
	})
}

func TestManifestStorage(t *testing.T) {
	t.Run("parses storage field for binary plugins", func(t *testing.T) {
		data := []byte(`
name: test-plugin
version: 1.0.0
type: binary
storage: postgres
binary-plugin:
  executable: ./plugin
commands:
  - name: test
    help: test command
`)
		m, err := ParseManifest(data)
		require.NoError(t, err)
		assert.Equal(t, StoragePostgres, m.Storage)
	})

	t.Run("defaults storage to kv", func(t *testing.T) {
		data := []byte(`
name: test-plugin
version: 1.0.0
type: binary
binary-plugin:
  executable: ./plugin
commands:
  - name: test
    help: test command
`)
		m, err := ParseManifest(data)
		require.NoError(t, err)
		assert.Equal(t, StorageKV, m.Storage)
	})

	t.Run("rejects postgres storage on lua plugins", func(t *testing.T) {
		data := []byte(`
name: test-plugin
version: 1.0.0
type: lua
storage: postgres
lua-plugin:
  entry: main.lua
commands:
  - name: test
    help: test command
`)
		_, err := ParseManifest(data)
		assert.Error(t, err)
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run "TestManifestRequiresProvides|TestManifestStorage" ./internal/plugin/`

Expected: Compilation errors — `Requires`, `Provides`, `Storage`, `StoragePostgres`, `StorageKV` not defined.

- [ ] **Step 3: Add fields to Manifest struct**

In `internal/plugin/manifest.go`, add to the `Manifest` struct:

```go
// Service contract declarations
Requires []string    `yaml:"requires,omitempty" json:"requires,omitempty"`
Provides []string    `yaml:"provides,omitempty" json:"provides,omitempty"`
Storage  StorageType `yaml:"storage,omitempty" json:"storage,omitempty"`
```

Add the `StorageType`:

```go
// StorageType declares the persistence tier a plugin requires.
type StorageType string

const (
	StorageKV       StorageType = "kv"       // KV store only (default)
	StoragePostgres StorageType = "postgres" // schema-isolated PostgreSQL
)
```

- [ ] **Step 4: Add validation rules**

In `Validate()`, add:

```go
// Validate provides — only binary plugins can provide services.
if len(m.Provides) > 0 && m.Type != TypeBinary {
	return oops.Code("INVALID_PROVIDES").
		Errorf("only binary plugins can provide services")
}

// Validate storage — postgres only for binary plugins.
if m.Storage == StoragePostgres && m.Type != TypeBinary {
	return oops.Code("INVALID_STORAGE").
		Errorf("postgres storage is only available for binary plugins")
}

// Default storage to KV if not specified.
if m.Storage == "" {
	m.Storage = StorageKV
}
```

- [ ] **Step 5: Run tests**

Run: `task test -- -run "TestManifestRequiresProvides|TestManifestStorage" ./internal/plugin/`

Expected: PASS.

- [ ] **Step 6: Run full manifest test suite**

Run: `task test -- ./internal/plugin/`

Expected: All existing tests still pass.

- [ ] **Step 7: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(plugin): add requires, provides, and storage fields to manifest"
jj new
```

---

## Task 4: Dependency Resolution (DAG)

**Files:**

- Create: `internal/plugin/dependency.go`
- Create: `internal/plugin/dependency_test.go`

- [ ] **Step 1: Write failing tests**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveDependencyOrder(t *testing.T) {
	t.Run("sorts plugins so providers load before consumers", func(t *testing.T) {
		plugins := []*DiscoveredPlugin{
			{Manifest: &Manifest{Name: "consumer", Requires: []string{"svc-a"}}},
			{Manifest: &Manifest{Name: "provider", Provides: []string{"svc-a"}}},
		}
		serverServices := []string{}

		order, err := ResolveDependencyOrder(plugins, serverServices)
		require.NoError(t, err)
		assert.Equal(t, "provider", order[0].Manifest.Name)
		assert.Equal(t, "consumer", order[1].Manifest.Name)
	})

	t.Run("allows requires satisfied by server services", func(t *testing.T) {
		plugins := []*DiscoveredPlugin{
			{Manifest: &Manifest{Name: "consumer", Requires: []string{"holomush.world.v1.WorldService"}}},
		}
		serverServices := []string{"holomush.world.v1.WorldService"}

		order, err := ResolveDependencyOrder(plugins, serverServices)
		require.NoError(t, err)
		assert.Len(t, order, 1)
	})

	t.Run("detects circular dependency", func(t *testing.T) {
		plugins := []*DiscoveredPlugin{
			{Manifest: &Manifest{Name: "a", Requires: []string{"svc-b"}, Provides: []string{"svc-a"}}},
			{Manifest: &Manifest{Name: "b", Requires: []string{"svc-a"}, Provides: []string{"svc-b"}}},
		}
		_, err := ResolveDependencyOrder(plugins, nil)
		assert.Error(t, err)
	})

	t.Run("returns error for unsatisfied requires", func(t *testing.T) {
		plugins := []*DiscoveredPlugin{
			{Manifest: &Manifest{Name: "consumer", Requires: []string{"svc-missing"}}},
		}
		_, err := ResolveDependencyOrder(plugins, nil)
		assert.Error(t, err)
	})

	t.Run("handles plugins with no requires or provides", func(t *testing.T) {
		plugins := []*DiscoveredPlugin{
			{Manifest: &Manifest{Name: "standalone"}},
			{Manifest: &Manifest{Name: "provider", Provides: []string{"svc-a"}}},
		}
		order, err := ResolveDependencyOrder(plugins, nil)
		require.NoError(t, err)
		assert.Len(t, order, 2)
	})

	t.Run("respects manifest dependencies in addition to service graph", func(t *testing.T) {
		plugins := []*DiscoveredPlugin{
			{Manifest: &Manifest{Name: "dependent", Dependencies: map[string]string{"base": ">= 1.0.0"}}},
			{Manifest: &Manifest{Name: "base", Version: "1.0.0"}},
		}
		order, err := ResolveDependencyOrder(plugins, nil)
		require.NoError(t, err)
		assert.Equal(t, "base", order[0].Manifest.Name)
		assert.Equal(t, "dependent", order[1].Manifest.Name)
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run TestResolveDependencyOrder ./internal/plugin/`

Expected: Compilation error — `ResolveDependencyOrder` not defined.

- [ ] **Step 3: Implement dependency resolution**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"github.com/samber/oops"
)

// ResolveDependencyOrder sorts plugins using topological sort (Kahn's algorithm)
// so that service providers load before consumers. serverServices lists proto
// service names provided by the server itself (always available).
//
// Returns an error if:
//   - The dependency graph contains a cycle
//   - A required service is not provided by any plugin or the server
func ResolveDependencyOrder(plugins []*DiscoveredPlugin, serverServices []string) ([]*DiscoveredPlugin, error) {
	// Build maps: service name → providing plugin name, plugin name → plugin
	serviceProviders := make(map[string]string) // service → plugin name
	pluginMap := make(map[string]*DiscoveredPlugin)

	for _, svc := range serverServices {
		serviceProviders[svc] = "" // empty = server-provided
	}

	for _, p := range plugins {
		pluginMap[p.Manifest.Name] = p
		for _, svc := range p.Manifest.Provides {
			if existing, ok := serviceProviders[svc]; ok && existing != "" {
				return nil, oops.Code("DUPLICATE_SERVICE_PROVIDER").
					With("service", svc).
					With("plugin_a", existing).
					With("plugin_b", p.Manifest.Name).
					Errorf("service %q provided by multiple plugins", svc)
			}
			serviceProviders[svc] = p.Manifest.Name
		}
	}

	// Build adjacency: plugin → plugins it depends on
	// (from requires → provider, and from dependencies → named plugin)
	inDegree := make(map[string]int)
	dependsOn := make(map[string][]string) // plugin → list of plugins it waits for

	for _, p := range plugins {
		name := p.Manifest.Name
		if _, ok := inDegree[name]; !ok {
			inDegree[name] = 0
		}

		// Service requires
		for _, svc := range p.Manifest.Requires {
			provider, ok := serviceProviders[svc]
			if !ok {
				return nil, oops.Code("UNSATISFIED_REQUIRES").
					With("plugin", name).
					With("service", svc).
					Errorf("plugin %q requires service %q but no provider registered", name, svc)
			}
			if provider == "" {
				continue // server-provided, always available
			}
			if provider == name {
				continue // self-provided
			}
			dependsOn[name] = append(dependsOn[name], provider)
			inDegree[name]++
		}

		// Named dependencies
		for dep := range p.Manifest.Dependencies {
			if _, ok := pluginMap[dep]; !ok {
				return nil, oops.Code("UNSATISFIED_DEPENDENCY").
					With("plugin", name).
					With("dependency", dep).
					Errorf("plugin %q depends on %q which is not discovered", name, dep)
			}
			if dep == name {
				continue
			}
			dependsOn[name] = append(dependsOn[name], dep)
			inDegree[name]++
		}
	}

	// Ensure all dependency targets have entries
	for _, deps := range dependsOn {
		for _, dep := range deps {
			if _, ok := inDegree[dep]; !ok {
				inDegree[dep] = 0
			}
		}
	}

	// Kahn's algorithm
	var queue []string
	for name, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, name)
		}
	}

	var order []*DiscoveredPlugin
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]

		if p, ok := pluginMap[name]; ok {
			order = append(order, p)
		}

		// Find all plugins that depend on this one
		for depName, deps := range dependsOn {
			for _, dep := range deps {
				if dep == name {
					inDegree[depName]--
					if inDegree[depName] == 0 {
						queue = append(queue, depName)
					}
					break
				}
			}
		}
	}

	if len(order) != len(plugins) {
		return nil, oops.Code("CIRCULAR_DEPENDENCY").
			Errorf("circular dependency detected in plugin dependency graph")
	}

	return order, nil
}
```

- [ ] **Step 4: Run tests**

Run: `task test -- -run TestResolveDependencyOrder ./internal/plugin/`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(plugin): add DAG-based dependency resolution for plugin load order"
jj new
```

---

## Task 5: In-Process gRPC Connection Adapter

**Files:**

- Create: `internal/plugin/inprocess_conn.go`
- Create: `internal/plugin/inprocess_conn_test.go`

This adapter wraps a Go gRPC server as a `grpc.ClientConnInterface` without
actual network transport. Used to register server-internal services (world,
sessions) in the service registry using the same interface as plugin-provided
services.

- [ ] **Step 1: Write failing test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestInProcessConnSatisfiesClientConnInterface(t *testing.T) {
	t.Run("implements grpc.ClientConnInterface", func(t *testing.T) {
		srv := grpc.NewServer()
		defer srv.Stop()

		conn, err := NewInProcessConn(srv)
		require.NoError(t, err)
		defer conn.Close()

		// Verify it satisfies the interface at compile time
		var _ grpc.ClientConnInterface = conn
		assert.NotNil(t, conn)
	})
}

func TestInProcessConnInvokeReturnsUnimplemented(t *testing.T) {
	t.Run("returns unimplemented for unknown method", func(t *testing.T) {
		srv := grpc.NewServer()
		defer srv.Stop()

		conn, err := NewInProcessConn(srv)
		require.NoError(t, err)
		defer conn.Close()

		err = conn.Invoke(context.Background(), "/test.Service/Method", nil, nil)
		assert.Error(t, err)
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test -- -run TestInProcessConn ./internal/plugin/`

Expected: Compilation error — `NewInProcessConn` not defined.

- [ ] **Step 3: Implement InProcessConn**

The in-process connection uses `grpc.DialContext` with a `bufconn` listener
(from `google.golang.org/grpc/test/bufconn`) to create a loopback
connection without TCP:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"net"

	"github.com/samber/oops"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

// InProcessConn wraps a gRPC server as a grpc.ClientConnInterface
// using an in-memory buffer connection. No network transport involved.
type InProcessConn struct {
	conn     *grpc.ClientConn
	listener *bufconn.Listener
}

// NewInProcessConn creates an in-process gRPC connection to the given server.
// The server MUST have services registered before calling this.
func NewInProcessConn(srv *grpc.Server) (*InProcessConn, error) {
	lis := bufconn.Listen(bufSize)

	go func() {
		_ = srv.Serve(lis)
	}()

	conn, err := grpc.NewClient(
		"passthrough://bufconn",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		lis.Close()
		return nil, oops.Code("INPROCESS_CONN_FAILED").Wrap(err)
	}

	return &InProcessConn{conn: conn, listener: lis}, nil
}

// Invoke delegates to the underlying grpc.ClientConn.
func (c *InProcessConn) Invoke(ctx context.Context, method string, args, reply any, opts ...grpc.CallOption) error {
	return c.conn.Invoke(ctx, method, args, reply, opts...)
}

// NewStream delegates to the underlying grpc.ClientConn.
func (c *InProcessConn) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return c.conn.NewStream(ctx, desc, method, opts...)
}

// Close shuts down the in-process connection and listener.
func (c *InProcessConn) Close() error {
	if err := c.conn.Close(); err != nil {
		return err
	}
	return c.listener.Close()
}
```

- [ ] **Step 4: Run tests**

Run: `task test -- -run TestInProcessConn ./internal/plugin/`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(plugin): add in-process gRPC connection adapter for server services"
jj new
```

---

## Task 6: Plugin Storage SDK

**Files:**

- Create: `pkg/plugin/storage/storage.go`
- Create: `pkg/plugin/storage/storage_test.go`

- [ ] **Step 1: Write failing test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseSchemaFromConnString(t *testing.T) {
	t.Run("extracts schema from search_path", func(t *testing.T) {
		connStr := "postgres://user:pass@localhost/db?search_path=plugin_scenes"
		schema, err := ParseSchemaFromConnString(connStr)
		assert.NoError(t, err)
		assert.Equal(t, "plugin_scenes", schema)
	})

	t.Run("returns error when search_path missing", func(t *testing.T) {
		connStr := "postgres://user:pass@localhost/db"
		_, err := ParseSchemaFromConnString(connStr)
		assert.Error(t, err)
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestParseSchemaFromConnString ./pkg/plugin/storage/`

Expected: Compilation error.

- [ ] **Step 3: Implement storage SDK**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package storage provides database utilities for binary plugins that
// declare storage: postgres in their manifest.
package storage

import (
	"context"
	"embed"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"
)

// Connect opens a connection pool to the plugin's schema-isolated database.
// The connection string MUST include a search_path parameter scoping queries
// to the plugin's schema.
func Connect(ctx context.Context, connString string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return nil, oops.Code("PLUGIN_DB_CONNECT_FAILED").Wrap(err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, oops.Code("PLUGIN_DB_PING_FAILED").Wrap(err)
	}

	return pool, nil
}

// RunMigrations runs embedded SQL migrations against the plugin's schema.
// Migrations MUST be named sequentially: 000001_name.up.sql, 000002_name.up.sql.
// Only .up.sql files are executed. The function is idempotent — it tracks
// applied migrations in a plugin_migrations table within the plugin's schema.
func RunMigrations(ctx context.Context, pool *pgxpool.Pool, migrations embed.FS) error {
	// Create migration tracking table
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS plugin_migrations (
			version INTEGER PRIMARY KEY,
			name    TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	if err != nil {
		return oops.Code("PLUGIN_MIGRATION_TABLE_FAILED").Wrap(err)
	}

	// Get current version
	var currentVersion int
	err = pool.QueryRow(ctx, "SELECT COALESCE(MAX(version), 0) FROM plugin_migrations").Scan(&currentVersion)
	if err != nil {
		return oops.Code("PLUGIN_MIGRATION_VERSION_FAILED").Wrap(err)
	}

	// Read migration files
	entries, err := migrations.ReadDir(".")
	if err != nil {
		return oops.Code("PLUGIN_MIGRATION_READ_FAILED").Wrap(err)
	}

	// Filter and sort .up.sql files
	var upFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			upFiles = append(upFiles, e.Name())
		}
	}
	sort.Strings(upFiles)

	// Apply pending migrations
	for _, name := range upFiles {
		version := parseMigrationVersion(name)
		if version <= currentVersion {
			continue
		}

		sql, readErr := migrations.ReadFile(name)
		if readErr != nil {
			return oops.Code("PLUGIN_MIGRATION_READ_FAILED").With("file", name).Wrap(readErr)
		}

		if _, execErr := pool.Exec(ctx, string(sql)); execErr != nil {
			return oops.Code("PLUGIN_MIGRATION_EXEC_FAILED").With("file", name).Wrap(execErr)
		}

		if _, trackErr := pool.Exec(ctx,
			"INSERT INTO plugin_migrations (version, name) VALUES ($1, $2)",
			version, name,
		); trackErr != nil {
			return oops.Code("PLUGIN_MIGRATION_TRACK_FAILED").With("file", name).Wrap(trackErr)
		}
	}

	return nil
}

// ParseSchemaFromConnString extracts the schema name from a connection
// string's search_path parameter.
func ParseSchemaFromConnString(connString string) (string, error) {
	u, err := url.Parse(connString)
	if err != nil {
		return "", oops.Code("PLUGIN_CONNSTRING_PARSE_FAILED").Wrap(err)
	}
	sp := u.Query().Get("search_path")
	if sp == "" {
		return "", oops.Code("PLUGIN_MISSING_SEARCH_PATH").
			Errorf("connection string missing search_path parameter")
	}
	return sp, nil
}

// parseMigrationVersion extracts the version number from a migration filename.
// Expected format: 000001_name.up.sql → 1
func parseMigrationVersion(name string) int {
	parts := strings.SplitN(name, "_", 2)
	if len(parts) == 0 {
		return 0
	}
	var v int
	_, _ = fmt.Sscanf(parts[0], "%d", &v)
	return v
}
```

- [ ] **Step 4: Run tests**

Run: `task test -- -run TestParseSchemaFromConnString ./pkg/plugin/storage/`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(plugin): add plugin storage SDK for schema-isolated Postgres"
jj new
```

---

## Task 7: Binary Plugin Host

**Files:**

- Create: `internal/plugin/binary_host.go`
- Create: `internal/plugin/binary_host_test.go`

This is the most complex task. The binary plugin host manages go-plugin
subprocesses, injects required services via gRPC client connections, and
registers provided services in the service registry.

- [ ] **Step 1: Write failing test for BinaryPluginHost.Load**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBinaryPluginHost_Load(t *testing.T) {
	t.Run("rejects non-binary plugin type", func(t *testing.T) {
		host := NewBinaryPluginHost(BinaryHostConfig{})
		manifest := &Manifest{Name: "test", Type: TypeLua}

		err := host.Load(context.Background(), manifest, "/tmp")
		assert.Error(t, err)
	})

	t.Run("rejects manifest without executable", func(t *testing.T) {
		host := NewBinaryPluginHost(BinaryHostConfig{})
		manifest := &Manifest{Name: "test", Type: TypeBinary}

		err := host.Load(context.Background(), manifest, "/tmp")
		assert.Error(t, err)
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestBinaryPluginHost ./internal/plugin/`

Expected: Compilation error.

- [ ] **Step 3: Implement BinaryPluginHost**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"log/slog"
	"os/exec"
	"path/filepath"
	"sync"

	hashiplug "github.com/hashicorp/go-plugin"
	"github.com/samber/oops"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
	"google.golang.org/grpc"
)

// BinaryHostConfig configures the binary plugin host.
type BinaryHostConfig struct {
	Registry    *ServiceRegistry // service registry for resolution and registration
	ConnString  string           // database connection string template (plugin name substituted)
}

// BinaryPluginHost manages binary plugins via hashicorp/go-plugin.
type BinaryPluginHost struct {
	cfg     BinaryHostConfig
	mu      sync.RWMutex
	clients map[string]*hashiplug.Client
	plugins map[string]*binaryPlugin
}

type binaryPlugin struct {
	manifest *Manifest
	client   pluginv1.PluginServiceClient
}

// NewBinaryPluginHost creates a new binary plugin host.
func NewBinaryPluginHost(cfg BinaryHostConfig) *BinaryPluginHost {
	return &BinaryPluginHost{
		cfg:     cfg,
		clients: make(map[string]*hashiplug.Client),
		plugins: make(map[string]*binaryPlugin),
	}
}

// Load starts a binary plugin subprocess and establishes gRPC communication.
func (h *BinaryPluginHost) Load(ctx context.Context, manifest *Manifest, dir string) error {
	if manifest.Type != TypeBinary {
		return oops.Code("INVALID_PLUGIN_TYPE").
			With("plugin", manifest.Name).
			Errorf("BinaryPluginHost only accepts binary plugins, got %s", manifest.Type)
	}
	if manifest.BinaryPlugin == nil || manifest.BinaryPlugin.Executable == "" {
		return oops.Code("MISSING_EXECUTABLE").
			With("plugin", manifest.Name).
			Errorf("binary plugin must specify executable path")
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if _, exists := h.plugins[manifest.Name]; exists {
		return oops.Code("PLUGIN_ALREADY_LOADED").
			With("plugin", manifest.Name).
			Errorf("plugin %q is already loaded", manifest.Name)
	}

	execPath := filepath.Join(dir, manifest.BinaryPlugin.Executable)

	client := hashiplug.NewClient(&hashiplug.ClientConfig{
		HandshakeConfig: pluginsdk.HandshakeConfig,
		Plugins: map[string]hashiplug.Plugin{
			"plugin": &clientGRPCPlugin{},
		},
		Cmd:              exec.Command(execPath),
		AllowedProtocols: []hashiplug.Protocol{hashiplug.ProtocolGRPC},
	})

	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return oops.Code("PLUGIN_CLIENT_FAILED").With("plugin", manifest.Name).Wrap(err)
	}

	raw, err := rpcClient.Dispense("plugin")
	if err != nil {
		client.Kill()
		return oops.Code("PLUGIN_DISPENSE_FAILED").With("plugin", manifest.Name).Wrap(err)
	}

	pluginClient, ok := raw.(pluginv1.PluginServiceClient)
	if !ok {
		client.Kill()
		return oops.Code("PLUGIN_TYPE_ASSERTION_FAILED").With("plugin", manifest.Name).
			Errorf("dispensed plugin does not implement PluginServiceClient")
	}

	h.clients[manifest.Name] = client
	h.plugins[manifest.Name] = &binaryPlugin{
		manifest: manifest,
		client:   pluginClient,
	}

	slog.Info("binary plugin loaded", "plugin", manifest.Name, "executable", execPath)
	return nil
}

// Unload stops a binary plugin subprocess.
func (h *BinaryPluginHost) Unload(_ context.Context, name string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	client, ok := h.clients[name]
	if !ok {
		return oops.Code("PLUGIN_NOT_LOADED").With("plugin", name).Errorf("plugin %q not loaded", name)
	}

	client.Kill()
	delete(h.clients, name)
	delete(h.plugins, name)
	return nil
}

// DeliverCommand delivers a command to a binary plugin.
func (h *BinaryPluginHost) DeliverCommand(ctx context.Context, name string, cmd pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	h.mu.RLock()
	p, ok := h.plugins[name]
	h.mu.RUnlock()
	if !ok {
		return nil, oops.Code("PLUGIN_NOT_LOADED").With("plugin", name).Errorf("plugin %q not loaded", name)
	}

	resp, err := p.client.HandleCommand(ctx, &pluginv1.HandleCommandRequest{
		Command: &pluginv1.CommandRequest{
			Command:       cmd.Command,
			Args:          cmd.Args,
			CharacterId:   cmd.CharacterID,
			CharacterName: cmd.CharacterName,
			LocationId:    cmd.LocationID,
			SessionId:     cmd.SessionID,
			PlayerId:      cmd.PlayerID,
			RawInput:      cmd.InvokedAs,
		},
	})
	if err != nil {
		return nil, oops.Code("PLUGIN_COMMAND_FAILED").With("plugin", name).Wrap(err)
	}

	return protoResponseToSDK(resp.GetResponse()), nil
}

// DeliverEvent delivers an event to a binary plugin.
func (h *BinaryPluginHost) DeliverEvent(ctx context.Context, name string, event pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	h.mu.RLock()
	p, ok := h.plugins[name]
	h.mu.RUnlock()
	if !ok {
		return nil, oops.Code("PLUGIN_NOT_LOADED").With("plugin", name).Errorf("plugin %q not loaded", name)
	}

	resp, err := p.client.HandleEvent(ctx, &pluginv1.HandleEventRequest{
		Event: &pluginv1.Event{
			Id:        event.ID,
			Stream:    event.Stream,
			Type:      string(event.Type),
			Timestamp: event.Timestamp,
			ActorKind: string(event.ActorKind),
			ActorId:   event.ActorID,
			Payload:   event.Payload,
		},
	})
	if err != nil {
		return nil, oops.Code("PLUGIN_EVENT_FAILED").With("plugin", name).Wrap(err)
	}

	emits := make([]pluginsdk.EmitEvent, len(resp.GetEmitEvents()))
	for i, e := range resp.GetEmitEvents() {
		emits[i] = pluginsdk.EmitEvent{
			Stream:  e.GetStream(),
			Type:    pluginsdk.EventType(e.GetType()),
			Payload: e.GetPayload(),
		}
	}
	return emits, nil
}

// Plugins returns the names of all loaded binary plugins.
func (h *BinaryPluginHost) Plugins() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	names := make([]string, 0, len(h.plugins))
	for name := range h.plugins {
		names = append(names, name)
	}
	return names
}

// Close stops all binary plugin subprocesses.
func (h *BinaryPluginHost) Close(_ context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	for name, client := range h.clients {
		client.Kill()
		slog.Info("binary plugin stopped", "plugin", name)
	}
	h.clients = make(map[string]*hashiplug.Client)
	h.plugins = make(map[string]*binaryPlugin)
	return nil
}

// clientGRPCPlugin implements go-plugin's GRPCPlugin for the host side.
type clientGRPCPlugin struct {
	hashiplug.NetRPCUnsupportedPlugin
}

func (p *clientGRPCPlugin) GRPCClient(_ context.Context, _ *hashiplug.GRPCBroker, cc *grpc.ClientConn) (interface{}, error) {
	return pluginv1.NewPluginServiceClient(cc), nil
}

func (p *clientGRPCPlugin) GRPCServer(_ *hashiplug.GRPCBroker, _ *grpc.Server) error {
	return oops.Errorf("GRPCServer not implemented on host side")
}

// protoResponseToSDK converts a proto CommandResponse to an SDK CommandResponse.
func protoResponseToSDK(resp *pluginv1.CommandResponse) *pluginsdk.CommandResponse {
	if resp == nil {
		return &pluginsdk.CommandResponse{}
	}

	events := make([]pluginsdk.EmitEvent, len(resp.GetEvents()))
	for i, e := range resp.GetEvents() {
		events[i] = pluginsdk.EmitEvent{
			Stream:  e.GetStream(),
			Type:    pluginsdk.EventType(e.GetType()),
			Payload: e.GetPayload(),
		}
	}

	return &pluginsdk.CommandResponse{
		Status: protoStatusToSDK(resp.GetStatus()),
		Output: resp.GetOutput(),
		Events: events,
	}
}

func protoStatusToSDK(s pluginv1.CommandStatus) pluginsdk.CommandStatus {
	switch s {
	case pluginv1.CommandStatus_COMMAND_STATUS_OK:
		return pluginsdk.CommandOK
	case pluginv1.CommandStatus_COMMAND_STATUS_ERROR:
		return pluginsdk.CommandError
	case pluginv1.CommandStatus_COMMAND_STATUS_FAILURE:
		return pluginsdk.CommandFailure
	case pluginv1.CommandStatus_COMMAND_STATUS_FATAL:
		return pluginsdk.CommandFatal
	default:
		return pluginsdk.CommandOK
	}
}

// Compile-time check that BinaryPluginHost implements Host.
var _ Host = (*BinaryPluginHost)(nil)
```

- [ ] **Step 4: Run tests**

Run: `task test -- -run TestBinaryPluginHost ./internal/plugin/`

Expected: PASS.

- [ ] **Step 5: Run full plugin package tests**

Run: `task test -- ./internal/plugin/...`

Expected: All pass.

- [ ] **Step 6: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(plugin): add BinaryPluginHost with go-plugin gRPC transport"
jj new
```

---

## Task 8: Integrate Service Registry into Manager

**Files:**

- Modify: `internal/plugin/manager.go`
- Modify: `internal/plugin/manager_test.go`

- [ ] **Step 1: Add ServiceRegistry to Manager**

Add a `registry *ServiceRegistry` field to the `Manager` struct. Accept it
via `WithServiceRegistry(reg *ServiceRegistry) ManagerOption`.

- [ ] **Step 2: Update LoadAll to use DAG resolution**

Replace the simple priority-sort in `LoadAll` with `ResolveDependencyOrder`
when a service registry is configured. Fall back to priority-sort when no
registry is present (backward compatibility).

- [ ] **Step 3: Register binary host type**

In the plugin subsystem setup (`internal/plugin/setup/subsystem.go`), create
and register a `BinaryPluginHost`:

```go
binaryHost := NewBinaryPluginHost(BinaryHostConfig{
    Registry: registry,
})
manager.RegisterHost(TypeBinary, instrumentedBinaryHost)
```

- [ ] **Step 4: Run full test suite**

Run: `task test`

Expected: All pass.

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(plugin): integrate service registry and DAG resolution into Manager"
jj new
```

---

## Task 9: gRPC Proxy for Plugin-Provided Services

**Files:**

- Create: `internal/plugin/grpc_proxy.go`
- Create: `internal/plugin/grpc_proxy_test.go`

This proxy registers on the server's gRPC listener and forwards calls to
plugin-provided services via the service registry.

- [ ] **Step 1: Write failing test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGRPCProxyRegistersServiceDescriptor(t *testing.T) {
	t.Run("creates a proxy handler for a registered service", func(t *testing.T) {
		reg := NewServiceRegistry()
		proxy := NewGRPCServiceProxy(reg)
		assert.NotNil(t, proxy)
	})
}
```

- [ ] **Step 2: Implement gRPC proxy**

The gRPC proxy uses `grpc.UnknownServiceHandler` to intercept calls for
plugin-provided services and forward them to the service registry's connection:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"strings"

	"github.com/samber/oops"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GRPCServiceProxy forwards gRPC calls for plugin-provided services
// to the service registry. It is installed as a grpc.UnknownServiceHandler.
type GRPCServiceProxy struct {
	registry *ServiceRegistry
}

// NewGRPCServiceProxy creates a proxy that routes unknown gRPC methods
// to plugin-provided services via the registry.
func NewGRPCServiceProxy(registry *ServiceRegistry) *GRPCServiceProxy {
	return &GRPCServiceProxy{registry: registry}
}

// Handler returns a grpc.ServerOption that installs this proxy as the
// unknown service handler on a gRPC server.
func (p *GRPCServiceProxy) Handler() grpc.ServerOption {
	return grpc.UnknownServiceHandler(p.streamHandler)
}

// streamHandler is the grpc.StreamHandler that proxies calls.
func (p *GRPCServiceProxy) streamHandler(srv interface{}, stream grpc.ServerStream) error {
	method, ok := grpc.MethodFromServerStream(stream)
	if !ok {
		return status.Error(codes.Internal, "failed to get method from stream")
	}

	// Extract service name from method: "/package.Service/Method" → "package.Service"
	serviceName := extractServiceName(method)
	if serviceName == "" {
		return status.Errorf(codes.Unimplemented, "unknown method %s", method)
	}

	svc, err := p.registry.Resolve(serviceName)
	if err != nil {
		return status.Errorf(codes.Unimplemented, "unknown service %s", serviceName)
	}

	if svc.Conn == nil {
		return status.Errorf(codes.Unavailable, "service %s has no connection", serviceName)
	}

	if svc.Health != nil && !svc.Health.Healthy() {
		return status.Errorf(codes.Unavailable, "service %s is unhealthy", serviceName)
	}

	// Forward the call to the plugin's connection
	clientStream, err := svc.Conn.NewStream(stream.Context(), &grpc.StreamDesc{ServerStreams: true, ClientStreams: true}, method)
	if err != nil {
		return oops.With("service", serviceName).With("method", method).Wrap(err)
	}

	// Bidirectional stream proxy
	return proxyStreams(stream, clientStream)
}

// extractServiceName extracts "package.Service" from "/package.Service/Method".
func extractServiceName(fullMethod string) string {
	if !strings.HasPrefix(fullMethod, "/") {
		return ""
	}
	parts := strings.SplitN(fullMethod[1:], "/", 2)
	if len(parts) != 2 {
		return ""
	}
	return parts[0]
}

// proxyStreams bidirectionally copies between server and client streams.
func proxyStreams(srv grpc.ServerStream, cli grpc.ClientStream) error {
	// Server→Client direction
	errCh := make(chan error, 1)
	go func() {
		for {
			msg := &rawMessage{}
			if err := srv.RecvMsg(msg); err != nil {
				_ = cli.CloseSend()
				errCh <- err
				return
			}
			if err := cli.SendMsg(msg); err != nil {
				errCh <- err
				return
			}
		}
	}()

	// Client→Server direction
	for {
		msg := &rawMessage{}
		if err := cli.RecvMsg(msg); err != nil {
			// Wait for the other direction to finish
			<-errCh
			return err
		}
		if err := srv.SendMsg(msg); err != nil {
			<-errCh
			return err
		}
	}
}

// rawMessage is a pass-through message for gRPC proxying.
type rawMessage struct {
	data []byte
}

func (m *rawMessage) Marshal() ([]byte, error)   { return m.data, nil }
func (m *rawMessage) Unmarshal(b []byte) error    { m.data = b; return nil }
func (m *rawMessage) ProtoMessage()               {}
func (m *rawMessage) Reset()                      { m.data = nil }
func (m *rawMessage) String() string              { return string(m.data) }
```

- [ ] **Step 3: Run tests**

Run: `task test -- -run TestGRPCProxy ./internal/plugin/`

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(plugin): add gRPC proxy for plugin-provided services"
jj new
```

---

## Task 10: Wire Infrastructure into Plugin Subsystem

**Files:**

- Modify: `internal/plugin/setup/subsystem.go`

- [ ] **Step 1: Add ServiceRegistry to PluginSubsystem**

Add a `registry *ServiceRegistry` field. Create it in `Start()`. Expose it
via `ServiceRegistry() *ServiceRegistry`.

- [ ] **Step 2: Create and register BinaryPluginHost**

In `Start()`, create a `BinaryPluginHost` with the registry, wrap with OTel
instrumentation, and register as the binary host type.

- [ ] **Step 3: Pass registry to Manager**

Use `WithServiceRegistry(registry)` when creating the Manager.

- [ ] **Step 4: Remove explicit core plugin handler registration**

Remove the lines:

```go
localHost.RegisterHandler("core-aliases", &corealiases.Handler{}, nil)
localHost.RegisterHandler("core-building", &corebuilding.Handler{}, nil)
// etc.
```

**Note:** This step is deferred to Phase 2 (core→Lua migration). For now,
keep the explicit registrations but add the registry infrastructure alongside.
The existing core plugins continue to work via LocalPluginHost. The registry
enables binary plugins to provide and consume services.

- [ ] **Step 5: Run full test suite**

Run: `task test`

Expected: All pass.

- [ ] **Step 6: Run build**

Run: `task build`

Expected: Build succeeds.

- [ ] **Step 7: Commit**

```bash
JJ_EDITOR=true jj --no-pager describe -m "feat(plugin): wire service registry and binary host into plugin subsystem"
jj new
```

---

## Deferred to Phase 2-4

These items are explicitly out of scope for this plan:

| Item | Phase | Notes |
|------|-------|-------|
| Core→Lua plugin migration | Phase 2 | Requires Lua hostfunc parity with current ServiceProxy |
| Lua host function generation from proto | Phase 2 | Protoc plugin or standalone generator |
| Scene plugin as binary plugin | Phase 3 | First real binary plugin proving the architecture |
| ServiceProxy domain method removal | Phase 4 | Replace with proto service contracts |
| Plugin admin commands | Phase 4 | `plugin list/info/reload/disable/enable/reset-data/purge` |
| Dynamic reload support | Phase 4 | In-flight request handling during reload |
| Plugin signing | Future | Certificate format, verification flow |
