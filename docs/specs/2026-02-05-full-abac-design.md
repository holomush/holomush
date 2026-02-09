<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Full ABAC Architecture Design

**Status:** Draft
**Date:** 2026-02-05
**Epic:** holomush-5k1 (Epic 7: Full ABAC)
**Task:** holomush-5k1.1

This document has been split into sections for easier navigation and context
management. Each section is a standalone file in the `abac/` directory.

## Sections

| # | File                                                        | Contents                                                                                                                                                           |
| - | ----------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 0 | [Overview](abac/00-overview.md)                             | Overview, Goals, Non-Goals, Glossary, Key Design Decisions, Reserved Prefixes, Architecture, Request Flow, Package Structure                                       |
| 1 | [Core Types](abac/01-core-types.md)                         | AccessPolicyEngine, PolicyCompiler, AccessRequest, Session Resolution, Decision, AttributeBags, Attribute Providers, Core Attribute Schema                         |
| 2 | [Policy DSL](abac/02-policy-dsl.md)                         | Grammar, Grammar Versioning, Type System, Supported Operators, Example Policies                                                                                    |
| 3 | [Property Model](abac/03-property-model.md)                 | Property Attributes, Visibility Levels, Visibility Seed Policies, Parent Location Resolution                                                                       |
| 4 | [Resolution & Evaluation](abac/04-resolution-evaluation.md) | Attribute Resolution Flow, Provider Registration, Schema Registry, Schema Validation, Error Handling, Evaluation Algorithm, Performance Targets, Attribute Caching |
| 5 | [Storage & Audit](abac/05-storage-audit.md)                 | Policy Storage Schema, Cache Invalidation, Audit Log Serialization, Policy Versions, Audit Configuration, Audit Retention, Visibility Defaults                     |
| 6 | [Layers & Commands](abac/06-layers-commands.md)             | Access Control Layers (Metadata/Locks/Policies), Lock Syntax, Lock Token Registry, Lock Compilation, Admin Commands, Policy Management                             |
| 7 | [Migration & Seeds](abac/07-migration-seeds.md)             | Replacing Static Roles, Seed Policies, Bootstrap Sequence, Seed Upgrades, Implementation Sequence, Plugin Capability Migration                                     |
| 8 | [Testing & Appendices](abac/08-testing-appendices.md)       | Testing Strategy, Fuzz Testing, Integration Tests, Known Limitations, Acceptance Criteria, Future Commands, References, Related ADRs                               |

## Quick Links

- [Seed Policies](abac/07-migration-seeds.md#seed-policies) — Default permission model
- [Evaluation Algorithm](abac/04-resolution-evaluation.md#evaluation-algorithm) — Step-by-step evaluation flow
- [Grammar](abac/02-policy-dsl.md#grammar) — DSL grammar specification
- [Core Attribute Schema](abac/01-core-types.md#core-attribute-schema) — Attribute definitions per provider
- [Performance Targets](abac/04-resolution-evaluation.md#performance-targets) — Latency and throughput targets
- [Bootstrap Sequence](abac/07-migration-seeds.md#bootstrap-sequence) — First-startup policy seeding
- [Lock Syntax](abac/06-layers-commands.md#lock-syntax) — Player lock expression language
- [Acceptance Criteria](abac/08-testing-appendices.md#acceptance-criteria) — Completion checklist
