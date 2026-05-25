<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Release and Deploy as Two Independent Dispatch Gates

**Date:** 2026-05-24
**Status:** Accepted
**Decision:** holomush-dgsts
**Deciders:** HoloMUSH Contributors

## Context

The original `release.yaml` triggered both the GoReleaser build and the sandbox
deploy in the same run, coupling release-cutting to deployment. Because a release
auto-deploys to the production sandbox, a fully-automatic release model would mean
every merge ships to prod — undesirable. The team wants explicit, separate control
over *when a release is cut* and *when it is deployed*.

## Decision

Release cadence and deploy cadence are independent knobs. Deploy is
`workflow_dispatch`-only and lives in a dedicated `.github/workflows/deploy.yaml`.
The release workflow MUST NOT trigger the deploy job; a tag push or release
dispatch cuts only the release (build/sign/publish/notes). Deploying a release is
a separate, explicit dispatch that takes the target tag as input.

## Rationale

- Once the GoReleaser job also triggers on `workflow_dispatch`, leaving
  `deploy-sandbox` in `release.yaml` would make a single dispatch fire *both*
  release and deploy — defeating the two-gate goal. Moving deploy to its own
  workflow is the literal implementation of "two gates" (INV-4).
- Decoupling lets any previously-released tag be deployed independently (staged
  rollouts, redeploys, validating a release before shipping it).
- A failed deploy no longer affects the release artifact, and vice versa.

## Alternatives Considered

**Option A: Single workflow — tag push triggers release + auto-deploy**

| Aspect     | Assessment                                                                                                                |
| ---------- | ------------------------------------------------------------------------------------------------------------------------- |
| Strengths  | Fewer manual steps; one workflow file                                                                                     |
| Weaknesses | Couples two distinct operations; a release that ships but fails to deploy leaves ambiguous state; can't deploy a prior tag |

**Option B: Two independent `workflow_dispatch` gates (chosen)**

| Aspect     | Assessment                                                                                          |
| ---------- | --------------------------------------------------------------------------------------------------- |
| Strengths  | Release and deploy are separate human decisions; any tag deployable independently; failures isolated |
| Weaknesses | Two manual steps from merged code to running server                                                  |

**Option C: Auto-release on merge → manual deploy approval**

| Aspect     | Assessment                                                                                              |
| ---------- | ------------------------------------------------------------------------------------------------------- |
| Strengths  | Maximal automation; every merge is releasable; promotion gated by a GitHub Environment required-reviewer |
| Weaknesses | Every merge cuts a release; no preview; more machinery than a small project needs right now              |

## Consequences

**Positive:**

- Deploy is a fully independent, auditable action; any released tag can be
  deployed without cutting a new release.
- The deploy job's `prod` environment and `deploy-sandbox` concurrency group are
  preserved.

**Negative:**

- Two dispatches are required to go from a code merge to a running server.

**Neutral:**

- The deploy job internals (SSH, Postgres snapshot, pull, migrate, restart, health
  probe) are unchanged. The `holomush-aocap` deploy bug is explicitly out of scope.

## References

- [Cocogitto Release Tooling Design — §Goals, §Components/2, INV-4](../superpowers/specs/2026-05-24-cocogitto-release-tooling-design.md)
- [Cut releases inline in one dispatched run (`holomush-4mmzy`)](holomush-4mmzy-inline-single-run-release.md)
