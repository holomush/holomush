<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-cr3gq; do not edit manually; use `/adr update holomush-cr3gq` -->

# Run-and-distill contract for local-* offload agents

**Date:** 2026-07-03
**Status:** Accepted
**Decision:** holomush-cr3gq
**Deciders:** Sean Brandt

## Context

Verbose command output (`task test`, `task pr-prep`, `task build`, `task lint`) is a major driver of MAIN-thread context growth (2–10K+ tokens per invocation for tests; `task lint` fans out across many linters × files). Growth in the main thread drives compaction and raises per-turn cost.

## Decision

Add a class of `local-*` offload agents (`local-test`, `local-pr-prep`, `local-build`, `local-lint`) that run their `task` command in an isolated sub-context and return ONLY a fixed compact verdict schema (verdict / summary / failures / truncated_note) — never raw output. Any future `local-*` agent MUST follow the same contract: schema-return, read-only-briefed (no Edit/Write in `tools`), `sonnet` model floor with `effort: low`, and never bundled in a parallel tool batch with other maybe-failing calls.

## Rationale

- The offload win SCALES with the MAIN session's model cost: on an opus/fable main session even short `build`/`lint` output is expensive to ingest into the main context, while the subagent distills at cheaper sonnet rates — so the break-even output size drops below where build/lint sit, and all four agents pay off.
- `local-pr-prep` is iteration/triage only: the parent MUST re-run `task pr-prep` itself before the real push, because a sub-agent cannot surface schema-regeneration side-effects.
- Bundling a `local-*` call in a parallel batch with other maybe-failing calls risks a cancel-storm, so the contract forbids it.

## Alternatives Considered

- **Run verbose commands inline in the main thread (status quo) (rejected):** simplest, but every invocation grows main-thread context 2–10K+ tokens, driving compactions.
- **test + pr-prep only, defer build/lint (rejected):** initially chosen for "short output," but reversed — the main-session-model-cost scaling makes build/lint worth offloading on opus/fable sessions at ~zero marginal cost.
- **Dispatch a sonnet, effort:low sub-agent returning a strict schema, read-only-briefed, never batched (chosen).**

## Consequences

- Positive: a reusable, auditable pattern for any verbose-command offload, keeping main-thread context flat regardless of command output size.
- Negative: does not reduce TOTAL system token spend (the command still runs and is read in the sub-context); adds a class of agents whose verdict must never be trusted unverified before a push.
- Neutral: applies uniformly to any later `local-*` agent.
