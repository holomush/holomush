---
title: "Binary Plugin Author Guide"
---

Binary plugins are standalone Go programs that communicate with HoloMUSH over
gRPC using HashiCorp's [go-plugin](https://github.com/hashicorp/go-plugin)
system. They run as separate processes with full access to Go's ecosystem.

## When to use binary plugins

| Consideration    | Lua                             | Binary                                  |
| ---------------- | ------------------------------- | --------------------------------------- |
| Complexity       | Simple event reactions          | Complex domain logic, state machines    |
| Dependencies     | None (sandboxed VM)             | Any Go library                          |
| Storage          | KV store only                   | KV or dedicated PostgreSQL schema       |
| Service exposure | Not supported                   | Provide gRPC services to the host       |
| Iteration speed  | Edit and reload                 | Compile, restart                        |
| Examples         | echo-bot, core-communication    | core-scenes                             |

Use binary plugins when you need persistent storage, complex data models, or
want to expose a gRPC service that other parts of the system can call.

## Plugin manifest

Every plugin needs a `plugin.yaml` in its directory. Here is the `core-scenes`
manifest as a reference:

```yaml
name: core-scenes
version: 1.0.0
type: binary

requires:
  - holomush.world.v1.WorldService

provides:
  - holomush.scene.v1.SceneService

storage: postgres

binary-plugin:
  executable: core-scenes

commands:
  - name: scene
    capabilities:
      - action: write
        resource: scene
        scope: local
    help: "Manage RP scenes"
    usage: "scene <subcommand> [args]"

  - name: scenes
    help: "Browse open scenes"
    usage: "scenes [--tags tag1,tag2]"
```

### Field reference

| Field            | Required | Description                                               |
| ---------------- | -------- | --------------------------------------------------------- |
| `name`           | Yes      | Unique identifier (lowercase, a-z0-9, hyphens)           |
| `version`        | Yes      | Semantic version                                          |
| `type`           | Yes      | Must be `binary`                                          |
| `requires`       | No       | Proto service names this plugin depends on                |
| `provides`       | No       | Proto service names this plugin exposes                   |
| `storage`        | No       | `postgres` for a dedicated schema, omit for KV-only      |
| `binary-plugin`  | Yes      | Binary-specific config                                    |
| `commands`       | No       | Commands this plugin handles                              |
| `policies`       | No       | ABAC policies (Cedar-style DSL)                           |

### Commands

Each command entry declares a name, help text, usage string, and optional
capabilities. Capabilities use the two-layer authorization model:

```yaml
commands:
  - name: scene
    capabilities:
      - action: write
        resource: scene
        scope: local    # local | self | global
    help: "Manage RP scenes"
    usage: "scene <subcommand> [args]"
```

## Getting started

### Minimal plugin

Create a directory under `plugins/` and add a `plugin.yaml` and `main.go`:

```text
plugins/my-plugin/
  plugin.yaml
  main.go
```

`plugin.yaml`:

```yaml
name: my-plugin
version: 1.0.0
type: binary
binary-plugin:
  executable: my-plugin
```

`main.go`:

```go
package main

import (
    "context"

    pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

type myPlugin struct{}

func (p *myPlugin) HandleEvent(_ context.Context, event pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
    // Process events here
    return nil, nil
}

func main() {
    pluginsdk.Serve(&pluginsdk.ServeConfig{
        Handler: &myPlugin{},
    })
}
```

### Adding command handling

Implement `CommandHandler` to handle commands declared in your manifest:

```go
func (p *myPlugin) HandleCommand(_ context.Context, req pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
    switch req.Command {
    case "greet":
        return pluginsdk.OK("Hello, " + req.CharacterName + "!"), nil
    default:
        return pluginsdk.Errorf("unknown subcommand: %s", req.Args), nil
    }
}
```

The `CommandRequest` provides full context about the invoking player:

| Field           | Description                                |
| --------------- | ------------------------------------------ |
| `Command`       | Parsed command name                        |
| `Args`          | Everything after the command name          |
| `CharacterID`   | Invoking character ULID                    |
| `CharacterName` | Character display name                     |
| `LocationID`    | Character's current location ULID          |
| `SessionID`     | Active session ULID                        |
| `PlayerID`      | Player account ULID                        |
| `InvokedAs`     | What the player actually typed             |

Response helpers: `pluginsdk.OK(output)`, `pluginsdk.Errorf(fmt, args...)`,
`pluginsdk.Failuref(fmt, args...)`.

## Service contracts

### requires

The `requires` list declares proto services your plugin depends on. The host
resolves these and provides connection details during `Init`. For Lua plugins,
`requires` entries inject capability host functions (e.g., `session.*`,
`alias.*`). For binary plugins, they provide gRPC client connections via the
broker.

### provides

The `provides` list declares proto services your plugin exposes. The host can
proxy RPCs from other subsystems to your plugin's gRPC server. Services are
registered on the same go-plugin transport via `RegisterServices`.

### Dependency resolution

The plugin manager validates that all `requires` entries can be satisfied before
loading a plugin. If a required service is unavailable, the plugin fails to load
with a clear error message.

## Service injection

Binary plugins that declare `requires` receive service connections through the
`Init` RPC. The host sends a `ServiceConfig` containing:

- `connection_string` -- PostgreSQL DSN (when `storage: postgres` is declared)
- `required_services` -- map of service name to broker address (`broker:<id>`)

### Receiving required services

Use `ParseBrokerServices` to extract broker IDs, then dial each service:

```go
func (p *myPlugin) Init(ctx context.Context, config *pluginv1.ServiceConfig) error {
    // Parse broker addresses from required_services map
    services, err := pluginsdk.ParseBrokerServices(config.GetRequiredServices())
    if err != nil {
        return fmt.Errorf("parse broker services: %w", err)
    }

    // Dial required services via the GRPCBroker
    for name, brokerID := range services {
        conn, err := p.broker.Dial(brokerID)
        if err != nil {
            return fmt.Errorf("dial %s: %w", name, err)
        }
        switch name {
        case "holomush.world.v1.WorldService":
            p.worldClient = worldv1.NewWorldServiceClient(conn)
        }
    }
    return nil
}
```

Each broker ID maps to a service the host is serving on behalf of your plugin.
The returned `grpc.ClientConn` gives you a typed gRPC client for that service.

## Storage

Plugins can use two storage tiers:

### KV store

All plugins (including Lua) can use the namespaced key-value store via the
`PluginHostService` callbacks. KV operations are scoped to the plugin name and
enforced by ABAC policies declared in the manifest.

### PostgreSQL (dedicated schema)

Binary plugins that declare `storage: postgres` get an isolated PostgreSQL
schema provisioned automatically by the `SchemaProvisioner`:

1. The host creates a schema named `plugin_<name>` (e.g., `plugin_core_scenes`)
2. A dedicated database role is created with access restricted to that schema
3. The connection string (with `search_path` set) is passed via `Init`

#### Using the storage SDK

The `pkg/plugin/storage` package provides `Connect` and `RunMigrationsFS`:

```go
import (
    "embed"
    "io/fs"

    "github.com/holomush/holomush/pkg/plugin/storage"
)

//go:embed migrations/*.up.sql
var migrationsFS embed.FS

func NewStore(ctx context.Context, connString string) (*Store, error) {
    pool, err := storage.Connect(ctx, connString)
    if err != nil {
        return nil, err
    }

    // Extract the sub-filesystem for migrations
    sub, err := fs.Sub(migrationsFS, "migrations")
    if err != nil {
        pool.Close()
        return nil, err
    }

    if err := storage.RunMigrationsFS(ctx, pool, sub); err != nil {
        pool.Close()
        return nil, err
    }

    return &Store{pool: pool}, nil
}
```

#### Writing migrations

Place SQL files in a `migrations/` directory with sequential numbering:

```text
plugins/my-plugin/
  migrations/
    000001_initial.up.sql
    000001_initial.down.sql
```

The storage SDK tracks applied migrations in a `plugin_migrations` table within
your schema. Only `.up.sql` files are executed; `.down.sql` files are for
manual rollback.

Migration rules:

- Use `IF NOT EXISTS` / `IF EXISTS` for idempotency
- No triggers or functions -- all logic lives in Go
- One logical change per migration file

## Providing services

Plugins that declare `provides` use `ServeWithServices` instead of `Serve`.
Your plugin struct implements `ServiceProvider`:

```go
type ServiceProvider interface {
    // RegisterServices registers gRPC services on the go-plugin transport.
    RegisterServices(registrar grpc.ServiceRegistrar)

    // Init is called by the host with DB connection string and service config.
    Init(ctx context.Context, config *pluginv1.ServiceConfig) error
}
```

### Full example (from core-scenes)

```go
type scenePlugin struct {
    store   *SceneStore
    service *SceneServiceImpl
}

// HandleEvent processes events (no-op for scenes currently).
func (p *scenePlugin) HandleEvent(_ context.Context, _ pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
    return nil, nil
}

// HandleCommand handles scene commands.
func (p *scenePlugin) HandleCommand(_ context.Context, req pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
    return pluginsdk.OK("Use scene service RPCs."), nil
}

// RegisterServices exposes SceneService on the go-plugin transport.
func (p *scenePlugin) RegisterServices(registrar grpc.ServiceRegistrar) {
    scenev1.RegisterSceneServiceServer(registrar, p.service)
}

// Init wires up storage and services.
func (p *scenePlugin) Init(ctx context.Context, config *pluginv1.ServiceConfig) error {
    connStr := config.GetConnectionString()
    if connStr == "" {
        return oops.Code("SCENE_INIT_FAILED").Errorf("connection_string is required")
    }

    store, err := NewSceneStore(ctx, connStr)
    if err != nil {
        return err
    }

    p.store = store
    p.service.store = store
    return nil
}

func main() {
    plugin := &scenePlugin{
        service: &SceneServiceImpl{},
    }

    pluginsdk.ServeWithServices(
        &pluginsdk.ServeConfig{Handler: plugin},
        plugin,
    )
}
```

Key pattern: pre-allocate the service struct in `main()` so that
`RegisterServices` (called during gRPC server setup, before `Init`) has a valid
receiver. `Init` wires the store into both the plugin and the service.

## Testing

### Unit tests with mocks

Define a narrow interface for your store and mock it:

```go
type sceneStorer interface {
    CreateScene(ctx context.Context, row *SceneRow) error
    GetScene(ctx context.Context, id string) (*SceneRow, error)
    // ...
}

// In tests, use a mock implementation
type mockStore struct {
    scenes map[string]*SceneRow
}

func (m *mockStore) CreateScene(_ context.Context, row *SceneRow) error {
    m.scenes[row.ID] = row
    return nil
}
```

Use testify for assertions:

```go
func TestCreateSceneRejectsEmptyTitle(t *testing.T) {
    svc := NewSceneServiceImpl(&mockStore{scenes: make(map[string]*SceneRow)})

    _, err := svc.CreateScene(context.Background(), &scenev1.CreateSceneRequest{
        CharacterId: "01ABC",
        Title:       "",
    })

    require.Error(t, err)
    st, ok := status.FromError(err)
    require.True(t, ok)
    assert.Equal(t, codes.InvalidArgument, st.Code())
}
```

### Integration tests with testcontainers

For database tests, use testcontainers to spin up a PostgreSQL instance:

```go
//go:build integration

func TestSceneStoreCreatesAndRetrievesScene(t *testing.T) {
    ctx := context.Background()
    connStr := setupTestDB(t) // testcontainers helper

    store, err := NewSceneStore(ctx, connStr)
    require.NoError(t, err)
    defer store.Close()

    row := &SceneRow{
        ID:      "01TEST",
        Title:   "Test Scene",
        OwnerID: "01OWNER",
        State:   "active",
        // ...
    }

    require.NoError(t, store.CreateScene(ctx, row))

    got, err := store.GetScene(ctx, "01TEST")
    require.NoError(t, err)
    assert.Equal(t, "Test Scene", got.Title)
}
```

## Building

### Single plugin

```bash
task plugin:build -- my-plugin
```

### All plugins

```bash
task plugin:build-all
```

This discovers all binary plugins (directories with `type: binary` in
`plugin.yaml`) and compiles them for the host platform plus linux/amd64 and
linux/arm64.

### Plugin discovery

The host discovers plugins at startup by scanning the `plugins/` directory for
`plugin.yaml` files. Binary plugins must have a compiled executable matching the
`binary-plugin.executable` field in the manifest.

## SDK reference

### Core types

| Type              | Purpose                                     |
| ----------------- | ------------------------------------------- |
| `Handler`         | Event handler interface (required)          |
| `CommandHandler`  | Command handler interface (optional)        |
| `ServiceProvider` | Service registration and init (optional)    |
| `ServeConfig`     | Configuration for `Serve`                   |
| `Event`           | Incoming event from the host                |
| `EmitEvent`       | Outgoing event to emit                      |
| `CommandRequest`  | Command invocation context                  |
| `CommandResponse` | Command result with status and output       |
| `CommandStatus`   | Outcome category (OK, Error, Failure, Fatal)|

### Entry points

| Function           | When to use                              |
| ------------------ | ---------------------------------------- |
| `Serve`            | Event-only plugins, no service injection |
| `ServeWithServices`| Plugins that provide or require services |

### Storage SDK (`pkg/plugin/storage`)

| Function              | Purpose                                        |
| --------------------- | ---------------------------------------------- |
| `Connect`             | Open a connection pool to the plugin's schema  |
| `RunMigrations`       | Run embedded SQL migrations (embed.FS)         |
| `RunMigrationsFS`     | Run migrations from any fs.FS                  |
| `ParseSchemaFromConnString` | Extract schema name from connection string|

### Broker helpers (`pkg/plugin`)

| Function              | Purpose                                        |
| --------------------- | ---------------------------------------------- |
| `ParseBrokerServices` | Parse required_services map into broker IDs    |

### EventSink

The `EventSink` facade is the plugin-side entry point for emitting events on
behalf of the plugin. Plugins that need to publish events to the host's event
store (rather than returning them from `HandleEvent`) call `EventSink.Emit`
directly. The SDK adapter detects `EventSinkAware` during `Init` and injects a
broker-backed sink.

#### Declaring EventSinkAware

```go
type myPlugin struct {
    sink pluginsdk.EventSink
}

func (p *myPlugin) SetEventSink(sink pluginsdk.EventSink) {
    p.sink = sink
}

func (p *myPlugin) someMethod(ctx context.Context, stream string) error {
    return p.sink.Emit(ctx, pluginsdk.EmitIntent{
        Stream:  stream,
        Type:    "my_event",
        Payload: `{"key":"value"}`,
    })
}
```

### FocusClient

The `FocusClient` facade is the plugin-side entry point for driving
server-owned session focus state. Plugins that need to declare a
character's participation in a focused context — scenes today,
mail/admin-views in the future — call `FocusClient` rather than
mutating session state directly. All calls cross the plugin broker
(mTLS) to the host's `PluginHostService`.

#### Methods

| Method | Purpose |
| --- | --- |
| `JoinFocus(ctx, sessionID, target)` | Add a focus membership. The server determines streams, replay mode, and cursor baselines based on the target's `FocusKind`. Idempotent — treat `FOCUS_ALREADY_MEMBER` as success. |
| `LeaveFocus(ctx, sessionID, target)` | Remove a focus membership. Idempotent on non-member. Clears `PresentingFocus` if it pointed at the removed target. |
| `PresentFocus(ctx, sessionID, target)` | Update the session's presenting-focus pointer. The target MUST already be a member; non-members get `FOCUS_NOT_MEMBER`. No replay or subscription change — pure bookkeeping. |
| `QueryStreamHistory(ctx, req)` | Read the tail of a stream for plugin-side display (e.g., last 20 messages on join). Read-only — does not mutate cursors. The host clamps `Count` at 500. |
| `SetConnectionFocus(ctx, connectionID, focusKey, isSceneGrid)` | Set per-connection focus (Phase 5). The substrate enforces INV-P5-1: `focusKey` MUST refer to a scene the character is already a member of; the call is rejected otherwise. Pass `isSceneGrid=true` to clear focus without removing membership. |
| `AutoFocusOnJoin(ctx, characterID, sceneID)` | Fan-out focus to all terminal and telnet connections of `characterID` when the character joins `sceneID` (Phase 5). Returns a summary `{focused, skipped, failed, total_connection_count}`. Comms-hub connections are never auto-focused (INV-P5-4). |
| `IsAnyConnFocused(ctx, characterID, sceneID)` | Returns `true` if at least one of `characterID`'s connections currently has `FocusKey` pointing at `sceneID` (Phase 5). Read-only. |

#### Declaring FocusClientAware

A plugin opts in by implementing `FocusClientAware`:

```go
type scenePlugin struct {
    focusClient pluginsdk.FocusClient
}

func (p *scenePlugin) SetFocusClient(client pluginsdk.FocusClient) {
    p.focusClient = client
}

func (p *scenePlugin) handleJoin(ctx context.Context, req pluginsdk.CommandRequest, args string) (*pluginsdk.CommandResponse, error) {
    sceneID := strings.TrimSpace(args)
    // ... persist the DB row first ...

    err := p.focusClient.JoinFocus(ctx, req.SessionID, pluginsdk.FocusKey{
        Kind:     pluginsdk.FocusKindScene,
        TargetID: sceneID,
    })
    if err != nil {
        // Inspect oops error code; FOCUS_ALREADY_MEMBER is idempotent-success.
        return pluginsdk.Errorf("failed to join scene: %v", err), nil
    }
    return pluginsdk.OK(fmt.Sprintf("Joined scene %s.", sceneID)), nil
}
```

The SDK adapter detects `FocusClientAware` during `Init` and injects a
broker-backed client. `EventSink` and `FocusClient` share a single
`*grpc.ClientConn` to `PluginHostService`, so a plugin implementing both
`EventSinkAware` and `FocusClientAware` opens ONE connection — not two.

#### Error codes

`FocusClient` methods return `oops`-coded errors; inspect via `errors.As`:

| Code | Meaning |
| --- | --- |
| `SESSION_NOT_FOUND` | The session does not exist. |
| `SESSION_EXPIRED` | The session is past its TTL. |
| `FOCUS_ALREADY_MEMBER` | Membership already exists — treat as idempotent success. |
| `FOCUS_KIND_UNREGISTERED` | The host has no policy registered for this `FocusKind`. |
| `FOCUS_POLICY_FAILED` | The kind policy rejected the join. |
| `FOCUS_NOT_MEMBER` | (`PresentFocus` only) Target is not in the session's memberships. |

#### Invariants

- Plugins MUST NOT mutate `session.FocusMemberships` directly. The facade is
  the only sanctioned path.
- Plugins MUST NOT declare replay modes or stream names on focus RPCs — the
  server owns those decisions.
- `QueryStreamHistory` is strictly read-only.

#### References

- Plugin adoption spec: [B10 core-scenes Adoption](../../../docs/superpowers/specs/2026-04-16-b10-core-scenes-adoption-design.md)
- Implementation: `pkg/plugin/focus_client.go`

## Audit-row SDK helpers (Phase 7)

Plugin authors don't write crypto code. The host owns encryption,
decryption, and authorization. After Phase 7, plugin-owned audit
tables hold ciphertext byte-equal to the bus envelope for sensitive
events; the plugin's `PluginAuditService.QueryHistory` returns those
ciphertext bytes verbatim to the host, which validates and decrypts
before delivering to clients.

The `pluginsdk` package provides two helpers in `pkg/plugin/audit.go`:

### `pluginsdk.StoreFromMessage(msg jetstream.Msg) (AuditRow, error)`

Call at `PluginAuditService.AuditEvent` RPC handler. Extracts an
`AuditRow` from the JetStream message — projection fields (id,
subject, type, timestamp, actor) plus crypto envelope (codec, payload,
dek_ref, dek_version) plus schema_ver. Plugin authors persist the row
fields verbatim into their own audit table.

### `pluginsdk.LoadForQuery(row AuditRow) (*pluginauditpb.AuditRow, error)`

Call at `PluginAuditService.QueryHistory` RPC handler. Converts a
stored `AuditRow` back to the proto frame returned on the stream.
Round-trip stable with `StoreFromMessage`.

### Plugin audit table schema

Phase 7 requires plugin audit tables to mirror `events_audit` for
crypto-bearing columns:

| Column | Type | Notes |
| --- | --- | --- |
| `id` | `BYTEA PRIMARY KEY` | 16-byte ULID; matches `AuditRow.id` |
| `subject` | `TEXT NOT NULL` | bus subject |
| `type` | `TEXT NOT NULL` | qualified `<plugin>:<event_type>` |
| `timestamp` | `TIMESTAMPTZ NOT NULL` | |
| `actor_kind`, `actor_id` | `TEXT`, `BYTEA` | from `AuditRow.actor` |
| `payload` | `BYTEA NOT NULL` | ciphertext when `codec != identity` |
| `schema_ver` | `SMALLINT NOT NULL` | from `AuditRow.schema_ver` |
| `codec` | `TEXT NOT NULL` | `identity` or `xchacha20poly1305-v1` |
| `dek_ref` | `BIGINT NULL` | NULL for identity codec |
| `dek_version` | `INTEGER NULL` | NULL for identity codec |

See `plugins/core-scenes/audit.go` for a reference implementation.
