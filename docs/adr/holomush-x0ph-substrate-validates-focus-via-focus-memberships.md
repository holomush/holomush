<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Substrate Validates Focus Membership via session.Info.FocusMemberships

**Date:** 2026-05-21
**Status:** Accepted
**Decision:** holomush-x0ph
**Deciders:** HoloMUSH Contributors

## Context

`SetConnectionFocus` and `AutoFocusOnJoin` both need to answer "is this character actually a member of this scene?" before applying a focus change — otherwise INV-P5-1 (focus-without-membership impossibility) collapses and a character could be focused on a scene they cannot read.

Two structural options exist:

1. **Host→plugin RPC** — the substrate calls a new `SceneService.IsParticipant(character_id, scene_id) → bool` RPC, treating scene membership as plugin-owned data (the plugin's `scene_participants` table is the canonical record).
2. **Substrate-internal lookup** — the substrate reads `session.Info.FocusMemberships`, which is already populated by the plugin's existing `JoinFocus` call after `JoinScene` succeeds. The substrate has the data on hand without leaving its own process.

Option (1) introduces a new transport direction (host calls plugin), ties focus-change latency to plugin responsiveness, and would require a new gRPC client + Lua hostfunc per RPC. Option (2) leverages an existing server-authoritative field but couples the substrate's trust to whether the plugin honors the `JoinScene → JoinFocus` pairing — an architectural commitment the plugin must keep.

The plugin's existing `handleJoin` flow at `plugins/core-scenes/commands.go:357-384` already pairs `JoinScene → JoinFocus` in sequence; the substrate's existing focus operations (`PresentFocus`) already validate against `FocusMemberships`. Reaching for callback RPCs would invent a new transport direction for a question the substrate can already answer locally.

## Decision

Validate focus changes against the server-authoritative `session.Info.FocusMemberships` set; never call back into the plugin.

The plugin's contract is to pair `JoinScene → JoinFocus`. `FocusMemberships ⊆ scene_participants` is a contract guarantee, not a substrate-checked invariant — a plugin that violates the pairing produces silent membership without focus eligibility, but never the reverse.

Concretely, `SetConnectionFocus`'s mutator (Phase 5 `SessionConnectionMutator`, see ADR `holomush-8new`) inspects `info.FocusMemberships` inside its locked callback before writing `Connection.FocusKey`; `AutoFocusOnJoin` does the same on each per-conn iteration.

## Rationale

`session.Info.FocusMemberships` is already server-authoritative — written by the substrate's own `JoinFocus` RPC (`internal/grpc/focus/join.go:36`) and only that RPC. The substrate doesn't have to introduce new state ownership; it just reads what it already maintains.

The plugin's existing join flow (`plugins/core-scenes/commands.go:357-384`) is `JoinScene → JoinFocus` in sequence. JoinFocus completes only after JoinScene; the converse cannot happen by construction. So at any moment when the substrate inspects `FocusMemberships`, the corresponding `scene_participants` row exists. The substrate can rely on this without scanning the plugin's database.

The atomic-mutator pattern (ADR `holomush-8new`) keys off this: validation against `FocusMemberships` happens inside the `SessionConnectionMutator` callback under one lock, so the read-validate-write sequence is structurally race-free. A callback-based approach (host→plugin RPC) would have made the validation point an out-of-process call, which cannot be held inside the same lock.

## Alternatives Considered

**Add `IsParticipant` RPC to `SceneService` (host→plugin direction).** Substrate calls the plugin to ask membership questions. Rejected because (a) introduces a new transport direction that no existing operation uses, (b) makes validation out-of-process so it cannot run inside the substrate's atomic-mutator callback, and (c) adds plugin-failure modes (plugin crash, slow response) to a hot path the substrate could otherwise handle internally.

**Replicate `scene_participants` into substrate state.** Materialize the plugin's scene_participants table into a substrate-owned cache (similar to FocusMemberships). Rejected because it adds a synchronization burden the plugin would have to participate in (write-through cache invalidation) and effectively duplicates state that `FocusMemberships` already represents for the substrate's validation purposes.

## Consequences

**Positive:**

- No new host→plugin transport direction. Phase 5 ships exactly 3 new RPCs (all plugin→substrate); no new gRPC client surface in the substrate.
- No plugin-callback latency on the focus hot path. The substrate validates inside its own per-session lock, so the read-validate-write window is structurally closed (see ADR `holomush-8new`).
- INV-S3 (Go+Lua hostfunc parity) is unaffected — there is no host→plugin RPC to mirror in Lua.

**Negative:**

- The substrate trusts the plugin to call `JoinFocus` after `JoinScene`. A plugin bug that skips `JoinFocus` would silently allow scene membership without focus eligibility. (The user still appears in `scene list`, but `scene focus #X` would fail with `FOCUS_WITHOUT_MEMBERSHIP`.) This is detectable via the existing plugin's own tests, not by the substrate.

**Neutral:**

- This decision constrains future plugin authors. Any new scene-like context (channels, forum threads, future social surfaces) that wants focus must also populate `FocusMemberships` via `JoinFocus` before its own focus operations can succeed. This is consistent with how scenes work today and provides a clean extension contract.

## Source

- Spec: `docs/superpowers/specs/2026-05-21-scenes-phase-5-focus-model-and-multi-connection-visibility-design.md` §3 D4, §6.1, §10 INV-P5-1
- Companion ADRs: `holomush-kuf8` (PluginHostService extension), `holomush-8new` (SessionConnectionMutator)
- Plan: `docs/superpowers/plans/2026-05-21-scenes-phase-5-focus-model-and-multi-connection-visibility.md` Phase D (T14)
