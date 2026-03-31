# Observability and Telemetry Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development
> (if subagents available) or superpowers:executing-plans to implement this plan.
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add configurable log levels, distributed tracing across all system
boundaries, frontend trace instrumentation, and a profile-gated Docker Compose
observability stack for local development.

**Architecture:** Three stacked jj PRs. PR 1 adds log level config and OTel SDK
init. PR 2 adds boundary instrumentation (gRPC, HTTP, telnet, DB) with debug
logging. PR 3 adds Docker Compose obs stack and SvelteKit frontend tracing.

**Tech Stack:** Go slog, OpenTelemetry Go SDK, OTel JS SDK for browser,
otelpgx, otelgrpc, otelhttp, Docker Compose profiles, Grafana, Tempo,
Prometheus, Dozzle.

**Spec:** `docs/superpowers/specs/2026-03-31-observability-and-telemetry-design.md`

---

## Chunk 1: Log Level Configuration + OTel SDK Init (PR 1)

### File Map

| Action | Path                               | Responsibility                                |
| ------ | ---------------------------------- | --------------------------------------------- |
| Modify | `cmd/holomush/root.go`             | Add `--log-level` persistent flag + env var   |
| Modify | `internal/logging/handler.go`      | Accept `slog.Level` param in Setup/SetDefault |
| Modify | `internal/logging/handler_test.go` | Test level parameterization                   |
| Modify | `cmd/holomush/core.go`             | Replace `setupLogging()` with `logging.SetDefault()`, add telemetry init |
| Modify | `cmd/holomush/core_test.go`        | Update setupLogging tests for new API         |
| Modify | `cmd/holomush/gateway.go`          | Same: replace `setupLogging()`, add telemetry init |
| Create | `internal/telemetry/provider.go`   | OTel SDK conditional init + shutdown          |
| Create | `internal/telemetry/provider_test.go` | Test init with/without env var             |

### Task 1: Add `--log-level` flag to root command

**Files:**

- Modify: `cmd/holomush/root.go:10-32`

- [ ] **Step 1: Add log level variable and persistent flag**

Add a `logLevel` package var and a `--log-level` persistent flag bound to
`LOG_LEVEL` env var:

```go
var (
    configFile string
    logLevel   string
)

func NewRootCmd() *cobra.Command {
    cmd := &cobra.Command{...}

    cmd.PersistentFlags().StringVar(&configFile, "config", "", "config file path")
    cmd.PersistentFlags().StringVar(&logLevel, "log-level", "info",
        "log level (debug, info, warn, error)")

    return cmd
}
```

- [ ] **Step 2: Verify build compiles**

Run: `task build`
Expected: builds successfully

- [ ] **Step 3: Commit**

```text
feat(cli): add --log-level persistent flag to root command
```

### Task 2: Parameterize logging.Setup with slog.Level

**Files:**

- Modify: `internal/logging/handler.go:67-99`
- Create: `internal/logging/handler_test.go` (or modify if exists)

- [ ] **Step 1: Write tests for level parameterization**

```go
func TestSetup_LevelFiltering(t *testing.T) {
    tests := []struct {
        name     string
        level    slog.Level
        logLevel slog.Level
        want     bool
    }{
        {"debug enabled at debug", slog.LevelDebug, slog.LevelDebug, true},
        {"debug disabled at info", slog.LevelDebug, slog.LevelInfo, false},
        {"info enabled at info", slog.LevelInfo, slog.LevelInfo, true},
        {"warn enabled at info", slog.LevelWarn, slog.LevelInfo, true},
        {"error enabled at error", slog.LevelError, slog.LevelError, true},
        {"info disabled at error", slog.LevelInfo, slog.LevelError, false},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            logger := Setup("test", "1.0", "json", nil, tt.level)
            assert.Equal(t, tt.want, logger.Handler().Enabled(context.Background(), tt.logLevel))
        })
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/logging/ -run TestSetup_LevelFiltering -v`
Expected: FAIL — `Setup` has wrong signature (no level param)

- [ ] **Step 3: Update Setup and SetDefault signatures**

Change `Setup` to accept a `slog.Level` parameter and use it in
`HandlerOptions`. Update `SetDefault` to pass through:

```go
func Setup(service, version, format string, w io.Writer, level slog.Level) *slog.Logger {
    if w == nil {
        w = os.Stderr
    }
    var baseHandler slog.Handler
    opts := &slog.HandlerOptions{Level: level}
    if format == "text" {
        baseHandler = slog.NewTextHandler(w, opts)
    } else {
        baseHandler = slog.NewJSONHandler(w, opts)
    }
    handler := &traceHandler{handler: baseHandler, service: service, version: version}
    return slog.New(handler)
}

func SetDefault(service, version, format string, level slog.Level) {
    logger := Setup(service, version, format, nil, level)
    slog.SetDefault(logger)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/logging/ -run TestSetup_LevelFiltering -v`
Expected: PASS

- [ ] **Step 5: Commit**

```text
feat(logging): parameterize Setup/SetDefault with slog.Level
```

### Task 3: Replace setupLogging with logging.SetDefault in core and gateway

**Files:**

- Modify: `cmd/holomush/core.go:208,842-861`
- Modify: `cmd/holomush/core_test.go` (update setupLogging tests)
- Modify: `cmd/holomush/gateway.go:141`

- [ ] **Step 1: Add parseLogLevel helper**

Add a helper in `cmd/holomush/root.go` (or a new `cmd/holomush/logging.go`)
that parses the string log level:

```go
func parseLogLevel(s string) (slog.Level, error) {
    switch strings.ToLower(s) {
    case "debug":
        return slog.LevelDebug, nil
    case "info":
        return slog.LevelInfo, nil
    case "warn":
        return slog.LevelWarn, nil
    case "error":
        return slog.LevelError, nil
    default:
        return slog.LevelInfo, oops.Code("CONFIG_INVALID").
            Errorf("invalid log level %q: must be debug, info, warn, or error", s)
    }
}
```

- [ ] **Step 2: Replace setupLogging calls in core.go and gateway.go**

In `core.go` line 208, replace:

```go
if err := setupLogging(cfg.LogFormat); err != nil {
```

With:

```go
level, err := parseLogLevel(logLevel)
if err != nil {
    return err
}
logging.SetDefault("holomush-core", version, cfg.LogFormat, level)
```

Same pattern in `gateway.go` line 141, using `"holomush-gateway"`.

- [ ] **Step 3: Delete the old setupLogging function**

Remove `setupLogging` from `core.go:842-861`.

- [ ] **Step 4: Update core_test.go**

Replace `setupLogging` tests with `parseLogLevel` tests:

```go
func TestParseLogLevel(t *testing.T) {
    tests := []struct {
        input string
        want  slog.Level
        err   bool
    }{
        {"debug", slog.LevelDebug, false},
        {"INFO", slog.LevelInfo, false},
        {"Warn", slog.LevelWarn, false},
        {"error", slog.LevelError, false},
        {"invalid", slog.LevelInfo, true},
    }
    for _, tt := range tests {
        t.Run(tt.input, func(t *testing.T) {
            got, err := parseLogLevel(tt.input)
            if tt.err {
                assert.Error(t, err)
            } else {
                require.NoError(t, err)
                assert.Equal(t, tt.want, got)
            }
        })
    }
}
```

- [ ] **Step 5: Run all tests**

Run: `task test`
Expected: PASS — no regressions. The `logging.SetDefault` call now uses
the `traceHandler` wrapper (which was being bypassed by the old
`setupLogging`), so trace context injection is automatically enabled.

- [ ] **Step 6: Commit**

```text
refactor(logging): consolidate onto logging.SetDefault with trace context

Replace the local setupLogging() in core.go/gateway.go with
logging.SetDefault(), which uses the traceHandler wrapper for
automatic trace_id/span_id injection. Add --log-level flag
parsing via parseLogLevel().
```

### Task 4: Create internal/telemetry package

**Files:**

- Create: `internal/telemetry/provider.go`
- Create: `internal/telemetry/provider_test.go`

- [ ] **Step 1: Write tests for Init**

```go
func TestInit_NoEndpoint(t *testing.T) {
    t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
    shutdown, err := Init(context.Background(), "test-svc", "1.0.0")
    require.NoError(t, err)
    require.NotNil(t, shutdown)
    // Shutdown should be no-op
    require.NoError(t, shutdown(context.Background()))
}

func TestInit_WithEndpoint(t *testing.T) {
    // Use a non-routable address so the exporter doesn't actually connect
    t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://192.0.2.1:4317")
    shutdown, err := Init(context.Background(), "test-svc", "1.0.0")
    require.NoError(t, err)
    require.NotNil(t, shutdown)

    // Verify global tracer provider is real (not no-op)
    tp := otel.GetTracerProvider()
    tracer := tp.Tracer("test")
    _, span := tracer.Start(context.Background(), "test-span")
    assert.True(t, span.SpanContext().IsValid())
    span.End()

    // Clean shutdown (will timeout trying to export, but should not error)
    ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
    defer cancel()
    _ = shutdown(ctx) // May return context deadline exceeded, that's OK
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/telemetry/ -v`
Expected: FAIL — package doesn't exist

- [ ] **Step 3: Implement provider.go**

```go
// Package telemetry provides OpenTelemetry SDK lifecycle management.
package telemetry

import (
    "context"
    "os"

    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
    "go.opentelemetry.io/otel/propagation"
    "go.opentelemetry.io/otel/sdk/resource"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Init initializes the OpenTelemetry SDK if OTEL_EXPORTER_OTLP_ENDPOINT is set.
// Returns a shutdown function that flushes and shuts down all providers.
// If the env var is unset, returns a no-op shutdown and nil error.
func Init(ctx context.Context, serviceName, serviceVersion string) (func(context.Context) error, error) {
    endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
    if endpoint == "" {
        return func(context.Context) error { return nil }, nil
    }

    // Suppress OTel internal error logging (exporter retries when collector unreachable)
    otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {}))

    res, err := resource.Merge(
        resource.Default(),
        resource.NewWithAttributes(
            semconv.SchemaURL,
            semconv.ServiceName(serviceName),
            semconv.ServiceVersion(serviceVersion),
        ),
    )
    if err != nil {
        return nil, oops.With("operation", "create otel resource").Wrap(err)
    }

    traceExporter, err := otlptracegrpc.New(ctx)
    if err != nil {
        return nil, oops.With("operation", "create trace exporter").Wrap(err)
    }

    tp := sdktrace.NewTracerProvider(
        sdktrace.WithBatcher(traceExporter),
        sdktrace.WithResource(res),
        sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
    )

    otel.SetTracerProvider(tp)
    otel.SetTextMapPropagator(propagation.TraceContext{})

    shutdown := func(ctx context.Context) error {
        return tp.Shutdown(ctx)
    }
    return shutdown, nil
}
```

Note: The `oops` import needs adding. The OTLP metric exporter can be
added as a follow-up — traces are the primary value. Start with traces
only to keep the initial PR focused.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/telemetry/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```text
feat(telemetry): add OTel SDK conditional initialization

New internal/telemetry package. Init() checks for
OTEL_EXPORTER_OTLP_ENDPOINT and initializes TracerProvider
with OTLP gRPC exporter when set, no-op when unset. Suppresses
OTel internal error handler to prevent log noise when collector
is unreachable.
```

### Task 5: Integrate telemetry.Init into core.go and gateway.go

**Files:**

- Modify: `cmd/holomush/core.go:208-212`
- Modify: `cmd/holomush/gateway.go:141-143`

- [ ] **Step 1: Add telemetry.Init call to core.go**

After the `logging.SetDefault` call (from Task 3), add:

```go
telemetryShutdown, telErr := telemetry.Init(ctx, "holomush-core", version)
if telErr != nil {
    return oops.Code("TELEMETRY_INIT_FAILED").Wrap(telErr)
}
defer func() {
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    if err := telemetryShutdown(shutdownCtx); err != nil {
        slog.Warn("telemetry shutdown error", "error", err)
    }
}()
```

Add the import: `"github.com/holomush/holomush/internal/telemetry"`

- [ ] **Step 2: Same for gateway.go**

Same pattern with `"holomush-gateway"`.

- [ ] **Step 3: Update OTel SDK dependency versions**

Run: `go get go.opentelemetry.io/otel/sdk@v1.42.0 go.opentelemetry.io/otel/sdk/metric@v1.42.0`
Then: `go mod tidy`

- [ ] **Step 4: Run full test suite**

Run: `task test && task lint`
Expected: PASS

- [ ] **Step 5: Commit**

```text
feat(core,gateway): wire OTel SDK initialization into startup

Call telemetry.Init() early in both core and gateway startup.
Defers shutdown to flush spans on graceful termination. Updates
OTel SDK packages to v1.42.0 to match core otel version.
```

---

## Chunk 2: System Boundary Instrumentation (PR 2)

### File Map

| Action | Path                                   | Responsibility                          |
| ------ | -------------------------------------- | --------------------------------------- |
| Modify | `internal/grpc/server.go:1030-1046`    | Add otelgrpc server interceptors        |
| Modify | `internal/grpc/client.go:44-86`        | Add otelgrpc client interceptors        |
| Modify | `internal/web/server.go:33-59`         | Wrap handler with otelhttp              |
| Modify | `internal/telnet/gateway_handler.go`   | Add per-command trace spans             |
| Modify | `internal/store/postgres.go:62-68`     | Two-step pgxpool with otelpgx tracer    |
| Modify | `go.mod`                               | Add otelgrpc, otelpgx dependencies      |

### Task 6: Add otelgrpc server interceptors

**Files:**

- Modify: `internal/grpc/server.go:1030-1046`

- [ ] **Step 1: Update NewGRPCServer to include interceptors**

```go
func NewGRPCServer(tlsConfig *tls.Config) *grpc.Server {
    creds := credentials.NewTLS(tlsConfig)
    return grpc.NewServer(
        grpc.Creds(creds),
        grpc.StatsHandler(otelgrpc.NewServerHandler()),
    )
}
```

The `otelgrpc.NewServerHandler()` approach (stats handler) is preferred
over the deprecated interceptor API. It handles both unary and streaming.

Also update `NewGRPCServerInsecure` with the same stats handler.

- [ ] **Step 2: Add import and dependency**

Run: `go get go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc`

Add import to `server.go`:

```go
"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
```

- [ ] **Step 3: Run tests**

Run: `task test`
Expected: PASS — interceptors are no-op when no TracerProvider is set

- [ ] **Step 4: Commit**

```text
feat(grpc): add otelgrpc server stats handler for distributed tracing
```

### Task 7: Add otelgrpc client interceptors

**Files:**

- Modify: `internal/grpc/client.go:59-66`

- [ ] **Step 1: Add stats handler to client dial options**

In `NewClient`, add the OTel stats handler to the options slice:

```go
opts := []grpc.DialOption{
    grpc.WithKeepaliveParams(keepalive.ClientParameters{...}),
    grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
}
```

- [ ] **Step 2: Run tests**

Run: `task test`
Expected: PASS

- [ ] **Step 3: Commit**

```text
feat(grpc): add otelgrpc client stats handler for trace propagation
```

### Task 8: Wrap HTTP handler with otelhttp

**Files:**

- Modify: `internal/web/server.go:33-59`

- [ ] **Step 1: Add otelhttp wrapper in NewServer**

After the CORS middleware and before assigning to `httpServer.Handler`,
wrap with `otelhttp.NewHandler`:

```go
// Wrap with CORS if origins configured
if len(cfg.CORSOrigins) > 0 {
    handler = CORSMiddleware(cfg.CORSOrigins, handler)
}

// Wrap with OpenTelemetry HTTP instrumentation
handler = otelhttp.NewHandler(handler, "holomush-gateway")
```

Add import: `"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"`

- [ ] **Step 2: Run tests**

Run: `task test`
Expected: PASS

- [ ] **Step 3: Commit**

```text
feat(web): wrap HTTP handler with otelhttp for request tracing
```

### Task 9: Add telnet command spans

**Files:**

- Modify: `internal/telnet/gateway_handler.go`

- [ ] **Step 1: Add tracer var and span creation**

Add a package-level tracer:

```go
var tracer = otel.Tracer("holomush.telnet")
```

In the command processing function (where `h.client.HandleCommand` is
called around line 537-546), wrap each command in a span:

```go
ctx, span := tracer.Start(ctx, "telnet.command",
    trace.WithAttributes(
        attribute.String("session.id", h.sessionID),
        attribute.String("character.id", h.characterID),
        attribute.String("command", cmd),
    ),
)
defer span.End()
```

Also add `slog.DebugContext` for connect/disconnect:

```go
slog.DebugContext(ctx, "telnet: client connected",
    "remote_addr", conn.RemoteAddr().String(),
)
```

- [ ] **Step 2: Add debug logging for session lifecycle**

Add `slog.DebugContext` calls at:

- Connection established
- Authentication success/failure
- Character selection
- Disconnect

Migrate any existing `slog.Debug()` calls in this file to
`slog.DebugContext()`.

- [ ] **Step 3: Run tests**

Run: `task test`
Expected: PASS

- [ ] **Step 4: Commit**

```text
feat(telnet): add per-command trace spans with session attributes

Each telnet command gets its own root span with session.id,
character.id, and command name attributes. Session lifecycle
events logged at debug level with context for trace correlation.
```

### Task 10: Add otelpgx database tracing

**Files:**

- Modify: `internal/store/postgres.go:62-68`

- [ ] **Step 1: Add otelpgx dependency**

Run: `go get github.com/exaring/otelpgx`

- [ ] **Step 2: Update NewPostgresEventStore to use ParseConfig**

Replace the one-step `pgxpool.New` with the two-step pattern:

```go
func NewPostgresEventStore(ctx context.Context, dsn string) (*PostgresEventStore, error) {
    cfg, err := pgxpool.ParseConfig(dsn)
    if err != nil {
        return nil, oops.With("operation", "parse database config").Wrap(err)
    }
    cfg.ConnConfig.Tracer = otelpgx.NewTracer()

    pool, err := pgxpool.NewWithConfig(ctx, cfg)
    if err != nil {
        return nil, oops.With("operation", "connect to database").Wrap(err)
    }
    // ... rest unchanged
}
```

- [ ] **Step 3: Run tests (unit + integration)**

Run: `task test && task test:int`
Expected: PASS — otelpgx is no-op when no TracerProvider is registered

- [ ] **Step 4: Commit**

```text
feat(store): add otelpgx tracer hook for database query tracing

Switch from pgxpool.New to pgxpool.ParseConfig + NewWithConfig
to attach the otelpgx tracer. Creates spans for every query with
db.statement and duration attributes.
```

### Task 11: Add debug logging at remaining boundaries

**Files:**

- Modify: various files as listed in spec Section 5

- [ ] **Step 1: Add debug logging to gRPC handlers**

In `internal/grpc/server.go`, add `slog.DebugContext` at key handler
entry/exit points (HandleCommand, Authenticate, Subscribe). Example:

```go
slog.DebugContext(ctx, "grpc: HandleCommand",
    "session_id", req.GetSessionId(),
    "command", req.GetCommand(),
)
```

- [ ] **Step 2: Add debug logging to web handler**

In `internal/web/handler.go`, add `slog.DebugContext` at Login,
SendCommand, StreamEvents entry points.

- [ ] **Step 3: Migrate existing slog.Debug to slog.DebugContext**

In any file touched in this PR, replace `slog.Debug(` with
`slog.DebugContext(ctx,` where a context is available.

- [ ] **Step 4: Run tests and lint**

Run: `task test && task lint`
Expected: PASS

- [ ] **Step 5: Commit**

```text
feat(logging): add debug logging at system boundaries

Add slog.DebugContext calls at gRPC handler entry, web handler
entry, and connection lifecycle events. Migrate existing
slog.Debug calls to slog.DebugContext in touched files for
trace context correlation.
```

---

## Chunk 3: Docker Compose Stack + Frontend Tracing (PR 3)

### File Map

| Action | Path                                                  | Responsibility                     |
| ------ | ----------------------------------------------------- | ---------------------------------- |
| Create | `docker/otel-collector/config.yaml`                   | Collector receivers/exporters      |
| Create | `docker/tempo/config.yaml`                            | Tempo storage config               |
| Create | `docker/prometheus/prometheus.yaml`                   | Scrape targets                     |
| Create | `docker/grafana/provisioning/datasources/datasources.yaml` | Auto-provision datasources    |
| Create | `docker/grafana/provisioning/dashboards/dashboards.yaml`   | Empty provider (sight line)   |
| Modify | `compose.yaml`                                        | Add obs services + OTLP env var    |
| Modify | `Taskfile.yaml`                                       | Add dev:obs target                 |
| Create | `web/src/lib/telemetry.ts`                            | Browser OTel SDK setup             |
| Modify | `web/src/routes/+layout.svelte`                       | Init telemetry on mount            |
| Modify | `web/src/lib/stores/authStore.ts`                     | session.restore span               |
| Modify | `web/src/routes/terminal/+page.svelte` (or equivalent) | command.roundtrip span            |
| Modify | `web/package.json`                                    | Add OTel devDependencies           |

### Task 12: Create OTel Collector config

**Files:**

- Create: `docker/otel-collector/config.yaml`

- [ ] **Step 1: Write collector config**

```yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
      http:
        endpoint: 0.0.0.0:4318
        cors:
          allowed-origins:
            - "http://localhost:*"

exporters:
  otlp/tempo:
    endpoint: tempo:4317
    tls:
      insecure: true

processors:
  batch:

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlp/tempo]
```

- [ ] **Step 2: Commit**

```text
feat(docker): add OTel Collector configuration
```

### Task 13: Create Tempo config

**Files:**

- Create: `docker/tempo/config.yaml`

- [ ] **Step 1: Write Tempo config**

```yaml
server:
  http_listen_port: 3200

distributor:
  receivers:
    otlp:
      protocols:
        grpc:

storage:
  trace:
    backend: local
    local:
      path: /tmp/tempo/blocks
    wal:
      path: /tmp/tempo/wal

overrides:
  defaults:
    global:
      max_traces_per_user: 0
```

- [ ] **Step 2: Commit**

```text
feat(docker): add Tempo trace storage configuration
```

### Task 14: Create Prometheus config

**Files:**

- Create: `docker/prometheus/prometheus.yaml`

- [ ] **Step 1: Write Prometheus scrape config**

```yaml
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: holomush-core
    static_configs:
      - targets: ["core:9100"]
  - job_name: holomush-gateway
    static_configs:
      - targets: ["gateway:9101"]
```

- [ ] **Step 2: Commit**

```text
feat(docker): add Prometheus scrape configuration
```

### Task 15: Create Grafana provisioning

**Files:**

- Create: `docker/grafana/provisioning/datasources/datasources.yaml`
- Create: `docker/grafana/provisioning/dashboards/dashboards.yaml`

- [ ] **Step 1: Write datasources provisioning**

```yaml
apiVersion: 1
datasources:
  - name: Prometheus
    type: prometheus
    access: proxy
    url: http://prometheus:9090
    isDefault: true

  - name: Tempo
    type: tempo
    access: proxy
    url: http://tempo:3200
    jsonData:
      tracesToMetrics:
        datasourceUid: prometheus
      serviceMap:
        datasourceUid: prometheus
```

- [ ] **Step 2: Write empty dashboards provider**

```yaml
apiVersion: 1
providers: []
```

- [ ] **Step 3: Commit**

```text
feat(docker): add Grafana datasource provisioning for Prometheus and Tempo
```

### Task 16: Update compose.yaml with observability services

**Files:**

- Modify: `compose.yaml`

- [ ] **Step 1: Add OTEL_EXPORTER_OTLP_ENDPOINT to core and gateway**

Add to core service environment:

```yaml
OTEL_EXPORTER_OTLP_ENDPOINT: http://otel-collector:4317
```

Same for gateway.

- [ ] **Step 2: Add profile-gated observability services**

Append to compose.yaml services section:

```yaml
  otel-collector:
    image: otel/opentelemetry-collector-contrib:latest
    profiles: [observability]
    volumes:
      - ./docker/otel-collector/config.yaml:/etc/otelcol-contrib/config.yaml
    ports:
      - "4317:4317"
      - "4318:4318"
    depends_on:
      - tempo

  tempo:
    image: grafana/tempo:latest
    profiles: [observability]
    volumes:
      - ./docker/tempo/config.yaml:/etc/tempo/config.yaml
    command: ["-config.file=/etc/tempo/config.yaml"]

  prometheus:
    image: prom/prometheus:latest
    profiles: [observability]
    volumes:
      - ./docker/prometheus/prometheus.yaml:/etc/prometheus/prometheus.yml
    depends_on:
      core:
        condition: service_healthy

  grafana:
    image: grafana/grafana:latest
    profiles: [observability]
    ports:
      - "3001:3000"
    environment:
      GF_AUTH_ANONYMOUS_ENABLED: "true"
      GF_AUTH_ANONYMOUS_ORG_ROLE: Admin
    volumes:
      - ./docker/grafana/provisioning:/etc/grafana/provisioning
    depends_on:
      - prometheus
      - tempo

  dozzle:
    image: amir20/dozzle:latest
    profiles: [observability]
    ports:
      - "8888:8080"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
```

- [ ] **Step 3: Verify compose config is valid**

Run: `docker compose config --quiet`
Expected: exits 0

- [ ] **Step 4: Commit**

```text
feat(docker): add profile-gated observability stack

Adds OTel Collector, Tempo, Prometheus, Grafana, and Dozzle
behind the 'observability' Docker Compose profile. Activated
via COMPOSE_PROFILES=observability or task dev:obs.
```

### Task 17: Add dev:obs Taskfile target

**Files:**

- Modify: `Taskfile.yaml`

- [ ] **Step 1: Add dev:obs task**

Add after the existing `dev:clean` task:

```yaml
  dev:obs:
    desc: Start dev environment with observability stack (Grafana, Tempo, Prometheus, Dozzle)
    cmds:
      - task: docker:build
      - COMPOSE_PROFILES=observability docker compose up --force-recreate --remove-orphans
```

- [ ] **Step 2: Commit**

```text
feat(taskfile): add dev:obs target for observability stack
```

### Task 18: Create SvelteKit telemetry module

**Files:**

- Create: `web/src/lib/telemetry.ts`
- Modify: `web/package.json`

- [ ] **Step 1: Install OTel JS dependencies**

Run in `web/` directory:

```bash
pnpm add -D @opentelemetry/sdk-trace-web \
  @opentelemetry/exporter-trace-otlp-http \
  @opentelemetry/instrumentation-fetch \
  @opentelemetry/resources \
  @opentelemetry/semantic-conventions
```

- [ ] **Step 2: Create telemetry.ts**

```typescript
import { env } from '$env/static/public';
import { WebTracerProvider, BatchSpanProcessor } from '@opentelemetry/sdk-trace-web';
import { OTLPTraceExporter } from '@opentelemetry/exporter-trace-otlp-http';
import { FetchInstrumentation } from '@opentelemetry/instrumentation-fetch';
import { Resource } from '@opentelemetry/resources';
import { ATTR_SERVICE_NAME, ATTR_SERVICE_VERSION } from '@opentelemetry/semantic-conventions';
import { registerInstrumentations } from '@opentelemetry/instrumentation';

let initialized = false;

export function initTelemetry(): void {
  const endpoint = env.PUBLIC_OTEL_ENDPOINT;
  if (!endpoint || initialized) return;
  initialized = true;

  const provider = new WebTracerProvider({
    resource: new Resource({
      [ATTR_SERVICE_NAME]: 'holomush-web',
      [ATTR_SERVICE_VERSION]: '0.1.0',
    }),
    spanProcessors: [
      new BatchSpanProcessor(
        new OTLPTraceExporter({ url: `${endpoint}/v1/traces` }),
        { scheduledDelayMillis: 1000 }
      ),
    ],
  });

  provider.register();

  registerInstrumentations({
    instrumentations: [
      new FetchInstrumentation({
        propagateTraceHeaderCorsUrls: [/localhost/],
      }),
    ],
  });

  window.addEventListener('beforeunload', () => {
    provider.shutdown();
  });
}
```

- [ ] **Step 3: Commit**

```text
feat(web): add OTel browser SDK with fetch auto-instrumentation

Conditional on PUBLIC_OTEL_ENDPOINT. Exports traces to OTel
Collector via OTLP HTTP. Auto-instruments all fetch() calls
with traceparent propagation.
```

### Task 19: Initialize telemetry from layout and add custom spans

**Files:**

- Modify: `web/src/routes/+layout.svelte`
- Modify: `web/src/lib/stores/authStore.ts`

- [ ] **Step 1: Call initTelemetry from layout**

In `+layout.svelte`, add to the `onMount`:

```svelte
<script lang="ts">
  import { initTelemetry } from '$lib/telemetry';
  import TopBar from '$lib/components/TopBar.svelte';
  import { restoreSession } from '$lib/stores/authStore';
  import { onMount } from 'svelte';
  let { children } = $props();
  onMount(() => {
    initTelemetry();
    restoreSession();
  });
</script>
```

- [ ] **Step 2: Add session.restore span to authStore**

In `authStore.ts`, wrap `restoreSession` with a manual span:

```typescript
import { trace } from '@opentelemetry/api';

const tracer = trace.getTracer('holomush-web');

export function restoreSession(): void {
  const span = tracer.startSpan('session.restore');
  try {
    // ... existing restore logic ...
  } finally {
    span.end();
  }
}
```

Note: Import `@opentelemetry/api` as a devDependency:
`pnpm add -D @opentelemetry/api`

- [ ] **Step 3: Add navigation spans to layout**

Use SvelteKit navigation hooks:

```svelte
<script lang="ts">
  import { beforeNavigate, afterNavigate } from '$app/navigation';
  import { trace } from '@opentelemetry/api';

  const tracer = trace.getTracer('holomush-web');
  let navSpan: ReturnType<typeof tracer.startSpan> | null = null;

  beforeNavigate(({ to }) => {
    navSpan = tracer.startSpan('navigation', {
      attributes: { 'navigation.to': to?.url.pathname ?? 'unknown' },
    });
  });

  afterNavigate(() => {
    navSpan?.end();
    navSpan = null;
  });
</script>
```

- [ ] **Step 4: Run web build to verify**

Run: `cd web && pnpm build`
Expected: builds without errors

- [ ] **Step 5: Commit**

```text
feat(web): initialize OTel from layout with custom spans

Add session.restore span in authStore, navigation spans via
SvelteKit hooks. Auto-instrumentation captures all fetch/ConnectRPC
calls with trace context propagation to backend.
```

### Task 20: Rebuild embedded web dist and verify

**Files:**

- Modify: `internal/web/dist/` (rebuilt from SvelteKit)

- [ ] **Step 1: Rebuild web dist**

Run: `cd web && pnpm build`

The build output in `internal/web/dist/` is committed (embedded in Go binary).

- [ ] **Step 2: Run full test suite**

Run: `task test && task lint`
Expected: PASS

- [ ] **Step 3: Verify obs stack starts**

Run: `COMPOSE_PROFILES=observability docker compose up -d`
Wait: all services healthy
Check: `curl -s http://localhost:3001/api/health` returns `{"database":"ok"}`
Check: `curl -s http://localhost:8888` returns Dozzle UI HTML

Run: `docker compose down`

- [ ] **Step 4: Commit**

```text
feat(web): rebuild embedded dist with OTel instrumentation
```

---

## Post-Implementation Checklist

- [ ] All tests pass: `task test && task lint && task test:int && task test:e2e`
- [ ] OTel SDK versions aligned (v1.42.0 for all `go.opentelemetry.io/otel/*`)
- [ ] `--log-level debug` produces debug output, `--log-level error` suppresses info
- [ ] `task dev:obs` starts full stack without errors
- [ ] Grafana shows Prometheus and Tempo datasources as healthy
- [ ] A command via web UI produces a trace visible in Grafana → Explore → Tempo
- [ ] `task dev` (without obs profile) starts normally, no OTel error spam in logs
- [ ] License headers on all new files
