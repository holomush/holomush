# Review Scope

## Target

PR #192 — Proto-first plugin architecture rework. 49 commits across 4 phases implementing a complete replacement of the plugin system. Working directory: `/Volumes/Code/github.com/holomush/holomush_worktrees/plugin-arch`.

Design spec: `docs/superpowers/specs/2026-04-05-plugin-architecture-rework-design.md`

## Phases

- **Phase 1**: Service registry, binary plugin host, storage SDK, DAG dependency resolution, gRPC proxy, InProcessConn
- **Phase 2**: Hostfunc capability module decomposition, 5 core plugins migrated from compiled-in Go to Lua, type:core removed
- **Phase 3**: WorldService proto + gRPC adapter, scene binary plugin, ServiceProvider SDK, schema provisioner, service auto-registration
- **Phase 4**: Dead code cleanup (~3000 lines), plugin admin commands, lint fixes

## Key Files (non-generated, non-deleted)

### New infrastructure (Phase 1)
- `internal/plugin/registry.go` + test — ServiceRegistry
- `internal/plugin/registered_service.go` + test — RegisteredService type
- `internal/plugin/dependency.go` + test — DAG resolution
- `internal/plugin/grpc_proxy.go` + test — GRPCServiceProxy
- `internal/plugin/inprocess_conn.go` + test — InProcessConn
- `pkg/plugin/storage/storage.go` + test — Plugin storage SDK
- `internal/plugin/host.go` — Host interface + ServiceConnProvider

### Hostfunc capability modules (Phase 2)
- `internal/plugin/hostfunc/capability.go` + test — CapabilityRegistry
- `internal/plugin/hostfunc/cap_alias.go` + test
- `internal/plugin/hostfunc/cap_property.go` + test
- `internal/plugin/hostfunc/cap_session.go` + test
- `internal/plugin/hostfunc/cap_world_query.go` + test
- `internal/plugin/hostfunc/errors.go` + test — Consolidated error sanitization
- `internal/plugin/hostfunc/adapter.go` — World query adapter
- `internal/plugin/hostfunc/world.go` + test — World hostfuncs
- `internal/plugin/hostfunc/world_write.go` — World mutation hostfuncs

### Lua plugin rewrites (Phase 2)
- `plugins/core-communication/main.lua` + plugin.yaml
- `plugins/core-building/main.lua` + plugin.yaml
- `plugins/core-objects/main.lua` + plugin.yaml
- `plugins/core-aliases/main.lua` + plugin.yaml
- `plugins/core-help/main.lua` + plugin.yaml

### Scene binary plugin (Phase 3)
- `api/proto/holomush/world/v1/world.proto` — WorldService contract
- `api/proto/holomush/scene/v1/scene.proto` — SceneService contract
- `internal/world/grpc_server.go` + test — WorldService gRPC adapter
- `internal/plugin/schema_provisioner.go` + test — Schema isolation
- `internal/plugin/setup/world_conn.go` — InProcessConn helper
- `pkg/plugin/service.go` + test — ServiceProvider + ServeWithServices
- `internal/plugin/goplugin/host.go` + test — Service injection
- `plugins/core-scenes/main.go` — Plugin binary entry point
- `plugins/core-scenes/store.go` + test — PostgreSQL store
- `plugins/core-scenes/service.go` + test — SceneService gRPC impl
- `plugins/core-scenes/plugin.yaml` — Manifest
- `plugins/core-scenes/migrations/` — SQL schema

### Modified existing files
- `internal/plugin/manifest.go` — Added Requires, Provides, Storage fields
- `internal/plugin/manager.go` — DAG resolution, service registration
- `internal/plugin/setup/subsystem.go` — WorldService registration, schema provisioner, proxy wiring
- `cmd/holomush/sub_grpc.go` — GRPCServiceProxy installation
- `cmd/holomush/core.go` — DatabaseConnStr wiring
- `internal/command/types.go` — Added "plugin" resource type
- `internal/command/handlers/register.go` — Plugin admin commands
- `api/proto/holomush/plugin/v1/plugin.proto` — Init RPC, ServiceConfig, removed world-query RPCs

### Deleted files (Phase 4)
- `internal/plugin/service_proxy.go` + impl + tests + mock
- `internal/plugin/scoped_proxy.go`
- `internal/plugin/otel_service_proxy.go`
- `internal/plugin/local_host.go` + test
- `internal/plugin/binary_host.go` + test (old BinaryPluginHost)
- `internal/plugin/parity_test.go`
- `internal/plugin/goplugin/host_service.go` + test

## Flags

- Security Focus: no
- Performance Critical: no
- Strict Mode: no
- Framework: Go 1.25, gRPC, hashicorp/go-plugin, gopher-lua, pgx/v5

## Review Phases

1. Code Quality & Architecture
2. Security & Performance
3. Testing & Documentation
4. Best Practices & Standards
5. Consolidated Report
