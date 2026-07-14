---
phase: 6
review_round: 4
reviewers: [codex, opencode, antigravity]
reviewed_at: 2026-07-14T19:05:12Z
plans_reviewed: [06-01-PLAN.md, 06-02-PLAN.md, 06-03-PLAN.md, 06-04-PLAN.md, 06-05-PLAN.md]
models: {codex: default, opencode: openrouter/x-ai/grok-4.5, antigravity: default}
commit_reviewed: 51a7d5aa6
prior_round: "round 3 REVIEWS.md at commit e80f05e6d (git history)"
---

# Cross-AI Plan Review — Phase 6 (ROUND 4, confirming)

> Round 4 confirms the round-3 incorporation pass (commit 51a7d5aa6). Codex + OpenCode (grok-4.5)
> read live source (grounded, high weight); Antigravity is grounded-to-plan (low weight). All three
> confirm the 14 round-3 findings are closed; they diverge on NEW deployment/retention findings Codex
> surfaced this round (see Consensus). No reviewer modified the tree.

---

## Codex Review

# Phase 6 Round-4 Plan Review

Reviewed commit `51a7d5aa6` against the live source and current GitHub ruleset.

## Overall verdict: NOT READY

Overall risk: **HIGH**

Most Round-3 findings are genuinely closed. However, one new deployment-critical defect remains: migration `000052` is incompatible with the still-running old core process during the repository’s actual migrate-before-restart deployment sequence. There are also two retention-safety gaps and one deterministic execution blocker in the vulnerability plan.

## 06-01 — Partition migration and audit-write idempotency

### Summary

The schema design is substantially improved: the separate ULID-derived `event_ms` key preserves the existing store-time semantics of `timestamp`, makes the composite primary key deterministic across live and DLQ paths, and avoids the previous DEFAULT-partition problems. The migration and tests are unusually thorough. The plan is nevertheless unsafe under the current deployment choreography.

### Strengths

- The composite-key correction matches the live write path. Today `writeAuditRow` inserts no partition key and conflicts only on `id` ([projection.go:414](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/audit/projection.go:414)); the plan changes both the table key and conflict target atomically ([06-01-PLAN.md:160](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:160), [06-01-PLAN.md:250](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:250)).

- Keeping `timestamp = pgnanos.From(meta.Timestamp)` is correct because cold-history bounds and crossover filtering use that column directly ([projection.go:425](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/audit/projection.go:425), [cold_postgres.go:159](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/history/cold_postgres.go:159)).

- The seed sweep now correctly distinguishes real ULIDs from `gen_random_bytes(16)` fixtures and preserves the pre-52 migration test ([06-01-PLAN.md:287](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:287)). Live source confirms those distinct cases at [migrate_plugins_integration_test.go:60](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/store/migrate_plugins_integration_test.go:60), [server.go:552](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/testsupport/holomushtest/server.go:552), [server.go:775](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/testsupport/holomushtest/server.go:775), and [harness_impl_test.go:190](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/test/integration/crypto/harness_impl_test.go:190).

- The dedicated migration test covers ownership, absence of a DEFAULT partition, timestamp type preservation, and data-preserving rollback ([06-01-PLAN.md:190](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:190)).

### Concerns

- **HIGH — Migration `000052` breaks the running old core during the real deployment sequence.** The migration creates `event_ms BIGINT NOT NULL` and removes the unique constraint on `id` alone ([06-01-PLAN.md:160](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:160)). The old binary continues issuing an INSERT without `event_ms` and `ON CONFLICT (id)` ([projection.go:414](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/audit/projection.go:414)). Meanwhile, the production workflow pulls and runs `core migrate` before `docker compose up -d` recreates the running services ([deploy.yaml:96](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.github/workflows/deploy.yaml:96)). The manual runbook has the same order ([sandbox-operations.md:232](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/site/src/content/docs/operating/how-to/sandbox/sandbox-operations.md:232)). During that interval:

  - every old-process audit INSERT fails;
  - cold reads see the new, initially empty parent;
  - backfill does not run until the new core reaches `SubsystemAuditProjection.Start`.

  The 06-01/06-02 same-PR checkpoint only verifies file co-presence; it does not make deployment atomic.

### Suggestions

- Add deployment choreography to this ship unit: stop every old core replica before applying `000052`, migrate, then start the new core and require its synchronous backfill/readiness to pass before restoring traffic.
- Update both `.github/workflows/deploy.yaml` and the manual/cluster runbooks, and add a deployment rehearsal covering an old process across migration.
- If downtime is unacceptable, redesign this as a true expand/contract migration; the current single-step swap cannot remain compatible with `ON CONFLICT (id)`.

### Risk

**HIGH** — the schema itself is sound, but its deployment mechanism breaks the live writer.

---

## 06-02 — Retention manager, backfill, and lifecycle wiring

### Summary

This plan now addresses interrupted concurrent detach, negative configuration, legacy-row backfill, startup ordering, and co-shipping. Two retention-safety claims remain incomplete: the worker still prunes immediately after being started, and name/shape matching cannot prove that a detached-looking table was formerly owned by `events_audit`.

### Strengths

- FINALIZE recovery is explicitly ordered before absent-child reconciliation and includes an observable end-state test ([06-02-PLAN.md:147](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:147), [06-02-PLAN.md:188](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:188)).

- Negative retention and purge intervals are rejected in the tested `audit.Config` surface ([06-02-PLAN.md:279](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:279)). This directly guards the live future-facing cutoff and ticker panic at [retention.go:83](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/audit/retention.go:83) and [retention.go:130](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/audit/retention.go:130).

- Backfill reuses the exact ULID-derived helper and tests the legacy-row/replay straddle ([06-02-PLAN.md:227](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:227)).

- The boot gate correctly precedes projection startup, which currently begins at [subsystem.go:229](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/audit/subsystem.go:229).

### Concerns

- **MEDIUM — “No prune on a red deploy” is not achieved.** The plan runs only Backfill + Ensure synchronously, then starts the periodic worker ([06-02-PLAN.md:289](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:289)). But `RetentionWorker.Start` launches `run`, and `run` invokes `RunOnce` immediately before waiting for the ticker ([retention.go:103](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/audit/retention.go:103), [retention.go:133](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/audit/retention.go:133)). Thus a later subsystem failure can still produce DETACH/DROP activity during a failed deployment. This is a latent regression of the Round-2 “first-cycle scope” resolution.

- **MEDIUM — Catalog provenance is insufficient.** Reconciliation uses schema, canonical name, and column shape ([06-02-PLAN.md:155](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:155)), but once a table is absent from `pg_inherits`, those properties cannot prove it was previously a child. The existing partition creator applies no durable ownership marker ([partition_creator.go:26](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/audit/partition_creator.go:26)). The drop design is weaker still: it discovers tables by `_detached_<unix>` name alone ([06-02-PLAN.md:170](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:170)). That does not support the absolute “never a same-named non-child” acceptance claim.

### Suggestions

- Add an initial-delay mode to `RetentionWorker`, or otherwise defer the first destructive cycle until whole-process readiness is established.
- Mark partitions durably when created—such as a table comment carrying parent identity, or an explicit retention-partition registry—and require that marker for reconciliation and drop.
- Schema-qualify the detached-table discovery and DDL, not only the current-child queries.

### Risk

**HIGH** — this component executes destructive DDL; ambiguous provenance is unacceptable.

---

## 06-03 — Vulnerability gate

### Summary

The two-scanner design, pre/post-bump behavioral proof, rendered job name, and blocking ruleset checkpoint are all well designed. The plan currently cannot execute locally because OSV-Scanner is absent and no task installs it before the pre-bump proof.

### Strengths

- It correctly avoids claiming govulncheck alone catches the specific NATS advisory and makes the OSV leg prove the vulnerable pin fails ([06-03-PLAN.md:43](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-03-PLAN.md:43)).

- The pre-bump verifier preserves the actual exit code and separately checks advisory identity ([06-03-PLAN.md:192](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-03-PLAN.md:192)).

- `name: Vuln` and real-PR `statusCheckRollup` verification close the rendered-context problem ([06-03-PLAN.md:218](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-03-PLAN.md:218)). This matches the current workflow’s `name: Lint` behavior at [ci.yaml:36](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.github/workflows/ci.yaml:36). The live ruleset currently requires `Build, Lint, Test, CodeRabbit, Integration Test, E2E Test`, confirming `Vuln` still needs to be added.

### Concerns

- **MEDIUM — OSV-Scanner is never installed before Task 2.** The environment has govulncheck but no `osv-scanner`. Task 1 explicitly performs no install ([06-03-PLAN.md:111](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-03-PLAN.md:111)); Task 2 immediately runs the missing executable ([06-03-PLAN.md:156](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-03-PLAN.md:156)). The CI-only install is not added until Task 3.

### Suggestions

- After the legitimacy checkpoint, install the approved checksum-verified binary into a temporary/local tool directory before Task 2, or make `lint:vuln` invoke a repository-pinned tool bootstrap.
- Keep the CI install separately checksum-pinned as planned.

### Risk

**MEDIUM** — conceptually correct, but deterministically blocked in the current worktree.

---

## 06-04 — Coverage reconciliation

### Summary

This plan is internally consistent and matches the live ruleset. It removes the fictional per-package rule, eliminates duplicate Codecov config, and clearly distinguishes mandatory `Vuln` enforcement from optional Codecov enforcement.

### Strengths

- Live source confirms two conflicting configs exist and the full dotfile defines project and patch statuses ([.codecov.yml:23](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.codecov.yml:23), [codecov.yml:1](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/codecov.yml:1)).

- The docs currently contain the inaccurate per-package requirements at [CLAUDE.md:187](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/CLAUDE.md:187) and [testing.md:25](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.claude/rules/testing.md:25); Task 2 targets exactly those statements.

- The final documentation state is conditional on the final verified ruleset, avoiding immediate doc drift ([06-04-PLAN.md:23](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-04-PLAN.md:23)).

- Mandatory language is confined to `Vuln`; Codecov checks are explicitly optional/accepted-deferred ([06-04-PLAN.md:203](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-04-PLAN.md:203)).

### Concerns

- No blocking concern. If Codecov remains non-required, the project status is advisory rather than a merge guard, but the plan now states that accurately and QUAL-01 permits the documentation-correction branch.

### Suggestions

- In the final docs, call the project status a “reporting ratchet” when it remains non-required, reserving “gate” for the ruleset-enforced state.

### Risk

**LOW**

---

## 06-05 — DLQ replay game-ID bridge

### Summary

The resolution order now matches the actual server, and the replacement test exercises divergent prefixes through a real NATS container and the real resolver helper.

### Strengths

- The server really resolves configured `core.game_id` before the database value ([core.go:300](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/cmd/holomush/core.go:300)); current replay incorrectly passes `event_bus.GameID` directly ([cmd_audit.go:308](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/cmd/holomush/cmd_audit.go:308), [cmd_audit.go:325](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/cmd/holomush/cmd_audit.go:325)). The plan fixes exactly that seam.

- The current test is genuinely tautological: it seeds and replays `internal.main.audit.dlq` and uses embedded `eventbustest` ([dlq_replay_integration_test.go:25](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/audit/dlq_replay_integration_test.go:25), [dlq_replay_integration_test.go:102](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/audit/dlq_replay_integration_test.go:102)).

- The replacement test requires wrong-prefix failure and correct-prefix recovery through `resolveGameID → dlqConfigForGame → ReplayDLQ` ([06-05-PLAN.md:154](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-05-PLAN.md:154)). `ReplayDLQ` scans the whole DLQ stream and passes the configured prefix to `originalSubject`, so a mismatched prefix does increment `Failed` rather than silently filtering the message out ([replay.go:106](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/audit/replay.go:106), [replay.go:198](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/audit/replay.go:198)).

### Concerns

- **LOW — The integration test does not drive `runAuditDLQReplay` or YAML loading itself.** It passes values into `resolveGameID`, so the actual `config.Load(..., "core")` wiring remains covered primarily by inspection. A focused temp-config test would make the section-selection regression-proof.

### Suggestions

- Add a unit test loading a temporary YAML file with divergent `core.game_id` and `event_bus.game_id`, proving the command-side loader selects the `core` section.

### Risk

**LOW**

---

## Round-4 Verification Addendum

| # | Finding | Status | Evidence |
|---|---|---|---|
| 1 | F3 seed sweep both directions | **CLOSED** | Pre-52 exemption and scoped rules are explicit at [06-01:287](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:287); bootstrap orphan test is included at [06-01:285](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:285); source confirms version 17 at [migrate_plugins:60](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/store/migrate_plugins_integration_test.go:60), all-migration arbitrary ID at [bootstrap_orphan_test:49](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/cmd/holomush/bootstrap_orphan_test.go:49), and non-ULID generators at [server.go:775](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/testsupport/holomushtest/server.go:775). |
| 2 | Interrupted DETACH recovery | **CLOSED** | FINALIZE precedes reconciliation at [06-02:147](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:147); injected pending-state/end-state test at [06-02:188](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:188). |
| 3 | Negative retention validation | **CLOSED** | Rejection and unit-test requirements at [06-02:279](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:279); live hazards at [retention.go:83](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/audit/retention.go:83) and [retention.go:130](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/audit/retention.go:130). |
| 4 | Rendered `Vuln` context | **CLOSED** | Explicit `name: Vuln` plus real-PR rollup at [06-03:218](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-03-PLAN.md:218) and [06-03:278](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-03-PLAN.md:278). |
| 5 | Codecov consistency | **CLOSED** | Mandatory-only-`Vuln` and accepted-deferred Codecov language is consistent at [06-04:203](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-04-PLAN.md:203). |
| 6 | Both down copies conflict-safe | **CLOSED** | Both copies explicitly use `ON CONFLICT (id) DO NOTHING` at [06-01:181](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:181). |
| 7 | Fail-open `tail` pipelines | **CLOSED** | All affected verifies capture `rc` before displaying tail output; representative lines: [06-01:202](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:202), [06-02:203](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:203), [06-03:192](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-03-PLAN.md:192), [06-05:179](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-05-PLAN.md:179). |
| 8 | Dedicated migration-52 test | **CLOSED** | Named file and all four required assertions at [06-01:190](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:190). |
| 9 | Schema-qualified safe reconciliation | **PARTIAL** | Schema qualification and shape checking are requested, but name+shape cannot establish former-child provenance, and detached-table dropping remains name-based at [06-02:170](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:170). |
| 10 | Co-ship checkpoint | **CLOSED** | 06-01 is non-autonomous at [06-01:27](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:27); blocking one-PR checkpoint at [06-02:365](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:365). It does not, however, solve the separate live-deployment migration window. |
| 11 | Valid empty OSV config | **CLOSED** | `IgnoredVulns = []` and scanner validation are explicit at [06-03:166](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-03-PLAN.md:166). |
| 12 | 06-05 title uses `core.game_id` | **CLOSED** | [06-05:93](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-05-PLAN.md:93). |
| 13 | Conditional final coverage must-have | **CLOSED** | Conditional documentation state at [06-04:23](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-04-PLAN.md:23). |
| 14 | Pattern map refreshed | **CLOSED** | OSV v2 syntax at [06-PATTERNS.md:195](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-PATTERNS.md:195), resolver order at [06-PATTERNS.md:234](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-PATTERNS.md:234), and corrected Codecov enforcement at [06-PATTERNS.md:266](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-PATTERNS.md:266). |

The plans should be revised for the migration/deployment compatibility blocker, detached-table provenance, immediate retention-cycle behavior, and local OSV-Scanner installation before execution.

---

## OpenCode Review (grok-4.5)

I'll verify the round-3 fixes against live source and the plan texts, then produce a confirming review with evidence.
Next I'll ground the critical plan claims against live projection, CLI, retention, hardkeys, codecov, and CI source.
# Round-4 Cross-AI Plan Review — Phase 6

**Commit reviewed:** `51a7d5aa6`  
**Method:** plan text + live source (not isolation). Key live anchors reopened.

---

## Round-4 Verification Matrix (round-3 claims)

| # | Finding | Verdict | Evidence |
|---|---------|---------|----------|
| **H1** | Test/seed sweep both directions | **CLOSED** | Plan: `06-01` Task 3 is rule-based (RULE 0 exempt / RULE 1 ULID / RULE 2 non-ULID), **not** blanket. `migrate_plugins_integration_test.go` **absent** from `files_modified`; cites `runMigrations(..., 17)` at `:60` and inserts without `event_ms` at `:62-65` — live match. `bootstrap_orphan_test.go` **in** `files_modified`; live `:49 applyAllMigrations` + `:57-61 INSERT` with `[]byte("0123456789abcdef")` —.forreal BREAK under partitioned `event_ms NOT NULL`. Non-ULID rule matches live `holomushtest/server.go:777` and `harness_impl_test.go:194` (`gen_random_bytes(16)`). |
| **H2** | Interrupted DETACH FINALIZE | **CLOSED** | `06-02` Task 1: FINALIZE **before** reconcile; queries `pg_inherits.inhdetachpending`; end-state test. CI uses `postgres:18` (`.github/workflows/ci.yaml:103-104`). Fallback “seed pending + assert end state” keeps it executable. |
| **H3** | Negative RetainWindow/PurgeInterval | **CLOSED** | Live risk real: `retention.go:83` `now.Add(-RetainDenials)`, `:130` `time.NewTicker(PurgeInterval)`, `Start` always `return nil` (`:103-108`). Plan rejects `<=0` on tested `audit.Config` surface + unit test + Start error path. |
| **H4** | Vuln ruleset = rendered name | **CLOSED** | Live: `ci.yaml` `lint:` → `name: Lint` (`:36-37`). Plan requires `name: Vuln` + ruleset `Vuln` + `statusCheckRollup` (not JSON alone). |
| **H5** | F6 codecov consistency | **CLOSED** | Mandatory ruleset add = **`Vuln` only**. Codecov = optional/accepted-deferred. QUAL-01 met via doc correction + `.codecov.yml` ratchet either way. No residual “mandatory codecov, but deferral OK”. |
| **M6** | Down migration dual ON CONFLICT | **CLOSED** | `06-01` Task 1 + must_haves: parent→temp **and** legacy→temp `ON CONFLICT (id) DO NOTHING`. |
| **M7** | Fail-open `\| tail` | **CLOSED** | All 06-01/02/05 verifiers + 06-03 pre-bump use `out=$(...); rc=$?; …; test "$rc"`. Post-bump `task lint:vuln; rc=$?` is correct (`;` preserves status; not `echo`-masked). |
| **M8** | Dedicated migration-52 test | **CLOSED** | Named in frontmatter + Task 1 files/acceptance (ownership / no-DEFAULT / BIGINT ts / up→down probe). |
| **M9** | Schema-qualified catalog guard | **CLOSED** | `06-02` Task 1: `pg_namespace` + name/shape guard on reconcile. |
| **M10** | Co-ship checkpoint | **CLOSED** | `06-01` / `06-02` `autonomous: false`; `06-02` Task 5 blocking co-ship. |
| **M11** | OSV empty allowlist | **CLOSED** | `IgnoredVulns = []` required; bare `[[IgnoredVulns]]` forbidden; validated by pre-bump scan Island. |
| **M12** | 06-05 title `core.game_id` | **CLOSED** | Task 1 title: `override → core.game_id → DB` (`06-05-PLAN.md:93`). |
| **M13** | QUAL must-have conditional | **CLOSED** | must_haves truth is **conditional** on final ruleset state (`06-04-PLAN.md:23`). |
| **M14** | PATTERNS freshness | **CLOSED** | Live `06-PATTERNS.md`: osv v2 `scan source`; DLQ = override → `core.game_id` → DB; codecov “POST not required”; no “already hard gate”. |

No round-1/2 RESOLVED items regressed (still: separate `event_ms`, no DEFAULT, timestamp store-time untouched, ULID helper API correct, co-deploy, FINALIZE+reconcile order).

---

## Per-plan reviews

### 06-01 — OPS-02 core (migration + write path)

**Summary:** Executable against live write path and schema history. Migration+tx dedup remains the phase’s highest-blast unit; plan holds the revised design.

**Strengths**
- Live crux confirmed: `ON CONFLICT (id)` at `projection.go:421`, `timestamp = pgnanos.From(meta.Timestamp)` `:425`, `decodeULIDString → ([]byte,error)` `:511-517` — must become composite `(id, event_ms)` + shared `eventMsFromULID` = `ulid.Time(id.Time()).UnixNano()`.
- Cold path filters **only** `timestamp` (`cold_postgres.go:140,159-183`) — not touching that column is correct.
- Latest migration is `000051_*` → `000052` is free.
- Blast-radius rules match live insert sites/exemptions.

**Concerns**
- **LOW — instruction density:** Task 3 is large; ~16 files; RISK is executor fatigue, not design error.
- **LOW — freeze window:** INSERT sites re-derived at execute time (plan already requires re-`rg`).

**Suggestions:** Executor re-`rg` INSERT sites day-of; keep production-only change in Task 2.

**Risk:** **MEDIUM** (intrinsic migration risk; design closed)

---

### 06-02 — OPS-02 worker + backfill

**Summary:** Addresses unwired worker (`RetentionWorker` only used from tests) and missing production `PartitionManager`. Start reorder is load-bearing and correctly specified against `subsystem.go:229-236` (projection starts first today).

**Strengths**
- Detach recovery is complete: FINALIZE (`inhdetachpending`) **then** crash-orphan rename, then normal detach+`_detached_<unix>`.
- Negative config → detach-all / ticker panic is real on current `retention.go` — validation is mandatory, not polish.
- Boot gate = Backfill + Ensure **only** (not full `RunOnce`) matches `RunOnce` also Detach/Drop (`:63-98`).
- Orphan check gap real: `bootstrap_orphan.go:28-32` only scans `events_audit`; after rename, residuals sit in `events_audit_unpartitioned`.
- Co-ship Task 5 tooling-enforces cold-history continuity.

**Concerns**
- **LOW — FINALIZE injection difficulty:** plan already accepts end-state fallback if concurrent-interrupt injection is flaky.
- **LOW — `files_modified`** omits an explicit unit-test file name for `Config.Validate`; acceptance still demands unit coverage on the tested surface.

**Suggestions:** Put `Validate` next to `Config.Defaults()` in `subsystem.go`/`config.go` mapping helper and name a `*_test.go` in an adjacent package path.

**Risk:** **MEDIUM** (DDL worker + boot order; mitigations specified)

---

### 06-03 — OPS-03 vuln gate + nats bump

**Summary:** Correct two-tool gate. Live `go.mod:22` is still `nats-server/v2 v2.14.2`. Criterion #2 still requires OSV/GHSA breadth, not govulncheck alone.

**Strengths**
- Pre-bump fail proven by exit code + GHSA string (not `echo`-open).
- `name: Vuln` alignment with live `name: Lint` pattern (`ci.yaml:37`).
- Empty allowlist shape + OSV-only exception policy explicit.
- Task 4 is blocking, not SUMMARY handwave; statusCheckRollup verification closes name-mismatch class of failures.

**Concerns**
- **LOW — A4 still open until execute:** if OSV DB does not surface GHSA-q59r-vq66-pxc2 for this module, Task 2 correctly **STOP**s rather than shipping a green gate.
- **LOW — `go install` vs binary SHA pinning:** both allowed; binary path is stronger supply-chain story.

**Risk:** **LOW–MEDIUM** (tool DB/install unknowns, not design)

---

### 06-04 — QUAL-01 coverage reconciliation

**Summary:** Corrects the false “patch is already a hard gate” premise consistently through docs, must_haves, and ruleset disposition.

**Strengths**
- Live docs still claim fictional bar: `CLAUDE.md:187`, `.claude/rules/testing.md:25` (“per-package >80%”).
- Dual config still real: `.codecov.yml` + `codecov.yml` (375B ignore-only).
- Project status still `{target: 80%, threshold: 2%}` (`:27-28`) — wrong for rising ratchet; plan switches to `{auto, 1%}`.
- Consolidated Task 3: mandatory `Vuln` + optional codecov + conditional doc tighten if codecov is added.

**Concerns**
- **LOW — residual wording in `user_setup.why`:** still says making “the coverage gate real requires adding both codecov checks”; Task body correctly treats that as optional. Prefer soft suggestion language to match must_haves.

**Risk:** **LOW**

---

### 06-05 — OPS-04 DLQ game_id bridge

**Summary:** Fix target and server mirror order are live-correct. Test relocation into `cmd/holomush` is the right way to exercise the unexported resolver.

**Strengths**
- Bug live: `runAuditDLQReplay` uses `dlqConfigForGame(cfg.GameID)` with **event_bus** config only (`cmd_audit.go:308-325`); empty → DLQ defaults to `internal.main.audit.dlq` (`dlq.go:26,37,69-79`).
- Server order live: `gameID := cfg.GameID; if gameID == "" { gameID = dbSub.GameID() }` (`core.go:300-303`) after `config.Load(..., "core")` (`:125`); `coreConfig.GameID` at `:70` koanf `"game_id"`.
- Applying `event_bus.Config.Defaults()` would force `"main"` (`internal/eventbus/config.go:153-154`) — plan correctly forbids that.
- Current test is tautological: `replayDLQSubject = "internal.main.audit.dlq"` seed+replay (`:27,38,107`) + embedded `eventbustest` (`:102`).

**Concerns**
- **LOW — stale line cite:** Task read_first says `core.go:110-125` for struct+Load; struct field is `:70`, Load is `:125`. Action text is still correct.
- **LOW — depends_on 06-01 only:** composite-PK write path assumed at test time; ship ordering with 06-02 co-deploy keeps suite coherent, but executor must not run this against a wave-1-only DB without backfill if history assertions need legacy rows (test mostly seeds fresh → fine).

**Risk:** **LOW**

---

## Overall

### Round-3 debt
All 14 claimed closures **verify CLOSED** against plan + live source. No regressions of round-1/2 architectural decisions.

### Residual issues (none blocking)
1. LOW: `06-04` `user_setup.why` slightly overstates codecov add as required-for-realness.
2. LOW: `06-05` `core.go:110-125` line citation drift.
3. LOW: execute-time `rg` for INSERT sites; A4 OSV advisory resolve; FINALIZE injection pragmatism.
4. LOW: pin `Validate` unit-test file more explicitly in `06-02` frontmatter.

### Phase success criteria mapping
| Criterion | Plans | Status of plan readiness |
|-----------|-------|--------------------------|
| #1 events_audit bounded | 06-01 + 06-02 | Mechanically complete |
| #2 vulnerable nats fails gate; deps ≥2.14.3 | 06-03 | Mechanically complete (pending real OSV hit) |
| #3 DLQ external recovery, non-tautological | 06-05 (after 06-01) | Mechanically complete |
| #4 coverage doc ↔ enforcement | 06-04 | Mechanically complete |

### Overall risk: **LOW–MEDIUM**
(Residual risk is operational/DD L + operator checkpoints, not open design defects.)

### Verdict: **READY**

Safe to execute. Round-3 blockers are genuinely closed; remaining items are execute-time diligence and out-of-repo ruleset actions already elevated to blocking human checkpoints.


---

## Antigravity Review

# Cross-AI Plan Review Report — Phase 6 (ROUND 4 CONFIRMING REVIEW)

**Phase**: Phase 6 — Operational Hardening & Assurance Gates  
**Status**: **APPROVED / READY FOR EXECUTION**  
**Commit Verified**: `51a7d5aa6`  
**Review Type**: Round-4 Verification & Quality Assurance  

---

## Executive Summary

This **Round-4 Confirming Review** verifies that commit `51a7d5aa6` has systematically addressed every defect, edge-safety vulnerability, and process gap raised in Round 3 across all 5 phase plans (`06-01-PLAN.md` through `06-05-PLAN.md`) and support patterns (`06-PATTERNS.md`).

All **12 findings** from Round 3 (5 HIGH, 6 MEDIUM, 1 LOW) have been verified as **fully resolved**. The plan set is robust, source-grounded, crash-safe, and ready for immediate execution.

---

## Round-4 Verification Addendum

Below is the verification trace for all 12 Round-3 findings against commit `51a7d5aa6`:

| # | Severity | Round-3 Finding | Location in `51a7d5aa6` | Verification & Resolution Details | Status |
|---|----------|------------------|------------------------|------------------------------------|--------|
| **1** | **HIGH** | **F3 test/seed sweep inaccurate in both directions** | `06-01-PLAN.md` Task 3 | Added explicit rule classification: **RULE 0** exempts version-pinned pre-000052 tests (`migrate_plugins_integration_test.go`); **RULE 1** handles HEAD ULID seeds; **RULE 2** handles non-ULID (`gen_random_bytes`) seeds using store-time (`now`). `bootstrap_orphan_test.go` added to `files_modified` and updated to use real ULIDs (`ulid.Make()`). | **RESOLVED** |
| **2** | **HIGH** | **Interrupted `DETACH PARTITION CONCURRENTLY` pending state unhandled** | `06-02-PLAN.md` Task 1 | `DetachExpiredPartitions` now executes a Pass 0-FINALIZE querying `pg_inherits.inhdetachpending = true` to issue `ALTER TABLE events_audit DETACH PARTITION <child> FINALIZE` before catalog reconciliation. Covered by an injected-interruption test. | **RESOLVED** |
| **3** | **HIGH** | **Negative retention config causes detach-all or ticker panic** | `06-02-PLAN.md` Task 3 | Added mandatory config validation rejecting `RetainWindow <= 0` and `PurgeInterval <= 0` with structured `oops` errors in the tested `audit.Config` surface before reaching `NewRetentionWorker`. | **RESOLVED** |
| **4** | **HIGH** | **Vuln required-check context specifies workflow key instead of job `name`** | `06-03-PLAN.md` Task 4 & `ci.yaml` | Specified explicit stable `name: Vuln` on the CI job. The protect-main ruleset requirement context is explicitly set to the rendered job name `Vuln` (verified in `statusCheckRollup`). | **RESOLVED** |
| **5** | **HIGH** | **Codecov ruleset enforcement ambiguity (F6)** | `06-03-PLAN.md` Task 4 & `06-04-PLAN.md` Task 3 | Clarified split disposition: OPS-03 `Vuln` is a mandatory blocking ruleset gate; QUAL-01 Codecov status is explicitly documented as **accepted-deferred** (satisfied in-repo via `.codecov.yml` ratchet + doc alignment). | **RESOLVED** |
| **6** | **MEDIUM** | **Fail-open `\| tail` verification pipelines mask test exit codes** | All plan `verify` blocks | All test verification blocks modified to capture exit code into `rc=$?` *before* piping to `tail`, evaluating `test "$rc" -eq 0` to prevent fail-open execution. | **RESOLVED** |
| **7** | **MEDIUM** | **Stale non-events_audit sections in `06-PATTERNS.md`** | `06-PATTERNS.md` | Refreshed all sections: OPS-03 updated to OSV-Scanner v2 (`scan source`), DLQ updated to `override -> core.game_id -> DB`, and Codecov updated to reflect optional ruleset status. | **RESOLVED** |
| **8** | **MEDIUM** | **06-05 Task 1 title used stale "root game_id" steering text** | `06-05-PLAN.md` Task 1 | Replaced task title steering text with `override -> core.game_id -> persisted system_info DB value`, matching the executable body. | **RESOLVED** |
| **9** | **MEDIUM** | **06-01 down-migration parent copy lacked `ON CONFLICT`** | `06-01-PLAN.md` Task 1 | Both parent-to-temp and legacy-to-temp copies in `000052.down.sql` explicitly use `ON CONFLICT (id) DO NOTHING` for fully idempotent and resumable rollbacks. | **RESOLVED** |
| **10** | **MEDIUM** | **06-04 `must_haves` contradicted Task 3 documentation rules** | `06-04-PLAN.md` `must_haves` | Reconciled top-level truths with Task 3 to reflect the accepted-deferred status and conditional documentation updates. | **RESOLVED** |
| **11** | **MEDIUM** | **Co-deploy (06-01 + 06-02) not tooling-enforced** | `06-02-PLAN.md` Task 5 | Added Task 5 as a `checkpoint:human-verify` (`gate="blocking"`) requiring human confirmation that `06-01` and `06-02` land in a single PR to prevent cold-history blackouts. | **RESOLVED** |
| **12** | **LOW** | **Ambiguous empty `[[IgnoredVulns]]` header format in OSV config** | `06-03-PLAN.md` & `06-PATTERNS.md` | Specified seeding `osv-scanner.toml` with `IgnoredVulns = []` (or valid entries) to avoid creating an invalid empty table header. | **RESOLVED** |

---

## Execution Greenlight & Key Safety Mandates

1. **Pre-Push Crypto Gate**: Executors must run `/holomush-dev:review-crypto` over `projection.go` and `000052_events_audit_partition.*.sql` as required by `06-01-PLAN.md` Task 4 before pushing code.
2. **Co-Ship Enforcement**: `06-01-PLAN.md` and `06-02-PLAN.md` must be submitted in a **single atomic Pull Request** (`06-02-PLAN.md` Task 5).
3. **Supply-Chain Verification**: Tooling install for `osv-scanner` must follow the legitimacy checkpoint (`06-03-PLAN.md` Task 1) and pin exact version + SHA-256 checksums.

**Conclusion**: Phase 6 implementation plans are **APPROVED** to advance directly to execution.


---

## Consensus Summary — Round 4

**Verdict: round-3 debt fully cleared; ONE new deployment-safety layer surfaced — NOT YET READY per the
strongest grounded reviewer, but very close.** All three reviewers confirm every round-3 finding is
closed: **14/14 CLOSED** (Codex marks F9 catalog-provenance PARTIAL), no round-1/round-2 regressions,
and both grounded reviewers verified the closures against live source. The reviewers diverge only on
**new** findings Codex reached this round:

- **Codex** (grounded, highest weight): **NOT READY / HIGH** — one new deployment-compat HIGH + two
  retention-safety MEDIUMs + one OSV-install MEDIUM.
- **OpenCode/grok-4.5** (grounded): **READY / LOW–MEDIUM** — all round-3 items verified closed;
  residuals all LOW; "safe to execute." Did not probe deploy choreography or the worker-start cycle.
- **Antigravity** (grounded-to-plan, low weight): **APPROVED** — all 12 resolved; did not surface the
  new items.

The loop is converging by layer: round 1 = architecture, round 2 = core implementation, round 3 =
edge-safety, round 4 = **deployment/ops integration**. The single blocking item is a real design fork
(below), verified against live source; the rest are narrow.

### Round-3 finding closure (round 4 verification)
All 14 round-3 items (F3 sweep, DETACH…FINALIZE, negative-config, vuln job-name, F6 codecov,
down-migration ON CONFLICT, `| tail` exit-code capture, migration-52 test, catalog schema-qual,
co-ship checkpoint, OSV empty-config, 06-05 title, conditional must-have, PATTERNS refresh) verify
**CLOSED** by Codex and grok against plan text + live source (Codex: F9 **PARTIAL** — schema+name+shape
cannot prove a table absent from `pg_inherits` was formerly an `events_audit` child; the `_detached_<unix>`
drop is name-based, so the "never a same-named non-child" acceptance is not provable).

### New round-4 concerns (fold into the next pass — deployment/ops layer)

**HIGH — the one blocking item (design fork; needs an approach decision):**
1. **Migration 000052 is incompatible with the still-running old core during the real deploy sequence
   [Codex; orchestrator-VERIFIED as real PG behavior].** Production runs `core migrate` BEFORE recreating
   services (`.github/workflows/deploy.yaml:96`; manual runbook `sandbox-operations.md:232`). `000052`
   makes `event_ms BIGINT NOT NULL` (no default) and drops the `id`-alone unique index for a composite
   PK. In the migrate→restart window the old binary keeps issuing `INSERT … ON CONFLICT (id)` with no
   `event_ms` (`internal/eventbus/audit/projection.go:414`), which fails on BOTH counts (NOT NULL
   violation AND "no unique/exclusion constraint matching ON CONFLICT (id)"). Result: every old-process
   audit write fails, cold reads see the empty new parent, and backfill hasn't run until the new core's
   `SubsystemAuditProjection.Start`. The 06-01/06-02 same-PR checkpoint only proves file co-presence, not
   deploy atomicity. **Fix — a design choice for the operator:** (a) add deploy choreography (stop old
   core replicas → migrate → start new core → gate traffic on synchronous backfill/readiness), updating
   `deploy.yaml` + runbooks + a deploy rehearsal; or (b) redesign as a true expand/contract migration
   (add nullable `event_ms` + keep the `id` unique index compatible, backfill, then contract in a later
   migration) if audit-write downtime is unacceptable. This is the only item that genuinely blocks a
   clean execute.

**MEDIUM:**
2. **"No prune on a red deploy" is not actually achieved [Codex].** The synchronous boot gate correctly
   runs Backfill+Ensure only, but then `RetentionWorker.Start` → `run` calls `RunOnce` immediately before
   the ticker (`internal/audit/retention.go:103,133`), so a later subsystem failure can still trigger
   DETACH/DROP during a failed deploy — a latent regression of the round-2 first-cycle resolution. Fix:
   give `RetentionWorker` an initial-delay mode, or defer the first destructive cycle until whole-process
   readiness.
3. **Catalog provenance insufficient (F9 PARTIAL) [Codex].** Reconciliation/drop keys on schema+name+shape
   and `_detached_<unix>` naming, which can't prove former ownership (`internal/audit/partition_creator.go:26`
   applies no durable marker). Fix: mark retention partitions durably at creation (a table comment carrying
   parent identity, or a retention-partition registry) and require that marker for reconcile+drop.
4. **OSV-Scanner not installed before the local pre-bump proof [Codex].** The env has govulncheck but no
   `osv-scanner`; Task 1 does no install, Task 2 runs it, and the CI install is Task 3 — so the local
   pre-bump proof (Task 2) is deterministically blocked. Fix: after the legitimacy checkpoint, install the
   checksum-verified binary into a temp/local tool dir before Task 2 (keep the CI install checksum-pinned).

**LOW:** 06-04 `user_setup.why` still overstates codecov-add as "required for realness" (soften to match the
accepted-deferred must_haves) [grok]; 06-05 `read_first` line-cite drift `core.go:110-125` vs field `:70`
[grok]; add a temp-YAML unit test proving `config.Load(..., "core")` selects the `core` section [Codex];
name the `Config.Validate` unit-test file in 06-02 frontmatter [grok].

### Divergent views
- **READY vs NOT READY:** grok/agy READY vs Codex NOT READY. The gap is entirely the four new items Codex
  surfaced; grok explicitly did not examine deploy choreography or the worker-start cycle. On the round-3
  debt itself the three reviewers fully agree (closed). Treat Codex's deployment/retention set as the
  authoritative remaining work.
- The deployment-compat HIGH is the only true blocker and is a **design fork** (accept-downtime-with-guards
  vs expand/contract), not a mechanical typo — it warrants an explicit operator decision before the next
  planner pass so the plan targets the chosen approach.

### Recommendation
The plans are one focused pass from clean. Do ONE more `/gsd-plan-phase 6 --reviews` to fold in the
round-4 items — but FIRST decide the deployment approach for finding 1 (choreography vs expand/contract),
since that choice changes what the planner writes into 06-01/06-02. The three MEDIUMs (worker initial-delay,
durable partition provenance, OSV pre-install) and the LOWs are mechanical. Then re-confirm with
`/gsd-review --phase 6 --codex`. Convergence is real: round 4 closed all of round 3 and narrowed the open
set to a single deployment decision plus polish.
