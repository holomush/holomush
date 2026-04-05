# Phase 1a: Code Quality Review

PR #192 -- Proto-first plugin architecture rework

Reviewer: Claude Opus 4.6 (1M context)
Date: 2026-04-05

---

## Summary

The PR introduces a well-structured plugin architecture with clean separation of concerns across the service registry, DAG resolution, capability modules, binary plugin host, and scene plugin. The overall design quality is high. This review identifies 18 findings across code complexity, maintainability, duplication, error handling, and technical debt.

**Finding counts by severity:**

| Severity | Count |
|----------|-------|
| Critical | 0     |
| High     | 3     |
| Medium   | 9     |
| Low      | 6     |

---

## Findings

### CQ-01 [High] Duplicated migration runner in core-scenes store

**File:** `plugins/core-scenes/store.go:88-134`
**Also:** `pkg/plugin/storage/storage.go:36-87`

The `runMigrationsFromFS` function in `store.go` is a near-verbatim copy of `storage.RunMigrations` from the SDK, differing only in accepting `fs.FS` instead of `embed.FS`. The `parseMigrationVersion` and `itoa` functions are also duplicated.

This creates a maintenance burden: any bug fix or enhancement to the migration logic must be applied in both places.

**Recommendation:** Add a `RunMigrationsFS(ctx, pool, fs.FS)` function to `pkg/plugin/storage` that accepts `fs.FS`, and have the existing `RunMigrations` delegate to it. The scene store can then call the shared function after `fs.Sub`.

```go
// In pkg/plugin/storage/storage.go
func RunMigrationsFS(ctx context.Context, pool *pgxpool.Pool, migrations fs.FS) error { ... }

func RunMigrations(ctx context.Context, pool *pgxpool.Pool, migrations embed.FS) error {
    return RunMigrationsFS(ctx, pool, migrations)
}
```

---

### CQ-02 [High] Custom itoa via recursion instead of strconv

**File:** `plugins/core-scenes/store.go:338-343`

The `itoa` function is a hand-rolled recursive integer-to-string converter. While functionally correct, it is:

1. Unnecessary -- `strconv.Itoa` is in the standard library
2. Recursive for multi-digit numbers (stack overhead, however marginal)
3. A maintenance risk -- custom utility functions for solved problems are code smells

**Recommendation:** Replace with `strconv.Itoa(n)`.

---

### CQ-03 [High] loadPlugin does not roll back service registrations on failure

**File:** `internal/plugin/manager.go:289-313`

When ABAC policy installation fails, `loadPlugin` correctly calls `host.Unload` to roll back the plugin load (line 281). However, when service registration partially fails (some services registered, then one fails), the already-registered services are not deregistered. This leaves phantom services in the registry pointing to a plugin that was never fully loaded.

The current code logs errors for service registration failures but continues. If service registration is truly best-effort, this is documented behavior. But if a partially-registered plugin is problematic, this needs cleanup.

**Recommendation:** Either:
- (a) Add rollback logic to deregister any successfully registered services before returning error, or
- (b) Document explicitly that partial service registration is acceptable and explain why (the log-and-continue pattern already suggests this is intentional, but it should be documented)

Note: The code currently does not return an error for registration failures at all -- it logs and continues to mark the plugin as loaded. This means a plugin can be "loaded" with some of its Provides unsatisfied. This may be the intended graceful degradation, but should be documented.

---

### CQ-04 [Medium] Capability module Lua function pattern is highly repetitive

**Files:**
- `internal/plugin/hostfunc/cap_alias.go` (7 functions, ~180 lines)
- `internal/plugin/hostfunc/cap_property.go` (3 functions, ~90 lines)
- `internal/plugin/hostfunc/cap_session.go` (5 functions, ~130 lines)
- `internal/plugin/hostfunc/cap_world_query.go` (2 functions, ~70 lines)

Each Lua-bound function follows an identical pattern:
1. Extract arguments from Lua state
2. Get context via `luaContext(L)`
3. Call Go interface method
4. On error: `SanitizeErrorForPlugin` + push nil + push error string
5. On success: build Lua table from result + push

The error path alone (lines 88-98 in cap_alias.go) is copy-pasted across ~17 functions with only the operation/subject strings changing.

This is not a blocking issue -- the pattern is clear and each function is individually simple. But it represents significant duplication that will compound as capabilities grow.

**Recommendation:** Consider a helper that eliminates the error-path boilerplate:

```go
func capError(L *lua.LState, ctx PluginErrorContext, err error) int {
    msg := SanitizeErrorForPlugin(ctx, err)
    L.Push(lua.LNil)
    L.Push(lua.LString(msg))
    return 2
}
```

This would reduce each error handler from 6 lines to 1. Low priority since each function is still readable.

---

### CQ-05 [Medium] SceneServiceImpl uses string literals for state/visibility constants

**File:** `plugins/core-scenes/service.go`

Scene states ("active", "paused", "ended"), visibility values ("open"), and roles ("owner", "member", "invited") are hardcoded as string literals throughout the service. For example:

- Line 45: `visibility = "open"`
- Line 49: `poseOrder = "free"`
- Line 59: `State: "active"`
- Line 75: `Role: "owner"`
- Line 151: `scene.State != "active" && scene.State != "paused"`
- Line 183: `scene.Visibility != "open"`

This violates the project's "no magic values" rule (MEMORY.md) and makes it easy to introduce typos that would only manifest at runtime.

**Recommendation:** Define string constants:

```go
const (
    SceneStateActive = "active"
    SceneStatePaused = "paused"
    SceneStateEnded  = "ended"

    VisibilityOpen = "open"

    RoleOwner   = "owner"
    RoleMember  = "member"
    RoleInvited = "invited"

    PoseOrderFree = "free"
)
```

---

### CQ-06 [Medium] proxyStreams goroutine leak potential

**File:** `internal/plugin/grpc_proxy.go:82-112`

In `proxyStreams`, the background goroutine (client-to-server forwarding) writes to `errCh` (buffered size 1), and the main goroutine reads from it with `<-errCh`. However, there are edge cases:

1. If `cli.RecvMsg` in the main goroutine returns an error before the background goroutine writes to `errCh`, the main goroutine blocks on `<-errCh` until the background goroutine also completes. This is correct synchronization but can hang if `srv.RecvMsg` blocks indefinitely.
2. If `cli.SendMsg` in the background goroutine fails, it writes to `errCh` but the value is discarded -- the main goroutine reads it (line 105) but ignores it, returning only `cli.RecvMsg`'s error.

The second point means upstream (client-to-server) errors are silently dropped. This is acceptable for a transparent proxy but should be documented.

**Recommendation:** Add a comment explaining that upstream errors are intentionally dropped because the response-side error is authoritative for gRPC status codes. Consider logging upstream errors at debug level for troubleshooting.

---

### CQ-07 [Medium] InProcessConn leaks gRPC server on Close

**File:** `internal/plugin/inprocess_conn.go:32-37, 67-77`

`NewInProcessConn` starts `srv.Serve(lis)` in a goroutine, but `Close()` only closes the client connection and listener. It does not call `srv.GracefulStop()` or `srv.Stop()`. The gRPC server goroutine will eventually exit because the listener is closed, but this is not a clean shutdown and may log errors.

The `newWorldInProcessConn` helper in `setup/world_conn.go` creates the server but never retains a reference to it for shutdown.

**Recommendation:** Store the `*grpc.Server` in `InProcessConn` and call `srv.Stop()` in `Close()`:

```go
type InProcessConn struct {
    conn     *grpc.ClientConn
    listener *bufconn.Listener
    server   *grpc.Server
}

func (c *InProcessConn) Close() error {
    c.server.Stop()
    connErr := c.conn.Close()
    lisErr := c.listener.Close()
    // ...
}
```

---

### CQ-08 [Medium] SchemaProvisioner vulnerable to SQL injection in schema name

**File:** `internal/plugin/schema_provisioner.go:47-55`

The `ProvisionSchema` method constructs DDL via `fmt.Sprintf`:

```go
ddl := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", schemaName)
```

While `pluginSchemaName` prepends "plugin_" and replaces hyphens with underscores, the plugin name itself comes from the manifest which is user-provided YAML. The manifest validation regex (`^[a-z](-?[a-z0-9])*$`) constrains names to lowercase alphanumeric + hyphens, which after the hyphen-to-underscore transform produces only `[a-z0-9_]` -- safe for unquoted SQL identifiers.

However, the safety depends on the manifest validation regex being the sole entry point for plugin names. If a future code path bypasses manifest validation, this becomes exploitable.

**Recommendation:** Add defensive quoting using `pgx.Identifier`:

```go
schemaIdent := pgx.Identifier{schemaName}
ddl := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", schemaIdent.Sanitize())
```

Or at minimum, add a comment documenting the security invariant and the regex dependency.

---

### CQ-09 [Medium] PluginSubsystem.Start is a 100-line orchestration method

**File:** `internal/plugin/setup/subsystem.go:111-213`

`Start()` performs 10 numbered steps in sequence: directory resolution, capability registry creation, hostfunc bridge creation, Lua host creation, service registry creation, WorldService registration, OTel wrapping, schema provisioner, binary host creation, manager creation, ABAC provider setup, plugin loading, command registry creation, and admin handler registration.

While each step is straightforward and the comments are excellent, the method has high cognitive complexity. Any change to startup ordering requires understanding the entire 100-line flow.

**Recommendation:** This is acceptable for now given the clear step numbering and comments. If more steps are added in the future, consider extracting groups of related steps into named private methods (e.g., `createHosts()`, `wireServices()`, `loadPlugins()`). No immediate action needed.

---

### CQ-10 [Medium] Manager.Close clears maps before closing hosts

**File:** `internal/plugin/manager.go:356-358`

`Close()` clears `m.loaded` and `m.pluginHosts` maps before calling `host.Close()` on each host. The comment says "Clear loaded maps first to ensure consistent state even if close fails." However, this means if any concurrent call to `IsPluginLoaded`, `DeliverCommand`, or `DeliverEvent` occurs during shutdown, it will see empty maps and return "not loaded" errors even while hosts are still shutting down.

The method holds the mutex for its entire duration, so truly concurrent access would block. But the ordering means the cleanup is not atomic with the host shutdown -- if `host.Close()` hangs, the maps are already cleared.

**Recommendation:** Swap the order: close hosts first, then clear maps. Or set a `closed` flag (like `goplugin.Host` does) to reject new operations during shutdown while keeping maps intact for in-flight operations.

---

### CQ-11 [Medium] WorldQuerierAdapter has repetitive nil-check boilerplate

**File:** `internal/plugin/hostfunc/adapter.go:74-164`

The four retrieval methods (`GetLocation`, `GetCharacter`, `GetCharactersByLocation`, `GetObject`) each follow an identical pattern:
1. Call service method
2. If error, wrap with PLUGIN_QUERY_FAILED
3. If nil result, log warning and return ErrNotFound wrapped

The nil-check + warning log is duplicated 4 times with only the entity type string changing.

**Recommendation:** Extract a generic helper:

```go
func wrapQueryResult[T any](result *T, err error, pluginName, entityType, entityID string) (*T, error) {
    if err != nil {
        return nil, oops.Code("PLUGIN_QUERY_FAILED").With("plugin", pluginName).With("entity_type", entityType).Wrapf(err, "get %s", entityType)
    }
    if result == nil {
        slog.Warn("service returned nil without error, treating as not found", "plugin", pluginName, "entity_type", entityType, "entity_id", entityID)
        return nil, oops.Code("PLUGIN_QUERY_FAILED").With("plugin", pluginName).With("entity_type", entityType).Wrap(world.ErrNotFound)
    }
    return result, nil
}
```

---

### CQ-12 [Medium] mapStoreError leaks error details in fallback path

**File:** `plugins/core-scenes/service.go:370-371`

```go
return status.Errorf(codes.Internal, "%s failed: %v", operation, err)
```

The fallback case in `mapStoreError` includes the raw error message in the gRPC status. While this service runs as a binary plugin (not directly exposed to untrusted clients), the error message could contain SQL details, connection strings, or other sensitive information from pgx errors.

**Recommendation:** Use a generic message in the fallback:

```go
return status.Errorf(codes.Internal, "%s failed", operation)
```

Log the full error server-side for debugging:

```go
slog.Error("unhandled store error", "operation", operation, "error", err)
return status.Errorf(codes.Internal, "%s failed", operation)
```

---

### CQ-13 [Low] SessionCapability missing compile-time interface check

**File:** `internal/plugin/hostfunc/cap_session.go`

Unlike `AliasCapability`, `PropertyCapability`, and `WorldQueryCapability`, the `SessionCapability` struct does not have a compile-time interface check:

```go
var _ Capability = (*SessionCapability)(nil)
```

All other capability modules include this check.

**Recommendation:** Add the compile-time check for consistency:

```go
var _ Capability = (*SessionCapability)(nil)
```

---

### CQ-14 [Low] SceneServiceImpl.CastPublishVote does linear scan for participant

**File:** `plugins/core-scenes/service.go:250-262`

`CastPublishVote` calls `ListParticipants` to get all participants, then does a linear scan to find the specific participant. This is O(n) per vote cast.

For scenes with many participants, this is inefficient. A `GetParticipant(sceneID, characterID)` store method would be O(1) via the primary key.

**Recommendation:** Add a `GetParticipant(ctx, sceneID, characterID)` method to the store. Low priority since scene participant counts are typically small (< 20).

---

### CQ-15 [Low] ListScenes hardcodes "open" visibility filter

**File:** `plugins/core-scenes/service.go:122-123`

```go
openVis := "open"
scenes, err := s.store.ListScenes(ctx, nil, &openVis, limit, offset)
```

`ListScenes` always filters by "open" visibility, ignoring any potential visibility parameter from the request. The `ListScenesRequest` proto does not expose visibility filtering, so this is the correct current behavior. However, the hardcoded string is a concern per CQ-05.

**Recommendation:** Use the constant from CQ-05 and add a comment explaining why visibility is hardcoded here (public listing security).

---

### CQ-16 [Low] EndScene does not verify caller is the scene owner

**File:** `plugins/core-scenes/service.go:138-164`

`EndScene` accepts a `session_id` but never checks whether that session is the scene owner or has appropriate permissions. Any authenticated caller can end any scene.

**Recommendation:** Add an ownership check:

```go
if scene.OwnerID != req.GetSessionId() {
    return nil, status.Errorf(codes.PermissionDenied, "only the scene owner can end a scene")
}
```

Or if staff should also be able to end scenes, add a role check. This is noted as a future concern since the scene system is in early implementation.

---

### CQ-17 [Low] Deprecated WorldService interface alongside WorldMutator type alias

**File:** `internal/plugin/hostfunc/adapter.go:23-33`

`WorldService` is marked deprecated in favor of `WorldMutator`, but `WorldMutator` is just a type alias for `world.Mutator`. The adapter still uses `WorldService` as its field type. The deprecation notice says "will be removed in a future version" but there is no tracking issue for this.

**Recommendation:** Either complete the migration (change the adapter field to `WorldMutator`) or file a tracking issue for the cleanup. Having a deprecated interface in new code is a code smell.

---

### CQ-18 [Low] CapabilityRegistry is not thread-safe

**File:** `internal/plugin/hostfunc/capability.go:20-57`

`CapabilityRegistry` uses a plain `map[string]Capability` with no synchronization. Currently, capabilities are registered during startup before any concurrent access, so this is safe. However, the type is exported and nothing prevents concurrent use.

**Recommendation:** Either:
- Add a `sync.RWMutex` for thread safety, or
- Document that the registry is not thread-safe and must be fully populated before first use

---

## Positive Observations

1. **Error sanitization is excellent.** The `SanitizeErrorForPlugin` function with correlation IDs is a well-designed pattern that protects internal details while maintaining debuggability.

2. **Narrow interfaces for capability modules.** Each capability defines its own minimal interface (`AliasAccess`, `SessionAccess`, `PropertyAccess`, `WorldQueryAccess`) rather than depending on large service interfaces. This is exemplary interface segregation.

3. **DAG dependency resolution is clean.** The Kahn's algorithm implementation in `dependency.go` is well-documented with clear error codes for each failure mode.

4. **Path traversal protection in goplugin Host.Load.** The symlink resolution and relative path validation (lines 152-168) is thorough security defense.

5. **GRPCServiceProxy is elegant.** Using `grpc.UnknownServiceHandler` to transparently proxy plugin services through the main gRPC server is a clean architectural choice.

6. **Schema isolation via search_path.** The SchemaProvisioner approach gives each binary plugin its own Postgres namespace without requiring separate databases.

7. **Graceful degradation throughout.** The consistent pattern of log-and-skip for individual plugin failures while continuing to load others is production-friendly.

---

## Actionable Summary

| ID    | Severity | File                              | One-line description                                |
|-------|----------|-----------------------------------|-----------------------------------------------------|
| CQ-01 | High     | plugins/core-scenes/store.go      | Duplicated migration runner from SDK                |
| CQ-02 | High     | plugins/core-scenes/store.go      | Custom recursive itoa instead of strconv.Itoa       |
| CQ-03 | High     | internal/plugin/manager.go        | No rollback of partial service registrations        |
| CQ-04 | Medium   | hostfunc/cap_*.go                 | Repetitive Lua error-path boilerplate               |
| CQ-05 | Medium   | plugins/core-scenes/service.go    | Magic string literals for state/visibility/role     |
| CQ-06 | Medium   | internal/plugin/grpc_proxy.go     | Upstream errors silently dropped in proxy           |
| CQ-07 | Medium   | internal/plugin/inprocess_conn.go | gRPC server not stopped on Close                    |
| CQ-08 | Medium   | internal/plugin/schema_provisioner.go | SQL identifier not defensively quoted           |
| CQ-09 | Medium   | internal/plugin/setup/subsystem.go | 100-line Start orchestration method                |
| CQ-10 | Medium   | internal/plugin/manager.go        | Maps cleared before hosts closed                    |
| CQ-11 | Medium   | hostfunc/adapter.go               | Repetitive nil-check boilerplate in adapter         |
| CQ-12 | Medium   | plugins/core-scenes/service.go    | Raw error leaked in gRPC status fallback            |
| CQ-13 | Low      | hostfunc/cap_session.go           | Missing compile-time interface check                |
| CQ-14 | Low      | plugins/core-scenes/service.go    | Linear scan for participant in CastPublishVote      |
| CQ-15 | Low      | plugins/core-scenes/service.go    | Hardcoded "open" visibility string                  |
| CQ-16 | Low      | plugins/core-scenes/service.go    | EndScene missing ownership authorization            |
| CQ-17 | Low      | hostfunc/adapter.go               | Deprecated interface in new code without tracking   |
| CQ-18 | Low      | hostfunc/capability.go            | CapabilityRegistry not thread-safe (no mutex)       |
