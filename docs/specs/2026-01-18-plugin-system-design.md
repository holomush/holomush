# Plugin System Design

**Status:** Draft
**Date:** 2026-01-18
**Epic:** holomush-1hq (Epic 2: Plugin System)
**Task:** holomush-1hq.1

## Overview

This document defines the architecture for HoloMUSH's two-tier plugin system: Lua for
simple scripts and go-plugin for complex extensions. The design establishes the
extension model that all game systems build upon.

### Goals

- Lightweight Lua scripts for commands and simple behaviors (minimal memory per state)
- Process-isolated go-plugin for complex systems (combat, economy, Discord)
- Unified capability model for both plugin types
- Event-driven communication with no blocking calls
- Observable execution with OTel tracing and metrics

### Non-Goals

- WASM plugins via Extism (Phase 1.6 spike completed; Lua chosen for v1 due to lower memory footprint and simpler embedding)
- Hot reload in v1 (planned for future)
- Plugin marketplace or distribution system

## Architecture

```text
┌─────────────────────────────────────────────────────────────────┐
│                         Go Core                                  │
├─────────────────────────────────────────────────────────────────┤
│  ┌──────────────────────┐      ┌──────────────────────────┐     │
│  │   PluginManager      │      │   CapabilityEnforcer     │     │
│  │   - Discovery        │      │   - Check before calls   │     │
│  │   - Lifecycle        │      │   - Audit logging        │     │
│  │   - Registry         │      └──────────────────────────┘     │
│  └──────────┬───────────┘                                       │
│             │                                                    │
│  ┌──────────┴───────────┐      ┌──────────────────────────┐     │
│  │   PluginHost         │      │   HostFunctions          │     │
│  │   (interface)        │◄─────│   - emit_event           │     │
│  └──────────┬───────────┘      │   - query_*              │     │
│             │                   │   - log, kv_*            │     │
│  ┌──────────┴──────────────┐   └──────────────────────────┘     │
│  │                         │                                     │
│  ▼                         ▼                                     │
│  ┌─────────────┐    ┌─────────────┐                             │
│  │ LuaHost     │    │ GoPluginHost│                             │
│  │ (gopher-lua)│    │ (go-plugin) │                             │
│  │ - In-process│    │ - Subprocess│                             │
│  │ - Sandboxed │    │ - gRPC      │                             │
│  └─────────────┘    └─────────────┘                             │
└─────────────────────────────────────────────────────────────────┘
```

### Package Structure

```text
internal/plugin/
  manager.go         # Discovery, lifecycle, registry
  host.go            # PluginHost interface
  manifest.go        # Manifest parsing and validation
  lua/
    host.go          # LuaHost implementation
    state.go         # StateFactory, state management
  goplugin/
    host.go          # GoPluginHost implementation
    proto/           # gRPC service definitions
  hostfunc/
    functions.go     # Host function implementations
  capability/
    enforcer.go      # Capability checking

api/proto/
  plugin/v1/
    plugin.proto     # Plugin service definition
    hostfunc.proto   # Host functions service definition

pkg/pluginsdk/
  sdk.go             # SDK for go-plugin authors

schemas/
  plugin.schema.json # JSON Schema for plugin.yaml
```

## Plugin Manifest

Plugins are discovered in `plugins/*/plugin.yaml`. The manifest declares metadata,
event subscriptions, and required capabilities.

### Schema

```yaml
# yaml-language-server: $schema=https://holomush.dev/schemas/plugin.schema.json
# NOTE: Schema URL is placeholder; actual path determined during implementation

# Required fields
name: echo-bot # Unique identifier, pattern: ^[a-z][a-z0-9-]*$
version: 1.0.0 # Semver
type: lua # Required enum: "lua" | "binary"

# Event subscriptions
events:
  - say
  - pose

# Capabilities requested (denied by default)
capabilities:
  - events.emit.location
  - world.read.*
  - kv.*
  - system.prompt

# Required when type: lua
lua-plugin:
  entry: main.lua # Entry point file

# Required when type: binary
binary-plugin:
  executable: echo-${os}-${arch} # Expands to echo-linux-amd64, etc.
```

### Validation Rules

| Rule                                                          | Enforcement    |
| ------------------------------------------------------------- | -------------- |
| `name` MUST match `^[a-z][a-z0-9-]*$`                         | Schema         |
| `version` MUST be valid semver                                | Schema         |
| `type` MUST be `lua` or `binary`                              | Schema         |
| When `type: lua`, `lua-plugin` MUST be present                | Schema (oneOf) |
| When `type: binary`, `binary-plugin` MUST be present          | Schema (oneOf) |
| Requested capabilities MUST be subset of granted capabilities | Runtime        |

### Directory Structure

```text
plugins/
  echo-bot/
    plugin.yaml
    main.lua
  combat-system/
    plugin.yaml
    combat-linux-amd64
    combat-darwin-arm64
```

## Capability Model

Plugins operate in a sandboxed environment. All access to host functions requires
explicit capability grants.

### Capability Hierarchy

```text
events
  events.subscribe.*        # Receive events (controlled by manifest)
  events.emit.*             # Emit to any stream
  events.emit.location      # Emit to location streams only
  events.emit.session       # Emit to session streams only

world
  world.read.*              # Read any world data
  world.read.location       # Read location data
  world.read.character      # Read character data
  world.read.object         # Read object data
  world.write.*             # Modify world (dangerous)
  world.write.character     # Modify character attributes

kv
  kv.read                   # Read plugin's KV namespace
  kv.write                  # Write plugin's KV namespace

net
  net.http                  # Make HTTP requests (go-plugin only)
  net.websocket             # WebSocket connections (go-plugin only)

system
  system.prompt             # Send prompts to users
  system.disconnect         # Disconnect sessions
```

### Enforcement

**Three-layer model:**

1. **Manifest declaration**: Plugin declares what it needs
2. **Server config**: Admin grants what's allowed
3. **Runtime check**: Every host function call verified

**Server configuration (`holomush.yaml`):**

```yaml
plugins:
  echo-bot:
    enabled: true
    capabilities:
      - events.emit.location
      - world.read.*
      - kv.*
  combat-system:
    enabled: true
    capabilities:
      - events.*
      - world.*
      - net.http
```

**Effective capabilities** = intersection of (requested ∩ granted).

### CapabilityEnforcer

```go
type CapabilityEnforcer struct {
    grants map[string][]string  // plugin -> granted capabilities
    mu     sync.RWMutex
}

func (e *CapabilityEnforcer) Check(plugin, capability string) bool {
    e.mu.RLock()
    defer e.mu.RUnlock()

    grants := e.grants[plugin]
    for _, grant := range grants {
        if matchCapability(grant, capability) {
            return true
        }
    }
    return false
}

// matchCapability handles wildcards at the final level using prefix matching.
// "world.read.*" matches "world.read.location" and "world.read.location.nested".
// For strict single-level matching, split on "." and compare segments (not implemented here).
func matchCapability(grant, requested string) bool {
    if grant == requested {
        return true
    }
    if strings.HasSuffix(grant, ".*") {
        prefix := strings.TrimSuffix(grant, "*")
        return strings.HasPrefix(requested, prefix)
    }
    return false
}
```

### Audit Logging

Every capability check (pass or fail) MUST be logged with:

- Plugin name
- Plugin version
- Capability requested
- Result (allowed/denied)
- Timestamp
- Event context (if applicable)

## PluginHost Interface

Both Lua and go-plugin implementations conform to the same interface.

```go
// PluginHost manages a specific plugin runtime type.
type PluginHost interface {
    // Load initializes a plugin from its manifest.
    Load(ctx context.Context, manifest Manifest, dir string) error

    // Unload tears down a plugin.
    Unload(ctx context.Context, name string) error

    // DeliverEvent sends an event to a plugin and returns response events.
    DeliverEvent(ctx context.Context, name string, event Event) ([]EmitEvent, error)

    // Plugins returns names of all loaded plugins.
    Plugins() []string

    // Close shuts down the host and all plugins.
    Close(ctx context.Context) error
}
```

## Lua Runtime

> **Note:** Code examples in this section are illustrative and show design intent.
> Implementation details (error handling, edge cases, library initialization patterns)
> will be refined during implementation.

### StateFactory

Lua states are created fresh per event delivery. The `StateFactory` interface allows
future optimization (pooling) without changing calling code.

```go
// StateFactory creates Lua states with host functions pre-registered.
type StateFactory interface {
    // NewState returns a fresh Lua state ready for plugin execution.
    NewState(ctx context.Context, pluginName string) (*lua.LState, error)
}

// simpleFactory creates fresh states (initial implementation).
type simpleFactory struct {
    hostFuncs *HostFunctions
    enforcer  *CapabilityEnforcer
}

func (f *simpleFactory) NewState(ctx context.Context, pluginName string) (*lua.LState, error) {
    L := lua.NewState(lua.Options{
        SkipOpenLibs: true,  // Sandbox: don't load os, io, etc.
    })

    // Load only safe libraries using gopher-lua's CallByParam for error handling
    for _, pair := range []struct {
        name string
        fn   lua.LGFunction
    }{
        {lua.BaseLibName, lua.OpenBase},
        {lua.TabLibName, lua.OpenTable},
        {lua.StringLibName, lua.OpenString},
        {lua.MathLibName, lua.OpenMath},
    } {
        if err := L.CallByParam(lua.P{
            Fn:      L.NewFunction(pair.fn),
            NRet:    0,
            Protect: true,
        }, lua.LString(pair.name)); err != nil {
            L.Close()
            return nil, fmt.Errorf("open library %s: %w", pair.name, err)
        }
    }

    // Register host functions
    f.hostFuncs.Register(L, pluginName)

    return L, nil
}
```

### LuaHost

```go
type LuaHost struct {
    factory  StateFactory
    plugins  map[string]*luaPlugin
    mu       sync.RWMutex
}

type luaPlugin struct {
    manifest Manifest
    code     []byte  // Compiled Lua bytecode (via lua.CompileString at Load time)
}
```

### Plugin Loading Flow

On `LuaHost.Load()`:

1. Read `main.lua` from disk
2. Compile to bytecode via `lua.CompileString()` (one-time cost)
3. Store bytecode in `luaPlugin.code`

### Event Delivery Flow

On `LuaHost.DeliverEvent()`:

1. Get fresh state from factory
2. Load pre-compiled bytecode via `L.DoCompiledChunk()`
3. Call `on_event(event)` function
4. Collect return value (emit events table or nil)
5. Close state

**Edge cases** (handled gracefully, logged, delivery continues):

- `on_event` function not defined → no-op
- Function returns non-table value → ignored
- Function returns empty table → treated as no events to emit

### Sandbox Configuration

| Library     | Loaded | Rationale                   |
| ----------- | ------ | --------------------------- |
| `base`      | Yes    | Core functions              |
| `table`     | Yes    | Table manipulation          |
| `string`    | Yes    | String operations           |
| `math`      | Yes    | Math functions              |
| `os`        | No     | File system, process        |
| `io`        | No     | File I/O                    |
| `debug`     | No     | Internal inspection         |
| `package`   | No     | Module loading              |
| `coroutine` | No     | Not needed, adds complexity |

## Host Functions

Host functions provide the API available to plugins. All functions (except `log`)
require capability checks.

### Supporting Interfaces

```go
// KVStore provides namespaced key-value storage for plugins.
type KVStore interface {
    Get(ctx context.Context, namespace, key string) ([]byte, error)
    Set(ctx context.Context, namespace, key string, value []byte) error
    Delete(ctx context.Context, namespace, key string) error
}

// EventEmitter sends events to the event bus.
type EventEmitter interface {
    Emit(ctx context.Context, stream, eventType string, payload []byte) error
}

// WorldReader provides read-only access to world data.
type WorldReader interface {
    GetLocation(ctx context.Context, id string) (*Location, error)
    GetCharacter(ctx context.Context, id string) (*Character, error)
    GetObject(ctx context.Context, id string) (*Object, error)
}
```

### Function Registry

```go
type HostFunctions struct {
    eventBus   EventEmitter
    worldStore WorldReader
    kvStore    KVStore
    logger     *slog.Logger
    enforcer   *CapabilityEnforcer
}

func (h *HostFunctions) Register(L *lua.LState, pluginName string) {
    mod := L.NewTable()

    // Event emission - capability checked dynamically based on stream parameter
    // e.g., "location:123" checks events.emit.location, "session:456" checks events.emit.session
    L.SetField(mod, "emit_event", h.wrapDynamic(pluginName, h.emitEvent))

    // World queries
    L.SetField(mod, "query_location", h.wrap(pluginName, "world.read.location", h.queryLocation))
    L.SetField(mod, "query_character", h.wrap(pluginName, "world.read.character", h.queryCharacter))
    L.SetField(mod, "query_object", h.wrap(pluginName, "world.read.object", h.queryObject))

    // Key-value storage
    L.SetField(mod, "kv_get", h.wrap(pluginName, "kv.read", h.kvGet))
    L.SetField(mod, "kv_set", h.wrap(pluginName, "kv.write", h.kvSet))
    L.SetField(mod, "kv_delete", h.wrap(pluginName, "kv.write", h.kvDelete))

    // Request IDs
    L.SetField(mod, "new_request_id", h.newRequestID)

    // Logging (always allowed)
    L.SetField(mod, "log", h.log)

    L.SetGlobal("holomush", mod)
}
```

### API Reference

| Function                                  | Capability               | Description                   |
| ----------------------------------------- | ------------------------ | ----------------------------- |
| `holomush.emit_event(stream, type, data)` | `events.emit.<type>`[^1] | Emit event to stream          |
| `holomush.query_location(id)`             | `world.read.location`    | Get location data             |
| `holomush.query_character(id)`            | `world.read.character`   | Get character data            |
| `holomush.query_object(id)`               | `world.read.object`      | Get object data               |
| `holomush.kv_get(key)`                    | `kv.read`                | Read from plugin KV store     |
| `holomush.kv_set(key, value)`             | `kv.write`               | Write to plugin KV store[^2]  |
| `holomush.kv_delete(key)`                 | `kv.write`               | Delete from plugin KV store   |
| `holomush.new_request_id()`               | (none)                   | Generate ULID for correlation |
| `holomush.log(level, message)`            | (none)                   | Structured logging            |

[^1]: Capability determined dynamically from stream parameter. Stream format is
    `<type>:<id>` (e.g., `location:123`). Emitting to `location:123` requires
    `events.emit.location` capability.

[^2]: Value can be string or table. Tables are automatically JSON-serialized;
    `kv_get` returns deserialized Lua tables.

### Capability Wrapper

```go
func (h *HostFunctions) wrap(plugin, cap string, fn lua.LGFunction) lua.LGFunction {
    return func(L *lua.LState) int {
        if !h.enforcer.Check(plugin, cap) {
            L.RaiseError("capability denied: %s requires %s", plugin, cap)
            return 0
        }
        return fn(L)
    }
}

// wrapDynamic determines capability from first argument (stream parameter).
// "location:123" → events.emit.location, "session:456" → events.emit.session
func (h *HostFunctions) wrapDynamic(plugin string, fn lua.LGFunction) lua.LGFunction {
    return func(L *lua.LState) int {
        stream := L.CheckString(1)
        streamType := strings.SplitN(stream, ":", 2)[0] // "location:123" → "location"
        cap := "events.emit." + streamType

        if !h.enforcer.Check(plugin, cap) {
            L.RaiseError("capability denied: %s requires %s", plugin, cap)
            return 0
        }
        return fn(L)
    }
}
```

## Plugin Interaction Patterns

Plugins communicate via events, not blocking calls. This matches the async nature
of MUSH games where users may not respond immediately.

### Event Types for Interaction

| Event Type        | Direction       | Purpose               |
| ----------------- | --------------- | --------------------- |
| `prompt`          | Plugin → User   | Request user input    |
| `prompt_response` | User → Plugin   | User's answer         |
| `prompt_timeout`  | System → Plugin | User didn't respond   |
| `plugin_request`  | Plugin → Plugin | Cross-plugin call     |
| `plugin_response` | Plugin → Plugin | Cross-plugin response |

### Example: User Prompt

```lua
function on_event(event)
    if event.type == "combat_start" then
        local request_id = holomush.new_request_id()

        -- Store pending state
        holomush.kv_set("pending:" .. request_id, {
            attacker = event.actor_id,
            target = event.payload.target
        })

        -- Emit prompt to user
        return {{
            stream = "session:" .. event.actor_id,
            type = "prompt",
            payload = {
                request_id = request_id,
                message = "Attack " .. event.payload.target .. "? [Y/N]",
                options = {"Y", "N"},
                timeout = 30,
                source_plugin = "combat"
            }
        }}
    end

    if event.type == "prompt_response" then
        local pending = holomush.kv_get("pending:" .. event.payload.request_id)
        if not pending then return end

        holomush.kv_delete("pending:" .. event.payload.request_id)

        if event.payload.response == "Y" then
            -- Execute attack...
        end
    end
end
```

**Note:** Table values passed to `kv_set` are automatically JSON-serialized; `kv_get`
returns the deserialized Lua table. String values are stored as-is.

## go-plugin Integration

Heavy plugins use HashiCorp go-plugin for process isolation with gRPC communication.

### gRPC Service Definitions

```protobuf
// api/proto/plugin/v1/plugin.proto
syntax = "proto3";
package holomush.plugin.v1;

service Plugin {
    rpc HandleEvent(HandleEventRequest) returns (HandleEventResponse);
}

service HostFunctions {
    rpc EmitEvent(EmitEventRequest) returns (EmitEventResponse);
    rpc QueryLocation(QueryLocationRequest) returns (QueryLocationResponse);
    rpc QueryCharacter(QueryCharacterRequest) returns (QueryCharacterResponse);
    rpc QueryObject(QueryObjectRequest) returns (QueryObjectResponse);
    rpc KVGet(KVGetRequest) returns (KVGetResponse);
    rpc KVSet(KVSetRequest) returns (KVSetResponse);
    rpc KVDelete(KVDeleteRequest) returns (KVDeleteResponse);
    rpc Log(LogRequest) returns (LogResponse);
    rpc NewRequestID(NewRequestIDRequest) returns (NewRequestIDResponse);
}

message Event {
    string id = 1;
    string stream = 2;
    string type = 3;
    int64 timestamp = 4;
    string actor_kind = 5;
    string actor_id = 6;
    bytes payload = 7;  // JSON-encoded payload; plugins decode as needed
}

// Request/response messages elided for brevity.
// Full definitions in api/proto/plugin/v1/ during implementation.
```

### GoPluginHost

```go
type GoPluginHost struct {
    clients  map[string]*plugin.Client
    plugins  map[string]PluginRPC
    hostFns  *HostFunctions
    enforcer *CapabilityEnforcer
    mu       sync.RWMutex
}

func (h *GoPluginHost) Load(ctx context.Context, manifest Manifest, dir string) error {
    binary := expandBinaryPath(manifest.BinaryPlugin.Executable, dir)

    // go-plugin uses a broker for bidirectional communication.
    // The plugin calls back to host functions via the broker.
    client := plugin.NewClient(&plugin.ClientConfig{
        HandshakeConfig:  handshake,
        Plugins: map[string]plugin.Plugin{
            "plugin": &pluginGRPCPlugin{
                hostFns:  h.hostFns,
                enforcer: h.enforcer,
                plugin:   manifest.Name,
            },
        },
        Cmd:              exec.Command(binary),
        AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
    })

    rpcClient, err := client.Client()
    if err != nil {
        return fmt.Errorf("connect to plugin: %w", err)
    }

    raw, err := rpcClient.Dispense("plugin")
    if err != nil {
        return fmt.Errorf("dispense plugin: %w", err)
    }

    h.mu.Lock()
    h.clients[manifest.Name] = client
    h.plugins[manifest.Name] = raw.(PluginRPC)
    h.mu.Unlock()

    return nil
}
```

### Plugin SDK

```go
// pkg/pluginsdk/sdk.go
package pluginsdk

type Plugin interface {
    HandleEvent(ctx context.Context, event Event) ([]EmitEvent, error)
}

type Host interface {
    EmitEvent(ctx context.Context, stream, eventType string, payload any) error
    QueryLocation(ctx context.Context, id string) (*Location, error)
    QueryCharacter(ctx context.Context, id string) (*Character, error)
    QueryObject(ctx context.Context, id string) (*Object, error)
    KVGet(ctx context.Context, key string) ([]byte, error)
    KVSet(ctx context.Context, key string, value []byte) error
    KVDelete(ctx context.Context, key string) error
    NewRequestID(ctx context.Context) (string, error)
    Log(ctx context.Context, level, message string) error
}

func Serve(p Plugin) {
    plugin.Serve(&plugin.ServeConfig{
        HandshakeConfig: handshake,
        Plugins: map[string]plugin.Plugin{
            "plugin": &pluginGRPC{impl: p},
        },
        GRPCServer: plugin.DefaultGRPCServer,
    })
}
```

## Error Handling

Plugins operate with isolated failure semantics. One buggy plugin MUST NOT affect
others or crash the server.

### Error Handling Rules

| Scenario             | Behavior                                 |
| -------------------- | ---------------------------------------- |
| Lua panic            | Caught, logged, event delivery continues |
| Lua error return     | Logged, event delivery continues         |
| Execution timeout    | Cancelled after 5s, logged, continues    |
| go-plugin crash      | Subprocess dies, logged, plugin disabled |
| Capability denied    | Lua error raised, logged with audit      |
| Invalid event return | Logged, ignored, continues               |

### Timeout Configuration

- Default: 5 seconds per event delivery
- Configurable per-plugin in server config
- Context cancellation propagated to host functions

When timeout fires, context cancellation propagates to all in-flight host function
calls. Long-running host calls (e.g., `query_location` with slow database) SHOULD
check `ctx.Done()` and return early with an appropriate error.

## Observability

### OTel Tracing

```go
func (h *LuaHost) DeliverEvent(ctx context.Context, name string, event Event) ([]EmitEvent, error) {
    ctx, span := tracer.Start(ctx, "plugin.deliver_event",
        trace.WithAttributes(
            attribute.String("plugin.name", name),
            attribute.String("plugin.version", h.plugins[name].manifest.Version),
            attribute.String("event.id", event.ID),
            attribute.String("event.type", event.Type),
        ))
    defer span.End()

    // ... execution ...

    if err != nil {
        span.RecordError(err)
        span.SetStatus(codes.Error, err.Error())
    }
    return result, err
}
```

### Metrics

| Metric                              | Type      | Labels                              |
| ----------------------------------- | --------- | ----------------------------------- |
| `plugin_events_delivered_total`     | Counter   | plugin, version, event_type, status |
| `plugin_events_emitted_total`       | Counter   | plugin, version, event_type         |
| `plugin_execution_duration_seconds` | Histogram | plugin, version                     |
| `plugin_capability_checks_total`    | Counter   | plugin, version, capability, result |
| `plugin_errors_total`               | Counter   | plugin, version, error_type         |

## Testing Strategy

| Test Type         | Approach                                            |
| ----------------- | --------------------------------------------------- |
| Unit tests        | Mock StateFactory, test host functions in isolation |
| Integration tests | Load real Lua plugins, verify event flow            |
| Capability tests  | Verify denied capabilities raise errors             |
| go-plugin tests   | Test subprocess lifecycle, gRPC communication       |
| Schema tests      | Validate plugin.yaml against JSON Schema            |

### Test Fixtures

```text
internal/plugin/testdata/
  valid-lua-plugin/
    plugin.yaml
    main.lua
  valid-binary-plugin/
    plugin.yaml
    test-plugin-linux-amd64
  missing-manifest/
    main.lua
  invalid-capability/
    plugin.yaml
    main.lua
  invalid-schema/
    plugin.yaml
```

## Migration from WASM

The `internal/wasm/` package contains the Phase 1.6 Extism spike. Once the new plugin
system is complete, this package will be **deleted entirely**.

Before deletion, relevant patterns (OTel tracing from `ExtismHost`) should be referenced
during implementation of the new system.

New code lives in `internal/plugin/`. Do not add dependencies on `internal/wasm/`.

## Acceptance Criteria

- [ ] Design document covers phases 2.1-2.6 (2.7 Echo bot is implementation)
- [ ] Host function API fully specified
- [ ] Capability model documented
- [ ] go-plugin integration approach defined
- [ ] Security model for sandboxing documented
- [ ] JSON Schema for plugin.yaml specified (actual schema created during implementation)
- [ ] Plugin interaction patterns documented
- [ ] Design reviewed and approved

## References

- [HoloMUSH Roadmap Design](../plans/2026-01-18-holomush-roadmap-design.md)
- [gopher-lua](https://github.com/yuin/gopher-lua)
- [HashiCorp go-plugin](https://github.com/hashicorp/go-plugin)
- [Phase 1.6 Extism Spike](../specs/2026-01-17-phase1-tracer-bullet.md)
