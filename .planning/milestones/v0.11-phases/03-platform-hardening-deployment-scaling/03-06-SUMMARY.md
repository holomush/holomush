---
phase: 03-platform-hardening-deployment-scaling
plan: 06
subsystem: eventbus-account-scoping
tags: [nats, external-mode, scoping, single-principal, fail-closed, security, CLUSTER-02]
status: complete
requires:
  - "03-01: eventbus.ModeExternal / Config.Mode"
  - "03-02: internal/testsupport/natstest.StartNATS / NATSEnv.URL"
  - "03-03: external-mode connect() + test/integration/eventbus_external tier"
provides:
  - "deploy/nats/holomush-server.account.conf — scoped NATS account template (events.>/audit.>/internal.>/_INBOX.>)"
  - "deploy/nats/verify-scoping.sh — external non-server denial proof (exit-code-driven)"
  - "deploy/nats/README.md — grant list + static/nsc walkthrough + both-sides verification"
  - "eventbus.VerifyAccountScoping(ctx, conn) — boot-time over-scope self-check (EVENTBUS_ACCOUNT_OVERSCOPED)"
  - "core.go external-mode boot refuses over-scoped account (EVENTBUS_SCOPE_CHECK_FAILED)"
  - "test/integration/eventbus_external/scopecheck_test.go — CI-backed both-sides scoping proof"
affects:
  - "CLUSTER-05 runbook (D-16) points operators at verify-scoping.sh + the boot self-check"
  - "compose.cluster.yaml (D-14) can carry the scoped account for the multi-node smoke"
tech-stack:
  added: []
  patterns:
    - "NATS static-account template loadable by nats-server -c AND translatable to nsc/JWT (identical allow-lists)"
    - "boot self-check reads nats.go async permissions-violation signal (SetErrorHandler + ErrPermissionViolation), fail-closed on a permitted forbidden probe"
    - "in-process nats-server (nats-server/v2/server ProcessConfigFile→NewServer→ReadyForConnections) loads the SHIPPED deploy artifact for the CI scoping proof"
    - "exit-code-driven shell verification (never grep output for a verdict; timeout(1) 124 vs permission-violation non-zero discriminates permitted-idle from denied)"
key-files:
  created:
    - deploy/nats/holomush-server.account.conf
    - deploy/nats/verify-scoping.sh
    - deploy/nats/README.md
    - internal/eventbus/scopecheck.go
    - internal/eventbus/scopecheck_test.go
    - test/integration/eventbus_external/scopecheck_test.go
  modified:
    - cmd/holomush/core.go
decisions:
  - "Static-account conf ships BOTH the scoped HOLOMUSH_SERVER principal and a minimal HOLOMUSH_VERIFY non-server identity (perms: _INBOX.> only) so verify-scoping.sh and the CI proof have a first-class denial subject; the server account still gets exactly the four prefixes and nothing else"
  - "VerifyAccountScoping detects denial via nats.go's async permissions-violation callback (SetErrorHandler) — works on the already-established boot connection; a permitted forbidden probe (no violation within a 3s window) == over-scoped == fail closed"
  - "Case B boots an in-process nats-server that loads the REAL deploy/nats conf (not a test copy), proving the shipped artifact itself scopes correctly; both Case A and Case B are Go assertions gated by task test:int (no runbook-manual fallback, W1-hardened)"
  - "No new registry invariant minted — the plan assigns no // Verifies: to scopecheck; behavior is proven by the unit + integration tests without a registry binding (no fabricated binding)"
metrics:
  duration: "~35m"
  completed: "2026-07-10"
  tasks: 3
  files: 7
---

# Phase 3 Plan 06: Single-principal account scoping (CLUSTER-02) Summary

Shipped the complementary three-part single-principal subject-scoping story for
external NATS (D-13): the `deploy/nats/` account template + external verification
script, and a boot-time self-check that refuses to start if the server's own
account is over-scoped. Single-principal is now proven from BOTH sides — the
self-check proves the server is not over-scoped, and (in the same CI-backed
integration package) a non-server credential is demonstrably denied on the three
game-topic prefixes against the shipped scoped account.

## What shipped

**Task 1 — `deploy/nats/` assets (`bdb9205a9`).** `holomush-server.account.conf`
grants the `holomush-server` account publish+subscribe on exactly `events.>`,
`audit.>`, `internal.>`, and `_INBOX.>` (the crypto invalidation reply inboxes —
`coordinator.go:253` `NewRespInbox` — without which N-of-N acks cannot return)
and nothing else; `internal.>` covers the DLQ subject
`internal.<game_id>.audit.dlq.>`. A minimal `HOLOMUSH_VERIFY` non-server identity
(perms `_INBOX.>` only) is the first-class denial subject for the scripts and CI
proof. `verify-scoping.sh` (`set -euo pipefail`, shellcheck-clean, SPDX)
connects with a non-server credential and asserts publish AND subscribe are
DENIED on a probe under each of the three prefixes, deciding pass/fail by EXIT
CODE only (publish denial is a clean exit-code signal; subscribe denial
discriminates permitted-idle `timeout` exit 124 / message exit 0 from a
permissions-violation non-zero). `README.md` documents the grant list, static +
nsc/JWT walkthroughs, both-sides verification, and marks the read-only operator
account as deferred (`holomush-s5ts`).

**Task 2 — boot self-check + external wiring (`2c40bea2e`).**
`eventbus.VerifyAccountScoping(ctx, conn)` probes subscribe and publish on
`forbidden.scopecheck.<nonce>` (outside every grant). It reads nats.go's async
permissions-violation signal via `SetErrorHandler` + `errors.Is(err,
nats.ErrPermissionViolation)`: a denied probe raises a violation fast (returns
denied); a PERMITTED probe raises none within a 3s window ⇒ over-scoped ⇒
`EVENTBUS_ACCOUNT_OVERSCOPED` (fail closed, D-13c). Enforcement stays at the NATS
account layer (phase3d Decision 4) — the server only observes its own reach.
`core.go` invokes it only in external mode, right after `eventBusSub.Start`, and
refuses boot on error (`EVENTBUS_SCOPE_CHECK_FAILED`); `productionSubsystems`
arity is unchanged; embedded mode is skipped (no account model).

**Task 3 — CI-backed both-sides proof (`3f59fcf6d`).**
`test/integration/eventbus_external/scopecheck_test.go` (`//go:build
integration`, Ginkgo) — Case A: a plain natstest node (default-open) is refused
`EVENTBUS_ACCOUNT_OVERSCOPED`. Case B (HARD, no runbook fallback): an in-process
`nats-server` loads the SHIPPED `deploy/nats/holomush-server.account.conf`, and
asserts in-Go BOTH — the correctly-scoped `holomush-server` credential PASSES
`VerifyAccountScoping` (its forbidden probe is denied → nil), AND the
`holomush-verify` non-server credential is DENIED publish+subscribe on a probe
under each of `events.>`/`audit.>`/`internal.>` (permissions-violation signal,
never grepped stdout). Both cases run under `task test:int` (D-06).

## Verification

- `task test -- ./internal/eventbus/` — 204 tests green (self-check unit: over-scoped default-open conn refused; nil conn fails closed).
- `task test:int -- ./test/integration/eventbus_external/` — 7 of 7 Ginkgo specs green (5 existing external-boot + Case A + Case B).
- `task test -- -run 'TestProductionSubsystems|TestSubsystemAdminSocket' ./cmd/holomush/` — 6 green (arity unchanged).
- `task build` green; `task lint` clean (shellcheck on verify-scoping.sh + markdown + errcheck); `task fmt` output committed.
- Grant grep: `holomush-server.account.conf` server account grants only `events.>`/`audit.>`/`internal.>`/`_INBOX.>`.

## Deviations from Plan

**1. [Rule 3 - blocking lint] errcheck `check-blank` on the probe teardown**
- **Found during:** Task 3 `task lint` (Task 2's verify gate was `task test` + `task build`, which do not run errcheck).
- **Issue:** `.golangci.yaml` sets `errcheck.check-blank: true`, so `_ = sub.Unsubscribe()` / `_ = conn.Flush()` in `scopecheck.go` were flagged.
- **Fix:** Adopted the repo best-effort idiom `defer sub.Unsubscribe() //nolint:errcheck // best-effort …` (precedent: `coordinator.go:258`). Behavior unchanged.
- **Files modified:** `internal/eventbus/scopecheck.go`
- **Commit:** `3f59fcf6d` (folded into Task 3, the commit that makes lint green).

**2. [Rule 3 - compile fix] Unused `natstest` import removed**
- **Found during:** Task 3 authoring — the integration file uses the shared `startExternalNATS` helper (from `external_boot_test.go`) and never names the `natstest` type directly.
- **Fix:** Dropped the redundant import before first build.
- **Commit:** `3f59fcf6d`.

Otherwise the plan executed as written (all three D-13 deliverables landed; Case B is the W1-hardened CI-backed both-sides proof with no runbook-manual fallback).

## Note on working tree

`.planning/config.json` `workflow._auto_chain_active` flipped `false → true`
during the session (GSD orchestration state, not this plan's work). It was left
unstaged and is not part of any 03-06 commit — out of scope for CLUSTER-02.

## Known Stubs

None. `verify-scoping.sh` requires the `nats` CLI at runtime (an operator/runbook
tool, intentionally not run in CI); the equivalent property is proven in-Go by
Case B under `task test:int`.

## Threat Flags

None beyond the plan's threat register. T-03-16 (rogue principal on game topics)
is mitigated by the account allow-list + `verify-scoping.sh`; T-03-17 (server
account over-scoped) by `VerifyAccountScoping`'s fail-closed boot refusal; T-03-18
(self-check false-negative) by the fail-closed-on-permitted-probe design + the
Case A/B integration assertions. No new trust-boundary surface introduced.

## Self-Check: PASSED

- FOUND: deploy/nats/holomush-server.account.conf, deploy/nats/verify-scoping.sh, deploy/nats/README.md
- FOUND: internal/eventbus/scopecheck.go, internal/eventbus/scopecheck_test.go
- FOUND: test/integration/eventbus_external/scopecheck_test.go
- FOUND commit: bdb9205a9 (Task 1 deploy/nats assets)
- FOUND commit: 2c40bea2e (Task 2 scopecheck + core wiring)
- FOUND commit: 3f59fcf6d (Task 3 integration proof + errcheck fix)
