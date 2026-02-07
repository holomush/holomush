<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Architecture Decision Records (ADRs)

This directory contains Architecture Decision Records (ADRs) documenting significant design
decisions made during HoloMUSH development. Each ADR captures the context, options
considered, decision made, and consequences of architectural choices.

## Purpose

ADRs provide:

- **Historical context** — Why was this choice made?
- **Trade-off analysis** — What alternatives were considered and why were they rejected?
- **Consequences** — What are the implications of this decision?
- **Rationale** — What reasoning led to this conclusion?

ADRs are immutable once accepted. If a decision is reversed, a new ADR supersedes the old
one rather than modifying the original.

## Index

| ADR                                                                              | Title                                                  | Date       | Status            |
| -------------------------------------------------------------------------------- | ------------------------------------------------------ | ---------- | ----------------- |
| [0001](0001-opaque-session-tokens.md)                                            | Use Opaque Session Tokens Instead of Signed JWTs      | 2026-02-02 | Accepted          |
| [0002](0002-argon2id-password-hashing.md)                                        | Argon2id Password Hashing                              | 2026-02-02 | Accepted          |
| [0003](0003-player-character-auth-model.md)                                      | Player-Character Authentication Model                  | 2026-02-02 | Accepted          |
| [0004](0004-timing-attack-resistant-auth.md)                                     | Timing-Attack Resistant Authentication                 | 2026-02-02 | Accepted          |
| [0005](0005-command-state-management.md)                                         | Command State Management for Multi-Turn Interactions   | 2026-02-04 | Accepted          |
| [0006](0006-unified-command-registry.md)                                         | Unified Command Registry                               | 2026-02-04 | Accepted          |
| [0007](0007-command-security-model.md)                                           | Command Security Model                                 | 2026-02-04 | Superseded by 0014|
| [0008](0008-command-conflict-resolution.md)                                      | Command Conflict Resolution                            | 2026-02-04 | Accepted          |
| [0009](0009-custom-go-native-abac-engine.md)                                     | Custom Go-Native ABAC Engine                           | 2026-02-05 | Accepted          |
| [0010](0010-cedar-aligned-fail-safe-type-semantics.md)                           | Cedar-Aligned Fail-Safe Type Semantics                 | 2026-02-05 | Accepted          |
| [0011](0011-deny-overrides-without-priority.md)                                  | Deny-Overrides Without Priority Ordering               | 2026-02-05 | Accepted          |
| [0012](0012-eager-attribute-resolution.md)                                       | Eager Attribute Resolution with Per-Request Caching    | 2026-02-05 | Accepted          |
| [0013](0013-properties-as-first-class-entities.md)                               | Properties as First-Class World Model Entities         | 2026-02-05 | Accepted          |
| [0014](0014-direct-static-access-control-replacement.md)                         | Direct StaticAccessControl Replacement                 | 2026-02-05 | Accepted          |
| [0015](0015-three-layer-player-access-control.md)                                | Three-Layer Player Access Control                      | 2026-02-05 | Accepted          |
| [0016](0016-listen-notify-policy-cache-invalidation.md)                          | PostgreSQL LISTEN/NOTIFY for Policy Cache Invalidation | 2026-02-05 | Accepted          |

## Format Evolution

ADRs in this repository use two valid formats:

### Original Format (ADRs 0001-0008)

- **Context** — Problem statement and background
- **Decision** — The choice made
- **Rationale** — Why this decision was made
- **Consequences** — Positive, negative, and neutral outcomes
- **Alternatives Considered** — Separate section for rejected options
- **References** — Related documents and implementations

### Current Format (ADRs 0009+)

- **Context** — Problem statement with embedded **Options Considered** subsection
- **Decision** — The choice made
- **Rationale** — Why this decision was made
- **Consequences** — Positive, negative, and neutral outcomes (may include testing requirements)
- **References** — Related documents and implementations

Both formats are valid. The evolution reflects a preference for more compact ADRs with
options embedded in context rather than separated. New ADRs SHOULD use the current format.

## Template for New ADRs

```markdown
# ADR NNNN: Title

**Date:** YYYY-MM-DD
**Status:** Proposed | Accepted | Deprecated | Superseded by ADR-XXXX
**Deciders:** HoloMUSH Contributors

## Context

[Describe the problem, background, and relevant constraints]

### Options Considered

**Option A: [Name]**

[Description]

| Aspect     | Assessment                    |
| ---------- | ----------------------------- |
| Strengths  | [What this option does well]  |
| Weaknesses | [What this option lacks]      |

**Option B: [Name]**

[Description]

| Aspect     | Assessment                    |
| ---------- | ----------------------------- |
| Strengths  | [What this option does well]  |
| Weaknesses | [What this option lacks]      |

## Decision

**Option [X]: [Chosen option name].**

[Brief statement of the decision and key details]

## Rationale

[Detailed explanation of WHY this decision was made]

- **Key factor 1:** [Explanation]
- **Key factor 2:** [Explanation]

## Consequences

**Positive:**

- [Benefit 1]
- [Benefit 2]

**Negative:**

- [Trade-off 1]
- [Trade-off 2]

**Neutral:**

- [Neither good nor bad implication]

## References

- [Related spec](../specs/filename.md)
- [Related design decision](../specs/design-decisions.md#anchor)
- [External reference](https://example.com)
```

## Writing Guidelines

| Guideline                 | Description                                                                                  |
| ------------------------- | -------------------------------------------------------------------------------------------- |
| **Immutability**          | ADRs are permanent records — do not edit accepted ADRs to change decisions                   |
| **Supersession**          | To reverse a decision, create a new ADR and mark the old one as "Superseded by ADR-XXXX"     |
| **RFC2119 keywords**      | Use MUST/SHOULD/MAY in consequences when describing implementation requirements              |
| **Comprehensive options** | Document ALL options considered, not just the chosen one                                     |
| **Trade-off clarity**     | Consequences should honestly capture both benefits and costs                                 |
| **Future-proof**          | Assume readers in 5 years won't have context — explain everything                            |

## References

- [Michael Nygard's ADR template](https://github.com/joelparkerhenderson/architecture-decision-record)
- [ADR Tools GitHub](https://github.com/npryce/adr-tools)
- [RFC 2119: Key words for RFCs](https://www.ietf.org/rfc/rfc2119.txt)
