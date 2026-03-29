<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Plugin-First Command Architecture Design

**Status:** Draft
**Date:** 2026-03-28
**Bead:** holomush-cv2k
**Scope:** Unified plugin contract, service proxy parity, command dispatch unification, core command migration, observability

## Overview

HoloMUSH has three command implementation paths with different capabilities: Go
handlers (compiled-in, full service access), Lua plugins (host functions, manifest),
and Go binary plugins (HandleEvent only, no command support). This creates
duplication (say/pose exist as both Go handlers and Lua plugins) and prevents
plugins from registering commands.

This spec unifies all commands under a single plugin architecture. Lua and Go
plugins have identical contracts — same dual-callback model, same service
access, same manifest-driven registration. The language choice is about
ergonomics (Lua for rapid iteration, Go for performance), not capability. All
game commands become plugins; only server lifecycle commands (`quit`, `shutdown`)
remain compiled-in.

## Goals

- MUST provide identical plugin contracts for Lua and Go plugins
- MUST support dual-callback model: `HandleCommand` + `HandleEvent`
- MUST provide a service proxy with full parity across all host types
- MUST unify command dispatch through the plugin manager
- MUST migrate all game commands from compiled-in handlers to plugins
- MUST support disabling built-in commands for game-specific overrides
- MUST instrument the plugin system with distributed tracing and metrics
- MUST rename Lua host functions to match terminology conventions (`query_room` → `query_location`)

## Non-Goals

- Plugin marketplace or distribution system
- Hot-reload of Go binary plugins (restart required)
- Plugin sandboxing beyond ABAC capability checks
- Plugin-to-plugin communication (plugins communicate via events)
- Configuration UI for plugin management

## Design Decisions

### Dual-Callback Model with Shared Services

Plugins MUST implement one or both of `HandleCommand` and `HandleEvent`. These
are separate interfaces because they serve different pipeline stages: command
handlers are producers (user input → events), event handlers are consumers
(events → side effects). The service access is identical — both callbacks can
query the world, check ABAC, emit events.

**Rationale:** Merging into a single interface forces either a fat request type
with unused fields or runtime type-checking to determine the trigger. The dual
model matches Lua's existing `on_command`/`on_event` split, which has proven
ergonomic for plugin authors. The service proxy is shared infrastructure, not
duplicated per callback.

### In-Process LocalPluginHost for Built-Ins

Built-in commands MUST run in-process via a `LocalPluginHost` that implements the
same `Host` interface as `LuaHost` and `BinaryHost`. Built-in plugins implement
the same `CommandHandler`/`EventHandler` interfaces as external plugins.

**Rationale:** Running built-in commands as separate processes (option A) adds
unnecessary overhead (~16 processes). Keeping them as direct function calls
without the plugin interface (option C) doesn't validate the contract.
`LocalPluginHost` gives zero transport overhead while proving every built-in
command works through the plugin interface — you could extract any core plugin
into a separate binary with no code changes.

### Only quit and shutdown Stay Compiled-In

The compiled-in command set MUST be limited to commands that manage systems
plugins themselves depend on: `quit` (tears down the connection plugins
communicate over) and `shutdown` (stops the plugin host). All other commands,
including `boot`, `who`, and admin commands, MUST be plugins.

**Rationale:** If the service proxy exposes session management, `boot` is just
"look up session, call DisconnectSession." Every command that can be expressed
through the service proxy SHOULD be a plugin. Minimizing the compiled-in set
maximizes the surface area validated by the plugin contract.

### Disable-Then-Replace for Command Overrides

Built-in commands MUST be disableable via server configuration. A game plugin
can then register its own version of the command. Two active registrations for
the same command name MUST NOT coexist.

**Rationale:** Priority-based override (where higher-priority plugins shadow
lower ones) creates ambiguity about which handler runs. Explicit disable-then-
replace is clear: the game designer says "I don't want the default say, here's
mine." If a built-in is disabled without a replacement, the command doesn't
exist — a diagnosable configuration error, not a silent fallback.

### Full Distributed Tracing

The plugin system MUST be instrumented with OpenTelemetry for distributed
tracing and Prometheus-compatible metrics. Every command and event delivery
creates a trace with spans for dispatch, handler execution, and service proxy
calls.

**Rationale:** Commands cross process boundaries (gRPC for binary plugins), pass
through multiple layers (dispatcher → host → handler → service proxy →
services), and the system is event-sourced. This is the exact use case
distributed tracing was designed for. The instrumentation points (Host middleware,
service proxy middleware) produce both metrics and traces from the same code.

## Plugin Contract

### Interfaces

```go
// CommandHandler processes player commands and produces events.
type CommandHandler interface {
    HandleCommand(ctx context.Context, cmd CommandRequest) (*CommandResponse, error)
}

// EventHandler reacts to game events and may produce more events.
type EventHandler interface {
    HandleEvent(ctx context.Context, event Event) ([]EmitEvent, error)
}
```

A plugin implements one or both. The manifest declares which commands and events
the plugin handles.

### CommandRequest

```go
type CommandRequest struct {
    Command       string    // parsed command name: "say", "dig"
    Args          string    // everything after the command name
    CharacterID   string    // invoking character ULID
    CharacterName string    // display name
    LocationID    string    // character's current location ULID
    SessionID     string    // active session ULID
    InvokedAs     string    // what the player actually typed (alias support)
    LastWhispered string    // last whisper target (for 'w' shorthand)
}
```

### CommandResponse

```go
type CommandResponse struct {
    Events         []EmitEvent   // events to append to the event store
    Output         string        // synchronous text output to the invoking player
    BootedSessions []string      // session IDs that were forcibly disconnected
    EndSession     bool          // signal that the invoking session should end
}
```

**Output field:** Many commands produce synchronous text output (who, where,
examine, help, alias management). The `Output` field carries this text. The
dispatcher emits it as a `command_response` event on the character stream. This
replaces the current `exec.Output()` io.Writer pattern. Handlers MUST use the
Output field instead of writing to a stream directly.

**BootedSessions field:** The `boot` command disconnects other players' sessions.
The dispatcher reads this field after command execution to emit leave events and
trigger session teardown. This replaces the current `exec.RecordBootedSession()`
/ `exec.BootedSessions()` pattern.

**EndSession field:** When true, signals the gRPC layer to close the invoking
connection after the response is sent. This replaces the current
`oops.Code("SESSION_ENDED")` sentinel error pattern used by self-boot. Compiled-
in commands (`quit`, `shutdown`) handle this directly, but plugins that need
session-ending behavior (e.g., a self-boot via the `boot` command) use this
field.

For pure event-producing commands (say, pose, page), `Output` is empty and
`Events` carries the events. For output-producing commands (who, where, examine),
`Events` may be empty and `Output` carries the display text. Both fields may be
populated simultaneously.

### Go Binary Plugin Proto

```protobuf
service PluginService {
    rpc HandleCommand(CommandRequest) returns (CommandResponse);
    rpc HandleEvent(HandleEventRequest) returns (HandleEventResponse);
}

service PluginHostService {
    // World read
    rpc QueryLocation(QueryLocationRequest) returns (QueryLocationResponse);
    rpc QueryCharacter(QueryCharacterRequest) returns (QueryCharacterResponse);
    rpc QueryLocationCharacters(QueryLocationCharactersRequest) returns (QueryLocationCharactersResponse);
    rpc QueryObject(QueryObjectRequest) returns (QueryObjectResponse);
    rpc FindLocation(FindLocationRequest) returns (FindLocationResponse);

    // World write
    rpc CreateLocation(CreateLocationRequest) returns (CreateLocationResponse);
    rpc CreateExit(CreateExitRequest) returns (CreateExitResponse);
    rpc CreateObject(CreateObjectRequest) returns (CreateObjectResponse);

    // Properties
    rpc SetProperty(SetPropertyRequest) returns (SetPropertyResponse);
    rpc GetProperty(GetPropertyRequest) returns (GetPropertyResponse);

    // Plugin KV
    rpc KVGet(KVGetRequest) returns (KVGetResponse);
    rpc KVSet(KVSetRequest) returns (KVSetResponse);
    rpc KVDelete(KVDeleteRequest) returns (KVDeleteResponse);

    // Session
    rpc FindSessionByName(FindSessionByNameRequest) returns (FindSessionByNameResponse);
    rpc SetLastWhispered(SetLastWhisperedRequest) returns (SetLastWhisperedResponse);
    rpc DisconnectSession(DisconnectSessionRequest) returns (DisconnectSessionResponse);

    // Commands
    rpc ListCommands(ListCommandsRequest) returns (ListCommandsResponse);
    rpc GetCommandHelp(GetCommandHelpRequest) returns (GetCommandHelpResponse);

    // Events
    rpc EmitEvent(EmitEventRequest) returns (EmitEventResponse);

    // Utility
    rpc Log(LogRequest) returns (LogResponse);
}
```

The `PluginHostService` is exposed by the host TO the plugin. During binary
plugin startup, the host starts this service and passes the connection details
to the plugin process via hashicorp/go-plugin's broker mechanism.

## Plugin Hosts

### Host Interface

```go
type Host interface {
    Load(ctx context.Context, manifest *Manifest, dir string) error
    Unload(ctx context.Context, name string) error
    DeliverEvent(ctx context.Context, name string, event Event) ([]EmitEvent, error)
    DeliverCommand(ctx context.Context, name string, cmd CommandRequest) (*CommandResponse, error)
    Plugins() []string
    Close(ctx context.Context) error
}
```

`DeliverCommand` is the new method. All existing Host implementations
(`LuaHost`, `BinaryHost`) MUST add this method.

### Three Host Implementations

| Host             | Manifest type | Transport                  | Service proxy          |
| ---------------- | ------------- | -------------------------- | ---------------------- |
| `LocalPluginHost` | `core`       | Direct Go function call    | Direct Go interface    |
| `LuaHost`        | `lua`         | In-process gopher-lua VM   | `holomush.*` host fns  |
| `BinaryHost`     | `binary`      | gRPC (hashicorp/go-plugin) | `PluginHostService` gRPC |

### LocalPluginHost

Wraps Go structs that implement `CommandHandler` and/or `EventHandler`. Core
plugins register their handler implementations during `Load()`. `DeliverCommand`
calls the handler directly — no serialization, no gRPC. The service proxy is a
Go interface passed to the handler, providing direct access to `WorldService`,
`SessionStore`, `EventStore`, and `AccessPolicyEngine`.

### LuaHost Changes

Add `DeliverCommand` implementation that calls `on_command(ctx)` in the Lua VM.
Today Lua command dispatch happens through a separate code path — it MUST flow
through the `Host` interface instead.

Rename host functions for terminology parity:

| Current name                      | New name                          |
| --------------------------------- | --------------------------------- |
| `holomush.query_room`             | `holomush.query_location`         |
| `holomush.query_room_characters`  | `holomush.query_location_characters` |

### BinaryHost Changes

Add `HandleCommand` RPC call in `DeliverCommand`. Implement `PluginHostService`
as a gRPC server started per-plugin during `Load()`. The plugin process connects
back to this service for world queries, event emission, etc.

## Command Dispatch Unification

### Current Flow

```text
Player input → Dispatcher.Dispatch()
  → alias resolution
  → capability check
  → registry.Lookup(commandName)
  → handler func(ctx, *CommandExecution) error  ← direct Go function call
```

Lua plugins use a separate code path for command dispatch.

### New Flow

```text
Player input → Dispatcher.Dispatch()
  → alias resolution
  → capability check
  → registry.Lookup(commandName)
  → returns (pluginName, hostType)
  → pluginManager.DeliverCommand(pluginName, request)
    → routes to correct Host (Local, Lua, Binary)
    → host.DeliverCommand(name, request)
    → host processes response events (emit to event store)
```

### Key Changes

**Command registry stores plugin routing, not function pointers.** `Register()`
takes a `CommandEntry` with `PluginName` and `Source` (core/lua/binary). The
registry no longer holds a `CommandHandler` function — it holds routing
information for the plugin manager.

**Dispatcher calls through PluginManager.** The dispatcher does not know or care
whether a command is handled by a Go function, Lua script, or binary plugin. It
asks the plugin manager to deliver the command to the correct plugin.

**One dispatch path.** No separate code paths for Lua commands vs Go handlers.
All commands flow through `PluginManager.DeliverCommand()` →
`Host.DeliverCommand()`.

**Post-dispatch infrastructure.** After successful command delivery, the
dispatcher (not the plugin) handles session activity updates
(`SessionStore.UpdateActivity()`), booted session teardown (reading
`CommandResponse.BootedSessions`), and session-end signals (reading
`CommandResponse.EndSession`). These are dispatcher responsibilities, not plugin
responsibilities.

**CommandRequest replaces CommandExecution.** The current `CommandExecution`
struct carries services directly. The new `CommandRequest` carries command
context only. Service access comes through the service proxy, not the request.

### Disabling Built-In Commands

Server configuration supports disabling built-in commands:

```yaml
plugins:
  disabled_commands:
    - say
    - pose
```

At startup, after registering built-in commands, the server removes disabled
entries from the command registry. If a game plugin registers `say` in its
manifest and the built-in `say` is disabled, the game plugin's registration
succeeds. If neither provides `say`, the command does not exist.

### Compiled-In Commands

`quit` and `shutdown` retain the current `CommandHandler func(ctx, *CommandExecution) error`
signature. They do NOT go through the plugin dispatch path. The dispatcher
checks for compiled-in commands before routing to the PluginManager. These
commands need direct access to connection state and server lifecycle that the
plugin contract intentionally does not expose.

### Verb Registry Interaction

Disabling a command does NOT remove its verb registration from the VerbRegistry
(per holomush-8l7d). Events of that type still render correctly — only the
command handler is removed.

## Service Proxy Parity

### Unified Operation Set

Every service operation MUST be available in all three host types. When a new
operation is added, all three implementations MUST be updated. A missing
operation in any host is a test failure.

| Operation                   | Category    | Lua host function                          | gRPC method                   |
| --------------------------- | ----------- | ------------------------------------------ | ----------------------------- |
| `QueryLocation`             | World read  | `holomush.query_location(id)`              | `QueryLocation`               |
| `QueryCharacter`            | World read  | `holomush.query_character(id)`             | `QueryCharacter`              |
| `QueryLocationCharacters`   | World read  | `holomush.query_location_characters(id)`   | `QueryLocationCharacters`     |
| `QueryObject`               | World read  | `holomush.query_object(id)`                | `QueryObject`                 |
| `FindLocation`              | World read  | `holomush.find_location(name)`             | `FindLocation`                |
| `CreateLocation`            | World write | `holomush.create_location(name, desc, t)`  | `CreateLocation`              |
| `CreateExit`                | World write | `holomush.create_exit(from, to, name, o)`  | `CreateExit`                  |
| `CreateObject`              | World write | `holomush.create_object(name, desc, o)`    | `CreateObject`                |
| `SetProperty`               | Property    | `holomush.set_property(id, key, val)`      | `SetProperty`                 |
| `GetProperty`               | Property    | `holomush.get_property(id, key)`           | `GetProperty`                 |
| `KVGet`                     | Plugin KV   | `holomush.kv_get(key)`                     | `KVGet`                       |
| `KVSet`                     | Plugin KV   | `holomush.kv_set(key, value)`              | `KVSet`                       |
| `KVDelete`                  | Plugin KV   | `holomush.kv_delete(key)`                  | `KVDelete`                    |
| `FindSessionByName`         | Session     | `holo.session.find_by_name(name)`          | `FindSessionByName`           |
| `SetLastWhispered`          | Session     | `holo.session.set_last_whispered(sid, n)`  | `SetLastWhispered`            |
| `DisconnectSession`         | Session     | `holo.session.disconnect(sid, reason)`     | `DisconnectSession`           |
| `ListCommands`              | Command     | `holomush.list_commands(char_id)`          | `ListCommands`                |
| `GetCommandHelp`            | Command     | `holomush.get_command_help(name, char_id)` | `GetCommandHelp`              |
| `EmitEvent`                 | Events      | `holo.emit.location(stream, type, pl)`     | `EmitEvent`                   |
| `Log`                       | Utility     | `holomush.log(level, message)`             | `Log`                         |
| `SetPlayerAlias`            | Aliases     | `holomush.set_player_alias(cid, a, cmd)`   | `SetPlayerAlias`              |
| `DeletePlayerAlias`         | Aliases     | `holomush.delete_player_alias(cid, a)`     | `DeletePlayerAlias`           |
| `ListPlayerAliases`         | Aliases     | `holomush.list_player_aliases(cid)`        | `ListPlayerAliases`           |
| `SetSystemAlias`            | Aliases     | `holomush.set_system_alias(a, cmd)`        | `SetSystemAlias`              |
| `DeleteSystemAlias`         | Aliases     | `holomush.delete_system_alias(a)`          | `DeleteSystemAlias`           |
| `ListSystemAliases`         | Aliases     | `holomush.list_system_aliases()`           | `ListSystemAliases`           |
| `CheckAliasShadow`          | Aliases     | `holomush.check_alias_shadow(a)`           | `CheckAliasShadow`            |
| `ListActiveSessions`        | Session     | `holo.session.list_active()`               | `ListActiveSessions`          |
| `BroadcastSystemMessage`    | Session     | `holo.session.broadcast(msg)`              | `BroadcastSystemMessage`      |
| `FindPropertyByPrefix`      | Property    | `holomush.find_property(prefix)`           | `FindPropertyByPrefix`        |
| `GetStartingLocationID`     | Config      | `holomush.starting_location()`             | `GetStartingLocationID`       |

**New operations:** `DisconnectSession` enables `boot` as a plugin.
`SetPlayerAlias`/`DeletePlayerAlias`/`ListPlayerAliases` and their system
variants enable the alias commands. `CheckAliasShadow` provides alias-command
shadow detection. `ListActiveSessions` and `BroadcastSystemMessage` enable
`who` and `wall`. `FindPropertyByPrefix` enables the `set` command's prefix
matching. `GetStartingLocationID` enables the `home` command's fallback.

**Rename cleanup:** `holomush.query_room` → `holomush.query_location`,
`holomush.query_room_characters` → `holomush.query_location_characters`. Old
names MUST be removed (not aliased) since this is a breaking change to plugin
APIs. Existing Lua plugins MUST be updated.

### LocalPluginHost Service Proxy

For `LocalPluginHost`, the service proxy is a Go interface that wraps the real
services:

```go
type ServiceProxy interface {
    QueryLocation(ctx context.Context, id string) (*Location, error)
    QueryCharacter(ctx context.Context, id string) (*Character, error)
    EmitEvent(ctx context.Context, stream, eventType string, payload []byte) error
    // ... all operations from the parity table
}
```

Core plugin handlers receive this interface. The implementation calls
`WorldService`, `SessionStore`, etc. directly. This is the same access pattern
as the current `CommandExecution.Services()` but through a defined interface
rather than a concrete struct.

## Core Command Migration

### Plugin Grouping

Commands are organized into logical plugins matching how a game designer thinks
about them:

| Plugin                | Type   | Commands                                                      |
| --------------------- | ------ | ------------------------------------------------------------- |
| `core-communication`  | `core` | say, pose, page, p, whisper, w, ooc, pemit, emit, wall        |
| `core-navigation`     | `core` | look, move, home, where, teleport                             |
| `core-building`       | `core` | dig, link                                                     |
| `core-objects`        | `core` | describe, desc, examine, create, set                          |
| `core-admin`          | `core` | boot, who                                                     |
| `core-help`           | `core` | help                                                          |
| `core-aliases`        | `core` | alias, unalias, aliases, sysalias, sysunsalias, sysaliases    |

This accounts for all 27 Go handler registrations in `RegisterAll()` (minus
`quit` and `shutdown` which stay compiled-in) plus 5 commands currently in Lua
plugins (`whisper`, `w`, `emit`, `dig`, `link`, `help`). Total: 31 plugin-
managed command registrations. Aliases like `p` (page), `w` (whisper), and
`desc` (describe) stay with their parent command's plugin. System alias
commands (`sysalias`, etc.) go in `core-aliases` with admin capabilities.

Each plugin has a `plugin.yaml` manifest, Go source files implementing
`CommandHandler`, and tests.

### Existing Lua Plugin Disposition

| Plugin                     | Action                                                    |
| -------------------------- | --------------------------------------------------------- |
| `plugins/communication/`   | Deleted. say/pose are dead code. whisper/emit move to core-communication as Go. |
| `plugins/building/`        | Absorbed into core-building. dig/link rewritten as Go.    |
| `plugins/help/`            | Absorbed into core-help. Rewritten as Go.                 |
| `plugins/echo-bot/`        | Kept as-is. Example plugin demonstrating Lua event handling. |

### Plugin Load Ordering

Plugin load order MUST be deterministic. Plugins declare an optional
`load_priority` field in their manifest (integer, default 0). Lower values
load first. Within the same priority, plugins load alphabetically by name.

Core plugins use a reserved priority (`-1000`) to guarantee they load before
all external plugins. This ensures the base command set exists for alias
shadow detection and help listing. The plugin loader MUST reject external
plugins with `load_priority` less than `-999` — the range below `-999` is
reserved for core plugins.

```yaml
# Default — loads with other priority-0 plugins, alphabetically
load_priority: 0

# Load early (e.g., a foundational library plugin)
# Valid range for external plugins: -999 to any positive integer
load_priority: -10
```

Host type (Lua vs binary) has no bearing on load order. A Lua plugin and a
binary plugin at the same priority are peers, ordered alphabetically.

### Migration Pattern Per Command

The Go handler logic moves from `internal/command/handlers/<cmd>.go` into the
core plugin. For pure event-producing commands (say, pose, ooc, page, pemit,
move, home, teleport, describe), the migration is mechanical:

1. Handler signature changes from `func(ctx, *CommandExecution) error` to
   implementing `CommandHandler` interface
2. Service access changes from `exec.Services().World()` to `proxy.QueryLocation()`
3. Business logic stays the same
4. Tests are adapted to use the service proxy mock instead of the Services mock

For output-producing commands (who, where, examine, help, alias management),
the migration also requires converting `exec.Output().Write()` calls to
`CommandResponse.Output` string building. For `boot`, the migration requires
using `CommandResponse.BootedSessions` and `CommandResponse.EndSession` instead
of `exec.RecordBootedSession()` and sentinel errors. These handlers require
more careful adaptation — they are not mechanical.

## Observability

### Instrumentation Points

Two middleware wrappers provide full observability:

**Host middleware** wraps every `Host` implementation:

- Creates root span for `DeliverCommand` / `DeliverEvent`
- Records `plugin_command_duration_seconds{plugin, command}` histogram
- Records `plugin_event_duration_seconds{plugin, event_type}` histogram
- Records `plugin_errors_total{plugin, kind}` counter (command\_error, event\_error, timeout)

**Service proxy middleware** wraps every service operation:

- Creates child span for each operation (linked to the command/event root span)
- Records `plugin_service_calls_total{plugin, operation}` counter
- Records `plugin_service_duration_seconds{plugin, operation}` histogram
- Records `plugin_events_emitted_total{plugin, event_type}` counter

### Trace Structure

A command produces a trace like:

```text
command:say (root span)
  ├── dispatch (alias resolution, capability check)
  ├── plugin:core-communication.HandleCommand
  │     ├── service:QueryLocation
  │     └── service:EmitEvent
  └── event_delivery (downstream subscribers)
```

An event delivery produces:

```text
event:say (root span)
  ├── plugin:echo-bot.HandleEvent
  │     └── service:EmitEvent
  └── plugin:logging.HandleEvent
        └── service:Log
```

### Technology

OpenTelemetry MUST be the single instrumentation layer for traces, metrics, and
log correlation. The Prometheus client library MUST NOT be used directly — all
metrics flow through the OTel SDK with an OTel-to-Prometheus exporter serving
the `/metrics` endpoint. This means one instrumentation API, one set of
middleware, and the exporter handles the Prometheus translation.

The existing slog structured logging integrates with OTel via the slog-otel
bridge for trace-log correlation (log entries include trace/span IDs).

## Files Affected

| File                                             | Change                                              |
| ------------------------------------------------ | --------------------------------------------------- |
| `internal/plugin/host.go`                        | Add `DeliverCommand` to Host interface               |
| `internal/plugin/local_host.go` (new)            | LocalPluginHost implementation                       |
| `internal/plugin/local_host_test.go` (new)       | LocalPluginHost tests                                |
| `internal/plugin/service_proxy.go` (new)         | ServiceProxy interface definition                    |
| `internal/plugin/service_proxy_impl.go` (new)    | ServiceProxy implementation (wraps real services)    |
| `internal/plugin/metrics.go` (new)               | Host and ServiceProxy observability middleware        |
| `internal/plugin/lua/host.go`                    | Add DeliverCommand, rename host functions            |
| `internal/plugin/goplugin/host.go`               | Add DeliverCommand, implement PluginHostService      |
| `api/proto/holomush/plugin/v1/plugin.proto`      | Add HandleCommand RPC, PluginHostService             |
| `pkg/plugin/sdk.go`                              | Add CommandHandler interface, CommandRequest type     |
| `internal/command/dispatcher.go`                 | Route through PluginManager instead of direct calls  |
| `internal/command/registry.go`                   | Store plugin routing instead of function pointers    |
| `internal/command/types.go`                      | CommandRequest replaces CommandExecution for plugins  |
| `cmd/holomush/core.go`                           | Create LocalPluginHost, register core plugins        |
| `plugins/core-communication/plugin.yaml` (new)   | Manifest for say, pose, page, whisper, ooc, pemit, wall |
| `plugins/core-communication/*.go` (new)          | Command handler implementations                     |
| `plugins/core-navigation/plugin.yaml` (new)      | Manifest for look, move, home, where, teleport      |
| `plugins/core-navigation/*.go` (new)             | Command handler implementations                     |
| `plugins/core-building/plugin.yaml` (new)        | Manifest for dig, link                               |
| `plugins/core-building/*.go` (new)               | Command handler implementations                     |
| `plugins/core-objects/plugin.yaml` (new)         | Manifest for describe, desc, examine, create, set    |
| `plugins/core-objects/*.go` (new)                | Command handler implementations                     |
| `plugins/core-admin/plugin.yaml` (new)           | Manifest for boot, who                               |
| `plugins/core-admin/*.go` (new)                  | Command handler implementations                     |
| `plugins/core-help/plugin.yaml` (new)            | Manifest for help                                    |
| `plugins/core-help/*.go` (new)                   | Command handler implementations                     |
| `plugins/core-aliases/plugin.yaml` (new)         | Manifest for alias                                   |
| `plugins/core-aliases/*.go` (new)                | Command handler implementations                     |
| `plugins/communication/` (deleted)               | Replaced by core-communication                       |
| `plugins/building/` (deleted)                    | Replaced by core-building                            |
| `plugins/help/` (deleted)                        | Replaced by core-help                                |
| `internal/command/handlers/` (deleted)           | Migrated to core plugins                             |
| `internal/plugin/hostfunc/functions.go`          | Rename query\_room → query\_location                 |

## Testing Strategy

- Unit tests for LocalPluginHost (DeliverCommand, DeliverEvent)
- Unit tests for ServiceProxy (each operation, mock underlying services)
- Unit tests for command dispatch through PluginManager
- Unit tests for command disable/replace via config
- Parity tests: verify every ServiceProxy operation exists in all three hosts
- Integration tests: command round-trip through LocalPluginHost
- Integration tests: command round-trip through LuaHost
- Integration tests: event delivery through all host types
- E2E tests: player commands work end-to-end after migration
- E2E tests: echo-bot Lua plugin still works after LuaHost changes
- Observability tests: verify spans and metrics are emitted for commands and events
