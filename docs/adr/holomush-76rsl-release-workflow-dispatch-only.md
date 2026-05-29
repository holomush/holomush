<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Release Workflow is Dispatch-Only

**Date:** 2026-05-29
**Status:** Accepted
**Decision:** holomush-76rsl
**Deciders:** HoloMUSH Contributors

## Context

`holomush-4mmzy` decided to cut releases inline in one `workflow_dispatch` run
and **retained `on: push: tags: 'v*'` only as a fallback for human-pushed local
tags**. Its Context reasoned that this fallback was harmless because a tag pushed
by a workflow authenticated with the default `GITHUB_TOKEN` does not re-trigger
`on: push: tags` — GitHub suppresses workflow-to-workflow triggering to prevent
recursion.

That analysis missed the credential the dispatch path actually uses. On the
dispatch path GoReleaser creates the remote tag with a **GitHub App token**
(`RELEASE_APP_ID` / `RELEASE_APP_PRIVATE_KEY`), needed to bypass `v*`
tag-protection. App-token (and PAT) pushes are **not** subject to the
`GITHUB_TOKEN` recursion guard — they re-trigger workflows normally.

The result, observed on the `v0.5.0` cut (2026-05-29):

| Run | Trigger | Actor | Outcome |
| --- | --- | --- | --- |
| [26648759017](https://github.com/holomush/holomush/actions/runs/26648759017) | `workflow_dispatch` | `seanb4t` | success — the real release |
| [26648915779](https://github.com/holomush/holomush/actions/runs/26648915779) | `push` (tag `v0.5.0`) | `holomush-release-bot[bot]` | failure |

The duplicate `push` run re-ran GoReleaser against the tag the dispatch run had
already published, failing with `422 Validation Failed … Code:already_exists` on
the release assets. Every dispatched release produced a spurious red run.

## Decision

The release workflow runs on `workflow_dispatch` **only**. The
`on: push: tags: 'v*'` trigger is removed. The now-unconditional dispatch path
also sheds its dual-path scaffolding (the per-step
`if: github.event_name == 'workflow_dispatch'` guards, the `GITHUB_REF_NAME`
branch in version resolution, and the `|| secrets.GITHUB_TOKEN` GoReleaser token
fallback).

The inline single-run decision of `holomush-4mmzy` (INV-1, INV-8) is **unchanged
and reinforced** — this ADR reverses only that ADR's fallback-trigger sub-clause.

## Rationale

- The fallback was not harmless: the App-token tag creation it relied on
  re-triggers the very workflow it was meant to back up, turning a "fallback"
  into a guaranteed duplicate failing run on every release.
- Building from `main` at dispatch time is as correct and repeatable as building
  "on the tag": the job pins one immutable checkout, `cog bump` mints the local
  tag at that exact SHA, and GoReleaser builds that same tree and points the
  remote tag at it via `release.target_commitish`. Tag and build commit are
  identical and created atomically in one job — there is no drift window that a
  tag-checkout would close.
- `task release:cut` already invokes `gh workflow run release.yaml`
  (`workflow_dispatch`), so the canonical release entry point is unaffected.
- A failed or partial release can still be rebuilt by re-running the recorded
  workflow run (`gh run rerun`), which replays the same SHA — so dropping the
  hand-pushed-tag entry point loses no rebuild capability.

## Alternatives Considered

**Option A: Keep `on: push: tags` and guard the job against the release bot**

Add `&& github.actor != 'holomush-release-bot[bot]'` to the job `if:` so the
self-triggered run no-ops while a genuine human-pushed tag still builds.

| Aspect | Assessment |
| --- | --- |
| Strengths | Preserves the human-pushed-tag entry point |
| Weaknesses | Keeps the dual-path conditional complexity `4mmzy` already flagged as a negative; couples the workflow to a specific bot login; the entry point is unused in practice (`task release:cut` always dispatches) |

**Option B: Dispatch-only (chosen)**

| Aspect | Assessment |
| --- | --- |
| Strengths | Eliminates the duplicate run at the source; removes all dual-path conditionals, simplifying the workflow; matches actual usage |
| Weaknesses | A hand-pushed `v*` tag no longer starts a release — mitigated by `gh run rerun` and by always cutting via `task release:cut` |

## Consequences

**Positive:**

- No more spurious failing `push`-triggered run per release; the dispatch run is
  the single source of truth for a cut.
- The workflow loses its dual-path branches and reads as a straight-line
  dispatch pipeline.

**Negative:**

- Pushing a `v*` tag by hand no longer triggers a release. Cuts go through
  `task release:cut`; ad-hoc rebuilds go through re-running the workflow run.

## References

- Supersedes: [`holomush-4mmzy`](holomush-4mmzy-inline-single-run-release.md) —
  inline single-run release; this ADR reverses only its `on: push: tags`
  fallback sub-clause.
- [Cocogitto Release Tooling Design — INV-1, INV-8, Component 3b](../superpowers/specs/2026-05-24-cocogitto-release-tooling-design.md)
- [GitHub Actions: events that trigger workflows — token-based recursion suppression](https://docs.github.com/en/actions/using-workflows/triggering-a-workflow#triggering-a-workflow-from-a-workflow)
