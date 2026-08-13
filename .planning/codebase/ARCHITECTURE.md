---
last_mapped_commit: 0047100ed380c2135d541d1120dd3a950714d2f1
last_mapped_at: 2026-08-13
---
<!-- refreshed: 2026-08-13 -->
# Architecture

**Analysis Date:** 2026-08-13

## System Overview

```text
┌─────────────────────────────────────────────────────────────┐
│                   Clients (out of process)                   │
├──────────────────┬──────────────────┬───────────────────────┤
│  Telnet client   │  SvelteKit PWA   │   holomush CLI /      │
│                  │  `web/src`       │   admin UDS socket    │
└────────┬─────────┴────────┬─────────┴──────────┬────────────┘
         │                  │ ConnectRPC/HTTP    │ Unix socket
         ▼                  ▼                    │
┌─────────────────────────────────────────────────────────────┐
│         GATEWAY process (protocol translation ONLY)          │
│  `cmd/holomush/gateway.go` · `internal/telnet` ·             │
│  `internal/web` (ConnectRPC handlers, static SPA, relays)    │
│  Holds gRPC CLIENTS only — no repos, no DB, no services      │
└────────────────────────────┬────────────────────────────────┘
                             │ gRPC (mTLS)
                             ▼
┌─────────────────────────────────────────────────────────────┐
│           CORE server  `cmd/holomush/core.go`                │
│  gRPC facade `internal/grpc` → command dispatch              │
│  `internal/command` → world `internal/world` → ABAC          │
│  `internal/access` → plugin host `internal/plugin`           │
│  Composed as ordered subsystems (`internal/lifecycle`)        │
└──────┬───────────────────────────┬──────────────────────────┘
       │ publish/subscribe         │ repositories
       ▼                           ▼
┌──────────────────────┐   ┌─────────────────────────────────┐
│ NATS JetStream       │   │ PostgreSQL                      │
│ `internal/eventbus`  │   │ `internal/store` (+ migrations) │
│ hot history + audit  │   │ `internal/world/postgres`       │
│ projection → cold    │──▶│ `events_audit`, plugin schemas  │
└──────────────────────┘   └─────────────────────────────────┘
       │ mTLS gRPC / in-process Lua
       ▼
┌─────────────────────────────────────────────────────────────┐
│  Plugins `plugins/*` — lua | binary (subprocess) | setting   │
│  SDK `pkg/plugin`, host bridge `internal/plugin/{lua,goplugin}` │
└─────────────────────────────────────────────────────────────┘
```

## Component Responsibilities

| Component | Responsibility | File |
|-----------|----------------|------|
| Root CLI | cobra command tree (`core`, `gateway`, `migrate`, `admin`, `plugin`, `audit`, `crypto`, `status`) | `cmd/holomush/root.go` |
| Core server | Composes and starts all server subsystems | `cmd/holomush/core.go` |
| Gateway | Telnet accept loop + web/ConnectRPC serving; gRPC clients only | `cmd/holomush/gateway.go` |
| Subsystem orchestrator | Topological start/stop, health tiers, readiness | `internal/lifecycle/orchestrator.go` |
| gRPC facade | Core RPC surface (command, query, history, characteraccess, sceneaccess, content, focus) | `internal/grpc/server.go` |
| Command dispatch | Parse → ABAC → builtin handler or plugin delivery, with focus redirects, rate limits, audit | `internal/command/dispatcher.go` |
| World model | Locations, exits, characters, objects, properties, scenes; ABAC-checked service | `internal/world/service.go` |
| World persistence | Repositories + transactional outbox | `internal/world/postgres/`, `internal/world/outbox/` |
| Event bus | JetStream publish/subscribe/history, crypto codecs, audit projection | `internal/eventbus/bus.go` |
| Access control | ABAC engine, DSL compiler, attribute providers, policy store | `internal/access/policy/engine.go` |
| Plugin host | Manifest parse/validate, dependency DAG, per-runtime hosts, host capabilities | `internal/plugin/manager.go` |
| Store | Postgres pool, goose migrations, session/role/plugin repos | `internal/store/postgres.go`, `internal/store/migrate.go` |
| Invariant registry | Named system invariants + rendering/meta-tests | `docs/architecture/invariants.yaml`, `cmd/inv-render` |

## Pattern Overview

**Overall:** Two-process (core + gateway) event-oriented service with an
ABAC-gated domain layer, a JetStream event log, and a three-runtime plugin host.
Composition is subsystem-based rather than a DI container.

**Key Characteristics:**
- Strict gateway/core split enforced as an invariant (`.claude/rules/gateway-boundary.md`, meta-tests in `internal/gateway_invariants/meta_test.go` and `cmd/holomush/gateway_imports_test.go`).
- World state is canonical in PostgreSQL; the event feed is an append-only audit/notification log, not the source of truth.
- Default-deny ABAC at every command and world-service call.
- Plugin runtime symmetry: host-side trust checks live on the common path shared by Lua and binary runtimes.

## Layers

**Entry / CLI layer:**
- Purpose: process bootstrap and flag/config handling
- Location: `cmd/holomush/`
- Depends on: `internal/config`, `internal/lifecycle`, every subsystem's `setup` package
- Used by: operators, Docker images

**Gateway layer (protocol translation):**
- Purpose: telnet framing, HTTP/ConnectRPC serving, static SPA, cookie/CORS/security headers, Sentry + OTLP relays
- Location: `internal/web/`, `internal/telnet/`, `cmd/holomush/gateway.go`
- Depends on: `internal/grpcclient` (gRPC clients to core), `internal/ulidgen`
- Constraint: MUST NOT import `internal/core`, repositories, or the store

**RPC facade layer:**
- Purpose: request validation, session identity resolution, translation to domain calls, streaming history/subscribe
- Location: `internal/grpc/`
- Depends on: `internal/command`, `internal/world`, `internal/eventbus`, `internal/access`

**Domain layer:**
- Purpose: world model, sessions, presence, scenes, character identity/naming/retirement
- Location: `internal/world/`, `internal/session/`, `internal/presence/`, `internal/charname/`, `internal/retirement/`
- Depends on: `internal/store`, `internal/access`

**Event layer:**
- Purpose: publish/subscribe/history, payload crypto, audit projection, DLQ, replay
- Location: `internal/eventbus/` (+ `audit/`, `history/`, `crypto/`, `codec/`, `consumer/`, `authguard/`)

**Persistence layer:**
- Purpose: Postgres pool, embedded goose migrations, repositories
- Location: `internal/store/`, `internal/world/postgres/`

**Plugin layer:**
- Purpose: manifest-governed extension host across three runtimes
- Location: `internal/plugin/` (host), `pkg/plugin/` (SDK), `plugins/` (in-tree plugins)

## Data Flow

### Conversational command (human/CLI verb)

1. Telnet or web client sends input; gateway forwards over gRPC (`cmd/holomush/gateway.go`, `internal/web/handler.go`).
2. Core RPC handler receives it (`internal/grpc/command_handler.go`).
3. `Dispatcher.Dispatch` parses, resolves aliases, applies focus redirects and rate limits (`internal/command/dispatcher.go:144`).
4. Two-layer authorization: `engine.Evaluate(subject,"execute","command:<name>")` then per-capability `CanPerformAction` (`internal/access/policy/engine.go`).
5. Either a builtin handler (`internal/command/handlers/`) or plugin delivery (`internal/command/dispatcher.go:389` → `internal/plugin/subscriber.go`).
6. Effects mutate the world through `internal/world/service.go`, writing via `internal/world/postgres/` inside a transaction that also writes the outbox.

### World change → event → audit

1. World mutation writes a row to the transactional outbox (`internal/world/outbox/store.go`).
2. The outbox relay subsystem reads and converts envelopes to events (`internal/world/outbox/relay.go`, `wire.go::EnvelopeToEvent`).
3. `eventbus.NewEvent(...)` stamps a monotonic ULID (`internal/eventbus/types.go:215`); `Qualify` prepends `events.<game_id>.` (`internal/eventbus/qualify.go:23`).
4. Publish to JetStream with `Nats-Msg-Id` dedup (`internal/eventbus/publisher.go`); sensitive payloads are encrypted per `crypto.emits` (`internal/eventbus/crypto/`).
5. The audit projection consumes and writes durable rows (`internal/eventbus/audit/projection.go` → `events_audit`); plugin-owned subjects route to the plugin's own audit table via `PluginAuditService.AuditEvent` (`internal/eventbus/audit/plugin_router.go`).

### History read

`HistoryReader.QueryHistory` dispatches hot (JetStream) then cold (Postgres) transparently: `internal/eventbus/history/dispatcher.go`, `hot_jetstream.go`, `cold_postgres.go`.

**State Management:**
- Canonical world state: PostgreSQL.
- Ordering: JetStream per-stream `uint64` sequence. ULIDs are identity/dedup keys only.
- Sessions: `internal/session/` backed by `internal/store/session_store.go`; leases in `internal/sessionlease/`.

## Key Abstractions

**EventBus (Publisher / Subscriber / HistoryReader):**
- Purpose: three narrow roles over one concrete bus
- Examples: `internal/eventbus/bus.go`, `publisher.go`, `subscriber.go`, `history/`
- Pattern: role interfaces; `eventbus.NewEvent` is the only sanctioned constructor

**Subsystem / Orchestrator:**
- Purpose: declared `DependsOn` edges, topological start, health tiers
- Examples: `internal/lifecycle/subsystem.go`, `orchestrator.go`, `cmd/holomush/core.go:1341`
- Pattern: each domain ships a `setup/` package producing a `lifecycle.Subsystem`

**AccessPolicyEngine + AttributeProvider:**
- Purpose: default-deny ABAC with pluggable attribute bags
- Examples: `internal/access/policy/engine.go`, `internal/access/policy/attribute/`
- Pattern: providers OMIT optional keys rather than emitting sentinels (`.claude/rules/abac-providers.md`)

**Plugin Manifest / Manager:**
- Purpose: declarative capability, emit, crypto, audit, and command declarations resolved as a DAG
- Examples: `internal/plugin/manifest.go`, `manager.go`, `plugins/*/plugin.yaml`
- Pattern: three runtime types (`TypeLua`, `TypeBinary`, `TypeSetting`, `internal/plugin/manifest.go:26`); binary plugins are subprocesses over mTLS gRPC (`internal/plugin/goplugin/`)

**Invariant registry:**
- Purpose: one canonical id space (`INV-<SCOPE>-N`) for durable system guarantees, bound to tests via `// Verifies:` annotations
- Examples: `docs/architecture/invariants.yaml`, `internal/invregistry/`, `cmd/inv-render`, `test/meta/invariant_registry_test.go`

## Entry Points

**`holomush core`:**
- Location: `cmd/holomush/core.go`
- Triggers: operator / container start
- Responsibilities: build and orchestrate all subsystems (database, TLS, ABAC, auth, world, plugins, sessions, bootstrap, eventbus, cluster, audit projection, crypto policy, gRPC, admin socket, outbox relay, retirement reactor, character activity, …)

**`holomush gateway`:**
- Location: `cmd/holomush/gateway.go`
- Triggers: operator / container start
- Responsibilities: telnet accept loop with backoff, HTTP/ConnectRPC server, static SPA

**Other CLI subcommands:** `migrate` (`cmd/holomush/migrate.go`), `admin` (`cmd_admin*.go`, UDS + SO_PEERCRED), `plugin` (`cmd_plugin*.go`), `audit`, `crypto rekey`, `status`, `character name`.

**Auxiliary binaries:** `cmd/inv-render`, `cmd/inv-migrate`, `cmd/lint-plugin-manifests`, `cmd/nats-floor-guard`, `cmd/holomush-cutover`, `cmd/internal`.

## Architectural Constraints

- **Gateway boundary:** the gateway holds gRPC clients, never repositories/DB/services. Structural writes use typed facade RPCs; `HandleCommand` is reserved for human/CLI conversational verbs.
- **Ordering ownership:** JetStream sequence, never ULID lex order.
- **Event construction:** `eventbus.NewEvent(...)` only; no `eventbus.Event{}` literals, no hand-stamped IDs.
- **Plugin runtime symmetry:** trust/policy gates sit at the common path (`internal/plugin/event_emitter.go::Emit`), not per-runtime. Permitted asymmetry is transport-only (Lua host capability vs binary service dial).
- **Default-deny ABAC:** every command and world-service call passes `internal/access`.
- **Threading:** Go goroutines; subsystem lifecycle is orchestrated rather than ad-hoc (`internal/lifecycle/`). Binary plugins are separate OS processes.
- **Test-import fence:** production code must not import `eventbustest`, `coretest`, `natstest`, `quarantinetest` (depguard in `.golangci.yaml`).
- **Timestamps:** epoch-nanosecond `BIGINT` columns, never `TIMESTAMPTZ` (`task lint:no-timestamptz`, `internal/pgnanos`).

## Anti-Patterns

### GUI structural write through the command path

**What happens:** a web button string-builds a command and calls `sendCommand`/`HandleCommand`.
**Why it's wrong:** routes a machine-initiated mutation through the human text parser and bypasses the typed facade contract.
**Do this instead:** add a typed RPC on the BFF facade (`internal/grpc/sceneaccess_service.go`, `characteraccess_write.go` are the shape).

### Gateway reaching for data directly

**What happens:** adding a repo field or DB query to `internal/web/` to "just fetch" state.
**Why it's wrong:** couples the separately-deployed gateway to internal data shapes.
**Do this instead:** add the core RPC first, then call it from the gateway (`internal/grpcclient/`).

### Sentinel values from attribute providers

**What happens:** `attrs["location"] = ""` alongside `has_location=false`.
**Why it's wrong:** the DSL treats missing as false but `"" == ""` as true — fail-open.
**Do this instead:** omit the key; keep the `has_X` witness (`internal/access/policy/attribute/stream.go`).

## Error Handling

**Strategy:** structured errors via `samber/oops`, wrapped with codes at boundaries.

**Patterns:**
- `oops.With(k,v).Wrap(err)`, `oops.Code("CODE").Wrap(err)`; helpers in `pkg/errutil`.
- gRPC handlers must not interpolate inner errors into `status.Errorf` — log with `errutil.LogErrorContext` and return a static message (`.claude/rules/grpc-errors.md`).
- Status↔oops translation happens at exactly one layer (outermost call site).

## Cross-Cutting Concerns

**Logging:** `log/slog` with a trace-injecting handler (`internal/logging/handler.go`); `*Context` variants mandatory when a `ctx` exists, enforced by `sloglint`.
**Telemetry:** OpenTelemetry in `internal/observability/`, `internal/telemetry/`, `internal/eventbus/telemetry/`; browser relays at `internal/web/otlp_relay.go` and `sentry_relay.go`.
**Validation:** manifest schema (`schemas/plugin.schema.json`), proto lint (buf), policy schema validators (`internal/plugin/policy_schema_validator.go`).
**Authentication:** player/session auth in `internal/auth/`, TOTP in `internal/totp/`, mTLS in `internal/tls/`, admin UDS peer credentials in `internal/admin/`.

---

*Architecture analysis: 2026-08-13*
