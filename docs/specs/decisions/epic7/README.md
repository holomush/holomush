<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Epic 7: Full ABAC — Design Decisions

This directory contains the individual design decisions captured during the
Full ABAC architecture design process. Each decision records the question,
options considered, and rationale for the chosen approach.

**Related:** [Full ABAC Architecture Design](../../2026-02-05-full-abac-design.md)

**Date:** 2026-02-05
**Participants:** Sean (lead), Claude (design assistant)

## Decision Index

| #   | Title                                                              | Phase   | Link                                                                      |
| --- | ------------------------------------------------------------------ | ------- | ------------------------------------------------------------------------- |
| 1   | Policy Engine Approach                                             | General | [001](general/001-policy-engine-approach.md)                              |
| 2   | Policy Definition Format                                           | General | [002](general/002-policy-definition-format.md)                            |
| 3   | Attribute Resolution Strategy                                      | General | [003](general/003-attribute-resolution-strategy.md)                       |
| 4   | Conflict Resolution                                                | General | [004](general/004-conflict-resolution.md)                                 |
| 5   | ~~Migration Strategy~~ (superseded by #36)                         | 7.6     | [005](phase-7.6/005-migration-strategy.md)                                |
| 6   | Plugin Attribute Contributions                                     | 7.3     | [006](phase-7.3/006-plugin-attribute-contributions.md)                    |
| 7   | Audit Logging Destination                                          | 7.3     | [007](phase-7.3/007-audit-logging-destination.md)                         |
| 8   | DSL Expression Language Scope                                      | 7.2     | [008](phase-7.2/008-dsl-expression-language-scope.md)                     |
| 9   | Property Model                                                     | 7.1     | [009](phase-7.1/009-property-model.md)                                    |
| 10  | Property Visibility Defaults                                       | 7.1     | [010](phase-7.1/010-property-visibility-defaults.md)                      |
| 11  | Cache Invalidation                                                 | 7.3     | [011](phase-7.3/011-cache-invalidation.md)                                |
| 12  | Player Access Control Layers                                       | 7.1     | [012](phase-7.1/012-player-access-control-layers.md)                      |
| 13  | Subject Prefix Normalization                                       | 7.1     | [013](phase-7.1/013-subject-prefix-normalization.md)                      |
| 14  | No Database Triggers                                               | General | [014](general/014-no-database-triggers.md)                                |
| 15  | Grammar: `in` Operator Extended to Attribute Expressions           | 7.2     | [015](phase-7.2/015-in-operator-extended-to-attribute-expressions.md)     |
| 16  | Entity References Explicitly Deferred                              | 7.2     | [016](phase-7.2/016-entity-references-explicitly-deferred.md)             |
| 17  | Session Resolution at Engine Entry Point                           | 7.3     | [017](phase-7.3/017-session-resolution-at-engine-entry-point.md)          |
| 18  | Property Package Ownership                                         | 7.1     | [018](phase-7.1/018-property-package-ownership.md)                        |
| 19  | Lock Policies Are Not Versioned                                    | 7.5     | [019](phase-7.5/019-lock-policies-are-not-versioned.md)                   |
| 20  | ~~`enter` Action as New ABAC-Only Path~~ (superseded by #37)       | 7.6     | [020](phase-7.6/020-enter-action-as-new-abac-only-path.md)                |
| 21  | ~~Shadow Mode Cutover Criteria~~ (superseded by #37)               | 7.6     | [021](phase-7.6/021-shadow-mode-cutover-criteria.md)                      |
| 22  | Flat Prefixed Strings Over Typed Structs                           | General | [022](general/022-flat-prefixed-strings-over-typed-structs.md)            |
| 23  | Performance Targets                                                | General | [023](general/023-performance-targets.md)                                 |
| 24  | Bootstrap Sequence                                                 | 7.4     | [024](phase-7.4/024-bootstrap-sequence.md)                                |
| 25  | Intentional Builder Permission Expansion                           | 7.4     | [025](phase-7.4/025-intentional-builder-permission-expansion.md)          |
| 26  | Per-Request Attribute Caching                                      | 7.3     | [026](phase-7.3/026-per-request-attribute-caching.md)                     |
| 27  | Unified `AttributeProvider` Interface                              | 7.3     | [027](phase-7.3/027-unified-attribute-provider-interface.md)              |
| 28  | Cedar-Aligned Missing Attribute Semantics                          | 7.3     | [028](phase-7.3/028-cedar-aligned-missing-attribute-semantics.md)         |
| 29  | DSL `like` Pattern Validation at Parser Layer                      | 7.2     | [029](phase-7.2/029-dsl-like-pattern-validation.md)                       |
| 30  | PolicyCompiler Component                                           | 7.2     | [030](phase-7.2/030-policy-compiler-component.md)                         |
| 31  | Provider Re-Entrance Prohibition                                   | 7.3     | [031](phase-7.3/031-provider-re-entrance-prohibition.md)                  |
| 32  | PropertyProvider Uses SQL JOIN for Parent Location                 | 7.3     | [032](phase-7.3/032-property-provider-sql-join.md)                        |
| 33  | Plugin Lock Tokens MUST Be Namespaced                              | 7.5     | [033](phase-7.5/033-plugin-lock-tokens-must-be-namespaced.md)             |
| 34  | Time-of-Day Attributes for Environment Provider                    | 7.3     | [034](phase-7.3/034-time-of-day-attributes.md)                            |
| 35  | Audit Log Source Column and No Decision Column                     | 7.1     | [035](phase-7.1/035-audit-log-source-column.md)                           |
| 36  | Direct Replacement (No Adapter)                                    | 7.6     | [036](phase-7.6/036-direct-replacement-no-adapter.md)                     |
| 37  | No Shadow Mode                                                     | 7.6     | [037](phase-7.6/037-no-shadow-mode.md)                                    |
| 38  | Audit Log Configuration Modes                                      | 7.1     | [038](phase-7.1/038-audit-log-configuration-modes.md)                     |
| 39  | `EffectSystemBypass` as Fourth Effect Variant                      | 7.1     | [039](phase-7.1/039-effect-system-bypass.md)                              |
| 40  | `has` Operator Supports Dotted Attribute Paths                     | 7.2     | [040](phase-7.2/040-has-operator-dotted-attribute-paths.md)               |
| 41  | LL(1) Parser Disambiguation for Condition Grammar                  | 7.2     | [041](phase-7.2/041-ll1-parser-disambiguation.md)                         |
| 42  | Sequential Provider Resolution                                     | 7.3     | [042](phase-7.3/042-sequential-provider-resolution.md)                    |
| 43  | Property Lifecycle: Go-Level CASCADE Cleanup                       | 7.3     | [043](phase-7.3/043-property-lifecycle-cascade-cleanup.md)                |
| 44  | Nested Container Resolution via Recursive CTE                      | 7.3     | [044](phase-7.3/044-nested-container-resolution-via-recursive-cte.md)     |
| 45  | Bounded List Sizes for `visible_to` / `excluded_from`              | 7.1     | [045](phase-7.1/045-bounded-list-sizes.md)                                |
| 46  | `policy validate` and `policy reload` Commands                     | 7.5     | [046](phase-7.5/046-policy-validate-and-reload-commands.md)               |
| 47  | Fuzz Testing for DSL Parser                                        | 7.2     | [047](phase-7.2/047-fuzz-testing-for-dsl-parser.md)                       |
| 48  | Deterministic Seed Policy Names                                    | 7.1     | [048](phase-7.1/048-deterministic-seed-policy-names.md)                   |
| 49  | Revised Audit Volume Estimate                                      | 7.1     | [049](phase-7.1/049-revised-audit-volume-estimate.md)                     |
| 50  | Plugin Attribute Collision Behavior                                | 7.3     | [050](phase-7.3/050-plugin-attribute-collision-behavior.md)               |
| 51  | Session Integrity Error Classification                             | 7.3     | [051](phase-7.3/051-session-integrity-error-classification.md)            |
| 52  | Async Audit Writes                                                 | 7.7     | [052](phase-7.7/052-async-audit-writes.md)                                |
| 53  | Audit WAL Best-Effort Semantics                                    | 7.7     | [053](phase-7.7/053-audit-wal-best-effort-semantics.md)                   |
| 54  | Property Location Resolution Eventual Consistency                  | 7.7     | [054](phase-7.7/054-property-location-resolution-eventual-consistency.md) |
| 55  | Session Error Code Simplification                                  | 7.3     | [055](phase-7.3/055-session-error-code-simplification.md)                 |
| 56  | Audit Minimal Mode Includes System Bypasses                        | 7.1     | [056](phase-7.1/056-audit-off-mode-includes-system-bypasses.md)           |
| 57  | ADR Format Evolution                                               | General | [057](general/057-adr-format-evolution.md)                                |
| 58  | Provider Re-Entrance Goroutine Prohibition                         | 7.3     | [058](phase-7.3/058-provider-re-entrance-goroutine-prohibition.md)        |
| 59  | Fair-Share Provider Timeout Scheduling                             | 7.3     | [059](phase-7.3/059-fair-share-provider-timeout-scheduling.md)            |
| 65  | Git Revert as Migration Rollback Strategy                          | 7.6     | [065](phase-7.6/065-git-revert-migration-rollback.md)                     |
| 66  | Sync Audit Writes for System Bypass                                | 7.5     | [066](phase-7.5/066-sync-audit-system-bypass.md)                          |
| 74  | Unified Circuit Breaker via Task 34                                | 7.7     | [074](phase-7.7/074-unified-circuit-breaker-task-34.md)                   |
| 75  | Dual-Use Resource Prefixes (exit:, scene:, character: as Resource) | General | [075](general/075-dual-use-resource-prefixes.md)                          |
| 76  | Compound Resource Decomposition During Migration                   | 7.6     | [076](phase-7.6/076-compound-resource-decomposition.md)                   |
| 77  | Decision Type Mixed Visibility (Value Semantics Safety)            | General | [077](general/077-decision-type-mixed-visibility.md)                      |
| 78  | Task 27b Split into 3 Sub-Tasks                                    | 7.5     | [078](phase-7.5/078-task-27b-split-into-sub-tasks.md)                     |
| 79  | Standardize ADR Titles Across All Locations                        | General | [079](general/079-standardize-adr-titles.md)                              |
| 80  | Add Task 0.5 — Dependency Audit Before Implementation              | 7.1     | [080](phase-7.1/080-task-0-5-dependency-audit.md)                         |
| 81  | Move Task 4c Orphan Cleanup Goroutine to Phase 7.7                 | 7.7     | [081](phase-7.7/081-move-orphan-cleanup-to-resilience.md)                 |
| 82  | Core-First Provider Registration Order                             | 7.3     | [082](phase-7.3/082-core-first-provider-registration-order.md)            |
| 83  | Circuit Breaker Threshold Increase (3 to 5)                        | 7.3     | [083](phase-7.3/083-circuit-breaker-threshold-increase.md)                |
| 84  | Goroutine Re-Entry Spec Clarification                              | 7.3     | [084](phase-7.3/084-goroutine-re-entry-spec-clarification.md)             |
| 85  | PropertyProvider Not on Critical Path                              | 7.3     | [085](phase-7.3/085-property-provider-not-on-critical-path.md)            |
| 86  | Audit Minimal Mode Logs Denials                                    | General | [086](general/086-audit-off-mode-logs-denials.md)                         |
| 87  | Remove seed:property-system-forbid Policy                          | 7.4     | [087](phase-7.4/087-remove-seed-property-system-forbid.md)                |
| 88  | Add ExitProvider and SceneProvider Stubs                           | 7.3     | [088](phase-7.3/088-exit-scene-provider-stubs.md)                         |
| 89  | Add original_subject Field to Audit Log Schema                     | 7.1     | [089](phase-7.1/089-original-subject-audit-field.md)                      |
| 90  | System Bypass Takes Precedence Over Degraded Mode                  | 7.3     | [090](phase-7.3/090-system-bypass-precedence-over-degraded-mode.md)       |
| 91  | Bootstrap Creates Initial Audit Partitions                         | 7.4     | [091](phase-7.4/091-bootstrap-creates-initial-partitions.md)              |
| 92  | Fatal Seed Bootstrap Failures                                      | 7.4     | [092](phase-7.4/092-fatal-seed-bootstrap-failures.md)                     |
| 93  | Pin CI Tool SHA256 Hashes as Literals                              | General | [093](general/093-pin-ci-tool-hashes.md)                                  |
| 94  | Elevate Seed Policy Gap Resolution to Dedicated Task               | 7.4     | [094](phase-7.4/094-elevate-seed-policy-gap-resolution.md)                |
| 95  | M5 Milestone Gate Is T28 (Call-Site Migration)                     | General | [095](general/095-m5-milestone-gate-is-t28.md)                            |
| 96  | Defer Phase 7.5 (Locks & Admin) to Epic 8                          | General | [096](general/096-defer-phase-7-5-to-epic-8.md)                           |
| 97  | P99 Performance Target Adjustment (5ms → 25ms)                     | General | [097](general/097-p99-performance-target-25ms.md)                         |
| 98  | Import Cycle Check for PropertyProvider (T4a)                      | 7.1     | [098](phase-7.1/098-import-cycle-check-t4a.md)                            |
| 99  | AccessPolicyEngine Contract Tests (Task 7b)                        | 7.1     | [099](phase-7.1/099-access-policy-engine-contract-tests.md)               |
| 100 | Standardize Dependency Format Across Phase Files                   | General | [100](general/100-standardize-dependency-format.md)                       |
| 101 | ADR #65 Is Sufficient for Rollback Coverage                        | 7.6     | [101](phase-7.6/101-adr65-sufficient-for-rollback.md)                     |
| 102 | Lefthook `fmt-markdown` Auto-Fix Is Intentional                    | General | [102](general/102-lefthook-markdown-autofix-intentional.md)               |
| 103 | Remove T7→T12 Dependency (PolicyStore → PolicyCompiler)            | General | [103](general/103-remove-t7-t12-dependency.md)                            |
| 104 | Rename Audit Mode `off` to `minimal`                               | General | [104](general/104-rename-audit-off-to-minimal.md)                         |
| 105 | LISTEN/NOTIFY Disconnection Recovery Strategy                      | 7.3     | [105](phase-7.3/105-listen-notify-disconnection-recovery.md)              |

## Spec ADR Reference Mapping

The main ABAC specification ([Full ABAC Architecture Design](../../2026-02-05-full-abac-design.md)) references 8 key ADRs using a dual numbering scheme (ADR 0009-0016). Implementation plans and modular spec sections reference additional ADRs. This table maps all spec-referenced ADRs to their corresponding decision file numbers in this directory:

### Main Spec ADRs (ADR 0009-0016)

| Spec ADR | File # | Title                                     | Link                                                              |
| -------- | ------ | ----------------------------------------- | ----------------------------------------------------------------- |
| ADR 0009 | 001    | Policy Engine Approach                    | [001](general/001-policy-engine-approach.md)                      |
| ADR 0010 | 028    | Cedar-Aligned Missing Attribute Semantics | [028](phase-7.3/028-cedar-aligned-missing-attribute-semantics.md) |
| ADR 0011 | 004    | Conflict Resolution                       | [004](general/004-conflict-resolution.md)                         |
| ADR 0012 | 003    | Attribute Resolution Strategy             | [003](general/003-attribute-resolution-strategy.md)               |
| ADR 0013 | 009    | Property Model                            | [009](phase-7.1/009-property-model.md)                            |
| ADR 0014 | 036    | Direct Replacement (No Adapter)           | [036](phase-7.6/036-direct-replacement-no-adapter.md)             |
| ADR 0015 | 012    | Player Access Control Layers              | [012](phase-7.1/012-player-access-control-layers.md)              |
| ADR 0016 | 011    | Cache Invalidation                        | [011](phase-7.3/011-cache-invalidation.md)                        |

### Additional Plan-Referenced ADRs

| ADR # | Title                                            | Phase | Link                                                            |
| ----- | ------------------------------------------------ | ----- | --------------------------------------------------------------- |
| 31    | Provider Re-Entrance Prohibition                 | 7.3   | [031](phase-7.3/031-provider-re-entrance-prohibition.md)        |
| 35    | Audit Log Source Column and No Decision Column   | 7.1   | [035](phase-7.1/035-audit-log-source-column.md)                 |
| 38    | Audit Log Configuration Modes                    | 7.1   | [038](phase-7.1/038-audit-log-configuration-modes.md)           |
| 39    | `EffectSystemBypass` as Fourth Effect Variant    | 7.1   | [039](phase-7.1/039-effect-system-bypass.md)                    |
| 48    | Deterministic Seed Policy Names                  | 7.1   | [048](phase-7.1/048-deterministic-seed-policy-names.md)         |
| 56    | Audit Minimal Mode Includes System Bypasses      | 7.1   | [056](phase-7.1/056-audit-off-mode-includes-system-bypasses.md) |
| 66    | Sync Audit Writes for System Bypass              | 7.5   | [066](phase-7.5/066-sync-audit-system-bypass.md)                |
| 76    | Compound Resource Decomposition During Migration | 7.6   | [076](phase-7.6/076-compound-resource-decomposition.md)         |
| 82    | Core-First Provider Registration Order           | 7.3   | [082](phase-7.3/082-core-first-provider-registration-order.md)  |

## Numbering Gaps

The ADR numbering has intentional gaps (60-64, 67-73) that reflect decision slots reserved during the initial design phases but not all utilized. These are **not** deleted decisions — they are unused reserved numbers. The gaps exist to maintain stable numbering should additional decisions be needed in those ranges during future design iterations or maintenance.

Current gaps:

- **60-64:** Reserved during Phase 7.3 design; design decisions addressed elsewhere
- **67-73:** Reserved during Phase 7.6 design; design decisions addressed elsewhere

## Superseded Decisions

**Superseded Decisions:** ADRs #5, #20, and #21 are superseded (see their 'Superseded by' notes). Superseded decisions are retained at their original paths for referential integrity. Their superseded status is documented within each file's header.
