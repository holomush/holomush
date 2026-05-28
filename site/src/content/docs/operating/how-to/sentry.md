---
title: "Sentry Integration (Trial)"
---

HoloMUSH can export traces, errors, and logs to [Sentry](https://sentry.io/)
alongside its existing OpenTelemetry pipeline. The integration is **opt-in
via environment variables** — when `SENTRY_DSN` is unset, no Sentry code runs
and the bundle is unaffected.

The integration is in active evaluation — the data shape and configuration
surface are stable, but some features (grouped Issues, plugin panic capture)
are still being wired up. The data shape is OTel-native: spans flow through
Sentry's OTLP-HTTP exporter (`sentry-go/otel/otlp`), which is wired as a
second `sdktrace.WithBatcher` alongside the existing collector path. Removing
Sentry is a one-line config change; no app code needs to be touched.

## Architecture

```text
                         ┌──────────────────────┐
                         │  Existing OTLP gRPC  │
                  ┌─────►│  → Grafana / Tempo   │
┌─────────────┐   │      └──────────────────────┘
│ holomush    │   │
│ TracerProv  │───┤
│             │   │      ┌──────────────────────┐
└─────────────┘   └─────►│  Sentry OTLP HTTP    │
                         │  → sentry.io         │
                         └──────────────────────┘

         (browser)       ┌──────────────────────┐
         WebTracerProv  ────► OTLP HTTP collector│
         + @sentry/svelte ──► sentry.io          │
                         └──────────────────────┘
```

Both backends receive a copy of every span. Errors and logs (captured via
`sentry.CaptureException` and `sentry.Logger`) are emitted only over the
Sentry channel.

## Server-side (Go binaries)

Set the following environment variables for `holomush core` and
`holomush gateway`:

| Variable                      | Required | Default            | Notes                                                |
| ----------------------------- | -------- | ------------------ | ---------------------------------------------------- |
| `SENTRY_DSN`                  | yes      | (unset = disabled) | The DSN from your Sentry project's Client Keys page  |
| `SENTRY_ENVIRONMENT`          | no       | (none)             | e.g. `production`, `staging`, `dev`                  |
| `SENTRY_RELEASE`              | no       | `<service>@<ver>`  | Override for the auto-populated release tag          |
| `SENTRY_TRACES_SAMPLE_RATE`   | no       | `1.0`              | Float in `[0,1]`. Lower in production to control cost |

Sentry initializes alongside (not instead of) the existing OTel exporter.
`OTEL_EXPORTER_OTLP_ENDPOINT` continues to drive the primary collector
path; set both for dual export, or only `SENTRY_DSN` for Sentry-only.

## Browser-side (SvelteKit)

Add the following to the web client's environment (SvelteKit reads
`PUBLIC_*` variables at build time and exposes them to the browser):

| Variable                              | Required | Default | Notes                                            |
| ------------------------------------- | -------- | ------- | ------------------------------------------------ |
| `PUBLIC_SENTRY_DSN`                   | yes      | (unset) | Browser-safe; same DSN as the server-side value  |
| `PUBLIC_SENTRY_ENVIRONMENT`           | no       | (none)  | Mirrors the Go-side environment tag              |
| `PUBLIC_SENTRY_RELEASE`               | no       | (none)  | Override release tag                             |
| `PUBLIC_SENTRY_TRACES_SAMPLE_RATE`    | no       | `1.0`   | Float in `[0,1]`                                 |

The browser SDK is dynamically imported and tree-shaken out when
`PUBLIC_SENTRY_DSN` is empty, so deployments without Sentry pay no bundle
cost.

## Local dev setup

Drop a single `.env` file in the repo root containing **both** the server
and browser DSN entries. `.env` is already gitignored.

```bash
# .env (repo root, gitignored)
# Server side — read by Docker Compose for core + gateway containers.
SENTRY_DSN=https://<public-key>@<org>.ingest.us.sentry.io/<project>
SENTRY_ENVIRONMENT=dev-local
SENTRY_TRACES_SAMPLE_RATE=1.0

# Browser side — read by Vite at web-bundle build time (`task web:build`)
# and by the SvelteKit dev server (`task web:dev:obs`). Same DSN is fine.
PUBLIC_SENTRY_DSN=https://<public-key>@<org>.ingest.us.sentry.io/<project>
PUBLIC_SENTRY_ENVIRONMENT=dev-local
PUBLIC_SENTRY_TRACES_SAMPLE_RATE=1.0
```

How the two halves get picked up:

- **Server containers**: Docker Compose auto-reads `.env` from the repo
  root for `${VAR}` interpolation in `compose.yaml` — no extra step.
- **Browser bundle**: Vite reads env vars from the shell environment at
  build time. `.env` values need to be exported to the shell before
  `task dev:obs` or `task web:dev:obs` runs. Pick whichever exposure
  mechanism fits your shell:

    ```bash
    # direnv (recommended — auto-exports on cd into the repo):
    echo 'dotenv' >> .envrc && direnv allow

    # one-shot manual source (bash / zsh):
    set -a; source .env; set +a

    # one-shot manual source (fish):
    for line in (string match -v '#*' < .env); set -gx (string split = $line); end
    ```

Then `task dev:obs` / `task web:dev:obs` work normally and Sentry picks
up both halves.

### When the bundle is rebuilt

`PUBLIC_*` values are inlined by Vite at build time (adapter-static does
not support runtime env injection). Concretely: a `task dev:obs` with the
`.env` exposed will bake the DSN into the bundle that ships in the docker
image. If you later run `task dev:obs` *without* the vars exposed, the
freshly-rebuilt bundle has Sentry disabled — even if the server side is
still emitting.

## Disabling

Unset `SENTRY_DSN` (server) and `PUBLIC_SENTRY_DSN` (browser). Restart the
processes / rebuild the web bundle. No code changes required.

## What goes to Sentry

| Surface                 | Server (Go)                                   | Browser (Svelte)                |
| ----------------------- | --------------------------------------------- | ------------------------------- |
| Distributed traces      | All spans (OTLP HTTP via Sentry exporter)     | Browser fetch spans + nav spans |
| Unhandled errors        | Yes, via `sentry.CaptureException` (partial coverage today) | Yes, browser SDK auto-captures  |
| Application logs        | OTel-native: `log/slog` → OTel `LoggerProvider` → Sentry OTLP-HTTP (`…/integration/otlp/v1/logs`); gated by `logging.sentry.enabled` + `SENTRY_DSN`. No `sentry.Logger` calls in app code. | Enabled (`enableLogs: true`); `console.error` + `console.warn` auto-forwarded via `consoleLoggingIntegration` |
| Metrics                 | Not wired (no OTel metrics pipeline today)    | Default-enabled (`enableMetrics: true` is the SDK default in 10.x) |
| Session replay / RUM    | n/a                                           | Not enabled in trial            |

## Application logs (OTel-native)

HoloMUSH's log pipeline is built entirely on OpenTelemetry. All `log/slog`
calls flow through a single OTel `LoggerProvider` (via the
`contrib/bridges/otelslog` bridge) and fan out to up to three independently
configurable sinks.

### Sinks and configuration

| koanf key                | CLI flag              | Default     | Purpose                              |
| ------------------------ | --------------------- | ----------- | ------------------------------------ |
| `logging.stderr.enabled` | `--log-stderr`        | `true`      | Toggle stderr output                 |
| `logging.stderr.level`   | `--log-stderr-level`  | (global)    | Per-sink level override              |
| `logging.otel.enabled`   | `--log-otel`          | `true`      | Toggle OTLP collector sink           |
| `logging.otel.level`     | `--log-otel-level`    | (global)    | Per-sink level override              |
| `logging.sentry.enabled` | `--log-sentry`        | `true`      | Toggle Sentry sink                   |
| `logging.sentry.level`   | `--log-sentry-level`  | `warn`      | Per-sink level override              |

**Toggle + endpoint rule:** a non-stderr sink is active only when its toggle
is on *and* its endpoint is configured — `OTEL_EXPORTER_OTLP_ENDPOINT` for
the collector, `SENTRY_DSN` for Sentry. If the endpoint is absent the sink
is skipped regardless of the toggle.

**Per-sink level overrides:** each sink can filter independently of the
global `LOG_LEVEL` / `--log-level`. The Sentry sink defaults to `warn`
(unlike stderr and the collector, which inherit the global level) as cost
control: Sentry Logs is volume-priced, so routing only WARN+ to Sentry while
keeping stderr at DEBUG avoids paying for every debug trace. Lower it with
`--log-sentry-level info` (or `debug`) if you need richer logs in Sentry
temporarily.

### Trace correlation

Logs carry `trace_id` and `span_id` only when emitted via the
context-aware slog functions (`slog.InfoContext`, `slog.WarnContext`, etc.).
Bare `slog.Info(…)` calls produce uncorrelated log records. See
`.claude/rules/logging.md` for the project MUST on context propagation.

### ERROR logs vs. Sentry Issues

ERROR-level records arrive in the Sentry **Logs** stream as individual
entries. They do **not** become grouped, deduplicated, alertable Sentry
**Issues** — that requires `sentry.CaptureException`, which is not yet wired up.

## Browser tunnel (ad-blocker bypass)

Browser SDKs POSTing directly to `*.ingest.sentry.io` are blocked by most
ad-blockers and privacy extensions — they can't distinguish first-party
error monitoring from third-party tracking. To work around this, the
gateway exposes a same-origin relay at **`/api/sentry-relay`** that the
browser SDK targets via Sentry's `tunnel` option. The relay forwards the
envelope body to Sentry's ingest server-side, where no client-side
extension can intercept it.

| Behaviour | Detail |
| --- | --- |
| **Endpoint** | `POST /api/sentry-relay` on the gateway web HTTP listener |
| **Registration gate** | Only registered when `SENTRY_DSN` is set on the gateway. Unset = no endpoint, no open-relay risk. |
| **DSN validation** | The relay parses the inbound envelope's header and rejects (`403`) any envelope whose embedded DSN doesn't match the configured project (host + project ID). Prevents abuse as a generic Sentry forwarder. |
| **Size limit** | Inbound bodies capped at 1 MiB; oversize returns `413`. |
| **Upstream timeout** | 30 s (matches Sentry JS SDK's default transport timeout). |
| **Method** | `POST` only; other methods get `405`. |

The browser SDK is already configured to use the tunnel
(`tunnel: '/api/sentry-relay'` in `web/src/lib/sentry.ts`). No client
config needed — when `PUBLIC_SENTRY_DSN` is baked into the bundle, the
SDK automatically routes envelopes through the relay.

Implementation: `internal/web/sentry_relay.go` + registration in
`internal/web/server.go`. Unit tests cover DSN validation, oversize
rejection, method/method-not-allowed, and the forward path
(`internal/web/sentry_relay_test.go`).

## DSN handling

Sentry DSNs are not strict secrets — the browser SDK embeds them in the
shipped bundle by design. They do reveal the project's Sentry org/project
identifiers, so treat them as configuration (env-var only, never checked
into the repo) and avoid unnecessary plaintext logging in operational
output. If a DSN must appear in logs or dashboards (e.g., for diagnosing
a connectivity issue), redact or truncate the public-key prefix before
sharing widely.
