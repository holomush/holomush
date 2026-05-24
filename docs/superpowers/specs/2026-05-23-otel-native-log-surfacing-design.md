<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# OTel-Native Application-Log Surfacing — Design

**Design bead:** `holomush-zgtqo`
**Status:** Draft (pending design-reviewer)
**Date:** 2026-05-23

## 1. Scope

### Goals

- Surface HoloMUSH's `log/slog` application logs into the OpenTelemetry
  log pipeline so they reach **Sentry** (Logs product) and the existing
  **OTel collector** (→ Grafana / Loki), with trace correlation.
- Mirror the trace dual-export pattern already shipped (`holomush-0wun`):
  one provider, multiple gated exporters, Sentry as a swappable OTLP
  consumer.
- Keep application code **vendor-neutral**: app code binds to
  OpenTelemetry only. Sentry remains a config-only OTLP consumer — no
  `sentryslog` / `sentry.NewLogger` in app or logging code.
- Three independently toggleable log sinks (stderr, collector, Sentry),
  each with an optional per-sink severity floor.

### Non-Goals (out of scope)

- **Error events as Sentry Issues** (grouped, alertable, stacktraces).
  That requires `sentry.CaptureException` and is deferred to a separate
  bead, to be implemented behind a `pkg/errutil` wrapper so the Sentry
  SDK touchpoint stays in exactly one package. ERROR-level records still
  appear in the Sentry **Logs** stream via this work; they just do not
  become grouped Issues.
- **Metrics** — Sentry does not ingest OTLP metrics; no OTel metrics
  pipeline exists today.
- **Browser-side OTLP logs** — already covered by the browser SDK's
  `consoleLoggingIntegration` (`web/src/lib/sentry.ts`).
- **The bare→`*Context` log-call migration** — codified as a MUST in
  `CLAUDE.md` / `.claude/rules/logging.md` and enforced mechanically by
  `sloglint` under bead `holomush-xrdjw`.

## 2. Background — current state (grounded)

- Logging: `internal/logging/handler.go` — `Setup`/`SetDefault` build a
  `slog.Logger` wrapping a JSON/text base handler in a `traceHandler`
  that injects `service`, `version`, and (when on the ctx) `trace_id` /
  `span_id` (`handler.go:24-65,70-91`). Logs go **only to stderr** today;
  no OTel `LoggerProvider` exists.
- Telemetry: `internal/telemetry/provider.go::Init` builds a
  `TracerProvider`, conditionally appends an OTLP-gRPC collector batcher
  (`provider.go:85-91`) and, when `SENTRY_DSN` is set, a Sentry OTLP-HTTP
  trace batcher (`provider.go:99-108`), then registers globals and
  returns a shutdown closure (`provider.go:125-132`).
- Sentry: `internal/telemetry/sentry.go::initSentry` calls `sentry.Init`
  with `EnableLogs: true` (`sentry.go:74-82`) — but **nothing emits Sentry
  logs**; `EnableLogs` is a dormant kill-switch. It builds the trace
  exporter via `sentryotlp.NewTraceExporter` (`sentry.go:111-113`), with
  an `OTEL_EXPORTER_OTLP_ENDPOINT` unset-during-construction guard to keep
  the Sentry POSTs on HTTPS (`sentry.go:86-106`).
- Config: observability secrets/endpoints are env-only (`SENTRY_DSN`,
  `OTEL_EXPORTER_OTLP_ENDPOINT`, `SENTRY_*`); there is **no** `SentryConfig`
  struct. Log *format* is a per-subcommand koanf field + CLI flag
  (`cmd/holomush/core.go:70,149`); log *level* is parsed in
  `cmd/holomush/root.go` from `LOG_LEVEL` / `--log-level`.
- Collector config is in-repo: `docker/otel-collector/config.yaml` (dev,
  traces-only today); the production collector plan already defines a
  `logs` pipeline.
- External facts: Sentry ingests OTLP **traces + logs** (not metrics).
  Logs endpoint: `https://<host>/api/<project>/integration/otlp/v1/logs`,
  auth header `x-sentry-auth: sentry sentry_key=<public-key>`. sentry-go
  (`v0.46.2`) ships **no** OTLP log exporter — only `NewTraceExporter` —
  so logs use the generic `otlploghttp`/`otlploggrpc` exporters.

## 3. Architecture

One `LoggerProvider` fed by a `log/slog`→OTel bridge, fanned out beside
the existing stderr handler. The collector/Sentry split happens inside
the provider via per-sink level-filtering processors:

```text
slog.Default()
  └─ FanoutHandler                          (Enabled = OR of children)
       ├─ traceHandler → JSON/text → stderr           [level: stderr]
       └─ otelslog.Handler → LoggerProvider           [floor: min(enabled OTel sinks)]
                               ├─ LevelFilter(collector) → BatchProcessor → otlploggrpc → collector → Loki/Grafana
                               └─ LevelFilter(sentry)    → BatchProcessor → otlploghttp → …/integration/otlp/v1/logs
```

Rationale for **one** `LoggerProvider` + `LevelFilter` (vs. one provider
per sink): maximal symmetry with the `TracerProvider`-with-two-batchers
shape, and a single bridge pass (the slog→OTel conversion runs once).
Per-sink levels — the one thing a single provider can't do natively — are
delivered by a ~20-line `sdklog.Processor` that drops records below a
threshold before its exporter's batch processor.

## 4. Components & files

| Action | Path | Responsibility |
| ------ | ---- | -------------- |
| Modify | `internal/telemetry/provider.go` | Build `LoggerProvider` with collector + Sentry log processors (gated like the trace batchers); return it + an `otelslog`-backed `slog.Handler`; add `lp.Shutdown` to the shutdown chain |
| New | `internal/telemetry/logexport.go` | Construct `otlploggrpc` (collector) and `otlploghttp` (Sentry) log exporters; derive the Sentry logs URL + `x-sentry-auth` header from the DSN; reuse the insecure-scheme unset guard |
| New | `internal/telemetry/logfilter.go` | `LevelFilter` — an `sdklog.Processor` that forwards only records ≥ a configured `log.Severity` |
| Modify | `internal/logging/handler.go` | Add `FanoutHandler` + `LevelGate`; extend `Setup` to accept an optional OTel bridge handler and per-sink levels; keep `traceHandler` as the stderr path |
| Modify | `cmd/holomush/core.go`, `cmd/holomush/gateway.go` | Two-phase init (§7); add `logging` koanf section + CLI flags (§6) |
| New | `internal/config/config.go` | `LoggingConfig` koanf struct for the per-sink toggles + levels, alongside the existing `GameConfig` / `AuthConfig` / `CryptoConfig` |
| Modify | `docker/otel-collector/config.yaml` | Add a `logs` pipeline (`receivers: [otlp]`, `exporters: [...]`) |
| Modify | `site/docs/operating/sentry.md` | Replace the "call `sentry.Logger.Info`" guidance with the OTel-native story; update the "Application logs" row |
| Modify | `go.mod` | Add `go.opentelemetry.io/otel/log`, `otel/sdk/log`, `exporters/otlp/otlplog/otlploghttp`, `exporters/otlp/otlplog/otlploggrpc`, `contrib/bridges/otelslog` |

(Already drafted in this branch, companion deliverables: `CLAUDE.md`
"Structured Logging" convention + `.claude/rules/logging.md`.)

## 5. Configuration

Two surfaces, by nature of the value:

**env (secrets / SDK integration / endpoints) — unchanged:**

| Var | Role |
| --- | ---- |
| `SENTRY_DSN` | Enables the Sentry sink (logs endpoint + auth derived from it) |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Enables the collector sink (shared with traces) |
| `SENTRY_ENVIRONMENT` / `SENTRY_RELEASE` | Resource tags (existing) |

**koanf config + CLI flags (logging behavior) — new, `logging` section:**

| Key (`koanf`) | CLI flag | Default | Meaning |
| ------------- | -------- | ------- | ------- |
| `logging.stderr.enabled` | `--log-stderr` | `true` | Toggle stderr sink |
| `logging.stderr.level` | `--log-stderr-level` | global | Per-sink floor |
| `logging.otel.enabled` | `--log-otel` | `true` | Toggle collector sink |
| `logging.otel.level` | `--log-otel-level` | global | Per-sink floor |
| `logging.sentry.enabled` | `--log-sentry` | `true` | Toggle Sentry sink |
| `logging.sentry.level` | `--log-sentry-level` | `warn` | Per-sink floor (cost control; Sentry Logs is volume-priced) |

- **Effective enablement** of a sink = `config.enabled` **AND** its
  endpoint is present (stderr has no endpoint, so it depends only on the
  toggle). This lets an operator with `SENTRY_DSN` set send traces to
  Sentry while keeping `logging.sentry.enabled: false`.
- **Level precedence** per sink: explicit per-sink level > global
  `LOG_LEVEL` / `--log-level`. The stderr and collector sinks default to the
  global level when unset; the Sentry sink defaults to `warn`
  (`config.SentryLogLevelDefault`) so info/debug never reach the volume-priced
  Sentry Logs view regardless of the global level.
- Flag/config/env precedence follows the existing `config.Load` order
  (CLI flag > config file > built-in default), matching `log_format`.

## 6. Sentry logs-endpoint derivation

Parse the DSN to build the logs URL and auth header. The DSN
(`https://<public-key>@<host>/<project-id>`) yields:

- URL: `https://<host>/api/<project-id>/integration/otlp/v1/logs`
- Header: `x-sentry-auth: sentry sentry_key=<public-key>`

DSN parsing MAY reuse the already-imported `sentry.Dsn` type **within
`internal/telemetry` only** (that package already depends on sentry-go for
traces). `internal/logging` MUST remain free of any sentry-go import. The
`otlploghttp` exporter MUST be constructed under the same
`OTEL_EXPORTER_OTLP_ENDPOINT` unset guard used for the trace exporter
(`sentry.go:86-106`) to avoid the insecure-scheme transport switch.

## 7. Init sequencing (two-phase)

Today `logging.SetDefault` runs before `telemetry.Init` so Init can log.
The bridge now originates *from* `telemetry.Init`, creating a cycle.
Resolution:

1. **Phase 1:** `logging.Setup` builds a stderr-only logger; set as
   default so `telemetry.Init` can log during startup.
2. **Phase 2:** `telemetry.Init` builds the providers including the
   `LoggerProvider`, and returns the `otelslog` bridge handler.
3. Re-seat the default logger via `logging.Setup` with the bridge
   attached and per-sink levels applied.

Init's own startup log lines land on stderr regardless, so nothing is
lost in the brief pre-bridge window.

### `telemetry.Init` return contract

The current signature is
`Init(ctx, serviceName, serviceVersion) (func(context.Context) error, error)`
(`internal/telemetry/provider.go:48`). To carry the bridge to the caller
without a positional-return sprawl, `Init` MUST return a small result
struct:

```go
type Result struct {
    // Shutdown flushes and stops all providers (logs, traces, Sentry).
    Shutdown func(context.Context) error
    // LogHandler is the otelslog-backed slog.Handler for the OTel log
    // pipeline. It is nil when no OTel log sink is enabled (INV-L7), in
    // which case the caller keeps the stderr-only logger from Phase 1.
    LogHandler slog.Handler
}

// Init also gains logCfg + global so it can build the gated log sinks and
// translate per-sink levels (§5). The collector/Sentry endpoints stay
// env-driven; logCfg only carries the toggles + per-sink level overrides.
func Init(
    ctx context.Context, serviceName, serviceVersion string,
    logCfg config.LoggingConfig, global slog.Level,
) (Result, error)
```

Callers MUST update accordingly: `cmd/holomush/core.go:191`,
`cmd/holomush/gateway.go:177`, and `internal/telemetry/provider_test.go`
(`:20,:28`). When `Result.LogHandler != nil`, the caller re-seats the
default logger (Phase 3) by composing it with the stderr handler via
`FanoutHandler`; when nil, no re-seat occurs.

## 8. Error handling & shutdown

- Log-exporter construction failure MUST NOT abort startup: log a warning
  and continue without that sink — the exact pattern at
  `provider.go:99-108` ("sentry init failed; continuing without Sentry").
- Shutdown order MUST be: `lp.Shutdown` (drains log batches incl. the
  Sentry OTLP-logs batch) → `tp.Shutdown` (spans) → `sentry.Flush` (SDK
  error buffers). This **changes** the current order
  (`provider.go:125-132` does `sentry.Flush` first): log batches must be
  flushed over their own HTTP/gRPC transports before those transports are
  torn down, and `sentry.Flush` now drains only the SDK's error buffers
  (logs no longer route through it). The existing "Flush Sentry's own
  buffers … first" comment MUST be updated, not preserved.
- When all OTel sinks are disabled, no `LoggerProvider` is created and no
  bridge is attached — behavior is identical to today (stderr only), zero
  added overhead.

## 9. Trace correlation & the context-logging rule

Trace context flows into OTel logs only from `slog.*Context` calls (the
bridge reads `trace_id`/`span_id` from the ctx). Bare `slog.Info(…)`
produce uncorrelated log lines. This is codified as a MUST in `CLAUDE.md`
("Structured Logging") and `.claude/rules/logging.md`, with mechanical
`sloglint` (`context: scope`) enforcement tracked under `holomush-xrdjw`.

## 10. PII consideration (follow-up, not in scope)

Application logs can contain player data. OTel-native redaction is a
`sdklog.Processor` that scrubs attributes before export. Flagged as a
follow-up; this design does not add redaction, and operators SHOULD set
conservative per-sink levels for the Sentry sink until it exists.

## 11. Testing strategy

- Unit (`internal/telemetry`): DSN→logs-endpoint derivation; `LevelFilter`
  forwards ≥ threshold and drops below; exporter-construction-failure path
  warns and continues; no-op when sinks disabled. Mirror
  `sentry_test.go`.
- Unit (`internal/logging`): `FanoutHandler.Enabled` is the OR of
  children; records reach both stderr and the bridge; `LevelGate` filters.
  When the OTel bridge child is `nil` (all OTel sinks disabled, INV-L7),
  `FanoutHandler` MUST degenerate to the stderr handler — `Enabled`
  delegates to the base handler, `Handle` writes only to stderr, and no
  panic occurs (the caller SHOULD simply not wrap in a `FanoutHandler`
  when there is a single child).
- Pipeline assertion: use `go.opentelemetry.io/otel/sdk/log/logtest` (or
  an in-memory exporter) to assert records emitted at the correct
  severities reach the provider.
- `task pr-prep` (full lane) green before push.

## 12. Invariants (RFC2119)

| # | Invariant | Test |
| - | --------- | ---- |
| INV-L1 | App code and `internal/logging` MUST NOT import sentry-go; the only sentry-go importer for logs is `internal/telemetry`, and only for DSN parsing + exporter wiring | Static: grep/`forbidigo`-style guard in test |
| INV-L2 | Each sink MUST be independently enable/disable-able; disabling one MUST NOT affect the others | Unit (toggle matrix) |
| INV-L3 | A sink's effective enablement MUST be `config.enabled AND endpoint-present` | Unit |
| INV-L4 | Per-sink level MUST override the global level for that sink only; unset per-sink level MUST inherit the global level | Unit |
| INV-L5 | Exporter construction failure MUST NOT abort process startup | Unit |
| INV-L6 | Shutdown MUST flush log batches before process exit (`lp.Shutdown` precedes return) | Unit |
| INV-L7 | With all OTel sinks disabled, no `LoggerProvider` is constructed (stderr-only parity with pre-change behavior) | Unit |
| INV-L8 | The Sentry logs `otlploghttp` exporter MUST target an `https://` endpoint regardless of `OTEL_EXPORTER_OTLP_ENDPOINT` scheme | Unit |
| INV-META | Every INV-L* above MUST have at least one referencing test | Meta-test enumerating invariant IDs |

## 13. Dependencies

- go.mod additions (§4). These are official OpenTelemetry modules but,
  unlike the `v1.43.0` trace/metric modules already in `go.mod`, the log
  SDK (`otel/sdk/log`), both OTLP log exporters, and the `otelslog`
  bridge are still **experimental `v0.x`** (no API-stability guarantee;
  minor versions may break). Accepting them is a deliberate trade-off —
  there is no `v1` log path yet. The plan MUST pin exact versions and
  treat OTel log-module bumps as review-gated.
- `docker/otel-collector/config.yaml` gains a `logs` pipeline so the
  collector accepts OTLP logs in the dev stack.
- Production collector config (deploy guide) already has a `logs`
  pipeline — verify it routes to the intended backend.

## 14. Documentation deliverables (PR-blocking)

- `site/docs/operating/sentry.md` — rewrite the "Application logs" row and
  add an OTel-native logs section (sinks, toggles, levels, endpoint).
- `CLAUDE.md` "Structured Logging" + `.claude/rules/logging.md` — already
  drafted in this branch.
<!-- adr-capture: sha256=137bf3efec54e21b adrs=holomush-ci829,holomush-stow5,holomush-1wbzn -->
