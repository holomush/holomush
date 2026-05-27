<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Derive Evaluate subject host-side; no subject field on wire

**Date:** 2026-05-25
**Status:** Accepted
**Decision:** holomush-qeypl
**Deciders:** Sean Brandt

## Context

An authorization-evaluation RPC exposed to plugin code is a subject-spoofing surface: a plugin could claim to evaluate as an arbitrary character rather than the authenticated actor, escalating privilege or probing other actors' permissions. The host already overwrites plugin-supplied identity fields in `dispatcher.go::extractAuditHints` ("the plugin cannot spoof these fields"), but that stance had not yet been extended to a plugin-callable evaluation surface.

## Decision

`EvaluateRequest` carries **no subject field** by construction. The host derives the subject from the authenticated actor already bound to the call: for binary plugins from the per-dispatch token (the same mechanism `EmitEvent` uses — `tokenStore.Lookup`, not plugin-supplied actor metadata); for Lua plugins from the dispatch `context.Context` (`core.ActorFromContext`). If no authenticated actor is bound, `Evaluate` fails closed (deny + error). Evaluating on behalf of a subject other than the current actor is out of scope for v1.

## Rationale

- A proto-level omission is a structural guarantee — a future refactor cannot accidentally thread a subject through without changing the proto and triggering review (INV-1 is a meta-test over the proto descriptor, not a runtime assertion).
- Mirrors the existing host-side anti-spoof posture (`EmitEvent` token authentication, `extractAuditHints` field overwrite).
- Fail-closed on missing actor prevents unauthenticated evaluation, which would be a silent hole.
- "Evaluate as another subject" is a meaningfully higher-risk capability that warrants its own design.

## Alternatives Considered

- **Subject field on `EvaluateRequest` (plugin supplies subject).** Rejected — a plugin can forge any subject over the RPC; the host cannot distinguish legitimate from forged values. Enables privilege escalation and cross-actor probing. The (legitimate) "can character X do Y?" use case is deferred to a future higher-risk design.

## Consequences

- **Positive:** subject spoofing is structurally impossible at the proto level; INV-1 is verifiable by a descriptor scan; consistent with existing anti-spoof posture.
- **Negative:** plugins cannot evaluate hypothetical/other subjects in v1.
- **Neutral:** both runtimes reach the same host derivation logic; only the actor-recovery mechanism differs (token vs ctx) — a runtime-specific concern, not a policy/trust difference.

## References

- Spec: `docs/superpowers/specs/2026-05-25-plugin-host-evaluate-design.md` §2
- Design bead: holomush-8kkv5
- Related: `internal/plugin/goplugin/host_service.go::EmitEvent` (token→actor pattern)
