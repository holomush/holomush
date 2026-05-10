---
name: amendment-contradicts-just-landed-sibling-spec
description: When a Phase-N+1 spec amends a master spec contract that a Phase-N sibling spec just-landed (with code-level docs and meta-tests enforcing the contract), the amendment must enumerate every artifact it invalidates, not present itself as a localized prose change.
type: feedback
---

When sub-epic N+1 of a multi-epic build amends a master-spec section that
sub-epic N just-shipped (with explicit comments in code, meta-test
substring assertions, and inheritance from a parent decomposition spec),
the amendment is structurally larger than a one-row "REMOVE" entry in an
amendments table.

**Why:** Multi-epic specs decompose with deliberate dependency edges
(e.g., B says "B does NOT ship X; D does"). When D rolls back that
decision rather than completing it, the rollback affects (a) sibling-spec
prose, (b) code-level doc comments shipped by the sibling, (c) meta-tests
the sibling's PR added (TestSpecAmendmentsLanded), (d) the parent
decomposition spec's enumerated decisions, and (e) the threat model's
documented defense-in-depth rationale. Reviewing this kind of amendment
purely on the new spec's amendment table is insufficient — the
enumeration is incomplete by construction.

**How to apply:** When reviewing an amendment-table row that says
"REMOVE: <thing>" or "REPLACE shape A with shape B" in a multi-epic
chain, run a five-point cross-check before accepting:

1. `rg` the dropped string across all sibling specs (decomposition, prior
   sub-epic specs in the same chain). Any hit is an undeclared amendment
   the table is missing.
2. `rg` the dropped string against `*.go` files (especially
   `internal/access/grants.go`-style policy/contract docs). Code comments
   that enshrine the contract must be amended in the same PR.
3. `rg` for `TestSpec*Amendments*` style meta-tests in the sibling's
   spec's enforcement directory. These will fail or silently mask
   regressions if the dropped substring is in the test's positive list.
4. Read the parent decomposition spec's decisions for the dropped contract.
   If the decomposition explicitly stated the contract as a MUST, the
   sub-epic spec is overriding its parent — that needs threat-model
   justification, not tractability prose.
5. If the rollback rationale is "tractability" (e.g., "needs a detour
   through table X to look up role Y"), separate "this is hard to
   implement" from "this is the wrong threat model". The first is not a
   valid reason to drop a security control; the second requires a
   threat-model amendment with a compensating control or an explicit
   risk acceptance.

Seen 2026-05-09 in event-payload-crypto-phase5-sub-epic-d-design.md
finding 1: D's §10 amendment row 1 ("REMOVE: §5.9 step 6 admin role
check") presented as a 5-row scope, but actually invalidates ~6 master-
spec strings, the doc comment on `internal/access/grants.go:14-18`, the
TestSpecAmendmentsLanded meta-test (which substring-asserts the
conjunction), the parent decomposition spec line 177 (Decision 5), and
the threat model's "RoleAdmin AND crypto.operator" defense layering.
