<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Enforce Conventional Commits via CI PR-Title Check, Not Local Hooks

**Date:** 2026-05-24
**Status:** Accepted
**Decision:** holomush-u2exm
**Deciders:** HoloMUSH Contributors

## Context

The repo is developed primarily via `jj`, which does not fire `lefthook` git hooks
reliably. All PRs are squash-merged, and the repo is configured so the squash
commit subject is the PR title (`squash_merge_commit_title = PR_TITLE`). The only
conventional-commit enforcement was a `lefthook` `cog verify` commit-msg hook —
which, under `jj`, fires rarely or never. It is also moot for the landing commit:
the squash subject is composed in the GitHub UI at merge time and never seen by a
local hook. Conventional-commit validation was therefore *de facto absent*, which
is how commit subjects that broke release-please's version derivation reached
`main`.

## Decision

Conventional-commit validation is enforced by a dedicated
`.github/workflows/commit-lint.yaml` CI workflow that runs `cog verify` on the PR
title for every `pull_request` to `main`. The `lefthook` commit-msg hook is
retained as best-effort local feedback only; CI is the authoritative gate.

## Rationale

- `jj` bypasses `lefthook`, so a local commit-msg hook cannot be the authoritative
  gate. This is the structural reason validation must live in CI.
- Squash-merge sets the commit subject from the PR title, so PR-title validation
  is equivalent to commit-subject validation — which is exactly what `cog bump`
  parses for version derivation.
- It MUST be a separate workflow, not a job in `ci.yaml`: `ci.yaml`'s
  `pull_request` trigger carries `paths-ignore` (docs, markdown, `.claude/**`),
  which would silently skip docs-only PRs — yet a docs-only PR still lands a
  commit subject on `main`. `commit-lint.yaml` has no `paths-ignore`.

## Alternatives Considered

**Option A: Continue with the `lefthook` commit-msg hook (local enforcement)**

| Aspect     | Assessment                                                                                                |
| ---------- | --------------------------------------------------------------------------------------------------------- |
| Strengths  | Fast local feedback before push                                                                           |
| Weaknesses | `jj` doesn't fire `lefthook` reliably; squash subject (PR title) is never seen locally — false assurance  |

**Option B: CI job on the PR title (chosen)**

| Aspect     | Assessment                                                                                                      |
| ---------- | --------------------------------------------------------------------------------------------------------------- |
| Strengths  | Fires on every PR regardless of VCS tooling; PR title is the squash subject, so it governs the landed commit    |
| Weaknesses | No local fast-fail; a CI round-trip; can't be a `ci.yaml` job (its `paths-ignore` would skip docs-only PRs)      |

## Consequences

**Positive:**

- Every commit subject on `main` is a valid conventional commit (INV-5), so
  `cog bump` derives versions reliably — closing the parse-failure class that
  burned release-please.
- No local-tooling dependency for the authoritative gate.

**Negative:**

- No local fast-fail; contributors get conventional-commit feedback only after
  opening a PR.
- The `lefthook` hook becomes best-effort, which may surprise contributors who
  assumed it was authoritative (documented inline in `lefthook.yaml`).

**Neutral:**

- The existing `cog` binary is reused (a pinned-binary composite action); no new
  tooling is introduced.

This decision establishes the pattern — "validation moves to CI because local
hooks are unreliable under `jj`" — that the broader `lefthook` → `jj fix`
migration (`holomush-gcio6`) generalizes.

## References

- [Cocogitto Release Tooling Design — §Problem, §Component 3, INV-5](../superpowers/specs/2026-05-24-cocogitto-release-tooling-design.md)
- lefthook → jj fix migration: bd issue `holomush-gcio6` (generalizes this pattern; `bd show holomush-gcio6`)
