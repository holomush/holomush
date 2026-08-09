---
status: testing
phase: 03-world-character-commands
source: [03-VERIFICATION.md]
started: 2026-08-09T23:30:36Z
updated: 2026-08-09T23:30:36Z
---

## Current Test

number: 1
name: IDENT-04 is legitimately `Complete` at Phase 3
expected: |
  A ruling is recorded on whether IDENT-04's "admin-driven soft retire" is satisfied by the
  domain capability plus the admin-only ABAC surface alone, given that
  `world.Service.RetireCharacter` has ZERO non-test callers (no gRPC RPC, no web handler, no
  CLI, no admin surface) and the traceability table maps IDENT-04 to Phase 3 only.
  Either (a) confirm the domain capability + ABAC surface is what IDENT-04 meant and the admin
  UI is a separate requirement, or (b) revert IDENT-04 to Pending / split it so the Phase 6
  admin half is traced.
awaiting: user response

## Tests

### 1. IDENT-04 is legitimately `Complete` at Phase 3
expected: A ruling is recorded on whether IDENT-04's "admin-driven soft retire" is satisfied by the domain capability plus the admin-only ABAC surface alone, given that `world.Service.RetireCharacter` has ZERO non-test callers and the traceability table (`.planning/REQUIREMENTS.md:372`) maps IDENT-04 to Phase 3 only. Either (a) confirm the domain capability + ABAC surface is what IDENT-04 meant and the admin UI is a separate requirement, or (b) revert IDENT-04 to Pending / split it so the Phase 6 admin half is traced.
why_human: Requirement-scope judgment, not a code fact. The code state is unambiguous and verified; what "Complete" means for a requirement whose user-facing half lands three phases later is the developer's call.
result: [pending]

### 2. The criterion-3 CI-gating caveat is accepted or scheduled
expected: A ruling is recorded on whether an ungated proof is acceptable coverage. The two-replica resilience proof (`test/integration/resilience/retire_concurrency_test.go`) lives in a suite gated on `quarantinetest.Enabled()` (`resilience_suite_test.go:50`), so it does NOT run in the gating `task test:int` and a regression in it would not be caught by PR CI. Either accept the ungated proof (the same guarantee IS covered in the gating lane by `test/integration/world/character_retire_atomicity_test.go:134` and `internal/world/service_retire_test.go:83`), or schedule #4953 so the resilience suite can rejoin the gating lane.
why_human: Whether an ungated proof is acceptable coverage is a policy decision about CI gating, not a codebase fact.
result: [pending]

## Summary

total: 2
passed: 0
issues: 0
pending: 2
skipped: 0
blocked: 0

## Gaps
