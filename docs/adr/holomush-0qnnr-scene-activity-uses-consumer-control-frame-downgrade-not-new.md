<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-0qnnr; do not edit manually; use `/adr update holomush-0qnnr` -->

# scene_activity Uses In-Consumer Control-Frame Downgrade, Not a New Stream

**Date:** 2026-06-07
**Status:** Accepted
**Decision:** holomush-0qnnr
**Deciders:** Sean Brandt (seanb4t)

## Context

The workspace needs per-scene unread badges for member connections not focused on a scene. Options included a dedicated notification stream, persisted badge state, or a new EventBus subject.

## Decision

Every member connection's Subscribe consumer carries its session's FocusMemberships scene subjects; at delivery, a scene event for a connection NOT focused on that scene is downgraded to ControlFrame{CONTROL_SIGNAL_SCENE_ACTIVITY, scene_id} — no payload, no decryption — instead of an EventFrame. WebListMyScenes snapshots provide initial sync and reconnect catch-up.

## Rationale

- Privacy-clean: the ping carries only scene_id; encrypted payloads are never touched on the badge path.
- INV-SCENE-62 holds by construction: FocusMemberships ⊆ scene_participants, so non-member consumers never carry the subject.
- No plugin manifest/emit changes; nothing persisted — missed pings self-heal via snapshot catch-up (the platform's established stream+snapshot pattern).

## Alternatives Considered

**Dedicated notification stream / new EventBus subject** — rejected: new streaming surface, subject registration, manifest changes, and badge persistence that v1 scope had already deferred.

**In-consumer downgrade (chosen)** — reuses the ControlFrame extension point; works wherever a Subscribe stream is open.

## Consequences

- Positive: zero new streaming trust surface; privacy-clean; self-healing.
- Negative: badges are per-workspace-session in v1 (persisted read markers are a follow-up bead); ControlFrame gains a scene_id field that the gateway's forwardFrame must propagate.
- Neutral: clients de-duplicate — focused-scene events never also count via the notification path.

Spec: 2026-06-07-web-portal-scenes-design.md §3 D7, §9 V5. Bead: holomush-5rh.8.
