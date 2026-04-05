# Phase 1b: Architecture Review -- PR #192 Plugin Architecture Rework

**Reviewer:** Claude Opus 4.6 (Architectural Review)
**Date:** 2026-04-05
**Spec:** `docs/superpowers/specs/2026-04-05-plugin-architecture-rework-design.md`

---

## Executive Summary

This is a well-designed architectural rework that delivers on its core promise: replacing compiled-in plugin registration with a proto-first, self-contained plugin system. The service registry, DAG dependency resolution, InProcessConn in-memory transport, and gRPC proxy are cleanly implemented and compose well. The scene binary plugin proves the end-to-end path.

There are two structural violations (one High severity around import boundaries, one Medium around code duplication) and several design observations that merit discussion. Overall the architecture is sound and the implementation is faithful to the spec's design decisions D1-D9.

---

## 1. Spec Compliance (D1-D9)

### D1: Proto as universal service contract language -- COMPLIANT

WorldService, SceneService, and PluginService are all proto-defined in `api/proto/`. The service registry stores `grpc.ClientConnInterface` for every service, making the transport layer uniform regardless of in-process vs. subprocess origin. Binary plugins communicate exclusively through proto contracts.

### D2: Plugins as service providers AND consumers -- COMPLIANT

The manifest schema supports both `requires` and `provides`. The scene plugin declares `requires: [holomush.world.v1.WorldService]` and `provides: [holomush.scene.v1.SceneService]`. DAG resolution handles the bidirectional relationship correctly.

### D3: `core` type removed -- COMPLIANT

All five core plugins (communication, building, objects, aliases, help) have been migrated to `type: lua`. The `TypeCore` constant is gone. The manifest validator only accepts `lua`, `binary`, and `setting`.

### D4: Tiered storage -- COMPLIANT

`StorageKV` and `StoragePostgres` are implemented. Manifest validation enforces that `postgres` is binary-only. The `SchemaProvisioner` creates `plugin_<name>` schemas and passes scoped connection strings through the Init RPC.

### D5: No cross-schema database access -- PARTIALLY COMPLIANT

| Aspect | Status |
|--------|--------|
| Schema creation with `CREATE SCHEMA IF NOT EXISTS` | Implemented |
| Connection string scoped via `search_path` | Implemented |
| Schema-scoped PostgreSQL role with restricted permissions | **NOT IMPLEMENTED** |

**Finding F-01 (Medium, Security).** The spec requires "a schema-scoped PostgreSQL role with USAGE and CREATE on `plugin_<name>` schema, no access to `public` schema." The `SchemaProvisioner.ProvisionSchema()` at line 46-55 of `schema_provisioner.go` only runs `CREATE SCHEMA IF NOT EXISTS`. It does not create a restricted role or grant/revoke permissions. The plugin receives a connection string using the server's own database credentials with only `search_path` changed. A malicious or buggy plugin can read/write the `public` schema and any other plugin's schema.

**Recommendation:** This is acceptable for Phase 3 where `core-scenes` is a first-party plugin, but must be addressed before third-party plugins are supported. Create a follow-up issue with P1 priority.

### D6: Server proxies plugin gRPC services -- COMPLIANT

`GRPCServiceProxy` is installed as `grpc.UnknownServiceHandler` on the main gRPC server (line 131 of `sub_grpc.go`). When an RPC arrives for an unregistered service, the proxy resolves it through the service registry and forwards the call. Health checking is included in the proxy path.

### D7: Lua host functions auto-generated from proto -- DEFERRED (acceptable)

The spec acknowledges this as "needing deeper design." The current implementation uses hand-written capability modules (`cap_session.go`, `cap_alias.go`, `cap_property.go`, `cap_world_query.go`) which is the correct intermediate step. The capability module decomposition lays the groundwork for future code generation by establishing the namespace-per-service pattern.

### D8: DAG MUST be a DAG -- COMPLIANT

`ResolveDependencyOrder` implements Kahn's algorithm correctly with proper error codes for all failure modes: `DUPLICATE_SERVICE_PROVIDER`, `UNSATISFIED_REQUIRES`, `UNSATISFIED_DEPENDENCY`, `CIRCULAR_DEPENDENCY`. Server-provided services are correctly excluded from plugin-to-plugin edges.

### D9: Plugin types describe runtime behavior -- COMPLIANT

No new plugin types were introduced. The `requires`/`provides`/`storage` manifest fields describe ecosystem relationships independently of runtime type.

---

## 2. Component Boundaries

### 2.1 `internal/plugin/` separation from `internal/world/`

**Clean.** The `internal/plugin/` package does not import `internal/world/`. The bridge is `internal/plugin/setup/world_conn.go` which lives in the setup sub-package that is allowed to import both.

The `internal/plugin/hostfunc/adapter.go` does import `internal/world` types directly (for `WorldQuerierAdapter`), which is acceptable since hostfuncs are server-internal code, not plugin code.

### 2.2 `pkg/plugin/` public SDK

**Clean and minimal.** The public API surface is:
- `ServeWithServices(config, provider)` -- entry point for binary plugins
- `ServiceProvider` interface -- `RegisterServices` + `Init`
- `ServeConfig`, `Handler`, `CommandHandler` -- existing SDK types
- `pkg/plugin/storage` -- `Connect()`, `RunMigrations()`, `ParseSchemaFromConnString()`

This is the right level of abstraction. Plugin authors see exactly what they need.

### 2.3 goplugin host decoupling

**Clean.** `internal/plugin/goplugin/host.go` depends on `internal/plugin` (for `Manifest`, `Host`, `ServiceConnProvider`, `SchemaProvisioner`) and `pkg/plugin` (for SDK types). It does not import `internal/world/`, `internal/session/`, or other domain packages. The `ClientFactory` interface enables testing without real subprocess execution.

### 2.4 Finding: Scene plugin imports `internal/`

**Finding F-02 (High, Architectural Boundary Violation).**

`plugins/core-scenes/service.go:15` imports `github.com/holomush/holomush/internal/idgen`. This violates the fundamental boundary that plugins (which live in `plugins/` and will become out-of-tree packages) must only import `pkg/` (public API).

```
plugins/core-scenes/service.go:15:	"github.com/holomush/holomush/internal/idgen"
```

This works today because `core-scenes` is in the same Go module, but it creates a structural dependency that:
1. Prevents `core-scenes` from being extracted to its own module
2. Violates D5's spirit of plugin isolation
3. Sets a bad precedent for future binary plugins

**Recommendation:** Either:
- Move `idgen.New()` to `pkg/plugin/idgen` (a thin wrapper around ULID generation), or
- Use `github.com/oklog/ulid/v2` directly in the plugin (it is already a transitive dependency), or
- Add ID generation to the plugin SDK

---

## 3. Dependency Management

### 3.1 Circular dependencies

**None found.** The dependency flow is strictly:

```
cmd/holomush/ -> internal/plugin/setup/ -> internal/plugin/ -> pkg/plugin/
                                        -> internal/plugin/goplugin/
                                        -> internal/plugin/hostfunc/
                                        -> internal/world/ (via setup only)
plugins/core-scenes/ -> pkg/plugin/ (+ internal/idgen violation noted above)
```

### 3.2 `plugins/core-scenes/` import boundary

Aside from the `internal/idgen` violation (F-02), imports are correct:
- `pkg/plugin` -- SDK types
- `pkg/proto/holomush/plugin/v1` -- plugin protocol
- `pkg/proto/holomush/scene/v1` -- service contract
- `pkg/plugin/storage` -- database SDK
- Third-party: `pgx`, `oops`, `grpc`

### 3.3 Service registry as abstraction boundary

**Clean.** The `ServiceRegistry` accepts and returns `RegisteredService` structs containing `grpc.ClientConnInterface`. Consumers never need to know the implementation origin. The registry is a concrete type (not an interface), which is the right choice since there is exactly one implementation and no testing need for alternatives.

---

## 4. API Design

### 4.1 WorldService proto

**Appropriate abstraction.** The service exposes read-only queries (`GetLocation`, `GetCharacter`, `ListCharactersAtLocation`, `ListExits`) which is exactly what plugins need for world model queries. Each RPC includes `subject_id` for server-side ABAC enforcement, preventing plugins from bypassing access control.

**Finding F-03 (Low, API Completeness).** The WorldService is read-only. The spec's section 2.2 mentions it wraps `internal/world.Service`, which also has mutation operations (create location, create exit, etc.). The `core-building` Lua plugin needs world mutations but accesses them through the old hostfunc adapter path (`worldMutator`), not through the proto service. This is fine for now but creates two paths to world mutations -- the Lua hostfunc path and (eventually) the proto path.

**Recommendation:** Track this as a known divergence. When Lua hostfuncs are auto-generated from proto (D7), the WorldService proto will need mutation RPCs and the Lua path will collapse into the proto path.

### 4.2 SceneService proto

**Well-designed.** Covers the full scene lifecycle: create, get, list, end, join, leave, invite, cast vote, get pose order. The message types are flat and proto-idiomatic. String IDs (not ULIDs) are the right choice for proto messages since they cross process boundaries.

**Finding F-04 (Low, Naming Consistency).** The `session_id` field in scene RPCs (e.g., `CreateSceneRequest.session_id`, `EndSceneRequest.session_id`) is used as the character identifier for the acting user. In the rest of the system, the acting character is `character_id`. The scene plugin uses `session_id` as the owner ID in `SceneRow.OwnerID` and as the `CharacterID` in `ParticipantRow`. This creates terminology confusion per the project's terminology table.

**Recommendation:** Rename to `character_id` in the proto and adjust the service implementation. This is a proto change that should happen before the service goes live.

### 4.3 Plugin SDK: ServeWithServices + ServiceProvider

**Good design.** The separation between `ServeConfig.Handler` (event/command handling) and `ServiceProvider` (gRPC service registration + initialization) is clean. A plugin that only handles events uses `Serve()`. A plugin that also provides services uses `ServeWithServices()`. The two interfaces compose without mutual dependency.

The `grpcServicePlugin.GRPCServer()` method registers both `PluginServiceServer` (for the plugin protocol) and the provider's custom services on the same gRPC server. This multiplexing is the correct approach for go-plugin.

### 4.4 Init RPC handshake

**Appropriate pattern.** The Init RPC sends `ServiceConfig` (connection string + required service addresses) after the go-plugin connection is established. This is the right sequence because:
1. go-plugin handshake establishes the gRPC transport
2. Init RPC passes configuration that depends on runtime state (DB connection, service addresses)
3. The plugin can fail initialization cleanly and report errors back

**Finding F-05 (Medium, Incomplete Implementation).** The `ServiceConfig.required_services` map is defined in the proto but never populated by the host. In `goplugin/host.go` line 214-225, only `connection_string` is set. The `required_services` map stays empty. The proto comment says "future use", but the scene plugin declares `requires: [holomush.world.v1.WorldService]` and never receives a connection to it.

This means the scene plugin cannot currently call WorldService. The service contract declaration in the manifest is validated by the DAG resolver but the actual service connection is not injected.

**Recommendation:** This is a known gap (the scene plugin's `HandleCommand` returns a placeholder). For the end-to-end proof, consider at minimum populating the map so the plugin can create a gRPC client to WorldService when needed. Or document this as a Phase 4 follow-up.

---

## 5. Design Patterns

### 5.1 ServiceRegistry as runtime service locator

**Appropriate here.** In general, the Service Locator pattern is an anti-pattern when it replaces dependency injection, because it hides dependencies and makes testing difficult. However, the plugin architecture requires runtime dynamism that DI cannot provide:

- Services register and deregister at runtime (plugin load/unload)
- The set of services is not known at compile time
- The gRPC proxy needs to resolve services by name from incoming requests
- Health checking must be evaluated at call time

The registry is used at exactly two points: (1) the gRPC proxy resolves services for external callers, and (2) the DAG resolver validates service availability. All internal wiring still uses constructor injection. This is the correct use of the pattern.

### 5.2 InProcessConn wrapping grpc.Server

**Clean, not over-engineered.** The implementation is 78 lines. It uses `grpc/test/bufconn` which is a well-tested in-memory transport from the gRPC team. The alternative (creating a custom `grpc.ClientConnInterface` implementation that dispatches directly) would be more code and harder to maintain.

The 1 MiB buffer size is appropriate for world model queries. The `Close()` method properly tears down both the client connection and the listener.

One structural observation: the `InProcessConn` starts a goroutine (`go srv.Serve(lis)`) that runs the gRPC server. The associated `*grpc.Server` is not stored, so `GracefulStop()` cannot be called on shutdown. Instead, `Close()` closes the listener, which causes `Serve()` to return. This is acceptable but means in-flight RPCs are abruptly terminated rather than drained.

### 5.3 GRPCServiceProxy as UnknownServiceHandler

**Elegant and appropriate.** The `UnknownServiceHandler` hook is the standard gRPC extension point for dynamic service routing. The implementation:

1. Extracts the service name from the method path
2. Resolves via the service registry
3. Checks health before forwarding
4. Bidirectionally proxies streams using `rawMessage` for zero-copy pass-through

The `rawMessage` type (Marshal/Unmarshal without deserialization) is the correct pattern for transparent proxying -- the proxy never needs to understand the payload.

**Finding F-06 (Low, Resilience).** The `proxyStreams` function starts a goroutine for client-to-server forwarding but uses a single-element error channel for synchronization. If the goroutine exits with a non-nil error and the main goroutine also encounters an error on `cli.RecvMsg`, the goroutine's error is silently read from the channel but never inspected. This is functionally correct (the stream is being torn down either way) but loses diagnostic information.

### 5.4 Capability modules for Lua hostfuncs

**Good decomposition.** Each capability module (`SessionCapability`, `AliasCapability`, `PropertyCapability`, `WorldQueryCapability`) implements a clean interface:

```go
type Capability interface {
    Namespace() string
    Register(L *lua.LState, pluginName string)
}
```

The `CapabilityRegistry` maps proto service names to modules, and `InjectRequired` selectively registers only the capabilities a plugin declared. This achieves the spec's requirement that undeclared services are not available in the Lua VM.

Each capability defines its own narrow access interface (`SessionAccess`, `AliasAccess`, `PropertyAccess`, `WorldQueryAccess`) using host-internal DTOs, avoiding coupling to domain types. This is the Interface Segregation Principle applied correctly.

---

## 6. Over-Engineering Assessment

### 6.1 SchemaProvisioner

**Justified.** Even without the restricted-role enforcement (F-01), the schema provisioner handles the essential operations: creating schemas, scoping connection strings, and managing the admin pool lifecycle. It is 93 lines. The alternative (inlining this into the goplugin host) would couple schema management to the host implementation.

### 6.2 Binary plugin path for scenes

**Justified.** The scene plugin requires:
- Its own PostgreSQL schema with 4 tables
- Complex domain logic (state machines, participant management, voting)
- A proto service contract other consumers will call

Lua cannot provide any of these. The binary plugin path is the correct choice per the spec's type selection guide.

### 6.3 Simpler alternatives

The architecture is at the right complexity level. I see no unnecessary layers. The key abstractions are:

| Abstraction | Lines | Justification |
|-------------|-------|---------------|
| ServiceRegistry | 77 | Required for dynamic service resolution |
| InProcessConn | 78 | Required for uniform grpc.ClientConnInterface |
| GRPCServiceProxy | 125 | Required for single-port service exposure |
| ResolveDependencyOrder | 153 | Required for DAG validation |
| SchemaProvisioner | 93 | Required for storage isolation |
| CapabilityRegistry | 58 | Required for selective Lua injection |

Total new infrastructure: approximately 584 lines. This is lean for what it delivers.

---

## 7. Simplification Opportunities

### 7.1 Duplicated migration runner

**Finding F-07 (Medium, Code Duplication).**

`plugins/core-scenes/store.go` contains `runMigrationsFromFS()` (lines 88-133) which is a near-copy of `pkg/plugin/storage.RunMigrations()`. The duplication exists because `storage.RunMigrations` takes `embed.FS` but the scene plugin needs `fs.FS` (after `fs.Sub` to strip the `migrations/` prefix).

Both implementations:
- Create the `plugin_migrations` table
- Query the current version
- Iterate `.up.sql` files
- Execute and track each migration

The `parseMigrationVersion` function is also duplicated.

**Recommendation:** Change `pkg/plugin/storage.RunMigrations` to accept `fs.FS` instead of `embed.FS` (which implements `fs.FS`). This eliminates approximately 50 lines of duplication and ensures migration behavior stays consistent.

### 7.2 Manifest `requires` validation vs. capability injection

The manifest declares `requires` for DAG resolution (checked by `ResolveDependencyOrder`), but the actual capability injection for Lua plugins happens through `CapabilityRegistry.InjectRequired`. These are two separate paths using the same data. If a capability module is not registered in the `CapabilityRegistry` for a service that a Lua plugin requires, the DAG resolves successfully but the Lua plugin gets no functions. The current behavior (silently skip unknown services in `InjectRequired`) is documented but could lead to confusion.

**Finding F-08 (Low, Diagnostic Gap).** When a Lua plugin declares `requires: [holomush.session.v1.SessionService]` and the `SessionCapability` is registered in the `CapabilityRegistry`, everything works. But if someone adds a new proto service and forgets to register a capability module, the plugin loads successfully but gets no functions for that service. A warning log at this point would catch configuration mismatches.

### 7.3 Manifest complexity

The manifest schema (`requires`, `provides`, `storage`, `commands`, `capabilities`, `policies`, `dependencies`, `events`, `priority`) is at the right level. Each field serves a distinct purpose. The `dependencies` map (named plugin dependencies with version constraints) and `requires` (service-level dependencies) are complementary, not redundant.

---

## 8. Additional Findings

### F-09 (Low, Graceful Degradation Trade-off)

`Manager.resolveLoadOrder()` falls back from DAG resolution to priority sort when the DAG fails. While this provides resilience during development, in production a failed DAG (e.g., unsatisfied requires) should be a hard error. The current warning log may be missed.

**Recommendation:** Consider a strict mode configuration option: in production, DAG failures should prevent startup. In development, the graceful fallback is useful.

### F-10 (Low, Missing GracefulStop)

The `InProcessConn.Close()` closes the listener and client connection but cannot call `GracefulStop()` on the wrapped `grpc.Server` because it does not retain a reference. In the current usage (WorldService with short queries), this is fine. If streaming RPCs are added to WorldService in the future, in-flight streams would be abruptly terminated on shutdown.

**Recommendation:** Store a reference to `*grpc.Server` in `InProcessConn` and call `GracefulStop()` before closing the listener.

### F-11 (Low, Schema Naming)

`pluginSchemaName()` replaces hyphens with underscores: `core-scenes` becomes `plugin_core_scenes`. This is correct for PostgreSQL identifier rules. However, there is no validation that the resulting schema name does not collide with PostgreSQL reserved words or existing schemas. The `plugin_` prefix makes collision unlikely but not impossible.

---

## Finding Summary

| ID | Severity | Category | Summary |
|----|----------|----------|---------|
| F-01 | Medium | Security | Schema provisioner does not create restricted PostgreSQL roles; plugins get full DB access |
| F-02 | **High** | Boundary | `plugins/core-scenes/service.go` imports `internal/idgen` -- violates plugin isolation |
| F-03 | Low | Completeness | WorldService is read-only; mutations still use old hostfunc path |
| F-04 | Low | Naming | `session_id` in scene proto should be `character_id` per terminology guide |
| F-05 | Medium | Completeness | `ServiceConfig.required_services` map never populated; scene plugin cannot call WorldService |
| F-06 | Low | Resilience | `proxyStreams` goroutine errors are silently discarded |
| F-07 | Medium | Duplication | Migration runner duplicated between `pkg/plugin/storage` and `plugins/core-scenes/store.go` |
| F-08 | Low | Diagnostics | Missing warning when capability module not registered for a declared `requires` service |
| F-09 | Low | Operations | DAG resolution failure falls back silently to priority sort |
| F-10 | Low | Lifecycle | `InProcessConn` cannot graceful-stop the wrapped gRPC server |
| F-11 | Low | Naming | No collision check on generated schema names |

---

## Verdict

**The architecture is sound and the implementation is faithful to the spec.** The proto-first service contract model, DAG dependency resolution, and uniform `grpc.ClientConnInterface` abstraction are well-designed. The capability module decomposition for Lua hostfuncs is a clean intermediate step toward auto-generation.

**Must-fix before merge:** F-02 (internal import from plugin) -- this is a structural violation that should be corrected now, before it becomes a pattern for future plugins.

**Should-fix before merge:** F-07 (migration duplication) -- straightforward cleanup that prevents divergent behavior.

**Track as follow-ups:** F-01 (restricted roles), F-04 (naming), F-05 (service injection), F-09 (strict mode).
