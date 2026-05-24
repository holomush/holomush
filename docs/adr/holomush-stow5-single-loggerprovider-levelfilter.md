<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Single LoggerProvider With Per-Sink LevelFilter Processors

**Date:** 2026-05-23
**Status:** Accepted
**Decision:** holomush-stow5
**Deciders:** HoloMUSH Contributors

## Context

The application-log pipeline (see ADR `holomush-ci829`) must fan records out
to up to three sinks — stderr, the OTLP collector (→ Loki/Grafana), and
Sentry — each with an **independent severity floor** (e.g. stderr at
`debug` locally while Sentry only takes `warn`). Two structural shapes were
evaluated for the OTel side.

The stderr sink is special: it is served by the existing `traceHandler`
(`internal/logging/handler.go`), which is a plain `slog.Handler` and is not
OTel-aware. So regardless of the OTel topology, a `slog`-layer fan-out is
needed to tee the stderr handler and the OTel bridge.

## Alternatives Considered

1. **One `LoggerProvider` per sink.** Each provider handles its own level
   natively, needing no custom processor. But the `slog`→OTel bridge
   conversion then runs once per provider, it is inconsistent with the
   `TracerProvider`-with-two-batchers shape already shipped, and it
   multiplies provider lifecycles to construct and shut down.
2. **Single `LoggerProvider` + per-sink `LevelFilter` processor (chosen).**
   The bridge conversion runs once; maximal symmetry with the existing
   `TracerProvider`; a single shutdown chain; a ~20-line custom
   `sdklog.Processor` (`LevelFilter`) delivers per-sink levels by dropping
   records below a threshold before each batch processor.

## Decision

A single `sdk/log.LoggerProvider` holds up to two batch-processor pipelines
— the collector via `otlploggrpc`, Sentry via `otlploghttp` — and each is
wrapped in a `LevelFilter` processor that drops records below the configured
per-sink severity floor. At the `slog` layer a `FanoutHandler` tees every
record to the stderr `traceHandler` (gated by its own `LevelGate`) and to
the `otelslog` bridge that feeds the provider.

## Rationale

- Mirrors the `TracerProvider`-with-two-batchers shape shipped in
  `holomush-0wun`, keeping the telemetry subsystem internally consistent.
- A single bridge pass avoids redundant `slog`→OTel record conversion.
- `LevelFilter` is a self-contained, testable type with no external
  dependencies; per-sink levels are the one thing a single provider cannot do
  natively, and this is the minimal mechanism that supplies them.

## Consequences

**Positive:**

- `telemetry.Init` manages one provider lifecycle instead of N.
- Adding a fourth sink is appending another processor, not constructing a
  new provider.

**Negative:**

- The custom `LevelFilter` must track the `sdklog.Processor` interface if it
  changes across future `v0.x` releases (caught by the Task-1 API smoke
  test).

**Neutral:**

- A `FanoutHandler` at the `slog` layer is still required to decouple the
  non-OTel stderr `traceHandler` from the bridge path.

## References

- Spec: `docs/superpowers/specs/2026-05-23-otel-native-log-surfacing-design.md` §3
- Plan: Tasks 3 (`LevelFilter`), 4 (`FanoutHandler`/`LevelGate`), 7 (provider build)
- Related: holomush-ci829 (OTel-native binding)
