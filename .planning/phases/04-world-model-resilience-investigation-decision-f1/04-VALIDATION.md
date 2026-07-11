---
phase: 4
slug: world-model-resilience-investigation-decision-f1
status: planned
nyquist_compliant: true
wave_0_complete: false
created: 2026-07-11
---

# Phase 4 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` + testify (unit); Ginkgo v2/Gomega with `//go:build integration` (harness suite) |
| **Config file** | Taskfile.yaml (`test`, `test:int` targets — `test:int` accepts CLI_ARGS package scoping) |
| **Quick run command** | `task test:int -- -run TestNoSuchTestZZZ ./test/integration/resilience/ ./internal/testsupport/integrationtest/` (compile gate under `-tags=integration`) |
| **Full suite command** | `HOLOMUSH_RUN_QUARANTINED=1 task test:int -- -run TestWorldModelResilience -timeout 30m ./test/integration/resilience/` (opt-in harness run) |
| **Estimated runtime** | quick ~1-2 min (plugin build cached); full opt-in run ~10-25 min (containers + chaos budgets) |

---

## Sampling Rate

- **After every task commit:** `task lint` + the quick compile gate above (plus the task's own `<automated>` verify)
- **After every plan wave:** env-UNSET lane check `task test:int -- -run TestWorldModelResilience ./test/integration/resilience/` (must exit 0 with 0 specs — D-05 gate) and the opt-in full suite for waves that added specs
- **Before `/gsd-verify-work`:** full opt-in suite green + `task test` + `task lint` green
- **Max feedback latency:** ~1500s (opt-in chaos suite upper bound); ~120s for the compile/lint loop

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 04-01-01 | 01 | 1 | OPS-05 | T-04-01 | new seams stay behind `//go:build integration`; default Start path unchanged | integration (compile+scoped) | `task test:int -- -run TestNoSuchTestZZZ ./internal/testsupport/integrationtest/ ./test/integration/privacy/` | ✅ (existing pkg) | ⬜ pending |
| 04-01-02 | 01 | 1 | OPS-05 | T-04-02 | suite self-skips on gating lane; no quarantine marker registered | integration (opt-in) | `HOLOMUSH_RUN_QUARANTINED=1 task test:int -- -run TestWorldModelResilience -timeout 15m ./test/integration/resilience/` + env-unset skip check + `task test -- -run TestQuarantineRegistryBijection ./test/meta/` | ❌ W0 (this task creates it) | ⬜ pending |
| 04-02-01 | 02 | 2 | OPS-05 | T-04-02 | M12 verdict specs inside gated entry | integration (opt-in) | `HOLOMUSH_RUN_QUARANTINED=1 task test:int -- -run TestWorldModelResilience -timeout 25m ./test/integration/resilience/` | ❌ W0 dep: 04-01-02 | ⬜ pending |
| 04-02-02 | 02 | 2 | OPS-05 | T-04-02 | restart/reconnect/flap-recovery specs inside gated entry | integration (opt-in) | same as 04-02-01 | ❌ W0 dep: 04-01-02 | ⬜ pending |
| 04-03-01 | 03 | 3 | OPS-05 | T-04-01/T-04-02 | test-only emitter wiring never leaves `test/`; raw publisher (no rendering bypass concerns) | integration (opt-in) | `HOLOMUSH_RUN_QUARANTINED=1 task test:int -- -run TestWorldModelResilience -timeout 30m ./test/integration/resilience/` | ❌ W0 dep: 04-01-02 | ⬜ pending |
| 04-03-02 | 03 | 3 | OPS-05, MODEL-01 | T-04-06 | verdict doc quotes verbatim evidence from an exit-code-judged run | doc + lint | `task lint` + rg assertions in plan acceptance | ❌ (doc created here) | ⬜ pending |
| 04-04-01 | 04 | 4 | MODEL-01 | T-04-04 | ADR draft carries both options equally (D-01), no lean | doc + lint | `task lint` + rg coverage assertions | ❌ (ADR created here) | ⬜ pending |
| 04-04-02 | 04 | 4 | MODEL-01 | T-04-07 | blocking human checkpoint — executor cannot self-decide | checkpoint (human) | — (resume-signal) | — | ⬜ pending |
| 04-04-03 | 04 | 4 | MODEL-01 | T-04-04 | Consequences records ABAC placement unchanged | doc + lint | `task lint` + rg assertions (Accepted, MODEL-03/04 named, no PENDING) | ❌ dep: 04-04-01 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/testsupport/integrationtest/` — `WithExternalNATS` / `WithSharedDatabase` StartOptions + `ConnStr()`/`Bus()` accessors (plan 04-01 task 1)
- [ ] `test/integration/resilience/` — suite entry with `quarantinetest.Enabled()` gate + chaos helpers + boot smoke (plan 04-01 task 2)
- [ ] Framework install: none — all libraries pinned in go.mod (RESEARCH: zero new packages)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| The ADR's decision content is correct and genuinely open (A vs B weighed equally) | MODEL-01 | An architecture decision's correctness is human judgment; lint verifies form only | Plan 04-04 Task 2 blocking checkpoint:decision — decider reviews the brief and selects option-a/option-b; final text re-checked at /gsd-verify-work |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies (checkpoint task exempt by type)
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (plan 04-01 creates the suite + seams every later spec depends on)
- [x] No watch-mode flags
- [x] Feedback latency < 1500s (opt-in chaos suite bound; compile/lint loop < 120s)
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
