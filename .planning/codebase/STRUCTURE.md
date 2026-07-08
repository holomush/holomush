# Codebase Structure

**Analysis Date:** 2026-07-08

## Directory Layout

```text
holomush/
├── cmd/                    # Executable entry points
│   ├── holomush/           # Main server binary (serve/admin/migrate/plugin CLI)
│   ├── holomush-cutover/   # One-shot cutover/migration tooling
│   ├── inv-render/         # Renders invariants.md from invariants.yaml
│   ├── inv-migrate/        # Invariant ID migration tooling
│   ├── lint-plugin-manifests/ # CI lint for plugins/*/plugin.yaml
│   └── internal/           # Shared helpers for cmd/ binaries
├── internal/                # Private application code (host/core)
│   ├── access/              # ABAC engine, policy DSL, attribute providers
│   ├── admin/                # Admin CLI/gRPC surface
│   ├── audit/                 # Host audit logger/projector
│   ├── auth/, totp/           # Authentication, TOTP
│   ├── bootstrap/             # First-run bootstrap orphan handling
│   ├── cluster/               # Cluster/coordination concerns
│   ├── command/               # Command dispatcher, alias cache, focus redirects
│   ├── config/, settings/     # Runtime configuration
│   ├── content/               # Content-warning taxonomy etc.
│   ├── control/               # Control-plane services
│   ├── core/                  # Core event/ULID primitives (core.Event, core.NewEvent)
│   ├── eventbus/               # EventBus (NATS JetStream), history, audit, crypto
│   ├── gateway_invariants/, gatewaymetrics/  # Gateway-specific invariant checks/metrics
│   ├── grpc/                   # Core gRPC service handlers
│   ├── idgen/                  # Entity primary-key ULID generator (idgen.New())
│   ├── invregistry/            # Invariant registry schema (Go)
│   ├── lifecycle/              # Process lifecycle management
│   ├── logging/                # slog setup (trace-context-aware handler)
│   ├── naming/                 # crypto/rand-based naming helpers
│   ├── observability/          # OpenTelemetry wiring
│   ├── pgnanos/                # Postgres nanosecond timestamp helpers
│   ├── plugin/                 # Plugin host: manifest, loader, event emitter, registry
│   ├── property/               # Object/character property system
│   ├── session/                # Session store (connections/presence)
│   ├── store/                  # PostgreSQL access + migrations
│   ├── telemetry/              # Telemetry helpers
│   ├── telnet/                 # Telnet protocol gateway
│   ├── test/, testsupport/     # Shared test harnesses (integrationtest, quarantinetest, etc.)
│   ├── tls/                    # TLS cert handling
│   ├── web/                    # Web/ConnectRPC gateway (protocol translation only)
│   ├── world/                  # World model: locations, exits, characters, objects, scenes
│   └── xdg/                    # XDG base directory helpers
├── pkg/                      # Public/importable packages
│   ├── errutil/                 # Error logging helpers (LogError/LogErrorContext)
│   ├── holo/                    # Shared domain helper types
│   ├── plugin/                  # Plugin SDK (ServiceProvider, Serve, CommandRequest/Response)
│   └── proto/                   # Generated protobuf/ConnectRPC Go code
├── plugins/                  # In-tree plugins (binary + Lua)
│   ├── core-aliases/, core-building/, core-communication/, core-help/,
│   │   core-objects/, core-scenes/   # Core gameplay plugins
│   ├── echo-bot/               # Example/reference plugin
│   ├── setting-crossroads/, setting-skeleton/  # `type: setting` game-world plugins
│   └── test-abac-widget/       # ABAC test fixture plugin
├── api/proto/holomush/         # Protobuf schema definitions (source of truth for pkg/proto)
├── web/                      # SvelteKit PWA web client (see `web/CLAUDE.md`)
├── site/                     # Astro-Starlight public docs website
├── docs/                     # Contributor docs: adr/, architecture/, plans/, specs/, superpowers/
├── test/                     # Cross-package integration/meta tests, fixtures
│   ├── integration/            # Ginkgo/Gomega BDD integration specs (`//go:build integration`)
│   ├── meta/                   # Meta-tests (invariant registry, quarantine registry, proto docs)
│   └── testutil/                # Shared test utilities
├── schemas/                  # JSON Schemas (e.g. plugin.schema.json)
├── scripts/                  # Bootstrap/ops scripts
├── deploy/                   # Deployment configs (cloudflared, doctl)
├── docker/                   # Local dev compose support (grafana, otel, postgres, prometheus)
├── gorules/                  # Custom static-analysis rules/analyzers
└── Taskfile.yaml              # `task` command definitions (build/test/lint/dev entry point)
```

## Directory Purposes

**`internal/eventbus/`:**

- Purpose: the durable, ordered event log — publish, subscribe, and history read paths over embedded NATS JetStream
- Contains: `bus.go` (core interfaces), crypto/codec subpackages, `history/` (dispatcher, cold Postgres fallback), `audit/` (projection)
- Key files: `internal/eventbus/bus.go`

**`internal/plugin/`:**

- Purpose: the plugin host — everything that loads, validates, and mediates Lua/binary plugin trust
- Contains: manifest parsing (`config.go`), dependency DAG (`dependency.go`), event emission gate (`event_emitter.go`), crypto manifest validation (`crypto_manifest.go`), service registry (`registry.go`), the `goplugin/` subpackage (binary transport), `dispatchwire/` (Lua wire format)
- Key files: `internal/plugin/host.go`, `internal/plugin/event_emitter.go`, `internal/plugin/registry.go`

**`internal/access/`:**

- Purpose: ABAC authorization — subject/action/resource parsing, policy DSL, attribute resolution
- Contains: `access.go` (subject parsing), `policy/` (DSL, types, attribute providers), `setup/` (engine construction)
- Key files: `internal/access/access.go`, `internal/access/policy/types/types.go` (defines `AccessPolicyEngine`)

**`internal/world/`:**

- Purpose: host-owned world model entities — locations, exits, characters, objects, scenes, properties — and their gRPC surface
- Contains: one file per entity type, `grpc_server.go` (WorldService RPC handlers), `event_store_adapter.go` (bridges mutations to the event log), `mutator.go`
- Key files: `internal/world/grpc_server.go`, `internal/world/mutator.go`

**`internal/command/`:**

- Purpose: parses and dispatches player/CLI commands, running the two-layer ABAC authorization before executing
- Contains: `dispatcher.go` (the `Dispatcher` type), `handlers/` (built-in command handlers), `access.go`, `alias.go`, `focus_redirect.go`
- Key files: `internal/command/dispatcher.go`

**`internal/web/` and `internal/telnet/`:**

- Purpose: protocol-translation gateways — MUST NOT touch the DB or domain services directly (`.claude/rules/gateway-boundary.md`)
- Contains: ConnectRPC handlers, cookie/CORS/security-header middleware, static file serving (`internal/web/`); telnet session loop (`internal/telnet/`)
- Key files: `internal/web/server.go`, `internal/web/handler.go`

**`internal/store/migrations/`:**

- Purpose: sequentially-numbered, paired `.up.sql`/`.down.sql` PostgreSQL migrations embedded at compile time
- Contains: 78 files as of this analysis (39 migration pairs), zero-padded 6-digit prefixes
- Key files: `internal/store/migrations/000001_baseline.up.sql` / `.down.sql`

**`plugins/<name>/`:**

- Purpose: one directory per in-tree plugin; each is a self-contained module with its own manifest, tests, and (for `storage: postgres` plugins) migrations
- Contains: `plugin.yaml` (manifest), `main.go` (binary entrypoint) or Lua scripts, `migrations/` (plugin-owned schema, e.g. `plugins/core-scenes/migrations/`)
- Key files: `plugins/<name>/plugin.yaml`, `plugins/<name>/main.go`

**`pkg/plugin/`:**

- Purpose: the public Go SDK binary plugins import to talk to the host (go-plugin transport, `ServiceProvider`, `ServeWithServices`)
- Contains: `service.go` (`ServiceProvider`, `AttributeResolverProvider`), command request/response types
- Key files: `pkg/plugin/service.go`

**`api/proto/holomush/`:**

- Purpose: source-of-truth protobuf schemas; `task proto` regenerates `pkg/proto/**/*.pb.go` and web `*_pb.ts` from these
- Contains: one subdirectory per service family (world, scene, plugin, admin, control, content)

**`docs/architecture/`:**

- Purpose: the invariant registry — `invariants.yaml` (source of truth) and generated `invariants.md`
- Contains: `invariants.yaml`, `invariants.md` (generated, do not hand-edit generated regions)

## Key File Locations

**Entry Points:**

- `cmd/holomush/main.go`: server binary entry point (`run()`/`main()`)
- `cmd/holomush/root.go`: cobra root command wiring subcommands (serve/admin/migrate/plugin)
- `cmd/holomush/gateway.go`: gateway process wiring

**Configuration:**

- `Taskfile.yaml`: all build/test/lint/dev commands — MUST use `task`, never raw `go`/`golangci-lint`
- `internal/config/`, `internal/settings/`: runtime configuration loading
- `compose.yaml`, `compose.e2e.yaml`, `compose.prod.yaml`: Docker Compose environments
- `schemas/plugin.schema.json`: JSON Schema validating every `plugins/*/plugin.yaml`

**Core Logic:**

- `internal/eventbus/bus.go`: EventBus interfaces
- `internal/access/policy/types/types.go`: `AccessPolicyEngine` interface
- `internal/plugin/event_emitter.go`: shared emit boundary for both plugin runtimes
- `internal/command/dispatcher.go`: command authorization + routing
- `internal/world/`: world model entities and mutations

**Testing:**

- `test/integration/`: Ginkgo/Gomega BDD specs (`//go:build integration`)
- `test/meta/`: meta-tests guarding the invariant registry, quarantine registry, proto doc comments
- `internal/testsupport/integrationtest/`: canonical in-process integration stack (Postgres + embedded NATS + production `CoreServer`)
- `.mockery.yaml`: mockery config for generated mocks

## Naming Conventions

**Files:**

- Go: `snake_case.go` matching the primary type/concept (e.g. `event_emitter.go`, `dispatcher.go`); test files are `<name>_test.go` co-located; integration tests are `*_integration_test.go` behind `//go:build integration`

**Directories:**

- One package per directory under `internal/`, `pkg/`; one plugin per directory under `plugins/<name>/` matching the manifest `name:` field
- Migrations: `NNNNNN_description.{up,down}.sql`, zero-padded to 6 digits, sequential (`internal/store/migrations/`)

## Where to Add New Code

**New Feature (host-side):**

- Primary code: the owning `internal/<domain>/` package (e.g. world mutation → `internal/world/`; new ABAC attribute → `internal/access/policy/attribute/`)
- Tests: co-located `_test.go`; integration coverage in `test/integration/<domain>/` if it crosses the full stack

**New plugin:**

- Create `plugins/<name>/` with `plugin.yaml` (see `.claude/rules/plugin-manifest.md` for required/optional fields), `main.go` for binary or Lua scripts for `type: lua`
- Plugin-owned migrations (if `storage: postgres`): `plugins/<name>/migrations/`
- Use the `plugin-dev:create-plugin` skill for guided scaffolding

**New gRPC RPC:**

- Define in `api/proto/holomush/<service>/`, regenerate with `task proto && task web:generate`, implement the handler in `internal/grpc/` (host-owned) or the owning plugin's service (plugin-owned)
- Every proto element needs a Go-grounded doc comment (`.claude/rules/proto-doc-comments.md`)

**New gateway endpoint:**

- Add/extend the RPC on the core server first; the gateway (`internal/web/`) only adds a client call — never a direct DB/service query (`.claude/rules/gateway-boundary.md`)

**Utilities:**

- Cross-cutting error helpers: `pkg/errutil/`
- Shared domain helper types importable outside `internal/`: `pkg/holo/`

## Special Directories

**`internal/testsupport/`:**

- Purpose: shared test harnesses (`integrationtest/` full-stack stack, `quarantinetest/`, `sessiontest/`)
- Generated: No
- Committed: Yes — but production code MUST NOT import it (depguard-enforced)

**`docs/superpowers/`:**

- Purpose: AI-tooling-generated specs/plans, equally valid alongside `docs/specs/`/`docs/plans/`
- Generated: Partially (agent-authored, human-reviewed)
- Committed: Yes

**`site/`:**

- Purpose: public Astro-Starlight docs website (`site/src/content/docs/`, audience-organized: guide/operating/extending/contributing/reference)
- Generated: `reference/` subtree is auto-generated (API/event refs); rest is hand-authored
- Committed: Yes

**`pkg/proto/`:**

- Purpose: generated protobuf/ConnectRPC Go bindings from `api/proto/`
- Generated: Yes (`task proto`)
- Committed: Yes — regenerate and commit in the same change as any proto schema edit

---

*Structure analysis: 2026-07-08*
