<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Architecture Decision Records (ADRs)

This directory contains Architecture Decision Records (ADRs) documenting
significant design decisions made during HoloMUSH development. Each ADR
captures the context, options considered, decision made, and consequences
of architectural choices.

ADRs are immutable once accepted. If a decision is reversed, a new ADR
supersedes the old one; the bd decision record gains a `--type supersedes`
edge and the file's `**Status:**` reflects the supersession.

## Index

| Title | Date | Status | bd decision |
|-------|------|--------|-------------|
| [AdminReadStream Bypasses HistoryReader/Dispatcher](holomush-8f2x-adminreadstream-bypasses-historyreaderdispatcher.md) | 2026-05-12 | Accepted | `holomush-8f2x` |
| [Custom Go-Native ABAC Engine](holomush-kokk-custom-go-native-abac-engine.md) | 2026-02-05 | Accepted | `holomush-kokk` |
| [Cedar-Aligned Fail-Safe Type Semantics](holomush-iv43-cedar-aligned-fail-safe-type-semantics.md) | 2026-02-05 | Accepted | `holomush-iv43` |
| [Deny-Overrides Without Priority Ordering](holomush-501i-deny-overrides-without-priority-ordering.md) | 2026-02-05 | Accepted | `holomush-501i` |
| [Eager Attribute Resolution with Per-Request Caching](holomush-fvn5-eager-attribute-resolution-per-request-caching.md) | 2026-02-05 | Accepted | `holomush-fvn5` |
| [Properties as First-Class World Model Entities](holomush-xx3e-properties-as-first-class-world-model-entities.md) | 2026-02-05 | Accepted | `holomush-xx3e` |
| [Direct StaticAccessControl Replacement](holomush-7kvy-direct-staticaccesscontrol-replacement.md) | 2026-02-05 | Accepted | `holomush-7kvy` |
| [Three-Layer Player Access Control](holomush-0tq6-three-layer-player-access-control.md) | 2026-02-05 | Accepted | `holomush-0tq6` |
| [PostgreSQL LISTEN/NOTIFY for Policy Cache Invalidation](holomush-5z2y-postgresql-listennotify-policy-cache-invalidation.md) | 2026-02-05 | Accepted | `holomush-5z2y` |
| [Use Opaque Session Tokens Instead of Signed JWTs](holomush-ydti-use-opaque-session-tokens-instead-signed-jwts.md) | 2026-02-02 | Accepted | `holomush-ydti` |
| [Argon2id Password Hashing](holomush-4x7x-argon2id-password-hashing.md) | 2026-02-02 | Accepted | `holomush-4x7x` |
| [Player-Character Authentication Model](holomush-ex40-player-character-authentication-model.md) | 2026-02-02 | Accepted | `holomush-ex40` |
| [Timing-Attack Resistant Authentication](holomush-8qbl-timing-attack-resistant-authentication.md) | 2026-02-02 | Accepted | `holomush-8qbl` |
| [Command State Management for Multi-Turn Interactions](holomush-8j8q-command-state-management-multi-turn-interactions.md) | 2026-02-02 | Proposed (Deferred to post-v1) | `holomush-8j8q` |
| [Unified Command Registry](holomush-5nu7-unified-command-registry.md) | 2026-02-02 | Accepted | `holomush-5nu7` |
| [Command Security Model](holomush-vi5e-command-security-model.md) | 2026-02-02 | Superseded by [ADR 0014](0014-direct-static-access-control-replacement.md) | `holomush-vi5e` |
| [Command Conflict Resolution](holomush-ogb4-command-conflict-resolution.md) | 2026-02-02 | Accepted | `holomush-ogb4` |

<!-- BEGIN MIGRATION MAP -->

## Migration map (2026-05-14)

The legacy `NNNN-<slug>.md` numbering was retired in favor of
bd-decision IDs. Stubs at the old paths preserve external references.

| Legacy | bd decision | Current file |
|--------|-------------|--------------|
| ADR 0001 | `holomush-ydti` | [holomush-ydti-use-opaque-session-tokens-instead-signed-jwts.md](holomush-ydti-use-opaque-session-tokens-instead-signed-jwts.md) |
| ADR 0002 | `holomush-4x7x` | [holomush-4x7x-argon2id-password-hashing.md](holomush-4x7x-argon2id-password-hashing.md) |
| ADR 0003 | `holomush-ex40` | [holomush-ex40-player-character-authentication-model.md](holomush-ex40-player-character-authentication-model.md) |
| ADR 0004 | `holomush-8qbl` | [holomush-8qbl-timing-attack-resistant-authentication.md](holomush-8qbl-timing-attack-resistant-authentication.md) |
| ADR 0005 | `holomush-8j8q` | [holomush-8j8q-command-state-management-multi-turn-interactions.md](holomush-8j8q-command-state-management-multi-turn-interactions.md) |
| ADR 0006 | `holomush-5nu7` | [holomush-5nu7-unified-command-registry.md](holomush-5nu7-unified-command-registry.md) |
| ADR 0007 | `holomush-vi5e` | [holomush-vi5e-command-security-model.md](holomush-vi5e-command-security-model.md) |
| ADR 0008 | `holomush-ogb4` | [holomush-ogb4-command-conflict-resolution.md](holomush-ogb4-command-conflict-resolution.md) |
| ADR 0009 | `holomush-kokk` | [holomush-kokk-custom-go-native-abac-engine.md](holomush-kokk-custom-go-native-abac-engine.md) |
| ADR 0010 | `holomush-iv43` | [holomush-iv43-cedar-aligned-fail-safe-type-semantics.md](holomush-iv43-cedar-aligned-fail-safe-type-semantics.md) |
| ADR 0011 | `holomush-501i` | [holomush-501i-deny-overrides-without-priority-ordering.md](holomush-501i-deny-overrides-without-priority-ordering.md) |
| ADR 0012 | `holomush-fvn5` | [holomush-fvn5-eager-attribute-resolution-per-request-caching.md](holomush-fvn5-eager-attribute-resolution-per-request-caching.md) |
| ADR 0013 | `holomush-xx3e` | [holomush-xx3e-properties-as-first-class-world-model-entities.md](holomush-xx3e-properties-as-first-class-world-model-entities.md) |
| ADR 0014 | `holomush-7kvy` | [holomush-7kvy-direct-staticaccesscontrol-replacement.md](holomush-7kvy-direct-staticaccesscontrol-replacement.md) |
| ADR 0015 | `holomush-0tq6` | [holomush-0tq6-three-layer-player-access-control.md](holomush-0tq6-three-layer-player-access-control.md) |
| ADR 0016 | `holomush-5z2y` | [holomush-5z2y-postgresql-listennotify-policy-cache-invalidation.md](holomush-5z2y-postgresql-listennotify-policy-cache-invalidation.md) |
| ADR 0017 | `holomush-8f2x` | [holomush-8f2x-adminreadstream-bypasses-historyreaderdispatcher.md](holomush-8f2x-adminreadstream-bypasses-historyreaderdispatcher.md) |

<!-- END MIGRATION MAP -->

## Format

See `docs/superpowers/specs/2026-05-13-adr-capture-skill-design.md`
§"ADR format (unified)" for the canonical template. All ADRs use one
format: Context, Decision, Rationale, Alternatives Considered,
Consequences, References.

## Template

New ADRs are written by the `/capture-adrs` skill, which renders from
the spec's format definition. To write one manually, follow the same
shape and use `bd create -t decision --validate` to file the record.

## Writing guidelines

| Guideline                 | Description                                                                                              |
| ------------------------- | -------------------------------------------------------------------------------------------------------- |
| **Immutability**          | ADRs are permanent records — do not edit accepted ADRs to change decisions                               |
| **Supersession**          | To reverse a decision, create a new ADR and mark the old one as "Superseded by `<bd-id>`"                |
| **RFC2119 keywords**      | Use MUST/SHOULD/MAY in consequences when describing implementation requirements                          |
| **Comprehensive options** | Document ALL options considered, not just the chosen one                                                 |
| **Trade-off clarity**     | Consequences should honestly capture both benefits and costs                                             |
| **Future-proof**          | Assume readers in 5 years won't have context — explain everything                                        |

## References

- [Michael Nygard's ADR template](https://github.com/joelparkerhenderson/architecture-decision-record)
- [ADR Tools GitHub](https://github.com/npryce/adr-tools)
- [RFC 2119: Key words for RFCs](https://www.ietf.org/rfc/rfc2119.txt)
