<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# The Gateway Is the Liveness Authority for Connection Leases

**Date:** 2026-05-30
**Status:** Accepted
**Decision:** holomush-2w9vh
**Deciders:** HoloMUSH Contributors

## Context

Given a decaying connection-lease model (see holomush-6syxb), the question is
*which process refreshes the lease*. Core holds the session and connection
records. The gateway (a separate, long-lived process) holds the actual client
sockets — web (`internal/web`) and telnet (`internal/telnet`) — and has direct
visibility into client-death signals (WebSocket close, TCP RST, idle-timeout,
heartbeat `Send` failure) that core cannot observe from its side of the
gateway↔core gRPC stream.

## Decision

The gateway process is the **sole authority** that refreshes a connection's
liveness lease. It calls `RefreshConnection` on each heartbeat tick while the
client socket is open and ceases refreshing within one tick of detecting
transport loss (invariant **I-LIVE-1**). The existing 15s client-liveness
heartbeat in `StreamEvents` is the refresh vehicle on web; the telnet read-loop
/ idle-timeout is the equivalent on telnet.

## Rationale

- The gateway directly observes client-socket state that core cannot: a
  half-open client whose gateway↔core stream is still open would fool any
  core-side timer, but the gateway's heartbeat `Send` failure detects it.
- The existing 15s heartbeat is a natural refresh hook — no new timer
  infrastructure is required.
- The gateway already calls `Disconnect` on client loss; `RefreshConnection` is
  the symmetric positive liveness assertion.

## Alternatives Considered

- **Core-side timer bump** (no gateway change, no new RPC): core bumps
  `last_seen_at` on inbound stream activity. Rejected: core's view of the
  gateway↔core stream cannot detect a dead client whose stream is still open, so
  it would keep a half-open client's lease alive indefinitely — defeating the
  entire purpose of the lease.

## Consequences

Steady-state cost: one `RefreshConnection` call per connection per refresh
interval (~15s). The gateway gains a correctness obligation — a bug that fails
to stop refreshing on client death would keep a ghost alive — but that surface
is small and symmetric with the existing `Disconnect`-on-close path.
