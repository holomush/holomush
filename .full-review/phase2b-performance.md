# Phase 2b: Performance Analysis

PR #192 -- Proto-first plugin architecture rework.

Reviewer focus: latency overhead in new call paths, serialization cost, database connection usage, contention, algorithmic cost, query patterns, and memory.

---

## Finding 1: InProcessConn Serialization Overhead for WorldService

**Severity:** Medium (latency) / Low (throughput)
**Location:** `internal/plugin/inprocess_conn.go`, `internal/plugin/setup/world_conn.go`, `internal/world/grpc_server.go`

**Analysis:**

Every Lua plugin that calls `holomush.query_location()`, `holomush.query_character()`, etc. now traverses this path:

```text
Lua hostfunc -> Go world.Service call (direct, in-process)
```

However, the WorldService is *also* registered as an InProcessConn for binary plugins:

```text
Binary plugin -> gRPC marshal -> bufconn -> gRPC unmarshal -> GRPCServer -> world.Service -> marshal response -> bufconn -> unmarshal response
```

The binary plugin path pays a full proto marshal/unmarshal round-trip even though caller and callee are in the same process. For a `GetLocation` call, this means:

1. Proto marshal `GetLocationRequest` (~50-100ns)
2. bufconn write through 1 MiB ring buffer (~200ns)
3. gRPC framing and HTTP/2 overhead in bufconn (~1-2us)
4. Proto unmarshal `GetLocationRequest` on server side (~50-100ns)
5. Actual `world.Service.GetLocation()` call
6. Proto marshal `GetLocationResponse` (~100-200ns)
7. bufconn return path (~1-2us)
8. Proto unmarshal `GetLocationResponse` on client side (~100-200ns)

**Estimated overhead:** ~5-10us per call vs. ~200ns for a direct function call. This is a ~25-50x multiplier on the call overhead (not counting the actual DB query).

**Impact:** For the scene plugin (the only binary plugin), this is acceptable because scene operations are infrequent (create/join/end scene = low QPS). If a future binary plugin makes world queries in a hot path (e.g., per-command or per-event), this becomes significant.

**Mitigation:** The architecture is correct for the current use case. The bufconn size (1 MiB) is generous. The key observation is that Lua plugins bypass this entirely -- they use direct hostfunc calls to `world.Service`, which is the right design for the high-frequency path.

**Recommendation:** No action needed now. Add a `BenchmarkInProcessConnLatency` benchmark so future regressions are caught. Document that binary plugins should minimize world query frequency, or consider a caching layer if a hot-path binary plugin emerges.

---

## Finding 2: gRPC Proxy Stream Handling -- Goroutine per Proxied Call

**Severity:** Low
**Location:** `internal/plugin/grpc_proxy.go:82-112`

**Analysis:**

`proxyStreams()` spawns a goroutine per proxied request to handle the bidirectional copy. For unary calls (which all current scene RPCs are), this means:

1. Goroutine A (spawned): reads client request, sends to plugin, closes send
2. Main goroutine: reads plugin response, sends to client

The `rawMessage` type avoids deserialization -- bytes pass through as-is. This is the standard gRPC proxy pattern and is efficient for the bytes themselves.

**Overhead per call:**

- 1 goroutine allocation (~2-4KB stack)
- 2 `rawMessage` allocations (reuses the byte slices from gRPC)
- 1 channel read on `errCh`

**Contention profile:** The `errCh` is buffered(1), so the goroutine never blocks on send. The main goroutine blocks on `<-errCh` only after the response stream ends, which is correct.

**Risk:** Under high concurrency (e.g., 1000 concurrent scene RPCs), this creates 1000 goroutines. Go handles this fine. No goroutine leak -- the spawned goroutine exits when `srv.RecvMsg` returns error (EOF from client or cancellation).

**Recommendation:** No action needed. The implementation is clean and follows established gRPC proxy patterns (similar to `grpc-go/proxy`).

---

## Finding 3: Lua VM Per-Delivery Cost with Capability Injection

**Severity:** Medium (but not yet active)
**Location:** `internal/plugin/lua/host.go:118-191`, `internal/plugin/hostfunc/functions.go:104-153`, `internal/plugin/hostfunc/capability.go:42-48`

**Analysis:**

Every `DeliverEvent` and `DeliverCommand` for a Lua plugin:

1. Creates a fresh `lua.LState` (~50us per gopher-lua benchmarks)
2. Sets context on the state
3. Calls `hostFuncs.Register(L, name, requires...)` which:
   a. Calls `RegisterStdlib(ls)` -- registers `holo.fmt.*` and `holo.emit.*` globals
   b. Optionally registers `holo.session.*` functions
   c. Creates `holomush` table with ~14 functions (log, kv_get/set/delete, query_location/character/object, create_location/exit/object, find_location, set_property, get_property, list_commands, get_command_help)
   d. Calls `capabilities.InjectRequired(ls, requires, pluginName)` -- iterates requires list and calls `Register` on each matching capability module
4. Executes `L.DoString(code)` -- parses and compiles plugin source code every time
5. Looks up and calls the handler function
6. Closes the Lua state

**Cost breakdown per delivery:**

| Step | Estimated Cost | Notes |
|------|---------------|-------|
| NewState | ~50us | Allocates LState, initializes standard libs |
| RegisterStdlib | ~5us | Table creation + field setting |
| Register 14 holomush functions | ~10us | Each NewFunction + SetField is ~700ns |
| InjectRequired (per capability) | ~5-15us each | AliasCapability: 7 functions, SessionCapability: 5 functions, PropertyCapability: 3 functions, WorldQueryCapability: 2 functions |
| DoString(code) | ~20-100us+ | Depends on plugin size; parses Lua source each time |
| Handler execution | variable | The actual plugin logic |
| Close state | ~5us | GC of Lua objects |

**Total per-delivery overhead: ~100-200us** (before actual plugin logic).

The `DoString(code)` call is the most expensive part -- it re-parses the Lua source text into bytecode on every single event delivery. gopher-lua supports `CompileString` which returns a `*FunctionProto` that can be reused, avoiding re-parsing.

**Current state:** The `CapabilityRegistry` is wired but **no capabilities are actually registered** in `subsystem.go` yet. The `capRegistry` is created empty and passed to `hostfunc.Functions`. When capabilities are registered (planned), each will add 5-15us of injection cost per delivery.

With 4 capabilities injected (alias: 7 fns, session: 5 fns, property: 3 fns, world_ext: 2 fns = 17 additional functions), the per-delivery overhead rises to ~150-250us.

**Recommendation (P1 -- pre-optimization for when scale matters):**

1. **Cache compiled Lua bytecode.** Change `Load()` to `CompileString()` the code into a `*FunctionProto` and store that instead of raw source. In `DeliverEvent`, use `L.Push(L.NewFunctionFromProto(proto))` then `L.PCall()`. This eliminates the ~20-100us parse cost per delivery.

2. **Consider LState pooling.** gopher-lua supports `sync.Pool` for LState reuse. Since plugins share the same host function set, a pool of pre-configured states (with holomush table already registered) would amortize the registration cost. Must be careful with state isolation -- reset globals between uses.

---

## Finding 4: Database Connection Pool Proliferation

**Severity:** Medium
**Location:** `internal/plugin/schema_provisioner.go`, `plugins/core-scenes/store.go`, `pkg/plugin/storage/storage.go`

**Analysis:**

The system opens multiple independent `pgxpool.Pool` instances:

| Pool | Purpose | Default Max Conns | Location |
|------|---------|-------------------|----------|
| Main application pool | Core DB operations | pgxpool default (4 per CPU) | `internal/store/` |
| SchemaProvisioner pool | DDL: `CREATE SCHEMA IF NOT EXISTS` | pgxpool default (4 per CPU) | `internal/plugin/schema_provisioner.go:32` |
| SceneStore pool | Scene CRUD via `storage.Connect()` | pgxpool default (4 per CPU) | `plugins/core-scenes/store.go:60` |

On a 4-core machine, pgxpool defaults to `max_conns = 4 * num_cpu = 16` per pool. Three pools = 48 potential connections.

PostgreSQL default `max_connections = 100`. After accounting for superuser reserved connections (3) and system processes, ~48 connections from pools alone consumes half the budget.

**Additional concerns:**

- The SchemaProvisioner pool stays open for the entire server lifetime but is only used during plugin startup (schema creation). It holds connections idle indefinitely.
- Each future binary plugin with `storage: postgres` will open another pool via `storage.Connect()`.
- The scene plugin's pool connection string is scoped to its schema (`search_path=plugin_core_scenes`) but uses the same PostgreSQL server.

**Recommendation (P2):**

1. **Close SchemaProvisioner pool after startup.** It's only needed during `LoadAll`. Add a `DoneProvisioning()` method that closes the pool, or restructure so the pool is opened/closed within `ProvisionSchema`.

2. **Configure explicit pool sizes.** The storage SDK's `Connect()` should accept pool configuration. Binary plugins should use small pools (2-4 connections) since they handle low-throughput operations.

3. **Document connection budget.** Add a comment in `core.go` or the operations guide noting total connection consumption: main(16) + provisioner(16) + N*plugin(16). Operators need to tune `max_connections` accordingly.

---

## Finding 5: ServiceRegistry Contention Under High Load

**Severity:** Low
**Location:** `internal/plugin/registry.go`

**Analysis:**

The `ServiceRegistry` uses `sync.RWMutex`. The gRPC proxy calls `Resolve()` on every proxied request, which takes a read lock.

```go
func (r *ServiceRegistry) Resolve(name string) (*RegisteredService, error) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    svc, ok := r.services[name]
    // ...
}
```

**Contention profile:**

- Read path (Resolve): called per proxied gRPC request. Multiple readers can proceed concurrently.
- Write path (Register/Deregister): called only during plugin load/unload (startup and admin commands). Extremely rare.

Since the write path is effectively startup-only, this is a read-dominated workload. `sync.RWMutex` with Go 1.25's improvements handles this well. At 10,000 concurrent scene RPCs, the RLock contention is negligible (~10ns per lock acquire).

**The `Resolve` method returns a pointer to a copy:**

```go
svc, ok := r.services[name]
// ...
return &svc, nil  // copies RegisteredService, takes address
```

This allocates a new `RegisteredService` on every call (escapes to heap). Since `RegisteredService` contains a `grpc.ClientConnInterface` (interface = 2 words) and strings, this is a ~80-byte allocation per proxied call.

**Recommendation (P3, if hot):** Return `RegisteredService` by value instead of pointer, or cache resolved services in the proxy (the set is static after startup). A `sync.Map` could also eliminate the lock entirely for the read path.

---

## Finding 6: DAG Resolution Cost

**Severity:** Negligible
**Location:** `internal/plugin/dependency.go`

**Analysis:**

`ResolveDependencyOrder` implements Kahn's algorithm. Called from `Manager.resolveLoadOrder()` which is called from `LoadAll()`.

**When is it called?**

- Once at startup during `LoadAll()`.
- NOT during dynamic reload (no hot-reload mechanism exists).

**Time complexity:** O(N + M) where N = number of plugins, M = total edges (requires + dependencies). With the current plugin set (5 Lua + 1 binary = 6 plugins), this is trivial.

**Space complexity:** O(N + M) for the adjacency list and in-degree map.

**Queue implementation note:** The queue uses slice-based dequeue (`queue[0]; queue = queue[1:]`), which is O(N) for each pop due to slice header adjustment. For 6 plugins this is irrelevant. At 1000 plugins it would be measurable but still fast (use a ring buffer if it matters).

**Recommendation:** No action needed. This is a correct, efficient implementation for the scale it operates at.

---

## Finding 7: Scene Store Query Patterns

**Severity:** Medium (N+1 pattern), Low (query building)
**Location:** `plugins/core-scenes/store.go`, `plugins/core-scenes/service.go`

### 7a: N+1 Query in GetScene and CreateScene

`GetScene` RPC (service.go:91-108) executes:

1. `store.GetScene(ctx, id)` -- 1 query
2. `store.ListParticipants(ctx, id)` -- 1 query

This is 2 queries per GetScene, not N+1. Acceptable.

`CreateScene` RPC (service.go:32-88) executes:

1. `store.CreateScene()` -- 1 INSERT
2. `store.AddParticipant()` -- 1 UPSERT
3. `store.ListParticipants()` -- 1 SELECT

3 queries, not transactional. If `AddParticipant` fails, the scene exists without an owner participant. This is a correctness issue more than performance, but noted.

### 7b: N+1 in CastPublishVote

`CastPublishVote` (service.go:242-273):

1. `store.ListParticipants(ctx, sceneID)` -- fetches ALL participants
2. Linear scan to find the voter
3. `store.AddParticipant(ctx, found)` -- upsert the vote

This fetches all participants just to update one. With 20 participants in a scene, it returns 20 rows to find 1.

**Fix:** Add a `GetParticipant(ctx, sceneID, characterID)` method that queries by the composite primary key directly:

```sql
SELECT ... FROM scene_participants WHERE scene_id = $1 AND character_id = $2
```

### 7c: Dynamic Query Building in ListScenes

`ListScenes` (store.go:210-259) builds SQL dynamically with string concatenation:

```go
query += " AND state = $" + itoa(argIdx)
```

The `itoa` function is a recursive custom implementation. For single-digit parameter indices (which is all that's needed here), it's fine. The string concatenation creates intermediate strings but this is a cold path.

**Missing index concern:** `ListScenes` filters by `state` AND `visibility` with `ORDER BY created_at DESC`. The migration creates `idx_scenes_state` on `state` alone. A query filtering `WHERE visibility = 'open' ORDER BY created_at DESC` would benefit from a composite index:

```sql
CREATE INDEX idx_scenes_visibility_created ON scenes(visibility, created_at DESC);
```

However, scene tables are expected to be small (hundreds to low thousands of rows), so sequential scan is fast enough.

### 7d: Missing Transactions

`CreateScene` and `CastPublishVote` perform multiple writes without a transaction. If the server crashes between writes, data is inconsistent. pgx supports `pool.Begin()` for this.

**Recommendation:**

- P2: Add `GetParticipant` to eliminate the ListParticipants fetch in CastPublishVote
- P3: Wrap multi-write operations in transactions
- P3: Consider composite index on `(visibility, created_at DESC)` if ListScenes becomes a hot path

---

## Finding 8: Memory and Resource Leaks

**Severity:** Low (no leaks found, one allocation concern)
**Location:** Various

### 8a: Goroutine Lifecycle in proxyStreams

The goroutine in `proxyStreams` (grpc_proxy.go:85-98) exits when `srv.RecvMsg` returns an error (EOF or context cancellation). The `errCh` is buffered(1) so the goroutine never blocks on send even if the main goroutine returns first. No leak.

### 8b: InProcessConn Goroutine

`NewInProcessConn` (inprocess_conn.go:32-37) spawns a goroutine for `srv.Serve(lis)`. This goroutine exits when `lis.Close()` is called in `InProcessConn.Close()`. The `Close()` method is called in `PluginSubsystem.Stop()`. No leak.

### 8c: bufconn Buffer Size

The bufconn listener uses a 1 MiB buffer (`inProcessBufSize = 1 << 20`). One InProcessConn is created (for WorldService). This is a fixed 1 MiB allocation at startup. Acceptable.

If future work adds more InProcessConn instances (one per server-internal service), each adds 1 MiB. Worth monitoring but not a concern at current scale.

### 8d: rawMessage Byte Slice Reuse

In `proxyStreams`, each iteration creates a new `rawMessage{}`. The `Unmarshal` method stores the gRPC-provided byte slice directly (`m.data = b`). The `Marshal` method returns it without copy. This is true zero-copy for the proxy path -- no additional allocations beyond what gRPC itself does. Efficient.

### 8e: Lua Plugin Code String Copies

In `lua/host.go:89`, the plugin code is stored as a `string`. In `DeliverEvent` (line 127), it's read under RLock. The `L.DoString(code)` call will copy the string into Lua's internal representation. This is a per-delivery copy of the entire plugin source. For a 10KB plugin, that's 10KB allocated and copied per event delivery.

**Recommendation (P2):** Compile to bytecode once (see Finding 3). This eliminates the source string copy and the parse cost.

### 8f: Unbounded Collections

- `ServiceRegistry.services` map: bounded by number of plugins, which is bounded by filesystem discovery. Not a concern.
- `Manager.loaded` map: same bound. Not a concern.
- `ListScenes` result slice: bounded by the `limit` parameter (default 50). Not a concern.
- `proxyStreams` has no buffering -- it processes one message at a time. Not a concern.

### 8g: OTel Middleware Allocations

The `HostMiddleware` (otel_middleware.go) creates `[]attribute.KeyValue` slices on every `DeliverCommand` and `DeliverEvent` call (lines 101-103, 141-143). These are 2-element slices that escape to heap. At high command throughput, this contributes to GC pressure.

**Recommendation (P3):** Pre-allocate attribute key constants and use `metric.WithAttributeSet` with cached attribute sets per plugin name.

---

## Summary

| # | Finding | Severity | Impact | Action |
|---|---------|----------|--------|--------|
| 1 | InProcessConn serialization for binary plugins | Medium | ~5-10us/call overhead | No action (acceptable for scene plugin's low QPS) |
| 2 | Goroutine per proxied gRPC call | Low | ~4KB/call, exits cleanly | No action |
| 3 | Lua VM re-creation + source re-parsing per delivery | Medium | ~100-200us/delivery | P1: Cache compiled bytecode via `CompileString` |
| 4 | Multiple pgxpool.Pool instances | Medium | 48+ connections from 3 pools | P2: Close provisioner after startup, configure pool sizes |
| 5 | ServiceRegistry RLock + heap alloc per Resolve | Low | ~10ns lock + ~80B alloc | P3: Return by value or cache |
| 6 | DAG resolution at startup | Negligible | O(N+M), N=6 | No action |
| 7a | 2-3 queries per scene RPC | Low | Acceptable for low QPS | No action |
| 7b | ListParticipants to update one vote | Medium | Fetches all rows unnecessarily | P2: Add GetParticipant |
| 7c | ListScenes missing composite index | Low | Small tables, not hot | P3: Add index if needed |
| 7d | Multi-write without transaction | Medium | Crash inconsistency risk | P3: Wrap in transactions |
| 8e | Source string copy per delivery | Medium | 10KB+ copy per event | P2: Use compiled bytecode |
| 8g | OTel attribute allocations in hot path | Low | GC pressure at high throughput | P3: Cache attribute sets |

**Overall assessment:** The architecture makes sound trade-offs. The InProcessConn overhead is confined to binary plugins (low QPS), while Lua plugins use direct in-process calls for the hot path. The main performance improvement opportunity is Lua bytecode caching (Finding 3/8e), which would reduce per-delivery overhead by ~30-50%. The connection pool proliferation (Finding 4) is the most operationally important issue to address before adding more binary plugins.
