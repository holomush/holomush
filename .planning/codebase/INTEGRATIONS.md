---
last_mapped_commit: 0047100ed380c2135d541d1120dd3a950714d2f1
last_mapped_at: 2026-08-13
---
# External Integrations

**Analysis Date:** 2026-08-13

## APIs & External Services

**RPC surface (self-hosted, not third-party):**
- gRPC + ConnectRPC over the same proto schemas. Public schemas live under `api/proto/holomush/<pkg>/v1/*.proto` across 13 packages: `admin`, `channel`, `characteraccess`, `comm`, `content`, `control`, `core`, `eventbus`, `plugin`, `scene`, `sceneaccess`, `web`, `world`.
  - Go stubs generate to `pkg/proto/holomush/...` via `buf.gen.yaml` (BSR plugins pinned: `protocolbuffers/go:v1.36.11`, `grpc/go:v1.6.2`, `connectrpc/go:v1.20.0`).
  - A separate internal eventbus cursor module generates via `buf.gen.internal.yaml`; docs via `buf.gen.docs.yaml`.
  - TypeScript clients generate to `web/src/lib/connect` via `web/buf.gen.yaml` (`bufbuild/es:v2.12.1`, matched to `@bufbuild/protoc-gen-es` in `web/package.json`). Consumed by `@connectrpc/connect-web`.
  - Regeneration is a single change: `task proto && task web:generate`, commit the output (stale-diff CI check).
- Request validation: `buf.build/go/protovalidate` (CEL-backed) applied at the proto layer.
- Gateway boundary: `internal/web/` and `internal/telnet/` are protocol translation only; all game state flows through core-server RPCs (`.claude/rules/gateway-boundary.md`).

**Plugin host boundary:**
- Binary plugins run as subprocesses over `hashicorp/go-plugin` gRPC. mTLS is configured via `goplugin.WithCA` (`internal/plugin/goplugin/host.go:131,850-902`, cert construction at `:1590-1600`). When no CA is configured the host logs an explicit warning that the plugin channel is unauthenticated and unencrypted (`host.go:380`) — production deployments MUST supply a CA.
- Lua plugins run in-process on gopher-lua (`internal/plugin/lua/host.go`), fresh state per delivery.
- A third `setting` runtime exists per the manifest `type` field.
- Manifests: `plugins/<name>/plugin.yaml`, validated against `schemas/plugin.schema.json` (`task plugin:validate`, `cmd/lint-plugin-manifests`).
- In-tree plugins: `core-aliases`, `core-building`, `core-channels`, `core-communication`, `core-help`, `core-objects`, `core-scenes`, `echo-bot`, `setting-crossroads`, `setting-skeleton`, `test-abac-widget`.

## Data Storage

**Databases:**
- PostgreSQL — the single store for all data. Dev image `postgres:18-alpine` (digest-pinned in `compose.yaml`).
  - Client: `github.com/jackc/pgx/v5` pool, traced by `github.com/exaring/otelpgx`.
  - Migrations: goose v3, embedded at compile time from `internal/store/migrations/` (48 files; one `NNNNNN_name.sql` per version carrying both `-- +goose Up` and `-- +goose Down`). Go migrations coexist (`000055_backfill_character_normalized_names.go`) and self-register in `init()` via `internal/store/migrations_register.go`.
  - Migration runner: `internal/store/migrate.go` (`Migrator` wrapping `goose.Provider`, with goose's advisory `lock` package). Auto-apply gated by `HOLOMUSH_DB_AUTO_MIGRATE`.
  - Plugin-owned schemas live in their own namespaces (e.g. `plugin_core_scenes.scene_log`).
  - Timestamp columns are `BIGINT` epoch-nanoseconds (INV-STORE-1, `task lint:no-timestamptz`).

**Event bus:**
- NATS JetStream. Two modes in `internal/eventbus/subsystem.go`:
  - **Embedded (implemented, default):** in-process `nats-server/v2` brought up by the subsystem; also the harness for every non-unit test tier (`eventbustest`).
  - **External / clustered (implemented as connect path, CLUSTER-01):** `connectExternal` (`subsystem.go:274`) dials an external cluster with creds/TLS; single-principal account scoping and fail-closed boot are covered (`subsystem.go:168-200`). Note the repo's own reference docs elsewhere still describe external NATS as unimplemented — the code path exists; treat the older prose as stale. Real-broker behavior is exercised via `internal/testsupport/natstest` containers.
  - Prometheus NATS exporter is embedded-only regardless of the `PrometheusExporter` flag (`subsystem.go:293-303`).
- Durable audit falls back from JetStream (recent) to the PostgreSQL `events_audit` table transparently in `HistoryReader.QueryHistory`.

**File Storage:**
- Local filesystem only. The SvelteKit bundle is embedded into the Go binary at build time (`task build`).

**Caching:**
- In-process only: `github.com/hashicorp/golang-lru/v2`. No external cache service.

## Authentication & Identity

**Player auth (self-hosted):**
- Password hashing: argon2id via `golang.org/x/crypto` (`internal/auth/hasher.go`)
- Second factor: TOTP (`github.com/pquerna/otp`, `internal/totp/`) — used for crypto-operator dual control
- Sessions: server-side session store in PostgreSQL (`internal/session`), `max_player_sessions_per_player` config knob
- Authorization: ABAC, default-deny, own DSL parsed with participle (`internal/access/`)

**Crypto key material:**
- KEK sourced from `HOLOMUSH_KEK_FILE` / `HOLOMUSH_KEK_PASSPHRASE` / `HOLOMUSH_KEK_PASSPHRASE_FILE`; local AEAD provider at `internal/eventbus/crypto/kek/local_aead.go`. Per-event DEKs for events declared in a plugin's `crypto.emits`. No external KMS/HSM integration.

## Monitoring & Observability

**Tracing/metrics/logs:**
- OpenTelemetry SDK v1.44.0 with OTLP gRPC and HTTP exporters for traces and logs; metrics via `otel/sdk/metric` (`internal/telemetry/provider.go`, `startup.go`, `logexport.go`)
- `otelslog` bridge routes `log/slog` into the OTel log pipeline; logging sinks are configurable (`stderr`, `otel`, `sentry`) in `internal/config/config.go:210-212`
- Browser-side OTel in the web client (`@opentelemetry/sdk-trace-web`, `exporter-trace-otlp-http`, `instrumentation-fetch`) relayed through `internal/web/otlp_relay.go`

**Error Tracking:**
- Sentry (`github.com/getsentry/sentry-go` + `otel/otlp` bridge, `internal/telemetry/sentry.go`); `@sentry/svelte` on the client

**Dev observability stack (`task dev:obs`, `compose.yaml`):**
- OTel Collector (`otel/opentelemetry-collector-contrib`, ports 4317/4318) → Jaeger (16686), Prometheus (9090 internal), Grafana (3001, anonymous admin), Dozzle log viewer (8888). All images digest-pinned.

## CI/CD & Deployment

**CI:** GitHub Actions — `.github/workflows/`: `ci.yaml`, `ci-docs-skip.yaml`, `buf.yml`, `commit-lint.yaml`, `release.yaml`, `deploy.yaml`, `site.yml`, `nightly-soak.yml`, `benchmark-check.yml`, `scripts-tests.yaml`, `issue-gate.yaml`, `bootstrap-sandbox.yaml`.
- CI invokes the underlying `task` targets directly (`task lint`, `task test:cover`, `task test:int`, `task test:e2e:cover`) — it does **not** run `task pr-prep`.
- Required protect-main checks: Build, Lint, Test, CodeRabbit, Integration Test, E2E Test (+ `Vuln`). Enforced by ruleset `11923801`, not classic branch protection.

**Third-party CI services:**
- Codecov (`codecov/codecov-action`, `.codecov.yml`) — patch + project statuses, posted not required
- Testcontainers Cloud (`atomicjar/testcontainers-cloud-setup-action`) for integration/E2E DB containers
- Namespace Labs cloud cache (`namespacelabs/nscloud-cache-action`)
- Buf Schema Registry via `bufbuild/buf-setup-action` and remote BSR codegen plugins
- CodeRabbit AI review (`.coderabbit.yaml`) — a required check
- Renovate (`.github/renovate.json`) — scoped to root `go.mod`, npm/pnpm, Actions, Docker digests, plus customManagers for the BSR codegen pins. It explicitly does **not** manage `go.tool.mod` / `go.tool-lint.mod`.

**Hosting:**
- Self-hosted containers (`Dockerfile`, `compose.prod.yaml`, `compose.cluster.yaml`, `deploy/`); sandbox at `game.holomush.dev`
- Docs site published from `site/` (Astro Starlight) via `.github/workflows/site.yml`
- Release artifacts via goreleaser (`.goreleaser.yaml`)

## Environment Configuration

**Required/observed env vars (names only):**
- `HOLOMUSH_GAME_ID`, `HOLOMUSH_DB_AUTO_MIGRATE`
- `HOLOMUSH_KEK_FILE`, `HOLOMUSH_KEK_PASSPHRASE`, `HOLOMUSH_KEK_PASSPHRASE_FILE`
- Test/CI: `HOLOMUSH_RUN_QUARANTINED`, `HOLOMUSH_PR_PREP_FORCE_FULL`, `GOWORK=off`

**Secrets location:**
- `.envrc` (direnv, `dotenv_if_exists`) for local dev; no `.env` is committed. GitHub Actions secrets for CI. KEK material is file- or passphrase-sourced per the vars above. No secret values are reproduced in this document.

## Webhooks & Callbacks

**Incoming:**
- None found for third-party services. GitHub-side automation runs through Actions events, not an app-hosted webhook endpoint. The only inbound non-RPC HTTP endpoint observed is the browser OTLP relay (`internal/web/otlp_relay.go`).

**Outgoing:**
- OTLP export to a collector endpoint (gRPC 4317 / HTTP 4318) and Sentry ingest. No other outbound service calls found.

---

*Integration audit: 2026-08-13*
