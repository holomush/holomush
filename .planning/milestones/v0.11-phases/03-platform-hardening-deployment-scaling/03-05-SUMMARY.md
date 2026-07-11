---
phase: 03-platform-hardening-deployment-scaling
plan: 05
subsystem: cluster / crypto-invalidation / invariant-registry
tags: [cluster, crypto, invalidation, nats, testcontainers, invariants, CLUSTER-03, D-05a, D-07, D-08]
status: complete
requires:
  - "internal/testsupport/natstest (03-02 external-NATS harness)"
  - "test/integration/eventbus_external/external_boot_test.go (03-03, // Verifies: INV-EVENTBUS-29)"
  - "internal/eventbus/audit/dlq_*_integration_test.go (03-04, // Verifies: INV-EVENTBUS-30)"
provides:
  - "clustertest.ExternalHarness — N cluster.Registry members each on its OWN *nats.Conn to one natstest container"
  - "Multi-node CLUSTER-03 proof: N-of-N acks + hung-replica probe-pill N-1 completion over per-replica conns"
  - "INV-CLUSTER-1 bound; INV-EVENTBUS-29 + INV-EVENTBUS-30 minted+bound"
affects:
  - "docs/architecture/invariants.yaml + invariants.md (registry consolidation for the whole phase, D-07)"
tech-stack:
  added: []
  patterns:
    - "clustertest.ExternalHarness mirrors the embedded Harness API but dials one independent *nats.Conn per member"
    - "Ordered Ginkgo container (BeforeAll StartNATS / AfterAll Terminate) shares one container across a file's multi-node specs"
    - "asserted_by-only new registry entries (no refs) avoid owned_paths ownership churn for out-of-scope test files"
key-files:
  created:
    - internal/cluster/clustertest/external.go
  modified:
    - test/integration/crypto/cache_invalidation_test.go
    - test/integration/cluster/cluster_test.go
    - docs/architecture/invariants.yaml
    - docs/architecture/invariants.md
decisions:
  - "Shared external multi-node harness placed in internal/cluster/clustertest (sanctioned home; depguard exempts clustertest/** from the natstest deny-rule) rather than duplicated inline across two packages"
  - "INV-EVENTBUS-29/30 registered with asserted_by only (no refs) — external_boot_test.go and the DLQ test files are outside INV-EVENTBUS owned_paths, so refs would trip the provenance ownership check; binding is proven by the // Verifies annotations the meta-test walks"
  - "INV-EVENTBUS-30 asserted_by points at the ACTUAL 03-04 DLQ test locations (internal/eventbus/audit/dlq_*_integration_test.go), not the plan's stale test/integration/audit/ path"
metrics:
  duration: "~70m"
  tasks: 3
  files: 5
  completed: 2026-07-10
---

# Phase 3 Plan 05: Multi-node crypto invalidation + invariant capstone (CLUSTER-03) Summary

Proved the shipped crypto-invalidation + cluster substrate against REAL multi-node replicas — each replica on its own `*nats.Conn` to a single external NATS testcontainer — closing the shared-conn gap at `cache_invalidation_test.go:41` (D-05a), and landed the phase's invariant-registry discipline in one consolidated, genuinely-asserted change (D-07).

## What Was Built

**Task 1 — per-replica external connections (`f53033937`).** Added `clustertest.ExternalHarness`: `NewExternal(t, env, clusterID, n)` dials `n` independent connections to one `natstest` container and stands up a `cluster.Registry` per member, with `AwaitConverged` / `PublishSyntheticHeartbeat` / `AwaitMemberPresent` mirroring the embedded harness. Rewrote `cache_invalidation_test.go` over it (Ordered container, one NATS node per suite run): KEK rotation N-of-N acks (`// Verifies: INV-CLUSTER-1`), rekey DEK-cache eviction (`INV-CLUSTER-2`), single-member degeneration (`INV-CLUSTER-2`), participants_changed (`INV-CLUSTER-9`), single-retry semantics (`INV-CLUSTER-6`), plus a **hung-replica** spec that closes one member's conn mid-flight and asserts probe-and-pill fires and the rekey completes with N-1 (D-08). No `h.Embedded.Conn` remains — every Coordinator wires to its member's own `Conn`.

**Task 2 — multi-node cluster_id filtering (`f2164a4f7`).** Extended `cluster_test.go` with a 2-member `ExternalHarness` spec (`// Verifies: INV-CLUSTER-4`) that publishes a foreign-cluster_id heartbeat and asserts every replica drops it — the cross-cluster message-injection mitigation (T-03-13) proven over real independent connections instead of the shared embedded conn.

**Task 3 — invariant registry capstone (`3531bb9b2`, `60b75d02e`).** In `invariants.yaml`: flipped `INV-CLUSTER-1` `pending → bound` (now genuinely asserted by the rebound KEK-rotation spec); minted `INV-EVENTBUS-29` (external-mode boot fails closed) and `INV-EVENTBUS-30` (DLQ capture never drops), both `bound` with `asserted_by` pointing at the Plan-03/04 integration tests already carrying the `// Verifies:` annotations; left `INV-CLUSTER-8` `pending` (analyzer-only, not multi-node) and filed coverage gap **#4777**. Regenerated `invariants.md` via `go run ./cmd/inv-render` (no drift).

## Invariant Registry Changes

| ID | Change | asserted_by |
|----|--------|-------------|
| INV-CLUSTER-1 | pending → **bound** | test/integration/crypto/cache_invalidation_test.go (KEK N-of-N) |
| INV-CLUSTER-2/4/9 | stay bound, strengthened in place (same files now per-replica multi-node) | (unchanged) |
| INV-CLUSTER-8 | left **pending** (analyzer-only); coverage issue #4777 | — |
| INV-EVENTBUS-29 | **minted + bound** (external boot fail-closed) | test/integration/eventbus_external/external_boot_test.go |
| INV-EVENTBUS-30 | **minted + bound** (DLQ never drops) | internal/eventbus/audit/dlq_capture_integration_test.go, dlq_neverdrop_integration_test.go |

## Verification

- `task test:int -- ./test/integration/cluster/` — green (22.6s; NATS container + multi-node cluster_id spec + existing embedded specs).
- `task test:int -- ./test/integration/crypto/` — green (44.2s, all 6 top-level tests). (One earlier run showed the documented transient Postgres-testcontainer drop under load in unrelated `readback_test.go`; re-ran green.) The multi-node NATS specs alone (`-ginkgo.focus='multi-node external NATS'`) green (14.7s).
- `go run ./cmd/inv-render -check` — no drift (exit 0).
- `task test -- -run 'TestEveryRegistryInvariantHasBinding|TestProvenanceGuard|TestBoundInvariantsAreGenuinelyAsserted|TestRegistryBindingChecks' ./test/meta/` — green (no fabricated bindings, no Skip-only bindings, no drift).
- `task test` — green (10196 tests, 3 skipped). `task lint` — exit 0. `task fmt` — clean (committed).
- Crypto-reviewer gate expected pre-push (touches `internal/eventbus/crypto/invalidation` surface via the test; no crypto invariant weakened — the multi-node test genuinely exercises the Coordinator's request-reply + probe-and-pill).

## Deviations from Plan

**1. [Rule 3 — blocking] Shared multi-node harness added at `internal/cluster/clustertest/external.go` (not in the plan's `files_modified`)**
- **Found during:** Task 1. The two integration files live in different packages (`crypto_test`, `cluster_test`) and cannot share a `_test.go` helper; per-file inline duplication of ~120 lines of registry+conn wiring is the alternative.
- **Fix:** Placed one `ExternalHarness` in `clustertest`, the sanctioned home for multi-Registry harnesses (its own doc comment says so) and explicitly **exempted** from the natstest depguard deny-rule (`.golangci.yaml:146` `!**/internal/cluster/clustertest/**`), so a non-`_test.go` file may import natstest there without a production-import violation.
- **Files:** `internal/cluster/clustertest/external.go` — **Commit:** `f53033937`

**2. [Rule 3 — plan/reality drift] `INV-EVENTBUS-30` asserted_by points at the real DLQ test locations**
- **Found during:** Task 3. The plan's action text cites `test/integration/audit/dlq_capture_test.go`, but Plan 03-04 (its own documented deviation) co-located the DLQ integration specs at `internal/eventbus/audit/dlq_capture_integration_test.go` + `dlq_neverdrop_integration_test.go` (the `test/integration/audit/` dir is a different `audit` package).
- **Fix:** `asserted_by` lists the actual files carrying `// Verifies: INV-EVENTBUS-30`. — **Commit:** `3531bb9b2`

**3. [Note] INV-CLUSTER-2/4/9 asserted_by unchanged**
- The plan's "gain the multi-node test as an additional asserted_by" is satisfied **in place**: the multi-node specs live in the same test files those entries already list (`cache_invalidation_test.go` for 2/9, `cluster_test.go` for 4), which this plan upgraded from shared-conn to per-replica-conn. No path addition was needed; the existing `asserted_by` now points at genuinely multi-node assertions.

**4. [Note] yamlfmt normalization**
- `task lint:yaml` required yamlfmt normalization of the hand-edited registry (trailing-comma + summary line-wrap; semantics unchanged), committed as `60b75d02e`.

## Coverage Gaps Filed

- **#4777** — "coverage gap: INV-CLUSTER-8 unbound" (`bug`): analyzer-enforced (`noremoteclockcompare`), not multi-node runtime behavior, so the CLUSTER-03 harness cannot genuinely assert it (D-07). Left `binding: pending`.

## Known Stubs

None. All new specs carry genuine assertions; no placeholder/Skip-only bindings (meta-test `TestBoundInvariantsAreGenuinelyAsserted` green).

## Threat Flags

None. No new network endpoint, auth path, or trust-boundary schema change. T-03-13 (cross-cluster injection) is now proven over per-replica conns; T-03-14 (fabricated binding) is guarded by the meta-tests + the filed coverage gap; T-03-15 (hung replica) is proven by the N-1 completion spec.

## Self-Check: PASSED

- FOUND: internal/cluster/clustertest/external.go
- FOUND: test/integration/crypto/cache_invalidation_test.go
- FOUND: test/integration/cluster/cluster_test.go
- FOUND: docs/architecture/invariants.yaml
- FOUND: docs/architecture/invariants.md
- FOUND commit f53033937 (Task 1), f2164a4f7 (Task 2), 3531bb9b2 + 60b75d02e (Task 3)
- INV-CLUSTER-1 binding: bound; INV-EVENTBUS-29 + INV-EVENTBUS-30 present + bound; inv-render -check clean
