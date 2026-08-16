---
phase: 4
slug: shared-facade-helpers-characteraccessservice
# status lifecycle: draft (seeded by plan-phase) → validated (set by validate-phase §6)
# audit-milestone §5.5 distinguishes NOT-VALIDATED (draft) from PARTIAL (validated + nyquist_compliant: false) (#2117)
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-08-10
---

# Phase 4 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` + `testify` (unit); Ginkgo/Gomega (full-stack integration, `//go:build integration`) |
| **Config file** | none (Go native) — harness at `internal/testsupport/integrationtest/`; ABAC engine builder at `internal/testsupport/abactest/abactest.go:68` |
| **Quick run command** | `task test -- ./internal/grpc/ ./internal/access/... ./internal/web/` |
| **Full suite command** | `task test` then `task test:int` (scoped: `task test:int -- ./test/integration/access/...`) |
| **Estimated runtime** | ~90 seconds quick; ~10-15 min full (`task test:int` needs Docker) |

---

## Sampling Rate

- **After every task commit:** Run `task test -- ./<touched-package>/` + `task lint`
- **After every plan wave:** Run `task test` (full unit) + `task test -- -run 'Census' ./test/meta/`
- **Before `/gsd-verify-work`:** Full suite must be green — `task pr-prep` green, then `task pr-prep:full` (integration + E2E) before push; `/holomush-dev:review-abac` READY
- **Max feedback latency:** 90 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| TBD | TBD | TBD | IDENT-02 | — | Guest gate + ownership reachable from exactly one definition | unit (meta) | `task test -- -run 'Census' ./test/meta/` | ❌ W0 | ⬜ pending |
| TBD | TBD | TBD | PROFILE-03 | T-4-EXIST-DISCLOSE | Withheld field absent from marshaled bytes; unreachable == nonexistent | unit + integration | `task test -- -run 'Profile.*Absent' ./internal/grpc/` | ❌ W0 | ⬜ pending |
| TBD | TBD | TBD | PROFILE-04 | T-4-TIER-BYPASS | Per-attribute tier floor governs every field; unknown tier denies | unit | `task test -- ./internal/grpc/ ./internal/access/...` | ❌ W0 | ⬜ pending |
| TBD | TBD | TBD | PROFILE-10, IDENT-02a | T-4-MASK-PASSTHRU | Owner edit; over-cap rejected server-side; reaches `world.Service.UpdateCharacterDescription` | unit + integration | `task test -- ./internal/grpc/` ; `task test:int` | ❌ W0 | ⬜ pending |
| TBD | TBD | TBD | PROFILE-05, EXT-06 | T-4-DIRECT-READ | Profile built exclusively from the viewer-filtered slice (compile fence); media proto ships empty | compile-time + unit + integration | `task build` ; `task lint:proto` ; `task test:int` | ❌ W0 | ⬜ pending |
| TBD | TBD | TBD | PROFILE-03 | T-4-ALT-LINKAGE | Off-location viewer reads description; projection carries no `PlayerId`/`LocationId` | unit + integration | `task test -- ./internal/access/policy/` ; `task test:int -- ./test/integration/access/...` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

> Task IDs are assigned by `/gsd-plan-phase` when PLAN.md files are written; `/gsd-validate-phase` reconciles this map against the executed plans.

---

## Wave 0 Requirements

- [ ] Generalize `serviceReceiverName` → a facade-aware receiver predicate (new helper in `test/meta/`)
- [ ] A symmetric-difference diff helper for census failures (01-SPEC §2.6 `:222-224`) — `test/meta/meta_helpers_test.go` is the existing shared-helper home
- [ ] Decide and implement the **web-half routing predicate** — without it, criterion 1's proxy half is vacuous (research Open Question 3)
- [ ] A `(*integrationtest.Server).NewCharacterAccessServer` harness constructor, mirroring `harness.go:1173`
- [ ] Framework install: **none** — all frameworks present

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Criterion 3's config-side clause — a game configuration cannot raise `name`/`pronouns` above the profile's own reachability floor | PROFILE-04 | 01-SPEC §8.8 (`:1855-1867`) records that **v0.13 ships no mechanism** enforcing this against a deliberately violating configuration; `INV-PRIVACY-10` "is phrased as a system guarantee while in fact resting on operator discipline" | Operator review of the shipped tier configuration. **Do NOT bind `INV-PRIVACY-10` to a test that proves only the facade half** — `TestBoundInvariantsAreGenuinelyAsserted` cannot detect a partial binding (`.claude/rules/invariants.md`). |
| Compile-fence RED for criterion 5 | PROFILE-05 | A compile error cannot be asserted by a passing test; the fence's RED must be *demonstrated and recorded*, not asserted | Temporarily add `s.propertyRepo.ListByParent(...)` to the facade, run `task build`, record the compile error verbatim in the plan's RED evidence, then revert. |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 90s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
