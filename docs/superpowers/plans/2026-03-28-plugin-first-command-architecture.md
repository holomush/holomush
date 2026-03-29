<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Plugin-First Command Architecture Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Unify all command execution under a plugin architecture where Lua and Go plugins have identical contracts, migrating ~31 commands from compiled-in handlers to core plugins.

**Architecture:** Add `DeliverCommand` to the Host interface. Create `LocalPluginHost` for in-process Go plugins. Create `ServiceProxy` interface for plugin-to-host service calls. Route command dispatch through PluginManager. Migrate handlers from `internal/command/handlers/` to `plugins/core-*` packages. Instrument with OpenTelemetry.

**Tech Stack:** Go (plugin hosts, service proxy, dispatch), Protobuf (plugin service RPC), OpenTelemetry (tracing + metrics), gopher-lua (Lua host changes), hashicorp/go-plugin (binary host changes)

**Spec:** `docs/specs/2026-03-28-plugin-first-command-architecture-design.md`

---

## Phase Overview

| Phase | Description | Produces |
| ----- | ----------- | -------- |
| 1     | Plugin contract + ServiceProxy + LocalPluginHost | Testable foundation — core plugins can register and handle commands in-process |
| 2     | Command dispatch unification | All commands route through PluginManager — same behavior, new path |
| 3     | Core command migration | ~31 commands move from `internal/command/handlers/` to `plugins/core-*` |
| 4     | Lua + Binary host parity | LuaHost gets DeliverCommand, BinaryHost gets HandleCommand RPC + PluginHostService |
| 5     | Observability | OTel middleware on Host and ServiceProxy |

Phases 1-2 are the foundation (must be sequential). Phase 3 is the bulk work (parallelizable per plugin). Phase 4 is independent of Phase 3. Phase 5 is cross-cutting (after Phase 2).

---

## Chunk 1: Plugin Contract Foundation

### Task 1: Add DeliverCommand to Host interface and SDK types

**Files:**

- Modify: `internal/plugin/host.go`
- Modify: `internal/plugin/manifest.go`
- Modify: `internal/plugin/goplugin/host.go` (stub DeliverCommand for compilation)
- Modify: `pkg/plugin/sdk.go`
- Create: `pkg/plugin/command.go`

**Context:** The `Host` interface currently has `DeliverEvent` only. Add `DeliverCommand` with a new `CommandRequest` / `CommandResponse` type in the plugin SDK. Also add `type: core` and `load_priority` to the manifest schema.

- [ ] **Step 1: Create `pkg/plugin/command.go` with SDK types**

```go
// CommandRequest carries command context to plugin handlers.
type CommandRequest struct {
    Command       string
    Args          string
    CharacterID   string
    CharacterName string
    LocationID    string
    SessionID     string
    InvokedAs     string
    LastWhispered string
}

// CommandResponse carries results back from plugin handlers.
type CommandResponse struct {
    Events         []EmitEvent
    Output         string
    BootedSessions []string
    EndSession     bool
}
```

- [ ] **Step 2: Add DeliverCommand to Host interface**

```go
// In host.go, add to Host interface:
DeliverCommand(ctx context.Context, name string, cmd pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error)
```

- [ ] **Step 3: Add `type: core` and `load_priority` to manifest schema**

In `internal/plugin/manifest.go`:

- Add `TypeCore Type = "core"` constant
- Add `LoadPriority int` field to `Manifest` struct (yaml tag: `load_priority`, default 0)
- Update `Validate()` to accept `type: core` (requires neither `lua-plugin` nor `binary-plugin`)
- Add validation: external plugins (lua/binary) with `load_priority < -999` MUST be rejected
- Core plugins use `load_priority: -1000` (reserved range)

- [ ] **Step 4: Fix compilation — add stub DeliverCommand to existing hosts (cont.)**

Add `DeliverCommand` returning `ErrNotImplemented` to `LuaHost` (`internal/plugin/lua/host.go`) and `BinaryHost` (`internal/plugin/goplugin/host.go`) so the code compiles. Both have compile-time interface checks (`var _ plugins.Host = ...`) that will fail without this. These get real implementations in Phase 4.

- [ ] **Step 5: Run task build — expect PASS**

- [ ] **Step 6: Commit**

`feat(plugin): add DeliverCommand to Host interface and SDK command types`

---

### Task 2: ServiceProxy interface

**Files:**

- Create: `internal/plugin/service_proxy.go`
- Create: `internal/plugin/service_proxy_test.go`

**Context:** The ServiceProxy is the interface plugins use to access game services. It mirrors every `holomush.*` Lua host function. The implementation wraps real services (WorldService, SessionStore, EventStore, etc.).

- [ ] **Step 1: Define ServiceProxy interface**

All operations from the spec's parity table. Group by category.

**subjectID convention:** Every world/property operation takes a `subjectID` parameter for ABAC authorization. For command handlers, this is `CommandRequest.CharacterID` — the command runs as the player, not as the plugin. The proxy implementation passes this through to the underlying `WorldService` methods. The plugin itself does not set subjectID; the LocalPluginHost (or gRPC adapter) supplies it from the command context.

```go
type ServiceProxy interface {
    // World read
    QueryLocation(ctx context.Context, subjectID, id string) (*LocationResult, error)
    QueryCharacter(ctx context.Context, subjectID, id string) (*CharacterResult, error)
    QueryLocationCharacters(ctx context.Context, subjectID, locationID string) ([]CharacterResult, error)
    QueryObject(ctx context.Context, subjectID, id string) (*ObjectResult, error)
    FindLocation(ctx context.Context, subjectID, name string) (*LocationResult, error)

    // World write
    CreateLocation(ctx context.Context, subjectID string, name, description, locationType string) (*LocationResult, error)
    CreateExit(ctx context.Context, subjectID string, fromID, toID, name string, opts CreateExitOpts) error
    CreateObject(ctx context.Context, subjectID string, name, description string) (*ObjectResult, error)

    // Properties
    SetProperty(ctx context.Context, subjectID, parentType, parentID, key, value string) error
    GetProperty(ctx context.Context, subjectID, parentType, parentID, key string) (string, error)
    FindPropertyByPrefix(ctx context.Context, prefix string) ([]PropertyInfo, error)

    // Plugin KV
    KVGet(ctx context.Context, pluginName, key string) (string, bool, error)
    KVSet(ctx context.Context, pluginName, key, value string) error
    KVDelete(ctx context.Context, pluginName, key string) error

    // Session
    FindSessionByName(ctx context.Context, name string) (*SessionResult, error)
    SetLastWhispered(ctx context.Context, sessionID, name string) error
    DisconnectSession(ctx context.Context, sessionID, reason string) error
    ListActiveSessions(ctx context.Context) ([]SessionResult, error)
    BroadcastSystemMessage(ctx context.Context, message string) error

    // Aliases
    SetPlayerAlias(ctx context.Context, characterID, alias, command string) error
    DeletePlayerAlias(ctx context.Context, characterID, alias string) error
    ListPlayerAliases(ctx context.Context, characterID string) ([]AliasEntry, error)
    SetSystemAlias(ctx context.Context, alias, command, createdBy string) error
    DeleteSystemAlias(ctx context.Context, alias string) error
    ListSystemAliases(ctx context.Context) ([]AliasEntry, error)
    CheckAliasShadow(ctx context.Context, alias string) (bool, string, error)

    // Commands
    ListCommands(ctx context.Context, characterID string) ([]CommandInfo, error)
    GetCommandHelp(ctx context.Context, name, characterID string) (*CommandHelpInfo, error)

    // Events
    EmitEvent(ctx context.Context, stream, eventType string, payload []byte) error

    // Config
    GetStartingLocationID(ctx context.Context) (string, error)

    // Utility
    Log(ctx context.Context, level, message string)
}
```

- [ ] **Step 2: Define result types (LocationResult, CharacterResult, etc.)**

These are simple structs carrying the data plugins need — not the full internal types. This is the SDK boundary.

- [ ] **Step 3: Write tests for the interface contract**

Compile-time interface check against a mock. Verify all parity table operations exist.

- [ ] **Step 4: Commit**

`feat(plugin): define ServiceProxy interface for plugin-to-host service calls`

---

### Task 3: ServiceProxy implementation

**Files:**

- Create: `internal/plugin/service_proxy_impl.go`
- Create: `internal/plugin/service_proxy_impl_test.go`

**Context:** The implementation wraps real services. For LocalPluginHost, this is called directly (no gRPC). For BinaryHost, a gRPC adapter will wrap it in Phase 4.

- [ ] **Step 1: Implement ServiceProxyImpl**

```go
type ServiceProxyImpl struct {
    world           WorldService       // same interface from command/types.go
    sessions        session.Store
    events          core.EventStore
    aliasWriter     AliasWriter
    aliasCache      *alias.Cache
    commandRegistry *Registry
    propertyReg     *property.Registry
    startingLocID   string
    logger          *slog.Logger
}
```

Each method delegates to the appropriate service, converting between SDK types and internal types.

- [ ] **Step 2: Write tests for key operations**

Test QueryLocation, EmitEvent, SetPlayerAlias with mocked services. Table-driven.

- [ ] **Step 3: Run task test**

- [ ] **Step 4: Commit**

`feat(plugin): implement ServiceProxy wrapping real services`

---

### Task 4: LocalPluginHost

**Files:**

- Create: `internal/plugin/local_host.go`
- Create: `internal/plugin/local_host_test.go`

**Context:** LocalPluginHost manages in-process Go plugins. It implements the `Host` interface. Core plugins register Go `CommandHandler` and `EventHandler` implementations. DeliverCommand calls the handler directly with the ServiceProxy.

- [ ] **Step 1: Define CommandHandler and EventHandler interfaces for local plugins**

```go
// LocalCommandHandler is implemented by in-process Go plugin command handlers.
type LocalCommandHandler interface {
    HandleCommand(ctx context.Context, cmd pluginsdk.CommandRequest, proxy ServiceProxy) (*pluginsdk.CommandResponse, error)
}

// LocalEventHandler is implemented by in-process Go plugin event handlers.
type LocalEventHandler interface {
    HandleEvent(ctx context.Context, event pluginsdk.Event, proxy ServiceProxy) ([]pluginsdk.EmitEvent, error)
}
```

Note: these receive `ServiceProxy` as a parameter, not via the SDK. This is the in-process optimization — no gRPC, no serialization.

- [ ] **Step 2: Implement LocalPluginHost**

```go
type LocalPluginHost struct {
    mu      sync.RWMutex
    plugins map[string]*localPlugin
    proxy   ServiceProxy
}

type localPlugin struct {
    manifest       *Manifest
    commandHandler LocalCommandHandler  // may be nil
    eventHandler   LocalEventHandler    // may be nil
}
```

Methods: `Load`, `Unload`, `DeliverCommand`, `DeliverEvent`, `Plugins`, `Close`.

`Load` takes the manifest and looks up the handler from a pre-registered map (core plugins register their handlers at startup before Load is called). During `Load`, the host MUST parse the manifest's `commands` slice and register each command in the command `Registry` with the plugin name as routing target. This is how plugin-backed commands enter the registry.

- [ ] **Step 3: Write tests**

- Register a plugin with a CommandHandler, deliver a command, verify response
- Register a plugin with an EventHandler, deliver an event, verify emits
- Deliver command to plugin that only has EventHandler → error
- Concurrent access safety

- [ ] **Step 4: Run task test**

- [ ] **Step 5: Commit**

`feat(plugin): implement LocalPluginHost for in-process Go plugins`

---

## Chunk 2: Command Dispatch Unification

### Task 5: Refactor CommandEntry to store plugin routing

**Files:**

- Modify: `internal/command/types.go`
- Modify: `internal/command/registry.go`
- Modify: `internal/command/dispatcher.go`
- Update affected tests

**Context:** CommandEntry currently stores a `CommandHandler` function pointer. Change it to store plugin routing info (plugin name, source type). The dispatcher calls PluginManager.DeliverCommand instead of calling the handler directly.

- [ ] **Step 1: Add PluginName field to CommandEntry, make handler optional**

The handler field stays for compiled-in commands (quit, shutdown). Plugin-backed commands have a nil handler and a non-empty PluginName.

**Two validation sites must be updated:**

1. `NewCommandEntry` in `types.go` (line ~175): currently rejects nil handler (`CodeNilHandler`). Change to: require at least one of handler or PluginName. If both are nil/empty → error. If both are set → error (ambiguous).
2. `Registry.Register` in `registry.go` (line ~39): currently checks `entry.Handler() == nil`. Remove this check — the constructor enforces the invariant.

```go
type CommandEntry struct {
    Name         string
    handler      CommandHandler // nil for plugin-backed commands
    PluginName   string         // non-empty for plugin-backed commands
    capabilities []string
    Help         string
    Usage        string
    HelpText     string
    Source       string         // "core", plugin name
}
```

- [ ] **Step 2: Add PluginManager interface to dispatcher**

```go
type PluginCommandDeliverer interface {
    DeliverCommand(ctx context.Context, pluginName string, cmd pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error)
}
```

The dispatcher receives this at construction time.

- [ ] **Step 3: Update Dispatcher.Dispatch to route through PluginManager**

After capability check:

- If `entry.handler != nil` → call directly (compiled-in: quit, shutdown)
- If `entry.PluginName != ""` → build `CommandRequest`, call `pluginManager.DeliverCommand()`
- Process `CommandResponse`: emit events, write output, handle booted sessions, handle end-session

- [ ] **Step 4: Add post-dispatch infrastructure**

After successful DeliverCommand:

- `SessionStore.UpdateActivity(sessionID)`
- For each `response.BootedSessions` → emit leave events, record for gRPC teardown
- If `response.EndSession` → return session-ended error
- If `response.Output != ""` → emit `command_response` event on character stream

- [ ] **Step 5: Update tests**

Existing dispatcher tests need to use the new routing. Create a mock PluginCommandDeliverer.

- [ ] **Step 6: Run task test — full suite**

- [ ] **Step 7: Commit**

`refactor(command): route plugin-backed commands through PluginManager`

---

### Task 6: Refactor PluginManager for multi-host routing

**Files:**

- Modify: `internal/plugin/manager.go`
- Create: `internal/plugin/manager_routing_test.go`

**Context:** The current Manager has a single `luaHost Host` field. It needs a host registry that routes by plugin type, and a `DeliverCommand` method so the dispatcher can call it.

- [ ] **Step 1: Add host registry to Manager**

Replace `luaHost Host` with `hosts map[Type]Host`. Add `RegisterHost(hostType Type, host Host)`. Update `loadPlugin` to look up the host by manifest type.

- [ ] **Step 2: Add DeliverCommand to Manager**

Given a plugin name, look up which host owns it (track plugin→host mapping during Load), then call `host.DeliverCommand()`. Return error for unknown plugins.

- [ ] **Step 3: Update DeliverEvent routing**

Current event delivery goes through a separate path via `Subscriber`. The Manager's `DeliverEvent` should also route through the host registry. Ensure consistency.

- [ ] **Step 4: Write routing tests**

- Route command to correct host (core vs lua vs binary)
- Unknown plugin → error
- Plugin loaded on wrong host → error
- Concurrent routing safety

- [ ] **Step 5: Run task test**

- [ ] **Step 6: Commit**

`refactor(plugin): multi-host routing in PluginManager with DeliverCommand`

---

### Task 7: Wire LocalPluginHost into server startup

**Files:**

- Modify: `cmd/holomush/core.go`

**Context:** Create the ServiceProxyImpl and LocalPluginHost in core.go, register them in the PluginManager, and configure the dispatcher.

- [ ] **Step 1: Create ServiceProxyImpl in core.go**

After service initialization (~line 400), create the proxy with all services. `subjectID` for proxy calls comes from `CommandRequest.CharacterID` — the proxy does NOT use the plugin name as subject.

- [ ] **Step 2: Create LocalPluginHost with the proxy**

- [ ] **Step 3: Register LocalPluginHost in PluginManager**

`manager.RegisterHost(plugins.TypeCore, localHost)`

- [ ] **Step 4: Pass PluginManager to Dispatcher as PluginCommandDeliverer**

- [ ] **Step 5: Add integration test: full dispatch round-trip**

Input string → dispatcher → PluginManager → LocalPluginHost → test command handler → events produced. Verify end-to-end.

- [ ] **Step 6: Run task test && task build**

- [ ] **Step 7: Commit**

`feat(core): wire LocalPluginHost and ServiceProxy into server startup`

---

### Task 8: Disabled commands configuration

**Files:**

- Modify: `cmd/holomush/core.go` (read config)
- Modify: `internal/command/registry.go` (support removal)

**Context:** Server config gains `plugins.disabled_commands` list. After all plugins register commands, disabled entries are removed from the registry.

- [ ] **Step 1: Add config parsing for disabled\_commands**

- [ ] **Step 2: Implement Registry.Unregister(name)**

- [ ] **Step 3: After plugin loading, remove disabled commands**

- [ ] **Step 4: Write test: disabled command returns "unknown command"**

- [ ] **Step 5: Commit**

`feat(command): support disabling built-in commands via config`

---

## Chunk 3: Core Command Migration

> **Migration pattern:** For each core plugin, create a `plugin.yaml` manifest, implement handlers as `LocalCommandHandler`, write tests against the ServiceProxy mock, and register in the LocalPluginHost.

### Task 9: core-communication plugin (say, pose, page, p, whisper, w, ooc, pemit, emit, wall)

**Files:**

- Create: `plugins/core-communication/plugin.yaml`
- Create: `plugins/core-communication/say.go`
- Create: `plugins/core-communication/pose.go`
- Create: `plugins/core-communication/page.go`
- Create: `plugins/core-communication/whisper.go`
- Create: `plugins/core-communication/ooc.go`
- Create: `plugins/core-communication/pemit.go`
- Create: `plugins/core-communication/emit.go`
- Create: `plugins/core-communication/wall.go`
- Create: `plugins/core-communication/plugin.go` (registers all handlers)
- Create: `plugins/core-communication/plugin_test.go`
- Delete: `internal/command/handlers/say.go` + test
- Delete: `internal/command/handlers/pose.go` + test
- Delete: `internal/command/handlers/page.go` + test
- Delete: `internal/command/handlers/ooc.go` + test
- Delete: `internal/command/handlers/pemit.go` + test
- Delete: `internal/command/handlers/wall.go` + test
- Delete: `plugins/communication/` (old Lua plugin — dead code for say/pose, whisper/emit absorbed)

**Migration notes:**

- `say`, `pose`, `ooc`, `pemit` are pure event emitters — mechanical migration
- `page` emits events + calls `SetLastWhispered` — straightforward
- `whisper` is Lua-only today — port to Go using Lua source as reference
- `emit` is Lua-only today — port to Go
- `wall` uses `ListActiveSessions` + `BroadcastSystemMessage` — uses new proxy operations
- `w` and `p` are aliases sharing handlers with `whisper` and `page`

- [ ] **Step 1: Create plugin.yaml manifest**
- [ ] **Step 2: Migrate say, pose (mechanical — emit events via proxy)**
- [ ] **Step 3: Migrate page, p (emit events + SetLastWhispered)**
- [ ] **Step 4: Port whisper, w from Lua to Go**
- [ ] **Step 5: Migrate ooc, pemit (mechanical)**
- [ ] **Step 6: Port emit from Lua to Go**
- [ ] **Step 7: Migrate wall (ListActiveSessions + BroadcastSystemMessage)**
- [ ] **Step 8: Create plugin.go registering all handlers**
- [ ] **Step 9: Write tests for each handler against ServiceProxy mock**
- [ ] **Step 10: Delete old handlers and Lua communication plugin**
- [ ] **Step 11: Register core-communication in core.go**
- [ ] **Step 12: Run task test — full suite**
- [ ] **Step 13: Commit**

`feat(plugin): migrate communication commands to core-communication plugin`

---

### Task 10: core-navigation plugin (look, move, home, where, teleport)

**Files:**

- Create: `plugins/core-navigation/plugin.yaml`
- Create: `plugins/core-navigation/look.go`
- Create: `plugins/core-navigation/move.go`
- Create: `plugins/core-navigation/home.go`
- Create: `plugins/core-navigation/where.go`
- Create: `plugins/core-navigation/teleport.go`
- Create: `plugins/core-navigation/plugin.go`
- Create: `plugins/core-navigation/plugin_test.go`
- Delete: corresponding `internal/command/handlers/` files

**Migration notes:**

- `look` produces formatted text output → uses `CommandResponse.Output`
- `move` emits move/arrive/leave events — mechanical
- `home` uses `GetStartingLocationID` fallback — uses new proxy operation
- `where` produces formatted text output → uses `CommandResponse.Output`
- `teleport` emits move events — mechanical

- [ ] **Step 1-8: Same pattern as Task 9**
- [ ] **Step 9: Commit**

`feat(plugin): migrate navigation commands to core-navigation plugin`

---

### Task 11: core-objects plugin (describe, desc, examine, create, set)

**Files:**

- Create: `plugins/core-objects/plugin.yaml`
- Create: `plugins/core-objects/*.go`
- Delete: corresponding `internal/command/handlers/` files

**Migration notes:**

- `examine` produces formatted text output → `CommandResponse.Output`
- `describe`, `desc` are mechanical (UpdateCharacterDescription via proxy)
- `create` uses CreateObject via proxy
- `set` uses `FindPropertyByPrefix` + `SetProperty` via proxy

- [ ] **Step 1-8: Same pattern as Task 9**
- [ ] **Step 9: Commit**

`feat(plugin): migrate object commands to core-objects plugin`

---

### Task 12: core-building plugin (dig, link)

**Files:**

- Create: `plugins/core-building/plugin.yaml`
- Create: `plugins/core-building/*.go`
- Delete: `plugins/building/` (old Lua plugin)

**Migration notes:**

- Port `dig` and `link` from Lua source to Go
- Both use CreateLocation, CreateExit, FindLocation via proxy

- [ ] **Step 1-8: Same pattern as Task 9**
- [ ] **Step 9: Commit**

`feat(plugin): migrate building commands to core-building plugin`

---

### Task 13: core-admin plugin (boot, who)

**Files:**

- Create: `plugins/core-admin/plugin.yaml`
- Create: `plugins/core-admin/*.go`
- Delete: corresponding `internal/command/handlers/` files

**Migration notes:**

- `boot` uses `DisconnectSession` + `CommandResponse.BootedSessions` + `CommandResponse.EndSession` for self-boot
- `who` uses `ListActiveSessions` + `CommandResponse.Output`
- Both are non-mechanical migrations requiring careful adaptation

- [ ] **Step 1-8: Same pattern as Task 9**
- [ ] **Step 9: Commit**

`feat(plugin): migrate admin commands to core-admin plugin`

---

### Task 14: core-help plugin (help)

**Files:**

- Create: `plugins/core-help/plugin.yaml`
- Create: `plugins/core-help/*.go`
- Delete: `plugins/help/` (old Lua plugin)

**Migration notes:**

- Port from Lua source to Go
- Uses `ListCommands` + `GetCommandHelp` via proxy
- Produces formatted text output → `CommandResponse.Output`

- [ ] **Step 1-8: Same pattern as Task 9**
- [ ] **Step 9: Commit**

`feat(plugin): migrate help command to core-help plugin`

---

### Task 15: core-aliases plugin (alias, unalias, aliases, sysalias, sysunsalias, sysaliases)

**Files:**

- Create: `plugins/core-aliases/plugin.yaml`
- Create: `plugins/core-aliases/*.go`
- Delete: `internal/command/handlers/alias.go` + tests

**Migration notes:**

- Uses `SetPlayerAlias`, `DeletePlayerAlias`, `ListPlayerAliases`, `SetSystemAlias`, `DeleteSystemAlias`, `ListSystemAliases`, `CheckAliasShadow` via proxy
- `alias_shadow_checks.go` logic moves into the plugin
- Non-mechanical migration — alias cache interaction is complex

- [ ] **Step 1-8: Same pattern as Task 9**
- [ ] **Step 9: Commit**

`feat(plugin): migrate alias commands to core-aliases plugin`

---

### Task 16: Clean up — remove internal/command/handlers/ and old register.go

**Files:**

- Delete: `internal/command/handlers/register.go` (RegisterAll function)
- Delete: `internal/command/handlers/testutil/` (test utilities for old Services pattern)
- Modify: `internal/command/handlers/quit.go` (stays, but verify it still works)
- Modify: `internal/command/handlers/shutdown.go` (stays)
- Modify: `cmd/holomush/core.go` (remove old RegisterAll call)

**Context:** After all core plugins are migrated, the old registration path is dead code. Only quit and shutdown remain as compiled-in handlers.

- [ ] **Step 1: Remove RegisterAll and all migrated handler files**
- [ ] **Step 2: Verify quit and shutdown still compile and work**
- [ ] **Step 3: Run task test && task lint**
- [ ] **Step 4: Commit**

`refactor(command): remove migrated handlers, keep quit/shutdown compiled-in`

---

## Chunk 4: Lua + Binary Host Parity

### Task 17: LuaHost DeliverCommand

**Files:**

- Modify: `internal/plugin/lua/host.go`
- Modify: `internal/plugin/hostfunc/functions.go` (rename query\_room → query\_location)
- Update Lua plugin tests

**Context:** Replace the stub DeliverCommand with a real implementation that calls `on_command(ctx)` in the Lua VM. Also rename host functions for terminology parity.

- [ ] **Step 1: Implement DeliverCommand in LuaHost**

Build Lua context table from CommandRequest, call `on_command(ctx)`, process return value into CommandResponse.

- [ ] **Step 2: Rename host functions**

`holomush.query_room` → `holomush.query_location`, `holomush.query_room_characters` → `holomush.query_location_characters`

- [ ] **Step 3: Update echo-bot and any remaining Lua plugins**
- [ ] **Step 4: Run task test**
- [ ] **Step 5: Commit**

`feat(plugin): implement DeliverCommand for LuaHost, rename host functions`

---

### Task 18: BinaryHost DeliverCommand + PluginHostService

**Files:**

- Modify: `internal/plugin/goplugin/host.go`
- Modify: `api/proto/holomush/plugin/v1/plugin.proto`
- Create: `internal/plugin/goplugin/host_service.go` (PluginHostService gRPC server)
- Modify: `pkg/plugin/sdk.go` (add CommandHandler interface for binary plugins)

**Context:** Add HandleCommand RPC to the plugin proto service. Implement PluginHostService as a gRPC server the host starts per-plugin, allowing binary plugins to call back for world queries and event emission. This is significant new infrastructure.

- [ ] **Step 1: Add HandleCommand RPC and PluginHostService to plugin.proto**
- [ ] **Step 2: Regenerate Go proto code**
- [ ] **Step 3: Implement DeliverCommand in BinaryHost**
- [ ] **Step 4: Implement PluginHostService (wraps ServiceProxy over gRPC)**
- [ ] **Step 5: Start PluginHostService per-plugin during Load**
- [ ] **Step 6: Write tests with mock plugin client**
- [ ] **Step 7: Run task test**
- [ ] **Step 8: Commit**

`feat(plugin): implement DeliverCommand and PluginHostService for BinaryHost`

---

### Task 19: Parity tests

**Files:**

- Create: `internal/plugin/parity_test.go`

**Context:** Verify every ServiceProxy operation exists across all three host types. This is the parity enforcement test.

- [ ] **Step 1: Write reflection-based test**

Use reflection to iterate ServiceProxy interface methods. For each method, verify that the LuaHost has a corresponding host function and the BinaryHost PluginHostService has a corresponding RPC. This catches drift.

- [ ] **Step 2: Run task test**
- [ ] **Step 3: Commit**

`test(plugin): add parity enforcement test across all host types`

---

## Chunk 5: Observability

### Task 20: OTel middleware for Host and ServiceProxy

**Files:**

- Create: `internal/plugin/otel_middleware.go`
- Create: `internal/plugin/otel_middleware_test.go`
- Modify: `cmd/holomush/core.go` (wrap host and proxy with middleware)

**Context:** Two middleware wrappers: one wraps `Host` (traces/metrics for command and event delivery), one wraps `ServiceProxy` (traces/metrics for each service call). Both use OpenTelemetry SDK with OTel-to-Prometheus exporter.

- [ ] **Step 1: Create HostMiddleware**

Wraps any `Host` implementation. On `DeliverCommand`:

- Start span: `plugin.command{plugin, command}`
- Record histogram: `plugin_command_duration_seconds{plugin, command}`
- On error: increment `plugin_errors_total{plugin, kind=command_error}`
- Record `plugin_events_emitted_total` from response

Same pattern for `DeliverEvent`.

- [ ] **Step 2: Create ServiceProxyMiddleware**

Wraps `ServiceProxy`. Each method:

- Start child span: `plugin.service{operation}`
- Record counter: `plugin_service_calls_total{plugin, operation}`
- Record histogram: `plugin_service_duration_seconds{plugin, operation}`

- [ ] **Step 3: Wire middleware in core.go**

Wrap each Host with HostMiddleware. Wrap ServiceProxyImpl with ServiceProxyMiddleware before passing to LocalPluginHost.

- [ ] **Step 4: Write tests verifying spans and metrics are recorded**

Use OTel test exporter.

- [ ] **Step 5: Run task test**
- [ ] **Step 6: Commit**

`feat(observability): OTel middleware for plugin Host and ServiceProxy`

---

## Post-Implementation Checklist

- [ ] `task test` passes
- [ ] `task lint` passes
- [ ] `task test:int` passes
- [ ] `task test:e2e` passes
- [ ] `task build` succeeds
- [ ] No remaining `RegisterAll()` calls (replaced by plugin loading)
- [ ] Only `quit.go` and `shutdown.go` in `internal/command/handlers/`
- [ ] `plugins/communication/`, `plugins/building/`, `plugins/help/` deleted
- [ ] All 7 `plugins/core-*` directories exist with manifests
- [ ] Echo-bot Lua plugin still works
- [ ] Proto regeneration committed
- [ ] Grep for `query_room` returns no Lua host function references
- [ ] Create PR using `commit-commands:commit-push-pr`

## Dependency Notes

- **holomush-8l7d** (comm event extensibility): Plugin verb registration (section 4 of that spec) can proceed after Phase 4 of this plan, when plugins can register and emit custom event types.
- **PR #145** added 6 handlers (ooc, pemit, home, teleport, examine, where) — all included in migration scope.
- **load\_priority** manifest field: defined in Task 1 Step 3 (manifest schema), used by LocalPluginHost in Task 4 for load ordering.
