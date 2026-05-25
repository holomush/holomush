<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Reserve "E2E" for Playwright; Ginkgo `test:int` Is Full-Stack Integration

**Date:** 2026-05-25
**Status:** Accepted
**Decision:** holomush-1r2hp
**Deciders:** HoloMUSH Contributors
**Related:** holomush-1eps2

## Context

"E2E" had three incompatible meanings in the repo: `CLAUDE.md` called the
Ginkgo `test:int` suite "E2E", the JetStream design spec (2026-04-18 §8) named
its top Go tier "E2E", and `pr-prep.md` + CI used "E2E" exclusively for the
Playwright browser suite. The collision made it impossible to state a tier
boundary unambiguously in docs, rules, or ADRs.

## Decision

"E2E" is reserved for tests that cross the **real user boundary** (browser or
telnet) against a running binary — run by `task test:e2e` (Playwright). The
Ginkgo `task test:int` suite is **integration**, with named tiers:
unit → bus-integration → audit-integration → full-stack integration (all Go,
embedded NATS, `//go:build integration`). All repo documentation adopts this
vocabulary; INV-4 keeps it from regressing (documented convention, optionally a
CI grep-lint per the plan).

## Alternatives Considered

### A — Reserve "E2E" for the real-user boundary; rename the Go tier (chosen)

Aligns with the standard industry definition (E2E = real client, real binary).
Makes the tier label informative: a contributor infers the test's dependencies
from its name. Requires a one-time doc migration.

### B — Retain "E2E" as a synonym for any high-fidelity in-process test

No migration, but perpetuates the three-way collision and makes "E2E" useless
as a signal — the label no longer tells you whether a browser, a binary, or
only an in-process `CoreServer` is involved.

## Rationale

- Boundary-crossed, not breadth-of-stack, is the defining property of E2E. The
  Ginkgo harness stands up a `CoreServer` in-process and calls Go/gRPC APIs
  directly — integration by any standard definition.
- The collision actively blocked accurate documentation: `testing.md` could not
  assert a true invariant without disambiguating which "E2E" it meant.
- Reserving a word for one meaning shapes every future test file and ADR — a
  cross-cutting vocabulary decision.

## Consequences

**Positive:** tier name implies dependencies; docs carry consistent vocabulary;
INV-4 prevents regression.

**Negative:** one-time migration of `CLAUDE.md`, `testing.md`, the JetStream
spec §8, and `integration-tests.md`; contributors must unlearn "E2E = Ginkgo".

**Neutral:** the Playwright suite and `task test:e2e` are unchanged — only the
label on the Ginkgo suite changes.

## Implementation

See `docs/superpowers/plans/2026-05-25-test-tier-taxonomy.md` Task 7.

## References

- Spec: `docs/superpowers/specs/2026-05-25-test-tier-taxonomy-design.md` §4
- `docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md` §8 (origin of the 4-tier model)
