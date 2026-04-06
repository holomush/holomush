# Plugin Architecture Phase 4: Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the dead ServiceProxy layer and related code (~3000 lines) that Phase 2's capability module decomposition rendered obsolete, then add plugin admin commands.

**Architecture:** The ServiceProxy interface, its implementation, OTel wrapper, scoped wrapper, mock, LocalPluginHost, goplugin PluginHostService world queries, parity test, and all associated tests are dead code. Phase 2 replaced all consumers with narrow interfaces injected from real services (world.Service, session.Access, property.Registry). The subsystem still creates a ServiceProxyImpl but nothing uses it. Phase 4 deletes the dead code and removes the subsystem wiring, then adds `plugin list` and `plugin info` admin commands.

**Tech Stack:** Go 1.25, testify

**Spec:** `docs/superpowers/specs/2026-04-05-plugin-architecture-rework-design.md` (Section 11 Phase 4)

**Depends on:** Phase 3 (complete)

---

## Scope

### In scope

1. Delete dead ServiceProxy layer: interface, impl, scoped wrapper, OTel wrapper, mock, tests
2. Delete dead LocalPluginHost and tests
3. Delete dead old BinaryPluginHost (not goplugin.Host) and tests
4. Remove world-query RPCs from PluginHostService (binary plugins use WorldService)
5. Remove world-query messages from plugin.proto PluginHostService
6. Remove ServiceProxy creation and wiring from subsystem + sub_grpc.go
7. Add `plugin list` and `plugin info` admin commands
8. Fix resulting lint issues

### Out of scope (future work)

- Proto code generator for Lua bindings (needs its own design)
- Dynamic plugin reload (needs design for in-flight request handling)
- Full admin command set (reload/disable/enable need dynamic reload)
- Plugin signing

---

## File Structure

### Files to Delete

| File | Reason |
|------|--------|
| `internal/plugin/service_proxy.go` | Interface — no runtime consumers |
| `internal/plugin/service_proxy_impl.go` | Implementation — dead |
| `internal/plugin/service_proxy_test.go` | Tests for dead interface |
| `internal/plugin/service_proxy_impl_test.go` | Tests for dead impl |
| `internal/plugin/scoped_proxy.go` | Wrapper — only used by dead LocalPluginHost |
| `internal/plugin/otel_service_proxy.go` | OTel wrapper — zero references |
| `internal/plugin/otel_middleware_test.go` | Tests for dead OTel wrapper (verify first) |
| `internal/plugin/local_host.go` | Dead since Phase 2 |
| `internal/plugin/local_host_test.go` | Tests for dead host |
| `internal/plugin/binary_host.go` | Old BinaryPluginHost — replaced by goplugin.Host |
| `internal/plugin/binary_host_test.go` | Tests for old host |
| `internal/plugin/mocks/mock_ServiceProxy.go` | Generated mock for dead interface |
| `internal/plugin/parity_test.go` | Tests ServiceProxy/hostfunc parity — dead |

### Files to Modify

| File | Change |
|------|--------|
| `internal/plugin/setup/subsystem.go` | Remove ServiceProxy creation, field, accessor, late-binding deps |
| `internal/plugin/setup/subsystem_test.go` | Remove ServiceProxy-related test expectations |
| `cmd/holomush/sub_grpc.go` | Remove `serviceProxy` variable, `SetLateBindings` call |
| `internal/plugin/goplugin/host_service.go` | Remove world-query RPCs (QueryLocation, QueryCharacter, QueryLocationCharacters) |
| `internal/plugin/goplugin/host_service_test.go` | Remove world-query tests |
| `api/proto/holomush/plugin/v1/plugin.proto` | Remove PluginHostService world-query RPCs and messages |

### New Files

| File | Responsibility |
|------|---------------|
| `internal/command/handlers/plugin_admin.go` | `plugin list` and `plugin info` commands |
| `internal/command/handlers/plugin_admin_test.go` | Tests |

---

## Task 1: Delete ServiceProxy Layer

Remove the dead ServiceProxy interface, implementation, wrappers, and all tests.

**Files to delete:**

- `internal/plugin/service_proxy.go`
- `internal/plugin/service_proxy_impl.go`
- `internal/plugin/service_proxy_test.go`
- `internal/plugin/service_proxy_impl_test.go`
- `internal/plugin/scoped_proxy.go`
- `internal/plugin/otel_service_proxy.go`
- `internal/plugin/mocks/mock_ServiceProxy.go`

- [ ] **Step 1: Verify no runtime imports of ServiceProxy**

Run: `cd /Volumes/Code/github.com/holomush/holomush_worktrees/plugin-arch && rg 'ServiceProxy' --type go -l | grep -v _test.go | grep -v mock_ | grep -v parity | grep -v local_host | grep -v scoped_proxy | grep -v otel_service | grep -v binary_host`

Verify the only production references are:

- `service_proxy.go` (the interface itself)
- `service_proxy_impl.go` (the implementation)
- `setup/subsystem.go` (creates it — will be cleaned in Task 3)
- `cmd/holomush/sub_grpc.go` (late bindings — will be cleaned in Task 3)
- `goplugin/host_service.go` (uses it — will be cleaned in Task 4)
- `hostfunc/cap_*.go` (comments only)
- `grpc_proxy.go` (GRPCServiceProxy — different thing, name overlap in grep)
- `pkg/plugin/command.go` (comment only)

- [ ] **Step 2: Delete the files**

```bash
rm internal/plugin/service_proxy.go
rm internal/plugin/service_proxy_impl.go
rm internal/plugin/service_proxy_test.go
rm internal/plugin/service_proxy_impl_test.go
rm internal/plugin/scoped_proxy.go
rm internal/plugin/otel_service_proxy.go
rm internal/plugin/mocks/mock_ServiceProxy.go
```

- [ ] **Step 3: Verify build fails with expected errors**

Run: `task build 2>&1 | head -20`

Expected: Compilation errors in `setup/subsystem.go` and `cmd/holomush/sub_grpc.go` referencing missing types. These will be fixed in Task 3.

- [ ] **Step 4: Commit (broken build — will be fixed in Task 3)**

```bash
JJ_EDITOR=true jj --no-pager commit -m "refactor(plugin): delete dead ServiceProxy layer (~2000 lines)"
```

Note: Committing a broken build is acceptable mid-cleanup when the fix is the next commit.

---

## Task 2: Delete Dead Hosts and Parity Test

Remove LocalPluginHost, old BinaryPluginHost, and the parity test.

**Files to delete:**

- `internal/plugin/local_host.go`
- `internal/plugin/local_host_test.go`
- `internal/plugin/binary_host.go`
- `internal/plugin/binary_host_test.go`
- `internal/plugin/parity_test.go`

- [ ] **Step 1: Verify LocalPluginHost has no subsystem references**

Run: `rg 'LocalPluginHost' --type go internal/plugin/setup/`

Expected: No matches.

- [ ] **Step 2: Verify old BinaryPluginHost has no subsystem references**

Run: `rg 'BinaryPluginHost' --type go internal/plugin/setup/`

Expected: No matches.

- [ ] **Step 3: Check if otel_middleware_test.go only tests dead code**

Read `internal/plugin/otel_middleware_test.go` and check if it tests only `OTelServiceProxy` (dead) or also `HostMiddleware` (live). If it tests both, only remove the OTelServiceProxy tests, not the whole file.

- [ ] **Step 4: Delete the files**

```bash
rm internal/plugin/local_host.go
rm internal/plugin/local_host_test.go
rm internal/plugin/binary_host.go
rm internal/plugin/binary_host_test.go
rm internal/plugin/parity_test.go
```

If `otel_middleware_test.go` only tests dead code, also delete it:

```bash
rm internal/plugin/otel_middleware_test.go
```

- [ ] **Step 5: Check for sentinel error conflicts**

`local_host.go` defines `ErrHostClosed`, `ErrPluginNotLoaded`, `ErrNoCommandHandler`, `ErrNoEventHandler`. If `goplugin/host.go` defines its own copies, the deletion is safe. If other code imports these from the `plugins` package, move them to a shared location or verify they're duplicated.

Run: `rg 'ErrHostClosed|ErrPluginNotLoaded|ErrNoCommandHandler|ErrNoEventHandler' --type go internal/plugin/`

- [ ] **Step 6: Commit**

```bash
JJ_EDITOR=true jj --no-pager commit -m "refactor(plugin): delete dead LocalPluginHost, old BinaryPluginHost, parity test"
```

---

## Task 3: Remove ServiceProxy Wiring from Subsystem and Server

Clean up the subsystem and server code that creates and wires the now-deleted ServiceProxy.

**Files:**

- Modify: `internal/plugin/setup/subsystem.go`
- Modify: `internal/plugin/setup/subsystem_test.go`
- Modify: `cmd/holomush/sub_grpc.go`

- [ ] **Step 1: Read current subsystem.go to identify ServiceProxy references**

In `internal/plugin/setup/subsystem.go`, find:

- The `proxy` field on `PluginSubsystem` struct
- The `NewServiceProxy()` call in `Start()`
- The `ServiceProxy()` accessor method
- The `ServiceProxyConfig` struct usage
- Any `SessionAccess` interface (if only used by ServiceProxy)
- The `EventStoreProvider` (if only used by ServiceProxy)

- [ ] **Step 2: Remove proxy field, creation, and accessor**

Remove from `PluginSubsystem`:

```go
proxy *plugins.ServiceProxyImpl  // DELETE this field
```

Remove from `Start()`:

```go
// DELETE this block:
proxy, proxyErr := plugins.NewServiceProxy(plugins.ServiceProxyConfig{...})
if proxyErr != nil { ... }
s.proxy = proxy
```

Remove the `ServiceProxy()` method entirely.

Remove `SessionAccess` interface and `SessionProvider` if they're only used by ServiceProxy. Check whether the hostfunc bridge still needs session access — it does (`WithSessionAccess(sessionStore)`), so keep `SessionProvider` but remove `SessionAccess` if it was ServiceProxy-specific.

Remove `EventStoreProvider` if only used by ServiceProxy.

- [ ] **Step 3: Remove from sub_grpc.go**

Remove:

```go
serviceProxy := s.cfg.Plugins.ServiceProxy()  // DELETE
```

Remove:

```go
serviceProxy.SetLateBindings(plugins.LateBindingsConfig{...})  // DELETE entire block
```

Remove the `LateBindingsConfig` type from `service_proxy_impl.go` — but that file was already deleted. Check if anything else references it.

- [ ] **Step 4: Update subsystem_test.go**

Remove any test expectations about ServiceProxy creation.

- [ ] **Step 5: Verify build compiles**

Run: `task build`

Expected: Clean build.

- [ ] **Step 6: Run tests**

Run: `task test`

Expected: All pass (with fewer tests since we deleted test files).

- [ ] **Step 7: Commit**

```bash
JJ_EDITOR=true jj --no-pager commit -m "refactor(plugin): remove ServiceProxy wiring from subsystem and server"
```

---

## Task 4: Remove World-Query RPCs from PluginHostService

Binary plugins now use WorldService directly. Remove the duplicate world-query RPCs from the PluginHostService gRPC callback service.

**Files:**

- Modify: `api/proto/holomush/plugin/v1/plugin.proto`
- Modify: `internal/plugin/goplugin/host_service.go`
- Modify: `internal/plugin/goplugin/host_service_test.go`

- [ ] **Step 1: Remove world-query RPCs and messages from plugin.proto**

In `api/proto/holomush/plugin/v1/plugin.proto`, delete:

- `rpc QueryLocation(...)` from `PluginHostService`
- `rpc QueryCharacter(...)` from `PluginHostService`
- `rpc QueryLocationCharacters(...)` from `PluginHostService`
- All `PluginHostServiceQueryLocation*` request/response messages
- All `PluginHostServiceQueryCharacter*` request/response messages
- The `PluginHostServiceLocationInfo` and `PluginHostServiceCharacterInfo` messages

Keep the base proxy RPCs: `EmitEvent`, `Log`, `KVGet`, `KVSet`, `KVDelete`.

- [ ] **Step 2: Regenerate Go code**

Run: `task proto`

- [ ] **Step 3: Update host_service.go**

Remove the `QueryLocation`, `QueryCharacter`, `QueryLocationCharacters` methods and the `characterResultToProto` helper.

Update `PluginHostService` to no longer depend on `ServiceProxy` — it only needs:

- `EmitEvent` → needs an event emitter (could be `core.EventStore.Append` or a narrow interface)
- `Log` → logger
- `KV*` → KV store

Since the ServiceProxy is gone, `PluginHostService` needs its dependencies refactored to narrow interfaces. Replace the `ServiceProxy` field with specific dependencies:

```go
type PluginHostService struct {
    pluginv1.UnimplementedPluginHostServiceServer
    emitter EventEmitter  // narrow interface for EmitEvent
    kvStore KVStore       // narrow interface for KV ops
    logger  *slog.Logger
}
```

Where `EventEmitter` is:

```go
type EventEmitter interface {
    EmitEvent(ctx context.Context, stream, eventType string, payload []byte) error
}
```

And `KVStore` is the existing `hostfunc.KVStore` interface or equivalent.

- [ ] **Step 4: Update host_service_test.go**

Remove world-query tests. Update remaining tests to use narrow interfaces instead of ServiceProxy mock.

- [ ] **Step 5: Verify build and tests**

Run: `task build && task test -- ./internal/plugin/goplugin/`

Expected: Clean build, all goplugin tests pass.

- [ ] **Step 6: Commit**

```bash
JJ_EDITOR=true jj --no-pager commit -m "refactor(plugin): remove world-query RPCs from PluginHostService, use narrow interfaces"
```

---

## Task 5: Plugin Admin Commands

Add `plugin list` and `plugin info <name>` commands for server operators.

**Files:**

- Create: `internal/command/handlers/plugin_admin.go`
- Create: `internal/command/handlers/plugin_admin_test.go`
- Modify: `internal/command/handlers/register.go` (add registration)

- [ ] **Step 1: Read existing admin command pattern**

Read `internal/command/handlers/admin.go` to understand how admin commands are registered and how `AdminDeps` provides dependencies.

Read `internal/command/handlers/register.go` for the registration pattern.

- [ ] **Step 2: Write failing test for plugin list command**

```go
func TestPluginListCommandReturnsLoadedPlugins(t *testing.T) {
    // Create mock plugin manager
    // Register "plugin" command with list subcommand
    // Execute command
    // Assert output contains plugin names, types, versions
}
```

- [ ] **Step 3: Implement plugin list**

The `plugin list` command should output:

```text
Loaded plugins:
  core-communication  lua      1.0.0  ✓ healthy
  core-building       lua      1.0.0  ✓ healthy
  core-scenes         binary   1.0.0  ✓ healthy  [provides: SceneService]
```

It needs access to the plugin Manager's `ListPlugins()` and loaded manifest data.

- [ ] **Step 4: Write failing test for plugin info command**

```go
func TestPluginInfoCommandReturnsPluginDetails(t *testing.T) {
    // Execute "plugin info core-scenes"
    // Assert output contains: name, version, type, requires, provides, storage, commands
}
```

- [ ] **Step 5: Implement plugin info**

The `plugin info <name>` command should output:

```text
Plugin: core-scenes
Version: 1.0.0
Type: binary
Storage: postgres
Requires: holomush.world.v1.WorldService
Provides: holomush.scene.v1.SceneService
Commands: scene, scenes
```

- [ ] **Step 6: Register commands**

Add `plugin` command registration in `RegisterAdmin()`.

- [ ] **Step 7: Run tests**

Run: `task test -- ./internal/command/handlers/`

Expected: All pass.

- [ ] **Step 8: Commit**

```bash
JJ_EDITOR=true jj --no-pager commit -m "feat(command): add plugin list and plugin info admin commands"
```

---

## Task 6: Clean Up Comments and Run Full Suite

- [ ] **Step 1: Remove stale ServiceProxy references from comments**

Search for "ServiceProxy" in comments across hostfunc/cap_*.go and pkg/plugin/command.go. Update or remove references.

- [ ] **Step 2: Run full test suite**

Run: `task test`

Expected: All pass with fewer total tests (deleted dead test files).

- [ ] **Step 3: Run lint**

Run: `task lint`

Expected: Fewer issues than before (removed files that had lint warnings).

- [ ] **Step 4: Fix any new lint issues**

- [ ] **Step 5: Commit**

```bash
JJ_EDITOR=true jj --no-pager commit -m "fix: clean up stale ServiceProxy references, fix lint"
```

---

## Dependency Map

```text
Task 1 (Delete ServiceProxy files)
  └→ Task 3 (Remove wiring from subsystem/server)

Task 2 (Delete dead hosts)
  └→ Task 3 (may uncover more wiring to remove)

Task 3 (Remove wiring)
  └→ Task 4 (Remove world queries from PluginHostService)
       └→ Task 6 (Final cleanup)

Task 5 (Admin commands) — independent, can run any time after Task 3

Task 1 + Task 2 → Task 3 → Task 4 → Task 6
                              Task 5 → Task 6
```

All tasks are sequential — each builds on the previous deletion.
