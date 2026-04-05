# Plugin Architecture Phase 3: Scene Binary Plugin Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the scene system as the first binary plugin, proving the full binary plugin path end-to-end — from WorldService proto contract through go-plugin subprocess to auto-proxied SceneService on the server's gRPC port.

**Architecture:** The server wraps `internal/world.Service` as an in-process gRPC `WorldService` and registers it in the service registry. The scene plugin is a standalone Go binary using hashicorp/go-plugin that receives WorldService as a gRPC client connection, provides `holomush.scene.v1.SceneService`, and uses `storage: postgres` with schema-isolated tables. The plugin SDK gains a `ServiceProvider` interface so binary plugins can register gRPC services on their go-plugin transport. The server auto-proxies SceneService calls via `GRPCServiceProxy`.

**Tech Stack:** Go 1.25, hashicorp/go-plugin v1.7, protobuf/gRPC, pgx/v5, testify, testcontainers

**Spec:** `docs/superpowers/specs/2026-04-05-plugin-architecture-rework-design.md` (Sections 2, 3, 5, 8, 11 Phase 3)

**Scene Spec:** `docs/superpowers/specs/2026-04-04-scenes-and-rp-design.md`

**Depends on:** Phase 1 infrastructure (complete), Phase 2 core-to-Lua migration (complete)

---

## Scope

Phase 3 builds the end-to-end binary plugin path using scenes as the proving ground. Specifically:

1. **WorldService proto contract** — server-provided service that wraps `internal/world.Service`
2. **Service injection for binary plugins** — SDK and host changes so plugins receive required services and register provided services
3. **Scene plugin binary** — standalone binary that implements SceneService, uses WorldService, owns its Postgres schema
4. **Server wiring** — auto-proxy SceneService, register WorldService, schema provisioning

**Out of scope:** Scene commands (Lua plugin that consumes SceneService), web client UI, pose order system, scene board, forum view, event stream routing. These are separate follow-up work.

---

## File Structure

### New Files

| File | Responsibility |
|------|---------------|
| `api/proto/holomush/world/v1/world.proto` | WorldService proto contract |
| `internal/world/grpc_server.go` | gRPC server adapter wrapping `world.Service` |
| `internal/world/grpc_server_test.go` | Tests |
| `pkg/plugin/service.go` | `ServiceProvider` interface + `ServeWithServices` for plugins that provide gRPC services |
| `pkg/plugin/service_test.go` | Tests |
| `plugins/core-scenes/main.go` | Scene plugin binary entry point |
| `plugins/core-scenes/plugin.yaml` | Plugin manifest |
| `plugins/core-scenes/service.go` | SceneService gRPC implementation backed by scene domain |
| `plugins/core-scenes/service_test.go` | Tests |
| `plugins/core-scenes/store.go` | PostgreSQL scene repository |
| `plugins/core-scenes/store_test.go` | Tests |
| `plugins/core-scenes/migrations/000001_scenes.up.sql` | Scene tables |
| `plugins/core-scenes/migrations/000001_scenes.down.sql` | Rollback |
| `internal/plugin/schema_provisioner.go` | Creates `plugin_<name>` Postgres schema + role |
| `internal/plugin/schema_provisioner_test.go` | Tests |

### Modified Files

| File | Change |
|------|--------|
| `internal/plugin/goplugin/host.go` | Pass required services + conn string during plugin load |
| `internal/plugin/goplugin/plugin.go` | Support plugins that register their own gRPC services |
| `internal/plugin/setup/subsystem.go` | Register WorldService in service registry, install gRPC proxy, schema provisioning |
| `pkg/plugin/sdk.go` | Add `ServeWithServices` alongside existing `Serve` |
| `cmd/holomush/sub_grpc.go` | Install `GRPCServiceProxy` on gRPC server |
| `api/proto/holomush/plugin/v1/plugin.proto` | Add `ServiceConfig` message to handshake |

---

## Task 1: WorldService Proto Contract

Define the protobuf service contract that wraps `internal/world.Service` for plugin consumption.

**Files:**

- Create: `api/proto/holomush/world/v1/world.proto`

- [ ] **Step 1: Create the proto file**

```protobuf
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

syntax = "proto3";

package holomush.world.v1;

option go_package = "github.com/holomush/holomush/pkg/proto/holomush/world/v1;worldv1";

// WorldService provides read access to the world model (locations, characters, exits).
// Implemented by the server and registered in the service registry for plugin consumption.
service WorldService {
  rpc GetLocation(GetLocationRequest) returns (GetLocationResponse);
  rpc GetCharacter(GetCharacterRequest) returns (GetCharacterResponse);
  rpc ListCharactersAtLocation(ListCharactersAtLocationRequest) returns (ListCharactersAtLocationResponse);
  rpc ListExits(ListExitsRequest) returns (ListExitsResponse);
}

message LocationInfo {
  string id = 1;
  string name = 2;
  string description = 3;
  string type = 4;
  string owner_id = 5;
}

message CharacterInfo {
  string id = 1;
  string player_id = 2;
  string name = 3;
  string description = 4;
  string location_id = 5;
}

message ExitInfo {
  string id = 1;
  string name = 2;
  string source_id = 3;
  string destination_id = 4;
  string description = 5;
}

message GetLocationRequest {
  string subject_id = 1;
  string location_id = 2;
}

message GetLocationResponse {
  LocationInfo location = 1;
}

message GetCharacterRequest {
  string subject_id = 1;
  string character_id = 2;
}

message GetCharacterResponse {
  CharacterInfo character = 1;
}

message ListCharactersAtLocationRequest {
  string subject_id = 1;
  string location_id = 2;
}

message ListCharactersAtLocationResponse {
  repeated CharacterInfo characters = 1;
}

message ListExitsRequest {
  string subject_id = 1;
  string location_id = 2;
}

message ListExitsResponse {
  repeated ExitInfo exits = 1;
}
```

- [ ] **Step 2: Generate Go code**

Run: `task proto`

Expected: Generated files in `pkg/proto/holomush/world/v1/`.

- [ ] **Step 3: Verify generation succeeded**

Run: `ls pkg/proto/holomush/world/v1/`

Expected: `world.pb.go`, `world_grpc.pb.go`

- [ ] **Step 4: Commit**

```bash
JJ_EDITOR=true jj --no-pager commit -m "feat(proto): add WorldService proto contract for plugin consumption"
```

---

## Task 2: WorldService gRPC Server Adapter

Wrap `internal/world.Service` as a gRPC server so it can be registered in the service registry via `InProcessConn`.

**Files:**

- Create: `internal/world/grpc_server.go`
- Create: `internal/world/grpc_server_test.go`

- [ ] **Step 1: Write failing test for GetLocation**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world_test

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/internal/world/mocks"
	worldv1 "github.com/holomush/holomush/pkg/proto/holomush/world/v1"
)

func startWorldServer(t *testing.T, svc *world.Service) worldv1.WorldServiceClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer() //nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection
	worldv1.RegisterWorldServiceServer(srv, world.NewWorldServiceServer(svc))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() { srv.Stop(); _ = lis.Close() })

	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()), //nosemgrep: go.grpc.tls.grpc-client-new-insecure-connection.grpc-client-new-insecure-connection
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return worldv1.NewWorldServiceClient(conn)
}

func TestWorldServiceServerGetLocationReturnsLocationInfo(t *testing.T) {
	locRepo := mocks.NewMockLocationRepository(t)
	locID := ulid.MustNew(1, nil)
	ownerID := ulid.MustNew(2, nil)

	locRepo.EXPECT().Get(mock.Anything, locID).Return(&world.Location{
		ID:          locID,
		Name:        "Town Square",
		Description: "A bustling town square.",
		Type:        world.LocationTypeRoom,
		OwnerID:     ownerID,
	}, nil)

	svc := world.NewService(world.ServiceConfig{
		LocationRepo: locRepo,
		Engine:       policytest.NewGrantEngine(),
	})

	client := startWorldServer(t, svc)
	resp, err := client.GetLocation(context.Background(), &worldv1.GetLocationRequest{
		SubjectId:  ownerID.String(),
		LocationId: locID.String(),
	})
	require.NoError(t, err)
	assert.Equal(t, "Town Square", resp.GetLocation().GetName())
	assert.Equal(t, locID.String(), resp.GetLocation().GetId())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestWorldServiceServerGetLocationReturnsLocationInfo ./internal/world/`

Expected: Compilation error — `world.NewWorldServiceServer` not defined.

- [ ] **Step 3: Implement WorldServiceServer**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package world

import (
	"context"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/holomush/holomush/internal/access"
	worldv1 "github.com/holomush/holomush/pkg/proto/holomush/world/v1"
)

// Compile-time interface check.
var _ worldv1.WorldServiceServer = (*WorldServiceServer)(nil)

// WorldServiceServer adapts world.Service to the WorldService gRPC contract.
type WorldServiceServer struct {
	worldv1.UnimplementedWorldServiceServer
	svc *Service
}

// NewWorldServiceServer creates a WorldServiceServer backed by the given Service.
func NewWorldServiceServer(svc *Service) *WorldServiceServer {
	return &WorldServiceServer{svc: svc}
}

// GetLocation retrieves a location by ID.
func (s *WorldServiceServer) GetLocation(ctx context.Context, req *worldv1.GetLocationRequest) (*worldv1.GetLocationResponse, error) {
	locID, err := ulid.ParseStrict(req.GetLocationId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid location_id: %v", err)
	}

	subjectID := access.CharacterSubject(req.GetSubjectId())
	loc, err := s.svc.GetLocation(ctx, subjectID, locID)
	if err != nil {
		return nil, mapWorldError(err)
	}

	return &worldv1.GetLocationResponse{
		Location: locationToProto(loc),
	}, nil
}

// GetCharacter retrieves a character by ID.
func (s *WorldServiceServer) GetCharacter(ctx context.Context, req *worldv1.GetCharacterRequest) (*worldv1.GetCharacterResponse, error) {
	charID, err := ulid.ParseStrict(req.GetCharacterId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid character_id: %v", err)
	}

	subjectID := access.CharacterSubject(req.GetSubjectId())
	char, err := s.svc.GetCharacter(ctx, subjectID, charID)
	if err != nil {
		return nil, mapWorldError(err)
	}

	return &worldv1.GetCharacterResponse{
		Character: characterToProto(char),
	}, nil
}

// ListCharactersAtLocation returns all characters at a location.
func (s *WorldServiceServer) ListCharactersAtLocation(ctx context.Context, req *worldv1.ListCharactersAtLocationRequest) (*worldv1.ListCharactersAtLocationResponse, error) {
	locID, err := ulid.ParseStrict(req.GetLocationId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid location_id: %v", err)
	}

	subjectID := access.CharacterSubject(req.GetSubjectId())
	chars, err := s.svc.ListCharactersAtLocation(ctx, subjectID, locID)
	if err != nil {
		return nil, mapWorldError(err)
	}

	protoChars := make([]*worldv1.CharacterInfo, len(chars))
	for i, c := range chars {
		protoChars[i] = characterToProto(c)
	}

	return &worldv1.ListCharactersAtLocationResponse{Characters: protoChars}, nil
}

// ListExits returns all exits from a location.
func (s *WorldServiceServer) ListExits(ctx context.Context, req *worldv1.ListExitsRequest) (*worldv1.ListExitsResponse, error) {
	locID, err := ulid.ParseStrict(req.GetLocationId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid location_id: %v", err)
	}

	subjectID := access.CharacterSubject(req.GetSubjectId())
	exits, err := s.svc.ListExits(ctx, subjectID, locID)
	if err != nil {
		return nil, mapWorldError(err)
	}

	protoExits := make([]*worldv1.ExitInfo, len(exits))
	for i, e := range exits {
		protoExits[i] = exitToProto(e)
	}

	return &worldv1.ListExitsResponse{Exits: protoExits}, nil
}

func locationToProto(loc *Location) *worldv1.LocationInfo {
	return &worldv1.LocationInfo{
		Id:          loc.ID.String(),
		Name:        loc.Name,
		Description: loc.Description,
		Type:        string(loc.Type),
		OwnerId:     loc.OwnerID.String(),
	}
}

func characterToProto(c *Character) *worldv1.CharacterInfo {
	return &worldv1.CharacterInfo{
		Id:          c.ID.String(),
		PlayerId:    c.PlayerID.String(),
		Name:        c.Name,
		Description: c.Description,
		LocationId:  c.LocationID.String(),
	}
}

func exitToProto(e *Exit) *worldv1.ExitInfo {
	return &worldv1.ExitInfo{
		Id:            e.ID.String(),
		Name:          e.Name,
		SourceId:      e.SourceID.String(),
		DestinationId: e.DestinationID.String(),
		Description:   e.Description,
	}
}

func mapWorldError(err error) error {
	code := oops.GetCode(err)
	switch {
	case code == "NOT_FOUND" || code == "LOCATION_NOT_FOUND" || code == "CHARACTER_NOT_FOUND":
		return status.Errorf(codes.NotFound, "%v", err)
	case code == "ACCESS_DENIED":
		return status.Errorf(codes.PermissionDenied, "%v", err)
	default:
		return status.Errorf(codes.Internal, "%v", err)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestWorldServiceServer ./internal/world/`

Expected: PASS

- [ ] **Step 5: Add tests for GetCharacter and ListCharactersAtLocation**

Write table-driven tests covering: successful lookup, not found, and invalid ULID for each RPC. Follow the pattern from Step 1 using mock repositories.

- [ ] **Step 6: Run all world tests**

Run: `task test -- ./internal/world/`

Expected: All PASS

- [ ] **Step 7: Commit**

```bash
JJ_EDITOR=true jj --no-pager commit -m "feat(world): gRPC server adapter wrapping world.Service for service registry"
```

---

## Task 3: Register WorldService in Service Registry

Wire the WorldService into the plugin subsystem startup so it's available for DAG resolution and plugin consumption.

**Files:**

- Modify: `internal/plugin/setup/subsystem.go`

- [ ] **Step 1: Write failing integration test**

Create a test that verifies after `PluginSubsystem.Start()`, the service registry contains `holomush.world.v1.WorldService`.

```go
func TestPluginSubsystemRegistersWorldService(t *testing.T) {
	// ... (setup subsystem with minimal config, call Start)
	reg := subsystem.ServiceRegistry()
	svc, err := reg.Resolve("holomush.world.v1.WorldService")
	require.NoError(t, err)
	assert.True(t, svc.IsServerInternal())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestPluginSubsystemRegistersWorldService ./internal/plugin/setup/`

Expected: FAIL — service not registered.

- [ ] **Step 3: Add WorldService registration to subsystem.Start**

In `internal/plugin/setup/subsystem.go`, after creating the service registry (step 4 in Start), add:

```go
// Register WorldService as server-internal service.
worldGRPCSrv := grpc.NewServer() //nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection
worldv1.RegisterWorldServiceServer(worldGRPCSrv, world.NewWorldServiceServer(s.cfg.World.Service()))
worldConn, err := plugins.NewInProcessConn(worldGRPCSrv)
if err != nil {
    return oops.Code("WORLD_SERVICE_REGISTRATION_FAILED").Wrap(err)
}
if regErr := s.registry.Register(plugins.RegisteredService{
    Name:       "holomush.world.v1.WorldService",
    Conn:       worldConn,
    PluginType: plugins.TypeServerInternal(),
}); regErr != nil {
    _ = worldConn.Close()
    return oops.Code("WORLD_SERVICE_REGISTRATION_FAILED").Wrap(regErr)
}
```

Note: `TypeServerInternal()` is unexported (`typeServerInternal`). Export it as a function or use the constant directly since `setup` is inside the same module. The exact approach depends on the access pattern — if `typeServerInternal` is in the `plugins` package and `setup` imports `plugins`, use the package-level constant directly. Since `typeServerInternal` is a `const` (unexported), add an exported helper:

```go
// In internal/plugin/registered_service.go:
// TypeServerInternal returns the Type value used for server-internal services.
func TypeServerInternal() Type { return typeServerInternal }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestPluginSubsystemRegistersWorldService ./internal/plugin/setup/`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager commit -m "feat(plugin): register WorldService in service registry during startup"
```

---

## Task 4: Schema Provisioner for Binary Plugins

When a binary plugin declares `storage: postgres`, the plugin manager must create a schema-isolated Postgres environment before loading the plugin.

**Files:**

- Create: `internal/plugin/schema_provisioner.go`
- Create: `internal/plugin/schema_provisioner_test.go`

- [ ] **Step 1: Write failing test for CreatePluginSchema**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
)

func TestSchemaProvisionerCreatesSchemaAndReturnsConnString(t *testing.T) {
	// This is a unit test using a mock pool.
	// Integration tests will verify real Postgres behavior.
	provisioner := plugins.NewSchemaProvisioner("postgres://localhost:5432/holomush?sslmode=disable")

	connStr, err := provisioner.ProvisionSchema(context.Background(), "core-scenes")
	require.NoError(t, err)
	assert.Contains(t, connStr, "search_path=plugin_core_scenes")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestSchemaProvisionerCreatesSchemaAndReturnsConnString ./internal/plugin/`

Expected: Compilation error — `SchemaProvisioner` not defined.

- [ ] **Step 3: Implement SchemaProvisioner**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"
)

// SchemaProvisioner creates schema-isolated Postgres environments for binary plugins.
type SchemaProvisioner struct {
	baseConnString string
	pool           *pgxpool.Pool
}

// NewSchemaProvisioner creates a provisioner that uses baseConnString to connect
// and create per-plugin schemas. Call Init() to open the connection pool.
func NewSchemaProvisioner(baseConnString string) *SchemaProvisioner {
	return &SchemaProvisioner{baseConnString: baseConnString}
}

// Init opens the connection pool. Must be called before ProvisionSchema.
func (p *SchemaProvisioner) Init(ctx context.Context) error {
	pool, err := pgxpool.New(ctx, p.baseConnString)
	if err != nil {
		return oops.Code("SCHEMA_PROVISIONER_INIT_FAILED").Wrap(err)
	}
	p.pool = pool
	return nil
}

// Close shuts down the connection pool.
func (p *SchemaProvisioner) Close() {
	if p.pool != nil {
		p.pool.Close()
	}
}

// ProvisionSchema creates a schema for the named plugin and returns a connection
// string scoped to that schema via search_path.
// Schema name: plugin_<sanitized_name> (hyphens → underscores).
func (p *SchemaProvisioner) ProvisionSchema(ctx context.Context, pluginName string) (string, error) {
	schemaName := pluginSchemaName(pluginName)

	// CREATE SCHEMA IF NOT EXISTS — idempotent.
	// Schema names are derived from validated plugin names (alphanumeric + hyphens),
	// so SQL injection is not possible. Using fmt.Sprintf because pgx does not
	// support parameterized DDL.
	ddl := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", schemaName)
	if _, err := p.pool.Exec(ctx, ddl); err != nil {
		return "", oops.Code("SCHEMA_CREATE_FAILED").With("plugin", pluginName).With("schema", schemaName).Wrap(err)
	}

	// Build connection string with search_path pointing to the plugin schema.
	u, err := url.Parse(p.baseConnString)
	if err != nil {
		return "", oops.Code("SCHEMA_CONNSTRING_PARSE_FAILED").Wrap(err)
	}
	q := u.Query()
	q.Set("search_path", schemaName)
	u.RawQuery = q.Encode()

	return u.String(), nil
}

// pluginSchemaName converts a plugin name to a valid Postgres schema name.
// "core-scenes" → "plugin_core_scenes"
func pluginSchemaName(name string) string {
	return "plugin_" + strings.ReplaceAll(name, "-", "_")
}
```

- [ ] **Step 4: Run test to verify it passes**

For the unit test that just tests `pluginSchemaName` and conn string building, split the test:

```go
func TestPluginSchemaNameConvertsHyphensToUnderscores(t *testing.T) {
	assert.Equal(t, "plugin_core_scenes", pluginSchemaName("core-scenes"))
	assert.Equal(t, "plugin_dice", pluginSchemaName("dice"))
}
```

Run: `task test -- -run TestPluginSchemaName ./internal/plugin/`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager commit -m "feat(plugin): schema provisioner for binary plugin Postgres isolation"
```

---

## Task 5: Service Injection in Plugin SDK

Extend the plugin SDK so binary plugins can both receive required services (as gRPC connections) and register their own gRPC services on the go-plugin transport.

**Files:**

- Create: `pkg/plugin/service.go`
- Create: `pkg/plugin/service_test.go`
- Modify: `api/proto/holomush/plugin/v1/plugin.proto`

- [ ] **Step 1: Add ServiceConfig to plugin.proto handshake**

Add to `api/proto/holomush/plugin/v1/plugin.proto`:

```protobuf
// ServiceConfig is passed from host to plugin during initialization.
// It provides the plugin with connection strings for required services
// and storage configuration.
message ServiceConfig {
  // connection_string is the schema-isolated Postgres connection string.
  // Only set when the plugin declares storage: postgres.
  string connection_string = 1;

  // required_services maps proto service names to gRPC endpoint addresses.
  // The plugin can connect to these to consume required services.
  // Key: fully qualified service name (e.g., "holomush.world.v1.WorldService")
  // Value: gRPC address (typically a Unix socket or bufconn address)
  map<string, string> required_services = 2;
}
```

Add an `Init` RPC to `PluginService`:

```protobuf
service PluginService {
  // Init configures the plugin with services and storage after startup.
  rpc Init(InitRequest) returns (InitResponse);
  rpc HandleEvent(HandleEventRequest) returns (HandleEventResponse);
  rpc HandleCommand(HandleCommandRequest) returns (HandleCommandResponse);
}

message InitRequest {
  ServiceConfig config = 1;
}

message InitResponse {
  // provided_services lists the proto service names this plugin provides.
  // The host uses these to register in the service registry.
  repeated string provided_services = 1;
}
```

- [ ] **Step 2: Regenerate Go code**

Run: `task proto`

Expected: Updated `pkg/proto/holomush/plugin/v1/plugin.pb.go` and `plugin_grpc.pb.go`.

- [ ] **Step 3: Write failing test for ServiceProvider interface**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

func TestServiceProviderInterfaceExists(t *testing.T) {
	// Compile-time check that ServiceProvider is defined.
	var _ pluginsdk.ServiceProvider = (*mockServiceProvider)(nil)
	assert.True(t, true)
}

type mockServiceProvider struct{}

func (m *mockServiceProvider) RegisterServices(registrar grpc.ServiceRegistrar) {}
func (m *mockServiceProvider) Init(ctx context.Context, config *pluginv1.ServiceConfig) error { return nil }
```

- [ ] **Step 4: Run test to verify it fails**

Run: `task test -- -run TestServiceProviderInterfaceExists ./pkg/plugin/`

Expected: Compilation error — `ServiceProvider` not defined.

- [ ] **Step 5: Implement ServiceProvider and ServeWithServices**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"

	hashiplug "github.com/hashicorp/go-plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
	"google.golang.org/grpc"
)

// ServiceProvider is implemented by binary plugins that provide gRPC services
// and/or need initialization with service configuration.
type ServiceProvider interface {
	// RegisterServices registers the plugin's gRPC service implementations
	// on the go-plugin transport. Called during plugin startup.
	RegisterServices(registrar grpc.ServiceRegistrar)

	// Init is called after the go-plugin connection is established, providing
	// the plugin with its service configuration (Postgres connection string,
	// required service connections).
	Init(ctx context.Context, config *pluginv1.ServiceConfig) error
}

// ServeWithServices starts the plugin server with service support.
// Use this instead of Serve when the plugin provides or consumes gRPC services.
func ServeWithServices(config *ServeConfig, provider ServiceProvider) {
	if config == nil {
		panic("plugin: config cannot be nil")
	}
	if config.Handler == nil {
		panic("plugin: config.Handler cannot be nil")
	}
	if provider == nil {
		panic("plugin: provider cannot be nil")
	}

	hashiplug.Serve(&hashiplug.ServeConfig{
		HandshakeConfig: HandshakeConfig,
		Plugins: map[string]hashiplug.Plugin{
			"plugin": &grpcServicePlugin{
				handler:  config.Handler,
				provider: provider,
			},
		},
		GRPCServer: hashiplug.DefaultGRPCServer,
	})
}

// grpcServicePlugin extends grpcPlugin to support service registration.
type grpcServicePlugin struct {
	hashiplug.NetRPCUnsupportedPlugin
	handler  Handler
	provider ServiceProvider
}

// GRPCServer registers the plugin's services on the gRPC server.
func (p *grpcServicePlugin) GRPCServer(_ *hashiplug.GRPCBroker, s *grpc.Server) error {
	// Register the standard PluginService for events and commands.
	adapter := &pluginServerAdapter{handler: p.handler}
	if ch, ok := p.handler.(CommandHandler); ok {
		adapter.cmdHandler = ch
	}
	// Set the service provider on the adapter so Init() can delegate.
	adapter.serviceProvider = p.provider
	pluginv1.RegisterPluginServiceServer(s, adapter)

	// Register the plugin's own gRPC services.
	p.provider.RegisterServices(s)

	return nil
}

// GRPCClient is not called on the plugin side.
func (p *grpcServicePlugin) GRPCClient(_ context.Context, _ *hashiplug.GRPCBroker, _ *grpc.ClientConn) (interface{}, error) {
	return nil, errors.New("plugin: GRPCClient not implemented on plugin side")
}
```

Then update `pluginServerAdapter` in `sdk.go` to handle Init:

```go
// Add to pluginServerAdapter struct:
serviceProvider ServiceProvider // nil if handler does not provide services

// Add Init method:
func (a *pluginServerAdapter) Init(ctx context.Context, req *pluginv1.InitRequest) (*pluginv1.InitResponse, error) {
	if a.serviceProvider == nil {
		return &pluginv1.InitResponse{}, nil
	}
	if err := a.serviceProvider.Init(ctx, req.GetConfig()); err != nil {
		return nil, oops.Wrap(err)
	}
	return &pluginv1.InitResponse{}, nil
}
```

- [ ] **Step 6: Run tests**

Run: `task test -- ./pkg/plugin/`

Expected: All PASS

- [ ] **Step 7: Commit**

```bash
JJ_EDITOR=true jj --no-pager commit -m "feat(sdk): ServiceProvider interface for binary plugins with service injection"
```

---

## Task 6: Host-Side Service Injection

Update the goplugin host to call `Init()` on loaded binary plugins, passing required service addresses and connection strings.

**Files:**

- Modify: `internal/plugin/goplugin/host.go`
- Modify: `internal/plugin/goplugin/plugin.go`

- [ ] **Step 1: Write failing test**

```go
func TestHostCallsInitWithServiceConfig(t *testing.T) {
	// Create a mock plugin that expects Init to be called.
	// Use a mock factory that returns a client whose plugin responds to Init.
	factory := &mockFactory{t: t, expectInit: true}
	host := goplugin.NewHostWithFactory(factory)
	t.Cleanup(func() { _ = host.Close(context.Background()) })

	manifest := &plugins.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeBinary,
		BinaryPlugin: &plugins.BinaryConfig{
			Executable: "test-plugin",
		},
		Requires: []string{"holomush.world.v1.WorldService"},
		Storage:  plugins.StoragePostgres,
	}

	// Host needs a registry and schema provisioner to inject services.
	// ... (configure host with service registry containing WorldService)

	err := host.Load(context.Background(), manifest, t.TempDir())
	require.NoError(t, err)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestHostCallsInitWithServiceConfig ./internal/plugin/goplugin/`

Expected: FAIL — host does not call Init.

- [ ] **Step 3: Update Host.Load to call Init after connection**

In `internal/plugin/goplugin/host.go`, after successfully dispensing the PluginServiceClient, add Init call:

```go
// After line: h.plugins[manifest.Name] = &loadedPlugin{...}
// But before returning nil:

// Call Init if the plugin has requires or storage declarations.
if len(manifest.Requires) > 0 || manifest.Storage == plugins.StoragePostgres {
    initReq := &pluginv1.InitRequest{
        Config: &pluginv1.ServiceConfig{},
    }

    // Populate connection string if storage: postgres.
    if manifest.Storage == plugins.StoragePostgres && h.schemaProvisioner != nil {
        connStr, provErr := h.schemaProvisioner.ProvisionSchema(ctx, manifest.Name)
        if provErr != nil {
            client.Kill()
            return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "provision_schema").Wrap(provErr)
        }
        initReq.Config.ConnectionString = connStr
    }

    // Populate required service addresses.
    // For in-process services, the plugin connects back through the go-plugin
    // broker. For now, required services are resolved from the registry and
    // their addresses are not needed — the plugin calls back through
    // PluginHostService which delegates to the registry.
    // TODO(phase3): Direct gRPC injection requires go-plugin broker multiplexing.
    // For Phase 3, we use the existing PluginHostService callback pattern.

    if _, initErr := svcClient.Init(ctx, initReq); initErr != nil {
        client.Kill()
        return oops.In("goplugin").With("plugin", manifest.Name).With("operation", "init").Wrap(initErr)
    }
}
```

Add `schemaProvisioner` field to `Host` struct and a `WithSchemaProvisioner` option.

- [ ] **Step 4: Update Host to accept SchemaProvisioner**

```go
// Add to Host struct:
schemaProvisioner *plugins.SchemaProvisioner

// Add option:
func WithSchemaProvisioner(p *plugins.SchemaProvisioner) HostOption {
    return func(h *Host) {
        h.schemaProvisioner = p
    }
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `task test -- -run TestHostCallsInitWithServiceConfig ./internal/plugin/goplugin/`

Expected: PASS

- [ ] **Step 6: Commit**

```bash
JJ_EDITOR=true jj --no-pager commit -m "feat(goplugin): host calls Init on binary plugins with service config"
```

---

## Task 7: Scene Plugin Migrations

Create the SQL schema for the scene plugin's Postgres tables.

**Files:**

- Create: `plugins/core-scenes/migrations/000001_scenes.up.sql`
- Create: `plugins/core-scenes/migrations/000001_scenes.down.sql`

- [ ] **Step 1: Write the up migration**

```sql
-- Scene plugin schema (runs within plugin_core_scenes schema)
CREATE TABLE IF NOT EXISTS scenes (
    id          TEXT PRIMARY KEY,
    title       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    location_id TEXT,
    owner_id    TEXT NOT NULL,
    state       TEXT NOT NULL DEFAULT 'active',
    pose_order  TEXT NOT NULL DEFAULT 'free',
    visibility  TEXT NOT NULL DEFAULT 'open',
    idle_timeout_secs INTEGER,
    template_id TEXT,
    content_warnings TEXT[] NOT NULL DEFAULT '{}',
    tags         TEXT[] NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at    TIMESTAMPTZ,
    archived_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS scene_participants (
    scene_id     TEXT NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,
    character_id TEXT NOT NULL,
    role         TEXT NOT NULL DEFAULT 'member',
    origin_location_id TEXT,
    joined_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    publish_vote BOOLEAN,
    PRIMARY KEY (scene_id, character_id)
);

CREATE TABLE IF NOT EXISTS scene_templates (
    id          TEXT PRIMARY KEY,
    owner_id    TEXT NOT NULL,
    title       TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    location_id TEXT,
    pose_order  TEXT NOT NULL DEFAULT 'free',
    content_warnings TEXT[] NOT NULL DEFAULT '{}',
    tags         TEXT[] NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS scene_logs (
    id           TEXT PRIMARY KEY,
    scene_id     TEXT NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,
    title        TEXT NOT NULL,
    content      TEXT NOT NULL,
    participants TEXT[] NOT NULL DEFAULT '{}',
    published_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_scenes_state ON scenes(state);
CREATE INDEX IF NOT EXISTS idx_scenes_location ON scenes(location_id) WHERE location_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_scene_participants_character ON scene_participants(character_id);
CREATE INDEX IF NOT EXISTS idx_scene_logs_scene ON scene_logs(scene_id);
```

- [ ] **Step 2: Write the down migration**

```sql
DROP INDEX IF EXISTS idx_scene_logs_scene;
DROP INDEX IF EXISTS idx_scene_participants_character;
DROP INDEX IF EXISTS idx_scenes_location;
DROP INDEX IF EXISTS idx_scenes_state;
DROP TABLE IF EXISTS scene_logs;
DROP TABLE IF EXISTS scene_templates;
DROP TABLE IF EXISTS scene_participants;
DROP TABLE IF EXISTS scenes;
```

- [ ] **Step 3: Commit**

```bash
JJ_EDITOR=true jj --no-pager commit -m "feat(scenes): SQL migrations for scene plugin Postgres schema"
```

---

## Task 8: Scene Plugin PostgreSQL Store

Implement `SceneRepository` using the plugin storage SDK and pgxpool.

**Files:**

- Create: `plugins/core-scenes/store.go`
- Create: `plugins/core-scenes/store_test.go`

- [ ] **Step 1: Write failing test for Create and Get**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSceneStoreCreateAndGet(t *testing.T) {
	// This test requires a real Postgres instance (testcontainers).
	// Skip in unit test mode.
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	store := setupTestStore(t) // helper that creates a testcontainer + runs migrations

	scene := &SceneRow{
		ID:          ulid.MustNew(1, nil).String(),
		Title:       "A Decades-Crossed Meeting",
		Description: "Two old friends meet at the crossroads.",
		OwnerID:     ulid.MustNew(2, nil).String(),
		State:       "active",
		PoseOrder:   "free",
		Visibility:  "open",
		CreatedAt:   time.Now(),
	}

	err := store.CreateScene(ctx, scene)
	require.NoError(t, err)

	got, err := store.GetScene(ctx, scene.ID)
	require.NoError(t, err)
	assert.Equal(t, scene.Title, got.Title)
	assert.Equal(t, scene.OwnerID, got.OwnerID)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestSceneStoreCreateAndGet ./plugins/core-scenes/`

Expected: Compilation error — types not defined.

- [ ] **Step 3: Implement SceneStore**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"embed"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"

	"github.com/holomush/holomush/pkg/plugin/storage"
)

//go:embed migrations/*.up.sql
var migrations embed.FS

// SceneRow represents a scene record in the database.
type SceneRow struct {
	ID              string
	Title           string
	Description     string
	LocationID      *string
	OwnerID         string
	State           string
	PoseOrder       string
	Visibility      string
	IdleTimeoutSecs *int
	TemplateID      *string
	ContentWarnings []string
	Tags            []string
	CreatedAt       time.Time
	EndedAt         *time.Time
	ArchivedAt      *time.Time
}

// ParticipantRow represents a scene participant record.
type ParticipantRow struct {
	SceneID          string
	CharacterID      string
	Role             string
	OriginLocationID *string
	JoinedAt         time.Time
	PublishVote      *bool
}

// SceneStore provides PostgreSQL persistence for scenes.
type SceneStore struct {
	pool *pgxpool.Pool
}

// NewSceneStore creates a store connected to the plugin's schema-isolated database.
func NewSceneStore(ctx context.Context, connString string) (*SceneStore, error) {
	pool, err := storage.Connect(ctx, connString)
	if err != nil {
		return nil, err
	}
	if err := storage.RunMigrations(ctx, pool, migrations); err != nil {
		pool.Close()
		return nil, err
	}
	return &SceneStore{pool: pool}, nil
}

// Close shuts down the connection pool.
func (s *SceneStore) Close() { s.pool.Close() }

// CreateScene inserts a new scene.
func (s *SceneStore) CreateScene(ctx context.Context, scene *SceneRow) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO scenes (id, title, description, location_id, owner_id, state,
			pose_order, visibility, idle_timeout_secs, template_id,
			content_warnings, tags, created_at, ended_at, archived_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		scene.ID, scene.Title, scene.Description, scene.LocationID, scene.OwnerID,
		scene.State, scene.PoseOrder, scene.Visibility, scene.IdleTimeoutSecs,
		scene.TemplateID, scene.ContentWarnings, scene.Tags,
		scene.CreatedAt, scene.EndedAt, scene.ArchivedAt,
	)
	if err != nil {
		return oops.Code("SCENE_CREATE_FAILED").Wrap(err)
	}
	return nil
}

// GetScene retrieves a scene by ID.
func (s *SceneStore) GetScene(ctx context.Context, id string) (*SceneRow, error) {
	row := &SceneRow{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, title, description, location_id, owner_id, state,
			pose_order, visibility, idle_timeout_secs, template_id,
			content_warnings, tags, created_at, ended_at, archived_at
		FROM scenes WHERE id = $1`, id,
	).Scan(
		&row.ID, &row.Title, &row.Description, &row.LocationID, &row.OwnerID,
		&row.State, &row.PoseOrder, &row.Visibility, &row.IdleTimeoutSecs,
		&row.TemplateID, &row.ContentWarnings, &row.Tags,
		&row.CreatedAt, &row.EndedAt, &row.ArchivedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, oops.Code("SCENE_NOT_FOUND").Errorf("scene %s not found", id)
		}
		return nil, oops.Code("SCENE_GET_FAILED").Wrap(err)
	}
	return row, nil
}

// UpdateScene updates an existing scene.
func (s *SceneStore) UpdateScene(ctx context.Context, scene *SceneRow) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE scenes SET title=$2, description=$3, location_id=$4, owner_id=$5,
			state=$6, pose_order=$7, visibility=$8, idle_timeout_secs=$9,
			template_id=$10, content_warnings=$11, tags=$12,
			ended_at=$13, archived_at=$14
		WHERE id=$1`,
		scene.ID, scene.Title, scene.Description, scene.LocationID, scene.OwnerID,
		scene.State, scene.PoseOrder, scene.Visibility, scene.IdleTimeoutSecs,
		scene.TemplateID, scene.ContentWarnings, scene.Tags,
		scene.EndedAt, scene.ArchivedAt,
	)
	if err != nil {
		return oops.Code("SCENE_UPDATE_FAILED").Wrap(err)
	}
	return nil
}

// ListScenes returns scenes matching optional filters.
func (s *SceneStore) ListScenes(ctx context.Context, state, visibility *string, limit, offset int) ([]*SceneRow, error) {
	query := "SELECT id, title, description, location_id, owner_id, state, pose_order, visibility, idle_timeout_secs, template_id, content_warnings, tags, created_at, ended_at, archived_at FROM scenes WHERE 1=1"
	args := []any{}
	argIdx := 1

	if state != nil {
		query += fmt.Sprintf(" AND state = $%d", argIdx)
		args = append(args, *state)
		argIdx++
	}
	if visibility != nil {
		query += fmt.Sprintf(" AND visibility = $%d", argIdx)
		args = append(args, *visibility)
		argIdx++
	}

	if limit <= 0 {
		limit = 50
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, oops.Code("SCENE_LIST_FAILED").Wrap(err)
	}
	defer rows.Close()

	var scenes []*SceneRow
	for rows.Next() {
		row := &SceneRow{}
		if err := rows.Scan(
			&row.ID, &row.Title, &row.Description, &row.LocationID, &row.OwnerID,
			&row.State, &row.PoseOrder, &row.Visibility, &row.IdleTimeoutSecs,
			&row.TemplateID, &row.ContentWarnings, &row.Tags,
			&row.CreatedAt, &row.EndedAt, &row.ArchivedAt,
		); err != nil {
			return nil, oops.Code("SCENE_LIST_FAILED").Wrap(err)
		}
		scenes = append(scenes, row)
	}
	return scenes, rows.Err()
}

// AddParticipant adds a character to a scene.
func (s *SceneStore) AddParticipant(ctx context.Context, p *ParticipantRow) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO scene_participants (scene_id, character_id, role, origin_location_id, joined_at, publish_vote)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (scene_id, character_id) DO UPDATE SET role = EXCLUDED.role, joined_at = EXCLUDED.joined_at`,
		p.SceneID, p.CharacterID, p.Role, p.OriginLocationID, p.JoinedAt, p.PublishVote,
	)
	if err != nil {
		return oops.Code("SCENE_ADD_PARTICIPANT_FAILED").Wrap(err)
	}
	return nil
}

// RemoveParticipant removes a character from a scene.
func (s *SceneStore) RemoveParticipant(ctx context.Context, sceneID, characterID string) error {
	tag, err := s.pool.Exec(ctx,
		"DELETE FROM scene_participants WHERE scene_id = $1 AND character_id = $2",
		sceneID, characterID,
	)
	if err != nil {
		return oops.Code("SCENE_REMOVE_PARTICIPANT_FAILED").Wrap(err)
	}
	if tag.RowsAffected() == 0 {
		return oops.Code("SCENE_NOT_FOUND").Errorf("participant %s not in scene %s", characterID, sceneID)
	}
	return nil
}

// ListParticipants returns all participants in a scene.
func (s *SceneStore) ListParticipants(ctx context.Context, sceneID string) ([]*ParticipantRow, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT scene_id, character_id, role, origin_location_id, joined_at, publish_vote FROM scene_participants WHERE scene_id = $1 ORDER BY joined_at",
		sceneID,
	)
	if err != nil {
		return nil, oops.Code("SCENE_LIST_PARTICIPANTS_FAILED").Wrap(err)
	}
	defer rows.Close()

	var participants []*ParticipantRow
	for rows.Next() {
		p := &ParticipantRow{}
		if err := rows.Scan(&p.SceneID, &p.CharacterID, &p.Role, &p.OriginLocationID, &p.JoinedAt, &p.PublishVote); err != nil {
			return nil, oops.Code("SCENE_LIST_PARTICIPANTS_FAILED").Wrap(err)
		}
		participants = append(participants, p)
	}
	return participants, rows.Err()
}
```

- [ ] **Step 4: Run tests**

Run: `task test -- -short ./plugins/core-scenes/` (unit tests only)

For integration: `task test:int -- -run TestSceneStore ./plugins/core-scenes/`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager commit -m "feat(scenes): PostgreSQL store implementation for scene plugin"
```

---

## Task 9: Scene Plugin gRPC Service

Implement `holomush.scene.v1.SceneService` in the plugin binary, backed by the SceneStore.

**Files:**

- Create: `plugins/core-scenes/service.go`
- Create: `plugins/core-scenes/service_test.go`

- [ ] **Step 1: Write failing test for CreateScene RPC**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

func TestSceneServiceCreateSceneReturnsSceneInfo(t *testing.T) {
	store := newMockSceneStore(t)
	svc := NewSceneServiceImpl(store)

	resp, err := svc.CreateScene(context.Background(), &scenev1.CreateSceneRequest{
		SessionId:   "session-1",
		Title:       "A Decades-Crossed Meeting",
		Description: "Two old friends meet.",
		Visibility:  "open",
	})
	require.NoError(t, err)
	assert.Equal(t, "A Decades-Crossed Meeting", resp.GetScene().GetTitle())
	assert.NotEmpty(t, resp.GetScene().GetId())
	assert.Equal(t, "active", resp.GetScene().GetState())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestSceneServiceCreateScene ./plugins/core-scenes/`

Expected: Compilation error — `SceneServiceImpl` not defined.

- [ ] **Step 3: Implement SceneServiceImpl**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/idgen"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// Compile-time interface check.
var _ scenev1.SceneServiceServer = (*SceneServiceImpl)(nil)

// SceneServiceImpl implements the SceneService gRPC contract.
type SceneServiceImpl struct {
	scenev1.UnimplementedSceneServiceServer
	store *SceneStore
}

// NewSceneServiceImpl creates a new SceneServiceImpl.
func NewSceneServiceImpl(store *SceneStore) *SceneServiceImpl {
	return &SceneServiceImpl{store: store}
}

// CreateScene creates a new scene.
func (s *SceneServiceImpl) CreateScene(ctx context.Context, req *scenev1.CreateSceneRequest) (*scenev1.CreateSceneResponse, error) {
	now := time.Now()
	sceneID := idgen.New().String()

	visibility := req.GetVisibility()
	if visibility == "" {
		visibility = "open"
	}
	poseOrder := req.GetPoseOrderMode()
	if poseOrder == "" {
		poseOrder = "free"
	}

	scene := &SceneRow{
		ID:              sceneID,
		Title:           req.GetTitle(),
		Description:     req.GetDescription(),
		LocationID:      nilIfEmpty(req.GetLocationId()),
		OwnerID:         req.GetSessionId(), // session → character mapping is caller's responsibility
		State:           "active",
		PoseOrder:       poseOrder,
		Visibility:      visibility,
		ContentWarnings: req.GetContentWarnings(),
		Tags:            req.GetTags(),
		CreatedAt:       now,
	}

	if scene.Title == "" {
		return nil, status.Error(codes.InvalidArgument, "title is required")
	}
	if scene.ContentWarnings == nil {
		scene.ContentWarnings = []string{}
	}
	if scene.Tags == nil {
		scene.Tags = []string{}
	}

	if err := s.store.CreateScene(ctx, scene); err != nil {
		return nil, status.Errorf(codes.Internal, "create scene: %v", err)
	}

	return &scenev1.CreateSceneResponse{
		Scene: sceneRowToProto(scene, nil),
	}, nil
}

// GetScene retrieves a scene by ID.
func (s *SceneServiceImpl) GetScene(ctx context.Context, req *scenev1.GetSceneRequest) (*scenev1.GetSceneResponse, error) {
	scene, err := s.store.GetScene(ctx, req.GetSceneId())
	if err != nil {
		if oops.GetCode(err) == "SCENE_NOT_FOUND" {
			return nil, status.Errorf(codes.NotFound, "scene not found")
		}
		return nil, status.Errorf(codes.Internal, "get scene: %v", err)
	}

	participants, err := s.store.ListParticipants(ctx, req.GetSceneId())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list participants: %v", err)
	}

	return &scenev1.GetSceneResponse{
		Scene: sceneRowToProto(scene, participants),
	}, nil
}

// ListScenes returns open scenes.
func (s *SceneServiceImpl) ListScenes(ctx context.Context, req *scenev1.ListScenesRequest) (*scenev1.ListScenesResponse, error) {
	open := "open"
	scenes, err := s.store.ListScenes(ctx, nil, &open, int(req.GetLimit()), int(req.GetOffset()))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list scenes: %v", err)
	}

	protoScenes := make([]*scenev1.SceneInfo, len(scenes))
	for i, scene := range scenes {
		protoScenes[i] = sceneRowToProto(scene, nil)
	}

	return &scenev1.ListScenesResponse{Scenes: protoScenes}, nil
}

// EndScene transitions scene to ended.
func (s *SceneServiceImpl) EndScene(ctx context.Context, req *scenev1.EndSceneRequest) (*scenev1.EndSceneResponse, error) {
	scene, err := s.store.GetScene(ctx, req.GetSceneId())
	if err != nil {
		if oops.GetCode(err) == "SCENE_NOT_FOUND" {
			return nil, status.Errorf(codes.NotFound, "scene not found")
		}
		return nil, status.Errorf(codes.Internal, "end scene: %v", err)
	}

	if scene.State != "active" && scene.State != "paused" {
		return nil, status.Errorf(codes.FailedPrecondition, "scene is not active or paused")
	}

	now := time.Now()
	scene.State = "ended"
	scene.EndedAt = &now

	if err := s.store.UpdateScene(ctx, scene); err != nil {
		return nil, status.Errorf(codes.Internal, "end scene: %v", err)
	}

	return &scenev1.EndSceneResponse{}, nil
}

// JoinScene adds a character as a member.
func (s *SceneServiceImpl) JoinScene(ctx context.Context, req *scenev1.JoinSceneRequest) (*scenev1.JoinSceneResponse, error) {
	scene, err := s.store.GetScene(ctx, req.GetSceneId())
	if err != nil {
		if oops.GetCode(err) == "SCENE_NOT_FOUND" {
			return nil, status.Errorf(codes.NotFound, "scene not found")
		}
		return nil, status.Errorf(codes.Internal, "join scene: %v", err)
	}

	if scene.State != "active" && scene.State != "paused" {
		return nil, status.Errorf(codes.FailedPrecondition, "scene is not active or paused")
	}

	if scene.Visibility == "private" {
		return nil, status.Errorf(codes.PermissionDenied, "scene is private — invite required")
	}

	participant := &ParticipantRow{
		SceneID:     req.GetSceneId(),
		CharacterID: req.GetSessionId(),
		Role:        "member",
		JoinedAt:    time.Now(),
	}

	if err := s.store.AddParticipant(ctx, participant); err != nil {
		return nil, status.Errorf(codes.Internal, "join scene: %v", err)
	}

	return &scenev1.JoinSceneResponse{}, nil
}

// LeaveScene removes a character from a scene.
func (s *SceneServiceImpl) LeaveScene(ctx context.Context, req *scenev1.LeaveSceneRequest) (*scenev1.LeaveSceneResponse, error) {
	if err := s.store.RemoveParticipant(ctx, req.GetSceneId(), req.GetSessionId()); err != nil {
		if oops.GetCode(err) == "SCENE_NOT_FOUND" {
			return nil, status.Errorf(codes.NotFound, "not a member of this scene")
		}
		return nil, status.Errorf(codes.Internal, "leave scene: %v", err)
	}

	return &scenev1.LeaveSceneResponse{}, nil
}

// InviteToScene adds a character as invited.
func (s *SceneServiceImpl) InviteToScene(ctx context.Context, req *scenev1.InviteToSceneRequest) (*scenev1.InviteToSceneResponse, error) {
	participant := &ParticipantRow{
		SceneID:     req.GetSceneId(),
		CharacterID: req.GetCharacterId(),
		Role:        "invited",
		JoinedAt:    time.Now(),
	}

	if err := s.store.AddParticipant(ctx, participant); err != nil {
		return nil, status.Errorf(codes.Internal, "invite to scene: %v", err)
	}

	return &scenev1.InviteToSceneResponse{}, nil
}

// CastPublishVote records a publish vote.
func (s *SceneServiceImpl) CastPublishVote(ctx context.Context, req *scenev1.CastPublishVoteRequest) (*scenev1.CastPublishVoteResponse, error) {
	// Update the participant's vote in the database.
	_, err := s.store.pool.Exec(ctx,
		"UPDATE scene_participants SET publish_vote = $1 WHERE scene_id = $2 AND character_id = $3",
		req.GetVote(), req.GetSceneId(), req.GetSessionId(),
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "cast vote: %v", err)
	}

	return &scenev1.CastPublishVoteResponse{}, nil
}

// GetPoseOrder returns the current pose order for a scene.
func (s *SceneServiceImpl) GetPoseOrder(ctx context.Context, req *scenev1.GetPoseOrderRequest) (*scenev1.GetPoseOrderResponse, error) {
	scene, err := s.store.GetScene(ctx, req.GetSceneId())
	if err != nil {
		if oops.GetCode(err) == "SCENE_NOT_FOUND" {
			return nil, status.Errorf(codes.NotFound, "scene not found")
		}
		return nil, status.Errorf(codes.Internal, "get pose order: %v", err)
	}

	return &scenev1.GetPoseOrderResponse{
		Mode:    scene.PoseOrder,
		Entries: []*scenev1.PoseOrderEntry{}, // Derived from event stream; placeholder for now
	}, nil
}

func sceneRowToProto(scene *SceneRow, participants []*ParticipantRow) *scenev1.SceneInfo {
	info := &scenev1.SceneInfo{
		Id:              scene.ID,
		Title:           scene.Title,
		Description:     scene.Description,
		OwnerId:         scene.OwnerID,
		State:           scene.State,
		PoseOrderMode:   scene.PoseOrder,
		ContentWarnings: scene.ContentWarnings,
		Tags:            scene.Tags,
		Visibility:      scene.Visibility,
		CreatedAt:       timestamppb.New(scene.CreatedAt),
	}
	if scene.LocationID != nil {
		info.LocationId = *scene.LocationID
	}
	if scene.EndedAt != nil {
		info.EndedAt = timestamppb.New(*scene.EndedAt)
	}
	if participants != nil {
		info.Participants = make([]*scenev1.ParticipantInfo, len(participants))
		for i, p := range participants {
			info.Participants[i] = &scenev1.ParticipantInfo{
				CharacterId: p.CharacterID,
				Role:        p.Role,
				JoinedAt:    timestamppb.New(p.JoinedAt),
			}
		}
	}
	return info
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
```

- [ ] **Step 4: Run tests**

Run: `task test -- -run TestSceneService ./plugins/core-scenes/`

Expected: PASS

- [ ] **Step 5: Add tests for GetScene, EndScene, JoinScene, LeaveScene**

Write test cases for each RPC covering: success path, not found, invalid state transitions.

- [ ] **Step 6: Run all service tests**

Run: `task test -- ./plugins/core-scenes/`

Expected: All PASS

- [ ] **Step 7: Commit**

```bash
JJ_EDITOR=true jj --no-pager commit -m "feat(scenes): SceneService gRPC implementation backed by SceneStore"
```

---

## Task 10: Scene Plugin Binary Entry Point

Wire together the plugin binary: implement `Handler`, `CommandHandler`, `ServiceProvider`, and `main()`.

**Files:**

- Create: `plugins/core-scenes/main.go`
- Create: `plugins/core-scenes/plugin.yaml`

- [ ] **Step 1: Create plugin.yaml manifest**

```yaml
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors

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

- [ ] **Step 2: Create main.go**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"log/slog"

	"google.golang.org/grpc"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// scenePlugin implements Handler, CommandHandler, and ServiceProvider.
type scenePlugin struct {
	store   *SceneStore
	service *SceneServiceImpl
}

// HandleEvent processes incoming events (scene plugins listen for session events).
func (p *scenePlugin) HandleEvent(_ context.Context, _ pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	// Scene plugin does not handle events in Phase 3.
	// Future: listen for session disconnect to update presence.
	return nil, nil
}

// HandleCommand processes scene commands.
func (p *scenePlugin) HandleCommand(ctx context.Context, req pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	// Command routing will be added in a follow-up task.
	// For Phase 3, commands go through the gRPC SceneService directly.
	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: "Scene commands are available via the SceneService gRPC API.",
	}, nil
}

// RegisterServices registers SceneService on the go-plugin gRPC transport.
func (p *scenePlugin) RegisterServices(registrar grpc.ServiceRegistrar) {
	scenev1.RegisterSceneServiceServer(registrar, p.service)
}

// Init receives the service configuration from the host.
func (p *scenePlugin) Init(ctx context.Context, config *pluginv1.ServiceConfig) error {
	connStr := config.GetConnectionString()
	if connStr == "" {
		return oops.Errorf("core-scenes requires storage: postgres but no connection string provided")
	}

	store, err := NewSceneStore(ctx, connStr)
	if err != nil {
		return oops.Wrap(err)
	}
	p.store = store
	p.service = NewSceneServiceImpl(store)

	slog.Info("core-scenes plugin initialized", "schema", connStr)
	return nil
}

func main() {
	plugin := &scenePlugin{}
	pluginsdk.ServeWithServices(
		&pluginsdk.ServeConfig{Handler: plugin},
		plugin,
	)
}
```

- [ ] **Step 3: Verify it compiles**

Run: `cd plugins/core-scenes && go build -o core-scenes . && echo "OK"`

Expected: Binary `core-scenes` produced.

- [ ] **Step 4: Commit**

```bash
JJ_EDITOR=true jj --no-pager commit -m "feat(scenes): binary plugin entry point with ServiceProvider wiring"
```

---

## Task 11: Install gRPC Service Proxy on Server

Wire the `GRPCServiceProxy` into the server's gRPC listener so plugin-provided services are auto-proxied.

**Files:**

- Modify: `cmd/holomush/sub_grpc.go`
- Modify: `internal/plugin/setup/subsystem.go`

- [ ] **Step 1: Write failing integration test**

A test that starts the server with a binary plugin providing SceneService, then calls SceneService through the server's gRPC port and gets a response.

- [ ] **Step 2: Add GRPCServiceProxy to gRPC server creation**

In `cmd/holomush/sub_grpc.go`, where `grpc.NewServer()` is called, add the proxy handler:

```go
// Get the service registry from the plugin subsystem.
serviceRegistry := s.cfg.Plugins.ServiceRegistry()
proxy := plugins.NewGRPCServiceProxy(serviceRegistry)

s.grpcServer = grpc.NewServer(
    grpc.Creds(creds),
    grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
        MinTime:             10 * time.Second,
        PermitWithoutStream: true,
    }),
    proxy.Handler(), // Auto-proxy plugin-provided services
)
```

- [ ] **Step 3: Wire schema provisioner in subsystem**

In `internal/plugin/setup/subsystem.go`, create the schema provisioner before creating the binary host:

```go
// Add to PluginSubsystemConfig:
DatabaseConnString string // base Postgres connection string

// In Start(), before creating binary host:
var schemaProvisioner *plugins.SchemaProvisioner
if s.cfg.DatabaseConnString != "" {
    schemaProvisioner = plugins.NewSchemaProvisioner(s.cfg.DatabaseConnString)
    if err := schemaProvisioner.Init(ctx); err != nil {
        return oops.Code("SCHEMA_PROVISIONER_INIT_FAILED").Wrap(err)
    }
}

// Pass to binary host:
binaryHost := goplugin.NewHost(goplugin.WithSchemaProvisioner(schemaProvisioner))
```

- [ ] **Step 4: Run integration tests**

Run: `task test:int`

Expected: PASS (existing tests still pass, new binary plugin path works)

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager commit -m "feat(server): install gRPC service proxy for auto-proxying plugin services"
```

---

## Task 12: End-to-End Verification

Verify the complete binary plugin path works end-to-end: server starts, discovers core-scenes plugin, provisions schema, loads plugin, proxies SceneService.

**Files:**

- Create: `test/integration/plugin/binary_plugin_test.go`

- [ ] **Step 1: Write E2E test**

```go
//go:build integration

package plugin_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

func TestBinaryPluginSceneServiceEndToEnd(t *testing.T) {
	// 1. Start a test server with the core-scenes binary plugin
	env := setupE2EEnv(t) // testcontainers Postgres + server + plugin binary

	// 2. Call CreateScene through the server's gRPC port
	client := scenev1.NewSceneServiceClient(env.grpcConn)
	resp, err := client.CreateScene(context.Background(), &scenev1.CreateSceneRequest{
		SessionId: env.testSessionID,
		Title:     "E2E Test Scene",
	})
	require.NoError(t, err)
	assert.Equal(t, "E2E Test Scene", resp.GetScene().GetTitle())
	assert.Equal(t, "active", resp.GetScene().GetState())

	// 3. Verify GetScene returns the created scene
	getResp, err := client.GetScene(context.Background(), &scenev1.GetSceneRequest{
		SessionId: env.testSessionID,
		SceneId:   resp.GetScene().GetId(),
	})
	require.NoError(t, err)
	assert.Equal(t, "E2E Test Scene", getResp.GetScene().GetTitle())

	// 4. Verify ListScenes includes the scene
	listResp, err := client.ListScenes(context.Background(), &scenev1.ListScenesRequest{
		Limit: 10,
	})
	require.NoError(t, err)
	assert.Len(t, listResp.GetScenes(), 1)
}
```

- [ ] **Step 2: Run E2E test**

Run: `task test:int -- -run TestBinaryPluginSceneServiceEndToEnd ./test/integration/plugin/`

Expected: PASS — full round-trip through server proxy → go-plugin → scene plugin → Postgres → response.

- [ ] **Step 3: Commit**

```bash
JJ_EDITOR=true jj --no-pager commit -m "test(e2e): binary plugin scene service end-to-end verification"
```

---

## Task 13: Run Full Test Suite

- [ ] **Step 1: Run pr-prep**

Run: `task pr-prep`

Expected: All checks pass (lint, fmt, schema, license, unit, integration, E2E).

- [ ] **Step 2: Fix any failures**

Address lint warnings, test failures, or formatting issues discovered by pr-prep.

- [ ] **Step 3: Commit fixes**

```bash
JJ_EDITOR=true jj --no-pager commit -m "fix: address pr-prep findings for Phase 3"
```

---

## Dependency Map

```text
Task 1 (WorldService proto)
  └→ Task 2 (WorldService gRPC adapter)
       └→ Task 3 (Register in service registry)
            └→ Task 11 (Server gRPC proxy wiring)

Task 4 (Schema provisioner)
  └→ Task 6 (Host service injection)
       └→ Task 11 (Server wiring)

Task 5 (SDK ServiceProvider)
  └→ Task 6 (Host service injection)
       └→ Task 10 (Plugin binary entry point)

Task 7 (Scene migrations)
  └→ Task 8 (Scene store)
       └→ Task 9 (Scene service)
            └→ Task 10 (Plugin binary entry point)

Task 10 + Task 11 → Task 12 (E2E) → Task 13 (Full suite)
```

**Parallelizable groups:**
- Tasks 1-3 (WorldService path) can run in parallel with Tasks 4-6 (injection path) and Tasks 7-9 (scene data path)
- Task 10 depends on 5, 7-9
- Task 11 depends on 3, 4, 6
- Task 12 depends on 10, 11
