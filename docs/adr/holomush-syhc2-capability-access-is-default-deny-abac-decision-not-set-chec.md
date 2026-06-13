<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-syhc2; do not edit manually; use `/adr update holomush-syhc2` -->

# Capability access is a default-deny ABAC decision, not a set check

**Date:** 2026-06-13
**Status:** Accepted
**Decision:** holomush-syhc2
**Deciders:** Sean Brandt

## Context

After sub-specs 1-3, a plugin's manifest declaration was both necessary and sufficient to reach a host capability — the consumption path had no policy decision. Operators had no lever to narrow or deny a declared capability without editing the plugin manifest.

## Decision

Capability/service consumption is authorized by a default-deny ABAC decision keyed on the host-stamped plugin:<name> subject. The existing pluginauthz machinery (subject derivation, engine, single-audit-event) is reused via a sibling capability-access entitlement path that substitutes the owned-type predicate with a declaration+resolver-satisfied check. Binds INV-PLUGIN-50.

## Rationale

- Declaration stays necessary (resolver gates reach) but is no longer sufficient — operator policy may deny, enabling defense in depth.
- Reusing pluginauthz preserves INV-PLUGIN-26 (one shared decision point, both runtimes); no second engine.
- A sibling entitlement path avoids breaking the EVALUATE_UNENTITLED_TYPE owned-type predicate, which would deny host-capability resources (location/kv/session) that are not plugin-owned.
- Static checks (declaration re-check + access: class) run before the policy Evaluate (cheap-before-expensive).

## Alternatives Considered

- **Declaration-as-sufficient (status quo):** zero overhead, but no operator control and any vulnerability in a declared capability is fully exploitable. Rejected.
- **Default-deny capability-access entitlement (chosen):** operators can deny by policy; reuses engine/subject/audit; declaration is the floor, policy the ceiling. Note: reusing pluginauthz.Evaluate verbatim was rejected because its owned-type gate denies all host-capability resources — hence the sibling path.

## Consequences

Positive: operators gain a compensating control without touching the manifest; all denials audited (INV-PLUGIN-25); default policy permits declared caps so existing plugins are unaffected. Negative: per-call Evaluate adds latency (mitigated by static short-circuit); the capability-access entitlement predicate must stay in sync with resolver satisfaction. Neutral: declaration remains the necessary floor.
