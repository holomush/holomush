# Phase 4a: Go Practices Review

PR #192 -- Proto-first plugin architecture rework

## Findings

### GP-01: `Resolve` returns pointer to map value copy -- potential stale data

**Severity:** Medium  
**File:** `internal/plugin/registry.go:39-50`

`ServiceRegistry.Resolve` copies the `RegisteredService` out of the map, then returns a pointer to that local copy. The caller receives a pointer that is detached from the registry. Mutations to the returned `*RegisteredService` will not affect the registry, and the `Health` field (an interface pointer) may create confusing semantics -- callers might expect the returned service to reflect registry state.

**Current:**

```go
func (r *ServiceRegistry) Resolve(name string) (*RegisteredService, error) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    svc, ok := r.services[name]
    if !ok { ... }
    return &svc, nil // pointer to local copy
}
```

**Recommended:** Either store `*RegisteredService` in the map (so Resolve returns the canonical pointer) or return by value. Since `RegisteredService` is small and contains an interface field, returning by value is cleaner:

```go
func (r *ServiceRegistry) Resolve(name string) (RegisteredService, bool) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    svc, ok := r.services[name]
    return svc, ok
}
```

The `(value, bool)` signature is also more idiomatic for lookups than `(*T, error)`, matching `map` and `sync.Map` conventions. The current callers only read the returned value, so this is not a bug today, but it is a latent footgun.

---

### GP-02: `err == pgx.ErrNoRows` instead of `errors.Is`

**Severity:** Medium  
**File:** `plugins/core-scenes/store.go:177`

Direct equality comparison misses wrapped errors. The rest of the codebase uses `errors.Is`.

**Current:**

```go
if err == pgx.ErrNoRows {
```

**Recommended:**

```go
if errors.Is(err, pgx.ErrNoRows) {
```

---

### GP-03: `mapStoreError` switches on `oopsErr.Code()` without string type assertion

**Severity:** Low  
**File:** `plugins/core-scenes/service.go:354-355`

Every other file in the codebase that inspects oops error codes uses the pattern `code, ok := oopsErr.Code().(string)` with a safety check. The scene plugin's `mapStoreError` switches on `oopsErr.Code()` directly, which compares `any` values. This works because oops stores string codes as `string`, but it is inconsistent and fragile.

**Current:**

```go
switch oopsErr.Code() {
case "SCENE_NOT_FOUND":
```

**Recommended:** Follow the project pattern:

```go
code, ok := oopsErr.Code().(string)
if !ok {
    return status.Errorf(codes.Internal, "%s failed: %v", operation, err)
}
switch code {
case "SCENE_NOT_FOUND":
```

---

### GP-04: `InProcessConn` has no way to stop the gRPC server goroutine

**Severity:** Medium  
**File:** `internal/plugin/inprocess_conn.go:32-37`

`NewInProcessConn` starts a goroutine running `srv.Serve(lis)`, but `Close()` only closes the client connection and listener. The gRPC server itself is never stopped via `srv.GracefulStop()` or `srv.Stop()`. While `lis.Close()` will cause `srv.Serve` to return, the server still holds resources (registered services, internal state). The `*grpc.Server` should be retained and stopped on `Close()`.

**Recommended:**

```go
type InProcessConn struct {
    conn     *grpc.ClientConn
    listener *bufconn.Listener
    server   *grpc.Server
}

func (c *InProcessConn) Close() error {
    c.server.Stop()        // stops the serving goroutine cleanly
    connErr := c.conn.Close()
    lisErr := c.listener.Close()
    // ...
}
```

---

### GP-05: Pre-Go 1.21 `sort.Slice` / `sort.Strings` used instead of `slices.SortFunc`

**Severity:** Low  
**File:** `internal/plugin/manager.go:11,223,339`, `pkg/plugin/storage/storage.go:65`

The project is on Go 1.25 but uses the legacy `sort` package in new code. The `slices` package (stable since Go 1.21) provides `slices.SortFunc` and `slices.Sort` with better type safety and no boxing of comparison functions.

**Current:**

```go
sort.Slice(discovered, func(i, j int) bool {
    return discovered[i].Manifest.EffectivePriority() < discovered[j].Manifest.EffectivePriority()
})
sort.Strings(names)
```

**Recommended:**

```go
slices.SortFunc(discovered, func(a, b *DiscoveredPlugin) int {
    return cmp.Compare(a.Manifest.EffectivePriority(), b.Manifest.EffectivePriority())
})
slices.Sort(names)
```

---

### GP-06: `interface{}` in new code instead of `any`

**Severity:** Low  
**File:** `internal/plugin/grpc_proxy.go:32`, `pkg/plugin/service.go:89`

Go 1.18+ introduced `any` as an alias for `interface{}`. New code should prefer the shorter form for readability. The `streamHandler` signature uses `_ interface{}` and `GRPCClient` returns `(interface{}, error)`.

**Current:**

```go
func (p *GRPCServiceProxy) streamHandler(_ interface{}, stream grpc.ServerStream) error {
```

**Recommended:**

```go
func (p *GRPCServiceProxy) streamHandler(_ any, stream grpc.ServerStream) error {
```

Note: The `GRPCClient` signature in `service.go:89` is constrained by the `hashicorp/go-plugin` interface contract and cannot be changed.

---

### GP-07: `WorldService` interface in adapter.go is not consumed where defined

**Severity:** Low  
**File:** `internal/plugin/hostfunc/adapter.go:23-28`

The `WorldService` interface is defined in the `hostfunc` package and consumed in the same package (by `WorldQuerierAdapter`). This is fine. However, it is marked `Deprecated` in favor of `WorldMutator`, yet `WorldQuerierAdapter` still accepts `WorldService` in its constructor -- not `WorldMutator`. The deprecation comment says "Use WorldMutator instead" but the adapter only needs read methods, so accepting the narrower `WorldService` is actually *correct* by Go interface segregation principles. The deprecation notice is misleading.

**Recommended:** Either remove the deprecation notice (the narrow interface is correct), or if the intention is to truly migrate, update `NewWorldQuerierAdapter` to accept `WorldMutator` and remove `WorldService`.

---

### GP-08: Duplicated migration runner logic between SDK and scene plugin

**Severity:** Medium  
**File:** `plugins/core-scenes/store.go:88-134` vs `pkg/plugin/storage/storage.go:36-87`

The scene plugin duplicates `RunMigrations` from the storage SDK because `storage.RunMigrations` requires `embed.FS` rather than `fs.FS`. The duplication includes the migration tracking table creation, version parsing, and file iteration. This is a ~50 line copy that will drift over time.

**Recommended:** Update `storage.RunMigrations` to accept `fs.FS` (which `embed.FS` satisfies) instead of `embed.FS`. This eliminates the need for `runMigrationsFromFS` in the scene plugin entirely:

```go
// In pkg/plugin/storage/storage.go
func RunMigrations(ctx context.Context, pool *pgxpool.Pool, migrations fs.FS) error {
```

---

### GP-09: `parseMigrationVersion` also duplicated between SDK and scene plugin

**Severity:** Low  
**File:** `plugins/core-scenes/store.go:327-335` vs `pkg/plugin/storage/storage.go:104-112`

Exact copy of the same function. Should be exported from the SDK or shared via a common internal helper.

---

### GP-10: `proxyStreams` goroutine leak on server-to-client error

**Severity:** Medium  
**File:** `internal/plugin/grpc_proxy.go:82-112`

In `proxyStreams`, if `cli.RecvMsg` returns an error (server-to-client direction, main goroutine), the function reads from `errCh` and returns. However, the goroutine forwarding client-to-server may be blocked on `srv.RecvMsg`. Since the function returns without closing the client stream explicitly on that path, the goroutine may hang until the underlying transport is torn down.

Additionally, if the goroutine sends a non-nil error to `errCh` (from `cli.SendMsg` failing), the main goroutine may still be blocked on `cli.RecvMsg` -- there is no cancellation path to unblock it.

**Recommended:** Use a context or explicit close to ensure both goroutines can terminate:

```go
func proxyStreams(srv grpc.ServerStream, cli grpc.ClientStream) error {
    errCh := make(chan error, 1)
    go func() {
        for {
            msg := &rawMessage{}
            if err := srv.RecvMsg(msg); err != nil {
                _ = cli.CloseSend()
                errCh <- nil
                return
            }
            if err := cli.SendMsg(msg); err != nil {
                _ = cli.CloseSend()
                errCh <- err
                return
            }
        }
    }()

    for {
        msg := &rawMessage{}
        if err := cli.RecvMsg(msg); err != nil {
            sendErr := <-errCh
            if sendErr != nil {
                return sendErr
            }
            return err
        }
        if err := srv.SendMsg(msg); err != nil {
            <-errCh
            return err
        }
    }
}
```

At minimum, the send-direction error should be preferred over the receive-direction error when both fail.

---

### GP-11: Scene plugin error codes are string literals with no constants

**Severity:** Low  
**File:** `plugins/core-scenes/store.go`, `plugins/core-scenes/service.go`

Error codes like `"SCENE_NOT_FOUND"`, `"SCENE_CREATE_FAILED"`, etc. appear as string literals in both the store (where they are set) and the service (where they are matched). This is the same class of issue as CQ-05 (magic strings), but in a different package. Confirming overlap -- if CQ-05 covers this specific location, skip. If CQ-05 is about hostfunc or other locations, this is a distinct finding.

---

### GP-12: `SchemaProvisioner.ProvisionSchema` uses unquoted identifiers in DDL

**Severity:** Medium  
**File:** `internal/plugin/schema_provisioner.go:48-49`

`pluginSchemaName` converts plugin names to schema names (e.g., `core-scenes` becomes `plugin_core_scenes`), but the resulting name is interpolated directly into DDL without quoting. While the current naming pattern produces safe identifiers, this is fragile -- if a plugin name ever produces a Postgres reserved word or unexpected character, the DDL will break or worse.

**Current:**

```go
ddl := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", schemaName)
```

**Recommended:**

```go
ddl := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %q", schemaName)
```

Or better, use `pgx.Identifier{schemaName}.Sanitize()` which properly quotes identifiers per PostgreSQL rules.

---

### GP-13: `CapabilityRegistry` concurrent read during `InjectRequired` while mutations via `Register` are unsynchronized

**Severity:** High (confirmed overlap with CQ-18)  
**File:** `internal/plugin/hostfunc/capability.go`

This confirms and refines CQ-18. `CapabilityRegistry` has no mutex. `Register` writes to the map, `InjectRequired` reads from it. In production, capabilities are registered during startup and injected during event delivery -- these can overlap if a late-starting subsystem registers capabilities while Lua VMs are already being created. The `ServiceRegistry` in the same PR demonstrates the correct pattern with `sync.RWMutex`.

**Not re-reported** -- CQ-18 already covers this. Confirming severity is appropriate.

---

## Summary

| ID | Severity | Category | Description |
|----|----------|----------|-------------|
| GP-01 | Medium | Interface design | `Resolve` returns pointer to map value copy |
| GP-02 | Medium | Error handling | `err ==` instead of `errors.Is` for pgx sentinel |
| GP-03 | Low | Error handling | Missing string type assertion on oops code switch |
| GP-04 | Medium | Concurrency | `InProcessConn` never stops the gRPC server |
| GP-05 | Low | Modern Go | `sort.Slice` instead of `slices.SortFunc` |
| GP-06 | Low | Modern Go | `interface{}` instead of `any` |
| GP-07 | Low | Interface design | Misleading deprecation on `WorldService` |
| GP-08 | Medium | Package org | Duplicated migration runner (~50 lines) |
| GP-09 | Low | Package org | Duplicated `parseMigrationVersion` |
| GP-10 | Medium | Concurrency | `proxyStreams` goroutine leak potential |
| GP-11 | Low | Error handling | Scene error codes as string literals (overlaps CQ-05) |
| GP-12 | Medium | Error handling | Unquoted schema name in DDL |

### Cross-reference with prior findings

| Prior | Status | Notes |
|-------|--------|-------|
| CQ-02 (custom itoa) | Not re-reported | Confirmed in `store.go:338-343` |
| CQ-05 (magic strings) | GP-11 confirms overlap in scene plugin | Different location than CQ-05 if CQ-05 was hostfunc-focused |
| F-02 (internal/ import from plugins/) | Not re-reported | Confirmed: `plugins/core-scenes/service.go` imports `internal/idgen` |
| CQ-18 (CapabilityRegistry not thread-safe) | GP-13 confirms | Severity appropriate |
| CQ-17 (deprecated interface in new code) | GP-07 refines | The deprecation notice itself is misleading |
