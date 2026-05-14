---
name: workflow_run-skipped-upstream
description: workflow_run cannot fire for path-skipped upstream — required when spec proposes paths-ignore aggregator
metadata:
  type: feedback
---

When a spec proposes a `workflow_run`-triggered aggregator workflow to
restore branch-protection coverage for `paths-ignore`-skipped upstreams,
the design is broken as stated. A path-skipped workflow produces zero
`workflow_run` events (no `requested`, no `in_progress`, no `completed`)
because no run was ever created.

**Why:** GitHub docs "Triggering a workflow" explicitly: skipped checks
remain in `Pending`. The `workflow_run` event activity types all gate
on a run existing.

**How to apply:** When reviewing any spec that combines `paths-ignore`
on a required workflow with a `workflow_run`-triggered status
aggregator, flag it BLOCKING. The known-working patterns are:

1. "Same-name skip workflow" — a sibling workflow with the OPPOSITE
   `paths`/`paths-ignore` filter, using the SAME job names so GitHub
   treats them as the same check (canonical pattern per GH docs and
   tsuji.tech 2026-03-07).
2. Single always-on workflow that uses `dorny/paths-filter` to
   conditionally run the expensive jobs.

A `pull_request`-triggered aggregator that introspects the `CI`
workflow status via API works but is a fundamentally different design
than `workflow_run` and must be specified as such.

**Companion gotcha:** rollouts that add a `CI Required` aggregator
ALONGSIDE the legacy required checks before deploying `paths-ignore`
have a chicken-and-egg deadlock: any docs-only PR opened between
"protection updated" and "legacy checks removed" stalls on the legacy
checks the paths-ignore prevents from reporting. Verify the rollout
sequence orders the protection-settings swap atomic with (or BEFORE)
the `paths-ignore` deployment.

Seen 2026-05-14 in `pr-prep-docs-fast-lane-design`.
