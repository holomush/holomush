---
status: testing
phase: 04-shared-facade-helpers-characteraccessservice
source: [04-VERIFICATION.md]
started: 2026-08-11T21:28:47Z
updated: 2026-08-11T21:28:47Z
---

## Current Test

number: 1
name: The `pronouns` reachability floor resting on a seeded default rather than structural immunity is an accepted v0.13 limitation
expected: |
  This is a decision to affirm or reject, not an activity to perform.

  ROADMAP criterion 3 says "the configuration cannot raise `name` or `pronouns`
  above the profile's own reachability floor". Half of that holds by
  construction and half holds only by seeded default:

  - `name` IS structurally immune. It is emitted from the character row at
    `internal/grpc/characteraccess_projection.go:75` and never routed through
    the per-attribute floor, so no configuration can raise it.
  - `pronouns` is NOT structurally immune. It is an `entity_properties` row
    evaluated through `profilevis.AttributeVisible`, seeded at the anonymous
    floor by `seed:profile-tier-floor-anonymous`. The ABAC engine is
    deny-overrides, so an admin `forbid` row would raise it above the
    reachability floor.

  This is exactly the clause 01-SPEC §8.8 makes unprovable in v0.13, which is
  why INV-PRIVACY-10 is deliberately `binding: pending` rather than bound to a
  test. No test can distinguish "correctly deferred" from "silently missed" —
  only the decision owner can.

  THE TRUTH TO RULE ON: that the `pronouns` half being enforced by seeded
  default rather than by construction is an ACCEPTED limitation for the v0.13
  milestone, and not an enforcement gap that must close within v0.13.

  Rule `pass` if the deferral stands for v0.13 (the position the phase was
  planned and executed under, per 04-CONTEXT.md and 01-SPEC §8.8).

  Rule `issue` if the `pronouns` floor must become non-overridable inside
  v0.13 — that is gap-closure work: either make pronouns structurally immune
  the way `name` is, or add an operator-facing guard that prevents a `forbid`
  row from beating the seeded floor.
awaiting: user response

## Tests

### 1. The `pronouns` reachability floor resting on a seeded default rather than structural immunity is an accepted v0.13 limitation
expected: `name` is structurally immune (projected from the character row, never floor-evaluated); `pronouns` is a floor-evaluated `entity_properties` row that a deny-overrides `forbid` could raise. 01-SPEC §8.8 makes the clause unprovable in v0.13; INV-PRIVACY-10 is deliberately `binding: pending`. The truth to rule on is that this is an accepted v0.13 limitation, not an enforcement gap to close in v0.13.
result: [pending]

## Summary

total: 1
passed: 0
issues: 0
pending: 1
skipped: 0
blocked: 0

## Gaps
