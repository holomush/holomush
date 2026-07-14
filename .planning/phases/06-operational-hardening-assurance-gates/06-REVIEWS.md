---
phase: 6
review_round: 3
reviewers: [codex, opencode, antigravity]
reviewed_at: 2026-07-14T17:13:31Z
plans_reviewed: [06-01-PLAN.md, 06-02-PLAN.md, 06-03-PLAN.md, 06-04-PLAN.md, 06-05-PLAN.md]
models: {codex: default, opencode: openrouter/x-ai/grok-4.5, antigravity: default}
prior_round: "round 2 REVIEWS.md at commit c62e2dc54 (git history)"
---

# Cross-AI Plan Review — Phase 6 (ROUND 3, post round-2 incorporation)

> Round 3 verifies the /gsd-plan-phase 6 --reviews revision (commit 92bc0ecd2) that addressed
> the round-2 consensus findings. **Codex** and **OpenCode (grok-4.5)** read live source on
> branch gsd/phase-06-operational-hardening-assurance-gates (grounded, high weight).
> **Antigravity** is grounded-to-plan but low-weight (marked F3/F6/F13 fixed that the grounded
> pair found only partial). Reviewers were given the 13 round-2 findings and told to trace each
> against source, not trust the claim. No reviewer modified the tree.

---

## Codex Review

# Phase 6 Round-3 Plan Review

## Overall verdict: NOT READY

Overall risk: **HIGH**

The revised architecture is substantially better, and most round-2 findings are addressed. However, execution still has four blocking defects:

1. 06-01’s seed sweep both misses a required test and instructs an invalid edit to a version-pinned migration test.
2. 06-02 does not recover an interrupted `DETACH PARTITION CONCURRENTLY` pending state.
3. Negative retention configuration can prune all partitions or panic the process.
4. The supposedly mandatory coverage ruleset checks remain explicitly deferrable.

Several automated verification commands also mask test failures through unguarded `| tail` pipelines.

## 06-01 — Partition migration and write-path idempotency

### Summary

The deterministic `event_ms` design, composite key, preserved store-time semantics, and legacy index renames are sound. The test/seed sweep is not executable as written.

### Strengths

- The ULID correction is exact: the plan specifies `ulid.Time(id.Time()).UnixNano()` and requires `task build` ([06-01:221](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:221)). This matches the live source issue: `decodeULIDString` currently returns only bytes ([projection.go:509](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/audit/projection.go:509)).
- Preserving `timestamp = pgnanos.From(meta.Timestamp)` is correct because cold queries filter that column directly ([projection.go:425](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/audit/projection.go:425), [cold_postgres.go:159](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/history/cold_postgres.go:159)).
- The legacy rename list covers the actual PK and five indexes created across migrations 000009, 000011, and 000014 ([000009:4](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/store/migrations/000009_create_events_audit.up.sql:4), [000011:9](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/store/migrations/000011_events_audit_js_seq_index.up.sql:9), [000014:13](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/store/migrations/000014_events_audit_dek_columns.up.sql:13)).
- Ownership, no-DEFAULT, and data-preserving rollback are explicit acceptance checks ([06-01:191](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:191)).

### Concerns

- **HIGH — The direct-insert sweep is wrong in both directions.** It omits `cmd/holomush/bootstrap_orphan_test.go`, which applies all migrations and then inserts without `event_ms` ([bootstrap_orphan_test.go:49](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/cmd/holomush/bootstrap_orphan_test.go:49), [bootstrap_orphan_test.go:58](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/cmd/holomush/bootstrap_orphan_test.go:58)). Conversely, it includes `migrate_plugins_integration_test.go`, whose insert intentionally runs at schema version 17 before `event_ms` exists ([migrate_plugins_integration_test.go:60](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/store/migrate_plugins_integration_test.go:60)). Adding `event_ms` there would break the historical migration test.
- **MEDIUM — The down migration is not fully specified as idempotent.** The legacy copy gets `ON CONFLICT`, but the parent-to-temp copy does not ([06-01:176](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:176)). A resumed partial down can encounter duplicates.
- **MEDIUM — Automated checks are fail-open.** All three verify commands pipe through `tail` without `pipefail`, so `tail` can return zero after a failed test ([06-01:189](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:189), [06-01:298](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:298)).
- **MEDIUM — No dedicated migration-52 test artifact is named.** The plan demands ownership and up/down data-preservation assertions but only runs the existing store suite.

### Suggestions

- Exclude version-pinned pre-52 migration tests from the mechanical sweep.
- Add `bootstrap_orphan_test.go` to 06-01 or move its compatibility edit ahead of 06-01’s full-suite gate; use a real ULID rather than its current arbitrary byte string.
- Add a dedicated `migrations_events_audit_partition_integration_test.go`.
- Replace piped verification with exit-code capture or `set -o pipefail`.

Risk assessment: **HIGH**

## 06-02 — Retention manager, backfill, and lifecycle wiring

### Summary

The lifecycle order and backfill strategy are strong, but the retention worker is not yet crash-safe or configuration-safe.

### Strengths

- The plan correctly reorders the current implementation, which presently starts the projection at [subsystem.go:229](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/audit/subsystem.go:229). The proposed order is backfill → ensure → projection → periodic worker ([06-02:234](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:234)).
- The synchronous boot gate deliberately avoids `RunOnce`, whose current implementation includes detach and drop ([retention.go:63](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/audit/retention.go:63)).
- Backfill reuse of the shared ULID helper and the migration-straddle test are appropriate.
- The orphan-check extension addresses the live query’s current single-table blind spot ([bootstrap_orphan.go:28](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/cmd/holomush/bootstrap_orphan.go:28)).

### Concerns

- **HIGH — Interrupted concurrent detach is not recovered.** The proposed reconciliation only finds tables no longer present in `pg_inherits` ([06-02:126](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:126)). PostgreSQL runs concurrent detach in two internal transactions; interruption can leave a partition marked pending detach, which must be completed with `DETACH PARTITION … FINALIZE`. The proposed test manually completes detach and merely skips rename, so it never exercises this state. See the [official PostgreSQL `DETACH PARTITION` documentation](https://www.postgresql.org/docs/current/sql-altertable.html).
- **HIGH — Retention configuration is not validated.** The plan defaults only zero values and allows negatives ([06-02:227](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:227)). A negative `RetainWindow` makes the cutoff future-facing and can detach every partition ([retention.go:82](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/audit/retention.go:82)); a negative `PurgeInterval` reaches `time.NewTicker` and panics ([retention.go:127](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/audit/retention.go:127)).
- **MEDIUM — Catalog-name reconciliation lacks durable provenance.** Any canonical-looking non-child table can be renamed and later dropped. Schema qualification and a pre-detach marker or metadata record would make this safer.
- **MEDIUM — Every automated test command uses a fail-open `| tail` pipeline.**

### Suggestions

- Detect `pg_inherits.inhdetachpending`; run `DETACH PARTITION … FINALIZE` before normal reconciliation, with an injected interruption test.
- Reject `RetainWindow <= 0` and `PurgeInterval <= 0` during configuration validation.
- Schema-qualify all catalog discovery and record durable detach provenance.
- Make verification pipelines preserve the test process’s exit code.

Risk assessment: **HIGH**

## 06-03 — Vulnerability gate

### Summary

The two-scanner mechanism correctly addresses govulncheck’s blind spot, but the CI/ruleset identity and empty allowlist format need tightening.

### Strengths

- The current vulnerable pin is confirmed at [go.mod:22](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/go.mod:22).
- The plan correctly requires both govulncheck and OSV-Scanner v2, and its v2 command form is supported by the [official OSV-Scanner usage documentation](https://google.github.io/osv-scanner/usage/).
- Pre-bump verification captures the real exit code and independently checks the advisory ID ([06-03:176](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-03-PLAN.md:176)).
- The scanner legitimacy and checksum checkpoints are appropriately blocking.

### Concerns

- **HIGH — The required status context is underspecified.** The plan says the job key and required check are `vuln`, but GitHub rulesets use the rendered job `name`. Current workflow key `lint` renders as `Lint`, and the live ruleset requires `Lint`, not `lint` ([ci.yaml:36](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.github/workflows/ci.yaml:36)). The new job must set an exact stable name and the ruleset must require that exact context.
- **MEDIUM — “Empty `[[IgnoredVulns]]` scaffold” is ambiguous and likely invalid.** That syntax creates an array entry; official examples pair every such entry with an `id` ([OSV-Scanner configuration](https://google.github.io/osv-scanner/configuration/)). Use a truly empty config or `IgnoredVulns = []`, then validate it by running the scanner.
- **MEDIUM — PATTERNS still shows the old v1 scanner command** at [06-PATTERNS:194](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-PATTERNS.md:194), while the plan requires v2.

### Suggestions

- Define `name: vuln` explicitly—or select another exact name consistently—and verify it in a real PR’s `statusCheckRollup`, not only ruleset JSON.
- Seed a syntactically valid empty OSV config.
- Refresh the stale OPS-03 PATTERNS section.

Risk assessment: **HIGH**

## 06-04 — Coverage policy

### Summary

The source-state diagnosis is correct, but the plan does not make all three assurance checks mandatory as claimed.

### Strengths

- Live ruleset inspection confirms only Build, Lint, Test, CodeRabbit, Integration Test, and E2E Test are required. This agrees with workflow job names such as [ci.yaml:99](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.github/workflows/ci.yaml:99).
- The duplicate configurations and current thresholds are correctly identified ([.codecov.yml:23](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.codecov.yml:23), [codecov.yml:1](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/codecov.yml:1)).
- The fictional per-package claims are real ([testing.md:25](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.claude/rules/testing.md:25), [CLAUDE.md:187](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/CLAUDE.md:187)).
- If ruleset state changes, the final doc-tightening is explicitly required ([06-04:193](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-04-PLAN.md:193)).

### Concerns

- **HIGH — The mandatory checkpoint permits coverage deferral.** The addendum requires `vuln`, `codecov/patch`, and `codecov/project` to be added. The plan instead says the two Codecov checks “MAY be deferred” ([06-04:190](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-04-PLAN.md:190)) and treats that as a successful checkpoint outcome ([06-04:219](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-04-PLAN.md:219)).
- **MEDIUM — The top-level must-have is incompatible with the successful “checks added” branch.** It requires final docs to say Codecov is not required, while Task 3 correctly requires those docs to change if checks are added.
- **LOW — PATTERNS still contains a stale “already ruleset-enforced hard gate” comment** at [06-PATTERNS:259](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-PATTERNS.md:259), contradicting the corrected section below it.

### Suggestions

- Remove the coverage-deferral success branch if round-2 finding 6 is to be considered closed.
- Make final must-haves conditional on the verified final ruleset state.
- Remove the stale PATTERNS comment.

Risk assessment: **HIGH**

## 06-05 — DLQ game ID resolution

### Summary

The executable task body now uses the correct server precedence and test tier. Stale plan and PATTERNS language can still steer implementation back to the rejected order.

### Strengths

- The live server loads `core.game_id` from the `core` section ([core.go:64](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/cmd/holomush/core.go:64), [core.go:125](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/cmd/holomush/core.go:125)) and prefers it over DB state ([core.go:300](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/cmd/holomush/core.go:300)). The task body mirrors that exactly.
- The current bug is accurately located: replay still passes `event_bus.game_id` ([cmd_audit.go:325](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/cmd/holomush/cmd_audit.go:325)).
- The replacement test uses the real `natstest` transport and the actual resolver seam. Fresh ordered consumers restart from the beginning, so the wrong-game pass followed by recovery is valid ([replay.go:106](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/audit/replay.go:106)).

### Concerns

- **MEDIUM — Stale contradictory guidance remains.** Task 1’s title still says “configured root game_id” ([06-05:87](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-05-PLAN.md:87)), and PATTERNS still prescribes override → DB → `cfg.GameID`, the rejected order ([06-PATTERNS:233](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-PATTERNS.md:233)).
- **MEDIUM — Both automated test commands mask failures through `| tail`** ([06-05:124](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-05-PLAN.md:124), [06-05:173](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-05-PLAN.md:173)).

### Suggestions

- Replace the remaining “root” wording with `core.game_id`.
- Refresh PATTERNS to override → `core.game_id` → DB.
- Preserve task exit codes in verification.

Risk assessment: **MEDIUM**

## Round-3 finding verification

| # | Status | Verification |
|---|---|---|
| 1. ULID compile fix | **CLOSED** | Correct helper and `task build` acceptance are explicit; live `decodeULIDString` indeed returns `[]byte` ([projection.go:511](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/audit/projection.go:511)). |
| 2. Legacy PK/index rename | **CLOSED** | All live names are enumerated, renamed before parent creation, and ownership is asserted ([06-01:147](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:147)). |
| 3. Test/seed blast radius | **NOT CLOSED** | Missing all-migrations bootstrap test; wrongly includes version-17 migration test. |
| 4. Co-deploy + Start order | **CLOSED** | Both plans declare single-ship-unit; Start ordering is explicit ([06-02:25](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:25)). |
| 5. Bootstrap orphan check | **CLOSED** | 06-02 includes both implementation and test files and handles table absence. |
| 6. Mandatory ruleset gate | **PARTIAL** | `vuln` is mandatory, but both Codecov checks may still be deferred. |
| 7. Crash-atomic detach | **CLOSED narrowly** | The exact post-detach/pre-rename window and test are present. New defect: interrupted detach/pending-finalization is unhandled. |
| 8. Exit-code capture | **CLOSED** | Pre-bump command captures `rc` and separately checks GHSA evidence. |
| 9. First-cycle scope | **CLOSED** | Boot gate is Backfill + Ensure only ([06-02:237](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:237)). |
| 10. Start order / dual-copy | **CLOSED** | Backfill → ensure → projection → worker is explicit and the window is documented. |
| 11. Doc-tighten required | **CLOSED** | Final doc update and lint are mandatory when checks are added. |
| 12. PATTERNS events-audit refresh | **CLOSED narrowly** | The events-audit section now reflects `event_ms`, no DEFAULT, Go backfill, and unchanged timestamp. Other PATTERNS sections remain stale. |
| 13. `core.game_id` precision | **PARTIAL** | The executable body is correct, but the task title and PATTERNS still contain stale root/DB-first guidance. |

No files were modified; the worktree remains clean.

---

## OpenCode Review (grok-4.5)

I'll verify plan claims against live source and check each round-2 finding for closure.
Next I'll check the load-bearing source claims the plans depend on.
Digging into the specific source sites the plans hinge on.
# Phase 6 Plan Review — Round 3

Source-checked on `gsd/phase-06-operational-hardening-assurance-gates` against live code. Plans are executable against current source in substance; a few residual gaps remain.

---

## Round-2 Finding Closure Matrix

| # | Finding | Status | Evidence |
|---|---------|--------|----------|
| 1 | ULID compile fix | **CLOSED** | Plans require `eventMsFromULID` = `ulid.Time(id.Time()).UnixNano()`; `oklog/ulid/v2@v2.1.1` confirms `Time(ms uint64)` + `(id ULID).Time() uint64` (`go.mod:25`, module API). `decodeULIDString` returns `([]byte, error)` only (`projection.go:511–517`). Shared reuse by 06-01 write path + 06-02 Backfill same-package; `task build` acceptance present. |
| 2 | Legacy PK/index rename | **CLOSED** | 06-01 Task 1 steps (2)/(3): rename `events_audit_pkey` + indexes to `_legacy` before parent create. Live names match: `000009` PK/indexes + `000011` `events_audit_subject_js_seq` + `000014` `events_audit_dek_ref`. Acceptance requires **ownership** (`indrelid` = parent), not name existence. |
| 3 | Test/seed blast radius | **PARTIAL** | Task 3 re-derives via `rg` and claims whole-suite green. Live `INSERT INTO events_audit` sites = 16 files. `files_modified` omits `cmd/holomush/bootstrap_orphan_test.go:58`. Also: intermediate-migration test `migrate_plugins_integration_test.go:62–65` inserts at **migration 17** (pre-000052, no `event_ms`); blind “every INSERT lists event_ms” acceptance **breaks** that test. Seeds using `gen_random_bytes(16)` (`holomushtest/server.go:777`, `harness_impl_test.go:194`) are not ULIDs — ULID-only `event_ms` formula does not apply. |
| 4 | 06-01↔06-02 co-deploy + Start order | **CLOSED** (design) / **MEDIUM process risk** | Both plans `must_haves` co-deploy. 06-02 Task 3 reorders Start: Backfill → Ensure → projection → worker. Today Start starts projection first (`subsystem.go:229–236`). No mechanical PR-gate for co-ship; waves allow 06-01 “complete” alone if ship discipline slips. |
| 5 | Bootstrap orphan-check | **CLOSED** | 06-02 Task 4 extends `runBootstrapOrphanCheck`; file in `files_modified`. Live only scans `events_audit` (`bootstrap_orphan.go:28–45`), called before audit Start (`core.go:296`). |
| 6 | Mandatory ruleset gate | **CLOSED** | 06-03 Task 4 + 06-04 Task 3 are `checkpoint:human-action`, IDs listed, `gh api …/rulesets/11923801` verify. Live ruleset required: `Build, Lint, Test, CodeRabbit, Integration Test, E2E Test` — no `vuln` / codecov. |
| 7 | Crash-atomic detach | **CLOSED** | 06-02 Task 1: reconcile non-child `events_audit_YYYY_MM` → `_detached_<unix>`; injected-crash test required. |
| 8 | Exit-code capture | **CLOSED** | 06-03 verify: `out=$(…); rc=$?; test "$rc" -ne 0` + separate GHSA grep (not `; echo`). |
| 9 | First-cycle scope | **CLOSED** (for dual-copy intent) | Boot gate = Backfill + Ensure only; not `RunOnce` (`retention.go:63–98` would Detach/Drop). Note: `RetentionWorker.run` still `RunOnce`s immediately after worker Start (`retention.go:133–135`) — prune can start moments later; not the dual-copy race. |
| 10 | Start order / dual-copy | **CLOSED** | Explicit order + dual-existence documented; gate fails before projection if Backfill/Ensure fail (`Start` returns nil forever from worker today — `retention.go:104–108`). |
| 11 | Doc-tighten required | **CLOSED** | 06-04 Task 3: IF codecov added → REQUIRED re-tighten + `task lint`; else keep “posts, not blocking”. |
| 12 | PATTERNS refresh | **CLOSED** | `06-PATTERNS.md` documents `event_ms`, no DEFAULT, Go Backfill, `timestamp` untouched, `_legacy` rename, crash reconcile. |
| 13 | `core.game_id` precision | **CLOSED** | 06-05: override → `config.Load(..., "core")` `GameID` → DB. Matches `core.go:70,125,300–302`. Forbids `event_bus.GameID` + `Defaults()` forcing `"main"` (`config.go:153–154`; `dlq.go:26,37`). Bug site still `cmd_audit.go:325` `dlqConfigForGame(cfg.GameID)` after `loadEventBusConfig` only. |

**No round-1 regressions** on DEFAULT partition / timestamp flip / ATTACH-as-DEFAULT.

---

## 06-01 — OPS-02 core (partition + writeAuditRow)

### Summary
Correct redesign: separate `event_ms`, leave store-time `timestamp`, no DEFAULT, data-preserving down, `_legacy` index rename, shared ULID helper, crypto gate. Executable vs `projection.go:414–435`, `cold_postgres.go:140,159–183`, migration history through `000051`.

### Strengths
- Composite PK + `ON CONFLICT (id, event_ms)` + ULID-derived key fixes live/replay store-time split (`projection.go:346–351,425`; `replay.go:219`).
- Cold path only filters `timestamp` (`cold_postgres.go:159–183`); leaving it store-time is sound.
- crypto-review surface + blocking checkpoint appropriate (`projection.go` + `events_audit` migration).

### Concerns
- **MEDIUM — Task 3 over-sweep / incomplete list:** (a) `files_modified` misses `cmd/holomush/bootstrap_orphan_test.go`; (b) `migrate_plugins_integration_test.go:62–65` is version-17 shape — do not force `event_ms`; (c) non-ULID ids need explicit seed rule (`timestamp`/`now`/`pgnanos.From` for `event_ms`, not `eventMsFromULID`).
- **MEDIUM — co-deploy process:** Wave-1 complete without 06-02 = cold-history blackout after rename. Needs explicit “single PR / do not ship 06-01 alone” in execute/ship path, not only `must_haves`.
- **LOW:** Down-migration ordering is careful; executor must not invent “drop parent first without copy.”

### Suggestions
1. Carve Task 3 acceptance: full-head inserts need `event_ms`; pre-000052 step migrations exempt; document non-ULID seed `event_ms` strategy.
2. Add `bootstrap_orphan_test.go` to `files_modified`.
3. Hold merge packaging until 06-02 lands in same PR.

### Risk: **MEDIUM**

---

## 06-02 — OPS-02 worker

### Summary
Addresses unwired `RetentionWorker` (only production-like symbols in `internal/audit/retention.go`; tests only). Full PartitionManager + Backfill + Start reorder + orphan scan is the right synthetic ops-02 completion of 06-01.

### Strengths
- Interface match (`retention.go:31–38`); Ensure/Detach/Drop/Purge/Health mapped correctly.
- Synchronous gate before projection fixes today's projection-first Start (`subsystem.go:229–236`) and dead “Start always returns nil” worker path (`retention.go:104–108`).
- Crash reconcile + straddle test + orphan-unpartitioned scan close real restore/history holes.

### Concerns
- **MEDIUM — periodic worker immediate prune:** After Start succeeds, `run()` still calls full `RunOnce` immediately (`retention.go:133–135`), i.e. Detach/Drop. Gate correctly avoids prune-before-traffic; addition of “no prune on boot day” is not fully true. Document operator expectation (first async cycle may prune old partitions).
- **LOW — EnsurePartitions API:** Worker always passes `months=3` (`retention.go:68`); backward span must come from manager config, not that arg — plan spells this; keep it in constructor comments/tests.
- **LOW:** `cmd/holomush/core.go` is codecov-ignored (`.codecov.yml:57`) — RetainWindow plumbing strip must live in tested `audit.Config` / `eventbus` config, not only core.

### Suggestions
- Unit-test mapping: RetainWindow → `RetentionConfig.RetainDenials`; zero PurgeInterval default.
- Name config distinctly from `DLQ.MaxAge` (`config.go:101–103`, `162–163`).

### Risk: **MEDIUM** (depends on co-ship with 06-01)

---

## 06-03 — OPS-03 vuln

### Summary
Spot-on vs govulncheck DB gap + GHSA reality. Two-tool gate, pre-bump fail-by-exit-code, legitimacy pin, mandatory ruleset for `vuln` satisfy success criterion #2 design.

### Strengths
- `nats-server/v2 v2.14.2` still pinned (`go.mod:22`); client deps correctly left alone.
- Fail-closed exception policy (OSV allowlist only; govulncheck hard stop) is honest.
- Ruleset forced (`gh` verified empty of `vuln`).

### Concerns
- **LOW — A4 residual:** Assertion “OSV cites GHSA-q59r-vq66-pxc2 pre-bump” is the right prove-or-stop gate; keep Task 2 as hard blocker if OSV misses it.
- **LOW:** Dual human checkpoints (Task 1 pin + Task 4 ruleset) are heavy but appropriate for supply-chain.

### Suggestions
No structural change. Ensure pin coordinates recorded before CI install.

### Risk: **LOW**

---

## 06-04 — QUAL-01 coverage

### Summary
Correctly **overrides** false D-07 premise. Live ruleset has no codecov checks; docs-to-reality + ratchet + dual-file cleanup + consolidated ruleset checklist is coherent.

### Strengths
- `target: auto` + `threshold: 1%` matches two-upload jitter (`after_n_builds: 2` at `.codecov.yml:20`; current project `80%/2%` at `:27–28`).
- Doc branch for add vs defer prevents new false MUSTs.
- Dual file still present (`.codecov.yml` + `codecov.yml` 375B) — delete is right.

### Concerns
- **LOW:** Criterion #4 can pass on docs only if codecov checks deferred — intentional; SUMMARY must not claim blocking snapshot coverage without ruleset proof.
- **LOW:** `AGENTS.md` → `CLAUDE.md` symlink; editing `CLAUDE.md` is sufficient.

### Risk: **LOW**

---

## 06-05 — OPS-04 DLQ game_id

### Summary
Fix topology matches server (`core.go:300–302`) and current bug (`cmd_audit.go:308,325` + empty→`internal.main.audit.dlq` in `dlq.go:26,37,69–75`). Non-tautological natstest plan correctly rejects same-game embedded test (`dlq_replay_integration_test.go:27,102,107`).

### Strengths
- resolve order, no `event_bus.Defaults()`, injected lookup seam, four unit cases.
- Test in `package main` so unexported `resolveGameID` is real — closes D-06 tautology risk.
- Depends on 06-01 so recovery asserts composite-PK `writeAuditRow`.

### Concerns
- **LOW:** All-empty → legacy `"main"` subject must stay documented so operators see mismatch remains only when neither config nor DB has game_id.
- **LOW:** Ensure `loadEventBusConfig` is retained for NATS URL/creds only; do not feed its `GameID` into DLQ prefix.

### Risk: **LOW**

---

## Cross-plan / phase goals

| Success criterion | Met by plans? |
|-------------------|---------------|
| #1 events_audit bounded (F4) | Yes, **if 06-01+06-02 ship together** |
| #2 vulnerable nats fails gate (F8) | Yes (OSV + pin + ruleset Task 4) |
| #3 DLQ external recover + non-tautology (F3) | Yes (06-05) |
| #4 coverage doc/enforcement (F7) | Yes (docs); blocking optional via ruleset |

### New defects introduced by revision
1. **Task 3 acceptance over-rigid / incomplete blast list** (see finding 3) — can red `task test:int` or break versioned migration tests.
2. **Co-deploy is content truth but not tooling-enforced** — highest remaining vehicle risk for F4 cold-history outage.

### No scope creep
Phase stays OPS-02/03/04 + QUAL-01. No movement/PWA/arch god-object pull-in.

---

## Overall

**Verdict: READY** (with **one pre-execute patch** recommended)

**Overall risk: MEDIUM** — residual medium is OPS-02 ship/process + Task 3 seed/migration wrinkles, not architectural reopen of round-1/2 blockers.

**Minimum pre-execute tweaks (do not re-enter full plan loop if patched inline):**
1. 06-01 Task 3: exempt pre-000052 migration tests; explicit non-ULID `event_ms` seeds; add `bootstrap_orphan_test.go` to `files_modified`.
2. 06-01/02 ship note: executors/orchestrator **must open one PR containing both** (block merge of 06-01-only).

After those, plans are source-grounded and round-2 is effectively closed.


---

## Antigravity Review

# Cross-AI Plan Review Report — Phase 6 (ROUND 3)

**Phase**: Phase 6 — Operational Hardening & Assurance Gates  
**Status**: **APPROVED / READY FOR EXECUTION**  
**Review Type**: Verification & Quality Assurance (Round 3)

---

## Executive Summary

The revised implementation plans (`06-01-PLAN.md` through `06-04-PLAN.md`) have **fully and correctly addressed all 12 findings** surfaced during the Round-2 Cross-AI review. The technical design, execution sequencing, migration safety, and CI/ruleset governance are robust, source-grounded, and ready for execution.

---

## Verification of Round-2 Review Findings

| # | Round 2 Finding | Plan / Location | Verification & Status |
|---|------------------|-----------------|-----------------------|
| 1 | **ULID helper compile error** (`ulid.Time(parsedID)` mismatch) | `06-01-PLAN.md` Task 2 | **VERIFIED FIXED**: Defined shared helper `eventMsFromULID(id ulid.ULID) int64 = ulid.Time(id.Time()).UnixNano()`. `id.Time()` correctly returns `uint64` ms passed to `ulid.Time(...)`. |
| 2 | **Legacy PK & index name collisions** (`events_audit_pkey` / `events_audit_subject_*`) | `06-01-PLAN.md` Task 1 | **VERIFIED FIXED**: `000052.up.sql` renames existing PK/indexes to `_legacy` before creating the new partitioned table. Acceptance criteria asserts constraint and index ownership via Postgres catalog queries. |
| 3 | **Test/seed blast radius** (`event_ms NOT NULL` breaking direct `INSERT`s) | `06-01-PLAN.md` Task 3 | **VERIFIED FIXED**: Sweeps ~14-16 test/seed files, updating direct `INSERT`s to include `event_ms` and establishing covering partitions via `holomushtest/server.go` or inline SQL. |
| 4 | **Co-deployment requirement** (`06-01` + `06-02`) | `06-01-PLAN.md` & `06-02-PLAN.md` | **VERIFIED FIXED**: Codified in `must_haves` and key links as a mandatory single PR ship unit to prevent cold history reading breakage. |
| 5 | **Bootstrap orphan check gap** | `06-02-PLAN.md` Task 4 | **VERIFIED FIXED**: `runBootstrapOrphanCheck` in `cmd/holomush/bootstrap_orphan.go` extended to check both `events_audit` and `events_audit_unpartitioned` (guarded by `to_regclass`). |
| 6 | **Protect-main ruleset required checks enforcement** | `06-03-PLAN.md` Task 4 & `06-04-PLAN.md` Task 3 | **VERIFIED FIXED**: Consolidated ruleset checkpoint in `06-04 Task 3` and `06-03 Task 4` mandating ruleset `11923801` updates verified via `gh api`. |
| 7 | **Detach $\to$ Rename crash atomicity** | `06-02-PLAN.md` Task 1 | **VERIFIED FIXED**: `DetachExpiredPartitions` runs an initial reconciliation pass targeting detached canonical tables missing `_detached_<unix>` suffixes before executing new detaches. |
| 8 | **Fail-open exit code check in pre-bump test** | `06-03-PLAN.md` Task 2 | **VERIFIED FIXED**: `task lint:vuln` exit code captured into `rc=$?` immediately and asserted non-zero (`test "$rc" -ne 0`), preventing false passes from trailing `echo`. |
| 9 | **First-cycle boot scope** | `06-02-PLAN.md` Task 3 | **VERIFIED FIXED**: Synchronous boot gate runs `Backfill` + `EnsurePartitions` ONLY. Background `RetentionWorker` handles `Detach` and `Drop` on its periodic ticker. |
| 10 | **Start ordering & dual-copy window** | `06-02-PLAN.md` Task 3 | **VERIFIED FIXED**: Strict start sequence: construct manager $\to$ `Backfill` $\to$ `EnsurePartitions` $\to$ start projection $\to$ start periodic worker. |
| 11 | **Doc-drift ordering** | `06-04-PLAN.md` Tasks 2 & 3 | **VERIFIED FIXED**: Docs initially state current reality ("posted status, not required check"), and are updated to "required/blocking" ONLY IF the maintainer applies the ruleset update. |
| 12 | **Patterns doc refreshed** | `06-PATTERNS.md` | **VERIFIED FIXED**: Grounding patterns updated to match `event_ms` partitioning, no-DEFAULT partition, Go backfill, and ruleset-based coverage enforcement. |

---

## Plan Quality & Architectural Highlights

1. **Deterministic Idempotency Key**:
   - Partitioning `events_audit` on `event_ms` (derived from event ULID) guarantees that live projection and DLQ replay produce the exact same composite key `(id, event_ms)`, ensuring `ON CONFLICT (id, event_ms) DO NOTHING` dedups properly even when JetStream `timestamp` values differ.
2. **Reversible & Data-Preserving Migration**:
   - `000052.down.sql` preserves data written after the migration by copying records from `events_audit` back into the unpartitioned structure before dropping the partitioned parent, completely eliminating data loss risk on rollback.
3. **Multi-Layer Vuln Scanning**:
   - Combining `govulncheck` (reachability) and `osv-scanner` (GitHub Advisory DB integration) ensures reachable vulnerabilities and ecosystem-level CVEs (like nats-server `GHSA-q59r-vq66-pxc2`) are both reliably caught.

---

## Execution Guidance for Executors

- **Pre-Push Security Checkpoint**: Ensure `/holomush-dev:review-crypto` is executed over `projection.go` and `000052_events_audit_partition.*.sql` as required by `06-01-PLAN.md` Task 4 before committing/pushing the PR.
- **Combined PR Boundaries**: Confirm `06-01-PLAN.md` and `06-02-PLAN.md` are submitted within the **same Pull Request** so schema changes and backfill logic land atomically.

**Conclusion**: The plan set is robust, complete, and approved to advance to execution.


---

## Consensus Summary — Round 3

**Verdict: CONVERGING but NOT YET READY.** The two source-grounded reviewers agree round 3
surfaced real residual defects; they diverge on how to weight them. **Codex** (grounded,
highest weight): **NOT READY / HIGH** — four blocking defects plus fail-open verification
pipelines. **OpenCode/grok-4.5** (grounded): **READY / MEDIUM** but only "with one pre-execute
patch," and its own matrix marks F3 **PARTIAL** and F4 a process risk. **Antigravity**
(grounded-to-plan, low weight): APPROVED — but it marked F3/F6/F13 "VERIFIED FIXED" that both
grounded reviewers found *not* fully closed, so its blanket approval is not corroborated.

Weighting the grounded pair over Antigravity (the round-2 convention), the honest reading is:
the round-1 (architecture) and round-2 (core implementation) blockers stay resolved and no
round-1/2 item regressed — but the round-2 incorporation pass **introduced new
implementation/edge-safety defects** and left **three round-2 findings only partially closed**.
These are narrow and mechanical (not an architecture reopen), but several are correctness/safety
HIGHs. One more incorporation pass is warranted before execute.

### Round-3 finding closure (merged Codex + grok; Antigravity concurs "fixed" on all)

| # | Finding | Codex | grok | Net |
|---|---------|-------|------|-----|
| 1 | ULID compile fix | CLOSED | CLOSED | **CLOSED** |
| 2 | Legacy PK/index rename | CLOSED | CLOSED | **CLOSED** |
| 3 | Test/seed blast radius | NOT CLOSED | PARTIAL | **NOT CLOSED** |
| 4 | Co-deploy + Start order | CLOSED (design) | CLOSED / process-risk | **CLOSED design; ship not tooling-enforced** |
| 5 | Bootstrap orphan-check | CLOSED | CLOSED | **CLOSED** |
| 6 | Mandatory ruleset gate | PARTIAL | CLOSED | **PARTIAL (codecov still deferrable)** |
| 7 | Crash-atomic detach | CLOSED narrowly + NEW defect | CLOSED | **CLOSED for the named window; new FINALIZE gap** |
| 8 | Exit-code capture (pre-bump) | CLOSED | CLOSED | **CLOSED** |
| 9 | First-cycle scope | CLOSED | CLOSED | **CLOSED** |
| 10 | Start order / dual-copy | CLOSED | CLOSED | **CLOSED** |
| 11 | Doc-tighten required | CLOSED | CLOSED | **CLOSED** |
| 12 | PATTERNS refresh | CLOSED (events_audit only) | CLOSED | **PARTIAL — only the events_audit section refreshed** |
| 13 | core.game_id precision | PARTIAL | CLOSED | **PARTIAL (executable body correct; title + PATTERNS still stale/rejected order)** |

No round-1/round-2 RESOLVED item regressed (DEFAULT partition, timestamp-flip, ATTACH-as-DEFAULT,
resolver precedence all remain fixed). Both grounded reviewers confirmed the plans are executable
against current source *in substance*.

### New round-3 concerns (fold into the next `--reviews` pass — incorporation, not redesign)

**HIGH**
1. **F3 test/seed sweep is wrong in both directions [Codex + grok].** It OMITS
   `cmd/holomush/bootstrap_orphan_test.go:49,58` (applies all migrations, then inserts without
   `event_ms` → will break), and it INCLUDES `internal/store/migrate_plugins_integration_test.go:60`
   whose insert intentionally runs at schema version 17, *before* `event_ms` exists → adding it
   there breaks the historical migration test. Fix: exempt version-pinned pre-000052 migration
   tests from the mechanical sweep; add `bootstrap_orphan_test.go` (use a real ULID, not an
   arbitrary byte string); define an explicit non-ULID seed rule for `event_ms` (the
   `gen_random_bytes(16)` seeds at `holomushtest/server.go:777`, `harness_impl_test.go:194` are
   not ULIDs, so `eventMsFromULID` doesn't apply).
2. **Interrupted `DETACH … CONCURRENTLY` pending-finalize not recovered [Codex].** PG runs
   concurrent detach in two internal transactions; interruption leaves the partition marked
   `pg_inherits.inhdetachpending`, which must be cleared with `ALTER TABLE … DETACH PARTITION …
   FINALIZE`. 06-02's reconciliation only finds tables already absent from `pg_inherits`, and its
   injected-crash test skips only the rename — it never exercises the pending-finalize state. Fix:
   detect `inhdetachpending` and run FINALIZE before normal reconciliation, with an injected-
   interruption test.
3. **Negative retention config → detach-all / panic [Codex].** The config defaults only zero
   values and allows negatives. A negative `RetainWindow` makes the cutoff future-facing and can
   detach every partition (`retention.go:82`); a negative `PurgeInterval` reaches
   `time.NewTicker` and panics the process (`retention.go:127`). Fix: reject `RetainWindow <= 0`
   and `PurgeInterval <= 0` at config validation.
4. **Vuln required-check context is a job NAME, not the key [Codex].** GitHub rulesets require the
   rendered job `name`, not the workflow key. The current `lint` key renders as `Lint` (which is
   what ruleset 11923801 actually requires — `ci.yaml:36`). The plan says the required check is
   `vuln` (the key). Fix: set an exact stable job `name` on the new job and require *that* exact
   context; verify in a real PR's `statusCheckRollup`, not only ruleset JSON.
5. **F6 mandatory codecov gate still deferrable [Codex].** 06-04 Task 3 still says the two Codecov
   checks "MAY be deferred" and treats deferral as a successful checkpoint outcome (`06-04:190,219`),
   so the round-2 "make the gate mandatory" finding is only half-closed (vuln mandatory; codecov
   not). Fix: remove the coverage-deferral success branch, or explicitly re-scope F6 to vuln-only
   and document codecov as accepted-deferred.

**MEDIUM**
6. **Fail-open `| tail` verification pipelines [Codex].** Multiple `<test> | tail` verify commands
   in 06-01/06-02/06-05 mask the test's exit code (tail returns 0 after a failed test) — the same
   fail-open class as round-2's F8, in a new place, and a direct violation of the repo search-tools
   rule ("judge by exit code; pipes mask `$?` unless `pipefail`"). Fix: capture the test exit code
   or `set -o pipefail`.
7. **Stale non-events_audit PATTERNS sections [Codex].** F12 refreshed only the events_audit
   section. The OPS-03 section still shows the v1 scanner command (`06-PATTERNS:194`), the DLQ
   section still prescribes the *rejected* order override → DB → `cfg.GameID` (`06-PATTERNS:233`),
   and a stale "already ruleset-enforced hard gate" codecov comment remains (`06-PATTERNS:259`) —
   executors read PATTERNS via `read_first`, so these actively mis-steer. Fix: refresh all three.
8. **06-05 F13 stale steering [Codex].** Task 1's title still says "configured root game_id"
   (`06-05:87`) even though the executable body is correct — align the title to `core.game_id`.
9. **06-01 down-migration parent copy lacks `ON CONFLICT` [Codex]** → a resumed partial down hits
   duplicates; make the parent→temp copy idempotent too.
10. **06-04 must-have vs Task-3 contradiction [Codex]** — the top-level must-have hard-codes
    "Codecov not required" while Task 3 requires the docs to flip if checks are added; make the
    final must-have conditional on the verified final ruleset state.
11. **Co-deploy is content-truth but not tooling-enforced [grok].** F4's single-PR rule lives in
    `must_haves`; nothing blocks a 06-01-only merge → cold-history blackout. Add an explicit
    execute/ship gate, not only a truth.

**LOW**
12. **OSV empty-config scaffold ambiguous [Codex].** "empty `[[IgnoredVulns]]`" creates an array
    entry; use a truly empty config or `IgnoredVulns = []` and validate by running the scanner.

### Divergent views (worth the operator's attention)
- **READY vs NOT READY:** Codex (NOT READY/HIGH) vs grok (READY/MEDIUM-with-inline-patch). The
  gap is severity weighting of the same evidence plus three extra HIGHs only Codex surfaced
  (FINALIZE, negative-config, ruleset-name). Treat Codex's set as the authoritative fix list.
- **F6 (codecov mandatory):** Codex PARTIAL (still deferrable) vs grok CLOSED. Codex is correct on
  the plan text (`06-04:190`).
- **F13 (core.game_id):** Codex PARTIAL (stale title/PATTERNS) vs grok CLOSED. Both agree the
  executable body is correct; the disagreement is whether stale steering text counts.
- **Antigravity's blanket APPROVE** on F3/F6/F13 is not corroborated by either grounded reviewer.

### Recommendation
Do ONE more incorporation pass — `/gsd-plan-phase 6 --reviews` — to fold in the round-3 findings
above (they are narrow, mechanical corrections and edge-safety guards, NOT an architecture
reopen: fix the F3 sweep list, add `DETACH … FINALIZE` recovery + negative-config validation,
pin the exact vuln job `name` for the ruleset, close or re-scope the F6 codecov deferral, replace
`| tail` with exit-code/pipefail, and finish the PATTERNS + 06-05-title refresh). Then re-confirm
with `/gsd-review --phase 6 --codex`. The loop is still converging — round 1 was architecture,
round 2 was core implementation, round 3 is implementation-detail and edge-safety.
