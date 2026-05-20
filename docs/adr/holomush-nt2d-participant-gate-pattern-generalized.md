<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Generalize Plugin-Code Participant Gate From Scene-Log to All Participant-Only Scene RPCs

**Date:** 2026-05-19
**Status:** Accepted
**Decision:** holomush-nt2d
**Supersedes:** [`holomush-c8a9`](holomush-c8a9-scene-privacy-plugin-code-enforcement.md) (broadens scope)
**Deciders:** HoloMUSH Contributors

## Context

ADR `holomush-c8a9` established that scene-log content reads (Phase 6 work, `holomush-cb4x`) are gated by a direct `scene_participants` membership check in plugin code, before any ABAC engine consultation. The rationale: scene privacy is unconditional (INV-S9); ABAC evaluation creates a policy-gap risk where misconfigured or future-added policies could leak content to non-participants. Plugin-code gates are unconditional under the call graph and survive ABAC engine evolution.

c8a9 was scoped to **scene-log reads** specifically. The ADR text said:

> "Pattern generalises: any future use with participant-only visibility (e.g., private channels with content-encryption) follows the same shape."

Phase 4 (`holomush-5rh.13`) introduces `GetPoseOrder` — a new RPC that returns participant-ordered pose metadata. Pose-order data reveals:

- Active participant set for the scene (`character_id` and `character_name` of every member).
- Per-participant timing (`last_posed_at` timestamps).
- Per-participant eligibility (computed from `total_pose_count` and per-character `last_pose_seq`).

This is participant-only data by the same logic as scene-log content. A non-participant who could call `GetPoseOrder` would learn who is in the scene and when they last interacted — leaking the participant set even if the IC content remained encrypted.

Two approaches were available:

1. Route `GetPoseOrder` through the ABAC engine (standard resource-authorization path).
2. Apply the c8a9 plugin-code gate pattern, generalizing it from scene-log to `GetPoseOrder`.

A secondary question: should the participant check use a new `IsParticipant` store method or reuse the existing `GetParticipant` (which returns a row or a `SCENE_PARTICIPANT_NOT_FOUND` error)?

## Decision

`GetPoseOrder` applies the INV-S9 plugin-code participant gate pattern. The handler calls `s.store.IsParticipant(ctx, sceneID, characterID)` — a new binary predicate store method that returns `true` only when the character has `role = 'owner'` or `role = 'member'` (NOT `'invited'`). The ABAC engine MUST NOT be consulted for this gate.

This generalizes c8a9's pattern from one RPC (scene-log read) to all participant-only scene RPCs. Future participant-only scene surfaces (Phase 6's `GetSceneLog` via `holomush-cb4x`, any future scene-presence RPC, any future scene-membership-list RPC) inherit the same gate.

The new ADR **supersedes c8a9** because it broadens the scope rather than adding alongside. c8a9's scene-log-specific pattern is fully absorbed; Phase 6's `GetSceneLog` will follow this generalized ADR.

The `IsParticipant` store method is distinct from the existing `GetParticipant`:

- `GetParticipant(ctx, sceneID, characterID) (*ParticipantRow, error)` returns a row or `SCENE_PARTICIPANT_NOT_FOUND` error.
- `IsParticipant(ctx, sceneID, characterID) (bool, error)` returns a binary predicate with the role filter `IN ('owner', 'member')` applied in SQL.

INV-P4-5 closes the indirect ABAC-attribute leak path: `AttributeResolverService.ResolveResource` MUST NOT expose pose-order data (`last_pose_at`, `last_pose_seq`, `total_pose_count`) as scene attributes. The ABAC engine sees only the existing Phase 3 scene attribute set; pose-order data is reachable exclusively via the gated `GetPoseOrder` RPC.

INV-P4-4 meta-test (`internal/test/invariants/scene_no_abac_in_getposeorder_test.go`) uses `go/parser` to extract the `GetPoseOrder` function body and rg-asserts no `engine.Evaluate` / `engine.CanPerformAction` call appears. This makes the structural invariant CI-enforced.

## Rationale

**Unconditional guarantee requires unconditional architecture.** "ABAC will be configured correctly" is not the same as "non-participants cannot see pose-order data." The former is a property of policy text; the latter is a property of the call graph. The plugin-code gate makes the guarantee architectural (not policy-dependent).

**c8a9's pattern explicitly generalizes.** c8a9 itself anticipated this case: its text says the pattern applies to "any future use with participant-only visibility." `GetPoseOrder` is the first exercise of that generalization. Filing a new ADR (vs. an addendum) makes the generalization explicit for future scene RPCs and for plugins outside scenes that adopt the same shape.

**`IsParticipant` names the gate's contract.** The existing `GetParticipant` conflates lookup with error handling — a caller using it for the gate would need to inspect the oops code to distinguish "not found" from "gate decision." `IsParticipant` is a binary predicate with the role filter `IN ('owner', 'member')` applied in SQL — the invited-role exclusion is pinned in one place (the SQL `WHERE` clause), not scattered across callers.

**The role filter is load-bearing.** Phase 3 introduced the `invited` role as a transient participant state (a row that grants the holder permission to JOIN, but not membership yet). An `invited` row is structurally a participant in `scene_participants` but is NOT a member of the scene's privacy boundary. The gate's `IN ('owner', 'member')` filter pins this distinction; using `GetParticipant` would risk treating invited rows as participants in callers that forget the filter.

**`AttributeResolverService` MUST NOT expose pose-order data.** The c8a9 ADR pinned this principle for scene-log content; INV-P4-5 extends it to pose-order metadata. Without this restriction, the ABAC engine could reach pose-order data indirectly (via attribute resolution during policy evaluation), bypassing the plugin-code gate. The restriction is enforced both by code-review (the resolver does not query pose-order columns) and by meta-test (rg-asserts no pose-metadata column references in `resolver.go`).

## Alternatives Considered

**Option A: Route `GetPoseOrder` through ABAC engine (standard resource authorization).**

| Aspect     | Assessment |
| ---------- | ---------- |
| Strengths  | Consistent with standard resource authorization; no special-case pattern; ABAC audit log records the decision |
| Weaknesses | Requires a policy declaring who can call GetPoseOrder; any policy gap or misconfiguration allows non-participants to see pose-order data; inconsistent with c8a9's rationale for unconditional participant-only surfaces; future ABAC engine evolution (admin-bypass patterns, emergency override) could leak pose-order data without intent |

**Option B: Extend c8a9's plugin-code gate to `GetPoseOrder` (chosen).**

| Aspect     | Assessment |
| ---------- | ---------- |
| Strengths  | Privacy boundary enforced before ABAC is consulted; no policy gap can leak pose-order data; pattern is now established for any future participant-only RPC; INV-P4-4 meta-test makes the structural invariant CI-enforced |
| Weaknesses | Plugin-code gate is not surfaced in ABAC audit logs (visibility cost); creates a dual-path pattern (some gates in ABAC, some in plugin code) that implementers must learn |

**Option C: Use existing `GetParticipant` instead of new `IsParticipant`.**

| Aspect     | Assessment |
| ---------- | ---------- |
| Strengths  | Reuses an existing interface method; no interface extension |
| Weaknesses | Conflates lookup with gate decision; caller must inspect oops code; role filter must be re-applied at every gate site (error-prone); the gate's intent is binary, not a row-lookup |

## Consequences

**Positive:**

- No policy gap can route non-participants to pose-order data.
- Pattern is now documented and established for `GetSceneLog` (Phase 6, `holomush-cb4x`) and any other participant-only scene RPC.
- `IsParticipant` is reusable across the codebase wherever the binary gate semantics are needed.
- INV-P4-4 meta-test catches accidental ABAC consultation in `GetPoseOrder` at CI time, not at code review.

**Negative:**

- Plugin-code gates are not visible in ABAC audit trails; operator forensic correlation must be done via the plugin's structured logs.
- Implementers must learn the dual-path convention: standard ABAC for resource operations that route through manifest policies; plugin-code gate for unconditional participant-only surfaces.
- One additional store method on `sceneStorer` interface (test fakes must implement it).

**Neutral:**

- Pose-order metadata is also excluded from `AttributeResolverService.ResolveResource` (INV-P4-5), closing the indirect ABAC leak path.
- c8a9 is superseded but not deleted — the ADR file remains for historical context; its decision is absorbed into this ADR's broader scope.
- Phase 6 (`cb4x`) `GetSceneLog` will follow this generalized ADR, not c8a9 directly.

## References

- [Scenes Phase 4 Design](../superpowers/specs/2026-05-19-scenes-phase-4-streams-and-pose-order-design.md) §7.3, §11
- [Substrate Contract Spec](../superpowers/specs/2026-05-16-social-spaces-substrate-contract.md) INV-S9
- [History Scope Privacy Design](../superpowers/specs/2026-05-17-history-scope-privacy-design.md) §3 (scene privacy is absolute)
- [Scenes v2 Design](../superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md) §5.5 (original hard privacy boundary framing)
- [Superseded: `holomush-c8a9`](holomush-c8a9-scene-privacy-plugin-code-enforcement.md) (narrower scope: scene-log reads only)
- [ADR `holomush-kokk`](holomush-kokk-custom-go-native-abac-engine.md) — Custom Go-native ABAC engine (not in the path for scene participant-only surfaces)
- Bead: `holomush-cb4x` (Phase 6 scene log + export — will follow this generalized ADR)
- Bead: `holomush-5rh.13` (Phase 4 design)
