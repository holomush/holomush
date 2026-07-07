<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-zcyvb; do not edit manually; use `/adr update holomush-zcyvb` -->

# Merge local-test/lint/build offload agents into one local-check agent

**Date:** 2026-07-06
**Status:** Accepted
**Decision:** holomush-zcyvb
**Deciders:** Sean Brandt

## Context

holomush-wagqb shipped four fixed local-* run-and-distill agents (local-test, local-pr-prep, local-build, local-lint). Every agent description is injected into every session's system prompt, so four near-identical contracts triple the always-paid description budget, and the choice of four names adds routing friction for the dispatching model.

## Decision

Create `.claude/agents/local-check.md`, parameterized by check kind (test | lint | build | int | cover, adding `task test:int` and `task test:cover` coverage), and delete `local-test.md`, `local-lint.md`, `local-build.md`. `local-pr-prep` is retained as a separate agent with a description-only rewrite. This does NOT supersede ADR holomush-cr3gq: its no-parallel-batching rule (never bundle an offload-agent call with other maybe-failing calls) survives the merge unchanged — only the agent enumeration in prose needed updating.

## Rationale

- The three merged agents share an identical contract; only the wrapped `task` command differs.
- local-pr-prep's contention protocol, result-file reading, and advisory-only semantics do not compose into the same schema.
- One obvious dispatch target beats three near-synonyms when a hook deny message must name the replacement.

## Alternatives Considered

- **Merge test/lint/build only (chosen):** cuts description budget; preserves the run-and-distill contract verbatim; pr-prep's distinct protocol stays isolated.
- **Merge all four (rejected):** pr-prep's advisory-only caveat and contention protocol muddy the shared contract.
- **Keep all four (rejected):** triples the per-session description cost for functionally identical contracts.

## Consequences

- Positive: repo-agent description budget drops (part of the ~600 to ~350 word per-session reduction).
- Negative: prose references to the old agent names (subagent-briefing.md, deny messages) must be updated in the same change.
- Neutral: the run-and-distill contract — strict compact verdict, exit-code-first, read-only, sonnet/effort:low, no batching with maybe-failing calls (holomush-cr3gq) — is unchanged.
