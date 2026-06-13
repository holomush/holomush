<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-u1sdq; do not edit manually; use `/adr update holomush-u1sdq` -->

# access: and scope: are valid only on capability entries, not service entries

**Date:** 2026-06-13
**Status:** Accepted
**Decision:** holomush-u1sdq
**Deciders:** Sean Brandt

## Context

The per-entry least-privilege parameters access: (operation class) and scope: (instance) needed a validity boundary. Manifests declare both host capabilities (capability:) and provider-plugin services (service:). The host owns the capability servers and can enforce host-authored scope; it owns neither the provider's method classification nor its resource model for plugin services.

## Decision

access: and scope: are valid only on capability: requires entries. Either parameter on a service: entry is a hard manifest error at load. The host cannot honor a scope/access promise on a provider-owned service. Binds INV-PLUGIN-53.

## Rationale

- The host can enforce what it promises: host.v1 servers are in-tree and host-owned, so the typed extractor and interceptor always run.
- Rejecting these params on service: entries at load prevents the 'declared but not enforced' smell — the worst outcome for a least-privilege feature.
- Provider-side scope is still fully supported via the provider's own policies keyed on the DispatchContext the host propagates across the broker.
- The growth path for a service needing host-enforced scope is promotion to a host capability.

## Alternatives Considered

- **scope: legal on service: with provider-side enforcement:** uniform syntax, but one keyword means two strengths (host-guaranteed vs advisory) — the privilege gradient the symmetry mandate abolishes. Rejected.
- **scope: via operator policy only, no keyword:** simpler schema, but removes the instance ceiling from the plugin's own auditable contract (invisible at plugin info). Rejected.
- **Capability-only, hard error on service: (chosen):** host enforces only what it owns; no ambiguity; provider scope still supported via its own policies.

## Consequences

Positive: no ambiguity between guaranteed and advisory scope; hard errors surface at load not silently at runtime; the contract auditable at plugin info is also the enforced contract. Negative: a provider service with real cross-plugin scope needs must be promoted to a host capability; authors may be surprised scope: on a service entry errors. Neutral: provider-side scope via host.Evaluate + DispatchContext is unaffected.
