<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Per-Connection JetStream Filtering via SessionStreamRegistry.SendToConnection

**Date:** 2026-05-21
**Status:** Accepted
**Decision:** holomush-nki4
**Deciders:** HoloMUSH Contributors

## Context

`SessionStreamRegistry` currently keys subscribers by `sessionID`; `Send(sessionID, update)` broadcasts to every channel registered under that session (`internal/grpc/stream_registry.go:79-95`). The filter set is per-`busStream` (one per Subscribe call), but the update channel that drives filter changes is shared session-wide.

Phase 5's multi-connection visibility requires per-Connection subscription deltas. The motivating example: alice has telnet focused on grid + web focused on scene #42. The two Subscribe calls must NOT mirror each other's filter updates — telnet must keep its grid filter; web must add scene #42 IC/OOC subscriptions; alice's character must remain visible at both contexts.

Three structural shapes were considered:

1. **Push per-Connection routing into the plugin-facing API** — plugins call `AddSessionStream(connection_id, stream)` etc. with a connection_id parameter.
2. **Extend `SessionStreamRegistry` internally with per-Connection routing**, keeping the plugin surface unchanged.
3. **Introduce a separate per-Connection registry** alongside the session-keyed one.

The plugin-facing API is already settled by ADR `holomush-kuf8` (three focus RPCs that don't expose connection identity for stream routing). `SubscribeRequest` already carries `ConnectionId` + `ClientType` (verified at `internal/grpc/subscribe_server_test.go:213-247`), so the substrate already has the data it needs.

## Decision

Extend `SessionStreamRegistry` internally with two new methods:

- `RegisterConnection(sessionID string, connectionID ulid.ULID, ch chan<- sessionStreamUpdate)` — pairs the new per-Connection routing alongside the existing session-keyed `Register`.
- `SendToConnection(sessionID string, connectionID ulid.ULID, update sessionStreamUpdate) error` — targets a single connection's filter-update channel. Returns `CONNECTION_NOT_REGISTERED` or `CONTROL_CHANNEL_FULL`.

The plugin-facing API stays scoped to the three new focus RPCs (`SetConnectionFocus`, `AutoFocusOnJoin`, `IsAnyConnFocused`). Per-Connection deltas are computed by a substrate-internal `subscription_router` (new file `internal/grpc/focus/subscription_router.go`) on each focus change and dispatched via `SendToConnection`.

The session-wide `Send` remains for character-level always-on streams (e.g., `notifications:<character_id>`) that must reach every connection regardless of focus.

`CoreServer.Subscribe` switches its registration call from `Register(sessionID, ch)` to `RegisterConnection(sessionID, connectionID, ch)` using the already-populated `SubscribeRequest.ConnectionId`.

## Rationale

`SubscribeRequest` already carries `ConnectionId` + `ClientType` (verified at `internal/grpc/subscribe_server_test.go:213-247`); telnet and web gateways both populate them. The substrate already has the data needed to key subscriber channels by connection identity — the existing `SessionStreamRegistry.Register(sessionID, ch)` just doesn't use it.

Keeping the plugin-facing API scoped to the three new focus RPCs (ADR `holomush-kuf8`) — without exposing connection identity for stream routing — preserves a clean separation: plugins manipulate focus state via RPCs that work on the same connection-id input shape they already use (proto `bytes ULID`). The internal subscription_router pulls the resulting stream deltas without plugins having to compute them.

The session-wide `Send` remains useful for character-level streams (e.g., `notifications:<character_id>`) that should reach every connection. Keeping both routing tables — session-keyed for broadcast, conn-keyed for targeted — preserves the existing surface for callers that don't need per-Connection precision while adding the new mechanism without breaking change.

## Alternatives Considered

**Expose connection_id in `AddSessionStream` / `RemoveSessionStream`.** Plugins would call `AddConnectionStream(connection_id, stream)` etc. Rejected because it pushes the per-Connection routing concern into the plugin API; plugins would have to track which connection each subscription belongs to, replicating substrate state in plugin code. It would also widen the surface area of plugin-host calls that depend on per-Connection identity beyond the focus subsystem.

**Replace session-wide `Send` with conn-keyed routing throughout.** Remove the existing session-wide broadcast in favor of per-Connection-only routing. Rejected because character-level always-on streams (notifications) legitimately need to reach all of a character's connections; replacing the broadcast would require callers to enumerate per-conn and dispatch N times, defeating the broadcast use case.

**Separate per-Connection registry as a sibling type.** A new `ConnectionStreamRegistry` distinct from `SessionStreamRegistry`. Rejected because the two routing surfaces share teardown logic (deregister-on-disconnect) and channel buffer-full handling; keeping them on one type lets one Subscribe-handler defer block handle both.

## Consequences

**Positive:**

- Plugins never learn about connection identity for focus-driven subscriptions. The per-Connection mechanism is fully isolated inside the substrate.
- No protocol change required — `SubscribeRequest` already carries `ConnectionId`; only the registration call inside `CoreServer.Subscribe` changes.
- The `SendToConnection` semantic is pinned by INV-P5-10 with a positive (target conn receives) + negative (other conns do NOT receive within 50ms) assertion.

**Negative:**

- The registry now maintains two routing tables (session-keyed and conn-keyed). Disconnect handlers must deregister from both (T11 calls `DeregisterConnection` paired with the existing `Deregister`).
- A `Send` returning `SESSION_NOT_FOUND` vs `SendToConnection` returning `CONNECTION_NOT_REGISTERED` is a small error-code surface duplication. Callers always know which method they invoked, so there is no call-site ambiguity, but the codebase carries both codes.

**Neutral:**

- Defines a structural boundary for any future feature that needs per-Connection targeting (e.g., per-tab notifications). They will use `SendToConnection` rather than inventing a new mechanism.

## Source

- Spec: `docs/superpowers/specs/2026-05-21-scenes-phase-5-focus-model-and-multi-connection-visibility-design.md` §3 D5, §4.3, §10 INV-P5-10
- Plan: `docs/superpowers/plans/2026-05-21-scenes-phase-5-focus-model-and-multi-connection-visibility.md` Phase B (T9-T11)
- Existing pattern: `internal/grpc/stream_registry.go:62-95` (Send / Deregister)
