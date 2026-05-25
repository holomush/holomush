<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-5eqiv; do not edit manually; use `/adr update holomush-5eqiv` -->

# quarantine flaky specs via governed registry, not deletion

**Date:** 2026-05-26
**Status:** Accepted
**Decision:** holomush-5eqiv
**Deciders:** Sean Brandt

## Context

Promoting integration/E2E to required CI checks would immediately block merges on ~8 known-flaky specs. The project's "no rerun — investigate" policy means these cannot be re-run past; deleting them loses coverage; silently skipping them (comment-out or bare `.Skip()` without a registry) risks a roach-motel where specs check in and never leave. A mechanism was needed that (a) excludes flaky specs from gating CI, (b) still runs them somewhere, and (c) creates a forcing-function toward eventual removal.

## Decision

Quarantined specs carry an in-code marker plus a `test/quarantine.yaml` registry row. The gating exclusion for all Go integration specs is a single environment variable, `HOLOMUSH_RUN_QUARANTINED`, checked by a `quarantinetest` helper (`Skip(t, bead)` for plain-`testing.T`; `if !quarantinetest.Enabled() { Skip(...) }` for Ginkgo). Playwright uses its native `{ tag: ['@quarantine', '@<bead>'] }` + `--grep-invert`. A bijection meta-test (runs in `task test`) enforces marker↔registry-row↔bead correspondence. The nightly lane runs the quarantined set and emits a health report; `task quarantine:audit` checks bead liveness where `bd` is reachable.

## Rationale

- The env-gate is the only mechanism that works uniformly across plain-`testing.T` and Ginkgo integration packages: `task test:int` runs `go test ./...` across both, and a Ginkgo `--ginkgo.label-filter` passed to `go test ./...` fails with "flag provided but not defined" on non-Ginkgo packages.
- The bijection meta-test is the hard, always-on forcing-function — a marker without a registry row (or vice versa) breaks the build on every PR.
- The nightly run surfaces "passing N consecutive nights" un-quarantine candidates without blocking PRs.
- A bead id in every marker makes each quarantined spec self-documenting and traceable to a fix bead.

## Alternatives Considered

- **Delete flaky specs until fixed.** Rejected: coverage lost, no audit trail, may never be re-added, violates "investigate, don't skip."
- **Ginkgo `--label-filter` to exclude flakes from gating runs.** Rejected: cannot be the single mechanism because `go test ./...` spans non-Ginkgo packages that don't register the flag.
- **Env-var gate + registry + bijection meta-test + nightly lane (chosen).** Uniform across Go stacks; Playwright uses native tag+grep-invert; registry + meta-test make the set auditable and self-limiting.

## Consequences

**Positive:** flaky specs are excluded from gating CI without deletion or silent skip; the registry + meta-test create an auditable, self-documenting set that cannot silently grow or shrink; the nightly health report drives burn-down signal without requiring it.

**Negative:** a new `internal/testsupport/quarantinetest` package must be depguard-denied from production; bead-liveness (INV-3) cannot be a CI hard-gate because the Dolt shared-server is not on ephemeral runners — it is an advisory local/pre-`bd close` audit; three marker idioms (plain-Go, Ginkgo, Playwright) must be documented and kept in sync.

**Neutral:** Ginkgo `Label("quarantine", ...)` is optional reporting metadata, not the gate; the quarantine seed list is re-derived from open flake beads at execution time, not frozen in the spec.
