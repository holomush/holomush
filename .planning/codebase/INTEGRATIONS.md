# External Integrations

**Analysis Date:** 2026-07-08

## APIs & External Services

**Error Tracking:**

- Sentry - `github.com/getsentry/sentry-go` v0.47.0, `sentry-go/otel/otlp` v0.47.0
  - Client/setup: `internal/telemetry/sentry.go`
  - Log export bridge: `internal/telemetry/logexport.go`
  - Auth: DSN via config (not committed; forbidden-file policy prevents reading env files for values)

**Observability backends (self-hosted, dev-only via Docker Compose):**

- Jaeger - trace UI, `compose.yaml` service `jaeger` (image `jaegertracing/jaeger:latest`, pinned by digest)
- Prometheus - metrics, `compose.yaml` service `prometheus`
- Grafana - dashboards, `compose.yaml` service `grafana`
- Dozzle - log viewer, `compose.yaml` service `dozzle`
- OpenTelemetry Collector - `compose.yaml` service `otel-collector` (image `otel/opentelemetry-collector-contrib:latest`), receives OTLP from core/gateway and forwards to Jaeger/Prometheus
- Started via `task dev:obs` (profile `COMPOSE_PROFILES=observability`); web-side traces additionally require `task web:dev:obs` (conditional on `PUBLIC_OTEL_ENDPOINT`)

**TOTP/2FA:**

- `github.com/pquerna/otp` v1.5.0 - `internal/totp/service.go` (no external service call; local TOTP secret generation/verification)

## Data Storage

**Databases:**

- PostgreSQL - sole system-of-record datastore
  - Version: `postgres:18-alpine` (pinned by digest in `compose.yaml`)
  - Driver/toolkit: `github.com/jackc/pgx/v5` v5.10.0, instrumented via `github.com/exaring/otelpgx` v0.11.1
  - Access layer: `internal/store/` (e.g. `internal/store/role_store.go`, `internal/store/character_settings_repo.go`, `internal/store/alias.go`)
  - Migrations: `internal/store/migrations/*.up.sql` / `*.down.sql`, embedded at compile time, run via `golang-migrate/migrate/v4` v4.19.1; sequential 6-digit prefixes (`000001_baseline.up.sql` ... currently through at least `000010_drop_events_and_cursors.up.sql`)
  - Audit tables also live here: host-owned `events_audit`, plugin-owned tables (e.g. `plugin_core_scenes.scene_log`) declared via plugin manifest `audit:` blocks
  - Test-tier access: `github.com/pashagolub/pgxmock/v5` for unit tests; `github.com/testcontainers/testcontainers-go/modules/postgres` v0.43.0 for integration tests (real ephemeral Postgres containers)

**File Storage:**

- Local filesystem only — no object-storage integration (S3/GCS/etc.) detected in `go.mod` or `web/package.json`

**Caching:**

- In-process only via `github.com/hashicorp/golang-lru/v2` v2.0.7 — no external cache service (Redis/Memcached) detected

## Authentication & Identity

**Auth Provider:**

- Custom, in-house — no third-party IdP (Auth0/Okta/Clerk) dependency detected in `go.mod`
- ABAC authorization engine (default-deny) lives in `internal/access/` (`access.go`, `grants.go`, `policy/`, `resolver.go`) — governs both command dispatch and gRPC/RPC-level checks
- TOTP-based 2FA: `internal/totp/service.go`

## Monitoring & Observability

**Error Tracking:**

- Sentry (`internal/telemetry/sentry.go`) — see APIs & External Services above

**Metrics:**

- Prometheus client (`github.com/prometheus/client_golang`), scraped by the `prometheus` compose service; NATS-specific metrics via `github.com/nats-io/prometheus-nats-exporter`

**Tracing/Logs:**

- OpenTelemetry SDK (traces, metrics, logs) exported via OTLP gRPC/HTTP to the `otel-collector` compose service, which fans out to Jaeger (traces) and Prometheus (metrics)
- Structured logging via `log/slog`, wrapped by `internal/logging/handler.go` (`traceHandler` injects `service`, `version`, `trace_id`, `span_id` from context) — see `.claude/rules/logging.md`
- Web client ships its own OTel Web SDK (`@opentelemetry/sdk-trace-web`, `instrumentation-fetch`, `exporter-trace-otlp-http` in `web/package.json`) plus `@sentry/svelte` v10.63.0 for browser-side error tracking

## CI/CD & Deployment

**Hosting:**

- Self-managed container hosting; `deploy/doctl/` (DigitalOcean, `deploy/doctl/README.md`, `deploy/doctl/firewall-sandbox.json`) targets the `game.holomush.dev` sandbox
- `deploy/cloudflared/config.yml.tmpl` — Cloudflare Tunnel used to expose the sandbox without opening inbound ports
- `Dockerfile` (root) builds the `holomush` server image used by both `core` and `gateway` services in `compose.yaml`
- Production compose profile: `compose.prod.yaml`; E2E test composes: `compose.e2e.yaml`, `compose.e2e.cover.yaml`

**CI Pipeline:**

- GitHub Actions - `.github/workflows/` (e.g. `ci.yaml`, plus a docs-skip fast lane `ci-docs-skip.yaml`)
- `.coderabbit.yaml` - CodeRabbit AI PR review integration
- `.codecov.yml` / `codecov.yml` - Codecov coverage reporting integration
- goreleaser (`.goreleaser.yaml`) drives release artifact builds

## Environment Configuration

**Required env vars:**

- Not enumerable without reading `.env*`/config files (forbidden by policy); configuration is loaded via koanf (`github.com/knadh/koanf/v2`) from YAML + CLI flags, not documented inline in this scan
- `.envrc` present at repo root (direnv-managed) — existence only, contents not read
- Web observability toggle: `PUBLIC_OTEL_ENDPOINT` (referenced in `Taskfile.yaml` comment for `task web:dev:obs`)

**Secrets location:**

- No `.env` files were found at the repository root during this scan; secrets are expected to be supplied via environment/CI secrets rather than committed files

## Webhooks & Callbacks

**Incoming:**

- None detected (no webhook receiver endpoints found in `internal/web` or `internal/grpc` during this scan)

**Outgoing:**

- OTLP export to the `otel-collector` service (traces/metrics/logs) — see Monitoring & Observability
- Sentry event submission (crash/error reports)

## Protocol Surfaces

- **Telnet** - `internal/telnet/` (`gateway_handler.go`, `limits.go`, `refuse.go`, `sanitize_test.go`, `metrics.go`) — classic MUSH client protocol, terminated at the gateway
- **Web/ConnectRPC** - `internal/web/` (gateway boundary; protocol-translation only, per `.claude/rules/gateway-boundary.md`) using `connectrpc.com/connect` v1.20.0 for ConnectRPC↔gRPC bridging to the core server; the web client (`web/`) talks ConnectRPC via `@connectrpc/connect` + `@connectrpc/connect-web`
- **gRPC (internal)** - core server services defined under `api/proto/holomush/{core,world,scene,comm,content,admin,control,plugin,eventbus,sceneaccess,web}` (buf-managed), consumed internally by the gateway and by binary plugins
- **EventBus** - embedded NATS JetStream (`internal/eventbus/`), not a network-external NATS deployment; same embedded model in dev, test, and production (external/clustered NATS is unimplemented, per `.claude/rules/references/testing-detail.md`)

---

*Integration audit: 2026-07-08*
