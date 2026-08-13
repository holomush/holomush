---
last_mapped_commit: 0047100ed380c2135d541d1120dd3a950714d2f1
last_mapped_at: 2026-08-13
---
# Technology Stack

**Analysis Date:** 2026-08-13

## Languages

**Primary:**
- Go 1.26.5 (`go.mod` line 3) — server core, gateway, plugin host, CLI (`cmd/`, `internal/`, `pkg/`, `plugins/`)
- TypeScript 6.0.3 + Svelte 5.56.8 — web PWA client (`web/`)

**Secondary:**
- Lua (gopher-lua 1.1.2) — Lua plugin runtime (`plugins/echo-bot/main.lua`, `internal/plugin/lua/`)
- Protocol Buffers — API schemas (`api/proto/holomush/{admin,channel,characteraccess,comm,content,control,core,eventbus,plugin,scene,sceneaccess,web,world}`)
- SQL — goose migrations (`internal/store/migrations/`, 48 entries, latest `000056_character_normalized_name_unique.sql`)
- Bash + bats — repo scripts and their tests (`scripts/`, `scripts/tests/`)
- Astro/MDX — docs site (`site/`)

## Runtime

**Environment:**
- Go 1.26 toolchain (module `github.com/holomush/holomush`)
- Node/pnpm for the web + docs builds
- Docker for integration tests (testcontainers) and the dev/prod stacks (`compose.yaml`, `compose.prod.yaml`, `compose.cluster.yaml`, `compose.e2e.yaml`, `compose.e2e.cover.yaml`)

**Package Manager:**
- Go modules — four modfiles: `go.mod` (app), `go.tool.mod` (`task`, `yq`, `gotestsum`, `gofumpt`), `go.tool-lint.mod` (`golangci-lint`, `yamlfmt`, `actionlint`), plus a `gorules/` analyzer module. Tool modules are invoked as `GOWORK=off go tool -modfile=<file> <name>` (see `Taskfile.yaml` `GO_TOOL` / `GO_TOOL_LINT`).
- pnpm 11.13.1 for `web/` (`packageManager` field in `web/package.json`; lockfile `web/pnpm-lock.yaml`, workspace `web/pnpm-workspace.yaml`)
- `site/package.json` has no pinned packageManager field and no committed lockfile alongside it (inference: it is built via `task docs:setup`/`docs:build`; check `Taskfile.yaml` docs targets for the exact runner)
- Lockfiles: `go.sum`, `go.tool.sum`, `go.tool-lint.sum`, `web/pnpm-lock.yaml`, `buf.lock` — all present.

## Frameworks

**Core:**
- gRPC `google.golang.org/grpc v1.82.1` + ConnectRPC `connectrpc.com/connect v1.20.0` — dual RPC surface (`internal/grpc/`, `internal/web/`)
- `google.golang.org/protobuf v1.36.11` — pinned deliberately for cross-binary `op_args_hash` determinism (INV-CRYPTO-85, comment in `go.mod`)
- `buf.build/go/protovalidate v1.2.0` (+ CEL via `google/cel-go`) — proto-level request validation
- `github.com/jackc/pgx/v5 v5.10.0` — PostgreSQL driver; `github.com/exaring/otelpgx` for tracing
- `github.com/pressly/goose/v3 v3.27.3` — migration engine (`internal/store/migrate.go`)
- `github.com/nats-io/nats.go v1.52.0` + `github.com/nats-io/nats-server/v2 v2.14.3` — JetStream event bus, embeddable in-process (`internal/eventbus/subsystem.go`)
- `github.com/hashicorp/go-plugin v1.8.0` — binary plugin host (`internal/plugin/goplugin/host.go`)
- `github.com/yuin/gopher-lua v1.1.2` — Lua plugin runtime
- `github.com/spf13/cobra v1.10.2` + `pflag` — CLI; `github.com/knadh/koanf/v2 v2.3.5` (YAML + posflag providers) for config (`internal/config/config.go`)
- `github.com/samber/oops v1.22.0` — structured errors (repo-mandated)
- `github.com/alecthomas/participle/v2 v2.1.4` — ABAC policy DSL parser (`internal/access/`)
- SvelteKit 2.69.3 + `@sveltejs/adapter-static` — static web client embedded into the Go binary (`task build`)
- Tailwind CSS 4.3.3 (`@tailwindcss/vite`), `bits-ui`, `tailwind-variants`, `paneforge`, `@lucide/svelte` — web UI layer
- Astro 6 + `@astrojs/starlight` — docs site (`site/`)

**Testing:**
- `github.com/stretchr/testify v1.11.1` — unit assertions
- `github.com/onsi/ginkgo/v2 v2.32.0` + `gomega v1.42.1` — integration BDD suites (`//go:build integration`)
- `github.com/testcontainers/testcontainers-go v0.43.0` (+ `modules/postgres`) — DB containers
- `github.com/pashagolub/pgxmock/v5`, mockery (`.mockery.yaml`), `pgregory.net/rapid` (property testing), `go.uber.org/goleak`
- Playwright 1.61.1 + Vitest 4.1.10 + jsdom — web E2E and unit tests (`web/e2e/`, `web/`)
- bats — shell tests (`scripts/tests/`, `task test:bats`)

**Build/Dev:**
- `task` (go-task) — the mandatory entrypoint; `Taskfile.yaml` is the authority (`task lint|fmt|test|build|dev|test:int|test:e2e|pr-prep`)
- `buf` — proto lint/generate; three gen configs: `buf.gen.yaml` (public → `pkg/proto`), `buf.gen.internal.yaml` (internal eventbus cursor module), `buf.gen.docs.yaml`, plus `web/buf.gen.yaml` (TS stubs → `web/src/lib/connect`). BSR remote plugin versions are pinned in lockstep with the Go/npm runtimes.
- `golangci-lint` v2 via a custom build (`.custom-gcl.yml`, `bin/custom-gcl`) with the in-repo `gorules/` analyzer module
- `gofumpt`, `yamlfmt`, `dprint` (`dprint.json`), `rumdl` (`.rumdl.toml`), `license-eye` (`.licenserc.yaml`), `actionlint`
- `goreleaser` (`.goreleaser.yaml`), `cog` (`cog.toml`) for conventional commits, `mockery`, Vite 8.1.5

## Key Dependencies

**Critical:**
- `github.com/oklog/ulid/v2 v2.1.2` — event and entity IDs (`internal/ulidgen`, `core.NewULID`, `idgen.New`)
- `github.com/cyberphone/json-canonicalization` (pinned pseudo-version) — RFC 8785 JCS for `crypto.policy_set` chain hashing; changing it is a chain-breaking amendment (INV-CRYPTO-80)
- `golang.org/x/crypto v0.54.0` — argon2id password hashing (`internal/auth/hasher.go`)
- `github.com/pquerna/otp v1.5.0` — TOTP second factor (`internal/totp/`)
- `github.com/santhosh-tekuri/jsonschema/v6` + `github.com/invopop/jsonschema` — plugin manifest schema (`schemas/plugin.schema.json`, `task generate:schema`)
- `github.com/Masterminds/semver/v3`, `github.com/gobwas/glob` — plugin version/pattern matching

**Infrastructure:**
- OpenTelemetry Go SDK v1.44.0 — traces, metrics, logs; OTLP gRPC + HTTP exporters, `otelgrpc`/`otelhttp`/`otelpgx` instrumentation, `otelslog` bridge
- `github.com/getsentry/sentry-go v0.47.0` (+ `otel/otlp`) — error/log sink (`internal/telemetry/sentry.go`)
- `github.com/prometheus/client_golang v1.23.2` + `nats-io/prometheus-nats-exporter` — metrics
- `github.com/hashicorp/golang-lru/v2` — in-process caches
- `github.com/moby/moby/client` — Docker client used by tooling/tests

## Configuration

**Environment:**
- YAML config loaded by koanf with CLI flag overlay (`internal/config/config.go`); XDG-aware path resolution (`internal/xdg`)
- `.envrc` present (direnv, `dotenv_if_exists`); no `.env` committed — contents never read here
- Observed env vars in core code: `HOLOMUSH_DB_AUTO_MIGRATE`, `HOLOMUSH_GAME_ID`, `HOLOMUSH_KEK_FILE`, `HOLOMUSH_KEK_PASSPHRASE`, `HOLOMUSH_KEK_PASSPHRASE_FILE`. Test/CI toggles: `HOLOMUSH_RUN_QUARANTINED`, `HOLOMUSH_PR_PREP_FORCE_FULL`, `GOWORK=off`.
- Config sections seen in `internal/config/config.go`: game (`guest_start_location`, `disabled_commands`, `plugin_trust_allowlist`), sessions (`max_player_sessions_per_player`), crypto operator policy (`operators`, `dual_control_required`, `rekey_checkpoint_ttl`, `operator_read_*`), logging sinks (`stderr`, `otel`, `sentry`).

**Build:**
- `Taskfile.yaml` (single entrypoint), `Dockerfile` + `docker/`, `compose*.yaml`, `.goreleaser.yaml`
- `.golangci.yaml` + `.custom-gcl.yml`, `.mockery.yaml`, `buf.yaml`/`buf.lock`, `dprint.json`, `.yamlfmt`, `.editorconfig`, `.rumdl.toml`, `.licenserc.yaml`, `.codecov.yml`, `.coderabbit.yaml`, `.github/renovate.json`

## Platform Requirements

**Development:**
- Go 1.26.x, Docker (integration + E2E), Node + pnpm, `task` (bootstrapped from the tool module)
- macOS and Linux both supported; binary plugins cross-compile to `linux/amd64` + `linux/arm64` (`task plugin:build-all`)

**Production:**
- Container image built from a locally-compiled Linux binary (`task docker:build`, `Dockerfile`), orchestrated by `compose.prod.yaml` / `compose.cluster.yaml`; deployment assets under `deploy/`
- Exposed dev ports (`compose.yaml`): gateway `8080`, `4201`; OTel collector `4317`/`4318`; Jaeger `16686`; Grafana `3001`; Dozzle `8888`
- Sandbox environment `game.holomush.dev` (see `site/src/content/docs/operating/how-to/sandbox/`)

---

*Stack analysis: 2026-08-13*
