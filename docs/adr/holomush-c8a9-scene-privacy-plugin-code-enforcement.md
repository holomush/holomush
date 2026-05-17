<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Enforce Scene Privacy at Plugin Code, Not ABAC Engine

**Date:** 2026-05-16
**Status:** Accepted
**Decision:** holomush-c8a9
**Deciders:** HoloMUSH Contributors

## Context

Scenes are HoloMUSH's primary unit of structured roleplay. Scene logs
contain private IC (in-character) content visible only to scene
participants. The privacy guarantee for scene logs is **unconditional**:
no role, including admin, may read a scene log they were not a
participant in. This was a deliberate design choice in Scenes v2 (§5.5)
and is preserved as INV-S9 in the substrate-contract spec.

HoloMUSH's general-purpose authorization is the `AccessPolicyEngine`
(ABAC). Most plugin-owned resources route read/write authorization
through ABAC: the plugin declares the resource type in its manifest,
ships policies in Cedar-style DSL, implements an attribute resolver,
and the host's ABAC engine evaluates `permit` / `forbid` decisions.

The question for scene-log reads: should they route through ABAC like
other resource access, or should the privacy boundary live in plugin
code, before the ABAC engine is ever consulted?

ABAC routing carries real risk for unconditional privacy guarantees:

1. **Policy misconfiguration.** A future `permit` clause added to
   manifest policies could silently grant log access to a role that
   should not have it (e.g., `admin`). The misconfiguration would
   not be caught by tests unless the test explicitly asserted that
   admin reads fail.

2. **Composition complexity.** ABAC policies compose via deny-overrides.
   Adding policies elsewhere in the manifest (or via future runtime
   policy additions) could shift the evaluation outcome in non-obvious
   ways. The `policy test` command helps but is not a formal proof.

3. **Admin-bypass surface.** Many ABAC engines support some form of
   admin-bypass pattern ("admins see everything"). HoloMUSH's engine
   does not today, but the architectural shape allows such a pattern
   to be added later. Once the engine is in the trusted path, future
   admin-bypass becomes a real risk.

4. **Trust co-location.** The participant table lives in the scene
   plugin's schema. The ABAC engine lives in `internal/access/`.
   Routing log reads through ABAC means the trust decision crosses
   a boundary — the engine queries the plugin's `AttributeResolver`
   to determine participant status, then makes a `permit` decision.
   The decision and the data live in different packages.

## Decision

The hard privacy boundary for scene-log reads (and any future use with
the same participant-only shape) is **plugin-code-enforced**, in the
scene plugin itself. ABAC is **NOT** in the path for scene-log reads.
A non-participant read of a scene log fails *before* the `AccessPolicyEngine`
is consulted.

Implementation (Phase 6 work, `holomush-5rh.15`):

- `plugins/core-scenes/store.go::GetSceneLog(ctx, sceneID, characterID)`
  performs a direct membership check in the plugin's own
  `scene_participants` table.
- If `characterID` is not a participant: return `permission denied`
  immediately. No ABAC engine call.
- If `characterID` IS a participant: return log content.
- The plugin's `AttributeResolverService.ResolveResource` MUST NOT
  return scene log content as an attribute (since attribute resolvers
  are queried by ABAC, this would create an indirect path for
  non-participant reads).
- The plugin's gRPC `GetSceneLog` RPC MUST be the only read path
  exposed externally.

The membership check is implemented in the same package as the
participant data, with no policy engine in the path.

## Rationale

**Carried forward from Scenes v2 §5.5.** The brainstorming session
that produced the substrate-contract spec explicitly identified this
as a load-bearing invariant inherited from v2. The decision predates
substrate pivots; the substrate-contract spec re-codifies it as a
numbered invariant (INV-S9) with named enforcement.

**Unconditional guarantee requires unconditional architecture.** "ABAC
will be configured correctly" is not the same as "scene logs cannot be
read by non-participants." The former is a property of policy text;
the latter is a property of the call graph. To make the guarantee
architectural (not policy-dependent), the call graph must be unconditional.

**Future-proofing against ABAC evolution.** ABAC is a general-purpose
engine. Future enhancements (admin-bypass patterns, emergency-override
policies, audit policies that need read access) could be added in good
faith without realizing they break scene privacy. Plugin-code-enforced
privacy is invariant under ABAC evolution.

**Co-located trust is correct.** The plugin owns both the participant
table and the privacy guarantee. The check happens in the same package,
in the same query, against the same row. There is no trust boundary
to cross.

**No admin bypass is possible.** The plugin's read RPC is the only
exposed path. The plugin does not query an "is admin" attribute. The
plugin does not import `internal/access/`. The architectural shape
forbids admin bypass.

## Alternatives Considered

**Option A: Route scene-log reads through ABAC like other resource access**

| Aspect     | Assessment                                                                                                                                                                                                       |
| ---------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Strengths  | Uniform authorization path; policy centralized in manifest; no special-case code; ABAC's audit logging applies                                                                                                   |
| Weaknesses | Policy misconfiguration or future policy addition can break the privacy guarantee; ABAC engine in trusted path for sensitive IC content; admin-bypass patterns in ABAC could leak content; trust crosses package |

**Option B: Plugin-code-enforced privacy boundary before ABAC (chosen)**

| Aspect     | Assessment                                                                                                                                                                                                       |
| ---------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Strengths  | Hard boundary: non-participant read fails before any engine consulted; no admin-bypass path can exist; scene plugin owns enforcement, co-located with data; survives future ABAC policy additions                |
| Weaknesses | Special-case code path diverges from standard authorization flow; testing requires call-stack assertions or boundary-violation tests; pattern must be re-applied for any future use with the same shape         |

## Consequences

**Positive:**

- Privacy guarantee is unconditional and survives future ABAC policy additions
- No admin-bypass path for scene log content
- Enforcement co-located with participant data (scene plugin owns both)
- Pattern generalizes: any future use with participant-only visibility (e.g., private channels with content-encryption) follows the same shape

**Negative:**

- Non-uniform authorization path (scene logs are a special case)
- Testing requires explicit boundary-violation tests (call-stack assertion that ABAC is not consulted)
- The pattern must be re-applied for any future use with the same shape (not automatic from substrate)

**Neutral:**

- The plugin's other operations (create / join / leave / pose) still route through ABAC normally
- The privacy boundary is at the *read* path only; emit-side enforcement uses the crypto envelope (sensitivity classification + AuthGuard fence)

## References

- [Substrate Contract Spec — §4.1, INV-S9](../superpowers/specs/2026-05-16-social-spaces-substrate-contract.md)
- [Scenes v2 Design — §5.5 Hard Privacy Boundary](../superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md)
- [Custom Go-Native ABAC Engine (`holomush-kokk`)](holomush-kokk-custom-go-native-abac-engine.md) — the engine NOT in the path for scene logs
