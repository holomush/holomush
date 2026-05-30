<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Gateway-Survival Resume Uses JetStream Durable-Consumer Re-Subscribe + Client-Side Dedup

**Date:** 2026-05-30
**Status:** Accepted
**Decision:** holomush-85exr
**Deciders:** HoloMUSH Contributors

## Context

When the core `Subscribe` stream breaks while a client socket is still open, the
gateway must resume event delivery gap-free instead of tearing the client down.
A prior design carried a caller-supplied replay cursor on `SubscribeRequest`
(`replay_from_cursor`); that field was **removed** and reserved
(`api/proto/holomush/core/v1/core.proto:228`) when the focus substrate landed.
The JetStream durable per-session consumer already tracks its own acked sequence
server-side (`internal/grpc/server.go:753-756/994/1065`, `internal/eventbus/bus.go:69-74`).

## Decision

On gateway-survival reconnect the gateway re-`Subscribe`s the **same**
`session_id` with **no** cursor input; core re-opens the durable per-session
JetStream consumer, which resumes from its acked sequence server-side. Because
core acks *after* sending (`server.go:1274`), at most the in-flight frame is
redelivered; the gateway tracks the last forwarded `EventFrame.cursor`/`id` and
dedups that overlap so the client sees no duplicate or missing events (invariant
**I-SURV-2**). If the session detached during the core-down gap, the gateway
reattaches via `SelectCharacter` (`reattached=true`). New web `ControlSignal`
values `RECONNECTING=4` / `RECONNECTED=5` surface the state to the client.

## Rationale

- `replay_from_cursor` was deliberately removed; the server-side durable consumer
  is the canonical resume mechanism and owns the replay policy.
- The consumer's acked-seq is the authoritative gap-free resume point; a
  gateway-supplied cursor would be redundant at best and incorrect at worst (the
  gateway may not know the server's exact acked position).
- Client-side dedup of the bounded overlap (at most one in-flight frame) is
  predictable, not a full replay scan.

## Alternatives Considered

- **Cursor-in-request resume** (re-add `replay_from_cursor`). Rejected: removed
  from the proto; caller-supplied cursors permit arbitrary rewinds (backpressure
  / abuse surface); the durable consumer already tracks the correct point.
- **Client-driven reconnect only** (no gateway survival; the browser reconnects).
  Rejected: exposes core restarts as user-visible disconnects and does not
  prevent ghost sessions (the client re-`SelectCharacter`s into a new session) —
  fails the "core restart doesn't interrupt activity" goal (G2).

## Consequences

The gateway must maintain per-connection last-forwarded event-id state for
dedup. Survival depends on the JetStream durable consumer surviving the core
restart; a lost consumer surfaces as `SESSION_NOT_FOUND` and routes to the
reattach path. Two new `ControlSignal` enum values are added to `web.proto`.
