<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Plugin Architecture Review Findings Remediation Design

**Status:** Draft | **Date:** 2026-04-05

## Overview

Address the findings from the comprehensive code review of PR #192 (plugin
architecture rework). Covers P0 code fixes, binary plugin build pipeline,
testing gaps, documentation, and operational readiness. Prepares the
codebase for the subsequent out-of-tree plugin migration to `holomush-plugins`.

## RFC2119 Keywords

The keywords MUST, MUST NOT, SHOULD, SHOULD NOT, and MAY are used per RFC2119.

## Design Decisions

| # | Decision | Rationale |
|---|----------|-----------|
| D1 | Build pipeline uses manifest-driven discovery | Mirrors runtime discovery; new binary plugins compile automatically without Taskfile edits |
| D2 | Binary plugins cross-compile for linux only | HoloMUSH is a server that runs in containers. No native macOS/Windows plugin binaries |
| D3 | Plugin binaries output to `build/plugins/` | Keeps source directories clean; `.gitignore`d build artifacts |
| D4 | Infrastructure integration test uses testcontainers + real plugin binary | Tests the actual subprocess→Init→proxy chain, not mocks |
| D5 | Lua plugins get Go integration tests via existing host test pattern | Tests Lua through real gopher-lua VM + hostfunc bridge |
| D6 | Error sanitization at gRPC boundaries follows the hostfunc pattern | `SanitizeErrorForPlugin` with correlation IDs is the gold standard; gRPC adapters MUST match |

## 1. P0 Code Fixes

These are direct fixes with no design ambiguity. Each maps to a review finding.

### 1.1 Scene Ownership Authorization (SEC-02)

`EndScene` and `InviteToScene` in `plugins/core-scenes/service.go` MUST
check that `req.GetSessionId()` matches `scene.OwnerID` before proceeding.
Return `codes.PermissionDenied` on mismatch.

`KickFromScene` (if present) MUST also check ownership.

### 1.2 Internal Import Boundary (F-02)

`plugins/core-scenes/service.go` MUST NOT import `internal/idgen`. Replace
with direct use of `github.com/oklog/ulid/v2` and `crypto/rand` for ID
generation. This is a prerequisite for eventual out-of-tree migration.

### 1.3 Duplicated Migration Runner (CQ-01, F-07)

`pkg/plugin/storage/storage.go` MUST expose `RunMigrationsFS(ctx, pool, fs.FS)`
that accepts `fs.FS` instead of `embed.FS`. The existing `RunMigrations` delegates
to it. `plugins/core-scenes/store.go` deletes its local copy and calls the SDK.

### 1.4 Custom itoa (CQ-02)

Replace the recursive `itoa` function in `plugins/core-scenes/store.go` with
`strconv.Itoa`. Delete the custom implementation.

### 1.5 Error Sanitization at gRPC Boundaries (SEC-04, SEC-05)

**Scene plugin** (`plugins/core-scenes/service.go`): The `mapStoreError` fallback
MUST NOT include `err` in the gRPC status message. Log the full error server-side,
return a generic `"operation failed"` message.

**WorldService adapter** (`internal/world/grpc_server.go`): The `mapWorldError`
function MUST NOT format errors with `%v` for `codes.Internal`. Log the full
error, return generic messages. `codes.NotFound` and `codes.PermissionDenied`
MAY include the entity type but MUST NOT include internal context.

### 1.6 Magic String Constants (CQ-05)

Define constants in `plugins/core-scenes/service.go` for scene states, roles,
visibility, and pose order modes. Replace all string literals.

### 1.7 Error Comparison (GP-02)

Replace `err == pgx.ErrNoRows` with `errors.Is(err, pgx.ErrNoRows)` in
`plugins/core-scenes/store.go` to handle wrapped errors correctly.

### 1.8 SQL Identifier Quoting (SEC-03)

`internal/plugin/schema_provisioner.go` MUST use `pgx.Identifier{schemaName}.Sanitize()`
for the DDL statement. Defense-in-depth against future code paths that bypass
manifest validation.

### 1.9 gRPC Proxy Error Messages (SEC-07)

`internal/plugin/grpc_proxy.go` MUST NOT include `streamErr` in client-facing
status messages. Log the full error server-side, return
`"service temporarily unavailable"` for internal errors.

### 1.10 ListScenes Limit Cap (SEC-11)

`plugins/core-scenes/service.go` MUST cap the `limit` parameter at a maximum
(200). Values above the cap are silently reduced.

### 1.11 InProcessConn Graceful Shutdown (CQ-07)

`InProcessConn` MUST store a reference to the `*grpc.Server` passed to
`NewInProcessConn` and call `srv.Stop()` in `Close()` before closing the
listener and client connection.

### 1.12 Manager.Close Ordering (CQ-10)

`Manager.Close()` MUST close all hosts before clearing the `loaded` and
`pluginHosts` maps. This ensures in-flight operations see consistent state
during shutdown.

## 2. Binary Plugin Build Pipeline

### 2.1 Discovery

`task plugin:build-all` MUST discover binary plugins by scanning
`plugins/*/plugin.yaml` for manifests with `type: binary`. For each match,
it reads the `binary-plugin.executable` field to determine the output binary
name.

Discovery logic (shell):

```bash
for manifest in plugins/*/plugin.yaml; do
  type=$(yq '.type' "$manifest")
  if [ "$type" = "binary" ]; then
    dir=$(dirname "$manifest")
    exec=$(yq '.binary-plugin.executable' "$manifest")
    # compile $dir to build/plugins/$(basename $dir)/$exec
  fi
done
```

### 2.2 Compilation

Binary plugins MUST be cross-compiled for `GOOS=linux GOARCH=amd64` (the
Docker runtime target). Output goes to `build/plugins/<name>/<executable>`.

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "build/plugins/$name/$exec" "./$dir"
```

The `build/` directory MUST be in `.gitignore`.

### 2.3 Taskfile Integration

| Task | Behavior |
|------|----------|
| `task plugin:build-all` | Discover + compile all binary plugins to `build/plugins/` |
| `task plugin:build -- <name>` | Compile a single named plugin |
| `task build` | Existing server build (unchanged) |
| `task docker:build` | Depends on `plugin:build-all`, then `docker build` |
| `task pr-prep` | Adds `plugin:build-all` as a step before integration tests |

### 2.4 Docker Integration

The Dockerfile multi-stage build MUST:

1. Copy `build/plugins/` from the build context (pre-compiled by `task docker:build`)
2. Place plugin binaries at `/home/holomush/.local/share/holomush/plugins/<name>/<executable>`
3. Copy `plugin.yaml` alongside each binary
4. Copy Lua plugin directories as-is (no compilation needed)

Alternatively, the Dockerfile builder stage MAY compile plugins natively
(eliminating the cross-compilation requirement on the host):

```dockerfile
FROM golang:1.25-alpine AS builder
# ... compile server ...
# Compile binary plugins
COPY plugins/ /src/plugins/
RUN for dir in /src/plugins/*/; do \
      [ -f "$dir/plugin.yaml" ] || continue; \
      type=$(grep 'type:' "$dir/plugin.yaml" | awk '{print $2}'); \
      [ "$type" = "binary" ] || continue; \
      exec=$(grep 'executable:' "$dir/plugin.yaml" | awk '{print $2}'); \
      go build -o "$dir/$exec" "./$dir"; \
    done
```

The Dockerfile approach is RECOMMENDED because it avoids cross-compilation
issues and produces binaries for the correct target architecture.

### 2.5 CI Integration

CI workflows MUST compile binary plugins. The existing `ci.yml` build step
SHOULD use the Dockerfile approach (build inside Docker) so CI does not
need cross-compilation tooling.

### 2.6 GoReleaser

GoReleaser config SHOULD include binary plugin compilation as extra build
targets. This ensures release artifacts contain working plugin binaries.

## 3. Testing

### 3.1 Plugin-Level Integration Tests

Each binary plugin SHOULD have integration tests that exercise its store and
service against a real PostgreSQL instance via testcontainers.

For `core-scenes`:

- `plugins/core-scenes/store_integration_test.go` (`//go:build integration`)
- Tests: CreateScene + GetScene round-trip, participant CRUD, migration
  idempotency, schema isolation
- Uses testcontainers PostgreSQL

These tests validate the plugin's own code without involving the go-plugin
subprocess mechanism.

### 3.2 Infrastructure Integration Test

A new integration test MUST verify the full binary plugin lifecycle:

1. Compile the test plugin binary (or use a minimal test-only plugin)
2. Start PostgreSQL via testcontainers
3. Create a plugin subsystem with the test plugin directory
4. Verify the service registry contains the plugin's provided services
5. Make gRPC calls through the proxy to the plugin's service
6. Verify schema was provisioned

Location: `test/integration/plugin/binary_plugin_test.go`

This test depends on `task plugin:build-all` having run. The `task test:int`
target MUST depend on `plugin:build-all`.

**Minimal test plugin option:** Instead of testing with `core-scenes` (which
has many dependencies), consider a minimal `test-echo` binary plugin that
provides a trivial gRPC service. This reduces test coupling and makes the
infrastructure test independent of scene plugin correctness.

### 3.3 Lua Plugin Integration Tests

Each Lua plugin rewrite MUST have integration tests that verify command
handling through the real gopher-lua VM + hostfunc bridge. Follow the
existing pattern in `internal/plugin/communication_integration_test.go`
and `internal/plugin/help_integration_test.go`.

Test structure per plugin:

```go
//go:build integration

func TestCoreAliasesPlugin(t *testing.T) {
    host := setupLuaHostWithPlugin(t, "core-aliases")

    t.Run("alias set creates player alias", func(t *testing.T) {
        resp := deliverCommand(t, host, "alias", "test=look")
        assert.Equal(t, pluginsdk.CommandOK, resp.Status)
    })
}
```

### 3.4 Security-Specific Tests

Tests MUST verify:

- `EndScene` rejects non-owner callers (after SEC-02 fix)
- `InviteToScene` rejects non-owner callers
- `mapStoreError` fallback does not include raw error text
- `mapWorldError` `codes.Internal` path does not include oops context
- `ListScenes` with limit > 200 returns at most 200 results

### 3.5 Docker Compose E2E

The existing E2E test suite (`task test:e2e`) MUST pass with binary plugins
loaded. No new E2E test cases are required — the server health check already
validates that plugins loaded successfully. The fix is in the Docker build
(Section 2.4), not in the test suite.

## 4. Documentation

### 4.1 CLAUDE.md Update

CLAUDE.md MUST be updated to:

- Remove references to `ServiceProxy`, `LocalPluginHost`, `type: core`
- Add plugin architecture overview (service registry, manifest schema,
  plugin types)
- Document `task plugin:build-all` and plugin build workflow
- Update the directory structure to include `build/plugins/`

### 4.2 Design Spec Accuracy (DOC-09)

`docs/superpowers/specs/2026-04-05-plugin-architecture-rework-design.md`
MUST be updated to note that Section 5.3 (Postgres Storage Flow) step 3
(schema-scoped PostgreSQL role) is NOT YET IMPLEMENTED. The current
implementation uses `search_path` scoping only. Add a note that role-based
isolation is required before third-party plugin support.

### 4.3 Plugin Author Guide

A plugin authoring guide SHOULD be added to `site/docs/extending/` covering:

- Plugin types (lua, binary, setting)
- Manifest format with `requires`, `provides`, `storage` fields
- Binary plugin SDK: `ServeWithServices`, `ServiceProvider`, `Init`
- Storage SDK: `Connect`, `RunMigrationsFS`
- How to test a plugin
- `core-scenes` as reference implementation

This is a SHOULD (not MUST) for this remediation — it can be a fast-follow.

### 4.4 Operator Documentation

Operator documentation SHOULD be added to `site/docs/operating/` covering:

- Plugin discovery and loading
- `plugin list` and `plugin info` admin commands
- Schema provisioning behavior
- Connection pool sizing implications
- Binary plugin subprocess lifecycle

## 5. Operational Improvements

### 5.1 Subprocess Environment (SEC-06)

The `goplugin.DefaultClientFactory.NewClient` SHOULD construct a minimal
`Cmd.Env` instead of inheriting the full parent environment. The minimal
env includes `PATH` and go-plugin's handshake variables. The connection
string is passed via the `Init` RPC, not the environment.

### 5.2 Plugin Health in Readiness Probe (OPS-07)

The `PluginSubsystem` SHOULD implement `HealthReporter` so plugin failures
are visible in the server's readiness probe. A plugin that fails to load
degrades readiness; a plugin that loads successfully but becomes unhealthy
(binary plugin process dies) also degrades readiness.

This is a SHOULD for this remediation — acceptable as follow-up.

## 6. Out of Scope (Follow-Up Work)

| Item | Reason | Tracking |
|------|--------|----------|
| PostgreSQL role-based schema isolation (SEC-01) | Requires DB role management design; acceptable for first-party only | `holomush-buph` |
| ServiceConfig.required_services population (F-05) | Requires go-plugin broker multiplexing design | Future epic |
| Lua bytecode caching (PERF-3) | Performance optimization, orthogonal to correctness | Future task |
| Plugin out-of-tree migration | Separate spec after this remediation lands | Future epic |
| Plugin as container image | Future distribution model exploration | Future epic |
| Proto-generated Lua bindings (D7) | Needs its own design spec | Future epic |
| Dynamic plugin reload | Needs its own design spec | Future epic |
