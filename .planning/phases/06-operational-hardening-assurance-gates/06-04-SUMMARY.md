---
phase: 06-operational-hardening-assurance-gates
plan: 04
subsystem: testing
tags: [codecov, coverage, ci, ruleset, branch-protection, docs]

# Dependency graph
requires:
  - phase: 06-03
    provides: "OPS-03 vuln: CI job (rendered Vuln check); the consolidated protect-main ruleset checkpoint this plan folds codecov into"
provides:
  - "codecov project-coverage ratchet ({target: auto, threshold: 1%}) — rising-floor whole-repo regression guard"
  - "single canonical .codecov.yml (duplicate codecov.yml deleted)"
  - "coverage docs corrected to enforced reality (patch + project, never per-package; posts-not-required)"
  - "ship-time operator note: consolidated protect-main assurance-gate checklist (mandatory Vuln from 06-03 + optional/accepted-deferred codecov checks)"
affects: [ship, gsd-verifier, contributor-onboarding]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "codecov project status uses target: auto (rising floor) rather than an absolute % — legacy code not retroactively blocked"
    - "docs describe assurance gates by their VERIFIED live ruleset state, not aspirational MUSTs"

key-files:
  created:
    - .planning/phases/06-operational-hardening-assurance-gates/06-04-SUMMARY.md
  modified:
    - .codecov.yml
    - .claude/rules/testing.md
    - CLAUDE.md
  deleted:
    - codecov.yml

key-decisions:
  - "Corrected D-07's false premise: codecov/patch is NOT a hard merge gate today (verified via gh api ruleset 11923801) — docs now say it POSTS but does not block"
  - "project ratchet threshold: 1% is a 1-percentage-point regression allowance, NOT 'no-drop' (tightenable toward 0% once coverage stabilizes)"
  - "codecov ruleset add is OPTIONAL/accepted-deferred (round-3 F6 resolution); only the OPS-03 Vuln check is a mandatory phase-6 ruleset add, and that is owned by 06-03 Task 4"

patterns-established:
  - "Assurance-gate docs are written truthfully for BOTH ruleset branches (added -> required/blocking; deferred -> posts, not required)"

requirements-completed: [QUAL-01]

coverage:
  - id: D1
    description: ".codecov.yml project status is the rising-floor ratchet {target: auto, threshold: 1%}; patch unchanged at {target: 80%, threshold: 5%}; validated by codecov"
    requirement: QUAL-01
    verification:
      - kind: automated
        ref: "curl --data-binary @.codecov.yml https://api.codecov.io/validate -> Valid! (project target auto / threshold 1.0)"
        status: pass
      - kind: automated
        ref: "test ! -e codecov.yml && rg 'target: auto|threshold: 1%' .codecov.yml"
        status: pass
    human_judgment: false
  - id: D2
    description: "Duplicate codecov.yml deleted — exactly one codecov config at repo root"
    requirement: QUAL-01
    verification:
      - kind: automated
        ref: "test ! -e codecov.yml"
        status: pass
    human_judgment: false
  - id: D3
    description: "Coverage docs in .claude/rules/testing.md + CLAUDE.md rewritten to enforced reality (patch/project, never per-package; posts-not-required); fictional per-package >80% MUST and 90%+ SHOULD removed"
    requirement: QUAL-01
    verification:
      - kind: automated
        ref: "task lint -> exit 0; task lint:docs-symmetry -> exit 0; negative grep 'Per-package coverage must|90%+ coverage' -> no matches"
        status: pass
    human_judgment: false
  - id: D4
    description: "Task 3 consolidated protect-main assurance-gate checklist — ship-time operator note (mandatory Vuln add owned by 06-03; codecov checks optional/accepted-deferred, default = posts-not-required)"
    verification: []
    human_judgment: true
    rationale: "Repo-settings action on the protect-main ruleset performed by the operator at ship time; no in-repo artifact and no automated verification at plan-execution time. Accepted-deferred default authorized — not a completion blocker."

# Metrics
duration: 8min
completed: 2026-07-15
status: complete
---

# Phase 6 Plan 04: QUAL-01 Coverage-Policy Reconciliation Summary

**Corrected the fictional per-package >80% coverage MUST to codecov's real patch+project model and added a rising-floor project ratchet (target: auto, threshold: 1%); the two codecov ruleset checks are an accepted-deferred enhancement, consolidated with 06-03's mandatory Vuln add into one ship-time operator checklist.**

## Performance

- **Duration:** ~8 min
- **Started:** 2026-07-15T11:54Z
- **Completed:** 2026-07-15T12:02Z
- **Tasks:** 2 auto tasks executed + 1 checkpoint resolved as accepted-deferred (ship-time operator note)
- **Files modified:** 3 (+1 deleted, +1 created)

## Accomplishments
- `.codecov.yml` project status changed `{target: 80%, threshold: 2%}` → `{target: auto, threshold: 1%}` — a rising-floor ratchet that blocks whole-repo coverage regressions beyond 1 percentage point without retroactively blocking the ~54.6% baseline. Patch status left unchanged at `{target: 80%, threshold: 5%}`. Validated via `api.codecov.io/validate` → **Valid!**.
- Deleted the 375-byte ignore-only duplicate `codecov.yml`, leaving one canonical config and removing codecov's undocumented dual-file precedence ambiguity (D-08).
- Rewrote the doc-fiction coverage rows in `.claude/rules/testing.md` and `CLAUDE.md` to the VERIFIED enforced reality: codecov measures **patch** (changed lines) and **project** (whole-repo), **never per-package**; both POST PR statuses but are **not** required protect-main checks today, so they do not block merges. Removed the 90%+ core-package SHOULD line. Documented that the classic-branch-protection 404 is expected (ruleset `11923801` is authoritative).
- Recorded the single consolidated protect-main assurance-gate checklist as a ship-time operator note (see User Setup Required).

## Task Commits

1. **Task 1: .codecov.yml project ratchet + delete duplicate codecov.yml** - `fe5e9ace6` (feat)
2. **Task 2: rewrite fictional per-package coverage MUST to enforced reality** - `aa2f1f9e6` (docs)
3. **Task 3: consolidated protect-main assurance-gate checklist** - checkpoint resolved as accepted-deferred (ship-time operator action; see User Setup Required)

**Plan metadata:** (this SUMMARY + STATE.md + ROADMAP.md commit)

## Files Created/Modified
- `.codecov.yml` - project status → rising-floor ratchet (auto/1%); patch annotated as posted-not-required
- `codecov.yml` - DELETED (dual-file conflict resolved)
- `.claude/rules/testing.md` - Coverage section rewritten to patch/project model
- `CLAUDE.md` - Testing-table coverage row rewritten to match (AGENTS.md symlink → CLAUDE.md, symmetry intact)

## Decisions Made
- **Accepted-deferred codecov ruleset add (round-3 F6 resolution).** QUAL-01 criterion #4 is satisfied by the doc correction + the `.codecov.yml` ratchet REGARDLESS of whether codecov ever becomes a required check. The only mandatory phase-6 ruleset add is the OPS-03 `Vuln` check, which is owned/tracked by 06-03 Task 4 — not duplicated here.
- **Docs written truthfully for both branches.** Default shipped state is "posts, not required." IF the operator later adds codecov/patch + codecov/project to the ruleset, the coverage docs MUST be re-tightened to "required/blocking" and `task lint` re-run (round-3 finding 10/11).
- **threshold: 1%, not 0%.** Honestly labeled a 1-percentage-point regression allowance (not "no-drop"); absorbs the two-upload merge jitter (unit + integration/e2e sessions merge after `notify.after_n_builds: 2`). Tightenable toward `threshold: 0%` once coverage stabilizes.

## Deviations from Plan

None - plan executed exactly as written. The plan's Task 3 is a blocking `checkpoint:human-action`; the executor was explicitly authorized to take the accepted-deferred default (do not stop for the optional codecov add; the mandatory Vuln is already owned by 06-03), so it is recorded here as a ship-time operator note rather than halting the plan.

## Issues Encountered
None.

## User Setup Required

**One consolidated protect-main ruleset edit at ship time** (GitHub → repo Settings → Rules → protect-main ruleset `11923801` → "Require status checks to pass"):

- **MANDATORY (owned by 06-03 Task 4, not this plan):** add the OPS-03 rendered `Vuln` check. This is the only mandatory phase-6 ruleset add.
- **OPTIONAL / ACCEPTED-DEFERRED (this plan's QUAL-01 ratchet):** the operator MAY also add `codecov/patch` + `codecov/project` in the same edit. Deferring them is a legitimate, documented outcome — QUAL-01 is met by the doc correction + the `.codecov.yml` ratchet either way. The default shipped state is "posts, not required."

**Verified live state (this session, `gh api repos/holomush/holomush/rulesets/11923801`):** required checks are exactly `[Build, Lint, Test, CodeRabbit, Integration Test, E2E Test]` — neither `Vuln` nor either codecov check is present yet.

**Doc-drift ordering (round-3 finding 11):** IF the codecov checks are added, a final docs edit is REQUIRED — tighten the Task 2 "posts, not required" rows in `.claude/rules/testing.md` + `CLAUDE.md` to "required/blocking" and re-run `task lint`. If codecov is deferred, the "posts, does not block" language stays as shipped (correct as-is).

## Next Phase Readiness
- QUAL-01 satisfied: documented bar and enforced bar now agree; a project ratchet guards whole-repo coverage.
- No blocker for plan/phase completion. The optional codecov ruleset add is a deferred ship-time enhancement (consolidate with 06-03's mandatory Vuln add in one operator pass).

## Self-Check: PASSED

- Files verified present: `.codecov.yml`, `.claude/rules/testing.md`, `CLAUDE.md`, `06-04-SUMMARY.md`
- Confirmed deleted: `codecov.yml`
- Commits verified in git: `fe5e9ace6` (Task 1), `aa2f1f9e6` (Task 2)

---
*Phase: 06-operational-hardening-assurance-gates*
*Completed: 2026-07-15*
