<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-5k6au; do not edit manually; use `/adr update holomush-5k6au` -->

# integration/E2E are CI-authoritative-and-required, local-optional

**Date:** 2026-05-26
**Status:** Accepted
**Decision:** holomush-5k6au
**Deciders:** Sean Brandt

## Context

The mandatory pre-push gate `task pr-prep` ran all CI tiers serially — including Docker-bound integration and E2E tests — behind a machine-global `flock`, taking 5–15 min per run and serializing N concurrent agent sessions at the finish line. The `protect-main` ruleset did not require `Integration Test` or `E2E Test`, so the local gate was strictly more expensive than the merge gate while being lower-fidelity (rumdl version skew, macOS vs linux/amd64, local Docker vs Testcontainers Cloud). The project owner decided `main` MUST be protected by these tiers. The open question was *where* the gate should live.

## Decision

`Integration Test` and `E2E Test` become required CI status checks on `protect-main`. The mandatory local `task pr-prep` shrinks to the cheap, deterministic, high-local-fidelity tier (schema, license, lint, fmt, unit, build, bats) with no Docker and no `flock`. Integration/E2E become CI-authoritative and local-optional (targeted `task test:int -- ./<domain>` or opt-in `task pr-prep:full`).

## Rationale

- The authoritative merge signal is CI regardless; "pr-prep green locally" was never a guarantee of "CI green" given version/OS/Docker-backend skew.
- A check should block at the cheapest point where its signal is trustworthy and restarting is cheap: cheap/deterministic checks block everywhere; slow/Docker-bound/lower-fidelity checks are CI-authoritative.
- The serial-flock contention cost scales with agent parallelism — the exact property the project optimizes for — making the status quo architecturally self-defeating.
- The `nightly-soak.yml` precedent already established shift-right for slow tiers ("`task pr-prep` stays fast").

## Alternatives Considered

- **Keep integration/E2E in the mandatory local gate (status quo).** Rejected: serial flock contention grows O(N) with agent count; local fidelity lower than CI; `protect-main` never required these tiers, so the cost was borne without proportionate safety gain.
- **Drop integration/E2E from both the local gate and required checks.** Rejected: regressions undetected until nightly; violates the owner's explicit requirement that `main` MUST be protected by these checks.
- **Move integration/E2E to CI-authoritative-and-required, local-optional (chosen).** Fast local lane (~2–3 min, no Docker/flock); CI fans work across five parallel runners (~6–8 min); authoritative, higher-fidelity merge signal.

## Consequences

**Positive:** mandatory local gate drops to ~2–3 min with no flock contention; `main` is protected by integration + E2E at the merge gate; CI signal is authoritative and higher-fidelity.

**Negative:** developers touching integration surfaces must run targeted local `test:int` proactively or wait for CI feedback; process-tooling (`branch-readiness-check`, `CLAUDE.md`, `landing-the-plane`, pr-prep/landing commands, the review-reminder hook) must be rewritten or it will actively fight the new policy.

**Neutral:** `task pr-prep:full` retains the flock and the full 9-step body as an opt-in path; the `holomush-qj5v1` result-file contract is extended with `lane=fast`, not bypassed.
