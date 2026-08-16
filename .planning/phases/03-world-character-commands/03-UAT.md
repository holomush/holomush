---
status: complete
phase: 03-world-character-commands
source: [03-VERIFICATION.md]
started: 2026-08-09T23:30:36Z
updated: "2026-08-10T13:39:50Z"
---

## Current Test

[testing complete]

## Tests

### 1. IDENT-04 is legitimately `Complete` at Phase 3

expected: A ruling is recorded on whether IDENT-04's "admin-driven soft retire" is satisfied by the domain capability plus the admin-only ABAC surface alone, given that `world.Service.RetireCharacter` has ZERO non-test callers and the traceability table (`.planning/REQUIREMENTS.md:372`) maps IDENT-04 to Phase 3 only. Either (a) confirm the domain capability + ABAC surface is what IDENT-04 meant and the admin UI is a separate requirement, or (b) revert IDENT-04 to Pending / split it so the Phase 6 admin half is traced.
why_human: Requirement-scope judgment, not a code fact. The code state is unambiguous and verified; what "Complete" means for a requirement whose user-facing half lands three phases later is the developer's call.
result: pass
ruling: |
  Option (a) with a traceability fix. IDENT-04's Phase 3 scope IS the domain capability plus the
  admin-only ABAC surface; the admin-reachable path is a separate requirement. `Complete` at
  Phase 3 stands.

  Grounding re-verified at HEAD during this UAT session:

  - `world.Service.RetireCharacter` is defined at `internal/world/service.go:929` and has ZERO
    non-test callers. The two non-test mentions are not invocations: `internal/retirement/reactor.go:42`
    is a doc comment ("respelled rather than imported"), and `internal/world/mutator.go:104` is a
    command-name string in a kind mapping.

  - Phase 6 already specifies the admin surface that will call it: ADMIN-05
    (`.planning/REQUIREMENTS.md:203`) — "Admin disable/delete reuses the same lifecycle states as
    player-initiated retire".

  ACCEPTED RISK / follow-up: the traceability table (`.planning/REQUIREMENTS.md:372`) maps IDENT-04
  to Phase 3 ONLY. Nothing links it to Phase 6, so if ADMIN-05 slips or is rescoped, no gate flags
  that a `Complete` requirement has no user-reachable path. The ruling is conditional on that link
  being added.
  RE-OPEN CONDITION: if ADMIN-05 is deferred out of v0.13, IDENT-04 must revert to Pending or split.

### 2. The criterion-3 CI-gating caveat is accepted or scheduled

expected: A ruling is recorded on whether an ungated proof is acceptable coverage. The two-replica resilience proof (`test/integration/resilience/retire_concurrency_test.go`) lives in a suite gated on `quarantinetest.Enabled()` (`resilience_suite_test.go:50`), so it does NOT run in the gating `task test:int` and a regression in it would not be caught by PR CI. Either accept the ungated proof (the same guarantee IS covered in the gating lane by `test/integration/world/character_retire_atomicity_test.go:134` and `internal/world/service_retire_test.go:83`), or schedule #4953 so the resilience suite can rejoin the gating lane.
why_human: Whether an ungated proof is acceptable coverage is a policy decision about CI gating, not a codebase fact.
result: pass
ruling: |
  SCHEDULED, NOT ACCEPTED-AS-IS. The ungated proof was explicitly NOT accepted as sufficient
  standing coverage; #4953 is to be fixed so the resilience suite can rejoin the gating lane.
  Phase 3 is not blocked on it, because the same guarantee IS covered in the gating
  `Integration Test` lane for the single-process case.

  Grounding re-verified at HEAD during this UAT session:

  - `test/integration/resilience/resilience_suite_test.go:50-51` skips the whole suite unless
    `quarantinetest.Enabled()`, so zero specs run in the required PR lane.

  - Gating-lane coverage of the same guarantee is real:
    `test/integration/world/character_retire_atomicity_test.go` (a rejected retire leaves status,
    version, and envelope count untouched) and `internal/world/service_retire_test.go` (the
    version precheck surfaces WORLD_CONCURRENT_EDIT without invoking the executor).

  - #4953 is OPEN, labels `bug`/`priority::high`/`tests`, no assignee, no milestone (the repo has
    no milestones at all). So the ungated proof is not merely unwatched — it is currently RED
    where it does run.

  Why not fixed in Phase 3: deliberate scope boundary. The defect is in
  `internal/testsupport/natstest`, not in anything Phase 3 touched (03-03-SUMMARY.md Deviation 1).

  SCHEDULING ACTION TAKEN: the dependency is recorded on the issue itself —
  https://github.com/holomush/holomush/issues/4953#issuecomment-5241066902 — per CLAUDE.md's
  division where GitHub Issues own discrete work items. ROADMAP.md was deliberately NOT
  hand-edited (rule a32nfcekfc); if a `## Backlog` 999.10 entry is also wanted, `/gsd-capture`
  is the sanctioned writer.

  ACCEPTED RISK: the TWO-REPLICA case remains ungated in PR CI until #4953 closes. A regression
  in two-replica retire concurrency would not be caught by the required checks.
  RE-OPEN CONDITION: if #4953 is closed as wontfix, or the resilience suite is deleted rather
  than repaired, criterion 3's two-replica proof needs a new home in the gating lane.

## Summary

total: 2
passed: 2
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps
