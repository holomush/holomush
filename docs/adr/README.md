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
| [Use topic-tab navigation over a single grouped sidebar](holomush-q924m-use-topic-tab-navigation-over-a-single-grouped-sidebar.md) | 2026-05-28 | Accepted | `holomush-q924m` |
| [Organize docs audience-first with Diátaxis modes within](holomush-md3k4-organize-docs-audience-first-di-taxis-modes-within.md) | 2026-05-28 | Accepted | `holomush-md3k4` |
| [Use autogenerate sidebar over explicit entries](holomush-38kmt-use-autogenerate-sidebar-over-explicit-entries.md) | 2026-05-28 | Accepted | `holomush-38kmt` |
| [Adopt Astro Starlight as the docs site platform](holomush-145ko-adopt-astro-starlight-as-docs-site-platform.md) | 2026-05-27 | Accepted | `holomush-145ko` |
| [Render Mermaid diagrams client-side via Starlight plugin](holomush-xneg2-render-mermaid-diagrams-client-side-via-starlight-plugin.md) | 2026-05-27 | Accepted | `holomush-xneg2` |
| [Use bun as the docs-site package manager](holomush-qf2oo-use-bun-as-docs-site-package-manager.md) | 2026-05-27 | Accepted | `holomush-qf2oo` |
| [integration/E2E are CI-authoritative-and-required, local-optional](holomush-5k6au-integration-e2e-are-ci-authoritative-and-required-local-opti.md) | 2026-05-26 | Accepted | `holomush-5k6au` |
| [quarantine flaky specs via governed registry, not deletion](holomush-5eqiv-quarantine-flaky-specs-via-governed-registry-not-deletion.md) | 2026-05-26 | Accepted | `holomush-5eqiv` |
| [Separate authorization (WHO) from business-state validity (WHEN) in scene policies](holomush-sqpnv-authz-who-vs-business-state-when-scene-policies.md) | 2026-05-25 | Accepted | `holomush-sqpnv` |
| [Add host Evaluate RPC for per-action plugin authorization](holomush-dttdj-host-evaluate-rpc-per-action-plugin-authz.md) | 2026-05-25 | Accepted | `holomush-dttdj` |
| [Derive Evaluate subject host-side; no subject field on wire](holomush-qeypl-host-derived-evaluate-subject.md) | 2026-05-25 | Accepted | `holomush-qeypl` |
| [Scope plugin Evaluate entitlement to owned resource types](holomush-61rdl-evaluate-entitlement-owned-resource-types.md) | 2026-05-25 | Accepted | `holomush-61rdl` |
| [Make plugin authorization gates structural via gated subcommand dispatcher](holomush-9l9pu-structural-gated-subcommand-dispatcher.md) | 2026-05-25 | Accepted | `holomush-9l9pu` |
| [Drop in-memory session.Store fake; test against real Postgres](holomush-bozv-drop-session-memstore-test-against-postgres.md) | 2026-05-23 | Accepted | `holomush-bozv` |
| [Denormalize Pose-Order Metadata Against scene_log Source of Truth](holomush-r4th-denormalize-pose-order-metadata.md) | 2026-05-19 | Accepted | `holomush-r4th` |
| [Migrate Scene Subjects to NATS Dot-Style Atomically With Plugin Emit Code](holomush-s9nu-scene-subject-atomic-migration.md) | 2026-05-19 | Accepted | `holomush-s9nu` |
| [Classify All Scene Content Events as sensitivity:always, Including OOC](holomush-sb3n-scene-content-sensitivity-always.md) | 2026-05-19 | Accepted | `holomush-sb3n` |
| [Generalize Plugin-Code Participant Gate from Scene-Log to All Participant-Only Scene RPCs](holomush-nt2d-participant-gate-pattern-generalized.md) | 2026-05-19 | Accepted (supersedes `holomush-c8a9`) | `holomush-nt2d` |
| [Extend sceneAuditLogStore With Operation-Specific InsertScenePose for Transactional Atomicity](holomush-1ang-audit-interface-operation-specific-tx.md) | 2026-05-19 | Accepted | `holomush-1ang` |
| [Snapshot RPC as Source of Truth for Current-State Presence](holomush-da2q-snapshot-rpc-source-of-truth-presence.md) | 2026-05-19 | Accepted | `holomush-da2q` |
| [Current-State Presence Snapshot Exempt from I-PRIV-1 Temporal Floor](holomush-o46k-presence-snapshot-exempt-from-priv-floor.md) | 2026-05-19 | Accepted | `holomush-o46k` |
| [Introduce list_presence ABAC Action with Default-Deny and Same-Location Seed](holomush-lp65-list-presence-abac-action.md) | 2026-05-19 | Accepted | `holomush-lp65` |
| [Plugin Manifests Opt-In to history_scope (vs Spec's Exempt-List Framing)](holomush-jhl5-plugin-history-scope-opt-in.md) | 2026-05-17 | Accepted | `holomush-jhl5` |
| [Hard-Gate (Current-Location-Only) for Location-Stream History Reads](holomush-wxty-hard-gate-location-stream-history.md) | 2026-05-17 | Accepted | `holomush-wxty` |
| [Per-Session Attach Intervals on SessionInfo for Multi-Session Continuity](holomush-rc8b-per-session-attach-intervals.md) | 2026-05-17 | Accepted | `holomush-rc8b` |
| [Session-Store Sync Hook on Character Move](holomush-kmac-session-store-sync-hook-character-move.md) | 2026-05-17 | Accepted | `holomush-kmac` |
| [In-Process Filter-at-Delivery as Load-Bearing Privacy Gate; NATS-as-Source-of-Truth for Consumer Config](holomush-ghpx-filter-at-delivery-and-nats-source-of-truth.md) | 2026-05-17 | Accepted | `holomush-ghpx` |
| [Use Init-RPC Protocol Extension to Communicate Code-Registered Emit Types](holomush-vie9-init-rpc-emit-type-communication.md) | 2026-05-17 | Accepted | `holomush-vie9` |
| [Scope Lua Load Capture Pass to crypto.emits-Declaring Plugins Only](holomush-7h0c-lua-load-pass-optin-scope.md) | 2026-05-17 | Accepted | `holomush-7h0c` |
| [Split Plugin SDK into eventkit and groupkit by Scope](holomush-p7w0-split-plugin-sdk-eventkit-groupkit.md) | 2026-05-16 | Accepted | `holomush-p7w0` |
| [Require N=2 Consumer Validation Before SDK Primitive Extraction](holomush-lrt3-n2-consumer-validation-sdk-extraction.md) | 2026-05-16 | Accepted | `holomush-lrt3` |
| [Strict Plugin-Boundary: Plugins Must Not Modify internal/](holomush-z1e7-strict-plugin-boundary.md) | 2026-05-16 | Accepted | `holomush-z1e7` |
| [Startup-Time Set-Equality Validation of crypto.emits Declarations](holomush-3vsb-manifest-emit-type-startup-validation.md) | 2026-05-16 | Accepted | `holomush-3vsb` |
| [Enforce Scene Privacy at Plugin Code, Not ABAC Engine](holomush-c8a9-scene-privacy-plugin-code-enforcement.md) | 2026-05-16 | Superseded by `holomush-nt2d` | `holomush-c8a9` |
| [Use suiteT Capture Pattern Instead of GinkgoT() for testing.TB](holomush-1f1w-suitet-capture-pattern-ginkgo-testing-tb.md) | 2026-05-16 | Accepted | `holomush-1f1w` |
| [Remap INV-Pinned Test\* Functions to Ginkgo Suite Entries on Migration](holomush-iv7l-remap-inv-pinned-tests-ginkgo-suite-entries.md) | 2026-05-16 | Accepted | `holomush-iv7l` |
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
| [Command Security Model](holomush-vi5e-command-security-model.md) | 2026-02-02 | Superseded by `holomush-7kvy` | `holomush-vi5e` |
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
