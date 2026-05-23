<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Remove `session.MemStore` — Design

**Bead:** `holomush-9mxr`
**Status:** Proposed
**Author:** Sean Brandt (with Claude)
**Date:** 2026-05-23

## Summary

Delete `internal/session/memstore.go` (552 lines) and
`internal/session/memstore_test.go` (856 lines) — 1408 lines total.
Single source of truth for `session.Store` becomes `store.PostgresSessionStore`.
All ~86 test sites that currently instantiate `session.NewMemStore()` are
converted to a new `internal/testsupport/sessiontest.NewStore(t)` helper that
wraps the existing `testutil.SharedPostgres` + `testutil.FreshDatabase`
infrastructure.

Tests in `internal/grpc/`, `internal/grpc/focus/`, `internal/command/handlers/`,
and `internal/auth/` gain a Docker dependency for `task test`. This is an
explicit convention shift from the current "SharedPostgres tests MUST be
`//go:build integration`" pattern: a narrow class of unit tests (those
exercising session.Store-touching handler logic) will run against a real
Postgres without the integration build tag.

## Motivation

Three problems compound:

1. **Drift risk.** Two implementations of `session.Store` means two contracts
   to keep aligned. The interface itself documents drift: `ListByPlayer` carries
   a `TODO: filter by playerID when player-character relationship table exists`
   that both impls satisfy by degraded behavior in lockstep
   (`internal/session/session.go:16-17`, `internal/session/memstore.go:89-90`).
2. **Test fidelity.** Handler tests passing against MemStore can mask real
   PostgresSessionStore bugs — the same class of incident as the
   `privacytest` harness wiring `allowAllPolicyEngine` and missing
   `LocationProvider` from `BuildABACStack` for six weeks
   (precedent: `feedback_privacytest_bypasses_abac`).
3. **Maintenance cost.** 552 lines of `internal/session/memstore.go` exist
   solely for test fixtures. The testcontainer cost problem they originally
   solved is solved differently today (`testutil.SharedPostgres` runs one
   container per test binary; `FreshDatabase` does per-test template-copy).

## Non-goals

- No changes to the `session.Store` interface itself. The `ListByPlayer`
  TODO and other interface-level questions are out of scope.
- No changes to `PostgresSessionStore` behavior unless drift bugs surfaced
  by the migration force them.
- No conformance-test scaffolding. Under this approach MemStore is gone;
  drift bugs surface as test failures during conversion and are fixed inline.
- No changes to the integration-test harness (`internal/testsupport/integrationtest`).
- No changes to production session-store wiring (already PostgresSessionStore).

## Approach

Single PR composed of five commits, each independently passing tests:

1. **Test consolidation pass.** Apply table-driven patterns to
   `internal/grpc/auth_handlers_test.go` (60 tests → ~35), and similar
   reductions in `internal/grpc/focus/*_test.go`,
   `internal/command/handlers/*_test.go`. Still using MemStore. Pure
   refactor; no behavior change. Each table-driven test case MUST carry
   a code comment listing the pre-consolidation test function names it
   replaces (e.g., `// replaces: TestCreatePlayer_UsernameTaken,
   TestCreatePlayerReturnsSanitizedMessageForUsernameTaken`). This
   gives reviewers (and INV-M-4) a concrete anchor for behavioral-
   coverage attestation.
2. **Add `sessiontest.NewStore` helper.** New package
   `internal/testsupport/sessiontest/` exporting:

   ```go
   func NewStore(t *testing.T) session.Store
   ```

   Wraps `testutil.SharedPostgres(t)` + `testutil.FreshDatabase(t, env)` +
   `pgxpool.New` + `store.NewPostgresSessionStore(pool)`. No callers yet.
   The package has **no** `//go:build integration` tag — it is intentionally
   importable from unit-tagged tests, since that is the whole point of
   the design. Devs without Docker who run `go vet ./...` will see runtime
   container-start failures (not compile-link failures) — documented in
   the commit-5 docs update.
3. **Convert call sites.** Replace every `session.NewMemStore()` with
   `sessiontest.NewStore(t)`. This is where drift bugs surface; fix them
   in `PostgresSessionStore` (the keeper) or in test expectations,
   whichever is wrong. Also fixes `test/integration/phase1_5_test.go:497`,
   which incongruously uses MemStore inside an integration test.

   **Known drift surfaces** (anticipate failures here; the list is not
   exhaustive):
   - `ReattachCAS` on missing session: MemStore returns `SESSION_NOT_FOUND`
     (`memstore.go:173-176`); Postgres returns `false, nil` (rows-affected
     == 0, `session_store.go:428`). Production callers gate with a prior
     `Get` (`server.go:826`) so the divergence is masked at the handler
     layer, but tests that call `ReattachCAS` directly will need updates.
   - `UpdateLocationOnMove` ordering / atomicity: MemStore iterates an
     in-memory map (`memstore.go:483`); Postgres does a single
     `UPDATE ... WHERE`. Tests asserting ordering across multiple sessions
     for the same character may need rewriting.
   - `UpdateFocusMemberships` mutator-error atomicity: MemStore reverts
     cleanly under in-process locking (`memstore.go:448-453`); Postgres
     needs transaction discipline. Verify the existing impl.
4. **Delete MemStore.** Remove `internal/session/memstore.go` and
   `internal/session/memstore_test.go`. `focus_mutator_test.go` survives
   (its FocusMutator-specific assertions are kept; the store fixture
   it uses is rewired in commit 3).
5. **Documentation update.** `CLAUDE.md` Testing section +
   `site/docs/contributing/integration-tests.md` document the
   convention shift: `task test` now requires Docker in
   `internal/grpc/`, `internal/grpc/focus/`,
   `internal/command/handlers/`, `internal/auth/`.

### Why no conformance suite

A `storetest.Run(t, ctor)` conformance suite was considered. Under an
alternative approach that *kept* MemStore as a test fake, the conformance
suite would be load-bearing — both impls would prove they conform to the
same contract.

Under deletion, the suite is transitional scaffolding: it would run against
both impls during migration to surface drift, then run against
PostgresSessionStore alone. That's equivalent to a well-organized
`session_store_test.go`, with the added cost of building and then dismantling
the parameterization machinery. Skipped.

### Pre-deletion audit (complete)

`PostgresSessionStore.AddConnection` already enforces the same
`client_type` validation MemStore does. `validClientTypes` lives at
`internal/store/session_store.go:459-471` and matches MemStore's set
exactly (`terminal`, `comms_hub`, `telnet`). INV-M-3 is provably
satisfiable today; verify only that a test covering the rejection
path exists in `session_store_test.go` before deletion (add one if not).

## Invariants

| ID | Statement | Test |
|---|---|---|
| **INV-M-1** | After deletion, the `session.Store` interface SHALL have exactly one production implementation in the repository: `store.PostgresSessionStore`. | Meta-test: `rg "_ session\.Store = " internal/` returns one match. |
| **INV-M-2** | `sessiontest.NewStore(t)` SHALL return a fresh, isolated `session.Store` for each test invocation. Mutations in one test MUST NOT be visible to another. | Per-test: registered cleanup drops the database via `FreshDatabase`'s built-in cleanup. |
| **INV-M-3** | `PostgresSessionStore.AddConnection` SHALL reject invalid `client_type` values with the same semantics as the deleted MemStore (accept `terminal`, `comms_hub`, `telnet`; reject all others). | Test in `internal/store/session_store_test.go` covering the rejection path. Implementation already at `session_store.go:459-471`. |
| **INV-M-4** | All tests that passed before commit 3 SHALL still pass after, with equivalent behavioral coverage. Test-count reduction from consolidation is permitted; behavioral-coverage reduction is not. | Per commit-1's `// replaces:` comment requirement, every surviving test names the pre-consolidation cases it absorbs. Reviewer's source of truth for the pre-consolidation test set is the parent-of-commit-1 state of each touched `_test.go` file (i.e., `jj diff @--..@-` against commit 1). Reviewer verifies each pre-consolidation test name appears in at least one `// replaces:` comment in the post-consolidation diff. `code-reviewer` agent runs as the spot-check. |

The "`rg MemStore` returns zero matches" check is a one-time PR-level
acceptance gate (see Acceptance Criteria), not promoted to an invariant
since it has no perpetual enforcement mechanism after the PR merges.

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| Drift bugs surface during conversion | High (expected) | Treat as the work, not a setback. Fix in PostgresSessionStore (the keeper) or test, whichever is wrong. |
| Inner-loop slowdown | Medium | `FreshDatabase` is template-copy (~50-150ms typical). With ~50 sites × 100ms ≈ ~5s added across the 4 packages. If intolerable, follow-up bead investigates per-package pool sharing. |
| Docker dependency change breaks dev env | Low | `task pr-prep` already requires Docker for the full pipeline. Devs without Docker were already partially blocked. |
| `PostgresSessionStore.AddConnection` lacks `client_type` validation | Resolved | Audit complete: validation lives at `session_store.go:459-471` and matches MemStore exactly. INV-M-3 already satisfied in production. |
| Test consolidation in commit 1 introduces subtle regressions | Low | Commit 1 is pure-refactor; any behavioral change is a bug. INV-M-4's `// replaces:` comment chain gives reviewers a concrete attestation anchor. |
| Large PR review burden | Medium | Five well-scoped commits. Reviewer can walk them in order; each compiles and passes independently. |

## Out-of-scope follow-ups

These are surfaced by the work but not addressed here:

- `session.Store.ListByPlayer` TODO — interface-level question about
  player↔character relationship.
- Investigation of per-package pool sharing if inner-loop slowdown
  is unacceptable.
- A genuine `storetest.Run(t, ctor)` conformance suite if a third
  `session.Store` impl is ever proposed (Redis, distributed, etc.).

## Acceptance criteria

- `task test` passes (with Docker) in all affected packages
- `task pr-prep` green
- `rg "MemStore" internal/` returns no matches
- `rg "session\.NewMemStore" .` returns no matches
- All four invariants verified
- `code-reviewer` agent green
- Reviewer attests no behavioral-coverage loss from consolidation

## Grounding traces

See `bd show holomush-9mxr` notes for full grounding-source log:

- probe: `NewMemStore` (exact) — 13 caller files, 86 sites mapped
- probe: `SharedPostgres OR FreshDatabase` — testcontainer infrastructure mapped
- probe: `session.Store interface` — ~20 methods enumerated, drift surface identified
- probe: `PostgresSessionStore` — production wiring mapped
- correction: all current SharedPostgres-using tests are `//go:build integration` (no precedent for non-integration unit tests using SharedPostgres — this design establishes one)
- context7/deepwiki: not invoked — no new external dependencies

<!-- adr-capture: sha256=b9c268b5945d5bea; session=brainstorm-9mxr; ts=2026-05-23T13:17:32Z; adrs=holomush-bozv -->
