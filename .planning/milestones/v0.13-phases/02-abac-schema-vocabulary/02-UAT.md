---
status: complete
phase: 02-abac-schema-vocabulary
source: [02-VERIFICATION.md]
started: 2026-08-05T22:30:00Z
updated: 2026-08-05T22:45:00Z
---

## Current Test

[testing complete]

## Tests

### 1. Dispatch `abac-reviewer` over the phase's authorization surface
expected: A READY / NOT READY verdict on `internal/access/`, `internal/admin/section/`, and the seed family
result: pass
source: automated
evidence: |
  Dispatched by the orchestrator during `/gsd-execute-phase 02` (the 02-11 executor
  was itself a sub-agent with no dispatch tool and correctly refused to fabricate
  a verdict rather than self-grading its own diff).

  VERDICT: READY — 0 blocking findings, 8 non-blocking (3 medium, 5 low).

  D-29 was verified THREE independent ways, none of them the in-repo grep gate
  (which is known-broken — it counted 3 before and 3 after, because shipped
  policies end with `resource is character)` and `resource is character_directory`
  matches the substring):
    1. compiled target set enumerated directly from every seed's parsed resource
       clause — exactly 2 policies with resource type `character`, both
       pre-existing and both conditioned (`seed.go:41`, `seed.go:53`)
    2. the replacement test `TestNoPhase2SeedIntroducesACharacterResourceTypePermit`
       (`seed_profile_visibility_test.go:692-726`), which pins the compiled target
    3. migration and plugin-manifest sources — zero `permit(`/`forbid(` text in
       000054/000055/000056, no `plugins/*/plugin.yaml` modified

  All seven briefed attention items were reached and reported. Findings filed as
  GitHub issues #4933, #4934, #4935, #4936.

  Persisted report:
  `.claude/agent-memory/abac-reviewer/reports/2026-08-05-1625-phase-02-abac-schema-vocabulary.md`

### 2. Reconcile `.planning/REQUIREMENTS.md`'s two disagreeing halves
expected: Checkboxes and traceability-table rows agree, and PROFILE-11 is not recorded as fully closed
result: skipped
reason: |
  Deferred follow-up: NOT passed — mechanically disproven, and not fixable from
  inside this phase.

  Evidence (checked at HEAD 801537f25): all six phase-2 requirement IDs disagree
  between the file's two halves.
    checkboxes  `.planning/REQUIREMENTS.md:114,118,121,124,173,241` -> `[x]`
    table rows  `.planning/REQUIREMENTS.md:353,354,355,356,369,385` -> `Pending`

  Root cause is upstream, not local: `gsd-tools query requirements.mark-complete`
  has no partial-credit model, and seven plans in this phase share `EXT-07` /
  `PROFILE-11`. The first plan to finish (02-03, wave 2) flipped both checkboxes
  for all of them; every later plan's run returned
  `{"updated": false, "table_unmatched": [...], "write_set_complete": false}`.

  NOT hand-edited, deliberately: `.planning/REQUIREMENTS.md` is a tool-owned
  parsed artifact and `.claude/rules/planning-artifacts.md` forbids working around
  tool behaviour with local edits. Reporting the gap is the sanctioned path.

  The accurate requirement state is recorded in `02-VERIFICATION.md`'s own
  frontmatter, which is authoritative over either half of REQUIREMENTS.md:
  IDENT-06/07/08/09 discharged; PROFILE-11 `partial_by_decision` (the
  `characters.description` half is Phase 4's, by D-29); EXT-07
  `discharged_to_phase_ceiling` (endpoint-level denial is Phase 4's per D-08,
  EXT-04's census is Phase 6's).

### 3. Re-run 02-10's exposure audit against a populated corpus
expected: The per-row verdict machinery adjudicates at least one real row
result: skipped
reason: |
  Deferred follow-up: NOT passed — provably not satisfied, and deliberately
  scheduled for before Phase 4 rather than now.

  The audit DID run against a real database (scratch `postgres:18` restored from
  sandbox kopia snapshot `7e48a9b592c2e0d302a5da3cf0171835`, goose level 53) and
  returned a legitimate zero: 3 characters, 0 non-empty descriptions, and an
  `entity_properties` table empty in its entirety. So the ledger's per-row
  machinery — the `in_spec_86` split, the two verdict vocabularies, the
  digest-based re-check — has never adjudicated a real row, and B-14's
  "strictly greater" fixture check is vacuous at 0 vs 0.

  This is a property of the sandbox corpus, not a phase-2 defect: the merge gate
  it exists to satisfy ("a query that actually reached a database") IS discharged,
  and `02-AUDIT-RESULT.md` records the corpus size plainly so the zero is not
  over-read.

  Tracked as GitHub issue #4937, scoped to land before Phase 4's
  `characters.description` widening — which is the change the audit exists to gate.
  The artifact was deliberately built to be re-run; run instructions are in
  `02-10-SUMMARY.md`.

## Summary

total: 3
passed: 1
issues: 0
pending: 0
skipped: 2
blocked: 0

## Deferred Follow-Ups

- test: 2
  idea: "Reconcile REQUIREMENTS.md's checkbox/table disagreement — upstream GSD gap: requirements.mark-complete has no partial-credit model for plans sharing a requirement ID"
  deferred_at: 2026-08-05
- test: 3
  idea: "Re-run the PROFILE-11 exposure audit against a populated corpus before Phase 4's description widening (GitHub #4937)"
  deferred_at: 2026-08-05

## Gaps

[none — neither skipped item is a phase-2 code defect; both are tracked follow-ups]
