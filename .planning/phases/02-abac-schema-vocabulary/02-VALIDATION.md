---
phase: 2
slug: abac-schema-vocabulary
# status lifecycle: draft (seeded by plan-phase) → validated (set by validate-phase §6)
# audit-milestone §5.5 distinguishes NOT-VALIDATED (draft) from PARTIAL (validated + nyquist_compliant: false) (#2117)
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-08-03
---

# Phase 2 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Seeded from `02-RESEARCH.md` § Validation Architecture. The Per-Task Verification Map
> is filled once `02-*-PLAN.md` exists and task IDs are allocated.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` + `testify` (`assert`/`require`); Ginkgo/Gomega for integration |
| **Config file** | `Taskfile.yaml` — no separate test config; the `test:int` package list is hard-coded there |
| **Quick run command** | `task test -- ./internal/<pkg>/` |
| **Full suite command** | `task test` (untagged), then `task test:int` (integration, needs Docker) |
| **Coverage command** | `task test:cover` |
| **Estimated runtime** | ~60s untagged; integration is minutes (testcontainers Postgres) |

**Critical:** `task test` does **not** compile `//go:build integration` files. This phase refactors
shared types (the `ExistsByName` interfaces, `world.Character`), so every such change MUST be
followed by `task test:int` or the breakage is silent.

**Delegation rule:** per `CLAUDE.md`, `task test|lint|build|test:int|test:cover` MUST be dispatched
to the `local-check` agent rather than run inline in the parent session — except the FINAL
`task pr-prep` before a push, which runs inline.

---

## Sampling Rate

- **After every task commit:** `task test -- ./<touched package>/` plus `task lint`
- **After every plan wave:** `task test` (full untagged) — and `task test:int` for any wave that
  touched a shared type or interface
- **Before `/gsd-verify-work`:** `task pr-prep` green; `task test:int` green (the DB-backed
  success criteria live there)
- **Max feedback latency:** ~60s for the untagged lane

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| *(pending — allocated when `02-*-PLAN.md` is written)* | — | — | — | — | — | — | — | — | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

The requirement→behavior→command mapping this table must satisfy is enumerated in
`02-RESEARCH.md` § Validation Architecture → "Phase Requirements → Test Map" (31 rows covering
IDENT-06, IDENT-07, IDENT-08, IDENT-09, PROFILE-11, EXT-07, plus D-03/D-04/D-06/D-07/D-09/D-12/
D-15/D-16/D-19/D-22/D-23, INV-PRIVACY-11, INV-WORLD-5, INV-WORLD-6, and the §8.4.1/§8.5/§8.6
obligations). Every row there must land on a task here.

---

## RED-first demonstrations (PORTAL-10 rule 4)

Three gates MUST be observed failing before their fix lands. Each is the only assertion that can
distinguish the correct implementation from the plausible wrong one:

| Gate | RED against | Method |
|------|-------------|--------|
| Name-uniqueness (criterion 2) | today's **unindexed** schema | Write the concurrent-claim integration test, run against schema `000053` before the index migration lands, record the failure, then add the index |
| Fourth-rung clearing (§8.2.1) | an **ordinal-comparison** implementation | Implement the clearing test with `>=` first, watch a synthetic `spectator` tier clear a `player` floor, then replace with set membership |
| Additive-permit regression (D-04) | **term B removed** | Evaluate term A alone, watch the `private` row publish, then reinstate the conjunction |

Staging a schema without polluting the real chain is already solved in-tree: a temp-dir migration
set plus `goose.WithDisableGlobalRegistry(true)`
(`internal/store/migrate_gointerleave_integration_test.go:134-208`). Use that rather than
hand-reverting the corpus.

---

## Wave 0 Requirements

- [ ] `internal/world/<name-pipeline>_test.go` — IDENT-06 pipeline, mixed script, empty normal form
- [ ] `internal/<confusables-pkg>/skeleton_test.go` + generated-table drift meta-test — IDENT-06, D-23
- [ ] `internal/<blocklist-pkg>/*_test.go` — IDENT-07, D-15, D-16
- [ ] `internal/access/policy/<tier-floor>_test.go` — D-03, D-04, §8.2.1 fourth rung, §8.6 totality
- [ ] `internal/access/policy/<admin-section>_test.go` — EXT-07 criterion 5, D-06, D-07/INV-PRIVACY-11, D-09
- [ ] `test/integration/world/<lifecycle>_test.go` — INV-WORLD-5, INV-WORLD-6
- [ ] `test/integration/<domain>/<name-uniqueness>_test.go` — criterion 2 concurrency, D-19 synthetic collisions, D-22 rollback
- [ ] Extend `test/integration/access/seed_policies_test.go` — criterion 4 plus its paired positive control
- [ ] Extend `internal/access/policy/seed_smoke_test.go` — a `viewerProvider` double beside `characterProvider` (`:35-52`)
- [ ] Extend `internal/auth/player_test.go` — IDENT-08 regression pin for `^[a-zA-Z][a-zA-Z0-9_]*$`
- [ ] `02-AUDIT-profile-public-read.sql` + `02-AUDIT-RESULT.md` — D-12 exposure audit artifact
- [ ] Framework install: **none needed** — Ginkgo, Gomega, testify, testcontainers are all present

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| `seed:profile-public-read` exposure audit | PROFILE-11 / D-12 | A **data** question about live rows, not a design one — no assertion can stand in for reading what is actually there | Run the committed read-only query from `02-AUDIT-profile-public-read.sql` against the target database; record the result in `02-AUDIT-RESULT.md`. The widening MUST NOT merge before this exists and is non-empty. |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s on the untagged lane
- [ ] All 31 rows of RESEARCH § "Phase Requirements → Test Map" land on a task
- [ ] All three RED-first gates are observed failing before their fix
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
