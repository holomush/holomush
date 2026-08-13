---
status: testing
phase: 05-character-identity-ui-public-profiles
source: [05-VERIFICATION.md]
started: 2026-08-13T12:21:26Z
updated: 2026-08-13T12:21:26Z
---

## Current Test

number: 1
name: Live create at /characters/new with a full-width-Latin name, then observe the roster confirmation
expected: |
  The confirmation names the SERVER-folded ASCII form, not the string typed. A rejected submit
  preserves all six entered values and moves focus to the name field.
awaiting: user response

## Tests

### 1. Live create at `/characters/new` with a full-width-Latin name (e.g. `Ｋａｅｌ`), then observe the roster confirmation
expected: The confirmation names the SERVER-folded ASCII form, not the string typed. A rejected submit preserves all six entered values and moves focus to the name field.
why_human: WINDOWS.md row 19 — 05-06 Task 3 human-check recorded UNRUN. Needs a live stack; the E2E covers the happy path but the plan's own walkthrough was never executed.
result: [pending]

### 2. Load `/characters` with a mixed roster (at least one active, one retired, one default) and exercise the sectioned grid
expected: Playable section first with the create link; Not playable second, expanded, its count chip collapsing the grid out of the flow; the retired card shows `Retired` and no session word; both echo sites render the server name.
why_human: WINDOWS.md row 20 — 05-07 Task 3 human-check recorded UNRUN. Requires a live grid.
result: [pending]

### 3. Open `/characters/[id]` in two tabs, save a section in tab A, then save a DIFFERENT section in tab B
expected: Tab B's failing section shows the concurrent-edit copy and keeps its typed text; the other four sections are untouched; focus moves to the failed section's first field.
why_human: 05-04's two-tab conflict walkthrough (D7) recorded unrun. The per-section conflict scoping (D-93) cannot be exercised without two live clients against one row.
result: [pending]

### 4. Confirm the roster ordering expectation for `/characters`
expected: Two consecutive roster loads with no intervening write render the Playable cards in the same order.
why_human: Plan 05-01 carried this truth as `verification: backstop` and it is NOT automated-verified. `charRepo.ListByPlayer` has no `ORDER BY` (WINDOWS.md row 22, issue #4965), so the property holds only by heap-scan accident.
result: [pending]

### 5. Confirm the two UI-SPEC `backstop` truths whose SOLE gate is the ungated web suite — the media renderer and the byte counter
expected: The `ProfileMedia` renderer and `ByteCounter` behave as `05-UI-SPEC.md` describes under a live page, independent of the ungated vitest runner.
why_human: Both truths are `verification: backstop` and are NOT automated-verified. Their only executing gate is the 566-test web suite, which has no Taskfile target and no CI job (#4964) — an unwired runner is not a gate, so these remain human items until #4964 lands.
result: [pending]

## Summary

total: 5
passed: 0
issues: 0
pending: 5
skipped: 0
blocked: 0

## Gaps
