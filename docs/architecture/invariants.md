<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# HoloMUSH Invariant Registry

Canonical registry of all named system invariants. Paired with
`invariants.yaml` (machine-readable source of truth). The meta-test at
`test/meta/invariant_registry_test.go` reads the YAML file directly.

**This document is generated** inside the `BEGIN GENERATED` / `END GENERATED`
regions below: `cmd/inv-render` renders them from `invariants.yaml`. Do not
hand-edit inside those regions — edit the YAML and run `task invariants:render`.
The prose outside the regions is hand-authored. CI runs `inv-render -check`
(generate-and-diff) and fails if the rendered regions drift from the YAML.

## Scope index

<!-- BEGIN GENERATED: scope-index (edit invariants.yaml + run `task invariants:render`) -->

| Scope | Description | Boundary |
|-------|-------------|----------|
| `INV-CRYPTO` | Event payload encryption, DEK lifecycle, key wrapping, decryption delivery, participant sets, AdminReadStream | Cryptographic operations on event payloads. Does NOT include: audit projection (→ INV-EVENTBUS), plugin manifest validation (→ INV-PLUGIN), cluster coordination (→ INV-CLUSTER). Crypto invariants that operate on in-process state (DEK cache, key material, envelope codec) belong here; invariants that govern wire-level coordination between replicas (invalidation pings, probe-and-pill, N-of-N ack contracts) belong under INV-CLUSTER. |
| `INV-PRIVACY` | Stream history temporal floors, scope gating, guest-session bounds, reattach/Idle arrival-timestamp semantics | Privacy-relevant gating on history reads. Does NOT include: ABAC policy evaluation (→ INV-ACCESS), subscribe authorization (→ INV-EVENTBUS). |
| `INV-PRESENCE` | Presence snapshot correctness, field enumeration, client-side dedup, ownership obscuration | Current-state presence queries. Does NOT include: session status lifecycle (→ INV-SESSION). |
| `INV-SCENE` | Scene lifecycle, board queries, content warnings, pose ordering, focus model, publish snapshot/state, IC isolation, history readability | All scene-domain behavior. Cross-cuts multiple Phase specs (P4–P8). |
| `INV-PLUGIN` | Runtime symmetry, manifest validation, hostfunc safety, emit gates, setting isolation, plugin authz | Plugin-system contracts applicable to both Lua and binary runtimes. Does NOT include: plugin crypto wiring (→ INV-CRYPTO). |
| `INV-EVENTBUS` | Subject naming, JetStream consumer config, audit projection, delivery contracts, tier routing, rendering completeness, colon eradication | Event infrastructure. Does NOT include: event payload encryption (→ INV-CRYPTO), history privacy gating (→ INV-PRIVACY). |
| `INV-CLUSTER` | Member identity, heartbeats, cache invalidation (cross-replica coordination path), probe-and-pill, clock independence | Multi-replica coordination. Includes cluster-scoped invalidation contracts (e.g., INV-28/INV-29 N-of-N ack pings, INV-56 Coordinator retry limits, INV-59 cache-invalidation correctness) that govern wire-level behavior between replicas. Does NOT include single-process DEK operations (→ INV-CRYPTO). |
| `INV-ACCESS` | ABAC policy evaluation, attribute provider invariants, seed policy shape, authorization decisions | Access control evaluation. Does NOT include: stream-access gating at gRPC boundary (→ INV-EVENTBUS). |
| `INV-SESSION` | Session status lifecycle, connection attachment, focus membership, idle detection | Session state machine. Does NOT include: presence snapshot (→ INV-PRESENCE). |
| `INV-STORE` | Migration discipline, no-DELETE enforcement, spec compliance scanning | Database invariants. |
| `INV-TELEMETRY` | Logging discipline, trace context, metric naming, sloglint policy | Observability contracts. |
| `INV-BRANDING` | Asset integrity, palette tokens, logo generation | Visual identity invariants. Does NOT include: docs quality (separate concern). |
| `INV-DOCS` | Proto doc comments, doc IA, contributor onboarding surface | Documentation quality invariants. |

<!-- END GENERATED: scope-index -->

A new scope is warranted when at least 3 invariants exist that don't fit an
existing scope's boundary, or when a new major subsystem ships with its own
invariants.

## Invariant tables

<!-- BEGIN GENERATED: invariant-tables (edit invariants.yaml + run `task invariants:render`) -->

_No invariants migrated yet — populated per-scope by the holomush-hz0v4.14 migration._

<!-- END GENERATED: invariant-tables -->
