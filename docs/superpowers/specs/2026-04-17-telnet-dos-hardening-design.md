# Telnet DoS Hardening Design

**Status:** Draft
**Date:** 2026-04-17
**Author:** seanb4t (via Claude Opus 4.7)
**Bead:** holomush-abbg (P0, security-finding, co-design-dos)
**Related:** holomush-brlb (IP rate limiting on auth — separate PR, shares `co-design-dos` label)

## RFC2119 Keywords

| Keyword        | Meaning                                    |
| -------------- | ------------------------------------------ |
| **MUST**       | Absolute requirement                       |
| **MUST NOT**   | Absolute prohibition                       |
| **SHOULD**     | Recommended, may ignore with justification |
| **SHOULD NOT** | Not recommended                            |
| **MAY**        | Optional                                   |

## Problem

The telnet gateway has no resource limits. Three independent gaps allow a single attacker to hold server resources indefinitely or exhaust them:

1. **Unbounded accept loop.** `cmd/holomush/gateway.go:runTelnetAcceptLoop` calls `listener.Accept()` in a loop and spawns one goroutine per connection without any capacity check. An attacker opening 50,000 TCP connections creates 50,000+ goroutines, exhausts file descriptors, and eventually OOMs the gateway.
2. **No read or write deadlines.** `internal/telnet/gateway_handler.go` reads from a `bufio.Scanner` wrapping a raw `net.Conn` with `SetReadDeadline` and `SetWriteDeadline` never called. A Slowloris attacker drip-feeding one byte every 10 minutes holds the goroutine and scanner buffer indefinitely. Writes to a stuck client block the handler against a full kernel send buffer.
3. **No pre-authentication timeout.** A connection that never sends `connect <user>` sits at the welcome prompt forever, consuming the same goroutine tree as a real player.

The gateway's resource budget is finite. Without explicit limits, one motivated attacker can take the telnet surface offline from a single source IP, bypassing the per-account lockout that only activates on actual authentication failures (`internal/auth/ratelimit.go`).

## Scope

**In scope for this spec:** Defensive controls contained within the telnet gateway — accept-loop capacity, per-connection deadlines, pre-auth timeout, operator-tunable config, and observability.

**Out of scope (deferred):**

- **Per-IP rate limiting on authentication RPCs** — tracked by `holomush-brlb`. That work addresses credential stuffing and auth spam across both telnet and web surfaces; the data model (per-IP token buckets applied at RPC entry) is distinct from a global connection cap. Shipping separately keeps each PR focused.
- **Per-IP connection caps** — a fair-sharing model (e.g., "max 10 concurrent connections per source IP") would overlap with `brlb`'s per-IP infrastructure and is premature before that lands.
- **Web gateway DoS controls** — web connections are HTTP/WebSocket, governed by `net/http` server timeouts that already set sensible defaults. Out of scope here.
- **Load testing at scale** — production Slowloris rehearsal, tuned against a real deployment, is a separate exercise.

## Design

### Four controls

| # | Control | Threshold (default) | Config knob |
| - | ------- | ------------------- | ----------- |
| 1 | Global concurrent-connection cap | 1000 | `--telnet-max-conns` |
| 2 | Per-connection read idle timeout | 5 min | `--telnet-idle-timeout` |
| 3 | Per-send write timeout | 30 s | `--telnet-write-timeout` |
| 4 | Pre-auth total timeout | 2 min | `--telnet-pre-auth-timeout` |

Defaults are chosen for a modest VPS (≈1 GB RAM) hosting a hobby-to-mid-size MUSH. Operators scaling up a grid tune the flags.

### Accept-loop capacity (control 1)

`cmd/holomush/gateway.go:runGatewayWithDeps` creates a buffered channel acting as a counting semaphore:

```go
slots := make(chan struct{}, cfg.TelnetMaxConns)
```

The channel is threaded into `runTelnetAcceptLoop` alongside `slots`. The loop becomes:

```go
for {
    conn, acceptErr := listener.Accept()
    // ... existing context-cancelled check + exponential backoff on error ...

    select {
    case slots <- struct{}{}:
        handler := telnet.NewGatewayHandler(conn, client, registry, telnetLimits)
        go func() {
            defer func() { <-slots }()
            handler.Handle(ctx)
        }()
    default:
        telnet.RefuseOverCapacity(conn)
    }
}
```

Resource accounting:

- **Gauge** `holomush_telnet_connections_active` MUST be incremented when a slot is acquired and decremented in the deferred release. Background: the channel length is not exposed; a gauge is required for observability.
- **Counter** `holomush_telnet_connections_refused_total` MUST be incremented in the `default` branch.

`RefuseOverCapacity(conn net.Conn)` lives in the telnet package. It sets a short write deadline (`WriteTimeout`, same as normal sends), writes `"Server at capacity. Try again later.\r\n"`, closes the connection, and returns. Write errors on refusal are logged at `debug` — the client has already given up or we cannot help them further.

**Rationale for channel-based semaphore over `golang.org/x/sync/semaphore`:** zero new dependencies, idiomatic Go, trivial to reason about in tests. `semaphore.Weighted` adds value only when you want weighted acquisitions or blocking `Acquire` — neither applies here.

**Rationale for accept-and-refuse over don't-accept:** without an accept, the kernel's listen backlog fills and silently drops SYNs, giving the client no feedback and making "why can't I connect?" operationally invisible. Refusing with a visible line costs one write + one close and generates a log / metric the operator can see. The refusal cost per attacker is trivial relative to the cost we prevent.

### Handler deadlines & pre-auth timer (controls 2, 3, 4)

A new exported struct collects the three per-connection knobs:

```go
// Limits bounds per-connection resource use. Zero values are treated as
// "use DefaultLimits" — callers MUST NOT rely on uninitialised Limits
// meaning "unlimited".
type Limits struct {
    IdleReadTimeout time.Duration
    WriteTimeout    time.Duration
    PreAuthTimeout  time.Duration
}

var DefaultLimits = Limits{
    IdleReadTimeout: 5 * time.Minute,
    WriteTimeout:    30 * time.Second,
    PreAuthTimeout:  2 * time.Minute,
}
```

`NewGatewayHandler` gains a `Limits` parameter. Callers in the accept loop construct `Limits` from `gatewayConfig`.

#### Read deadline (control 2)

The current code wraps the connection in `bufio.NewReader(h.conn)`. It is replaced with a thin wrapper:

```go
// deadlineReader refreshes the read deadline on every underlying Read, so
// an idle TCP stream is closed by the kernel after IdleReadTimeout
// regardless of how the bufio.Scanner schedules its reads.
type deadlineReader struct {
    conn    net.Conn
    timeout time.Duration
}

func (r *deadlineReader) Read(p []byte) (int, error) {
    if err := r.conn.SetReadDeadline(time.Now().Add(r.timeout)); err != nil {
        return 0, err
    }
    return r.conn.Read(p)
}
```

`NewGatewayHandler` constructs `bufio.NewReader(&deadlineReader{conn, limits.IdleReadTimeout})`. The scanner goroutine is otherwise unchanged.

A client that stops sending bytes sees the next `conn.Read` return an `i/o timeout` net.Error after `IdleReadTimeout`. The scanner surfaces this as a scan error; the existing `errCh` path in `Handle()` closes the connection. Metric `holomush_telnet_idle_timeouts_total` MUST be incremented when the error is a `net.Error` with `Timeout() == true`.

#### Write deadline (control 3)

`send()` is updated to set a deadline before every write:

```go
func (h *GatewayHandler) send(msg string) {
    if err := h.conn.SetWriteDeadline(time.Now().Add(h.limits.WriteTimeout)); err != nil {
        slog.Debug("gateway: failed to set write deadline", "error", err)
        return
    }
    if _, err := fmt.Fprintln(h.conn, sanitizeTelnetOutput(msg)); err != nil {
        slog.Debug("gateway: failed to send message", "error", err)
    }
}
```

A write that fails against a stuck client returns immediately. Subsequent writes reset the deadline; if the client remains stuck, the idle-read timeout eventually tears down the connection. No retry loop — retries against a stuck peer only burn CPU.

#### Pre-auth timer (control 4)

The timer is a local variable in `Handle()`. Its firing closes the connection only if the handler is still unauthenticated; a fire after successful character selection is a harmless no-op that falls through to the next select iteration.

Implementation in `Handle()`:

```go
preAuth := time.NewTimer(h.limits.PreAuthTimeout)
defer preAuth.Stop() // no-op if already fired; safe to call

for {
    select {
    case <-preAuth.C:
        if !h.authed {
            h.send("Authentication timeout.")
            telnetMetrics.RecordPreAuthTimeout()
            return
        }
        // authed already — timer won through the select but is now
        // moot; fall through to the next iteration.

    // ... existing cases ...
    }
}
```

No boolean guard and no cross-method `Stop()` call are needed. The timer field does not need to live on the handler struct — the local variable plus the `h.authed` check in the case body is sufficient and keeps the pre-auth concern isolated to `Handle()`.

Metric `holomush_telnet_preauth_timeouts_total` MUST be incremented when the timer fires while unauthenticated.

**Why cancel on character-selected, not on `AuthenticatePlayer` success:** the laxer option (cancel after `AuthenticatePlayer`) leaves a window where a registered player who sat in `selectMode` indefinitely is only covered by the 5-min idle-read deadline. The stricter option (this design) sets a hard 2-min cap from connect to "actually playing a character," which also covers the case where `selectMode` state is tickled by keep-alive bytes that reset the idle deadline but never progress the auth flow. Single knob, single concept, simplest model.

### Config

`gatewayConfig` in `cmd/holomush/gateway.go` gains four fields:

```go
TelnetMaxConns       int           `koanf:"telnet_max_conns"`
TelnetIdleTimeout    time.Duration `koanf:"telnet_idle_timeout"`
TelnetWriteTimeout   time.Duration `koanf:"telnet_write_timeout"`
TelnetPreAuthTimeout time.Duration `koanf:"telnet_pre_auth_timeout"`
```

Cobra flag registration:

```go
cmd.Flags().IntVar(&cfg.TelnetMaxConns, "telnet-max-conns", defaultTelnetMaxConns, "max concurrent telnet connections")
cmd.Flags().DurationVar(&cfg.TelnetIdleTimeout, "telnet-idle-timeout", defaultTelnetIdleTimeout, "idle read timeout before disconnect")
cmd.Flags().DurationVar(&cfg.TelnetWriteTimeout, "telnet-write-timeout", defaultTelnetWriteTimeout, "per-write deadline")
cmd.Flags().DurationVar(&cfg.TelnetPreAuthTimeout, "telnet-pre-auth-timeout", defaultTelnetPreAuthTimeout, "disconnect unauthenticated clients after this duration")
```

Defaults as named constants (`defaultTelnetMaxConns = 1000`, etc.).

`Validate()` MUST reject zero/negative values with `CONFIG_INVALID`:

```go
if cfg.TelnetMaxConns <= 0 {
    return oops.Code("CONFIG_INVALID").Errorf("telnet-max-conns must be positive, got %d", cfg.TelnetMaxConns)
}
if cfg.TelnetIdleTimeout <= 0 { /* ... */ }
// etc.
```

### Metrics

New package `internal/telnet` metrics file (`metrics.go`) following the pattern of `internal/audit/plugin_metrics.go`:

| Metric | Type | Purpose |
| ------ | ---- | ------- |
| `holomush_telnet_connections_active` | Gauge | Current open connection count. Primary DoS signal. |
| `holomush_telnet_connections_refused_total` | Counter | Capacity refusals. |
| `holomush_telnet_preauth_timeouts_total` | Counter | Pre-auth timer fires while unauthenticated. |
| `holomush_telnet_idle_timeouts_total` | Counter | Read deadline expiry. Slowloris indicator. |

Registered via `observability.MustRegister` in `runGatewayWithDeps` alongside the existing command metrics.

No histogram for connection lifetime — the gauge plus counters cover the DoS question without over-investing in observability that has no current consumer.

## Error handling

| Event | Response | Logging | Metric |
| ----- | -------- | ------- | ------ |
| Accept while `slots` full | Refusal line + close | `debug` | `connections_refused_total++` |
| Read deadline expires | Handler exits via `errCh` | `debug` | `idle_timeouts_total++` |
| Write deadline expires | `send()` logs and returns; caller unaffected | `debug` | — |
| Pre-auth timer fires unauthed | `"Authentication timeout."` + return | `debug` | `preauth_timeouts_total++` |
| Config non-positive limit | Startup failure | — | — (no telemetry yet) |

No `error`-level logs on normal-course rejection paths — a flood of errors from probes and scans creates noise that hides real problems. `debug` suffices; operators watch the counters for attack signal.

## Testing

Unit tests use short real timeouts (≤ 200 ms) rather than introducing a clock abstraction. A clock interface for one feature is over-engineering; network code with short real timers is idiomatic and the existing suite already uses real `time` in similar tests.

### Unit tests

| Test | File | Covers |
| ---- | ---- | ------ |
| `TestAcceptLoopRefusesAtCapacity` | `cmd/holomush/gateway_test.go` | `MaxConns=2`, third connection gets refusal line, close, counter +1 |
| `TestAcceptLoopReleasesSlotOnHandlerExit` | `cmd/holomush/gateway_test.go` | Fill, drain, new accept succeeds; gauge returns to 0 |
| `TestDeadlineReaderRefreshesOnEachRead` | `internal/telnet/deadline_reader_test.go` | Mocked `net.Conn` records `SetReadDeadline` calls; one per `Read` |
| `TestReadDeadlineFiresOnIdleClient` | `internal/telnet/gateway_handler_test.go` | `IdleReadTimeout=100ms`, no bytes sent, handler exits; `idle_timeouts_total` incremented |
| `TestReadDeadlineResetsOnByte` | `internal/telnet/gateway_handler_test.go` | Byte every 50 ms with `IdleTimeout=100ms`; handler alive after 300 ms |
| `TestWriteDeadlineAppliedBeforeEachSend` | `internal/telnet/gateway_handler_test.go` | Mock conn; `send()` sets deadline `≥ now + WriteTimeout` |
| `TestPreAuthTimerFiresForUnauthedClient` | `internal/telnet/gateway_handler_test.go` | `PreAuthTimeout=100ms`, no `connect`; "Authentication timeout." sent, handler exits, counter +1 |
| `TestPreAuthTimerCancelledAfterGuestSelect` | `internal/telnet/gateway_handler_test.go` | Guest flow; timer cancelled; handler alive after `PreAuthTimeout` |
| `TestPreAuthTimerCancelledAfterTwoPhaseSelect` | `internal/telnet/gateway_handler_test.go` | `connect alice pwd` + `PLAY alice`; timer cancelled |
| `TestGatewayConfigValidateRejectsNonPositiveLimits` | `cmd/holomush/gateway_test.go` | Zero/negative values for each knob return `CONFIG_INVALID` |
| `TestRefuseOverCapacityWritesMessageAndCloses` | `internal/telnet/refuse_test.go` | Mock conn; refusal line written; `Close` called; write deadline set |

### Integration tests

Under `//go:build integration` in `test/integration/telnet/`:

| Test | Covers |
| ---- | ------ |
| `TestTelnetSlowlorisDropped` | Open real TCP conn; send 1 byte / 2 s under `IdleTimeout=1s`; server closes us within 2 s |
| `TestTelnetCapacityRefusal` | Open `MaxConns` real connections; next accept receives refusal line and EOF |
| `TestTelnetPreAuthTimeoutDropsIdleConnect` | Open conn, send nothing, under `PreAuthTimeout=500ms`; receive "Authentication timeout." and EOF |

Integration surface is intentionally tight — unit tests carry the bulk of correctness proofs.

### What is not tested

- Slowloris at scale (hundreds of concurrent slow clients) — load-test territory, not unit CI.
- Per-IP fairness — `holomush-brlb` scope.
- Prometheus metric output shape beyond "counter increments" — relies on `prometheus/client_golang` library correctness.

### Existing test suite

`internal/telnet/gateway_handler_test.go` continues to pass. The new `Limits` parameter on `NewGatewayHandler` is supplied via a test helper defaulting to `DefaultLimits`, so existing cases do not grow noise.

## Documentation

`site/docs/operating/telnet-security.md` gains a "Resource limits" section listing the four knobs, defaults, tuning guidance (`MaxConns ≥ peak_users × 1.5`), and the associated metrics for operator dashboards.

## Risks

| Risk | Likelihood | Mitigation |
| ---- | ---------- | ---------- |
| Default `MaxConns=1000` too low for a grid with a busy peak | Low | Documented; operators tune via flag; `connections_refused_total` flags the need in production |
| 5-min idle too aggressive for legitimate thinkers | Low | Documented; operators can raise; MUSH idlers typically hit the MUSH-level `idle` command or send keep-alives |
| Pre-auth 2-min timeout trips slow typers at character-picker | Low | Default is generous for 1–20 character inventories; very large inventories can increase the flag or bubble-migrate to web client |
| Write deadline too short on very slow networks | Low | 30 s is generous; any legitimate client whose TCP send buffer isn't draining within 30 s is effectively dead; operators can raise |
| Channel-semaphore races under very high accept rate | None observed | Buffered channel is a well-understood primitive; accept loop is single-goroutine |

## Non-goals (explicit)

- This PR does NOT add per-IP connection caps.
- This PR does NOT add auth-endpoint rate limiting.
- This PR does NOT add any web-gateway DoS controls.
- This PR does NOT alter the telnet protocol, character sets, or line semantics.
- This PR does NOT change the handler's command dispatch, event formatting, or RPC contract with core.

## Success criteria

- `task pr-prep` passes cleanly before merge.
- `holomush_telnet_connections_active` visible in operator's Prometheus/Grafana after deploy.
- A manual `nc host port` that sits idle is dropped within `IdleReadTimeout` with `idle_timeouts_total` incremented.
- A manual burst of `MaxConns + 10` connections produces 10 refusal lines and `connections_refused_total == 10`.
- Existing telnet unit + integration + E2E tests remain green.
