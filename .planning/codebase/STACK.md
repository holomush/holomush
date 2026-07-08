# Technology Stack

**Analysis Date:** 2026-07-08

## Languages

**Primary:**

- Go 1.26.4 - core server, plugins, CLIs (`go.mod`)
- TypeScript/Svelte 5 - web PWA client (`web/package.json`, `web/tsconfig.json`)

**Secondary:**

- Lua - in-process scripted plugins, executed via gopher-lua (`plugins/*/`, e.g. `plugins/echo-bot`)
- SQL - PostgreSQL migrations (`internal/store/migrations/*.sql`)
- Astro/MDX - public documentation site (`site/`)

## Runtime

**Environment:**

- Go 1.26.4 toolchain (`go.mod` line 3: `go 1.26.4`)
- Node.js runtime for `web/` (SvelteKit) and `site/` (Astro) — no `.nvmrc`/`.node-version` committed; the authoritative pin is CI's `node-version: 24` (`.github/workflows/ci.yaml`, `release.yaml`, `site.yml`, `scripts-tests.yaml`)

**Package Manager:**

- Go modules (`go.mod`, `go.sum`) — no vendoring
- pnpm for `web/` — pinned via `"packageManager": "pnpm@11.9.0"` in `web/package.json`
- Isolated Go tool modules for build tooling: `go.tool.mod` / `go.tool-lint.mod`, invoked with `GOWORK=off go tool -modfile=...` (`Taskfile.yaml` vars `GO_TOOL`, `GO_TOOL_LINT`) — kept separate because linter dependency closures conflict with `task`'s
- Lockfiles present: `go.sum`, `web/pnpm-lock.yaml` (implied by pnpm pin), `buf.lock`

## Frameworks

**Core:**

- Standard library `net/http` + ConnectRPC (`connectrpc.com/connect` v1.20.0) - dual gRPC/HTTP protocol surface for core↔gateway and web BFF (`internal/grpc`, `internal/web`)
- `google.golang.org/grpc` v1.82.0 - core server gRPC transport
- `github.com/hashicorp/go-plugin` v1.8.0 - binary plugin runtime (`internal/plugin`, `pkg/plugin`)
- `github.com/yuin/gopher-lua` v1.1.2 - Lua plugin VM, fresh state per event delivery
- `github.com/nats-io/nats-server/v2` v2.14.2 + `github.com/nats-io/nats.go` v1.52.0 - embedded NATS JetStream EventBus (`internal/eventbus`)
- SvelteKit 2.69.1 + Svelte 5.56.4 - web PWA (`web/package.json`)
- Astro 6 + `@astrojs/starlight` - public docs site (`site/package.json`)

**Testing:**

- `github.com/stretchr/testify` v1.11.1 - unit test assertions (`require`/`assert`)
- `github.com/onsi/ginkgo/v2` v2.32.0 + `github.com/onsi/gomega` v1.42.1 - BDD-style integration tests (`test/integration/**`, build tag `integration`)
- `github.com/testcontainers/testcontainers-go` v0.43.0 + `.../modules/postgres` v0.43.0 - Postgres testcontainer for integration/session-store tests
- `github.com/pashagolub/pgxmock/v5` v5.1.0 - pgx mock driver for store-layer unit tests
- mockery (config `.mockery.yaml`, `.mockery-boilerplate.txt`) - generated interface mocks
- `pgregory.net/rapid` v1.3.0 - property-based testing
- `go.uber.org/goleak` v1.3.0 - goroutine leak detection
- `@playwright/test` 1.61.1 (`web/package.json`) - browser E2E suite (`task test:e2e`)
- `vitest` 4.1.9 + `@vitest/ui` - web unit tests (`web/package.json`)

**Build/Dev:**

- `task` (go-task, `Taskfile.yaml`) - single entry point for build/test/lint/fmt/dev; MUST be used over raw `go`/lint commands per `CLAUDE.md`
- `buf` - protobuf lint/breaking/generate (`buf.yaml`, `buf.gen.yaml`, `buf.gen.internal.yaml`, `buf.gen.docs.yaml`, `buf.lock`, `web/buf.gen.yaml`)
- `golangci-lint` v2 via `bin/custom-gcl` (custom-built linter binary, `.custom-gcl.yml`, `.golangci.yaml`) — includes `sloglint`, `wrapcheck`, `depguard`
- `github.com/apache/skywalking-eyes/cmd/license-eye` pinned `v8.0.0`-style tag (`LICENSE_EYE_VERSION: v0.8.0` in `Taskfile.yaml`) - SPDX header enforcement (`.licenserc.yaml`)
- `golang-migrate/migrate/v4` v4.19.1 - DB migration engine, migrations embedded at compile time (`internal/store/migrations/`)
- goreleaser (`.goreleaser.yaml`) - release builds
- Vite 8 - web bundler/dev server (`web/package.json`)
- `@bufbuild/protoc-gen-es` 2.12.1 - proto-to-TS codegen for web client (`web/buf.gen.yaml`)

## Key Dependencies

**Critical:**

- `github.com/samber/oops` v1.22.0 - structured error wrapping (`oops.With(...).Wrap(err)`, `oops.Code(...)`) per `CLAUDE.md` Error Handling convention
- `github.com/oklog/ulid/v2` v2.1.1 - event ID generation (`core.NewULID()`)
- `github.com/jackc/pgx/v5` v5.10.0 - PostgreSQL driver/toolkit (`internal/store`)
- `github.com/exaring/otelpgx` v0.11.1 - OTel instrumentation for pgx
- `github.com/knadh/koanf/v2` v2.3.5 (+ `parsers/yaml`, `providers/file`, `providers/posflag`) - configuration loading
- `github.com/spf13/cobra` v1.10.2 + `github.com/spf13/pflag` v1.0.10 - CLI command framework (`cmd/holomush`)
- `github.com/pquerna/otp` v1.5.0 - TOTP/2FA (`internal/totp/service.go`)
- `github.com/alecthomas/participle/v2` v2.1.4 - parser combinator, used for the ABAC policy DSL (`internal/access/policy`)
- `github.com/cyberphone/json-canonicalization` (pinned pseudo-version, see `go.mod` comment) - RFC 8785 JCS canonicalization for crypto policy-set chain hashing (INV-CRYPTO-80)
- `buf.build/go/protovalidate` v1.2.0 + `buf.build/gen/go/bufbuild/protovalidate/...` - proto field validation

**Infrastructure:**

- `go.opentelemetry.io/otel` v1.44.0 stack (`sdk`, `sdk/metric`, `sdk/log`, `metric`, `trace`, `log`, `exporters/otlp/otlptrace{grpc,http}`, `exporters/otlp/otlplog/{otlploggrpc,otlploghttp}`, `contrib/bridges/otelslog`, `contrib/instrumentation/net/http/otelhttp`, `contrib/instrumentation/google.golang.org/grpc/otelgrpc`) - distributed tracing/metrics/logs
- `github.com/getsentry/sentry-go` v0.47.0 + `sentry-go/otel/otlp` v0.47.0 - error tracking (`internal/telemetry/sentry.go`)
- `github.com/prometheus/client_golang` v1.23.2, `client_model` v0.6.2, `github.com/nats-io/prometheus-nats-exporter` v0.20.1 - metrics export
- `github.com/hashicorp/golang-lru/v2` v2.0.7 - in-memory caching
- `google.golang.org/protobuf` v1.36.11 (pinned; see `go.mod` comment on cross-binary determinism, INV-CRYPTO-85)

## Configuration

**Environment:**

- `github.com/knadh/koanf/v2`-based config loading merges YAML files + flags (`providers/file`, `providers/posflag`, `parsers/yaml`)
- `.envrc` present at root (direnv) — content not read (forbidden-file policy: existence only)
- `.env*` files: none observed at root during exploration

**Build:**

- `Taskfile.yaml` - canonical build/test/lint/dev entry point; `vars.MIGRATIONS_DIR: internal/store/migrations`
- `.golangci.yaml` + `.custom-gcl.yml` - lint rule config and custom linter build
- `buf.yaml` / `buf.gen*.yaml` - protobuf toolchain config
- `.mockery.yaml` - mock generation config
- `web/tsconfig.json`, `web/vite.config.*` (implied by Vite/SvelteKit) - web build config
- `site/astro.config.mjs` - docs site build config

## Platform Requirements

**Development:**

- Go 1.26.4 (pinned in `go.mod`)
- Node.js + pnpm 11.9.0 (`web/package.json` `packageManager`)
- Docker (for `task dev`, `task test:int`, testcontainers-based session-store tests)
- `task` CLI (go-task) as the mandatory command runner

**Production:**

- Container-based deployment via `Dockerfile` and `compose.prod.yaml` (root)
- PostgreSQL 18 (Alpine image pinned by digest in `compose.yaml`: `postgres:18-alpine@sha256:...`)
- Deploy tooling for a hosted sandbox: `deploy/doctl/` (DigitalOcean) and `deploy/cloudflared/` (Cloudflare Tunnel) — see `deploy/doctl/README.md`, `deploy/cloudflared/config.yml.tmpl`

---

*Stack analysis: 2026-07-08*
