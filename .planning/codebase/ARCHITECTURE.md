# Architecture

**Analysis Date:** 2026-07-08

## System Overview

```text
┌─────────────────────────────────────────────────────────────┐
│         Protocol / Gateway Layer (translation only)          │
├──────────────────┬──────────────────┬───────────────────────┤
│  telnet server   │  web gateway     │   admin CLI/gRPC       │
│ `internal/telnet`│ `internal/web`   │  `internal/admin`      │
│                  │ `cmd/holomush/gateway.go`                 │
└────────┬─────────┴────────┬─────────┴──────────┬────────────┘
         │ gRPC/ConnectRPC   │                    │
         ▼                   ▼                    ▼
┌─────────────────────────────────────────────────────────────┐
│              Core gRPC Services (`internal/grpc`)             │
│   CoreService, WorldService, SceneService, PluginAuditService │
└────────┬──────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────┐
│   Domain / Command Layer                                     │
│   `internal/command` (Dispatcher) → `internal/world`         │
│   `internal/access` (ABAC engine) → `internal/plugin` (host)  │
└────────┬──────────────────────────────────────────────────────┘
         │ Publish(event)
         ▼
┌─────────────────────────────────────────────────────────────┐
│  EventBus (`internal/eventbus`) — embedded NATS JetStream     │
│  Publisher / Subscriber / HistoryReader                       │
└────────┬──────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────┐
│  Stores: PostgreSQL (`internal/store`, host `events_audit`)   │
│  + plugin-owned audit tables (e.g. `plugin_core_scenes.*`)    │
└─────────────────────────────────────────────────────────────┘
```

## Component Responsibilities

| Component | Responsibility | File |
|-----------|----------------|------|
| Gateway/web | Protocol translation (ConnectRPC ↔ gRPC), connection mgmt, static serving | `internal/web/server.go`, `cmd/holomush/gateway.go` |
| Telnet server | Telnet protocol translation to core gRPC | `internal/telnet/` |
| Core gRPC services | Expose world/scene/plugin-audit RPCs to gateways | `internal/grpc/`, `internal/world/grpc_server.go` |
| Command dispatcher | Parses commands, runs two-layer authz, routes to handlers/plugins | `internal/command/dispatcher.go` |
| ABAC engine | Default-deny policy evaluation (Cedar-aligned) | `internal/access/`, `internal/access/policy/types/types.go` |
| World model | Locations, exits, characters, objects, scenes (host-owned entities) | `internal/world/` |
| Plugin host | Loads/manages Lua + binary plugins, manifest validation, event emission gate | `internal/plugin/host.go`, `internal/plugin/event_emitter.go` |
| EventBus | Publish/Subscribe/History over embedded NATS JetStream | `internal/eventbus/bus.go` |
| Store / migrations | PostgreSQL schema (host + plugin tables) | `internal/store/migrations/` (78 files) |
| Plugins | Domain-specific verbs/services (scenes, building, communication, aliases) | `plugins/core-scenes/`, `plugins/core-building/`, etc. |

## Pattern Overview

**Overall:** Event-sourced core with an ABAC-gated plugin host, fronted by protocol-translation gateways.

**Key Characteristics:**

- Actions produce immutable ordered events; downstream state (world/scene/session) derives from replaying or projecting these events.
- ABAC (`internal/access`) is default-deny; every subject/action/resource triple is evaluated explicitly — there is no implicit allow.
- Plugin runtime symmetry: Lua (gopher-lua) and binary (hashicorp/go-plugin) plugins are treated identically by every host-side trust/gate check (`.claude/rules/plugin-runtime-symmetry.md`), even though they use different wire transports.
- Gateway processes (`internal/web`, `internal/telnet`) never touch the DB or domain services directly — they are pure protocol translators calling core gRPC (`.claude/rules/gateway-boundary.md`).

## Layers

**Protocol/Gateway layer:**

- Purpose: translate wire protocols (telnet, HTTP/ConnectRPC) into gRPC calls against the core server
- Location: `internal/web/`, `internal/telnet/`, `cmd/holomush/gateway.go`
- Contains: connection management, cookie/session/CORS handling (`internal/web/cookie.go`, `internal/web/cors.go`), static file serving (`internal/web/static.go`)
- Depends on: gRPC clients to core server (never internal services directly)
- Used by: SvelteKit web client (`web/`), telnet clients

**Core gRPC services:**

- Purpose: expose the domain surface (world queries/mutations, scenes, plugin audit) as gRPC/ConnectRPC services
- Location: `internal/grpc/`, `internal/world/grpc_server.go`
- Contains: RPC handlers that translate proto requests into domain calls and error codes back to `status.Status`
- Depends on: `internal/world`, `internal/access`, `internal/eventbus`
- Used by: gateway layer, admin CLI (`internal/admin`)

**Command/domain layer:**

- Purpose: parse human/CLI commands, enforce two-layer authorization, execute world mutations, route to plugins
- Location: `internal/command/dispatcher.go`, `internal/world/`
- Contains: `Dispatcher`, command registry, alias cache, focus-redirect table
- Depends on: `internal/access` (ABAC), `internal/plugin` (`PluginCommandDeliverer`)
- Used by: core gRPC handlers, telnet input loop

**ABAC access layer:**

- Purpose: default-deny authorization for every subject/action/resource
- Location: `internal/access/`, `internal/access/policy/`
- Contains: `AccessPolicyEngine` interface (`Evaluate`, `CanPerformAction`), attribute providers (`internal/access/policy/attribute/`)
- Depends on: world/session repositories (via interfaces to avoid import cycles)
- Used by: command dispatcher, world services, plugin host capability interceptor

**Plugin host layer:**

- Purpose: load, sandbox, and mediate all Lua/binary plugin interaction with the host
- Location: `internal/plugin/host.go`, `internal/plugin/event_emitter.go`, `internal/plugin/crypto_manifest.go`
- Contains: manifest parsing/validation (`internal/plugin/config.go`), DAG dependency resolution (`internal/plugin/dependency.go`), emit-type validation (`internal/plugin/emit_type_validator.go`), service registry (`internal/plugin/registry.go`)
- Depends on: `pkg/plugin` (SDK contracts consumed by binary plugins), `internal/eventbus`
- Used by: `plugins/*` binary/Lua plugins, `internal/command` (as `PluginCommandDeliverer`)

**EventBus layer:**

- Purpose: durable, ordered event publish/subscribe/history over embedded NATS JetStream
- Location: `internal/eventbus/bus.go` and siblings
- Contains: `Publisher`/`Subscriber`/`HistoryReader` interfaces, `Delivery`/`SessionStream` abstractions
- Depends on: embedded NATS JetStream, PostgreSQL (`events_audit` fallback)
- Used by: `internal/plugin/event_emitter.go` (emit path), gRPC `Subscribe`/`QueryHistory` handlers

## Data Flow

### Primary Request Path (player command → event)

1. Telnet/web client sends a command string; gateway forwards it via gRPC to core (`internal/telnet/`, `internal/web/server.go`)
2. Core gRPC handler calls `command.Dispatcher` (`internal/command/dispatcher.go`)
3. Dispatcher runs **layer 1**: `engine.CanPerformAction(subject, "execute", "command:<name>", scope)` — a coarse type-level pre-flight check (`internal/access/policy/types/types.go:474`)
4. Dispatcher runs **layer 2** per required capability: `engine.Evaluate(ctx, AccessRequest)` against the specific resource instance
5. On permit, the handler mutates world state (`internal/world/*.go`) and/or routes to a plugin via `PluginCommandDeliverer.DeliverCommand` (`internal/command/dispatcher.go:31-34`)
6. Plugin or host code emits an event through `internal/plugin/event_emitter.go::Emit`, which enforces manifest gates (`actor_kinds_claimable`, `emits`, `crypto.emits`) identically for Lua and binary runtimes
7. `Emit` calls `eventbus.Publisher.Publish` (`internal/eventbus/bus.go:16`), which writes to JetStream with `Nats-Msg-Id` set to the event's ULID for dedup
8. Host-owned subjects are durably audited to PostgreSQL `events_audit`; plugin-owned subjects audit via `PluginAuditService.AuditEvent` to plugin-declared tables (e.g. `plugin_core_scenes.scene_log`)

### Subscribe / History Read Path

1. gRPC `Subscribe` handler calls `eventbus.Subscriber.OpenSession` with session identity and subject filters, returning a `SessionStream`
2. Consumers call `SessionStream.Next` to receive typed `Delivery` handles; `Delivery.MetadataOnly()` signals when the AuthGuard withheld plaintext from an unauthorized recipient
3. `HistoryReader.QueryHistory` transparently falls back from JetStream (recent) to PostgreSQL audit (older than JS retention) — callers never see the boundary

**State Management:**

- World state (locations, characters, objects, scenes) is projected/derived from events, not held as the sole source of truth; `internal/world/mutator.go` and `internal/world/event_store_adapter.go` bridge domain mutations to the event log.
- Plugin state for domain-specific concerns (e.g. scene rosters) is owned entirely by the plugin (`plugins/core-scenes/store.go`), never by `internal/world`.

## Key Abstractions

**EventBus (Publisher / Subscriber / HistoryReader):**

- Purpose: three narrow interfaces for the three consumer roles (emit, live subscribe, historical read)
- Examples: `internal/eventbus/bus.go:16,29,35`
- Pattern: interface segregation — callers depend on the narrowest interface they need; `EventBus` composes all three

**ServiceRegistry (host-side):**

- Purpose: maps fully-qualified proto service names to registered implementations, enforcing single registration
- Examples: `internal/plugin/registry.go:14`
- Pattern: thread-safe map behind `sync.RWMutex`, `SERVICE_ALREADY_REGISTERED`/`SERVICE_NOT_FOUND` typed errors via `oops`

**ServiceProvider (plugin-side SDK):**

- Purpose: implemented by binary plugins to register gRPC services and receive host `Init` configuration
- Examples: `pkg/plugin/service.go:41`
- Pattern: multiplexes plugin-provided services onto the same go-plugin gRPC transport as the core `PluginService`

**AccessPolicyEngine (ABAC):**

- Purpose: single interface for both fine-grained (`Evaluate`) and coarse pre-flight (`CanPerformAction`) authorization checks
- Examples: `internal/access/policy/types/types.go:474`
- Pattern: fail-closed — `CanPerformAction` returns `(false, err)` on infra failure, never a permissive decision; `ErrEngineDegraded` signals degraded mode to callers

**Plugin manifest / loader:**

- Purpose: declares a plugin's type, resource types, required/provided services, capabilities, and crypto/emit gates
- Examples: `plugins/core-scenes/plugin.yaml`, validated by `internal/plugin/config.go`, `internal/plugin/dependency.go` (DAG resolution)
- Pattern: `requires`/`provides` form a dependency DAG resolved at load; `capability:` entries support least-privilege `access:`/`scope:` narrowing (`.claude/rules/plugin-manifest.md`)

## Entry Points

**`cmd/holomush` (main server binary):**

- Location: `cmd/holomush/main.go`, `cmd/holomush/root.go`, `cmd/holomush/core.go`
- Triggers: `holomush` CLI invocation (serve, admin, migrate, plugin subcommands)
- Responsibilities: wires the core server (gRPC services, EventBus, plugin host, ABAC engine), the gateway (`cmd/holomush/gateway.go`), and admin/crypto/migration subcommands (`cmd/holomush/cmd_admin.go`, `cmd/holomush/migrate.go`, `cmd/holomush/cmd_crypto_rekey.go`)

**`cmd/holomush-cutover`:**

- Location: `cmd/holomush-cutover/`
- Triggers: one-shot cutover/migration operational tooling (separate from the long-running server)

**`cmd/inv-render` / `cmd/inv-migrate`:**

- Location: `cmd/inv-render/`, `cmd/inv-migrate/`
- Triggers: invariant registry tooling — renders `docs/architecture/invariants.md` from the YAML source, migrates legacy invariant IDs

**`cmd/lint-plugin-manifests`:**

- Location: `cmd/lint-plugin-manifests/`
- Triggers: CI lint step validating every `plugins/*/plugin.yaml` against `schemas/plugin.schema.json`

**Plugin binaries (`plugins/*/main.go`):**

- Location: e.g. `plugins/core-scenes/main.go`
- Triggers: spawned as separate OS processes by the host via hashicorp/go-plugin when `type: binary`

## Architectural Constraints

- **Threading:** Go's standard goroutine-per-request model under gRPC; `internal/plugin/registry.go` and similar shared registries use `sync.RWMutex` for concurrent access; Lua plugins get a fresh VM state per event delivery (no shared mutable Lua state — see `.claude/rules/references/testing-detail.md`).
- **Global state:** deliberately minimal; service registries and the plugin manager are constructed and owned by `cmd/holomush/core.go`'s dependency wiring (`cmd/holomush/deps.go`) rather than package-level singletons.
- **Circular imports:** `AccessPolicyEngine` is defined in `internal/access/policy/types` (not `internal/access` or `internal/world`) specifically to let both `internal/world` and `internal/access/policy/attribute` depend on it without a cycle (comment at `internal/access/policy/types/types.go:466`). `eventbus.SessionIdentity` is defined in `internal/eventbus` itself (not `authguard`) to avoid `eventbus → authguard → plugin → eventbus`.
- **Runtime symmetry:** any new host-side trust/gate/manifest check MUST apply identically to Lua and binary plugins; asymmetry is only permitted when it is purely a transport difference reaching the same policy chokepoint (`.claude/rules/plugin-runtime-symmetry.md`).

## Anti-Patterns

### Gateway querying data directly

**What happens:** a gateway endpoint (`internal/web/`) adds a DB query, repository lookup, or direct service struct field instead of calling a core gRPC RPC.
**Why it's wrong:** couples the horizontally-scalable gateway process to internal data shapes and breaks the multi-process deployment model.
**Do this instead:** add/extend an RPC on the core server (`internal/grpc/`) and have the gateway call it; the gateway only holds gRPC clients (`.claude/rules/gateway-boundary.md`).

### Structural GUI writes via the command path

**What happens:** a button/form-driven mutation (create/set/end/invite/kick/transfer) is implemented by string-building a `sendCommand`/`HandleCommand` call instead of a typed RPC.
**Why it's wrong:** routes a machine-initiated action through the human/CLI text-command parser, which is reserved for conversational verbs (`pose`, `say`, `ooc`, `join`).
**Do this instead:** add a typed RPC on the BFF facade (e.g. `EndScene`, `InviteToScene`) per ADR `holomush-v4qmu` (`.claude/rules/gateway-boundary.md`).

### Double-translating gRPC status errors

**What happens:** a helper converts `status.Error` ↔ `oops` at an inner layer, then an outer caller wraps and translates again.
**Why it's wrong:** breaks `status.FromError` chain-walking; the inner translation strips the `GRPCStatus()` method so opacity/error-code invariants silently fail.
**Do this instead:** translate exactly once, at the outermost gRPC boundary call site (`.claude/rules/grpc-errors.md`).

## Error Handling

**Strategy:** structured errors via `oops` (`oops.With(k,v).Wrap(err)`, `oops.Code("CODE").Wrap(err)`), never leaking internal error text across a gRPC trust boundary.

**Patterns:**

- gRPC handlers log internally with `errutil.LogErrorContext(ctx, msg, err, ...)` and return a static `status.Errorf(codes.Internal, "internal error")` — never `%v`-interpolating the inner error into the client-visible message (`.claude/rules/grpc-errors.md`)
- ABAC engine failures return `(false, err)`/an infra-failure `Decision` (policy ID prefixed `infra:`), never a permissive result on error

## Cross-Cutting Concerns

**Logging:** `log/slog` via context-carrying variants (`InfoContext`/`WarnContext`/`ErrorContext`) so `trace_id`/`span_id` propagate; built in `internal/logging/handler.go` (`.claude/rules/logging.md`).
**Validation:** plugin manifests validated against `schemas/plugin.schema.json` at load (`internal/plugin/config.go`); event types validated against `verbs[].type` (`internal/plugin/emit_type_validator.go`).
**Authentication:** ABAC subjects are prefixed strings (`character:01ABC`, `session:01XYZ`, `plugin:echo-bot`, `system`) parsed by `access.ParseSubject` (`internal/access/access.go`); auth flows live in `internal/auth/` and `internal/totp/`.

---

*Architecture analysis: 2026-07-08*
