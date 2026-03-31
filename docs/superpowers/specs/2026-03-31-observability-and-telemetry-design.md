# Observability and Telemetry — Design

Configurable log levels, distributed tracing across all system boundaries,
frontend trace instrumentation, and a profile-gated Docker Compose
observability stack for local development.

## RFC 2119

Keywords MUST, SHOULD, MAY per RFC 2119.

---

## 1. Scope

### Goals

- Configurable log level via CLI flag and environment variable
- OpenTelemetry SDK initialization conditional on collector availability
- Distributed tracing across all system boundaries (gRPC, HTTP, telnet, DB)
- SvelteKit frontend auto-instrumentation with custom spans for key UX flows
- Profile-gated Docker Compose stack: OTel Collector, Tempo, Prometheus,
  Grafana, Dozzle
- Structured debug/trace logging at decision points throughout the codebase

### Non-Goals

- Production deployment topology (operators choose their own backends)
- Full RUM / Core Web Vitals (future follow-up, not precluded)
- Pre-built Grafana dashboards (provisioning directory exists for later)
- Alerting rules or on-call integration
- Loki or centralized log aggregation (Dozzle covers dev log viewing)

### Audience

This stack is for **local development only**. The application code speaks
standard OTLP — operators MAY point it at any compatible backend
(Jaeger, Datadog, Honeycomb, etc.) in production.

---

## 2. Log Level Configuration

### CLI Flag

A `--log-level` flag MUST be added to the root command (applies to both
`core` and `gateway` subcommands).

Accepted values: `debug`, `info`, `warn`, `error` (case-insensitive).
Default: `info`.

### Environment Variable

The `LOG_LEVEL` environment variable MUST be honored. The CLI flag MUST
take precedence over the environment variable when both are set.

### Implementation

- `cmd/holomush/root.go` — add `--log-level` persistent flag with env
  var binding
- `internal/logging/handler.go` — `Setup()` MUST accept a `slog.Level`
  parameter instead of hardcoding `slog.LevelDebug`. The
  `slog.HandlerOptions.Level` field MUST be set to the parsed level
  value. `SetDefault()` MUST accept the same parameter.
- `cmd/holomush/core.go` — pass parsed level to `logging.Setup()`
- `cmd/holomush/gateway.go` — same

### Behavior

```text
holomush core --log-level debug           # debug level
LOG_LEVEL=debug holomush core             # debug level via env
LOG_LEVEL=info holomush core --log-level debug  # debug (flag wins)
holomush core                             # info (default)
```

---

## 3. OTel SDK Initialization

### Trigger

The SDK MUST initialize only when `OTEL_EXPORTER_OTLP_ENDPOINT` is set.
When unset, the application MUST use no-op providers (zero overhead).

### New Package: `internal/telemetry/`

Separate from `internal/observability/` (which owns Prometheus metrics
and health probes). Clean boundary: `observability` = metrics/health,
`telemetry` = distributed tracing SDK lifecycle.

### `provider.go` — `Init(ctx, serviceName, serviceVersion) (shutdown, error)`

When `OTEL_EXPORTER_OTLP_ENDPOINT` is **unset**:

- Return a no-op shutdown function and nil error
- No SDK initialization, no resource allocation

When `OTEL_EXPORTER_OTLP_ENDPOINT` is **set**:

- Create a `resource.Resource` with `service.name`, `service.version`,
  `deployment.environment` attributes
- Create an `otlptracegrpc` exporter targeting the endpoint
- Create a `sdktrace.TracerProvider` with `BatchSpanProcessor`, the
  exporter, and the resource
- Create a `sdkmetric.MeterProvider` with OTLP metric exporter and
  periodic reader
- Register both as global providers via `otel.SetTracerProvider()` and
  `otel.SetMeterProvider()`
- Set up W3C `TraceContext` propagator via `otel.SetTextMapPropagator()`
- Set a no-op OTel error handler via `otel.SetErrorHandler()` to
  suppress OTLP exporter retry/connection warnings when the collector
  is unreachable. This prevents log pollution when the compose
  observability profile is inactive but the env var is set.
- Return a shutdown function that flushes and shuts down both providers

### Sampling

Default: `ParentBasedSampler(AlwaysSample())` — every trace in dev.
Operators MAY override via the standard `OTEL_TRACES_SAMPLER` env var.

### Integration Points

- `cmd/holomush/core.go` — call `telemetry.Init(ctx, "holomush-core",
  version)` early in startup, defer `shutdown(ctx)`
- `cmd/holomush/gateway.go` — call `telemetry.Init(ctx,
  "holomush-gateway", version)` early in startup, defer `shutdown(ctx)`
- Existing code using `otel.GetTracerProvider()` (plugin middleware,
  command dispatcher) automatically picks up the real provider

---

## 4. System Boundary Instrumentation

### 4a. gRPC Server (Core)

Add `otelgrpc` interceptors to `internal/grpc/server.go`:

- `otelgrpc.UnaryServerInterceptor()` — span per unary RPC with method
  name, status code, duration
- `otelgrpc.StreamServerInterceptor()` — span per streaming RPC
  (Subscribe, StreamEvents)
- Interceptors extract `traceparent` from incoming gRPC metadata,
  linking gateway-to-core traces automatically

**Files:** `internal/grpc/server.go`

### 4b. HTTP/ConnectRPC (Gateway)

Wrap the ConnectRPC handler mux with `otelhttp.NewHandler()`:

- Auto-creates spans for every HTTP request (unary + server-streaming)
- Records HTTP method, status code, URL path, duration

Add `otelgrpc` client interceptors to the gateway's gRPC dial:

- `otelgrpc.UnaryClientInterceptor()` — propagates `traceparent` to
  core server
- `otelgrpc.StreamClientInterceptor()` — same for streaming calls

**Trace chain:** browser fetch span -> HTTP handler span ->
gRPC client span -> gRPC server span -> command dispatch span

**Files:** `internal/web/handler.go`, `cmd/holomush/gateway.go`

### 4c. Telnet Adapter (Gateway)

Create spans in `internal/telnet/gateway_handler.go`:

- `telnet.command` — root span per command, with session-scoped
  attributes (`session.id`, `session.start_time`, `character.id`)
  attached to each command span. Propagates context into the gRPC call.

Telnet sessions can last hours or days. Long-lived parent spans are an
anti-pattern (memory pressure in span processor, trace backend duration
limits). Instead of a `telnet.session` parent span, each command gets
its own root span with session attributes for correlation. Session
lifecycle events (connect/disconnect) SHOULD be logged via
`slog.InfoContext`, not traced.

Uses the same gRPC client interceptors as the web handler (shared dial).

**Files:** `internal/telnet/gateway_handler.go`

### 4d. Database (pgx)

Use `github.com/exaring/otelpgx` — a pgx tracer hook:

- Span per query with `db.statement`, `db.system`, duration
- Requires a two-step pool creation pattern:
  1. `pgxpool.ParseConfig(dsn)` to get a `*pgxpool.Config`
  2. Set `config.ConnConfig.Tracer = otelpgx.NewTracer()`
  3. `pgxpool.NewWithConfig(ctx, config)` to create the pool
- Zero changes to query call sites — pool-level hook
- Only production pool creation in `internal/store/postgres.go` needs
  modification. Test pool helpers MAY skip the tracer hook.

**Files:** `internal/store/postgres.go`, `go.mod`

### End-to-End Trace (Web)

```text
browser: fetch POST /holomush.web.v1.WebService/SendCommand
  gateway: otelhttp handler span
    gateway->core: otelgrpc client span
      core: otelgrpc server span (WorldService/ExecuteCommand)
        core: command.execute span (dispatcher)
          core: plugin.command span (Lua delivery)
            core: db.query span (pgx location lookup)
```

### End-to-End Trace (Telnet)

```text
telnet.command (per command, with session.id attribute)
  gateway->core: otelgrpc client span
    core: otelgrpc server span
      core: command.execute span
        ...same depth as web
```

---

## 5. Debug/Trace Logging Enhancement

### Logging Additions by System

| System           | Level | What to Log                                                     |
| ---------------- | ----- | --------------------------------------------------------------- |
| Auth/Session     | debug | session create/restore/destroy, character select, auth decision |
| Event Delivery   | debug | LISTEN/NOTIFY receipt, dispatch to subscriber, replay counts    |
| Plugin Lifecycle | debug | plugin load/unload, Lua VM creation, delivery timing            |
| Command Dispatch | debug | alias resolution, routing decisions, plugin match               |
| gRPC Handlers    | debug | RPC entry/exit with key params (character ID, method)           |
| Connection Mgmt  | debug | telnet/web connect/disconnect, connection count changes         |
| Bootstrap        | debug | demote routine startup messages (migration checks, seed skips)  |

### Exclusions

- No per-query DB logging (otelpgx traces cover this)
- No per-event-frame stream logging (too noisy even at debug)
- No request/response body logging (security concern)

### Convention

All debug logging MUST use `slog.DebugContext(ctx, ...)` (not
`slog.Debug`) so that trace\_id and span\_id from the logging handler's
trace context injection flow through automatically.

Existing `slog.Debug()` calls in files touched by boundary
instrumentation (Section 4) SHOULD be migrated to
`slog.DebugContext()` as part of the same change. This keeps migration
scoped to files already being modified rather than requiring a
separate pass.

### Approach

Debug logging additions MUST be done alongside boundary instrumentation
(Section 4), not as a separate pass. When adding spans to a file, add
the corresponding `slog.DebugContext` calls at the same time.

---

## 6. SvelteKit Frontend Tracing

### SDK Setup

New file: `web/src/lib/telemetry.ts`

Initialize once from root `+layout.svelte`:

- `@opentelemetry/sdk-trace-web` — TracerProvider with
  `BatchSpanProcessor`
- `@opentelemetry/exporter-trace-otlp-http` — exports to OTel
  Collector via HTTP (browsers cannot use gRPC)
- `@opentelemetry/instrumentation-fetch` — auto-instruments all
  `fetch()` calls, injects `traceparent` header
- `@opentelemetry/resources` and `@opentelemetry/semantic-conventions`

Browser async context propagation uses the default
`StackContextManager` (sufficient for spans created and ended within
the same synchronous scope). Zone.js is NOT used — it is heavy (~50KB),
monkey-patches browser async primitives, and conflicts with SvelteKit.

### Conditional Initialization

The SDK MUST initialize only when `PUBLIC_OTEL_ENDPOINT` is set
(SvelteKit `$env/static/public`). When unset, no SDK is loaded.

The value MUST be the base URL only (e.g., `http://localhost:4318`).
The OTel JS SDK appends `/v1/traces` automatically.

### Export Path

Browser -> OTLP HTTP (`http://localhost:4318/v1/traces`) -> OTel
Collector -> Tempo

### Shutdown

The `TracerProvider` SHOULD be shut down on `beforeunload` to flush
buffered spans:

```typescript
window.addEventListener('beforeunload', () => provider.shutdown());
```

Alternatively, configure `BatchSpanProcessor` with a short
`scheduledDelayMillis` (e.g., 1000ms) so spans export promptly
without relying on shutdown.

### Auto-Instrumentation

`FetchInstrumentation` auto-creates spans for every `fetch()`:

- All ConnectRPC calls (Login, SendCommand, StreamEvents, Disconnect)
- Propagates `traceparent` header — gateway `otelhttp` handler picks
  it up and links browser span to backend trace
- Context propagation into `fetch()` works without zone.js — the
  instrumentation patches `fetch` directly and injects headers from
  the active context at call time

### Custom Spans

| Span                | Location              | What It Captures                                              |
| ------------------- | --------------------- | ------------------------------------------------------------- |
| `command.roundtrip` | Command input handler | Start on send, end on matching command\_response event        |
| `stream.lifecycle`  | Event channel store   | Connect, reconnect events, disconnect. Duration = uptime.     |
| `session.restore`   | Auth store            | Time to restore from sessionStorage and validate              |
| `navigation`        | Root layout           | SvelteKit beforeNavigate/afterNavigate. Route transition time |

#### Command Roundtrip Correlation

The `command.roundtrip` span starts when `SendCommand` is called and
ends when the matching `command_response` event arrives on the event
stream. Correlation uses the command's request trace context: the
`SendCommand` fetch produces a `traceparent` that propagates through
the backend. The resulting `command_response` event arrives on
`StreamEvents` — the span ends when the next `command_response` event
is received. For simplicity, this uses a single in-flight command
assumption (one pending span at a time). If a second command is sent
before the first response arrives, the first span SHOULD be ended
with a timeout status.

### CORS

The OTel Collector MUST accept browser OTLP HTTP with CORS headers:

```yaml
receivers:
  otlp:
    protocols:
      http:
        endpoint: 0.0.0.0:4318
        cors:
          allowed-origins:
            - "http://localhost:*"
```

### Dependencies

All added as devDependencies (bundled into SvelteKit build):

- `@opentelemetry/sdk-trace-web`
- `@opentelemetry/exporter-trace-otlp-http`
- `@opentelemetry/instrumentation-fetch`
- `@opentelemetry/resources`
- `@opentelemetry/semantic-conventions`

### Future: RUM (Not Precluded)

The architecture supports adding `@opentelemetry/sdk-metrics-web` and
the `web-vitals` library later for Core Web Vitals, long task detection,
and error boundary tracking. This is a separate instrumentation domain
(metrics, not traces) and SHOULD be a follow-up effort.

---

## 7. Docker Compose Observability Stack

### Activation

All observability services MUST use Docker Compose profiles:

```yaml
services:
  otel-collector:
    profiles: [observability]
    # ...
```

Activated via `COMPOSE_PROFILES=observability docker compose up` or a
convenience Taskfile target `task dev:obs`.

### Services

**OTel Collector** (`otel/opentelemetry-collector-contrib`):

- Receives OTLP gRPC (4317) from Go services
- Receives OTLP HTTP (4318) from browser (with CORS)
- Exports traces to Tempo via OTLP
- Exports metrics to Prometheus via prometheusremotewrite or
  Prometheus scrapes the collector
- Config: `docker/otel-collector/config.yaml`

**Tempo** (`grafana/tempo`):

- Receives traces from collector via OTLP gRPC
- Local storage (ephemeral for dev, short retention ~1h)
- No UI — Grafana queries it
- Config: `docker/tempo/config.yaml`

**Prometheus** (`prom/prometheus`):

- Scrapes `/metrics` from core (9100) and gateway (9101)
- Scrape interval: 15s
- Retention: 1h (dev)
- No alerting rules
- Config: `docker/prometheus/prometheus.yaml`

**Grafana** (`grafana/grafana`):

- Port: 3001 (avoids conflict with SvelteKit dev server on 3000)
- Anonymous auth enabled (no login for dev)
- Auto-provisioned datasources: Prometheus and Tempo
- Dashboard provisioning directory exists but is empty (sight line
  to pre-built dashboards later)
- Config: `docker/grafana/provisioning/`

**Dozzle** (`amir20/dozzle`):

- Port: 8888
- Read-only Docker socket mount
- Zero configuration

### Configuration Directory Layout

```text
docker/
  grafana/
    provisioning/
      datasources/
        datasources.yaml
      dashboards/
        dashboards.yaml
  otel-collector/
    config.yaml
  prometheus/
    prometheus.yaml
  tempo/
    config.yaml
```

### OTLP Endpoint Injection

`OTEL_EXPORTER_OTLP_ENDPOINT: http://otel-collector:4317` MUST be set
unconditionally on the `core` and `gateway` services in `compose.yaml`.

When the observability profile is inactive, the collector is unreachable.
The OTel SDK handles this gracefully — the OTLP exporter retries with
backoff and drops spans silently. A no-op OTel error handler (Section 3)
suppresses retry/connection warnings to prevent log pollution during
normal `task dev` usage.

When the profile is active, the collector is running and traces flow
through immediately.

### Taskfile Target

A `dev:obs` task MUST be added:

```yaml
dev:obs:
  desc: Start dev environment with observability stack
  cmds:
    - COMPOSE_PROFILES=observability docker compose up --build
```

### Exposed Ports Summary

| Service   | Port | Purpose              |
| --------- | ---- | -------------------- |
| Grafana   | 3001 | Traces + metrics UI  |
| Dozzle    | 8888 | Container log viewer |
| Collector | 4317 | OTLP gRPC (internal) |
| Collector | 4318 | OTLP HTTP (browser)  |

Prometheus (9090) and Tempo (3200) do NOT need exposed ports — Grafana
accesses them via the Docker network.

---

## 8. PR Strategy

Three stacked PRs using jj, each building on the previous:

### PR 1: `feat: configurable log level and OTel SDK initialization`

- Log level flag and env var (Section 2)
- `internal/telemetry/` package (Section 3)
- Integration into `core.go` and `gateway.go` startup/shutdown
- Unit tests for telemetry init and log level parsing

**Value:** Immediate `--log-level debug`. OTel SDK wired, ready for
a collector.

### PR 2: `feat: distributed tracing at system boundaries`

- gRPC server and client interceptors (Section 4a, 4b)
- HTTP/ConnectRPC otelhttp wrapper (Section 4b)
- Telnet command spans with session attributes (Section 4c)
- pgx otelpgx tracer hook (Section 4d)
- Debug logging alongside each boundary (Section 5)
- New dependency: `otelpgx`

**Value:** Full end-to-end traces. Works with any OTLP-compatible
collector.

### PR 3: `feat: observability compose stack and frontend tracing`

- Docker Compose services with `profiles: [observability]` (Section 7)
- `docker/` configuration directory
- SvelteKit telemetry setup, auto-instrumentation, custom spans
  (Section 6)
- `task dev:obs` Taskfile target
- Operator docs update

**Value:** `task dev:obs` delivers the full stack with browser-to-DB
trace correlation.

### jj Stack

```text
main
  pr1 (log-level + SDK init)
    pr2 (boundary instrumentation)
      pr3 (compose + frontend)
```

Review feedback on any PR propagates through the stack via
`jj rebase`.

---

## 9. Dependencies

### Go (added or newly used)

| Package                                                                        | Status          | Purpose                   |
| ------------------------------------------------------------------------------ | --------------- | ------------------------- |
| `go.opentelemetry.io/otel/sdk` (existing, unused)                             | activate        | TracerProvider setup      |
| `go.opentelemetry.io/otel/sdk/metric` (existing, unused)                      | activate        | MeterProvider setup       |
| `go.opentelemetry.io/otel/exporters/otlp/otlptrace` (existing)               | activate        | OTLP trace exporter       |
| `go.opentelemetry.io/otel/exporters/otlp/otlptracegrpc`                       | new             | gRPC transport for traces |
| `go.opentelemetry.io/otel/exporters/otlp/otlpmetric`                          | new             | OTLP metric exporter      |
| `go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc` | new             | gRPC interceptors         |
| `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` (existing)    | activate        | HTTP middleware            |
| `github.com/exaring/otelpgx`                                                  | new             | pgx query tracing         |

Before adding SDK initialization code, `otel/sdk` and `otel/sdk/metric`
SHOULD be updated to match the `otel` core version (currently v1.42.0
vs v1.41.0 for SDK packages). OTel Go modules within the same repo are
versioned together and version skew can cause subtle incompatibilities.

### JavaScript (devDependencies)

| Package                                    | Purpose                   |
| ------------------------------------------ | ------------------------- |
| `@opentelemetry/sdk-trace-web`             | Browser TracerProvider    |
| `@opentelemetry/exporter-trace-otlp-http`  | OTLP HTTP exporter        |
| `@opentelemetry/instrumentation-fetch`     | Auto-instrument fetch()   |
| `@opentelemetry/resources`                 | Resource attributes       |
| `@opentelemetry/semantic-conventions`      | Standard attribute names  |

---

## 10. Testing Strategy

### Unit Tests

- `internal/telemetry/` — test Init with/without env var, verify
  shutdown flushes, verify resource attributes
- `internal/logging/` — test log level parsing and application
- `cmd/holomush/` — test flag/env var precedence

### Integration Tests

- Verify gRPC interceptors produce spans (use in-memory span exporter
  in test)
- Verify otelhttp middleware produces spans
- Verify otelpgx produces query spans against testcontainers Postgres

### E2E Verification

- `task dev:obs` starts all services without errors
- Grafana datasources are auto-provisioned and healthy
- A command sent via the web UI produces a trace visible in
  Grafana Tempo
- Prometheus scrapes show core and gateway metrics
