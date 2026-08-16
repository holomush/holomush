---
created: 2026-08-16T10:55:13.298Z
title: Dedupe the two CI runs firing per push
area: tooling
severity: minor
files:
  - .github/workflows/ci.yaml
---

## Problem

Every push to `v013-phase-03` triggers **two** separate `CI` workflow runs on the same
SHA, so every required check — `Build`, `Lint`, `Test`, `Integration Test`, `E2E Test`,
`Vuln` — executes twice per push.

Observed on `ddafac161` (2026-08-16):

| Run | Event | Conclusion |
| --- | --- | --- |
| `31923483446` | `pull_request` | success |
| `31923483542` | `pull_request` | failure |

Both report a check named `Build`, and GitHub aggregates the check as FAILURE when
either run's job fails. The same doubling appeared on `33731f215`
(`31922771399` success / `31922771418` failure).

**Why it matters: it multiplies the blast radius of any flake on a required check.**
A check that passes with probability `p` per run must now pass twice, so the effective
pass rate is `p²`. During the bits-ui `Build` flake (#4987, ~50% per run) that meant
roughly a 25% chance of a green PR per push — which is approximately what PR #4984
experienced: three `Build` failures across about six attempts, including one that
failed *again* on a manual re-run.

Note this is an amplifier, not a defect in itself: the runs are not wrong, there are
just twice as many of them. It cost real time here only because a genuine flake was
present at the same time. But it will do so again for any future flake on a required
check, and it also doubles CI minutes for every push.

## Solution

Not yet diagnosed — deliberately left open rather than guessed at. Candidate causes,
in rough order of likelihood:

1. **Overlapping triggers** in `.github/workflows/ci.yaml` — e.g. both `push` and
   `pull_request` firing for a branch that also has an open PR. Note that both observed
   runs reported `event: pull_request`, which argues *against* the simple
   push+pull_request overlap and is the first thing to explain.
2. **A second workflow file** that also declares a job named `Build` (there is already
   a lowercase `build` check distinct from `Build`, from `Deploy Site`) — check whether
   two workflows both contribute a `Build` context.
3. **A re-dispatch or matrix expansion** producing a second run record.

Likely fix once identified: a `concurrency` group with `cancel-in-progress: true` keyed
on the ref, and/or narrowing the trigger set. Verify by pushing a trivial commit and
confirming exactly one `CI` run appears for the SHA.

Do NOT "fix" this by removing a required check from the ruleset — the checks are
correct; the duplication is the problem.

## Context

Surfaced 2026-08-16 while fixing #4986 (E2E D-110 extra list call) on PR #4984.
Related: #4987 (the bits-ui `Build` flake this amplified).
