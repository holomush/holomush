<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# OTel-Native Application-Log Surfacing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Surface `log/slog` application logs into the OpenTelemetry log pipeline so they reach Sentry (Logs) and the OTel collector (→ Loki/Grafana), with trace correlation, while app code stays vendor-neutral (binds to OTel only).

**Architecture:** A `FanoutHandler` tees every slog record to the existing stderr `traceHandler` and to an `otelslog` bridge feeding one `sdk/log.LoggerProvider`. The provider holds up to two batch-processor pipelines (collector via `otlploggrpc`, Sentry via `otlploghttp`), each wrapped in a per-sink `LevelFilter` processor. Sinks are independently toggled by koanf config; endpoints/secrets stay env-driven. Mirrors the shipped trace dual-export (`holomush-0wun`).

**Tech Stack:** Go 1.26, `log/slog`, `go.opentelemetry.io/otel/{log,sdk/log}` (v0.x experimental), `exporters/otlp/otlplog/{otlploghttp,otlploggrpc}`, `contrib/bridges/otelslog`, koanf, cobra, samber/oops, testify.

> **v0.x API note (Rule 7 degraded-mode):** The OTel log SDK, OTLP log exporters, and `otelslog` bridge are experimental `v0.x` modules. context7 was consulted and returned design-level docs but not exact symbol signatures. **Task 1 pins exact versions and confirms the live API surface by compiling a smoke test.** If any later task's code block disagrees with the pinned version's signatures (e.g., a `Processor` method name or an exporter option), treat Task 1's confirmed surface as authoritative and adjust the call — the TDD compile step in each task surfaces drift immediately.

**Spec:** `docs/superpowers/specs/2026-05-23-otel-native-log-surfacing-design.md`
**Design bead:** `holomush-zgtqo`

---

## Phase 1: Dependencies & log exporters

### Task 1: Add OTel log modules and confirm the API surface

**Files:**

- Modify: `go.mod`
- Test: `internal/telemetry/logsmoke_test.go`

- [ ] **Step 1: Add the dependencies**

Run (from repo root):

```bash
go get go.opentelemetry.io/otel/log@latest \
       go.opentelemetry.io/otel/sdk/log@latest \
       go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp@latest \
       go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc@latest \
       go.opentelemetry.io/contrib/bridges/otelslog@latest
```

Then record the resolved versions: `go list -m go.opentelemetry.io/otel/sdk/log go.opentelemetry.io/contrib/bridges/otelslog`. These are the authoritative signatures for all later tasks.

- [ ] **Step 2: Write a smoke test that exercises the real API surface**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/contrib/bridges/otelslog"
)

// TestLogSDKSurface pins the experimental v0.x API shapes this plan relies
// on. If an OTel log-module bump changes any of these, this test breaks
// first and the implementer reconciles the rest of the package against it.
func TestLogSDKSurface(t *testing.T) {
	lp := sdklog.NewLoggerProvider() // no processors → no-op
	t.Cleanup(func() { _ = lp.Shutdown(context.Background()) })

	// otelslog bridge produces a slog.Handler bound to a provider.
	h := otelslog.NewHandler("holomush-test", otelslog.WithLoggerProvider(lp))
	require.NotNil(t, h)

	// Severity type used by the LevelFilter (Task 3).
	require.Equal(t, otellog.SeverityWarn, otellog.SeverityWarn)
}
```

- [ ] **Step 3: Run the smoke test**

Run: `task test -- -run TestLogSDKSurface ./internal/telemetry/`
Expected: PASS. If it fails to compile, the resolved API differs from this plan — fix the symbol names here and propagate to later tasks before continuing.

- [ ] **Step 4: Commit**

Commit per `references/vcs-preamble.md`: `feat(telemetry): add OTel log SDK deps + API smoke test`.

### Task 2: Sentry logs-endpoint derivation + log exporters

**Files:**

- Create: `internal/telemetry/logexport.go`
- Test: `internal/telemetry/logexport_test.go`

- [ ] **Step 1: Write the failing test for DSN→endpoint derivation**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telemetry

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSentryLogsTarget(t *testing.T) {
	dsn := "https://abc123@o4509.ingest.us.sentry.io/4510"
	url, header, err := sentryLogsTarget(dsn)
	require.NoError(t, err)
	require.Equal(t, "https://o4509.ingest.us.sentry.io/api/4510/integration/otlp/v1/logs", url)
	require.Equal(t, "sentry sentry_key=abc123", header)
}

func TestSentryLogsTarget_Invalid(t *testing.T) {
	_, _, err := sentryLogsTarget("not a dsn")
	require.Error(t, err)
}
```

- [ ] **Step 2: Run it to confirm it fails**

Run: `task test -- -run TestSentryLogsTarget ./internal/telemetry/`
Expected: FAIL — `undefined: sentryLogsTarget`.

- [ ] **Step 3: Implement the derivation + exporter constructors**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telemetry

import (
	"context"
	"fmt"
	"os"

	"github.com/getsentry/sentry-go"
	"github.com/samber/oops"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// sentryLogsTarget derives Sentry's OTLP logs endpoint URL and the
// x-sentry-auth header value from a DSN. DSN parsing stays inside
// internal/telemetry (the only package permitted to import sentry-go);
// internal/logging never imports sentry-go (INV-L1).
func sentryLogsTarget(dsn string) (url, authHeader string, err error) {
	d, perr := sentry.NewDsn(dsn)
	if perr != nil {
		return "", "", oops.Code("SENTRY_DSN_INVALID").Wrap(perr)
	}
	host := d.GetHost()
	projectID := d.GetProjectID()
	publicKey := d.GetPublicKey()
	url = fmt.Sprintf("https://%s/api/%s/integration/otlp/v1/logs", host, projectID)
	authHeader = fmt.Sprintf("sentry sentry_key=%s", publicKey)
	return url, authHeader, nil
}

// newCollectorLogExporter builds the OTLP-gRPC log exporter targeting the
// shared collector endpoint (OTEL_EXPORTER_OTLP_ENDPOINT, env-driven).
func newCollectorLogExporter(ctx context.Context) (sdklog.Exporter, error) {
	exp, err := otlploggrpc.New(ctx)
	if err != nil {
		return nil, oops.Code("OTEL_LOG_EXPORTER_FAILED").Wrap(err)
	}
	return exp, nil
}

// newSentryLogExporter builds the OTLP-HTTP log exporter targeting Sentry.
// It reuses the OTEL_EXPORTER_OTLP_ENDPOINT unset guard from initSentry so
// the otlploghttp transport stays on HTTPS (INV-L8): the SDK forces
// Insecure=true when that env var carries an http:// scheme.
func newSentryLogExporter(ctx context.Context, dsn string) (sdklog.Exporter, error) {
	url, authHeader, err := sentryLogsTarget(dsn)
	if err != nil {
		return nil, err
	}
	prev, had := os.LookupEnv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if had {
		if uerr := os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT"); uerr != nil {
			return nil, oops.Wrap(uerr)
		}
		defer func() { _ = os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", prev) }()
	}
	exp, err := otlploghttp.New(ctx,
		otlploghttp.WithEndpointURL(url),
		otlploghttp.WithHeaders(map[string]string{"x-sentry-auth": authHeader}),
		otlploghttp.WithCompression(otlploghttp.GzipCompression),
	)
	if err != nil {
		return nil, oops.Code("SENTRY_LOG_EXPORTER_FAILED").Wrap(err)
	}
	return exp, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `task test -- -run TestSentryLogsTarget ./internal/telemetry/`
Expected: PASS. (Confirm `sentry.NewDsn` accessor names — `GetHost`/`GetProjectID`/`GetPublicKey` — against the pinned sentry-go v0.46.2; adjust if the smoke test in Task 1 revealed different names.)

- [ ] **Step 5: Commit**

`feat(telemetry): Sentry OTLP-logs endpoint derivation + log exporters`.

---

## Phase 2: Per-sink level filter

### Task 3: `LevelFilter` log processor

**Files:**

- Create: `internal/telemetry/logfilter.go`
- Test: `internal/telemetry/logfilter_test.go`

- [ ] **Step 1: Write the failing test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// recordingProcessor captures emitted records for assertions.
type recordingProcessor struct{ severities []otellog.Severity }

func (r *recordingProcessor) OnEmit(_ context.Context, rec *sdklog.Record) error {
	r.severities = append(r.severities, rec.Severity())
	return nil
}
func (r *recordingProcessor) Shutdown(context.Context) error   { return nil }
func (r *recordingProcessor) ForceFlush(context.Context) error { return nil }

func TestLevelFilter_DropsBelowThreshold(t *testing.T) {
	sink := &recordingProcessor{}
	f := newLevelFilter(sink, otellog.SeverityWarn)

	for _, sev := range []otellog.Severity{
		otellog.SeverityInfo, otellog.SeverityWarn, otellog.SeverityError,
	} {
		var rec sdklog.Record
		rec.SetSeverity(sev)
		require.NoError(t, f.OnEmit(context.Background(), &rec))
	}

	require.Equal(t, []otellog.Severity{otellog.SeverityWarn, otellog.SeverityError}, sink.severities)
}
```

- [ ] **Step 2: Run it to confirm it fails**

Run: `task test -- -run TestLevelFilter ./internal/telemetry/`
Expected: FAIL — `undefined: newLevelFilter`.

- [ ] **Step 3: Implement the filter**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telemetry

import (
	"context"

	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// levelFilter is a log.Processor that forwards only records at or above a
// minimum severity to the wrapped downstream processor. It is the mechanism
// that lets a single LoggerProvider give the collector and Sentry sinks
// independent severity floors (spec INV-L4).
type levelFilter struct {
	next sdklog.Processor
	min  otellog.Severity
}

func newLevelFilter(next sdklog.Processor, min otellog.Severity) *levelFilter {
	return &levelFilter{next: next, min: min}
}

func (f *levelFilter) OnEmit(ctx context.Context, rec *sdklog.Record) error {
	if rec.Severity() < f.min {
		return nil
	}
	return f.next.OnEmit(ctx, rec)
}

func (f *levelFilter) Shutdown(ctx context.Context) error   { return f.next.Shutdown(ctx) }
func (f *levelFilter) ForceFlush(ctx context.Context) error { return f.next.ForceFlush(ctx) }
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `task test -- -run TestLevelFilter ./internal/telemetry/`
Expected: PASS. If `sdklog.Processor` declares additional methods in the pinned version (e.g., an `Enabled`), add delegating implementations — Task 1's smoke confirms the interface.

- [ ] **Step 5: Commit**

`feat(telemetry): per-sink level filter log processor`.

---

## Phase 3: slog fanout + bridge wiring

### Task 4: `FanoutHandler` and `levelGate`

**Files:**

- Modify: `internal/logging/handler.go`
- Test: `internal/logging/fanout_test.go`

- [ ] **Step 1: Write the failing test**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFanout_TeesToAllChildren(t *testing.T) {
	var a, b bytes.Buffer
	h := NewFanout(
		slog.NewJSONHandler(&a, &slog.HandlerOptions{Level: slog.LevelInfo}),
		slog.NewJSONHandler(&b, &slog.HandlerOptions{Level: slog.LevelInfo}),
	)
	logger := slog.New(h)
	logger.Info("hello")

	require.Contains(t, a.String(), "hello")
	require.Contains(t, b.String(), "hello")
}

func TestLevelGate_FiltersBelowMin(t *testing.T) {
	var buf bytes.Buffer
	gated := NewLevelGate(slog.LevelWarn, slog.NewJSONHandler(&buf, nil))
	logger := slog.New(gated)
	logger.Info("dropped")
	logger.Warn("kept")

	out := buf.String()
	require.False(t, strings.Contains(out, "dropped"))
	require.True(t, strings.Contains(out, "kept"))
}

func TestFanout_SingleChildIsTransparent(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, nil)
	h := NewFanout(base) // single child
	require.True(t, h.Enabled(context.Background(), slog.LevelError))
}
```

- [ ] **Step 2: Run it to confirm it fails**

Run: `task test -- -run 'TestFanout|TestLevelGate' ./internal/logging/`
Expected: FAIL — `undefined: NewFanout`, `NewLevelGate`.

- [ ] **Step 3: Implement Fanout + LevelGate**

Append to `internal/logging/handler.go`:

```go
// fanoutHandler dispatches each record to every child handler. Enabled is
// the OR of children, so a record is processed if any sink wants it; each
// child then applies its own level/filtering. Used to tee stderr logging
// and the OTel bridge from one slog.Logger (spec §3, INV-L2).
type fanoutHandler struct{ children []slog.Handler }

// NewFanout returns a handler that tees to all children. With a single
// child it is transparent (degenerate case, spec §11 / INV-L7): no panic,
// behaviour identical to using that child directly.
func NewFanout(children ...slog.Handler) slog.Handler {
	return &fanoutHandler{children: children}
}

func (h *fanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, c := range h.children {
		if c.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h *fanoutHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, c := range h.children {
		if !c.Enabled(ctx, r.Level) {
			continue
		}
		if err := c.Handle(ctx, r.Clone()); err != nil {
			return err //nolint:wrapcheck // slog.Handler contract: pass child error through
		}
	}
	return nil
}

func (h *fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, len(h.children))
	for i, c := range h.children {
		next[i] = c.WithAttrs(attrs)
	}
	return &fanoutHandler{children: next}
}

func (h *fanoutHandler) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, len(h.children))
	for i, c := range h.children {
		next[i] = c.WithGroup(name)
	}
	return &fanoutHandler{children: next}
}

// levelGate wraps a handler with a minimum level, giving a per-sink floor
// (spec INV-L4) independent of the global logger level.
type levelGate struct {
	min     slog.Level
	handler slog.Handler
}

func NewLevelGate(min slog.Level, h slog.Handler) slog.Handler {
	return &levelGate{min: min, handler: h}
}

func (g *levelGate) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= g.min && g.handler.Enabled(ctx, level)
}

func (g *levelGate) Handle(ctx context.Context, r slog.Record) error {
	//nolint:wrapcheck // slog.Handler contract: pass child error through
	return g.handler.Handle(ctx, r)
}

func (g *levelGate) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &levelGate{min: g.min, handler: g.handler.WithAttrs(attrs)}
}

func (g *levelGate) WithGroup(name string) slog.Handler {
	return &levelGate{min: g.min, handler: g.handler.WithGroup(name)}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `task test -- -run 'TestFanout|TestLevelGate' ./internal/logging/`
Expected: PASS.

- [ ] **Step 5: Commit**

`feat(logging): fanout handler + per-sink level gate`.

### Task 5: Extend `logging.Setup` to attach an OTel bridge

**Files:**

- Modify: `internal/logging/handler.go:70-97`
- Test: `internal/logging/handler_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestSetupWithBridge_TeesToBridge(t *testing.T) {
	var stderr bytes.Buffer
	var bridged []string
	bridge := captureHandler{onHandle: func(r slog.Record) { bridged = append(bridged, r.Message) }}

	logger := SetupWithBridge("svc", "v1", "json", &stderr, slog.LevelInfo, bridge, slog.LevelInfo)
	logger.Info("both")

	require.Contains(t, stderr.String(), "both")
	require.Equal(t, []string{"both"}, bridged)
}
```

(Define `captureHandler` as a minimal `slog.Handler` test double in the test file: `Enabled` returns true, `Handle` calls `onHandle`, `WithAttrs`/`WithGroup` return self.)

- [ ] **Step 2: Run it to confirm it fails**

Run: `task test -- -run TestSetupWithBridge ./internal/logging/`
Expected: FAIL — `undefined: SetupWithBridge`.

- [ ] **Step 3: Implement SetupWithBridge and refactor Setup to call it**

Replace the body of `Setup` (`handler.go:70-91`) so the existing function delegates, and add the bridge-aware variant:

```go
// Setup creates a stderr-only logger (unchanged public behaviour).
func Setup(service, version, format string, w io.Writer, level slog.Level) *slog.Logger {
	return SetupWithBridge(service, version, format, w, level, nil, level)
}

// SetupWithBridge creates a logger that tees stderr (gated at stderrLevel)
// and, when bridge != nil, an OTel bridge handler (gated at bridgeLevel).
// When bridge is nil the result is the stderr-only logger (INV-L7).
func SetupWithBridge(
	service, version, format string, w io.Writer, stderrLevel slog.Level,
	bridge slog.Handler, bridgeLevel slog.Level,
) *slog.Logger {
	if w == nil {
		w = os.Stderr
	}
	var base slog.Handler
	opts := &slog.HandlerOptions{Level: stderrLevel}
	if format == "text" {
		base = slog.NewTextHandler(w, opts)
	} else {
		base = slog.NewJSONHandler(w, opts)
	}
	stderrHandler := &traceHandler{handler: base, service: service, version: version}

	if bridge == nil {
		return slog.New(stderrHandler)
	}
	gatedBridge := NewLevelGate(bridgeLevel, bridge)
	return slog.New(NewFanout(stderrHandler, gatedBridge))
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `task test -- ./internal/logging/`
Expected: PASS (existing `handler_test.go` tests still green — `Setup` behaviour unchanged).

- [ ] **Step 5: Commit**

`feat(logging): SetupWithBridge to tee stderr + OTel bridge`.

---

## Phase 4: Logging config

### Task 6: `LoggingConfig` koanf struct

**Files:**

- Modify: `internal/config/config.go`
- Test: `internal/config/logging_config_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestLoggingConfig_Defaults(t *testing.T) {
	c := DefaultLoggingConfig()
	require.True(t, c.Stderr.Enabled)
	require.True(t, c.OTel.Enabled)
	require.True(t, c.Sentry.Enabled)
}

func TestLoggingSink_EffectiveLevel(t *testing.T) {
	global := slog.LevelInfo
	require.Equal(t, slog.LevelWarn, LoggingSink{Level: "warn"}.EffectiveLevel(global))
	require.Equal(t, global, LoggingSink{}.EffectiveLevel(global)) // unset → global
}
```

- [ ] **Step 2: Run it to confirm it fails**

Run: `task test -- -run 'TestLoggingConfig|TestLoggingSink' ./internal/config/`
Expected: FAIL — `undefined: DefaultLoggingConfig`.

- [ ] **Step 3: Implement the struct**

Add to `internal/config/config.go`:

```go
// LoggingSink configures one log destination. Level is a slog level name
// ("debug"|"info"|"warn"|"error"); empty inherits the global level.
type LoggingSink struct {
	Enabled bool   `koanf:"enabled"`
	Level   string `koanf:"level"`
}

// EffectiveLevel returns the sink's level, falling back to global when the
// per-sink level is unset or unparseable (spec INV-L4).
func (s LoggingSink) EffectiveLevel(global slog.Level) slog.Level {
	if s.Level == "" {
		return global
	}
	var l slog.Level
	if err := l.UnmarshalText([]byte(s.Level)); err != nil {
		return global
	}
	return l
}

// LoggingConfig configures the three log sinks. Endpoints/secrets remain
// env-driven (SENTRY_DSN, OTEL_EXPORTER_OTLP_ENDPOINT); these toggles gate
// behaviour. Effective enablement of a non-stderr sink = Enabled AND its
// endpoint present (spec INV-L3), enforced at telemetry.Init.
type LoggingConfig struct {
	Stderr LoggingSink `koanf:"stderr"`
	OTel   LoggingSink `koanf:"otel"`
	Sentry LoggingSink `koanf:"sentry"`
}

// DefaultLoggingConfig enables all three sinks with global-inherited levels.
func DefaultLoggingConfig() LoggingConfig {
	return LoggingConfig{
		Stderr: LoggingSink{Enabled: true},
		OTel:   LoggingSink{Enabled: true},
		Sentry: LoggingSink{Enabled: true},
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `task test -- -run 'TestLoggingConfig|TestLoggingSink' ./internal/config/`
Expected: PASS.

- [ ] **Step 5: Commit**

`feat(config): LoggingConfig with per-sink toggles and levels`.

---

## Phase 5: telemetry.Init wiring + CLI

### Task 7: Build the LoggerProvider in `telemetry.Init`

**Files:**

- Modify: `internal/telemetry/provider.go`
- Test: `internal/telemetry/provider_log_test.go`

- [ ] **Step 1: Write the failing test**

Use `package telemetry_test` (external) to match the existing
`provider_test.go`, and write the test against the **target** 5-arg
signature so no rewrite is needed after Step 3:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telemetry_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/config"
	"github.com/holomush/holomush/internal/telemetry"
)

func TestINV_L7_NoLogSinks_NilHandler(t *testing.T) {
	// No OTEL endpoint, no SENTRY_DSN, default config → LogHandler nil (INV-L7).
	res, err := telemetry.Init(context.Background(), "svc", "v1",
		config.DefaultLoggingConfig(), slog.LevelInfo)
	require.NoError(t, err)
	require.Nil(t, res.LogHandler)
	require.NoError(t, res.Shutdown(context.Background()))
}
```

- [ ] **Step 2: Run it to confirm it fails**

Run: `task test -- -run TestINV_L7 ./internal/telemetry/`
Expected: FAIL to compile — current `Init` is 3-arg and returns
`(func(context.Context) error, error)`, not `(Result, error)`. This is the
expected red state; Step 3 makes it pass.

- [ ] **Step 3: Change the signature to return `Result` and build the LoggerProvider**

Add the `Result` type and a `buildLogProcessors` helper, and rewrite `Init`'s tail. Key changes to `provider.go`:

```go
// Result carries the shutdown closure and the optional OTel log bridge
// handler back to the caller (spec §7). LogHandler is nil when no OTel log
// sink is enabled, in which case the caller keeps the stderr-only logger.
type Result struct {
	Shutdown   func(context.Context) error
	LogHandler slog.Handler
}

// buildLogProcessors returns the gated processors for the enabled OTel log
// sinks. Returns nil when none are enabled. global is the global slog level
// floor used to translate per-sink levels.
func buildLogProcessors(
	ctx context.Context, cfg config.LoggingConfig, global slog.Level,
	endpoint, sentryDSN string, sentryEnabled bool,
) []sdklog.Processor {
	var procs []sdklog.Processor
	if endpoint != "" && cfg.OTel.Enabled {
		if exp, err := newCollectorLogExporter(ctx); err != nil {
			errutil.LogError(slog.Default(), "collector log exporter init failed; skipping", err)
		} else {
			procs = append(procs, newLevelFilter(
				sdklog.NewBatchProcessor(exp), slogToOTel(cfg.OTel.EffectiveLevel(global))))
		}
	}
	if sentryEnabled && cfg.Sentry.Enabled {
		if exp, err := newSentryLogExporter(ctx, sentryDSN); err != nil {
			errutil.LogError(slog.Default(), "sentry log exporter init failed; skipping", err)
		} else {
			procs = append(procs, newLevelFilter(
				sdklog.NewBatchProcessor(exp), slogToOTel(cfg.Sentry.EffectiveLevel(global))))
		}
	}
	return procs
}
```

Then, after the `TracerProvider` is built and before `return`, construct the `LoggerProvider` from those processors, register it globally via `global.SetLoggerProvider(lp)` (`go.opentelemetry.io/otel/log/global`), build the bridge handler with `otelslog.NewHandler(serviceName, otelslog.WithLoggerProvider(lp))`, and return it on `Result.LogHandler`. Update the shutdown closure to the spec §8 order: `lp.Shutdown` → `tp.Shutdown` → `sentry.Flush`. Update the no-op fast-path (`provider.go:54`) and the doc-comment (`provider.go:33-47`, including the stale "Flush Sentry's own buffers … first" line) to describe `Result`.

`Init` must also accept the `config.LoggingConfig` and global level — change the signature to `Init(ctx, serviceName, serviceVersion string, logCfg config.LoggingConfig, global slog.Level) (Result, error)`. Add a `slogToOTel(slog.Level) otellog.Severity` helper (Debug→SeverityDebug, Info→SeverityInfo, Warn→SeverityWarn, Error→SeverityError).

- [ ] **Step 4: Run the test to verify it passes**

Run: `task test -- ./internal/telemetry/`
Expected: PASS. As part of Step 3, also update the **existing**
`provider_test.go` `Init` call sites (`:20,:28`) to the 5-arg form
(`config.DefaultLoggingConfig(), slog.LevelInfo`) and switch their result
handling from the old `shutdown, err :=` to `res, err :=` /
`res.Shutdown(...)`.

- [ ] **Step 5: Commit**

`feat(telemetry): build LoggerProvider with gated log sinks; Init returns Result`.

### Task 8: Two-phase init + CLI flags in core/gateway

**Files:**

- Modify: `cmd/holomush/core.go:144-201`, `cmd/holomush/gateway.go:155-187`
- Test: `cmd/holomush/core_test.go`

- [ ] **Step 1: Write the failing test (flag presence)**

```go
func TestCoreCommand_LogSinkFlags(t *testing.T) {
	cmd := NewCoreCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--help"})
	require.NoError(t, cmd.Execute())
	for _, f := range []string{"--log-sentry", "--log-sentry-level", "--log-otel", "--log-otel-level", "--log-stderr-level"} {
		require.Contains(t, buf.String(), f)
	}
}
```

- [ ] **Step 2: Run it to confirm it fails**

Run: `task test -- -run TestCoreCommand_LogSinkFlags ./cmd/holomush/`
Expected: FAIL — flags absent from help.

- [ ] **Step 3: Register flags, load the `logging` config section, and re-seat the logger**

In `NewCoreCmd` flag registration (`core.go:144-157`) add:

```go
cmd.Flags().Bool("log-stderr", true, "enable stderr log sink")
cmd.Flags().String("log-stderr-level", "", "stderr log level override (default: global)")
cmd.Flags().Bool("log-otel", true, "enable OTLP-collector log sink")
cmd.Flags().String("log-otel-level", "", "collector log level override (default: global)")
cmd.Flags().Bool("log-sentry", true, "enable Sentry log sink")
cmd.Flags().String("log-sentry-level", "", "Sentry log level override (default: global)")
```

In the RunE config-load block (`core.go:120-140`), add `var logConfig = config.DefaultLoggingConfig()` and `config.Load(configFile, cmd, &logConfig, "logging")`, then pass `logConfig` to `runCoreWithDeps` at the call site (`core.go:140`). This requires:

- Adding a `logConfig config.LoggingConfig` parameter to the `runCoreWithDeps` signature (`core.go:167`) — append it after the existing config params.
- Updating the `runCoreWithDeps(...)` call in `RunE` (`core.go:140`) to pass `logConfig`.
- Updating any test that calls `runCoreWithDeps` directly to pass `config.DefaultLoggingConfig()`.

In `runCoreWithDeps` replace the `core.go:184-201` block with the two-phase sequence:

```go
// --- 1. Logging (phase 1: stderr-only) + telemetry ---
level, err := resolveLogLevel(cmd)
if err != nil {
	return err
}
stderrLevel := logConfig.Stderr.EffectiveLevel(level)
logging.SetDefaultWithBridge("holomush-core", version, cfg.LogFormat, stderrLevel, nil, level)

res, telErr := telemetry.Init(ctx, "holomush-core", version, logConfig, level)
if telErr != nil {
	return oops.Code("TELEMETRY_INIT_FAILED").Wrap(telErr)
}
// Phase 2: re-seat the default logger with the OTel bridge when present.
if res.LogHandler != nil && logConfig.Stderr.Enabled {
	logging.SetDefaultWithBridge("holomush-core", version, cfg.LogFormat, stderrLevel, res.LogHandler, level)
}
defer func() {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if shutdownErr := res.Shutdown(shutdownCtx); shutdownErr != nil {
		slog.Warn("telemetry shutdown error", "error", shutdownErr)
	}
}()
```

Add `logging.SetDefaultWithBridge` to `internal/logging/handler.go` mirroring `SetDefault` but delegating to `SetupWithBridge`. Apply the identical change to `gateway.go:171-187` with `"holomush-gateway"`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `task test -- ./cmd/holomush/ ./internal/logging/`
Expected: PASS.

- [ ] **Step 5: Commit**

`feat(cmd): two-phase logger init + per-sink log flags`.

---

## Phase 6: Collector pipeline, invariants, docs

### Task 9: Add a logs pipeline to the dev collector

**Files:**

- Modify: `docker/otel-collector/config.yaml`

- [ ] **Step 1: Add a logs-capable exporter and pipeline**

The dev collector (`docker/otel-collector/config.yaml`) currently has only
`otlp/jaeger` (traces-only — Jaeger does not ingest logs) and a single
`traces` pipeline. Add a `debug` exporter (logs render to the collector's
own stdout, visible via `docker logs` / Dozzle) and a `logs` pipeline.
Under `exporters:` add:

```yaml
  debug:
    verbosity: detailed
```

Under `service.pipelines:` add, beside the existing `traces` block:

```yaml
    logs:
      receivers: [otlp]
      processors: [batch]
      exporters: [debug]
```

(Dev only — a production deployment routes logs to a real backend; the
production collector config in the operator-deployment plan already
defines a `logs` pipeline.)

- [ ] **Step 2: Validate the collector config**

Run: `docker compose --profile obs config` (or the project's compose-validate task) and confirm no schema error.
Expected: config parses; collector starts and logs "Everything is ready".

- [ ] **Step 3: Commit**

`feat(obs): accept OTLP logs in the dev collector`.

### Task 10: Invariant guard + meta-test

**Files:**

- Test: `internal/logging/import_guard_test.go`
- Test: `internal/telemetry/invariants_test.go`

- [ ] **Step 1: Write INV-L1 import-guard test**

```go
// In internal/logging/import_guard_test.go — INV-L1: logging never imports sentry-go.
func TestINV_L1_NoSentryImport(t *testing.T) {
	pkgs, err := packages.Load(&packages.Config{Mode: packages.NeedImports | packages.NeedDeps},
		"github.com/holomush/holomush/internal/logging")
	require.NoError(t, err)
	for _, p := range pkgs {
		for imp := range p.Imports {
			require.NotContains(t, imp, "getsentry/sentry-go",
				"internal/logging must not import sentry-go (INV-L1)")
		}
	}
}
```

- [ ] **Step 2: Write the meta-test enumerating invariants**

```go
// internal/telemetry/invariants_test.go — INV-META: every INV-L* is referenced by a test.
func TestINV_Meta_AllInvariantsReferenced(t *testing.T) {
	want := []string{"INV_L1", "INV_L2", "INV_L3", "INV_L4", "INV_L5", "INV_L6", "INV_L7", "INV_L8"}
	// grep the test sources under internal/logging + internal/telemetry for each ID;
	// fail if any ID has zero references. (Use os.ReadDir + os.ReadFile + strings.Contains.)
	for _, id := range want {
		require.True(t, invariantReferenced(id), "no test references %s", id)
	}
}
```

(Implement `invariantReferenced` to walk `*_test.go` in both packages and match the ID; map remaining INV-L2/L3/L5/L6/L8 onto the tests written in Tasks 3–8, renaming test funcs to embed the IDs, e.g. `TestINV_L4_PerSinkLevel`.)

- [ ] **Step 3: Run the invariant tests**

Run: `task test -- -run 'TestINV' ./internal/logging/ ./internal/telemetry/`
Expected: PASS. Add `golang.org/x/tools/go/packages` to go.mod if not present (it is, indirectly).

- [ ] **Step 4: Commit**

`test(telemetry): INV-L1 import guard + invariant meta-test`.

### Task 11: Operator docs

**Files:**

- Modify: `site/docs/operating/sentry.md:126-134,175-183`

- [ ] **Step 1: Rewrite the "Application logs" row and follow-ups**

Replace the "Application logs" row (`sentry.md:132`) so it reads that Go logs flow OTel-natively via the `LoggerProvider` → `otlploghttp` to Sentry's OTLP logs endpoint (no `sentry.Logger` calls), gated by `logging.sentry.enabled`. Add a short "Application logs (OTel-native)" subsection documenting the three sinks, the `logging.*` config keys + CLI flags from Task 8, and that ERROR logs appear in Sentry **Logs** (grouped Issues remain a separate follow-up). Update the "Open follow-ups" list (`sentry.md:175-183`): the slog-bridge follow-up is now **done**; the `CaptureException`→Issues item moves behind a `pkg/errutil` wrapper.

- [ ] **Step 2: Verify docs lint**

Run: `task fmt && task lint:docs-symmetry`
Expected: no diff after fmt; symmetry check passes.

- [ ] **Step 3: Commit**

`docs(operating): OTel-native application-log surfacing`.

---

## Verification (whole-plan)

- [ ] `task test` — all unit tests green.
- [ ] `task lint` — clean (line-scoped `//nolint:wrapcheck` only on the handler pass-throughs shown above).
- [ ] `task test:int` — integration build compiles (shared-type refactor safety).
- [ ] `task pr-prep` — full lane green before any push.
- [ ] Manual smoke: run `holomush core` with a real `SENTRY_DSN` + `OTEL_EXPORTER_OTLP_ENDPOINT`, emit a `slog.WarnContext` inside a span, and confirm the log line appears in Sentry Logs correlated to the trace, and in the collector pipeline.

## Carried review findings (from spec round-1/2)

- Update the `telemetry.Init` doc-comment block (`provider.go:33-47`) when the signature changes — covered in Task 7 Step 3.
- `FanoutHandler` single-child transparency is implemented (Task 4), not just advisory.
<!-- adr-capture: sha256=31a7856e8ec29609 adrs=holomush-ci829,holomush-stow5,holomush-1wbzn -->
