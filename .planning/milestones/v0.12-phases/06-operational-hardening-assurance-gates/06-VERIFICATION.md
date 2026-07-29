---
phase: 06-operational-hardening-assurance-gates
verified: 2026-07-15T00:00:00Z
status: passed
score: 10/10 must-haves verified
behavior_unverified: 0
overrides_applied: 0
re_verification:
  # No prior VERIFICATION.md — initial verification.
pending_operator_actions:  # Ship-time operator/deploy steps — NOT implementation gaps (see report)
  - action: "Add the rendered `Vuln` check to protect-main ruleset 11923801 (OPS-03 D-04 blocking-gate clause)"
    owner: "06-03 Task 4"
    why_deferred: "A ruleset required-check is verified against a LIVE PR's statusCheckRollup; no PR exists during execute-phase. REQUIREMENTS.md tracks OPS-03 as Pending for exactly this reason. The vuln: CI job already RUNS on every PR."
    mandatory: true
  - action: "06-01 + 06-02 co-ship as ONE PR; production migrate uses the deploy.yaml/sandbox choreography"
    owner: "06-02 Task 6 (co-ship) + Task 5 (choreography)"
    why_deferred: "Structurally guaranteed by the single phase branch; enforced at ship, not execute."
    mandatory: true
  - action: "Optionally add codecov/patch + codecov/project to ruleset 11923801"
    owner: "06-04 Task 3"
    why_deferred: "OPTIONAL / accepted-deferred. QUAL-01 is satisfied by the doc correction + .codecov.yml ratchet regardless. Docs already truthful for the deferred (posts-not-required) default."
    mandatory: false
---

# Phase 6: Operational Hardening & Assurance Gates Verification Report

**Phase Goal:** Close the remaining operational Highs and stand up the CI assurance gates the later phases rely on.
**Verified:** 2026-07-15
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

The phase goal is achieved in the codebase. All four ROADMAP success criteria
(OPS-02 events_audit retention, OPS-03 nats CVE + vuln gate, OPS-04 DLQ replay
game_id bridge, QUAL-01 coverage-policy reconciliation) are implemented, wired,
and verified against source — not merely claimed by the SUMMARYs. The only
outstanding items are documented **ship-time operator actions** (ruleset edits
against a live PR that cannot exist during execute-phase), correctly deferred and
tracked; these are NOT implementation gaps and are not scored as such.

### Observable Truths

| #  | Truth | Status | Evidence |
| -- | ----- | ------ | -------- |
| 1  | OPS-02: `events_audit` is RANGE-partitioned on a deterministic BIGINT `event_ms` (composite PK `(id, event_ms)`, **no** DEFAULT partition, legacy PK/indexes renamed `_legacy`) | ✓ VERIFIED | `000052_events_audit_partition.up.sql:57-124` — `PARTITION BY RANGE (event_ms)`, `PRIMARY KEY (id, event_ms)`, `RENAME ... _legacy`, no DEFAULT partition (grep confirmed none) |
| 2  | OPS-02: `writeAuditRow` dedups under the composite PK via `eventMsFromULID(id)` + `ON CONFLICT (id, event_ms) DO NOTHING`; `timestamp` column kept at store-time | ✓ VERIFIED | `projection.go:334-430` — `eventMS := eventMsFromULID(parsedID)`, `ON CONFLICT (id, event_ms)`; doc comment confirms timestamp unchanged |
| 3  | OPS-02: the RetentionWorker runs in production inside `SubsystemAuditProjection` and prunes on its cycle; synchronous Backfill+EnsurePartitions boot gate precedes the projection; worker uses initial-delay (no destructive prune on Start) | ✓ VERIFIED | `subsystem.go:293-353` — boot gate (Backfill@294, EnsurePartitions@297) BEFORE `newProjection`@314, then `NewRetentionWorker(..., WithSkipFirstRun())`@352; manager methods `EnsurePartitions/DetachExpiredPartitions/DropDetachedPartitions/Backfill` all present in `retention_partitions.go` |
| 4  | OPS-02: retention config validated — non-positive `RetainWindow`/`PurgeInterval` rejected in the tested `audit.Config` surface | ✓ VERIFIED | `subsystem.go:153-172` `Config.Validate()` rejects `RetainWindow<=0` and `PurgeInterval<=0`; called at `Start`@283 |
| 5  | OPS-03: `nats-server/v2` remediated to ≥ v2.14.3 (GHSA-q59r-vq66-pxc2 / CVE-2026-58207) | ✓ VERIFIED | `go.mod:22` `github.com/nats-io/nats-server/v2 v2.14.3` |
| 6  | OPS-03: the vuln gate CATCHES a vulnerable nats pin via the deterministic `cmd/nats-floor-guard` (the corrected mechanism — scanners are blind to the git-range-only OSV record); `task lint:vuln` runs three fail-closed legs | ✓ VERIFIED (behavioral) | Ran `nats-floor-guard` on current tree → exit 0; ran `TestCheckNatsFloor` (7 tests, PASS) — asserts v2.14.2/v2.10.0 FAIL, v2.14.3+ PASS. `Taskfile.yaml:746` three legs (floor guard → govulncheck → osv-scanner) judged by exit code |
| 7  | OPS-03: a `vuln:` CI job (rendered name `Vuln`) runs `task lint:vuln` on every PR; osv-scanner allowlist scoped to 5 test-only docker findings, tracked #4817 | ✓ VERIFIED | `ci.yaml:99-132` `vuln:` job, `name: Vuln`, `run: task lint:vuln`, checksum-pinned scanners; `osv-scanner.toml` `[[IgnoredVulns]]` GO-2026-4883/4887/… with `ignoreUntil` + #4817 |
| 8  | OPS-04: `runAuditDLQReplay` resolves game_id mirroring the server (`--game-id` override → `core.game_id` via `config.Load(..., "core")` → persisted DB), and the resolved id feeds `dlqConfigForGame` | ✓ VERIFIED | `cmd_audit.go:141-155` precedence override→coreGameID→DB; `:370` loads `"core"` section; `:373-378` resolved `gameID` → `dlqConfigForGame(gameID)` → `ReplayDLQ`. No `event_bus.game_id`, no `Defaults()` on the game_id path |
| 9  | OPS-04: the tautological same-`"main"` embedded-NATS test is REPLACED with a divergent-game `natstest` test driving the real resolver seam (failure guard + recovery) | ✓ VERIFIED | old `internal/eventbus/audit/dlq_replay_integration_test.go` DELETED; new `cmd/holomush/cmd_audit_dlq_replay_integration_test.go` uses `natstest.StartNATS`, asserts `Failed>0`/0 rows (wrong game) and `Replayed==1`/subject match (recovered from DB) |
| 10 | QUAL-01: coverage docs corrected to enforced reality (patch+project, never per-package, posts-not-required); `.codecov.yml` project ratchet `{auto, 1%}`, patch `{80%, 5%}`; duplicate `codecov.yml` deleted | ✓ VERIFIED | `.codecov.yml:36-44` project `target: auto`/`threshold: 1%`, patch `80%`/`5%`; `codecov.yml` deleted; `testing.md:23-30` + `CLAUDE.md:187` corrected, no fictional per-package MUST (negative grep clean) |

**Score:** 10/10 truths verified (0 present-behavior-unverified)

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | -------- | ------ | ------- |
| `internal/store/migrations/000052_events_audit_partition.{up,down}.sql` | Partition-swap, no DEFAULT, data-preserving down | ✓ VERIFIED | Both present; up = RANGE partition + composite PK + legacy rename; down data-preserving |
| `internal/eventbus/audit/projection.go` | `eventMsFromULID` + composite ON CONFLICT | ✓ VERIFIED | Present at :430 / :334 |
| `internal/eventbus/audit/retention_partitions.go` | PartitionManager + Backfill | ✓ VERIFIED | `NewEventsAuditPartitionManager`, Ensure/Detach/Drop/Backfill all present |
| `internal/eventbus/audit/subsystem.go` | RetentionWorker wired w/ boot gate | ✓ VERIFIED | Start ordering correct; worker started + Stop drains |
| `cmd/nats-floor-guard/main.go` | Deterministic floor guard @ v2.14.3 | ✓ VERIFIED | `natsSecurityFloor = "v2.14.3"`, table-driven test green |
| `Taskfile.yaml` (`lint:vuln`) | Three fail-closed legs by exit code | ✓ VERIFIED | :746 floor guard + govulncheck + osv-scanner |
| `.github/workflows/ci.yaml` (`vuln:`) | Rendered `Vuln` job on every PR | ✓ VERIFIED | :99 `name: Vuln` → `task lint:vuln` |
| `osv-scanner.toml` | Scoped allowlist w/ expiry + tracking | ✓ VERIFIED | 5 docker findings, `ignoreUntil`, #4817 |
| `cmd/holomush/cmd_audit.go` | `resolveGameID` + `dlqConfigForGame` | ✓ VERIFIED | Precedence + core-section load correct |
| `.codecov.yml` | project ratchet, single config | ✓ VERIFIED | `{auto,1%}` + patch `{80%,5%}`; dup deleted |

### Key Link Verification

| From | To | Via | Status |
| ---- | -- | --- | ------ |
| `writeAuditRow` (projection.go) + Backfill (retention_partitions.go) | `events_audit` composite PK | shared `eventMsFromULID` → identical dedup key across live/replay/backfill | ✓ WIRED |
| `SubsystemAuditProjection.Start` | `EventsAuditPartitionManager` + `RetentionWorker` | construct → Backfill → EnsurePartitions (boot gate) → projection → worker | ✓ WIRED |
| `runAuditDLQReplay` | `audit.ReplayDLQ` | `resolveGameID` → `dlqConfigForGame(gameID)` (subject prefix matches server) | ✓ WIRED |
| `ci.yaml vuln:` | `task lint:vuln` → `cmd/nats-floor-guard` | rendered `Vuln` context on PR | ✓ WIRED (blocking-gate ruleset add pending at ship) |
| `.codecov.yml` | protect-main ruleset | codecov POSTS statuses; blocks only when added to ruleset | ✓ WIRED (ratchet posts; ruleset add optional/deferred) |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| Floor guard passes on remediated tree | `go run ./cmd/nats-floor-guard` | exit 0, "pinned >= v2.14.3" | ✓ PASS |
| Floor guard catches vulnerable pins | `task test -- -run TestCheckNatsFloor ./cmd/nats-floor-guard/` | 7 tests PASS (v2.14.2/v2.10.0 → err; v2.14.3+ → ok) | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan(s) | Description | Status | Evidence |
| ----------- | -------------- | ----------- | ------ | -------- |
| OPS-02 | 06-01, 06-02 | events_audit growth bounded (RetentionWorker extended to it) | ✓ SATISFIED | Truths 1-4; REQUIREMENTS.md: Complete |
| OPS-03 | 06-03 | nats CVE remediated + vuln-scan CI gate | ✓ SATISFIED (impl); blocking-gate ruleset add pending at ship | Truths 5-7; REQUIREMENTS.md: Pending (documented D-04 operator step) |
| OPS-04 | 06-05 | DLQ replay recovers external-NATS + tautological test replaced | ✓ SATISFIED | Truths 8-9; REQUIREMENTS.md: Complete |
| QUAL-01 | 06-04 | coverage doc/CI reconciled (>80% MUST corrected) | ✓ SATISFIED | Truth 10; REQUIREMENTS.md: Complete |

All four phase requirement IDs are accounted for across the five plans. No
orphaned requirements (no additional Phase-6 IDs in REQUIREMENTS.md go unclaimed).

### Code-Review Fixes (06-REVIEW.md status: resolved — independently confirmed landed)

| Finding | Disposition | Verified in code |
| ------- | ----------- | ---------------- |
| WR-01 (search_path hardening on Backfill/rename/HealthCheck) | Fixed `bb9d68fc7` | ✓ `retention_partitions.go:377,473,496` use `public.events_audit` / schema-qualified rename |
| WR-02 (malformed legacy row bricks boot) | Fixed `695e40a7d` | ✓ `:429-501` skip-and-count (`skipped_malformed`), cursor advances every row |
| WR-03 (down doesn't clean runtime-detached partitions) | Accepted, tracked #4818 | ✓ documented; runtime residue self-heals on re-up |
| IN-01 (backfill re-stamps inserted_at) | Accepted | ✓ documented; cold filtering uses `timestamp` |
| IN-02 (deploy count(*) full scan) | Fixed `606d0bcd3` | ✓ `deploy.yaml:128` uses `pg_class.reltuples` estimate |

### Anti-Patterns Found

None blocking. Debt is tracked, not orphaned: `#4817` (osv allowlist expiry) and
`#4818` (down-migration detached cleanup) are referenced follow-ups. No
unreferenced TBD/FIXME/XXX in the phase surface.

### Pending Operator Actions at Ship (NOT gaps)

These are deployment/repo-settings steps that structurally cannot be performed
or verified during execute-phase (they act against a **live PR** or the GitHub
ruleset, neither of which exists yet). They are documented, owned, and — for
OPS-03 — reflected as `Pending` in REQUIREMENTS.md. They do **not** reduce the
verified score.

1. **OPS-03 D-04 (mandatory at ship):** add the rendered `Vuln` check to
   protect-main ruleset `11923801`. The `vuln:` CI job already RUNS on every PR
   today; making it a *blocking merge gate* is the only remaining step, verified
   against a live PR's `statusCheckRollup`.
2. **06-01+06-02 co-ship (mandatory at ship):** ship as ONE PR and use the
   `deploy.yaml`/`sandbox-operations.md` stop-traffic-path → migrate →
   readiness-gated-new-core choreography. Co-presence is structurally guaranteed
   by the single phase branch.
3. **QUAL-01 codecov ruleset add (optional / accepted-deferred):** QUAL-01 is met
   by the doc correction + `.codecov.yml` ratchet regardless; docs are already
   truthful for the shipped "posts, not required" default.

### Gaps Summary

No gaps. Every ROADMAP success criterion is implemented, wired, and behaviorally
or structurally verified against the codebase. The lone requirement carrying a
`Pending` status (OPS-03) is implementation-complete with a single documented
ship-time operator ruleset action — a PASS-with-documented-pending-operator-action,
not a missing must-have.

---

_Verified: 2026-07-15_
_Verifier: Claude (gsd-verifier)_
