---
phase: 06-operational-hardening-assurance-gates
plan: 03
subsystem: infra
tags: [supply-chain, govulncheck, osv-scanner, nats-server, ci, security, go-modules]

# Dependency graph
requires:
  - phase: 06-operational-hardening-assurance-gates
    provides: "phase branch + assurance-gate framing (06-01 events_audit partitioning landed prior)"
provides:
  - "cmd/nats-floor-guard: deterministic go.mod version-floor guard (compensating control for OSV blind spots)"
  - "task lint:vuln: three-leg supply-chain gate (nats floor guard + govulncheck + osv-scanner v2)"
  - "osv-scanner.toml allowlist (id + reason + ignoreUntil) — the only OSV suppression surface"
  - "ci.yaml vuln: job (rendered name Vuln) running the gate on every PR"
  - "nats-server/v2 remediated to v2.14.3 (GHSA-q59r-vq66-pxc2 / CVE-2026-58207, F8/#4790)"
affects: [06-04, ship, protect-main-ruleset]

# Tech tracking
tech-stack:
  added:
    - "govulncheck@v1.6.0 (golang.org/x/vuln), osv-scanner@v2.4.0 (github.com/google/osv-scanner/v2) — CI-pinned via go install + Go checksum DB"
    - "golang.org/x/mod (modfile + semver) — promoted to a direct dependency for the floor guard"
  patterns:
    - "Deterministic version-floor guard as a compensating control for advisories no manifest/reachability scanner can see (git-range-only OSV records)"
    - "Fail-closed multi-leg vuln gate judged by EXIT CODE (never stdout grep); OSV allowlist scoped to osv-scanner ONLY, govulncheck + floor guard have no suppression"

key-files:
  created:
    - "cmd/nats-floor-guard/main.go — the floor guard (checkNatsFloor + main)"
    - "cmd/nats-floor-guard/main_test.go — table-driven TDD test (fail < v2.14.3, pass >=)"
    - "osv-scanner.toml — allowlist for 5 test-only docker/docker findings"
  modified:
    - "Taskfile.yaml — lint:vuln three-leg gate"
    - ".github/workflows/ci.yaml — vuln: job (name: Vuln)"
    - "go.mod / go.sum — nats-server/v2 v2.14.3; x/mod promoted to direct"
    - ".planning/phases/06-operational-hardening-assurance-gates/06-03-PLAN.md — criteria #2/#3 corrected"

key-decisions:
  - "Assumption A4 (osv-scanner flags nats v2.14.2) is empirically FALSE — GHSA-q59r-vq66-pxc2 is a git-range-only OSV record with no Go-ecosystem package binding; no manifest/reachability scanner can flag it."
  - "User chose Option A: keep the scanner gate for forward coverage AND add a deterministic nats version-floor guard as the compensating control that actually proves criterion #2."
  - "5 docker/docker OSV findings are indirect/test-only (golang-migrate → dktest), not production-reachable, no upstream fix — allowlisted with ignoreUntil expiry, tracked in #4817, so the gate exits 0."
  - "CI scanners pinned via `go install @version` (verified against sum.golang.org) rather than a hand-copied release-binary SHA — a stronger, less error-prone supply-chain control, consistent with the local install."

patterns-established:
  - "Compensating control for scanner-invisible advisories: a small Go guard (modfile + semver) wired into lint:vuln, TDD-tested, no network, runs first."
  - "OSV allowlist entries MUST carry id + reason + ignoreUntil EXPIRY and cite a tracking issue."

requirements-completed: []  # OPS-03 is IMPLEMENTATION-complete; its D-04 blocking-gate clause is PENDING the operator ruleset action (Task 4). Not marked complete here — closed at ship.

coverage:
  - id: D1
    description: "nats-server/v2 remediated to v2.14.3 (GHSA-q59r-vq66-pxc2 / CVE-2026-58207, F8/#4790)"
    requirement: "OPS-03"
    verification:
      - kind: unit
        ref: "go.mod pins github.com/nats-io/nats-server/v2 v2.14.3; cmd/nats-floor-guard/main_test.go#TestCheckNatsFloor"
        status: pass
    human_judgment: false
  - id: D2
    description: "Deterministic nats version-floor guard fails lint:vuln below v2.14.3 (criterion #2 provable mechanism)"
    requirement: "OPS-03"
    verification:
      - kind: unit
        ref: "cmd/nats-floor-guard/main_test.go#TestCheckNatsFloor (fails v2.14.2/v2.10.0, passes v2.14.3/v2.14.9/v2.15.0)"
        status: pass
      - kind: other
        ref: "pre-bump: `go run ./cmd/nats-floor-guard` on nats v2.14.2 exits 1 naming floor v2.14.3"
        status: pass
    human_judgment: false
  - id: D3
    description: "task lint:vuln (three fail-closed legs) exits 0 on the clean post-bump tree"
    requirement: "OPS-03"
    verification:
      - kind: other
        ref: "`out=$(task lint:vuln); rc=$?` → rc=0 (floor guard pass + govulncheck 0 + osv-scanner 0 with docker filtered)"
        status: pass
    human_judgment: false
  - id: D4
    description: "5 test-only docker/docker OSV findings allowlisted in osv-scanner.toml; tracking issue #4817 filed"
    requirement: "OPS-03"
    verification:
      - kind: other
        ref: "osv-scanner scan source with --config filters GO-2026-4883/4887/5617/5668/5746 (+aliases), exits 0; gh issue #4817 open"
        status: pass
    human_judgment: false
  - id: D5
    description: "vuln: CI job (rendered name Vuln) runs task lint:vuln on every PR"
    requirement: "OPS-03"
    verification:
      - kind: automated_ui
        ref: "task lint:actions + task lint:yaml pass; first live run + rendered-context attachment observable only on a real PR"
        status: pass
    human_judgment: true
    rationale: "The job's actual run and its rendered `Vuln` statusCheckRollup context attach only on a live PR — confirmed at ship, not during execute-phase."
  - id: D6
    description: "Make the rendered Vuln check a REQUIRED protect-main ruleset check (D-04 blocking gate)"
    requirement: "OPS-03"
    verification: []
    human_judgment: true
    rationale: "PENDING OPERATOR STEP (plan Task 4). Repo-settings/ruleset action, not an in-repo edit; verified against a live PR's statusCheckRollup which does not exist during execute-phase. See Pending Operator Step below."

# Metrics
duration: ~30min
completed: 2026-07-15
status: complete
---

# Phase 6 Plan 03: nats CVE remediation + supply-chain vuln gate Summary

**Bumped nats-server/v2 to v2.14.3 and stood up a three-leg `task lint:vuln` gate — a deterministic go.mod version-floor guard (the real fix for a scanner-invisible OSV record) plus govulncheck + osv-scanner v2 — with the docker test-only findings allowlisted and a `Vuln` CI job wired in.**

## Performance

- **Duration:** ~30 min (continuation executor)
- **Started:** 2026-07-15T07:40:00Z (approx)
- **Completed:** 2026-07-15T07:53:00Z (approx)
- **Tasks:** Task 1 pre-satisfied (tools pinned); Tasks 2–3 + Option-A re-scope implemented; Task 4 PENDING operator
- **Files modified:** 8 (293 insertions, 5 deletions)

## Accomplishments
- **nats-server/v2 → v2.14.3** — the real CVE remediation (GHSA-q59r-vq66-pxc2 / CVE-2026-58207, closes F8/#4790). Surgical `go get`; `go-retry` and all other deps untouched.
- **cmd/nats-floor-guard** — a TDD-tested deterministic guard (modfile + semver) that hard-fails `lint:vuln` if `go.mod` pins nats-server/v2 below v2.14.3. This is the compensating control for the OSV blind spot and the provable mechanism for criterion #2.
- **task lint:vuln** finalized as three fail-closed legs: (1) floor guard (no network, runs first), (2) govulncheck, (3) osv-scanner v2. Exits 0 on the clean post-bump tree.
- **osv-scanner.toml** allowlist for the 5 indirect/test-only `github.com/docker/docker` findings (no upstream fix, not production-reachable), each with an `ignoreUntil` expiry and issue link; tracking issue **holomush/holomush#4817** filed.
- **ci.yaml `vuln:` job** (explicit `name: Vuln`) runs the gate on every PR with checksum-verified pinned scanners.

## Task Commits

1. **Floor guard RED test** - `e9292718b` (test)
2. **Floor guard GREEN + wire into lint:vuln** - `6ef38b2dc` (feat)
3. **osv-scanner allowlist (docker test-only vulns)** - `a1b173ee5` (chore)
4. **Bump nats-server/v2 → v2.14.3** - `3b6a45bf8` (fix)
5. **vuln: CI job (name Vuln)** - `a05276a23` (feat)
6. **Correct plan criteria #2/#3 + DEVIATION NOTE** - `a07863a8e` (docs)

_TDD: commits 1 (RED) → 2 (GREEN) form the floor-guard gate sequence._

## Files Created/Modified
- `cmd/nats-floor-guard/main.go` — `checkNatsFloor(gomod, floor)` (modfile.Parse + semver.Compare) + `main` reading `go.mod`; exits non-zero below floor, absent, or non-semver.
- `cmd/nats-floor-guard/main_test.go` — table-driven test: fails v2.14.2/v2.10.0, passes v2.14.3/v2.14.9/v2.15.0, plus absent-from-go.mod case (7 tests).
- `osv-scanner.toml` — 5 `[[IgnoredVulns]]` entries (GO-2026-4883/4887/5617/5668/5746) with reason + `ignoreUntil` 2027-01-15; header documents the OSV-only exception policy.
- `Taskfile.yaml` — `lint:vuln` three-leg gate (outside the `lint:` umbrella).
- `.github/workflows/ci.yaml` — `vuln:` job, `name: Vuln`, scanners via `go install @pinned`.
- `go.mod` / `go.sum` — nats-server/v2 v2.14.3; `golang.org/x/mod` promoted to direct (imported by the guard).
- `06-03-PLAN.md` — criteria #2/#3 rewritten + DEVIATION NOTE for the verifier.

## Decisions Made
See `key-decisions` frontmatter. Core: assumption A4 was empirically false, so the gate's criterion-#2 proof moved from a scanner citation to a deterministic version-floor guard (user-chosen Option A).

## Deviations from Plan

This plan was a re-scope of a VERIFIED Rule-4 architectural blocker. The prior executor correctly halted when the plan's assumption A4 (osv-scanner would flag nats v2.14.2 and cite GHSA-q59r-vq66-pxc2) proved false. The orchestrator independently confirmed it; the user chose Option A. This is the sanctioned re-scope, not silent scope creep.

### Deviation 1 — [Rule 4, architectural] Assumption A4 empirically false → floor-guard mechanism

- **Found during:** Task 2 (prior executor) and re-confirmed here.
- **Empirical finding (re-verified in this session):**
  - `osv-scanner scan source -L go.mod` on nats v2.14.2 surfaces ONLY 5 `github.com/docker/docker` findings — **zero** nats findings. (`GHSA-q59r-vq66-pxc2` / `CVE-2026-58207` is `package: None`, ranges `['GIT']` — a git-commit-range-only OSV record with no Go-ecosystem package binding.)
  - `govulncheck ./...` → exit 0 (no reachable nats record in the Go vuln DB).
  - So NO manifest/reachability scanner can fail on nats v2.14.2.
- **Fix (Option A):** bump nats → v2.14.3 (real remediation) + add `cmd/nats-floor-guard` as the deterministic compensating control that proves criterion #2, + keep the scanner gate for forward coverage, + allowlist the pre-existing test-only docker findings so the gate exits 0.
- **Files:** all in this plan.
- **Verification:** floor-guard unit test (7 pass); `task lint:vuln` exits 0 post-bump; `task lint` + `task build` green.
- **Committed in:** `e9292718b`, `6ef38b2dc`, `a1b173ee5`, `3b6a45bf8`, `a05276a23`, `a07863a8e`.

### Deviation 2 — [Rule 2, missing critical] Pre-existing docker/docker OSV findings block the gate

- **Found during:** finalizing `lint:vuln` — osv-scanner returns rc=1 on the 5 docker findings regardless of the nats bump.
- **Issue:** `github.com/docker/docker v28.5.2+incompatible` is an indirect, test-only dep (internal/store → golang-migrate/pgx test harness → dktest → docker/docker), all "FIXED VERSION --" (0 fixable). Not production-reachable, but keeps the osv-scanner leg red.
- **Fix:** allowlisted the 5 IDs in `osv-scanner.toml` (id + reason + `ignoreUntil`), filed tracking issue **#4817** (remove on upstream fix or when golang-migrate/dktest drops docker/docker).
- **Committed in:** `a1b173ee5`.

---

**Total deviations:** 1 architectural re-scope (user-approved Option A) + 1 missing-critical allowlist. **Impact:** the gate is now honest and green; the nats CVE is genuinely remediated AND regression-guarded. No unrelated scope touched (go-retry left as-is per instruction).

## Issues Encountered
- `go run` from a temp dir cannot resolve the in-module guard package (expected — the guard reads the cwd `go.mod`); the durable proof is the unit test plus the real pre-bump run. Resolved by relying on those.
- The `enforce-task-runner.sh` hook blocks raw `go build`/`go test`; used `task build` / `task test -- ./cmd/nats-floor-guard/` throughout.

## User Setup Required — PENDING OPERATOR STEP (plan Task 4, D-04 blocking gate)

**The `vuln:` CI job RUNS and REPORTS but does not yet BLOCK merges.** Making it a blocking gate is a GitHub repo-settings action that CANNOT be done during execute-phase (it verifies against a live PR's statusCheckRollup, which does not exist yet). Do this at ship:

1. **GitHub → repo Settings → Rules → `protect-main` ruleset → "Require status checks to pass"** → add the rendered check **`Vuln`** (the job's `name:`, NOT the workflow key `vuln`). Optionally add `codecov/patch` + `codecov/project` in the same edit per 06-04 Task 3.
2. Confirm via API:
   ```
   gh api repos/holomush/holomush/rulesets/11923801 \
     --jq '.rules[]?|select(.type=="required_status_checks")|.parameters.required_status_checks[]?.context'
   ```
   `Vuln` must appear in the list.
3. Confirm on a REAL PR: `gh pr view <n> --json statusCheckRollup` shows a `Vuln` context (the rendered name actually attaches).

**Current live state (verified this session):** required checks are `[Build, Lint, Test, CodeRabbit, Integration Test, E2E Test]` — `Vuln` is NOT among them yet, so Task 4 is genuinely pending.

## Pinned tool coordinates (Task 1)
- `govulncheck` — `golang.org/x/vuln/cmd/govulncheck@v1.6.0` (official Go team tool). Local + CI via `go install`, verified against sum.golang.org.
- `osv-scanner` — `github.com/google/osv-scanner/v2/cmd/osv-scanner@v2.4.0` (Google-maintained). Local + CI via `go install`, verified against sum.golang.org. `osv-scanner --version` → `2.4.0`.

## Next Phase Readiness
- Supply-chain gate is live and green; forward coverage for future Go-ecosystem-bound vulns is in place.
- **OPS-03 is implementation-complete; its D-04 blocking clause is the one pending operator ruleset action above** (enumerated alongside the codecov ratchet in 06-04 Task 3). Ship must complete it to close OPS-03.

## Self-Check: PASSED
- Created files verified on disk: `cmd/nats-floor-guard/main.go`, `cmd/nats-floor-guard/main_test.go`, `osv-scanner.toml`, `06-03-SUMMARY.md`.
- Task commits verified in git: `e9292718b`, `6ef38b2dc`, `a1b173ee5`, `3b6a45bf8`, `a05276a23`, `a07863a8e`.
- `go.mod`: nats-server/v2 pinned v2.14.3; `go-retry` untouched.
- Gates green: `task lint:vuln` rc=0, `task lint` rc=0, `task build` rc=0, floor-guard unit test 7/7.

---
*Phase: 06-operational-hardening-assurance-gates*
*Completed: 2026-07-15*
