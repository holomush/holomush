<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-40ssh; do not edit manually; use `/adr update holomush-40ssh` -->

# Atomic (big-bang) plugin-capability cutover over phased allowlist rollout

**Date:** 2026-06-14
**Status:** Accepted
**Decision:** holomush-40ssh
**Deciders:** Sean Brandt

## Context

The host-brokered capability path has been gated behind a `WithHostCapBridge` opt-in allowlist (default empty) since sub-specs 1–4 landed, keeping the legacy `hostfunc.Register` unconditional injection alongside the new path. Two migration strategies were on the table: remove the allowlist in one atomic change (all plugins flip simultaneously) or extend the allowlist mechanism to let plugins opt in incrementally.

## Decision

The `WithHostCapBridge` allowlist is removed entirely; the declaration-gated brokered path becomes the sole host-capability-consumption path for all runtimes in one atomic change. All backing-server preconditions (eykuh.4.1 WorldMutationService, eykuh.4.2 SessionAdminService, eykuh.4.5 actor-identity interceptor) must be satisfied before the flip.

## Rationale

- The coexistence surface exists only to bridge implementation lag, not for permanent multi-path policy; atomic removal closes that surface completely.
- Fail-fast boot (INV-PLUGIN-43, already live) makes the atomic approach safe: a missed declaration is a loud boot failure, not a silent degradation.
- The whole-system load test (`WithInTreePlugins`) serves as the gate, so the risk of an incomplete manifest audit surfaces before the flip lands.

## Alternatives Considered

- **Big-bang atomic cutover (chosen):** Eliminates the coexistence surface in one change; no plugin can linger on the unenforced legacy path; manifest-audit completeness becomes a hard gate. Unforgiving — any missed declaration is a hard boot failure.
- **Phased per-plugin opt-in via allowlist (rejected):** Lower per-deploy blast radius, but prolongs dual-path complexity, defers binding INV-PLUGIN-45 until all plugins migrate, and makes the allowlist itself drift surface.

## Consequences

- Positive: enforcement is structural (no plugin can use an undeclared capability post-cutover); INV-PLUGIN-45 bindable in the same change; dual-path complexity removed permanently.
- Negative: any missed manifest declaration blocks the entire server from booting; all preconditions must land before the flip.
- Neutral: the allowlist mechanism is deleted, not left disabled.
