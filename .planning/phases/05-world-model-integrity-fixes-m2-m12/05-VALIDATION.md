---
phase: 5
slug: world-model-integrity-fixes-m2-m12
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-11
---

# Phase 5 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` + testify (unit); Ginkgo/Gomega (integration); driven via `task` |
| **Config file** | `Taskfile.yaml` (no separate framework config) |
| **Quick run command** | `task test -- ./internal/world/... ./internal/store/...` |
| **Full suite command** | `task test` (unit) then `task test:int` (integration + resilience, needs Docker) |
| **Estimated runtime** | unit ~10-60s per package; `task test:int` several minutes (Docker) |

---

## Sampling Rate

- **After every task commit:** Run `task test -- ./<touched-package>/...` (dispatch `local-check`)
- **After every plan wave:** Run `task test` + the wave's integration slice (`task test:int` when the wave touches the resilience/outbox surface)
- **Before `/gsd-verify-work`:** Full suite must be green; `task pr-prep` fast lane green
- **Max feedback latency:** ~60s for unit-level; integration waves gated separately (Docker)

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| {populated by executor from PLAN.md task IDs} | | | | | | | | | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

> The concrete per-task rows are filled during execution once PLAN.md task IDs
> exist. The Validation Architecture section of `05-RESEARCH.md` is the source
> for which success criterion each test proves (guard-conflict test,
> atomic-feed fault-injection test, doc-grep assertion, `// Verifies:`
> invariant-binding test).

---

## Wave 0 Requirements

- [ ] Existing Go test infrastructure covers all phase requirements — no new framework install.
- [ ] `test/integration/resilience/` two-replica harness + `internal/testsupport/integrationtest` seams already exist (extend with fault injection, not stand up fresh).
- [ ] Non-quarantined seam-existence tests required for any new `internal/testsupport` code (codecov/patch reads quarantine-gated suites as uncovered).

*If none: "Existing infrastructure covers all phase requirements."*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| {none expected — all integrity behaviors are automatable via guard/outbox/doc-grep/invariant tests} | | | |

*If none: "All phase behaviors have automated verification."*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
