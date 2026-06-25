<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-f9z8f; do not edit manually; use `/adr update holomush-f9z8f` -->

# Self-gate ABAC at SceneService handlers, not the facade

**Date:** 2026-06-25
**Status:** Accepted
**Decision:** holomush-f9z8f
**Deciders:** Sean Brandt

## Context

The eight SceneService lifecycle/membership RPC handlers (EndScene, PauseScene, ResumeScene, UpdateScene, InviteToScene, KickFromScene, TransferOwnership, LeaveScene) had no self-enforced authorization — they relied on the telnet command-dispatch wrapper's ABAC gate. Adding a web facade (SceneAccessService) that calls these RPCs directly created an authorization gap: web-originated calls would bypass the command-wrapper gate entirely, letting any non-guest player mutate any scene. The pivotal design decision was where to place the per-verb ABAC gate on the RPC path.

## Decision

ABAC authorization for the eight SceneService lifecycle/membership handlers MUST be self-enforced at the handler level (INV-SCENE-65), evaluating the per-verb action on `scene:<id>` with the subject taken from the dispatch-actor context, so authorization holds for every caller regardless of which facade or command wrapper sits above it. Publish handlers are excluded (INV-SCENE-33 forbids the ABAC engine on the participant-gated publication read path).

## Rationale

- The handler is the one common chokepoint both the telnet command path and the web facade traverse; it is the only location that holds for all current and future callers.
- Action strings must originate in the plugin's Cedar policy registrations (the single source of truth); a facade gate would duplicate them host-side, creating a drift surface.
- The plugin-runtime-symmetry rule ("place the gate at the common path") and the existing WatchScene precedent (service.go self-gates `spectate`) both support handler-level enforcement.
- The double-gate on the telnet path is idempotent and safe: identical (subject, action, resource) yields an identical decision.

## Alternatives Considered

- **Self-gate at the SceneService handler — the common chokepoint (chosen):** the evaluator call lives at the one code path both surfaces traverse; action strings stay in the plugin beside the policy registrations; defense-in-depth for any future direct caller; mirrors WatchScene. Cost: the telnet path double-evaluates (harmless), and converging to handler-only gating (removing the command wrapper's gate) is a deferred follow-up.
- **Gate inside the SceneAccessService facade, before dispatch (rejected):** keeps plugin handlers free of host-side ABAC machinery and mirrors the facade's existing identity-resolution role, but duplicates the action vocabulary host-side (drift risk against the plugin's policy registrations) and leaves the SceneService handlers permanently ungated for any future direct caller — a weaker symmetry and defense-in-depth story.
- **Hybrid — facade-gate now, push to the handler later (rejected):** ships sooner but knowingly carries the duplicated gate and the latent ungated-handler footgun until a follow-up lands.

## Consequences

- Positive: authorization holds for every caller of these handlers — telnet command path, web facade, and any future direct caller; action strings remain in the plugin alongside the policy registrations; defense-in-depth even if a facade omits its own gate.
- Negative: the telnet path evaluates ABAC twice per verb (command wrapper + handler), adding one evaluator round-trip per telnet lifecycle command; converging to handler-only gating requires removing the command wrapper's gate as a deferred follow-up.
- Neutral: gate-with-exposure sequencing (spec §5.4) — each handler self-gate MUST ship in the same slice as its web facade exposure, never expose-then-harden; registered as INV-SCENE-65 (binding: pending) in the invariant registry.
