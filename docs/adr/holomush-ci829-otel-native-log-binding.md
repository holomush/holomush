<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Bind Application Logs to OpenTelemetry; Sentry as a Config-Only OTLP Consumer

**Date:** 2026-05-23
**Status:** Accepted
**Decision:** holomush-ci829
**Deciders:** HoloMUSH Contributors

## Context

HoloMUSH already exports traces to Sentry through an OpenTelemetry-native
dual-export (`holomush-0wun`): one `TracerProvider`, a collector batcher,
and a second batcher pointing at Sentry's OTLP-HTTP endpoint. The governing
invariant from that work, stated in `site/docs/operating/sentry.md`, is that
"removing Sentry is a one-line config change; no app code needs to be
touched."

Surfacing `log/slog` application logs into Sentry presented a fork. sentry-go
(`v0.46.2`, already a dependency) ships first-party logging integrations —
the `sentryslog` handler and `sentry.NewLogger` — that route logs directly to
Sentry. Alternatively, logs can flow through the OpenTelemetry log pipeline
with Sentry as a generic OTLP logs consumer. Sentry ingests OTLP logs at
`…/integration/otlp/v1/logs` (auth via the `x-sentry-auth` header); notably,
sentry-go provides **no** OTLP log exporter (only `NewTraceExporter`), so the
OTel path uses the generic `otlploghttp` exporter.

## Alternatives Considered

1. **`sentryslog` / `sentry.NewLogger` (vendor-SDK path).** Fewer new
   modules; sentry-go already imported; simplest wiring. But it couples
   application logging code to Sentry's API surface, breaks the
   vendor-neutral invariant established for traces, cannot share a single
   provider with the collector sink, and would require a second
   vendor-specific handler to add any future destination.
2. **OTel log pipeline; Sentry as OTLP consumer (chosen).** Application code
   stays vendor-neutral; mirrors the existing trace dual-export shape; a
   single bridge conversion pass feeds all sinks; collector and Sentry sinks
   are symmetric; swapping or removing Sentry needs no app-code change. Cost:
   the OTel log SDK and exporters are experimental `v0.x`, and DSN→endpoint
   derivation must be maintained.

## Decision

Application logs are routed through the OpenTelemetry `LoggerProvider` via a
`contrib/bridges/otelslog` bridge; Sentry receives logs as a plain OTLP-HTTP
consumer. **No sentry-go import is permitted outside `internal/telemetry`**
— in particular `internal/logging` and general application code must never
import it. This is enforced statically by INV-L1 (an import-guard test).

## Rationale

- Preserves the vendor-neutral invariant already established for traces.
- A single provider + bridge means the `slog`→OTel conversion runs once,
  regardless of how many sinks are active.
- Sentry can be replaced or disabled by changing configuration, not code.
- INV-L1 makes the boundary a grep-able static check, not a review hope.

## Consequences

**Positive:**

- Application code has no observable coupling to Sentry.
- Adding future sinks (Loki direct, CloudWatch, …) requires only a new
  processor, not new handler code.
- Trace correlation is automatic via OTel context propagation.

**Negative:**

- The OTel log SDK modules are `v0.x`; minor-version bumps may require
  coordinated updates (mitigated by pinning + a Task-1 API smoke test).
- DSN→OTLP-endpoint derivation must be maintained in `internal/telemetry`.

**Neutral:**

- `sentry.Flush` at shutdown now drains only the SDK's error buffers, not log
  buffers; the shutdown order changes accordingly (logs drain via
  `LoggerProvider.Shutdown`).

## References

- Spec: `docs/superpowers/specs/2026-05-23-otel-native-log-surfacing-design.md` §1, §6, §12 (INV-L1)
- Plan: `docs/superpowers/plans/2026-05-23-otel-native-log-surfacing.md`
- Prior art: `holomush-0wun` (trace dual-export), `site/docs/operating/sentry.md`
- Related: holomush-stow5 (provider topology), holomush-1wbzn (errutil error-event seam)
