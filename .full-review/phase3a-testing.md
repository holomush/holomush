# Phase 3a: Testing Strategy & Coverage Review

## Summary

The PR has solid unit test coverage for new infrastructure components and
hostfunc capability modules. The capability tests use well-crafted narrow
interface mocks and cover both happy and error paths. However, there are
significant gaps in integration/E2E testing, the Lua plugin rewrites have
zero dedicated tests, and several security-relevant behaviors identified in
Phase 2 lack test coverage.

---

## 1. Coverage of New Infrastructure

| Component | Has Tests | Coverage Assessment |
|-----------|-----------|---------------------|
| `registry.go` (ServiceRegistry) | Yes | Good — register, resolve, deregister, list, duplicate rejection |
| `registered_service.go` | Yes | Adequate — metadata storage, IsServerInternal |
| `dependency.go` (DAG resolver) | Yes | **Excellent** — circular deps, unsatisfied deps, diamond deps, duplicate providers, server services, manifest dependencies |
| `grpc_proxy.go` (GRPCServiceProxy) | Yes | **Weak** — only tests constructor, Handler() non-nil, extractServiceName, rawMessage. No test of actual proxy forwarding behavior |
| `inprocess_conn.go` | Yes | Adequate — interface satisfaction, unknown method error, idempotent close |
| `schema_provisioner.go` | Yes | **Partial** — tests naming, conn string scoping, constructor, no-op close. No test of actual schema creation with DB |
| `goplugin/host.go` (service injection) | Yes | **Excellent** — load/unload cycle, error paths, path traversal, symlink escape, context cancellation, duplicate, permissions, Init delegation |
| `pkg/plugin/service.go` (ServiceProvider) | Yes | Good — nil panics, GRPCServer registration, Init delegation, error propagation |
| `pkg/plugin/storage/storage.go` | Yes | Adequate — conn string parsing, migration version parsing |
| `hostfunc/capability.go` (CapabilityRegistry) | Yes | Good — register, get, inject required, list, skip unknown |

### T-01: GRPCServiceProxy lacks forwarding test
**Severity:** Medium

The proxy's core behavior — intercepting gRPC calls and forwarding them to
plugin connections — has zero test coverage. Only the helper function
`extractServiceName` and `rawMessage` round-trip are tested.

**Untested paths:**
- Proxy forwarding a unary call to a registered service
- Proxy returning Unimplemented for unknown service
- Proxy returning Unavailable for unhealthy service (line 52-54)
- Proxy returning Unavailable for nil connection (line 48-50)
- Error propagation from upstream stream creation (line 62-64)

**Recommendation:** Create a test with a real gRPC server using the proxy's
`Handler()` option, register a mock service via the registry with a bufconn
`ClientConnInterface`, and verify call forwarding. Add negative cases for
unhealthy and nil-conn scenarios.

```go
func TestGRPCServiceProxy_ForwardsCallToRegisteredService(t *testing.T) {
    reg := NewServiceRegistry()
    // Register a service with a bufconn connection
    // Make a call through the proxy
    // Assert it reaches the backend
}

func TestGRPCServiceProxy_ReturnsUnavailableWhenServiceUnhealthy(t *testing.T) {
    // Register service with Health.Healthy() returning false
}
```

### T-02: SchemaProvisioner has no integration test with PostgreSQL
**Severity:** Medium

The provisioner's core operation (`ProvisionSchema`) — creating schemas,
running migrations, returning scoped connection strings — is only tested
at the unit level for string manipulation. The actual `CREATE SCHEMA IF NOT
EXISTS` and migration execution paths are untested.

**Recommendation:** Add an integration test using testcontainers that:
1. Creates a schema via `ProvisionSchema`
2. Verifies the schema exists in `pg_catalog.pg_namespace`
3. Verifies migrations ran (check `schema_migrations` table)
4. Calls `ProvisionSchema` again for the same plugin (idempotent)
5. Verifies scoped connection string works

---

## 2. Test Quality in Capability Modules

All four capability modules (`cap_alias`, `cap_property`, `cap_session`,
`cap_world_query`) follow an exemplary testing pattern:

**Strengths:**
- Compile-time interface satisfaction checks (`var _ Capability = ...`)
- Narrow interface mocks (hand-crafted, specific to each capability)
- Both happy path and error path for every function
- Lua table structure verification (fields, types)
- Call capture on mocks to verify argument forwarding
- Empty/nil collection handling (empty tables, nil results)

**No findings** — the capability module tests are well-designed.

---

## 3. Scene Plugin Test Gaps

### T-03: No ownership check tests for EndScene and InviteToScene (SEC-02)
**Severity:** High

`EndScene` (lines 137-164) and `InviteToScene` (lines 216-239) perform
no ownership verification — any caller can end any scene or invite to any
scene. The test file only validates input (empty field rejection) and proto
conversion. There are zero tests asserting that a non-owner caller receives
`PermissionDenied`.

This is a **test gap directly corresponding to SEC-02**. The bug exists in
production code, and no test would catch it if ownership checks were added
(or broken later).

**Recommendation:**
```go
func TestEndSceneRejectsNonOwner(t *testing.T) {
    store := newMockStore()
    store.GetSceneReturns(&SceneRow{ID: "scene-1", OwnerID: "owner-1", State: "active"})
    svc := NewSceneServiceImpl(store)

    _, err := svc.EndScene(t.Context(), &scenev1.EndSceneRequest{
        SessionId: "not-the-owner",
        SceneId:   "scene-1",
    })
    requireGRPCCode(t, err, codes.PermissionDenied)
}

func TestInviteToSceneRejectsNonOwner(t *testing.T) { ... }
```

### T-04: No tests for error sanitization at gRPC boundary (SEC-04)
**Severity:** Medium

The `mapStoreError` function has a fallback path at line 370:
```go
return status.Errorf(codes.Internal, "%s failed: %v", operation, err)
```
This leaks the raw error (which may contain database details) to the caller.
No test verifies that unmatched oops codes produce sanitized messages.

**Recommendation:**
```go
func TestMapStoreErrorDoesNotLeakRawError(t *testing.T) {
    rawErr := errors.New("pq: password authentication failed for user 'holomush'")
    result := mapStoreError(rawErr, "get_scene")
    assert.NotContains(t, result.Error(), "password")
    assert.NotContains(t, result.Error(), "pq:")
}
```

### T-05: No integration test for scene store with real database
**Severity:** Medium

`store_test.go` only tests struct field assignment and embedded filesystem.
The only DB-touching test (`TestSceneStoreRequiresDatabase`) just verifies
connection failure to a bogus address. There are no tests verifying:
- Scene CRUD operations against PostgreSQL
- Participant management
- ListScenes pagination
- Schema isolation via `search_path`

**Recommendation:** Add testcontainers-based integration tests for SceneStore
(tagged `//go:build integration`).

### T-06: No test for ListScenes limit upper bound (SEC-11)
**Severity:** Low

`ListScenes` defaults to 50 when `limit <= 0` but has no upper cap. If a
client passes `limit=1000000`, the store will attempt to return that many rows.
No test verifies limit bounding.

**Recommendation:**
```go
func TestListScenesCapsLimitAt200(t *testing.T) {
    svc := NewSceneServiceImpl(mockStore)
    _, err := svc.ListScenes(t.Context(), &scenev1.ListScenesRequest{Limit: 1000})
    // After fix: verify store.ListScenes was called with limit <= 200
}
```

### T-07: No transaction atomicity test for CreateScene (PERF-7d)
**Severity:** Low

`CreateScene` performs two separate writes (`CreateScene` + `AddParticipant`)
without a transaction. No test verifies that if `AddParticipant` fails, the
scene is rolled back (it won't be, because there is no transaction).

---

## 4. Test Pyramid

| Level | Count | Components |
|-------|-------|------------|
| Unit tests | ~200+ | All infrastructure, hostfuncs, capabilities, goplugin host, manager, SDK, scene service validation, world gRPC server |
| Integration tests (Ginkgo) | ~15 files | Echo-bot, communication, help plugin (pre-existing); world repo/scene repo (pre-existing) |
| E2E tests | 1 file | Telnet E2E (`test/integration/telnet/e2e_test.go`) |

### T-08: No E2E test for binary plugin path
**Severity:** High

The entire binary plugin flow — discovery, DAG resolution, subprocess launch,
gRPC handshake, service registration, proxy forwarding, Init RPC with schema
provisioning — is tested only via unit tests with mocks. There is no
integration or E2E test that actually launches a binary plugin subprocess and
verifies end-to-end gRPC communication.

This is the most significant gap given this PR's scope. The binary plugin
path is the primary new architectural feature.

**Recommendation:** Add an integration test that:
1. Builds `core-scenes` plugin binary (or a test-only minimal binary)
2. Launches it via `goplugin.Host.Load()`
3. Verifies Init RPC is called
4. Makes a `CreateScene` call through the gRPC proxy
5. Verifies the response

This requires `//go:build integration` and Docker for PostgreSQL.

### T-09: No tests for Lua plugin rewrites
**Severity:** High

Five Lua plugins were rewritten from compiled-in Go to Lua:
- `core-communication/main.lua` (18KB)
- `core-building/main.lua`
- `core-objects/main.lua`
- `core-aliases/main.lua`
- `core-help/main.lua`

None have dedicated unit or integration tests. The existing integration tests
in `communication_integration_test.go` and `help_integration_test.go` reference
old directory names (`communication`, not `core-communication`) and test the
pre-existing plugin structure, not the rewritten Lua code.

The capability modules they call are well-tested, but the Lua glue code —
command parsing, event routing, argument validation, output formatting — is
untested.

**Recommendation:** Add integration tests for each rewritten Lua plugin,
loading the actual `main.lua` and exercising command handling. At minimum:
- `core-communication`: say, pose, emit, whisper, page commands
- `core-building`: dig, open, describe
- `core-help`: help, help <topic>

---

## 5. Edge Cases

### T-10: DAG circular dependency detection — TESTED
The `dependency_test.go` file has an explicit test for circular dependencies
(lines 37-44). It also tests diamond dependencies and unsatisfied requires.
**No finding.**

### T-11: Plugin load failure rollback — NOT TESTED
**Severity:** Medium

When `manager.LoadAll()` encounters a load failure for one plugin, no test
verifies that:
- Previously loaded plugins remain functional
- The failed plugin is not listed in `ListPlugins()`
- Service registrations for the failed plugin are rolled back

`TestManagerLoadAllFailsOnLuaSyntaxError` verifies LoadAll succeeds (skips the
bad plugin), but doesn't verify rollback of any partial service registrations.

**Recommendation:**
```go
func TestManagerLoadAllRollsBackServiceRegistrationOnFailure(t *testing.T) {
    // Plugin A provides svc-x (loads OK)
    // Plugin B provides svc-y (Load fails)
    // Verify svc-y is NOT registered, svc-x IS registered
}
```

### T-12: gRPC proxy with unhealthy service — NOT TESTED
**Severity:** Medium

Lines 52-54 of `grpc_proxy.go` check `svc.Health.Healthy()` and return
`codes.Unavailable`. This path has zero test coverage. (See T-01.)

### T-13: Schema provisioner with existing schema — NOT TESTED
**Severity:** Low

Idempotent schema creation (`CREATE SCHEMA IF NOT EXISTS`) is untested.
Needs integration test with real DB. (See T-02.)

### T-14: InProcessConn close behavior — TESTED
`TestInProcessConnCloseIsIdempotent` verifies double-close doesn't panic.
**No finding.**

---

## 6. Missing Tests for Security Findings

| Security Finding | Test Exists? | Gap |
|-----------------|-------------|-----|
| SEC-02 (ownership checks) | **No** | No test asserts PermissionDenied for non-owner EndScene/InviteToScene (T-03) |
| SEC-04 (error leakage in scene service) | **No** | `mapStoreError` fallback leaks raw errors; no sanitization test (T-04) |
| SEC-05 (error leakage in WorldService) | **Partial** | `mapWorldError` uses `%v` on oops errors. grpc_server_test covers NotFound/PermissionDenied but not the Internal fallback path that leaks details |
| SEC-07 (proxy leaks service names) | **No** | Proxy error messages include `serviceName` and upstream errors; no test verifies sanitization |
| SEC-11 (ListScenes unbounded limit) | **No** | No upper bound test (T-06) |

### T-15: WorldService mapWorldError leaks oops context on Internal path
**Severity:** Medium

Lines 155-156 and 167-168 of `grpc_server.go`:
```go
return status.Errorf(codes.Internal, "%v", err)
```
When `err` is an oops error, `%v` includes context key-value pairs and
potentially stack traces. The existing tests only exercise `NotFound` and
`PermissionDenied` branches, not the `Internal` fallback.

**Recommendation:**
```go
func TestMapWorldErrorDoesNotLeakInternalDetails(t *testing.T) {
    err := oops.With("query", "SELECT * FROM locations").
        With("host", "10.0.0.5").
        Errorf("connection refused")
    grpcErr := mapWorldError(err)
    st, _ := status.FromError(grpcErr)
    assert.NotContains(t, st.Message(), "10.0.0.5")
    assert.NotContains(t, st.Message(), "SELECT")
}
```

---

## 7. Lua Plugin Tests

### Testing strategy for Lua plugins

The project uses two strategies for Lua testing:
1. **Capability module unit tests** (hostfunc/cap_*_test.go) — test the
   Go-to-Lua bridge functions in isolation with mock backends
2. **Integration tests** (integration_test.go, communication_integration_test.go,
   help_integration_test.go) — load actual Lua files and exercise via
   `DeliverEvent`/`DeliverCommand`

The capability module tests are thorough. The integration tests exist only for
pre-existing plugins (`echo-bot`, `communication`, `help`) and reference old
directory paths.

### T-16: No benchmarks for Lua event delivery (PERF-3/8e)
**Severity:** Low

Per-delivery Lua VM overhead (source re-parsing, string copying) was flagged as
PERF-3 and PERF-8e. There are zero benchmark tests (`Benchmark*` functions) in
the entire plugin directory. Without benchmarks, the performance impact cannot
be measured or regression-tested.

**Recommendation:**
```go
func BenchmarkLuaDeliverEvent(b *testing.B) {
    host := pluginlua.NewHost()
    // Load a minimal plugin
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        host.DeliverEvent(ctx, "echo", event)
    }
}
```

---

## Findings Summary

| ID | Severity | Category | Description |
|----|----------|----------|-------------|
| T-01 | Medium | Infrastructure | GRPCServiceProxy lacks forwarding/error path tests |
| T-02 | Medium | Infrastructure | SchemaProvisioner has no integration test with PostgreSQL |
| T-03 | **High** | Security | No ownership check tests for EndScene/InviteToScene (SEC-02) |
| T-04 | Medium | Security | No error sanitization test for mapStoreError fallback (SEC-04) |
| T-05 | Medium | Integration | No integration test for scene store with real database |
| T-06 | Low | Security | No test for ListScenes limit upper bound (SEC-11) |
| T-07 | Low | Correctness | No transaction atomicity test for CreateScene (PERF-7d) |
| T-08 | **High** | E2E | No E2E test for binary plugin path |
| T-09 | **High** | Coverage | No tests for 5 rewritten Lua plugins (18KB+ of new code) |
| T-10 | -- | Edge case | DAG circular dependency: tested (no finding) |
| T-11 | Medium | Correctness | Plugin load failure rollback: service registration not verified |
| T-12 | Medium | Edge case | gRPC proxy unhealthy service path: untested |
| T-13 | Low | Edge case | Schema provisioner idempotent creation: untested |
| T-14 | -- | Edge case | InProcessConn close: tested (no finding) |
| T-15 | Medium | Security | WorldService mapWorldError leaks oops context on Internal path |
| T-16 | Low | Performance | No benchmarks for Lua event delivery overhead |

**By severity:** 3 High, 7 Medium, 4 Low, 2 No Finding
