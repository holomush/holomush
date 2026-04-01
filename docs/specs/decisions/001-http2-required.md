<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# ADR-001: Gateway Requires HTTP/2 for All Connections

**Date:** 2026-04-01
**Status:** Accepted
**Participants:** Sean, Claude

## Context

The web gateway uses ConnectRPC server-streaming for event subscriptions
(location events, say/pose, presence updates). These are long-lived streams
that persist for the duration of a player's session — potentially hours.

When a client silently disconnects (network drop, tab close, mobile app
killed), the server must detect the dead connection and clean up the session
(emit leave events, update presence, delete or detach the session record).

### The HTTP/1.1 Problem

With HTTP/1.1, the server has no transport-level mechanism to detect dead
peers. The only detection path is:

1. Server writes data to the stream (heartbeat or game event)
2. TCP write succeeds because the kernel send buffer absorbs the small frame
3. Kernel TCP retransmit timer eventually expires (~1-2 minutes)
4. Next write fails with broken pipe

This means sessions leak for 1-2 minutes after every unclean disconnect.
During that window, the player appears "grid present" to other players,
presence lists are stale, and session resources are held.

### The HTTP/2 Solution

HTTP/2 provides PING/PONG frames at the transport layer. Go's `x/net/http2`
server supports:

- `ReadIdleTimeout`: send PING after N seconds of silence
- `PingTimeout`: close connection if no PONG within N seconds
- `WriteByteTimeout`: close if a write blocks longer than N seconds

When a dead connection is detected, all stream contexts on that connection
are cancelled, and cleanup defers fire immediately.

## Decision

The web gateway MUST use HTTP/2 for all client connections. HTTP/1.1 is not
supported.

- **Production (TLS):** HTTP/2 via ALPN negotiation (standard)
- **Development (no TLS):** HTTP/2 via h2c (HTTP/2 cleartext)

The gateway is configured with:

| Setting            | Value | Purpose                         |
| ------------------ | ----- | ------------------------------- |
| `ReadIdleTimeout`  | 30s   | Send PING after 30s of silence  |
| `PingTimeout`      | 15s   | Close if no PONG within 15s     |
| `WriteByteTimeout` | 10s   | Close if write blocks >10s      |

**Maximum detection latency:** 45 seconds (30s idle + 15s ping timeout).

## Alternatives Considered

- **Application-level heartbeat only:** Heartbeat writes to dead HTTP/1.1
  connections succeed because TCP write buffers absorb small frames. Detection
  still takes ~1-2 minutes. Does not solve the fundamental problem.

- **Dual HTTP/1.1 + HTTP/2 support:** Adds complexity for a protocol no target
  client needs. Session cleanup behavior would differ by protocol version,
  creating subtle bugs in presence and disconnect handling.

- **WebSocket for streaming:** Large architectural change that abandons
  ConnectRPC server-streaming and requires separate connection management.
  Disproportionate to the problem.

## Consequences

### Positive

- Dead connection detection works consistently in all modes (~45s)
- No application-level heartbeat workarounds needed for connection liveness
- Simpler stream handler code (no dual-protocol paths)

### Negative

- `curl` requests in dev need `--http2-prior-knowledge` flag
- Very old HTTP clients or proxies that don't support HTTP/2 cannot connect
- Reverse proxies in front of the gateway MUST support HTTP/2 upstream

### Neutral

- All modern browsers support HTTP/2 over TLS (>97% global support)
- ConnectRPC Go/Java clients support h2c natively
- **Browsers do NOT support h2c** — they only negotiate HTTP/2 via TLS ALPN.
  In dev mode (no TLS), browser connections fall back to HTTP/1.1 through the
  h2c handler's HTTP/1.1 passthrough. HTTP/2 ping-based detection only applies
  to production (TLS) and programmatic clients.
- In dev mode, the application-level heartbeat (15s interval) remains the
  primary dead connection detection mechanism for browser clients. Detection
  latency depends on TCP retransmit timeout (~1-2 minutes)

## Affected Components

| Component                  | Change                                           |
| -------------------------- | ------------------------------------------------ |
| `internal/web/server.go`   | Wrap handler with `h2c.NewHandler`               |
| `internal/web/handler.go`  | Stream loop uses `ctx.Done()` for cancellation   |
| `site/docs/operating/`     | Document HTTP/2 requirement                      |
| `compose.yaml`             | No change (internal networking supports h2c)     |

## References

- [ConnectRPC #668: detect if client closed stream](https://github.com/connectrpc/connect-go/issues/668)
- [Go x/net/http2.Server](https://pkg.go.dev/golang.org/x/net/http2#Server)
- [h2c: HTTP/2 without TLS](https://pkg.go.dev/golang.org/x/net/http2/h2c)
