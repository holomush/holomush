<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Cut Releases Inline in One Dispatched Run

**Date:** 2026-05-24
**Status:** Superseded by holomush-76rsl
**Decision:** holomush-4mmzy
**Deciders:** HoloMUSH Contributors

## Context

A tag pushed by a workflow authenticated with the default `GITHUB_TOKEN` does not
trigger another workflow's `on: push: tags` event — GitHub suppresses
workflow-to-workflow triggering to prevent recursion. This is the *same* root
cause as release-please pain #1. Any design that pushes a tag first and expects a
downstream workflow to fire on that push will fail silently. Separately,
GoReleaser does not push local tags: run against a local-only tag it would fail at
the GitHub Releases API call unless told which commit to tag.

## Decision

The release workflow performs version-derivation, local tag creation
(`cog bump --auto --disable-bump-commit`), and `goreleaser release` in a **single**
`workflow_dispatch` run. GoReleaser creates the remote tag atomically as part of
publishing the Release, via `release.target_commitish: "{{ .Commit }}"` in
`.goreleaser.yaml`. The `on: push: tags: 'v*'` trigger is retained only as a
fallback for human-pushed local tags.

## Rationale

- `GITHUB_TOKEN`-pushed tags not triggering `on: push: tags` is immutable GitHub
  platform behavior; an inline run removes the cross-workflow trigger dependency
  entirely (INV-1).
- `release.target_commitish` is the GoReleaser-documented mechanism for "creating
  tags locally before running GoReleaser." It makes remote tag creation atomic
  with Release publication, so a GoReleaser failure leaves at most a *local* tag in
  the ephemeral runner — no orphaned remote tag (INV-8).
- A GitHub App token (reusing the orphaned `RELEASE_APP_ID` /
  `RELEASE_APP_PRIVATE_KEY` secrets) authenticates GoReleaser's tag/Release
  creation, giving bot attribution and bypassing any `v*` tag-protection.

## Alternatives Considered

**Option A: Push tag first, rely on `on: push: tags` to trigger GoReleaser**

| Aspect     | Assessment                                                                                                  |
| ---------- | ----------------------------------------------------------------------------------------------------------- |
| Strengths  | Clean separation; GoReleaser triggered naturally by tag creation                                            |
| Weaknesses | A `GITHUB_TOKEN`-pushed tag does NOT trigger `on: push: tags` — reproduces release-please pain #1 identically |

**Option B: Single inline dispatched run (chosen)**

| Aspect     | Assessment                                                                                            |
| ---------- | ----------------------------------------------------------------------------------------------------- |
| Strengths  | No cross-workflow trigger dependency; remote tag created atomically via `target_commitish`; no orphans |
| Weaknesses | The release workflow is more complex (conditional steps on the dispatch vs. tag-push fallback path)    |

## Consequences

**Positive:**

- INV-1 (single-run release) and INV-8 (atomic-or-recoverable cut) are both
  satisfied.
- Failure recovery is clean: a failed GoReleaser run leaves no remote state to
  unwind.

**Negative:**

- The release workflow has conditional steps that run only on the dispatch path,
  increasing its complexity versus a pure tag-triggered build.

**Neutral:**

- The `on: push: tags: 'v*'` trigger remains as a human-pushed-tag fallback path.

## References

- Superseded by: [`holomush-76rsl`](holomush-76rsl-release-workflow-dispatch-only.md) —
  drops the `on: push: tags` fallback because the App-token tag creation
  re-triggered this workflow (duplicate `422 already_exists` run). The inline
  single-run decision below is unchanged; only the fallback sub-clause is reversed.
- [Cocogitto Release Tooling Design — §Why a single inline dispatched run, §Component 3b, INV-1, INV-8](../superpowers/specs/2026-05-24-cocogitto-release-tooling-design.md)
- [Tag-only release; GitHub Release notes as canonical history (`holomush-jfb9x`)](holomush-jfb9x-tag-only-release-drop-changelog.md)
