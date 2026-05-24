---
paths:
  - "internal/**/*.go"
  - "cmd/**/*.go"
  - "pkg/**/*.go"
  - "plugins/**/*.go"
---

# Structured Logging Conventions

HoloMUSH logs through `log/slog`. The logger is built in
`internal/logging/handler.go` (`Setup`/`SetDefault`) and wraps a base
JSON/text handler with a `traceHandler` that injects `service`, `version`,
and — when present on the `context.Context` — `trace_id` and `span_id`.

## MUST use context-carrying log variants

| Requirement | Description |
| ----------- | ----------- |
| **MUST** call `*Context` variants | `slog.InfoContext(ctx, …)`, `WarnContext`, `ErrorContext`, `DebugContext`, and `errutil.LogErrorContext(ctx, msg, err, …)` whenever a `context.Context` is in scope |
| **MUST NOT** use bare variants when a ctx exists | `slog.Info(…)`, `logger.Warn(…)`, `errutil.LogError(…)` discard trace context and produce orphaned log lines |
| **MUST** thread the ctx | If a `ctx` is reachable as a parameter, struct field, or derivable value, plumb it into the log call rather than reaching for the bare form |
| **MAY** use bare variants | Only in the **absolutely-impossible** case: no `ctx` exists and one cannot reasonably be plumbed (init code, `main`, bare goroutines without a request/operation context, pure helpers with no caller context) |

## Why this is a MUST, not a SHOULD

Trace context (`trace_id` / `span_id`) is carried on the
`context.Context`, not on the logger. Only the `*Context` slog variants
extract it (via `traceHandler` today, and via the
`contrib/bridges/otelslog` bridge once application logs flow to the
OpenTelemetry log pipeline → Loki / Grafana / Sentry).

A bare `slog.Info("scene started", "id", id)` emits a log line with **no
trace correlation** — in Sentry's Logs view or Loki it cannot be linked
back to the span/transaction it happened inside. That defeats the entire
point of surfacing logs into a distributed-tracing backend: the value is
in *correlation*, and correlation is lost the moment the ctx is dropped.

## Examples

```go
// WRONG — ctx is right there in the signature, but dropped.
func (s *CoreServer) handle(ctx context.Context, req *Req) error {
    slog.Info("handling request", "kind", req.Kind)        // orphaned
    if err != nil {
        errutil.LogError(s.logger, "handle failed", err)   // orphaned
    }
}

// RIGHT — ctx threaded; the log line carries trace_id/span_id.
func (s *CoreServer) handle(ctx context.Context, req *Req) error {
    slog.InfoContext(ctx, "handling request", "kind", req.Kind)
    if err != nil {
        errutil.LogErrorContext(ctx, "handle failed", err)
    }
}

// ACCEPTABLE — no ctx in scope and none derivable (process bootstrap).
func main() {
    slog.Info("starting holomush", "version", buildVersion)
}
```

## Enforcement (planned)

This rule will be enforced mechanically by the `sloglint` linter with
`context: scope` — that setting flags a bare log call **only when a
`context.Context` is in scope**, which is exactly the "unless absolutely
impossible" carve-out above. Enabling it is a large mechanical migration
(~600 call sites today) tracked as `holomush-xrdjw`; until it lands, the
rule is enforced by review. Do not introduce new bare-variant call sites that
have a ctx available.
