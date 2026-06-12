<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-nbscl; do not edit manually; use `/adr update holomush-nbscl` -->

# Domain-primary carving axis for host capability services

**Date:** 2026-06-12
**Status:** Accepted
**Decision:** holomush-nbscl
**Deciders:** Sean Brandt

## Context

The 23-RPC PluginHostService god-service must be decomposed into per-capability proto services (sub-spec 2 of epic holomush-eykuh). Two structural carving axes were candidates: domain-primary (group by subject domain) and trust-tier (group by blast radius across domains, e.g. a cross-domain DangerousOps service). The choice constrains where every future capability operation lands and what a least-privilege declaration means.

## Decision

Domain-primary is the sole structural carving axis. Trust tier is a policy concern, expressed via sub-spec 4's per-entry `access:` scoping, NOT via service topology. Within a single domain, split into separate services only when a subset has a materially different blast radius AND plausibly independent consumers (world -> world.query/world.mutation; stream -> stream.history/stream.subscription; session -> session/session.admin). A tight get/set pair on one resource (property, settings) stays one service.

## Rationale

- A trust-tier structural axis is cross-domain lumping that recreates the god-service problem in miniature: it answers 'how dangerous' not 'what authority'.
- Domain-primary makes each service a coherent least-privilege unit (`capability: session.admin` = moderation authority over sessions, not a generic dangerous bag).
- Selective mutation-axis splits are intra-domain sub-divisions that preserve the domain boundary while expressing real blast-radius differences.
- Read-vs-write scoping within a single-service domain is encoded as per-entry `access:` in the manifest, deferring to sub-spec 4 without a second service.

## Alternatives Considered

**Trust-tier structural axis (cross-domain dangerous-ops grouping)** — REJECTED. Strength: a single binary safe/dangerous grant. Weakness: cross-domain lumping (focus-writes + disconnects + settings-writes share no domain) makes the capability boundary meaningless as an authority description and recreates the god-service at smaller scale; a new high-blast op must pick a tier, not a domain.

## Consequences

Positive: each capability service describes a coherent authority domain; new RPCs have an unambiguous home; trust-tier policy upgrades (sub-spec 4) need no topology change; the vocabulary stays auditable as one host.v1 namespace. Negative: split/no-split needs a documented heuristic (materially-different-blast-radius + independent-consumers) rather than a mechanical rule; three split domains yield more than 14 one-per-domain services. Neutral: the splitting principle is codified in the spec and INV-PLUGIN-47.
