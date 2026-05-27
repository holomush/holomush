<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Drop in-memory session.Store fake; test against real Postgres

**Date:** 2026-05-23
**Status:** Accepted
**Decision:** holomush-bozv
**Related:** holomush-9mxr (design bead)
**Deciders:** Sean Brandt

## Context

`session.Store` (defined in `internal/session/session.go`) had two
implementations:

1. **`store.PostgresSessionStore`** (`internal/store/session_store.go`,
   811 lines) — the production implementation, also used in true
   integration tests.
2. **`session.MemStore`** (`internal/session/memstore.go`, 552 lines) —
   an in-memory implementation used as a test fake by 13 test files
   across `internal/grpc/`, `internal/grpc/focus/`,
   `internal/command/handlers/`, `internal/auth/`, and one integration
   test that incongruously chose the fake (`test/integration/phase1_5_test.go`).
   86 test sites total.

Maintaining two implementations created three problems:

1. **Drift risk.** Both impls had to be kept aligned on a ~20-method
   interface. The interface itself documents drift: `ListByPlayer`
   carries a `TODO: filter by playerID when player-character relationship
   table exists` that both impls satisfy by degraded behavior in
   lockstep.
2. **Test fidelity.** Handler tests passing against MemStore can mask
   real `PostgresSessionStore` bugs — the same class of incident as
   the `privacytest` harness wiring `allowAllPolicyEngine` and missing
   `LocationProvider` from `BuildABACStack` for six weeks (precedent
   memory: `feedback_privacytest_bypasses_abac`).
3. **Maintenance cost.** 552 lines of MemStore existed solely for
   test fixtures. The original "testcontainers are too slow for unit
   tests" problem MemStore solved is solved differently today:
   `testutil.SharedPostgres(t)` (`test/testutil/postgres.go:50`) runs
   one container per test binary, and `testutil.FreshDatabase(t, env)`
   creates per-test databases via pre-migrated template copy in
   ~50-150ms per test.

The repo's existing convention, observed across all current
SharedPostgres-using test files (`internal/store/events_audit_test.go`,
`internal/content/postgres_store_test.go`,
`internal/access/policy/store/postgres_integration_test.go`,
`cmd/holomush/crypto_operator_validation_test.go`), is to gate
SharedPostgres tests behind `//go:build integration`. This means
`task test` (the inner loop) does not compile them — only `task test:int`
does. That convention left no place for "unit tests that want a real
session.Store" to live.

## Decision

Delete `internal/session/memstore.go` (552 lines) and
`internal/session/memstore_test.go` (856 lines) entirely. Establish a
new helper package `internal/testsupport/sessiontest` exporting
`NewStore(t *testing.T) session.Store` that wraps `testutil.SharedPostgres`,
`testutil.FreshDatabase`, and `store.NewPostgresSessionStore`. Convert all
86 sites to use the helper.

The `sessiontest` package deliberately does **not** carry
`//go:build integration`. This is the documented exception to the repo
convention: session-store-touching handler tests in `internal/grpc/`,
`internal/grpc/focus/`, `internal/command/handlers/`, and `internal/auth/`
will run under `task test` and require Docker to do so. Developers
without Docker will see runtime container-start errors at test execution,
not compile-link failures.

After this decision, `session.Store` has exactly one implementation
in the repository.

## Rationale

- **Drift surface eliminated.** With one impl, semantic divergences (such
  as the `ReattachCAS` SESSION_NOT_FOUND-vs-`(false, nil)` divergence
  surfaced in spec analysis, at `memstore.go:173-176` vs
  `session_store.go:428`) cannot exist by construction.
- **Fidelity matches production.** Handler-logic tests exercise the real
  SQL paths that production callers hit, including transaction
  semantics, `pgx` error wrapping, and `oops.Code` propagation through
  the Postgres-specific layer.
- **Maintenance burden removed.** 1408 lines of test-only code go away
  with no production impact.
- **Shared container economics are good enough.** `SharedPostgres` runs
  one container per test binary; `FreshDatabase` is template-copy
  (~50-150ms per test). Estimated inner-loop slowdown across the four
  affected packages: ~5 seconds total. Acceptable.
- **The convention exception is narrow and documented.** Only one
  package (`internal/testsupport/sessiontest`) breaks the
  `SharedPostgres = //go:build integration` pattern, and only one
  subsystem (session-touching handler tests) consumes it. The exception
  is documented in this ADR, the design spec, `CLAUDE.md`, and
  `site/docs/contributing/integration-tests.md`.

## Alternatives Considered

### Keep `MemStore` and add a `storetest.Run(t, ctor)` conformance suite

Both implementations would prove they conform to the same contract
via a parameterized test suite. Drift risk would be locked down without
removing the inner-loop-fast fake.

Rejected because:

- Solves the drift problem but does not solve the maintenance or
  fidelity problems.
- Doubles the contract-maintenance surface (the conformance suite
  itself becomes a third artifact to keep aligned).
- Hides interface-level limitations (the `ListByPlayer` TODO) by making
  "both impls degraded equally" feel intentional.

### Move all 86 sites behind `//go:build integration`

Preserve MemStore's deletion and migration to Postgres while keeping
the repo convention pristine.

Rejected because:

- `task test` would silently skip the 86 converted tests, eliminating
  inner-loop regression coverage on auth-handler logic — a worse
  outcome than the original problem.
- "Skip a test class from the inner loop" is a coverage regression that
  rarely gets fixed once accepted.

### Keep MemStore as the test fake; do nothing

Status quo. Accept ongoing drift risk and the documented degraded
contract.

Rejected because the privacytest precedent demonstrated that drift
between fake and real implementations can hide bugs for weeks. The
maintenance cost was a sunk cost; the fidelity cost was ongoing.

## Consequences

### Positive

- Single implementation of `session.Store` eliminates the drift surface.
- 1408 lines of test-only code deleted.
- Future bugs in `PostgresSessionStore` are caught at unit-test time
  in four packages, not deferred to integration tests.
- The convention exception is documented; future contributors can
  reach for `sessiontest.NewStore(t)` confidently when adding new
  session-touching tests.

### Negative

- `task test` now requires Docker in four packages
  (`internal/grpc/`, `internal/grpc/focus/`, `internal/command/handlers/`,
  `internal/auth/`). Developers without Docker were already partially
  blocked (`task pr-prep` already requires it), but the friction now
  extends to the inner loop in this subsystem.
- The `//go:build integration` convention has a documented exception,
  which contributors must learn. Risk: someone notices `sessiontest`
  lacks the tag and adds it to other tests, breaking the inner loop
  more broadly. Mitigation: this ADR plus the CLAUDE.md and
  `integration-tests.md` updates that ship with the implementation.

### Neutral

- A `storetest.Run(t, ctor)` conformance suite remains a documented
  out-of-scope follow-up if a third `session.Store` implementation is
  ever proposed (Redis, distributed, etc.). The decision to skip it
  now is not load-bearing for that future scenario.
