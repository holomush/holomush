<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# ADR-001: Dead Connection Detection Strategy

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

### The Problem

Neither HTTP/1.1 nor HTTP/2 automatically notifies the server when a TCP
peer silently dies. Detection requires active probing:

- **HTTP/2 (TLS, production):** Server PING/PONG frames detect dead peers
  in ~45 seconds via `ReadIdleTimeout` + `PingTimeout` on `http2.Server`.
- **HTTP/1.1 (no TLS, dev):** No transport-level ping mechanism. Browsers
  only negotiate HTTP/2 via TLS ALPN — they do NOT support h2c (HTTP/2
  cleartext). In dev mode without TLS, browser connections are HTTP/1.1.

## Decision

Use a two-layer detection strategy:

**Layer 1 — HTTP/2 server pings (production, TLS):**

Configure `http2.Server` with keepalive settings. When browsers negotiate
HTTP/2 via TLS ALPN, server pings detect dead peers in ~45 seconds.

| Setting            | Value | Purpose                        |
| ------------------ | ----- | ------------------------------ |
| `ReadIdleTimeout`  | 30s   | Send PING after 30s of silence |
| `PingTimeout`      | 15s   | Close if no PONG within 15s    |
| `WriteByteTimeout` | 10s   | Close if write blocks >10s     |

**Layer 2 — Application heartbeat (dev, HTTP/1.1):**

The `StreamEvents` handler sends a control frame every 15 seconds. When the
heartbeat write fails (broken pipe after TCP retransmit timeout), the stream
exits and the cleanup defer fires. Detection latency: ~1-2 minutes depending
on kernel TCP settings.

**Layer 3 — Client-side disconnect (SPA navigation):**

The terminal page's `onDestroy` calls `client.disconnect()` when the user
navigates away within the SPA. This provides immediate cleanup for the
common case of in-app navigation.

### h2c Considered and Rejected

We evaluated wrapping the handler with `h2c.NewHandler` for HTTP/2 cleartext
in dev mode. This was rejected because:

- Browsers do NOT support h2c — they only negotiate HTTP/2 via TLS ALPN
- h2c adds attack surface (`h2c.NewHandler` buffers the entire first request
  body in memory, requiring `MaxBytesHandler` mitigation)
- The only beneficiaries would be programmatic Go/Java clients in dev, which
  is not a meaningful use case
- The heartbeat already handles dev mode disconnect detection

## Alternatives Considered

- **h2c for dev mode:** Rejected — browsers don't support it, adds complexity
  and attack surface for no practical benefit. See above.

- **WebSocket for streaming:** Large architectural change that abandons
  ConnectRPC server-streaming. Disproportionate to the problem.

- **Heartbeat only (no HTTP/2 pings):** Insufficient for production. TCP
  write buffer absorbs small heartbeat frames; detection takes 1-2 minutes.
  HTTP/2 pings operate at the transport layer and detect dead peers in ~45s.

## Consequences

### Positive

- Production (TLS): dead connection detection in ~45 seconds
- Dev (no TLS): detection via heartbeat, acceptable latency for development
- Clean client-side disconnect on SPA navigation
- No unnecessary h2c complexity or attack surface

### Negative

- Dev mode detection is slower (~1-2 min) than production (~45s)
- Reverse proxies in production MUST support HTTP/2 upstream

## Affected Components

| Component                 | Change                                                |
| ------------------------- | ----------------------------------------------------- |
| `internal/web/server.go`  | `http2.ConfigureServer` with ping/timeout settings    |
| `internal/web/handler.go` | Heartbeat + `ctx.Done()` select in stream loop        |
| `internal/web/handler.go` | Defer calls core `Disconnect` RPC on stream close     |
| `terminal/+page.svelte`   | `onDestroy` calls `client.disconnect()`               |
| `site/docs/operating/`    | HTTP/2 requirement noted for reverse proxies          |

## References

- [ConnectRPC #668: detect if client closed stream](https://github.com/connectrpc/connect-go/issues/668)
- [Go x/net/http2.Server](https://pkg.go.dev/golang.org/x/net/http2#Server)
