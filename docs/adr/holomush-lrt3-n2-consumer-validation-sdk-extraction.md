<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Require N=2 Consumer Validation Before SDK Primitive Extraction

**Date:** 2026-05-16
**Status:** Accepted
**Decision:** holomush-lrt3
**Deciders:** HoloMUSH Contributors

## Context

When extracting shared primitives into a Plugin SDK, "extract once the
pattern is visible" is a well-known anti-pattern with a specific failure
mode: with only one concrete consumer, the extracted API accidentally
encodes consumer-specific assumptions as if they were general design
choices. The second consumer then either accepts a misfit API (creating
technical debt at the API boundary) or forks bespoke code anyway (defeating
the extraction's purpose).

For `theme:social-spaces` specifically: at the time the substrate-contract
spec was written, the only fully-built consumer of the candidate SDK
primitives (`membership`, `replay`, `focuswire`, `groupabac`, `cryptoemit`)
was the Scenes plugin (`plugins/core-scenes/`). Channels rework (`0sc.12`)
was in-progress but not yet adopting any shared abstraction. Extracting an
SDK from N=1 (Scenes alone) would lock in scenes-specific column names,
role vocabularies (`owner/member/invited`), focus semantics, and ABAC policy
shapes.

The discipline question: when does SDK code actually land?

## Decision

No `eventkit` or `groupkit` primitive lands as substrate code until two
distinct use plugins concretely consume it. The substrate-contract spec
names the primitives' shapes (acts as a contract), but Phase 4 of the
scenes work implements bespoke (scene-internal) versions of the
functionality. Channels rework (`0sc.12`) is the validating second
consumer. After channels rework concretely adopts the primitives,
substrate beads file to extract the validated primitive into
`pkg/plugin/eventkit/<primitive>/` or `pkg/plugin/groupkit/<primitive>/`
with Lua hostfunc parity, and the scenes plugin refactors to consume
the SDK (retroactive consolidation).

The discipline is enforced by `plan-reviewer`: a `## SDK primitive
validation` section in `0sc.12`'s eventual design spec is the required
artifact, with per-primitive verdict (`adopt as-is` / `adopt with API
tweak: <details>` / `reject as not-fit, reasoning: <details>`).
`plan-reviewer` blocks any SDK-extraction plan that lacks this section.

## Rationale

**N=1 extraction risk is well-established.** With only Scenes as consumer,
the `membership` primitive's API would assume `(scene_id, character_id,
role)` and `owner/member/invited` roles. Channels' membership might want
`(channel_id, character_id, role)` with `owner/op/member` roles. Forcing
Channels onto Scenes' role vocabulary corrupts the abstraction.

**Channels validates ALL primitives, not just membership.** The N=2
discipline applies per-primitive. Channels' brainstorm explicitly evaluates
each candidate primitive for fit and reports the verdict. A primitive can
be "adopt as-is" for one consumer and "reject as not-fit" for another —
the discipline catches this before substrate code is written.

**Deferred extraction means SDK PRs are smaller.** Each primitive's
extraction PR ships with the second consumer already wired in. The API
shape is pre-validated; the PR is mechanical (move code, add hostfunc
parity, write parity tests). No API churn post-merge.

**The artifact gate is enforceable.** `plan-reviewer` already has a
defined role and gating capability. Adding "check for `## SDK primitive
validation` section in any SDK-extraction plan" is a small extension to
its mandate (filed as follow-up to add to plan-reviewer's memory).

**Phase 4 paying the duplicate-code tax is acceptable.** Scenes Phase 4
implements bespoke versions of the primitives. After SDK extraction,
those implementations refactor to consume the SDK. The duplicated code
is bounded (Phase 4 only) and time-bounded (until channels rework
validates). The alternative (premature extraction) creates *permanent*
API debt.

## Alternatives Considered

**Option A: Extract primitives immediately when Scenes needs them (N=1)**

| Aspect     | Assessment                                                                                                                          |
| ---------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| Strengths  | Faster path to shared code; DRY sooner; no duplicate-code phase                                                                     |
| Weaknesses | API shape reflects Scenes-specific assumptions; Channels forced to accept misfit API or fork bespoke code; classic premature abstraction |

**Option B: N=2 validation discipline before extraction (chosen)**

| Aspect     | Assessment                                                                                                                                          |
| ---------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| Strengths  | API forced through two different domains before landing; misfit surfaces during design (not implementation); zero wasted SDK code; `plan-reviewer` gate is artifact-checkable |
| Weaknesses | Phase 4 ships duplicate code; SDK extraction timeline depends on channels rework cadence; documented validation artifact required                  |

**Option C: Spec the API, extract immediately, let Channels adapt**

| Aspect     | Assessment                                                                                                                          |
| ---------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| Strengths  | SDK available sooner for Channels to consume                                                                                        |
| Weaknesses | Still N=1 at extraction time; Channels adapting to misfitted API generates technical debt at the API boundary; defeats the purpose |

## Consequences

**Positive:**

- API shape validated by two domain consumers before becoming substrate
- `plan-reviewer` gate creates a formal artifact trail for each primitive's acceptance
- Eliminates API churn after extraction
- Future SDK additions (e.g., new primitives discovered post-extraction) follow the same discipline

**Negative:**

- Phase 4 ships duplicate code (bespoke membership ops, bespoke focuswire) that is later refactored
- SDK extraction timeline depends on `0sc.12` brainstorm cadence
- Requires `plan-reviewer`'s memory or definition to be updated with the artifact check (filed as follow-up)

**Neutral:**

- Retroactive consolidation beads filed under `5rh` epic after SDK lands
- Forums adopting eventkit later does not change SDK extraction timing (eventkit lands when channels validates it, not when forums does)

## References

- [Substrate Contract Spec — §3.4, INV-S7](../superpowers/specs/2026-05-16-social-spaces-substrate-contract.md)
- [Split Plugin SDK into eventkit and groupkit (`holomush-p7w0`)](holomush-p7w0-split-plugin-sdk-eventkit-groupkit.md)
