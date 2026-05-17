<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Hard-Gate (Current-Location-Only) for Location-Stream History Reads

**Date:** 2026-05-17
**Status:** Accepted
**Decision:** holomush-wxty
**Deciders:** HoloMUSH Contributors

## Context

`QueryStreamHistory` historically routed all public-stream authorization through the ABAC engine (`internal/grpc/query_stream_history.go:179-214`). The history-scope privacy spec (`holomush-iwzt`) introduces an invariant requiring that a character's view of location-stream history be limited to time intervals of active presence. Implementing this via ABAC alone is feasible but couples the privacy invariant to policy-author discipline: a misconfigured policy could re-introduce the leak.

The privacy invariant is fundamental — "a character cannot read a location they are not in" is not context-dependent. The question is where in the stack the invariant lives.

## Decision

**Replace ABAC with a hardcoded gate for location streams specifically.** The gate enforces `session.LocationID.String() == extractLocationID(stream)`, default-deny. ABAC remains active for all other public streams (`global`, `system`, etc.). Staff and admin characters bypass the hard-gate via a separate ABAC predicate (`read_unrestricted_history` action) that is itself restricted to specific roles by seeded policy.

## Rationale

**Trust ownership.** The privacy invariant is fundamental, not contextual. A hardcoded gate places enforcement under the same review discipline as the code itself; an ABAC policy puts it under the discipline of the policy author, who may be a different person. For default-deny invariants, code is the right enforcement boundary.

**Symmetry with scene streams.** Scene streams already use an I-17 hardcoded membership gate (`internal/grpc/query_stream_history.go:157-178`) for the same reason — ABAC is never consulted for private streams; no policy override is possible. Adopting the same pattern for location streams aligns the model: gating the access-or-no-access question is code; policy authoring continues to govern attribute-driven decisions (factions, properties, etc.).

**Staff bypass is the legitimate exception.** Debugging incidents requires staff to read locations they are not in. Routing that through ABAC keeps the bypass auditable (policy ID logged), opt-in (must have role), and revocable (delete the policy). The bypass affects only the hard-gate location-match check; the temporal floor still applies per I-PRIV-6.

## Alternatives Considered

**A. Keep ABAC, add the temporal floor inside the policy.** Rejected: misconfiguration risk; one bad policy edit re-opens the leak. The hardcoded enforcement is precisely what protects against this class of failure.

**B. Hard gate with no override path.** Rejected: blocks legitimate staff/admin debugging. Would force staff to physically move characters to investigate incidents in locations they were not in at the time.

**C. Three-layer model (hard gate by default + ABAC consultation + audit-only fallback).** Rejected: complexity disproportionate to the requirement.

**D (selected): Hard gate by default + ABAC override path via dedicated `read_unrestricted_history` action.**

## Consequences

- **MUST** maintain the seeded `read_unrestricted_history` policy. Removal would break staff debugging.
- **MUST** apply the temporal floor even under staff override (I-PRIV-6) — debugging "what did guest X see when they connected?" should reproduce the guest's view, not retroactive omniscience.
- **MUST NOT** rely on ABAC alone for location-stream privacy invariants.
- Removes one category of policy from ABAC eval cost, slightly improving the public-stream query path.
- Sets precedent for future privacy invariants: code-enforced default with ABAC override for legitimate bypass.

## References

- Spec: `docs/superpowers/specs/2026-05-17-history-scope-privacy-design.md` §3, §6.1, I-PRIV-1, I-PRIV-6
- Bead: `holomush-iwzt`
- Related ADRs: `holomush-kokk` (custom Go-native ABAC engine — staff override leverages this), `holomush-0tq6` (three-layer player access control — different layering pattern for player-level concerns)
- Code: `internal/grpc/query_stream_history.go:157-214`, `internal/access/policy/seed.go:104` (existing admin role template)
