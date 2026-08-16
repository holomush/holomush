---
phase: 6
slug: admin-portal-shell-character-administration
# status lifecycle: draft (seeded by plan-phase) → validated (set by validate-phase §6)
# audit-milestone §5.5 distinguishes NOT-VALIDATED (draft) from PARTIAL (validated + nyquist_compliant: false) (#2117)
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-08-13
---

# Phase 6 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
>
> **Seeded by `/gsd-plan-phase 6`.** `status: draft` and the empty Per-Task
> Verification Map below are the seeded state, not coverage. `/gsd-validate-phase 6`
> fills the map and flips `status: validated`. A `produces:` hook is satisfied by this
> filename existing, so never read "the nyquist gate passed" as "coverage exists" —
> check `status:` and re-derive the row count.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` + testify (unit) · Ginkgo/Gomega (integration, `//go:build integration`) · Vitest + svelte-check (web unit) · Playwright (E2E) |
| **Config file** | `Taskfile.yaml` (targets verified: `test`:185, `test:int`:265, `test:e2e`:310, `web:test`:710) |
| **Quick run command** | `task test -- ./internal/admin/... ./internal/grpc/...` |
| **Full suite command** | `task pr-prep` (fast lane: schema/license/lint/fmt/unit/build/bats + `web:test`) |
| **Estimated runtime** | ~{TBD — measure at Wave 0} seconds |

> Tier selection is governed by `.claude/rules/testing.md`. `task test` does **NOT**
> compile `//go:build integration` files — any refactor of shared types MUST also run
> `task test:int`. Docker is required for `test:int` and `test:e2e`.

---

## Sampling Rate

- **After every task commit:** Run `task test -- ./<touched-package>/`
- **After every plan wave:** Run `task test` (+ `task test:int` when the wave touched shared types)
- **Before `/gsd-verify-work`:** Full suite must be green (`task pr-prep`)
- **Max feedback latency:** {TBD — measure at Wave 0} seconds

---

## Per-Task Verification Map

<!-- SEEDED EMPTY by plan-phase. /gsd-validate-phase 6 populates this table once
     PLAN.md files exist. An empty map here is expected at this stage and is NOT
     evidence of coverage. -->

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| _(pending — populated by `/gsd-validate-phase 6` after planning)_ | | | | | | | | | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] _(pending — derived from `06-RESEARCH.md` § Validation Architecture once plans exist)_

*Existing infrastructure is expected to cover most of this phase: `internal/testsupport/integrationtest`
(full-stack tier), `test/meta/` (set-equality / census meta-tests), `web/e2e/` (Playwright),
and `task web:test` (Vitest + svelte-check, gated in `pr-prep` and the CI `Build` job).*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| _(pending — populated by `/gsd-validate-phase 6`)_ | | | |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < {N}s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
