<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-w1a26; do not edit manually; use `/adr update holomush-w1a26` -->

# Narrow the subagent model-floor to a haiku carve-out

**Date:** 2026-07-03
**Status:** Accepted
**Decision:** holomush-w1a26
**Deciders:** Sean Brandt

## Context

`.claude/rules/subagent-briefing.md` stated a blanket sonnet floor for all dispatched sub-agents ("never haiku for sub-agents"). The token/cost/latency optimization design (holomush-wagqb) introduces per-agent model-tier selection and needs a rule that captures cost wins on genuinely mechanical work without letting an unverified low-judgment model drive decisions the caller acts on.

## Decision

Replace the blanket sonnet floor with a narrow exception: `haiku` is permitted ONLY for agents whose output is schema-constrained AND independently verified downstream. Unverified-judgment agents (test triage, review, flake-vs-real) keep the sonnet floor; all reviewer-tier agents (code / crypto / abac / design / plan) stay `opus`/`fable` regardless of cost. Prefer `effort: low` on a sonnet agent over dropping to haiku.

## Rationale

- Reviewer-tier misses (security / design defects) cost orders of magnitude more than the tokens saved by downgrading.
- `effort: low` on sonnet usually beats a haiku downgrade on both cost and reliability, and `effort` errors on Haiku 4.5 — choosing haiku forecloses the effort dial.
- Verify-cheap / judge-expensive fan-out is the only shape where haiku's judgment risk is contained by a downstream check.

## Alternatives Considered

- **Blanket sonnet floor (prior rule) (rejected):** simple and safe, but misses cost/latency wins on mechanical, downstream-verified fan-out work.
- **Broadly permit haiku wherever cost matters (rejected):** maximizes savings but risks unverified low-judgment output driving caller decisions; also loses the effort dial.
- **Narrow carve-out (chosen):** haiku only if schema-constrained + downstream-verified; reviewers fixed opus/fable; prefer effort:low on sonnet.

## Consequences

- Positive: a bounded, auditable rule prevents ad-hoc unverified-haiku creep into review/judgment paths.
- Negative: requires a per-agent judgment call at definition time (is the output verified downstream?).
- Neutral: no agent in the initial rollout uses haiku — local-test/pr-prep/build/lint are all sonnet.
