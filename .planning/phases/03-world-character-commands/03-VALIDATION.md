---
phase: 3
slug: world-character-commands
# status lifecycle: draft (seeded by plan-phase) → validated (set by validate-phase §6)
# audit-milestone §5.5 distinguishes NOT-VALIDATED (draft) from PARTIAL (validated + nyquist_compliant: false) (#2117)
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-08-06
updated: 2026-08-08
---

# Phase 3 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (testify units; Ginkgo/Gomega integration suites under `//go:build integration`; gotestsum runner via go-task) |
| **Config file** | `Taskfile.yaml` (`test` at :~230, `test:int` at :265-289 — forwards `{{.CLI_ARGS}}`, default `./...`) |
| **Quick run command** | `task test -- ./<changed-package>/...` (scoped; per-commit) |
| **Full suite command** | `task test && task test:int` (test:int needs Docker for testcontainers) |
| **Estimated runtime** | quick scoped run ~30-90s; full unit suite ~3-5 min; `task test:int` ~10-20 min; resilience suite (quarantine-gated) 10+ min extra when opted in |

---

## Sampling Rate

- **After every task commit:** Run `task test -- ./<packages the task touched>/...` (plus `task test -- ./test/meta/` when the task touches census/fence/invariant surfaces)
- **After every plan wave:** Run `task test && task test:int`
- **Before `/gsd-verify-work`:** Full suite must be green, including `task test:int -- -run TestRetirementReactor ./test/integration/retirement/` and `task test:int -- -run TestCharacterActivityFlush ./test/integration/charactivity/`; the resilience Describe runs once by hand with `HOLOMUSH_RUN_QUARANTINED=1` (nightly lane owns it thereafter)
- **Max feedback latency:** 90s for per-task scoped runs; 1200s for the integration tier (decide pass/fail by EXIT CODE, never by grepping runner output)

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 03-01-01 | 01 | 1 | IDENT-04, IDENT-10 | T-03-01 / T-03-02 | checkAccess("retire") gate; typed status const bound as SQL param; caller expected_version guard chain (R1) | unit + integration | `task test -- ./internal/world/... ./test/meta/ && task test:int -- ./internal/world/...` | ❌ RED-first in-task | ⬜ pending |
| 03-01-02 | 01 | 1 | IDENT-04, IDENT-10 | T-03-04 | version conflict outranks lifecycle guards; rejected transitions write nothing | unit | `task test -- ./internal/world/... ./test/meta/` | ❌ RED-first in-task | ⬜ pending |
| 03-01-03 | 01 | 1 | IDENT-04 | T-03-03 | D-34 clear commits atomically with the CAS; stale CAS leaves pointer untouched | integration | `task test -- ./internal/world/postgres/ && task test:int -- ./internal/world/postgres/` | ❌ RED-first in-task | ⬜ pending |
| 03-02-01 | 02 | 1 | IDENT-04 | T-03-05 | relocation preserves both audit error codes | unit + integration | `task test -- ./internal/eventbus/... && task test:int -- ./internal/eventbus/...` | ❌ RED-first in-task | ⬜ pending |
| 03-02-02 | 02 | 1 | IDENT-04 | T-03-07 | constructors allocate nothing; lifecycle guards idempotent | unit | `task test -- ./internal/retirement/ ./internal/charactivity/` | ❌ RED-first in-task | ⬜ pending |
| 03-02-03 | 02 | 1 | IDENT-04 | T-03-06 | 20-subsystem pins + topo order derived from observed diff | unit + integration | `task test -- ./cmd/holomush/ ./internal/lifecycle/ && task test:int` | ✅ (existing pins go RED then GREEN) | ⬜ pending |
| 03-03-01 | 03 | 2 | IDENT-10 | T-03-08 | stale writer gets WORLD_CONCURRENT_EDIT, not ALREADY_RETIRED (guard-order proof) | two-replica integration | `HOLOMUSH_RUN_QUARANTINED=1 task test:int -- -run TestWorldModelResilience ./test/integration/resilience/` | ❌ new Describe in-task | ⬜ pending |
| 03-03-02 | 03 | 2 | IDENT-04, IDENT-10 | T-03-08 | row+envelope commit-or-rollback together; retired name still refused | integration | `task test:int -- ./test/integration/world/` | ❌ new specs in-task | ⬜ pending |
| 03-04-01 | 04 | 2 | IDENT-04 | T-03-12 / T-03-13 / T-03-14 | per-effect skip gates; status guard with denying default; no borrowed actor identity | unit | `task test -- ./internal/retirement/ ./internal/core/` | ❌ RED-first in-task | ⬜ pending |
| 03-04-02 | 04 | 2 | IDENT-04 | T-03-10 / T-03-11 / T-03-15 | admin-only surface: non-admin DENY on own character + admin positive control (U1); no system-namespace permit added | unit + integration | `task test -- ./internal/access/... ./internal/retirement/ ./cmd/holomush/ && task test:int -- ./test/integration/access/` | ❌ new specs in-task | ⬜ pending |
| 03-05-01 | 05 | 3 | IDENT-04, IDENT-10 | T-03-18 / T-03-19 | fenced SQL confined to writer boundary; monotonic predicate; no version bump, no envelope | unit + integration | `task test -- ./internal/world/postgres/ && task test:int -- ./internal/world/postgres/ && task test -- -run 'TestWorldSQLFence' ./test/meta/` | ❌ RED-first in-task | ⬜ pending |
| 03-05-02 | 05 | 3 | IDENT-04 | T-03-16 / T-03-17 | revision-conditional deletes (LastRevision); poison-pill hygiene; forbidden RefreshConnection seam unhooked | unit | `task test -- ./internal/charactivity/ ./cmd/holomush/ && task lint` | ❌ RED-first in-task | ⬜ pending |
| 03-05-03 | 05 | 3 | IDENT-04, IDENT-10 | T-03-19 | emit path touches no Postgres; flush emits no envelope; registry amendment genuine | integration + meta | `go run ./cmd/inv-render && task test -- -run 'TestEveryRegistryInvariantHasBinding\|TestProvenanceGuard\|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/ && task test:int -- -run TestCharacterActivityFlush ./test/integration/charactivity/` | ❌ new suite in-task | ⬜ pending |
| 03-06-01 | 06 | 4 | IDENT-04 | T-03-22 | harness options boot REAL subsystems with production-shaped deps | integration | `task test:int -- ./internal/testsupport/integrationtest/` | ✅ (existing harness tests) | ⬜ pending |
| 03-06-02 | 06 | 4 | IDENT-04, IDENT-10 | T-03-21 (proves T-03-10/11/13) | observable fanout via real relay chain; instance-scope DENY + positive control; feed order | integration | `task test:int -- -run TestRetirementReactor ./test/integration/retirement/` | ❌ new suite in-task | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

*"❌ RED-first in-task": the test file does not exist at plan time BY DESIGN — every tdd-marked task writes its failing tests as its first commit (`test(03-NN)`), so there is no separate Wave 0; the RED observation is part of the task's own gate (PORTAL-10 rule 4).*

---

## Wave 0 Requirements

Existing infrastructure covers all phase requirements: go test + testify (unit), the `internal/world/postgres` testcontainer harness, the `test/meta/` census/fence/invariant guards, the `integrationtest` full-stack harness (03-05/03-06 extend it with StartOptions as owned in-plan work, not Wave 0), and the two-replica resilience suite. No framework installs, no test scaffolds needed before Wave 1.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Resilience Describe at phase gate | IDENT-10 | Quarantine-gated suite (10+ min, real NATS container) — excluded from the PR gate by design; nightly lane owns it thereafter | `HOLOMUSH_RUN_QUARANTINED=1 task test:int -- -run TestWorldModelResilience ./test/integration/resilience/` — run in background, judge by EXIT CODE |

All other phase behaviors have automated verification.

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (none — RED-first tests are in-task by design)
- [x] No watch-mode flags
- [x] Feedback latency < 90s (scoped unit) / < 1200s (integration tier)
- [ ] `nyquist_compliant: true` set in frontmatter (set by validate-phase §6)

**Approval:** pending
