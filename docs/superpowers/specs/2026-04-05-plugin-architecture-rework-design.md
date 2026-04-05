# Plugin Architecture Rework Design

**Epic:** Plugin System | **Status:** Draft | **Date:** 2026-04-05

## Overview

Replace the current plugin system — which requires compiled-in registration,
a monolithic ServiceProxy, and manual gRPC wiring — with a proto-first
architecture where plugins are self-contained, auto-discovered, and
communicate exclusively through protobuf service contracts.

Every domain service (world, scenes, channels, forums) is defined as a
protobuf service contract. Plugins declare what they `require` and `provide`
in their manifest. The plugin manager resolves a DAG of dependencies, loads
plugins in order, injects required services, and registers provided services.
No plugin leaks beyond its package boundary.

## RFC2119 Keywords

The keywords MUST, MUST NOT, SHOULD, SHOULD NOT, and MAY are used per RFC2119.

## Design Decisions

| #   | Decision                                                                 | Rationale                                                                                                              |
| --- | ------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------- |
| D1  | Proto is the universal service contract language                         | Already used for gRPC; works across go-plugin (gRPC transport), Lua (generated bindings), and server-internal services |
| D2  | Plugins are service providers AND consumers                              | Scene plugin provides SceneService, consumes WorldService. Bidirectional service mesh, not one-way injection           |
| D3  | `core` plugin type removed                                               | Compiled-in Go plugins create import coupling. Existing core plugins migrate to `lua` or `binary`                      |
| D4  | Tiered storage: KV for Lua, schema-isolated Postgres for binary          | Matches capability needs. Lua plugins are simple; complex plugins use go-plugin and get full SQL                       |
| D5  | No cross-schema database access                                          | Plugin Postgres schemas cannot read or write `public` schema. All cross-domain data flows through proto contracts      |
| D6  | Server proxies plugin gRPC services                                      | One external port. Server owns TLS, auth, rate limiting. Plugin never binds a port.                                    |
| D7  | Lua host functions auto-generated from proto                             | Eliminates hand-maintained hostfunc bindings. Adding a service = define proto + run code generator                     |
| D8  | Dependency graph MUST be a DAG                                           | Circular dependencies are a fatal startup error                                                                        |
| D9  | Plugin types describe runtime behavior, dependencies describe ecosystems | A D&D game system is a setting + binary mechanics plugin + dependency declarations, not a new plugin type              |

## 1. Plugin Types

Three plugin types remain. `core` is removed.

| Type      | Runtime                     | Commands | Events | Service contracts                                | Storage                          | Discovery                       |
| --------- | --------------------------- | -------- | ------ | ------------------------------------------------ | -------------------------------- | ------------------------------- |
| `lua`     | gopher-lua VM per delivery  | Yes      | Yes    | Consume only (via auto-generated host functions) | KV only                          | Auto (plugin.yaml)              |
| `binary`  | hashicorp/go-plugin (gRPC)  | Yes      | Yes    | Consume and provide                              | KV or Postgres (schema-isolated) | Auto (plugin.yaml + executable) |
| `setting` | Bootstrap only (no runtime) | No       | No     | No                                               | None (reads files)               | Auto (plugin.yaml)              |

### 1.1 Irreducible Server-Internal Commands

These commands MUST remain compiled into the server binary because they
require direct access to session teardown, server lifecycle, or auth
internals that cannot be safely exposed across a plugin boundary:

- `quit` — ends the invoking session
- `shutdown` — initiates server shutdown
- `resetpassword` — modifies auth credentials

Everything else — including all current core plugins — migrates to `lua`
or `binary`.

### 1.2 Plugin Type Selection Guide

| Plugin needs...                                        | Use       |
| ------------------------------------------------------ | --------- |
| Simple command handlers (marshal payload, emit events) | `lua`     |
| Complex domain logic, own database, provides a service | `binary`  |
| World content seeding (locations, NPCs, themes)        | `setting` |
| Game system mechanics (character sheets, dice, combat) | `binary`  |

### 1.3 Current Core Plugin Migration Targets

| Current plugin       | Target type | Rationale                             |
| -------------------- | ----------- | ------------------------------------- |
| `core-communication` | `lua`       | Simple payload marshal + event emit   |
| `core-building`      | `lua`       | Same pattern                          |
| `core-objects`       | `lua`       | Same pattern                          |
| `core-aliases`       | `lua`       | Alias CRUD via host functions         |
| `core-help`          | `lua`       | Registry query via host functions     |
| `core-scenes`        | `binary`    | Provides SceneService, needs Postgres |

## 2. Proto-First Service Contracts

Every domain service is defined as a protobuf service. This is the universal
contract language across all plugin types and transports.

### 2.1 Contract Location

Service contracts live in `api/proto/` (existing convention). A contract
defines the operations (RPCs), request/response types, and service identity.

### 2.2 Server-Provided Services

These services are implemented by the server and registered in the service
registry for plugins to consume:

| Proto service                        | Current implementation                      |
| ------------------------------------ | ------------------------------------------- |
| `holomush.world.v1.WorldService`     | New proto wrapping `internal/world.Service` |
| `holomush.session.v1.SessionService` | New proto wrapping session operations       |
| `holomush.auth.v1.AuthService`       | New proto wrapping auth operations          |
| `holomush.content.v1.ContentService` | Existing proto                              |
| `holomush.event.v1.EventService`     | New proto wrapping event store operations   |

### 2.3 Plugin-Provided Services

Plugins that declare `provides:` register their service implementation in
the service registry after loading. Other plugins and the server can consume
these services through the registry.

Example: the scene plugin provides `holomush.scene.v1.SceneService`.

### 2.4 Manifest Declarations

```yaml
name: core-scenes
version: 1.0.0
type: binary

requires:
  - holomush.world.v1.WorldService
  - holomush.session.v1.SessionService

provides:
  - holomush.scene.v1.SceneService

storage: postgres

commands:
  - name: scene
    capabilities:
      - action: write
        resource: scene
        scope: local
    help: "Manage RP scenes"
    usage: "scene <subcommand> [args]"
```

The plugin manager validates that all `requires` services are available
before loading the plugin. Missing requirements are a fatal load error
for that plugin.

## 3. Service Registry

The plugin manager maintains a service registry — a runtime map of proto
service names to their implementations.

### 3.1 RegisteredService

```go
type RegisteredService struct {
    Name       string                    // "holomush.scene.v1.SceneService"
    Conn       grpc.ClientConnInterface  // the transport
    PluginName string                    // "core-scenes" or "" for server-internal
    PluginType PluginType                // binary, lua, or server-internal
    Health     HealthReporter            // reports health state
}
```

Everything is a `grpc.ClientConnInterface` — whether the implementation is
in-process (server-provided services) or out-of-process (binary plugin
services). Consumers do not know or care where the service lives.

### 3.2 Server-Provided Service Registration

Server-internal services (world, sessions, auth) MUST be wrapped in an
in-process gRPC server adapter and registered in the same registry as
plugin-provided services. This ensures a uniform interface: a Lua plugin
calling `world.get_location()` goes through the same registry path as a
binary plugin calling `WorldService.GetLocation()`.

### 3.3 Registry Interface

```go
type ServiceRegistry interface {
    Register(service RegisteredService) error
    Resolve(serviceName string) (*RegisteredService, error)
    List() []RegisteredService
    Deregister(serviceName string) error
}
```

### 3.4 Dependency Resolution

The plugin manager resolves the dependency graph from manifest declarations:

1. Parse all manifests and extract `requires`, `provides`, `dependencies`
2. Build a DAG (Directed Acyclic Graph) from:
   - `requires` → the service MUST be provided by another plugin or the server
   - `provides` → the plugin registers this service after loading
   - `dependencies` → the named plugin MUST be loaded first (for version constraints)
3. Topological sort (Kahn's algorithm, matching the lifecycle orchestrator pattern)
4. Circular dependencies are a fatal startup error
5. Load plugins in resolved order

## 4. Plugin Lifecycle

### 4.1 Startup Flow

1. **Discover** — Scan plugins directory, parse all `plugin.yaml` manifests
2. **Register server services** — Wrap server-internal implementations and register in the service registry
3. **Resolve** — Build DAG from `requires`/`provides`/`dependencies`. Topological sort determines load order. Circular dependencies are a fatal error.
4. **Load** — For each plugin in resolved order:
   - `setting`: Run bootstrap (seed content)
   - `lua`: Load into Lua host, inject auto-generated host functions for required services
   - `binary`: Start go-plugin subprocess, handshake, pass connection string (if `storage: postgres`), inject required services as gRPC client connections
5. **Register provided services** — After a plugin starts successfully, its `provides` services are registered in the service registry
6. **Register commands** — Commands from all loaded plugins are registered in the command registry
7. **Expose gRPC** — For each provided service, the server registers a gRPC proxy on its listener that forwards to the plugin's go-plugin transport

### 4.2 Lifecycle Events

```text
Plugin loaded    -> register services, register commands, start health checks
Plugin unhealthy -> degrade provided services, notify dependents
Plugin recovered -> restore services
Plugin unloaded  -> deregister services, deregister commands, notify dependents
```

### 4.3 Health Checking

| Plugin type | Health mechanism                                                                                                                   |
| ----------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| `binary`    | gRPC health check protocol (`grpc.health.v1.Health`) — go-plugin already supports this                                             |
| `lua`       | Host-managed — Lua VM is healthy if it can execute. Host tracks delivery failures and marks unhealthy after configurable threshold |
| `setting`   | Not applicable (no runtime)                                                                                                        |

Plugin health is aggregated into the server's `ReadinessRegistry` (from
the lifecycle package). A plugin going unhealthy triggers degraded mode
for services it provides.

### 4.4 Observability

| Signal  | Mechanism                                                                                                                                            |
| ------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| Metrics | OTel instrumentation wraps service registry calls — latency, error rate, per-plugin, per-service                                                     |
| Tracing | Trace context propagates through gRPC naturally (go-plugin transport carries trace headers). Lua calls get spans from host wrapper                   |
| Logging | Plugins log via base proxy `Log()`. Binary plugin log messages forward over go-plugin transport to host's structured logger, tagged with plugin name |

## 5. Plugin Storage

### 5.1 Lua Plugins

KV store only. Existing `KVGet`/`KVSet`/`KVDelete` on the base proxy.
No change from current behavior.

### 5.2 Binary Plugins

Two tiers, declared in manifest:

```yaml
storage: kv       # default — KV store only
storage: postgres  # full Postgres with schema isolation
```

### 5.3 Postgres Storage Flow

1. Plugin declares `storage: postgres` in manifest
2. At load time, plugin manager creates the schema:
   `CREATE SCHEMA IF NOT EXISTS plugin_<name>`
3. Plugin manager creates a schema-scoped PostgreSQL role with:
   - `USAGE` and `CREATE` on `plugin_<name>` schema
   - No access to `public` schema or any other plugin's schema
4. Connection string (with `search_path=plugin_<name>`) is passed to the
   plugin during go-plugin handshake
5. Plugin uses the **plugin storage SDK** (`pkg/plugin/storage`) to:
   - Open a `pgxpool.Pool` from the connection string
   - Run embedded migrations (same sequential numbering pattern as core)
   - Access its tables within its schema
6. At unload, the pool is closed. Schema and data persist.

### 5.4 Schema Isolation

Plugin schemas MUST NOT have access to the `public` schema or other plugin
schemas. All cross-domain data flows through proto service contracts.

If a plugin needs to reference core entities (characters, locations), it
stores the ID as a plain `TEXT` column — no foreign key constraints across
schemas. Referential integrity across the service boundary is the plugin's
responsibility via application logic.

### 5.5 Plugin Storage SDK

Provided in `pkg/plugin/storage` for binary plugin authors:

```go
func Connect(ctx context.Context, connString string) (*pgxpool.Pool, error)
func RunMigrations(pool *pgxpool.Pool, migrations embed.FS) error
```

Minimal API. The plugin owns its schema entirely.

## 6. Base Proxy

After proto contracts handle domain services, the base proxy is the
irreducible set of capabilities every plugin gets regardless of declared
services:

| Capability      | Why baseline                                                 |
| --------------- | ------------------------------------------------------------ |
| Event emission  | Every plugin emits events — it's how commands produce output |
| Logging         | Every plugin needs structured logging                        |
| Plugin KV store | Stateful plugins need per-plugin key-value storage           |

Everything else — world queries, session management, aliases, commands,
content — becomes a proto service that plugins explicitly `require`.

## 7. Lua Host Function Generation

### 7.1 Problem

Currently, Lua host functions are hand-maintained in `internal/plugin/hostfunc/`.
Adding a new service method means manually writing a binding. This does not
scale and creates maintenance burden.

### 7.2 Solution

A code generator reads proto service definitions and produces Go host
function binders. These live in the server binary (not the plugin), generated
into `internal/plugin/hostfunc/gen/`.

For each RPC in a required service:

```protobuf
rpc GetLocation(GetLocationRequest) returns (GetLocationResponse);
```

Generates a Lua host function `world.get_location(params)` that:

1. Marshals Lua table to protobuf request
2. Calls the service via the service registry
3. Unmarshals protobuf response to Lua table
4. Returns to the Lua script

### 7.3 Injection

At plugin load time, the host checks the plugin's `requires` list. For each
required service, the host registers the corresponding generated Lua
functions into the VM's global namespace, namespaced by service
(`world.*`, `scenes.*`, `sessions.*`).

A plugin that does not declare a requirement does not get the functions —
they are not present in the Lua VM. Calling an undeclared service produces
a Lua runtime error.

### 7.4 Generation Trigger

`task proto` (existing build step) generates Go gRPC code from proto
definitions. Lua binding generation is added to the same step.

### 7.5 Out-of-Tree Plugin Author Experience

Out-of-tree Lua plugin authors do not run `task proto` or see proto files.
They declare `requires` in their manifest and use Lua functions by name.
A generated API reference documents available functions per service.

```lua
-- Plugin declares requires: [holomush.world.v1.WorldService]
-- world.* functions are available at runtime
local loc = world.get_location(cmd.character_id, cmd.location_id)
```

### 7.6 Base Proxy Functions

Event emission, logging, and KV functions remain hand-written since they
are not proto services. They are always available regardless of `requires`
declarations.

## 8. gRPC Service Exposure

### 8.1 Server Proxy Model

When a binary plugin provides a proto service, the server registers a
thin gRPC proxy on its listener that forwards calls to the plugin's
go-plugin gRPC transport.

```text
External client -> Server gRPC port -> Proxy -> go-plugin gRPC -> Plugin
```

### 8.2 Gateway Integration

The web gateway (ConnectRPC) proxies to the server's gRPC port. Since
plugin-provided services are registered on the same port as core services,
the gateway picks them up via the same ConnectRPC mechanism. No per-plugin
gateway changes.

```text
Web client -> Gateway (ConnectRPC) -> Server gRPC -> Proxy -> Plugin
```

### 8.3 Lifecycle

When a plugin is loaded, its provided services are registered on the gRPC
listener. When unloaded, the proxy returns `Unavailable` for those services.
Dynamic load/unload is possible because the proxy indirects through the
service registry.

## 9. Plugin Administration

Server admin commands for plugin lifecycle and data management. These are
compiled-in commands (like `shutdown` and `resetpassword`) because they
operate on the plugin system itself.

| Command                    | Description                                                                        |
| -------------------------- | ---------------------------------------------------------------------------------- |
| `plugin list`              | List discovered/loaded plugins with status, health, provided services              |
| `plugin info <name>`       | Plugin details — version, type, services, dependencies, storage tier               |
| `plugin reload <name>`     | Unload + reload a binary plugin (pick up new binary)                               |
| `plugin disable <name>`    | Mark as disabled, unload, deregister services                                      |
| `plugin enable <name>`     | Re-enable and load a disabled plugin                                               |
| `plugin reset-data <name>` | Drop and recreate plugin's schema, re-run migrations. Requires confirmation.       |
| `plugin purge <name>`      | Disable + drop schema + remove plugin directory. Permanent. Requires confirmation. |

## 10. Security Considerations

### 10.1 Plugin Signing (Future)

Proto contracts provide a natural verification surface for plugin signing.
A signed plugin declares its contracts, and the signature covers both the
code and the contract manifest. The server can verify that a plugin only
accesses services it declared (and was signed for).

### 10.2 Schema Isolation

Plugin PostgreSQL roles are scoped to their own schema. No cross-schema
access prevents plugins from reading or modifying core data or other
plugins' data.

### 10.3 Service Access Control

Plugins only receive service connections for services declared in `requires`.
The Lua VM does not expose functions for undeclared services. Binary plugins
receive only the gRPC client connections they declared.

## 11. Migration Path

### Phase 1: Infrastructure

- Service registry (`internal/plugin/registry`)
- Plugin storage SDK (`pkg/plugin/storage`)
- Schema isolation and role provisioning
- Manifest schema additions (`requires`, `provides`, `storage`)
- Proto service definitions for existing server services (WorldService, SessionService, EventService)
- Binary plugin host (hashicorp/go-plugin integration)
- `RegisteredService` type with metadata and health

### Phase 2: Migrate Existing Core Plugins to Lua

- Ensure Lua host function bindings cover what existing core plugins need
- Auto-generate Lua bindings from proto (or hand-write remaining gaps)
- Rewrite each core plugin (`core-communication`, `core-building`, `core-objects`, `core-aliases`, `core-help`) as Lua
- Remove `type: core` from manifest schema
- Remove `LocalPluginHost` explicit handler registration from `plugin/setup/subsystem.go`

### Phase 3: Scene Plugin as First Binary Plugin

- `core-scenes` becomes `type: binary` with `storage: postgres`
- Scene domain types, repository, service move into the plugin binary
- Plugin provides `holomush.scene.v1.SceneService`, consumes `holomush.world.v1.WorldService`
- Server auto-proxies SceneService on gRPC port
- Proves the full binary plugin path end-to-end

### Phase 4: Cleanup

- Remove `ServiceProxy` domain methods (world, sessions, aliases become proto services)
- Remove hand-maintained `hostfunc` bindings (replaced by generated bindings)
- Plugin admin commands (`plugin list/info/reload/disable/enable/reset-data/purge`)
- Dynamic reload support

## 12. Areas Needing Deeper Design

| Area                                  | Notes                                                                                                  |
| ------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| Proto code generator for Lua bindings | Protoc plugin or standalone generator? Template-based vs reflection-based?                             |
| go-plugin handshake protocol          | How connection string and service connections are passed during handshake                              |
| In-process gRPC adapter               | How to wrap server-internal Go services as `grpc.ClientConnInterface` without actual network transport |
| ABAC integration                      | How plugins declare and install ABAC policies in the new architecture                                  |
| Event subscription for binary plugins | How binary plugins subscribe to event streams (currently Lua-only via `HandleEvent`)                   |
| Plugin versioning and upgrades        | How schema migrations interact with plugin version bumps                                               |
| Dynamic reload edge cases             | In-flight requests during reload, dependent service availability                                       |
| Plugin signing implementation         | Certificate format, verification flow, trust chain                                                     |
