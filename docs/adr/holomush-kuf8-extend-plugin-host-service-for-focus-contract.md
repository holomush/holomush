<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Extend PluginHostService with Focus RPCs (Substrate-Hosted Bridge)

**Date:** 2026-05-21
**Status:** Superseded by holomush-2fb90
**Decision:** holomush-kuf8
**Deciders:** HoloMUSH Contributors

## Context

Scenes v2 §11 left the plugin↔server focus contract open. Phase 5 needs to close the gap: the core-scenes plugin must be able to tell the substrate (a) that a given connection is now focused on a specific scene, (b) that newly-joined characters should auto-focus their terminal/telnet connections, and (c) whether the notification emitter is allowed to elide a notification because the recipient is already focused on the source scene.

Two structural shapes were considered:

1. **Extend the existing substrate-hosted `PluginHostService`** (plugin is the gRPC client; mirrors the existing `JoinFocus` / `LeaveFocus` / `PresentFocus` precedent at `api/proto/holomush/plugin/v1/plugin.proto`).
2. **Introduce a host→plugin callback RPC** where the substrate calls into the plugin (e.g., a new `SceneService.IsParticipant` RPC) to ask membership questions.

The choice determines which side owns trust, where the gRPC client lives, and whether INV-S3 (Go+Lua hostfunc parity) can be preserved cheaply. Phase 4's parallel decision (denormalize pose-order metadata in the substrate) prefigured the same trust direction: substrate holds state, plugin calls in.

## Decision

Extend `PluginHostService` with three new RPCs:

- `SetConnectionFocus(connection_id, focus_key, is_scene_grid) → focus_key` — apply a focus change to a specific connection.
- `AutoFocusOnJoin(character_id, scene_id) → { focused, skipped, failed, total }` — fan-out terminal-only auto-focus on scene join.
- `IsAnyConnFocused(character_id, scene_id) → bool` — notification-emission helper.

The substrate remains the gRPC server; the plugin remains the client. All three RPCs use `bytes` ULIDs on the wire and ship with Lua hostfunc bindings alongside the Go SDK methods (INV-S3 parity, per the existing `internal/plugin/hostfunc/stdlib_focus.go::parseFocusKey` convention).

No host→plugin callback path is introduced. The substrate never calls into untrusted plugin code on the focus hot path.

## Rationale

The substrate already hosts `JoinFocus` / `LeaveFocus` / `PresentFocus` RPCs on `PluginHostService` (`api/proto/holomush/plugin/v1/plugin.proto`); the plugin is the gRPC client. Phase 5's three new RPCs slot in naturally next to those existing RPCs without inventing a new transport direction.

Keeping the trust boundary one-directional (plugin → substrate) means the substrate never has to deal with the failure modes of calling untrusted plugin code on a hot path: no per-call timeouts to tune for plugin responsiveness, no plugin-crash recovery on the substrate side, no in-flight cancellation across the trust boundary. The substrate validates against state it owns (`FocusMemberships`, see companion ADR `holomush-x0ph`) and returns synchronously.

The Lua hostfunc surface (INV-S3 parity) is a thin shim per RPC mirroring the existing `internal/plugin/hostfunc/stdlib_focus.go::parseFocusKey` pattern. A bidirectional transport would have required either a new Lua callback registry or a generated stub per RPC in both directions — substantially more glue.

## Alternatives Considered

**Host→plugin callback RPC.** The substrate would call a new `SceneService.IsParticipant(character_id, scene_id) → bool` to ask membership questions. Rejected because (a) it introduces a new transport direction the substrate hasn't needed for any other operation, (b) ties focus-change latency to plugin responsiveness even though the substrate already has the answer in `session.Info.FocusMemberships`, and (c) doubles the Lua hostfunc surface — both a new client stub (for the substrate calling out) and a new server registration (for plugins serving it).

**Hybrid: plugin-owned auto-focus, substrate-owned focus state.** Plugin handles `scene join` end-to-end including the auto-focus side effect via its own machinery (e.g., emitting an event the substrate observes). Rejected because the substrate is the source of truth for `Connection.FocusKey`; routing the auto-focus through an event channel would mean async writes from the plugin to substrate state, breaking the atomic-mutator pattern (ADR `holomush-8new`).

## Consequences

**Positive:**

- Every focus operation traverses the existing trust boundary (plugin → substrate); the substrate validates inside its own lock and never has to call back into plugin code that could be slow, fault, or misbehave.
- INV-S3 Go+Lua hostfunc parity is a single hostfunc shim per RPC, not a new bidirectional transport that would have to be replicated in both runtimes.
- The chosen shape composes with the existing `PresentFocus` / `JoinFocus` / `LeaveFocus` RPCs and inherits their existing Lua hostfunc convention.

**Negative:**

- The substrate carries a slightly wider API surface (3 new RPCs).
- The substrate must own membership validation logic — addressed by the companion ADR `holomush-x0ph` (substrate validates focus membership via `session.Info.FocusMemberships`).

**Neutral:**

- The decision generalizes: future plugin types (channels, forums) that want focus operations will plumb through the same `PluginHostService` extension, not via per-plugin callback RPCs.

## Source

- Spec: `docs/superpowers/specs/2026-05-21-scenes-phase-5-focus-model-and-multi-connection-visibility-design.md` §3 D1, §3 D3, §6
- Companion ADR: `holomush-x0ph` (FocusMemberships validation)
- Plan: `docs/superpowers/plans/2026-05-21-scenes-phase-5-focus-model-and-multi-connection-visibility.md` Phase C (T12-T13)
