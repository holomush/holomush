<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-nthq6; do not edit manually; use `/adr update holomush-nthq6` -->

# Derive world-read subject host-side; no subject field on wire

**Date:** 2026-05-30
**Status:** Accepted
**Decision:** holomush-nthq6
**Deciders:** Sean Brandt

## Context

Plugin world-read RPCs on `WorldService` accepted a caller-supplied `subject_id`, allowing binary plugins to name any character as the acting subject — a confused-deputy / privilege-escalation surface identical to the EmitEvent actor-forgery class. ADR holomush-qeypl established the host-derived, forgery-free subject pattern for `Evaluate`; the same structural fix is needed for all four world-read host functions (`QueryLocation`, `QueryCharacter`, `QueryLocationCharacters`, `QueryObject`). The new `PluginHostService` Query* RPCs carry only the target entity id, with no subject field; the host recovers the actor from the dispatch token (binary) or the VM context (Lua) and maps it through `pluginauthz.ActorSubject`.

## Decision

Every `PluginHostServiceQuery*Request` message carries no subject field. The host derives the ABAC subject from the authenticated actor bound to the dispatch context, via the single shared `pluginauthz.ActorSubject`, applying on-behalf-of (OBO) semantics: a character actor resolves to `character:<id>` (the acting character's authority), a plugin actor resolves to `plugin:<name>` (the self-fallback for plugin-initiated reads). This extends ADR holomush-qeypl from the `Evaluate` surface to all four world-read RPCs, for both the binary and Lua runtimes.

## Rationale

- A proto-level omission is a structural guarantee — INV-1 is a descriptor scan, not a runtime assertion; a future refactor cannot thread a subject through without changing the proto and triggering review.
- Mirrors the established anti-spoof posture: holomush-qeypl (Evaluate), holomush-ec22.1 (EmitEvent token auth).
- OBO semantics (the acting character's authority, not the plugin's broad authority) prevent confused-deputy reads where a plugin serving character A could read character B's world view.
- Fail-closed on a missing/zero actor (yields `""` subject → world read denied) prevents unauthenticated reads.
- Both runtimes converge on the same `ActorSubject` call and the same `world.Service.checkAccess` chokepoint, so INV-2 (shared derivation) and INV-5 (cross-runtime parity) are verifiable.

## Alternatives Considered

### Host-derived subject via dispatch context (chosen)

- **Strengths:** Structurally forgery-free at the proto level (INV-1 is a descriptor scan, not a runtime check); OBO semantics fall out naturally from the actor-stamping already done at dispatch; consistent with holomush-qeypl and EmitEvent; a single shared `ActorSubject` function prevents runtime divergence.
- **Weaknesses:** Plugins cannot query world state on behalf of an arbitrary subject in v1; plugin-initiated reads (no acting character) resolve to `plugin:<name>`, which may be broader than the minimal-privilege ideal.

### Subject field on request (plugin supplies subject)

- **Strengths:** Allows "what can character X see?" queries from a plugin acting in a broader context.
- **Weaknesses:** The host cannot distinguish forged from legitimate values over the RPC; enables privilege escalation and cross-actor probing. Rejected for the same reason as in holomush-qeypl.

### Interceptor that overwrites a plugin-supplied subject

- **Strengths:** Keeps the existing `WorldService` gRPC server; less proto churn.
- **Weaknesses:** An interceptor still requires the proto field to exist, making the forgery-free guarantee a runtime policy rather than a structural one; does not close the registry-injection surface (see the companion decision).

## Consequences

**Positive:**

- Subject spoofing is structurally impossible at the proto level for all world reads.
- OBO scoping: a plugin serving a command reads only what the acting character may read.
- Consistent host-side anti-spoof posture across Evaluate, EmitEvent, and world reads.
- A single shared subject-derivation path (`pluginauthz.ActorSubject`) cannot diverge between runtimes.

**Negative:**

- Plugins cannot query world state on behalf of a hypothetical/other subject in v1.
- Plugin-initiated world reads (no acting character) resolve to `plugin:<name>`, which may be broader than the minimal-privilege ideal.

**Neutral:**

- The actor-recovery mechanism differs by runtime (dispatch token vs VM context) — a runtime-specific concern, not a policy difference.
- The `WorldQuerierAdapter` hard-coded subject is retired for reads; its type may be removed or reduced.
