---
last_mapped_commit: 0047100ed380c2135d541d1120dd3a950714d2f1
last_mapped_at: 2026-08-13
---
<!-- refreshed: 2026-08-13 -->
# Codebase Structure

**Analysis Date:** 2026-08-13

## Directory Layout

```text
holomush/
├── api/proto/holomush/     # Protobuf source of truth (14 packages)
├── cmd/                    # Binaries (holomush + tooling)
├── internal/               # Go implementation (not importable externally)
├── pkg/                    # Public Go surface: errutil, holo, plugin SDK, proto
├── plugins/                # In-tree plugins (lua | binary | setting)
├── web/                    # SvelteKit PWA client + Playwright E2E
├── site/                   # Astro-Starlight public docs website
├── test/                   # Cross-cutting integration, meta, fixtures, testutil
├── scripts/                # Shell/Python tooling, bats tests, codemods
├── gorules/                # ruleguard custom lint rules
├── schemas/                # JSON schemas (plugin.schema.json)
├── docs/                   # ADRs, architecture registry, plans, specs, reviews
├── deploy/ docker/ build/  # Deployment + container assets
├── .planning/              # GSD artifacts (roadmap, phases, codebase map)
├── .claude/                # Agent rules, skills, hooks
├── Taskfile.yaml           # The only sanctioned build/test/lint entry point
└── CLAUDE.md  (AGENTS.md → symlink)
```

## Directory Purposes

**`api/proto/holomush/`:**
- Purpose: protobuf schemas, one directory per service package
- Contains: `admin`, `channel`, `characteraccess`, `comm`, `content`, `control`, `core`, `eventbus`, `plugin`, `scene`, `sceneaccess`, `web`, `world`
- Every element requires a Go-grounded doc comment (buf `COMMENTS` + name-echo gate)

**`cmd/`:**
- Purpose: executables
- Key files: `cmd/holomush/root.go` (cobra tree), `core.go`, `gateway.go`, `migrate.go`, `cmd_admin*.go`, `cmd_plugin*.go`, `cmd_audit.go`, `cmd_crypto_rekey.go`
- Tooling binaries: `cmd/inv-render`, `cmd/inv-migrate`, `cmd/lint-plugin-manifests`, `cmd/nats-floor-guard`, `cmd/holomush-cutover`, `cmd/internal`

**`internal/`:**
- Purpose: all server implementation. Notable packages:
  - `eventbus/` — bus, publisher, subscriber, `history/`, `audit/`, `crypto/`, `codec/`, `authguard/`, `consumer/`, `eventbustest/`
  - `access/` — ABAC: `policy/engine.go`, `policy/dsl/`, `policy/attribute/`, `policy/store/`, `policy/policytest/`
  - `world/` — domain service, `postgres/` repos, `outbox/` relay, `wmodel/`, `worldtest/`
  - `plugin/` — host, manifest, `lua/`, `goplugin/`, `hostfunc/`, `hostcap/`, `plugintest/`
  - `store/` — pool, `migrations/`, session/role/plugin repos
  - `grpc/` — core RPC facade; `web/` + `telnet/` — gateway surfaces
  - `lifecycle/` — subsystem orchestration; `bootstrap/` — seeding
  - support: `admin`, `auth`, `totp`, `tls`, `cluster`, `session`, `sessionlease`, `presence`, `charname`, `charactivity`, `retirement`, `settings`, `content`, `command`, `cmdparse`, `eventvocab`, `focuscontract`, `observability`, `telemetry`, `logging`, `idgen`, `ulidgen`, `pgnanos`, `invregistry`, `testsupport`

**`pkg/`:**
- `pkg/plugin/` — plugin SDK consumed by out-of-tree plugins
- `pkg/proto/holomush/<pkg>/v1/` — generated `*.pb.go`, `*_grpc.pb.go`, plus `<pkg>v1connect/` ConnectRPC bindings
- `pkg/errutil/`, `pkg/holo/`

**`plugins/`:**
- `core-aliases`, `core-building`, `core-channels`, `core-communication`, `core-help`, `core-objects`, `core-scenes`, `echo-bot`, `setting-crossroads`, `setting-skeleton`, `test-abac-widget`
- Each has `plugin.yaml` (see `.claude/rules/plugin-manifest.md`)

**`test/`:**
- `test/integration/<domain>/` — Ginkgo/Gomega suites, `//go:build integration`
- `test/meta/` — repo-invariant meta-tests (invariant registry, quarantine registry, proto doc comments)
- `test/quarantine.yaml`, `test/fixtures/`, `test/testutil/`

**`.planning/`:**
- GSD-owned: `ROADMAP.md`, `STATE.md`, `PROJECT.md`, `phases/<phase>/<NN>-SPEC.md`, `codebase/` (this map), `archive/`
- Tool-owned files — do not invent structure in them

## Key File Locations

**Entry Points:**
- `cmd/holomush/main.go`, `root.go`: CLI bootstrap
- `cmd/holomush/core.go`: core server subsystem composition (`productionSubsystems` at `:1341`)
- `cmd/holomush/gateway.go`: gateway process
- `web/src/routes/+layout.ts`: web client root

**Configuration:**
- `Taskfile.yaml`: all build/test/lint entry points
- `.golangci.yaml`, `.custom-gcl.yml`, `gorules/`: linting
- `buf.yaml`, `buf.gen*.yaml`: proto generation
- `compose*.yaml`, `Dockerfile`, `docker/`, `deploy/`: runtime
- `.mockery.yaml`: mock generation

**Core Logic:**
- `internal/command/dispatcher.go`: command dispatch
- `internal/world/service.go`: world model service
- `internal/eventbus/bus.go`, `publisher.go`, `subscriber.go`: event bus
- `internal/access/policy/engine.go`: ABAC engine
- `internal/plugin/manager.go`: plugin lifecycle

**Testing:**
- `*_test.go` beside implementation
- `test/integration/`, `test/meta/`
- `web/e2e/*.spec.ts` (Playwright), `web/src/**/*.test.ts` (Vitest)
- `scripts/tests/*.bats`

**Docs / registry:**
- `docs/architecture/invariants.yaml` (source of truth) → `invariants.md` (generated)
- `docs/adr/`, `docs/plans/`, `docs/specs/`, `docs/superpowers/`
- `site/src/content/docs/{guide,operating,extending,contributing,reference}/`

## Naming Conventions

**Go files:**
- `foo.go` → `foo_test.go` beside it
- `foo_integration_test.go` carries `//go:build integration`
- `export_test.go` exposes package internals to same-package tests
- Package-scoped helpers in `*test/` sibling packages (`eventbustest`, `policytest`, `worldtest`, `plugintest`, `coretest`, `natstest`, `quarantinetest`)

**Migrations:**
- `internal/store/migrations/NNNNNN_name.sql` — 6-digit zero-padded, snake_case, BOTH `-- +goose Up` and `-- +goose Down` in ONE file (no `.up`/`.down` pair)
- Go migrations `NNNNNN_name.go` in the same directory, registered in `init()`, wired by `internal/store/migrations_register.go`
- Current head: `000056_character_normalized_name_unique.sql`

**Protobuf:**
- Source `api/proto/holomush/<pkg>/v1/<file>.proto`
- Generated `pkg/proto/holomush/<pkg>/v1/<file>.pb.go` + `<file>_grpc.pb.go`
- ConnectRPC bindings in the `<pkg>v1connect/` sub-package (e.g. `pkg/proto/holomush/web/v1/webv1connect/`)
- Web TypeScript bindings generated by `task web:generate`

**Plugins:**
- Directory name matches manifest `name`; manifest filename is `plugin.yaml` (never `manifest.yaml`)

**Docs:**
- ADRs `docs/adr/<id>-<slug>.md`; GSD specs `.planning/phases/<phase>/<NN>-SPEC.md`

**Licensing:** SPDX Apache-2.0 headers on `.go`, `.sh`, `.proto` (applied by `task fmt`; skipped for `*.pb.go`).

## Where to Add New Code

**New core RPC:**
- Schema: `api/proto/holomush/<pkg>/v1/*.proto` (doc-comment every element), then `task proto && task web:generate` and commit generated output
- Handler: `internal/grpc/<feature>.go`
- Gateway pass-through (if client-facing): `internal/web/<feature>_handlers.go` — clients only, never data access

**New world feature:**
- Service logic: `internal/world/<feature>.go`
- Persistence: `internal/world/postgres/<feature>_repo.go` + a migration
- Events: publish via the outbox (`internal/world/outbox/`)

**New command:**
- Builtin: `internal/command/handlers/`; register in `internal/command/registry.go`
- Plugin-owned: declare in the plugin's `plugin.yaml` `commands:` with `capabilities:`

**New plugin:**
- `plugins/<name>/plugin.yaml` + `main.lua` or `main.go`; binary plugins build via `task plugin:build -- <name>`

**New subsystem:**
- `internal/<domain>/setup/` producing a `lifecycle.Subsystem`; append the `SubsystemID` at the END of the const block in `internal/lifecycle/subsystem.go`, run `task generate`, wire into `cmd/holomush/core.go`

**New invariant:**
- Add to `docs/architecture/invariants.yaml`, regenerate with `go run ./cmd/inv-render`, bind via a `// Verifies: INV-<SCOPE>-N` annotation

**Tests:**
- Unit beside implementation; integration in `test/integration/<domain>/` (Ginkgo, build tag); repo-wide guarantees in `test/meta/`

## Special Directories

**`pkg/proto/`:** generated; committed; regenerate in the same change or CI fails a stale-diff check.
**`internal/web/dist/`, `web/build/`, `web/node_modules/`:** build/vendor output.
**`bin/`:** custom golangci-lint build (`bin/custom-gcl`) and helpers.
**`.planning/`:** GSD tool-owned artifacts; committed.
**`.claude/`:** agent rules (`rules/`), skills, hooks; committed and load-bearing.
**`.codegraph/`, `.serena/`, `.superpowers/`, `.task/`:** tool caches/indexes.

---

*Structure analysis: 2026-08-13*
