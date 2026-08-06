---
status: complete
phase: 01-portal-spec
source: [01-01-SUMMARY.md, 01-02-SUMMARY.md, 01-03-SUMMARY.md, 01-04-SUMMARY.md, 01-05-SUMMARY.md, 01-06-SUMMARY.md]
started: 2026-08-06T00:13:00Z
updated: 2026-08-06T00:32:00Z
---

## Current Test

[testing complete]

## Tests

### 1. §10.5 admin gate — per acting character or per player
expected: §10.5 answers /admin gating as PER PLAYER, grounded in resolving path:line citations. Determines WebCheckSessionResponse's role field shape — a wire-compat commitment Phase 4/6 cannot cheaply revisit.
coverage_id: D5
source: human
result: pass

### 2. §9 RPC surface completeness
expected: §9 fixes the full new RPC surface with an audience verdict on every character-returning RPC, discharging §3.4's obligation on the Phase-4 census. A mechanical check confirms expected_version is present but cannot confirm no required operation was omitted.
coverage_id: D1
source: human
result: pass
note: "Passed with a deferred follow-up — see ## Deferred Follow-Ups. Admin rename remains absent by decision; Phase 3 must settle it."

### 3. §16 grounding trace walkability
expected: §16 carries ref, date, counts, a point-in-time disclaimer, a grouped citation list, and a stated non-coverage section — such that a reviewer can walk the trace without reading the SPEC.
coverage_id: D2
source: human
result: pass
note: |
  Spot-checked 3 of 189 citations against tree e6f36284a (post-Phase-2):
  000001_baseline.sql:265 lands exactly; prefix.go:23-33 and property.go:80-86
  both drifted but re-derived by name in one grep each (:62 and :108-111
  respectively). Claims held; only line numbers rotted — exactly the behavior
  §16.8's disclaimer predicts and prescribes the remedy for. No-CI-gate tradeoff
  accepted deliberately: a design record should not break the build when
  unrelated code moves.

### 4. §9.4 expected_version as int32 scalar per request message
expected: §9.4 fixes expected_version as an int32 scalar per request message, rejects absent-or-zero, names WORLD_CONCURRENT_EDIT, and mandates in-transaction outbox emission
result: pass
source: automated
coverage_id: D2

### 5. INV-WORLD-7 declared and registered
expected: INV-WORLD-7 declared in §13 and registered with binding: pending and no asserted_by; invariants.md regenerated
result: pass
source: automated
coverage_id: D3

### 6. §10 seven-section registry
expected: §10 fixes the seven-section registry with a mandatory authorization descriptor and gate-before-NOT_IMPLEMENTED ordering
result: pass
source: automated
coverage_id: D4

### 7. §10.8 role mutation exclusion
expected: §10.8 records role mutation as an explicit exclusion alongside impersonation, break-glass identifiers and a raw DB console, each with a reason
result: pass
source: automated
coverage_id: D6

### 8. §11 PORTAL-09 verdict
expected: §11 states the PORTAL-09 verdict as an explicit no with three reasons, and bounds the one permitted sorting surface to four intrinsic columns
result: pass
source: automated
coverage_id: D7

### 9. path:line citation resolution
expected: Every path:line citation in 01-SPEC.md resolves to exactly one tracked file, checked at a named commit
result: pass
source: automated
coverage_id: D1

### 10. D-19 pointer edit applied at both passages
expected: D-19 pointer edit applied at both CLAUDE.md passages and the invariants rule, with the walk-root prose byte-identical
result: pass
source: automated
coverage_id: D3

### 11. Completeness gate
expected: 16 sections, zero placeholders, 10/10 PORTAL requirements traced, 5/5 roadmap criteria traced, bidirectional invariant set equality, all pending with no asserted_by
result: pass
source: automated
coverage_id: D4

### 12. VERIFICATION gap 1 closed — governed profile attribute name consistency
expected: The SPEC is internally consistent about the governed profile attribute names — §8.6 no longer diverges from §7.2/§9.5/§10.6.
result: pass
source: automated
verifies_gap: 1
evidence: |
  `rg 'profile\.preferences' 01-SPEC.md` returns ZERO matches; all 5 occurrences
  read `profile.rp_preferences`. The §8.6 divergence named in the gap is gone.

### 13. VERIFICATION gap 2 closed — expected_version rule is implementable as written
expected: The mutation-carriage rule no longer makes CreateCharacter unshippable; the exclusion is stated normatively rather than left to inference.
result: pass
source: automated
verifies_gap: 2
evidence: |
  01-SPEC.md excludes CreateCharacter at FOUR sites: :2018 ("No `expected_version`
  — it creates the row a version guard would protect (§9.4.2)"), :2061
  ("`CreateCharacter` is excluded"), :2100 ("MUST NOT carry `expected_version` at
  all"), :2217 (CHARACTER_VERSION_REQUIRED scoped to *guarded* mutations, "Not
  reachable from `CreateCharacter`"). Ruling recorded at phase close 2026-08-02.

### 14. VERIFICATION gap 3 closed — superseded text annotated in sibling artifacts
expected: Amendment row 6's superseded count is annotated in `.planning/research/SUMMARY.md` in the same form rows 4 and 7 used.
result: pass
source: automated
verifies_gap: 3
evidence: |
  `.planning/research/SUMMARY.md:360` carries "**SUPERSEDED IN PART — see
  `.planning/phases/01-portal-spec/01-SPEC.md` §14, row 6.**", naming
  WebListPublishedScenes as the fourth surface and stating the deliverable itself
  is unchanged. Signed "Annotated 2026-08-01 by the phase-1 gap-closure pass."
  The stale count at :352 is intentionally left verbatim — the gap's own remedy
  was to annotate, not rewrite. The gap's second item is marked "Optionally" and
  is not required for closure.

## Summary

total: 14
passed: 14
issues: 0
pending: 0
skipped: 0

## Deferred Follow-Ups

- test: 2
  idea: "A more general RPC for character property updates — the §9.3 surface is
    nine intent-named mutations; a generic property-update RPC would collapse the
    per-field surface. Fine as-is for v0.13; backlog for a later milestone."
  deferred_at: 2026-08-06

## Gaps

[none yet]
