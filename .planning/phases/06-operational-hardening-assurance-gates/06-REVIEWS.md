---
phase: 6
review_round: 2
reviewers: [codex, opencode, antigravity]
reviewed_at: 2026-07-14T00:46:05Z
plans_reviewed: [06-01-PLAN.md, 06-02-PLAN.md, 06-03-PLAN.md, 06-04-PLAN.md, 06-05-PLAN.md]
models: {codex: default, opencode: openrouter/x-ai/grok-4.5, antigravity: default}
prior_round: "round 1 REVIEWS.md at commit 9e21023ca (git history)"
---

# Cross-AI Plan Review — Phase 6 (ROUND 2, post-revision)

> Round 2 verifies the /gsd-plan-phase 6 --reviews revision that addressed round 1.
> **Codex** and **OpenCode (grok-4.5)** read live source (grounded). **Antigravity**
> is low-weight (converging/APPROVE, largely restates). The orchestrator independently
> VERIFIED the two headline findings (see Consensus Summary).

---

## Codex Review

# Phase 6 round-2 review

## Overall verdict

**NOT READY — overall risk HIGH.**

The revision is materially better than round 1: the DEFAULT partition, timestamp semantic drift, tautological DLQ test, Codecov premise, and resolver precedence were all addressed. However, OPS-02 still has two implementation-blocking defects, and the vuln job may complete without becoming a required gate.

No `000052` migration or new implementation files exist yet, so this review verifies whether the plans are executable against current source—not completed code.

## Round-1 blocker status

| # | Blocker | Status | Finding |
|---|---|---|---|
| 1 | Partition strategy/idempotency | **PARTIAL** | No DEFAULT, plain `event_ms`, unchanged `timestamp`, and straddle coverage are specified correctly; two HIGH implementation defects remain. |
| 2 | Data-preserving down | **RESOLVED** | The plan copies parent and surviving legacy rows before dropping and requires a rollback probe. |
| 3 | Backfill/detach/startup | **PARTIAL** | Backward partition coverage, backfill, detach naming, name-age drop, and synchronous failure propagation are present; ULID conversion is invalid and detach→rename is crash-unsafe. |
| 4 | Vuln gate mechanics | **RESOLVED** | OSV-only allowlist, unsuppressible govulncheck, pinned v2 CLI, and pre/post behavioral evidence are explicit. Blocking enforcement remains an overall-plan concern. |
| 5 | Codecov premise | **RESOLVED** | The plan now says statuses post but do not block, cites ruleset 11923801, overrides D-07, and accurately labels 1% as an allowance. |
| 6 | DLQ resolver/test | **RESOLVED** | Precedence matches the server and the replacement acceptance test exercises the actual unexported resolver in `cmd/holomush` using `natstest`. |

## 06-01 — events_audit partition core

**Summary:** Strong redesign, but the proposed migration cannot currently be implemented as written.

**Strengths**

- No DEFAULT partition is allowed, and current plus two forward partitions are required ([06-01-PLAN.md:101](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:101)).
- `event_ms` is a normal `BIGINT NOT NULL`, not generated, with `(id,event_ms)` as PK ([06-01-PLAN.md:107](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:107)).
- `timestamp` remains sourced from JetStream metadata; this matters because cold reads filter `NotBefore`, `NotAfter`, and crossover edges on `timestamp` ([cold_postgres.go:159](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/history/cold_postgres.go:159)).
- The shared write seam is real: live projection calls `writeAuditRow` ([projection.go:329](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/audit/projection.go:329)) and replay calls the same function ([replay.go:219](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/audit/replay.go:219)).
- Down migration explicitly copies parent and legacy rows before dropping, with a survival test ([06-01-PLAN.md:123](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:123)).

**Concerns**

- **HIGH — invalid ULID API usage.** The plan repeatedly specifies `ulid.Time(parsedID).UnixNano()` ([06-01-PLAN.md:165](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:165)). In `oklog/ulid/v2`, `ulid.Time` accepts the ULID’s `uint64` millisecond timestamp, not a `ulid.ULID`. The repository’s established form is `ulid.Time(q.After.Time())`. Also, current `decodeULIDString` returns only `[]byte`, not the parsed ULID ([projection.go:509](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/audit/projection.go:509)). As written, this does not compile.
- **HIGH — renamed-table index/PK collision.** Migration 000009 created `events_audit_pkey` and named `events_audit_*` indexes ([000009_create_events_audit.up.sql:4](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/store/migrations/000009_create_events_audit.up.sql:4)). Renaming the table does not free those relation names. The plan then creates a new `events_audit` with an implicit PK and recreates indexes ([06-01-PLAN.md:103](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:103)). The PK creation can collide with `events_audit_pkey`; `CREATE INDEX IF NOT EXISTS` can silently see legacy indexes and leave the new parent unindexed.

**Suggestions**

- Use `parsedID, err := ulid.Parse(msgID)`, `idBytes := parsedID.Bytes()`, then `eventNS := ulid.Time(parsedID.Time()).UnixNano()`.
- Rename the legacy PK constraint/backing index and every existing `events_audit_*` index to explicit `_legacy` names before creating the new parent. Test index ownership on both tables, not merely index-name existence.

**Risk: HIGH**

## 06-02 — backfill and retention worker

**Summary:** The round-1 mechanism is now present, but it inherits the ULID defect and has a new crash-consistency gap.

**Strengths**

- `EnsurePartitions` explicitly covers the configurable retention horizon backward and the requested forward horizon ([06-02-PLAN.md:82](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:82)).
- Backfill is chunked, derives the same dedup key, creates historical partitions, and includes the required migration-straddle test ([06-02-PLAN.md:148](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:148)).
- Detach-renaming and drop-by-parsed-name-age are real mechanisms rather than stubs ([06-02-PLAN.md:98](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:98)).
- The plan correctly recognizes that `RetentionWorker.Start` always returns nil ([retention.go:103](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/audit/retention.go:103)) and requires synchronous preflight plus projection rollback ([06-02-PLAN.md:194](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:194)).

**Concerns**

- **HIGH — backfill repeats the invalid `ulid.Time(id)` expression** ([06-02-PLAN.md:151](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:151)).
- **MEDIUM — detach and rename are not crash-atomic.** `DETACH ... CONCURRENTLY` must run outside a transaction, then the plan separately renames the table ([06-02-PLAN.md:100](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:100)). A crash or rename failure between those statements leaves a detached `events_audit_YYYY_MM` table that is no longer a child and does not match the `_detached_<unix>` drop scanner. It becomes permanently stranded.
- **LOW — dynamic catalog-derived identifiers need explicit identifier quoting.**

**Suggestions**

- Correct the ULID conversion in one shared helper used by projection and backfill.
- Add reconciliation for canonical monthly tables that are detached but lack the suffix, or persist detach timestamps in a small metadata table before detaching.
- Test an injected rename failure followed by a second retention cycle.

**Risk: HIGH until the inherited compile blocker is fixed; otherwise MEDIUM.**

## 06-03 — vulnerability gate

**Summary:** Scanner composition is substantially corrected, but completion semantics still permit a reporting-only job.

**Strengths**

- OSV-Scanner is explicitly v2 and the command uses `scan source` ([06-03-PLAN.md:131](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-03-PLAN.md:131)).
- The OSV allowlist is explicitly scoped away from govulncheck; reachable govulncheck findings remain hard failures ([06-03-PLAN.md:141](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-03-PLAN.md:141)).
- Pre-bump failure on v2.14.2 and post-bump success on v2.14.3 are required, with a stop condition if the GHSA is not detected ([06-03-PLAN.md:152](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-03-PLAN.md:152)).
- Tool versions/checksums cannot float ([06-03-PLAN.md:185](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-03-PLAN.md:185)).

**Concerns**

- **HIGH — the plan may finish without a blocking gate.** OPS-03/D-04 requires blocking enforcement, but the ruleset update is merely recorded as an open action ([06-03-PLAN.md:193](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-03-PLAN.md:193)). A CI job that runs but is not required does not satisfy the blocking-gate requirement.
- **MEDIUM — pre-bump verification is fail-open as automation.** `task lint:vuln; echo "exit=$?"` always returns the `echo` exit status ([06-03-PLAN.md:158](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-03-PLAN.md:158)).
- **LOW — “empty `[[IgnoredVulns]]`” should mean a comments-only scaffold, not a literal empty array-table entry.** Validate the exact pinned v2 config before committing it.

**Suggestions**

- Make the `vuln` ruleset addition a blocking human-action checkpoint, not an open follow-up.
- Use a verification script that captures the status and asserts it is nonzero, then separately checks the advisory evidence.
- Context7 documentation verification was attempted but unavailable due DNS failure; the pinned-tool behavioral test is therefore especially important.

**Risk: HIGH**

## 06-04 — Codecov reconciliation

**Summary:** The round-1 factual premise is corrected accurately.

**Strengths**

- The plan explicitly overrides D-07 and states that Codecov statuses currently post but do not block.
- It cites ruleset 11923801 and the exact required checks ([06-04-PLAN.md:131](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-04-PLAN.md:131)).
- Current workflow uploads are indeed nonfatal ([ci.yaml:141](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.github/workflows/ci.yaml:141)).
- The 1% threshold is honestly labeled a one-point regression allowance, not “no-drop” ([06-04-PLAN.md:100](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-04-PLAN.md:100)).
- The duplicate configs are real: `.codecov.yml` has statuses ([.codecov.yml:23](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.codecov.yml:23)); `codecov.yml` is only a subset ignore file.

**Concern**

- **MEDIUM — ordering can recreate doc drift.** Task 2 writes “not required”; Task 3 may then add both checks, but says docs “may” be tightened ([06-04-PLAN.md:169](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-04-PLAN.md:169)). If the recommended ruleset action is completed without another doc edit, the finished documentation is immediately false.

**Suggestion**

- If checks are added, require—not merely permit—a final docs update and lint pass. If deferred, retain “posting, not blocking.”

**Risk: MEDIUM**

## 06-05 — DLQ game_id bridge

**Summary:** The redesigned resolver and test correctly target the live mismatch.

**Strengths**

- Server precedence is exactly `cfg.GameID`, then persisted `dbSub.GameID()` ([core.go:300](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/cmd/holomush/core.go:300)).
- The plan now mirrors override → configured core value → DB ([06-05-PLAN.md:93](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-05-PLAN.md:93)).
- Current CLI only loads `event_bus` ([cmd_audit.go:141](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/cmd/holomush/cmd_audit.go:141)) and incorrectly passes its `GameID` directly ([cmd_audit.go:325](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/cmd/holomush/cmd_audit.go:325)).
- The replacement test must live in package `main`, invoke the real resolver, use `natstest`, and assert mismatch plus recovery ([06-05-PLAN.md:139](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-05-PLAN.md:139)).
- The existing test is demonstrably tautological: it seeds and replays `internal.main.audit.dlq` using embedded `eventbustest` ([dlq_replay_integration_test.go:25](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/audit/dlq_replay_integration_test.go:25)).

**Concern**

- **LOW — “ROOT config” is imprecise.** The value is `core.game_id`: `coreConfig.GameID` is loaded using `config.Load(..., "core")` ([core.go:64](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/cmd/holomush/core.go:64)). The implementation instruction should name that exact section to prevent accidentally loading the YAML root.

**Suggestion**

- Replace every “ROOT game_id” instruction with “`core.game_id`, loaded via `config.Load(configFile, cmd, &cfg, "core")`.”

**Risk: LOW**

## Success-criteria assessment

- **OPS-02:** Not yet achievable as written because the ULID expression does not compile and the migration has PK/index-name collisions.
- **OPS-03:** Mechanically sound scanner design, but not complete unless `vuln` becomes a required ruleset check.
- **OPS-04:** Achievable after the narrow `core.game_id` wording clarification.
- **QUAL-01:** Achievable through the documentation-correction path; require a second doc update if ruleset state changes.

The redesign is converging, not regressing. Fix the two 06-01 blockers, make the vuln ruleset step mandatory, and close the two medium crash/order gaps before execution.


---

## OpenCode Review (grok-4.5)

I'll verify each round-1 blocker and redesign claim against live source, starting with session search skill plus the key files the plans cite.
Next I'll ground the OPS-02/04 claims in projection, retention, cold history, and game_id resolution source.
# Phase 6 Plan Review — ROUND 2 (post-revision)

**Verdict:** better than round 1 and **mostly ready** for execute after a few HIGH gaps. Round-1 **DEFAULT / timestamp-flip / false-codecov / resolver-order / detach-age** blockers are addressed in the revised plans. Redesign introduces **new ship-ordering and blast-radius issues** that plans do not scope.

Live checks this session: ruleset `11923801` required checks = `[Build, Lint, Test, CodeRabbit, Integration Test, E2E Test]`; codecov upload `fail_ci_if_error: false` at `.github/workflows/ci.yaml:146+`; nats-server still `v2.14.2` at `go.mod:22`.

---

## Round-1 blocker resolution matrix

| # | Blocker | Status | Evidence |
|---|---------|--------|----------|
| 1 | DEFAULT partition (ATTACH fail + `DETACH … CONCURRENTLY` illegal) | **RESOLVED** (in plan) | 06-01 prohibitions/task: no DEFAULT; legacy left in `events_audit_unpartitioned`; 06-02 backfill |
| 1b | Deterministic partition key (real column, not GENERATED; identical live/replay) | **RESOLVED** (in plan) | `event_ms BIGINT NOT NULL` + `ulid.Time` in shared `writeAuditRow` (`projection.go:353-435` is today’s single path) |
| 1c | Keep `timestamp` store-time for cold filters | **RESOLVED** (in plan) | cold filters `timestamp` at `cold_postgres.go:159-165`; plan leaves `pgnanos.From(meta.Timestamp)` at `projection.go:425` |
| 1d | Migration-straddle idempotency (pre-up live + post-up replay → 1 row) | **PARTIALLY** | 06-01 tests dual post-partition writes; full legacy→backfill→replay sits in **06-02 Task 2** (depends_on OK if waves co-deploy) |
| 2 | Down migration data-preserving | **RESOLVED** (in plan) | 06-01 Task 1 down: copy partition (+ remaining unpartitioned) before drop |
| 3 | 06-02 backfill event_ms + EnsurePartitions **backward** + rename-to-`_detached_<unix>` + real first-cycle gate | **RESOLVED** (in plan) | Matches live defect: `Start` always `return nil` + async `RunOnce` at `retention.go:104-108,135` |
| 4 | 06-03 allowlist OSV-only + v2 CLI + pre/post bump behavior | **RESOLVED** (in plan) | Plan is explicit |
| 5 | 06-04 false “patch is hard gate” + honest 1% label | **RESOLVED** | Plan overrides D-07; matches live ruleset API + `fail_ci_if_error: false` |
| 6 | 06-05 resolver mirrors server + real `cmd/holomush` seam test | **RESOLVED** (in plan) | Server order `core.go:300-303` = root `cfg.GameID` then `dbSub.GameID()`; not `event_bus.GameID` which Defaults to `"main"` (`eventbus/config.go:153-154`) |

---

## 06-01 — partition migration + writeAuditRow atomic core

### Summary
Redesign is the right fix for the DEFAULT disaster: **no DEFAULT**, separate **`event_ms`**, **`timestamp` untouched**, **data-preserving down**, crypto gate. Atomic PK + `ON CONFLICT` coupling is still correct against source (`projection.go:414-421`, `replay.go:219`).

### Strengths
- Correctly requires post-000038 column set (000009 alone reintroduces TIMESTAMPTZ via old shape).
- Composite `(id, event_ms)` matches Postgres rule that UNIQUE/PK must include partition key; shared writer is the only production insert (`projection.go:416`).
- Cold path truly uses `timestamp`, not a new key (`cold_postgres.go:140,159-165`).
- Crypto neutrality claim is grounded in `replay.go:204-208` (column AAD note).

### Concerns
- **HIGH — test/seed blast radius unplanned.** `event_ms NOT NULL` + composite ON CONFLICT will break many direct inserts/`ON CONFLICT (id)` still present across ~16 files (e.g. `internal/store/events_audit_test.go:62-66`, `internal/testsupport/holomushtest/server.go:556+`, cold/readstream/crypto integ tests). Production path is only `writeAuditRow`; **suite CI will go red** unless 06-01 (or a sibling) updates helpers. Plan `files_modified` does not list these.
- **HIGH — deploy unit with 06-02.** Up **renames all history off `events_audit`**. Cold history, rekey/readstream helpers, and every `FROM events_audit` read empty/partial until 06-02 backfill. Success criterion #1 needs both plans; shipping 06-01 alone is a history outage.
- **HIGH — bootstrap orphan check gap (new from redesign).** `runBootstrapOrphanCheck` runs **before** audit subsystem Start (`core.go:292-297`) and only scans `events_audit` (`bootstrap_orphan.go:28-35`). After 06-01, orphans in `events_audit_unpartitioned` are invisible; backfill later re-homes them **after** the gate passed. Defense-in-depth hole for restore/partial-legacy data.
- **MEDIUM — straddle proof not in this plan.** 06-01 Task 2 only proves two post-partition writes with different store times; legacy-unpartitioned path is 06-02.
- **MEDIUM — stale 06-PATTERNS.md.** Still describes `PARTITION BY RANGE (timestamp)` + ATTACH of old table (`06-PATTERNS.md` events_audit section). Executor `read_first` includes PATTERNS — conflict risk with revised plan.
- **LOW — static month partitions in SQL.** Apply-month baking is fine if 06-02 EnsurePartitions runs same deploy; multi-month 06-01-only lag can “no partition for row.”

### Suggestions
1. Require **06-01+06-02 single ship unit** (or merge 000052 after backfill is code-ready) and document ops freeze.
2. Expand 06-01/02 to patch **test INSERT helpers** / shared seed APIs (`holomushtest`, store tests).
3. Orphan check: also scan `events_audit_unpartitioned` if present, **or** run check after backfill.
4. Refresh 06-PATTERNS to `event_ms` + no-DEFAULT + Go backfill.
5. Prefer `event_ms = int64(id.Time()) * int64(time.Millisecond)` (ULID ms) or document `ulid.Time(u.Time()).UnixNano()` API clearly (`decodeULIDString` only returns `[]byte` today at `projection.go:511-518`).

### Risk: **MEDIUM-HIGH** (plan design sound; integration/ship gaps)

**R1 blockers for 06-01:** DEFAULT **RESOLVED**; down **RESOLVED**; timestamp semantics **RESOLVED**; straddle **PARTIAL**.

---

## 06-02 — PartitionManager + backfill + worker wiring

### Summary
Closes the production gap: worker unwired (`NewRetentionWorker` only tests), interface mostly unimplemented in prod, `Start` never surfaces failure (`retention.go:104-108`). Redesign (backward Ensure, rename-age drop, sync first cycle, no DEFAULT) is coherent with 06-01.

### Strengths
- Correct note that plain `RetentionWorker.Start` cannot drive all-or-nothing rollback.
- Co-home under `SubsystemAuditProjection` avoids `SubsystemID` cascade (Start already has pool + projection rollback pattern at `subsystem.go:202-260`).
- Detach→rename→`DropDetachedPartitions` by name-age is a real bookkeeping mechanism (round 1 gap).
- Straddle test (legacy + replay → 1 row) is the right place for migration idempotency.

### Concerns
- **HIGH — same co-deploy / history-blackout dependence as 06-01.** Backfill is the only path that restores cold visibility.
- **MEDIUM — first-cycle scope ambiguity.** “Backfill + EnsurePartitions **or** first RunOnce”: full `RunOnce` also Detach/Drop (`retention.go:63-98`). Prefer **backfill + Ensure only** as the sync gate; leave Detach/Drop to the periodic worker so boot doesn’t prune on first red deploy.
- **MEDIUM — dual-copy window.** Replay can insert into partitioned parent **before** backfill while legacy row still sits in `events_audit_unpartitioned` (no cross-table UNIQUE). After backfill `ON CONFLICT`, one row remains; intermediate dual existence should be documented; prefer “backfill before projection accepts traffic” or order Start: backfill → ensure → then start projection (plan today starts projection first at `subsystem.go:230-239`, then retention — **legacy invisible until later in Start**, and dual possible if DLQ replay races).
- **LOW — EnsurePartitions API overload.** Worker always passes `months=3` (`retention.go:68`); manager holds RetainWindow for backward span — OK if documented; don’t confuse ABAC `PostgresPartitionCreator` (different table/interface).

### Suggestions
1. Order Start: construct manager → **Backfill → EnsurePartitions** → start projection → start periodic worker (or freeze projection until backfill completes).
2. Acceptance: assert never DETACH/DROP `events_audit_unpartitioned` / non-child.
3. Document immediate detach eligibility of super-old backfilled month partitions (expected under 90d).

### Risk: **MEDIUM**

**R1 blockers for 06-02:** DEFAULT interaction **RESOLVED** (no DEFAULT); detach age **RESOLVED**; Ensure horizon **RESOLVED**; Start rollback **RESOLVED** (if sync path is real, not `worker.Start`).

---

## 06-03 — nats bump + vuln gate

### Summary
Sound: govulncheck alone cannot catch GHSA-q59r on nats-server (dep still `v2.14.2` at `go.mod:22`); OSV leg required; pre-bump fail → bump → green; allowlist OSV-only; pin install; ruleset manual step.

### Strengths
- Fail-closed exception policy matches golang/go#59507 “no govulncheck suppressions.”
- Exit-code based pass/fail (not stdout regex) matches repo search-tools rule.
- Out of `lint:` umbrella so local lint stays fast.

### Concerns
- **LOW — A4 residual:** if OSV lacks GHSA-q59r at pin time, plan correctly STOPs — keep that hard stop.
- **LOW — ruleset “vuln must block”** needs same operator checklist as codecov (called out — good).

### Suggestions
Single protect-main checklist issue: `vuln` + `codecov/patch` + `codecov/project`.

### Risk: **LOW**

**R1 blocker:** **RESOLVED**

---

## 06-04 — coverage doc + ratchet

### Summary
Correctly **falsifies** D-07 / memory; matches live ruleset + nonfatal codecov upload. Dual-file cleanup and project `{target: auto, threshold: 1%}` are right. Honest “1% regression allowance” / no false “hard gate” language is fixed.

### Strengths
- Success criterion #4 “correct the doc to enforced reality” achievable even if ruleset step is deferred.
- Delete plain `codecov.yml` (ignore-only subset of `.codecov.yml`) is safe.

### Concerns
- **LOW — optional Task 3** means “gate blocks merges” half may stay unmet; plan records that — OK if SUCCESS text stays “docs agree.”
- **LOW — docs still cite concrete ruleset IDs** that can drift; prefer “via protect-main ruleset” + how to verify with `gh api`.

### Suggestions
After operator adds checks, optionally tighten docs once with API evidence in SUMMARY only.

### Risk: **LOW**

**R1 blocker (false patch gate premise):** **RESOLVED**

---

## 06-05 — DLQ game_id resolve + non-tautological test

### Summary
Resolver order now matches the server. Bug remains live: `runAuditDLQReplay` uses `dlqConfigForGame(cfg.GameID)` after loading **only** `event_bus` (`cmd_audit.go:145-148,325`) while `eventbus.Config.Defaults` forces `"main"` and audit `DLQConfig.Defaults` → `internal.main.audit.dlq` (`dlq.go:26-37,69-75`). Dedicated `cmd/holomush` package-main natstest test fixes round-1 “hand-built config tautology.”

### Strengths
- Explicit: **not** `event_bus.game_id`, **no** `event_bus.Config.Defaults()` on this path.
- Root load path analog: `config.Load(..., "core")` + `GameID` at `core.go:70,125,300-303`.
- Removing/replacing `internal/eventbus/audit/dlq_replay_integration_test.go` same-`"main"` + `eventbustest` shape (`:27,102,107`) is required by D-06.
- Package main tests already exist for integration (`cmd/holomush/*_integration_test.go`).

### Concerns
- **MEDIUM — integration harness weight.** Driving real NATS + Postgres + seed system_info + ReplayDLQ in `cmd/holomush` is doable but heavy; acceptance correctly forbids bypassing resolveGameID — keep it as MUST.
- **LOW — all-empty → legacy main** still the old default; document for multi-game external ops (escape hatch `--game-id`).

### Suggestions
Unit-test inject for GetSystemInfo already planned — keep; ensure integration seeds only DB ULID for recovery path without writing magic prefixes into resolveGameID.

### Risk: **LOW-MEDIUM**

**R1 blockers:** order + real-seam test **RESOLVED** (as written)

---

## Overall

### Converging?
**Yes.** Round 1 HIGH architectural conflicts (DEFAULT vs DETACH, timestamp/tier, false codecov MUST, inverted resolver) are fixed. Round 2 issues are **narrower**: primarily **ship coupling**, **orphan-check ordering**, **test insert blast radius**, and **stale PATTERNS**.

### Do plans still meet the four success criteria?
| Criterion | Verdict |
|-----------|---------|
| #1 events_audit bounded by RetentionWorker | **Yes if 06-01+06-02 co-deploy and worker runs** — no permanent unprunable DEFAULT remnant |
| #2 vulnerable nats fails gate; ≥2.14.3 | **Yes** (osv-scanner leg + bump) |
| #3 DLQ external game_id recovery + non-tautological test | **Yes** (as planned) |
| #4 coverage policy vs enforcement agree | **Yes via doc correction**; blocking still optional ruleset |

### Top new redesign issues (fix before execute)
1. **HIGH** — Co-deploy 06-01 with 06-02 (history blackout + dual-table window + orphan-check hole).
2. **HIGH** — Scope updates for all `INSERT INTO events_audit` / `ON CONFLICT (id)` test paths post-`event_ms`.
3. **HIGH** — Orphan check vs unpartitioned residual (bootstrap_orphan.go + core Start order).
4. **MEDIUM** — Sync first cycle = backfill+Ensure only (not full RunOnce Detach/Drop).
5. **MEDIUM** — Rewrite stale **06-PATTERNS.md** so executors don’t re-implement DEFAULT/timestamp partition.

### Overall risk: **MEDIUM** (down from HIGH)

**Ready for execute?** After addressing the three HIGHs above (even as deltas on the plans), **yes**. No rewrite of the partition strategy is needed — this is an integration/ops completion pass, not a round-1-class redesign.


---

## Antigravity Review

I am locating the HoloMUSH repository directory on your system to verify the implementation plans directly against the live codebase. Once the location is confirmed, I will examine the relevant files to perform the requested cross-AI plan review.
I am locating the source repository worktree containing the migration files and source code. As soon as the search returns, I will inspect the relevant files.
I am locating the active Conductor worktree for HoloMUSH.
# Cross-AI Plan Review Report — Phase 6 (ROUND 2, Post-Revision)

## Overall Verdict & Review Signal

**Review Result: APPROVED WITH HIGH CONFIDENCE (CONVERGING SIGNAL)**

The Round 2 revisions to the 5 implementation plans (`06-01-PLAN.md` through `06-05-PLAN.md`) represent a **textbook converging review**. Every single blocker identified in Round 1 has been directly addressed against live source code (`/Volumes/Code/github.com/holomush/holomush`), without introducing new architectural debt, security flaws, or invalid postgres/Go constructs.

---

## Round 1 Blockers Verification Matrix

| Blocker ID | Description | Status | Verification & Live Source Findings |
| :--- | :--- | :--- | :--- |
| **B1** | **06-01 Partition Strategy & Deduplication** | **RESOLVED** | • **No DEFAULT partition:** Migration `000052` explicitly creates monthly range partitions (`events_audit_YYYY_MM`) for current + 2 future months, with no DEFAULT partition. This guarantees that `ALTER TABLE events_audit DETACH PARTITION ... CONCURRENTLY` in `06-02` is valid SQL in PostgreSQL 14+.<br>• **Deterministic `event_ms` partition key:** `event_ms BIGINT NOT NULL` is populated in Go via `ulid.Time(id).UnixNano()` (derived from the 48-bit timestamp embedded in the event ULID). Because the event ULID is byte-identical across live projection and DLQ replay, `(id, event_ms)` is deterministic, making `ON CONFLICT (id, event_ms) DO NOTHING` hold across both write paths.<br>• **`timestamp` column untouched:** `timestamp` remains `pgnanos.From(meta.Timestamp)` (JetStream store-time). Verified in `internal/eventbus/history/cold_postgres.go:160-165` that queries filter on `timestamp`, preserving cold-history window filtering and edge logic without tier-boundary drift. |
| **B2** | **06-01 Down Migration Data Preservation** | **RESOLVED** | • The `000052` down migration creates a temporary single-table `events_audit` and executes `INSERT INTO events_audit ... SELECT ... FROM events_audit` (and `events_audit_unpartitioned` if present) before dropping the partitioned parent and child tables. Post-up audit records are preserved on rollback. |
| **B3** | **06-02 Backfill & Detach Tracking** | **RESOLVED** | • `Backfill(ctx)` re-homes legacy rows from `events_audit_unpartitioned` into `events_audit`, deriving `event_ms` in Go and running `EnsurePartitions` across historical ranges before insertion.<br>• `DetachExpiredPartitions` renames detached children to `events_audit_<YYYY_MM>_detached_<unix>`. `DropDetachedPartitions` discovers detached tables by name pattern `events_audit_%_detached_%`, parses the epoch, and drops only those exceeding grace.<br>• `SubsystemAuditProjection.Start` synchronously executes `Backfill` and initial `EnsurePartitions`, aborting subsystem start and rolling back if the initial DDL/backfill fails. |
| **B4** | **06-03 Vuln Gate Scope & Pinning** | **RESOLVED** | • **Allowlist Scoping:** `osv-scanner.toml` suppressions apply *only* to OSV-Scanner. `govulncheck` has no suppression mechanism, remaining a hard stop for reachable Go vulns.<br>• **OSV-Scanner Versioning:** Pins OSV-Scanner v2 (with human verification in Task 1) using the v2 CLI subcommand `osv-scanner scan source -L go.mod --config=osv-scanner.toml`.<br>• **Behavioral Failure Proof:** Task 2 verifies that `task lint:vuln` with `nats-server@v2.14.2` exits non-zero citing `GHSA-q59r-vq66-pxc2`. Task 3 bumps `go.mod` to `v2.14.3` and verifies the gate turns green (exit code 0). |
| **B5** | **06-04 Codecov Premises & Ruleset Reality** | **RESOLVED** | • **Ruleset Facts:** Verified live via `gh api repos/holomush/holomush/rulesets/11923801` — required status checks are `[Build, Lint, Test, CodeRabbit, Integration Test, E2E Test]`. Neither `codecov/patch` nor `codecov/project` is required currently.<br>• **Doc Correction:** `.claude/rules/testing.md` and `CLAUDE.md` updated to state that codecov posts status checks on PRs but only blocks if added to ruleset `11923801`. Fictional per-package >80% requirement removed.<br>• **Ratchet & Cleanup:** Duplicate root `codecov.yml` deleted. `.codecov.yml` sets `project` status to `{target: auto, threshold: 1%}` (a 1-point regression allowance). An operator checkpoint/task provides explicit instructions to add status checks to ruleset `11923801`. |
| **B6** | **06-05 DLQ Resolver & Divergent-Game Test** | **RESOLVED** | • **Parity with Server Precedence:** `resolveGameID` in `cmd/holomush/cmd_audit.go` mirrors `cmd/holomush/core.go:300-303`: `--game-id` flag override → root `cfg.GameID` → DB `GetSystemInfo("game_id")`. It bypasses `event_bus.Config.Defaults()` to prevent defaulting to `"main"`.<br>• **Non-Tautological Integration Test:** New `natstest`-backed test lives in `cmd/holomush` (package `main`), directly calling `resolveGameID` against a real NATS container with a ULID game ID in DB. Asserts failure guard on wrong game ID (`Failed > 0`, 0 audit rows) and successful recovery on resolved game ID (`Replayed == 1`). Removed tautological test in `internal/eventbus/audit`. |

---

## Detailed Plan-by-Plan Evaluations

### 06-01-PLAN.md — OPS-02 Core: Partition Swap & Idempotency Crux
- **Summary:** Converts `events_audit` to a RANGE-partitioned table on deterministic `event_ms` with composite primary key `(id, event_ms)`, updates `writeAuditRow` in `internal/eventbus/audit/projection.go` to insert `event_ms = ulid.Time(id).UnixNano()`, updates conflict target to `ON CONFLICT (id, event_ms) DO NOTHING`, keeps `timestamp` as JetStream store-time, provides a data-preserving down migration, and gates pre-push on a crypto-reviewer check.
- **Strengths:**
  - Standard Postgres `PARTITION BY RANGE (event_ms)` with composite PK `(id, event_ms)` and matching `ON CONFLICT` clause.
  - Zero DEFAULT partition guarantees clean execution of `DETACH PARTITION CONCURRENTLY`.
  - Preserves `timestamp` column semantics for `cold_postgres.go` window queries.
  - Mandatory crypto review checkpoint ensures `event_ms` addition is certified crypto-neutral.
- **Concerns:** None.
- **Suggestions:** When computing `event_ms` in Go, use `time.UnixMilli(int64(parsedID.Time())).UnixNano()` (or `int64(parsedID.Time()) * 1_000_000`) for explicit nanosecond precision.
- **Risk Rating:** **LOW**

---

### 06-02-PLAN.md — OPS-02 Worker: Partition Manager, Legacy Backfill & Wiring
- **Summary:** Implements `EventsAuditPartitionManager` (`EnsurePartitions`, `DetachExpiredPartitions`, `DropDetachedPartitions`, `HealthCheck`), adds a chunked legacy row `Backfill(ctx)`, and wires the `RetentionWorker` into `SubsystemAuditProjection` with a synchronous first-cycle gate and operator config (`RetainWindow` default 90d).
- **Strengths:**
  - `DetachExpiredPartitions` renames tables to `events_audit_<YYYY_MM>_detached_<unix>`, storing durable epoch metadata directly in table names.
  - `EnsurePartitions` covers both backward retention and forward window, preventing missing-partition INSERT failures during historical DLQ replay.
  - Synchronous first cycle in `SubsystemAuditProjection.Start` ensures startup fails fast if initial partition creation or backfill encounters database errors.
- **Concerns:** None.
- **Suggestions:** Ensure logging inside `DropDetachedPartitions` clearly outputs table names and parsed age vs grace duration for operational visibility.
- **Risk Rating:** **LOW**

---

### 06-03-PLAN.md — OPS-03: nats-server Bump & Vuln-Scan Gate
- **Summary:** Bumps `github.com/nats-io/nats-server/v2` to `>= v2.14.3`, creates `task lint:vuln` running `govulncheck` (reachability) + `osv-scanner` v2 (`osv-scanner scan source -L go.mod --config=osv-scanner.toml`), pins scanner versions with SHA-256 validation in CI, and adds a `vuln:` CI job.
- **Strengths:**
  - Combines `govulncheck` and `osv-scanner` v2 to solve the `govulncheck` vulnerability DB gap for GitHub Security Advisories.
  - Includes empirical pre-bump failure verification proving `nats-server@v2.14.2` fails `task lint:vuln` citing `GHSA-q59r-vq66-pxc2`.
  - Explicit human legitimacy verification checkpoint (Task 1) prevents supply-chain risks when introducing `osv-scanner`.
- **Concerns:** None.
- **Suggestions:** Document in `Taskfile.yaml` that `task lint:vuln` can be run manually before submitting PRs, keeping umbrella `task lint` fast for local workflows.
- **Risk Rating:** **LOW**

---

### 06-04-PLAN.md — QUAL-01: Coverage Policy Reconciliation & Project Ratchet
- **Summary:** Reconciles policy documentation with actual GitHub ruleset state, updates `.codecov.yml` project status to `{target: auto, threshold: 1%}`, deletes duplicate `codecov.yml`, rewrites `.claude/rules/testing.md` and `CLAUDE.md`, and provides an operator action step to add status checks to protect-main ruleset `11923801`.
- **Strengths:**
  - Grounded by live repository examination via `gh api repos/holomush/holomush/rulesets/11923801`.
  - Eliminates inaccurate doc claims regarding per-package coverage gates and false assertions about current merge blocking.
  - Establishes a no-regression project ratchet (`target: auto`) with a 1% tolerance allowance for multi-upload session merge jitter.
- **Concerns:** None.
- **Suggestions:** Maintainer should execute Task 3 to register `codecov/patch` and `codecov/project` in ruleset `11923801` status checks as soon as CI uploads are verified.
- **Risk Rating:** **LOW**

---

### 06-05-PLAN.md — OPS-04: DLQ Replay `game_id` Bridge & Non-Tautological Test
- **Summary:** Implements `--game-id` flag and `resolveGameID` in `cmd/holomush/cmd_audit.go` mirroring `core.go` precedence (`--game-id` override → root `cfg.GameID` → DB `holomush_system_info`), and adds a non-tautological `natstest` integration test in `cmd/holomush` exercising divergent server/CLI game IDs.
- **Strengths:**
  - Guarantees CLI and core server agree on the DLQ subject prefix `internal.<game_id>.audit.dlq`.
  - Package `main` placement (`cmd/holomush/cmd_audit_dlq_replay_integration_test.go`) directly exercises the unexported resolver seam end-to-end against a real containerized NATS broker.
  - Verifies both the failure guard on mismatched game ID (`Failed > 0`, 0 audit rows) and recovery on matching game ID (`Replayed == 1`).
- **Concerns:** None.
- **Suggestions:** Keep `natstest` strictly bounded to integration tests tagged with `//go:build integration`.
- **Risk Rating:** **LOW**

---

## Conclusion & Recommendation

The revised Phase 6 plans are **complete, accurate, and ready for execution**. The redesign successfully resolves all architectural, database, testing, and CI policy concerns flagged in Round 1. Proceed directly to execution.

---

## Consensus Summary — Round 2

**Verdict: CONVERGING — the redesign is the right shape; no round-1-class rewrite is needed.** Both source-grounded reviewers agree: every round-1 ARCHITECTURAL blocker (the non-viable DEFAULT partition + DETACH-CONCURRENTLY incompatibility, the timestamp-semantic drift, the tautological DLQ test, the false codecov-gate premise, the inverted resolver precedence) is RESOLVED. Round 2 surfaces narrower **implementation / integration / ship-ordering** issues, not design flaws. Codex: "NOT READY / HIGH … converging, not regressing." OpenCode/grok: "MEDIUM (down from HIGH) … an integration/ops completion pass, not a round-1-class redesign." Antigravity: converging / APPROVE (low weight).

### Round-1 blocker resolution
| # | Round-1 blocker | Status |
|---|-----------------|--------|
| 1 | DEFAULT partition non-viable / DETACH incompatibility | **RESOLVED** (no DEFAULT; legacy re-homed by 06-02 backfill) |
| 2 | Timestamp not canonical / cold-filter break | **RESOLVED** (separate `event_ms`; `timestamp` untouched — cold_postgres.go:159 filters `timestamp`) |
| 3 | Down migration data loss | **RESOLVED** (copy-back before drop + survival probe) |
| 4 | 06-02 detach→drop tracking / horizon / dead rollback | **RESOLVED in mechanism** (rename `_detached_<unix>`, backward Ensure, synchronous first cycle) |
| 5 | 06-03 allowlist / OSV CLI | **RESOLVED** (OSV-only allowlist, govulncheck-no-suppression hard stop, pinned v2 `scan source`) |
| 6 | 06-04 codecov premise false | **RESOLVED** (docs corrected to "posts, doesn't block"; cites ruleset 11923801; D-07 overridden; "1%" = regression allowance) |
| 7 | 06-05 resolver order inverted / test can't reach seam | **RESOLVED** (order mirrors server core.go:300; test in `cmd/holomush` drives the real resolver) |

### New round-2 concerns (fix before execute — an incorporation pass, NOT a redesign)

**HIGH:**
1. **ULID API compile bug [orchestrator-VERIFIED] (codex HIGH, grok corroborated).** The plans specify `ulid.Time(parsedID).UnixNano()` in 06-01 (writeAuditRow) and 06-02 (backfill). But `oklog/ulid/v2@v2.1.1` defines `func Time(ms uint64) time.Time` — it takes a `uint64` ms, not a `ulid.ULID`; and `decodeULIDString` returns `([]byte, error)` (projection.go:511), not a parsed ULID. As written it does NOT compile. Fix: one shared helper — `parsedID, err := ulid.Parse(msgID); idBytes := parsedID.Bytes(); event_ms := ulid.Time(parsedID.Time()).UnixNano()` — used by both projection and backfill.
2. **Renamed-table PK/index name collision (codex HIGH).** `ALTER TABLE events_audit RENAME TO events_audit_unpartitioned` does NOT free the relation names `events_audit_pkey` / `events_audit_*` indexes (000009). Creating a new `events_audit` with the same PK/index names collides, and `CREATE INDEX IF NOT EXISTS` can silently reuse legacy indexes, leaving the new parent unindexed. Fix: rename the legacy PK + all `events_audit_*` indexes to `_legacy` before creating the new parent; assert index OWNERSHIP (which table), not just name existence.
3. **Test/seed blast radius [orchestrator-VERIFIED: 27 files match, ~17 tests] (grok HIGH).** Adding `event_ms NOT NULL` + the composite `ON CONFLICT (id, event_ms)` breaks every direct `INSERT INTO events_audit` / `ON CONFLICT (id)` outside the production `writeAuditRow` path (e.g. `internal/store/events_audit_test.go`, `internal/testsupport/holomushtest/server.go`, cold/readstream/crypto integ tests). Production is fine (only writeAuditRow inserts), but the SUITE goes red. Fix: 06-01 (or a sibling task) updates the shared test-insert helpers + seed APIs; list them in `files_modified`.
4. **06-01↔06-02 co-deploy / history blackout (grok HIGH).** The up migration renames all history off `events_audit`; cold history, rekey/readstream, and every `FROM events_audit` read is empty/partial until 06-02's backfill re-homes rows. Shipping 06-01 without 06-02 is a history outage. Fix: require 06-01+06-02 as a single ship/deploy unit (same wave), and order Start so the backfill completes before the projection accepts traffic.
5. **Bootstrap orphan-check gap (grok HIGH).** `runBootstrapOrphanCheck` runs before audit-subsystem Start (core.go:292-297) and scans only `events_audit` (bootstrap_orphan.go:28-35); after the rename, residual rows in `events_audit_unpartitioned` are invisible until the later backfill — a defense-in-depth hole for restore/partial-legacy data. Fix: also scan `events_audit_unpartitioned` when present, or move the check after backfill.
6. **Vuln + codecov gates may finish NON-blocking (codex HIGH).** OPS-03/D-04 requires a BLOCKING gate, but the ruleset addition (add `vuln` / `codecov/patch` / `codecov/project` to protect-main) is only an "open action." A CI job that runs but isn't required doesn't block. Fix: make the ruleset-add a MANDATORY human-action checkpoint (not an open follow-up), verified via `gh api …/rulesets/11923801`.

**MEDIUM:**
7. **06-02 detach→rename not crash-atomic (codex).** `DETACH … CONCURRENTLY` (outside tx) then a separate rename → a crash between them leaves a `events_audit_YYYY_MM` that is no longer a child and doesn't match the `_detached_<unix>` drop scanner → permanently stranded. Fix: persist detach metadata before detaching, or reconcile canonical-named detached tables.
8. **06-03 pre-bump check is fail-open (codex).** `task lint:vuln; echo "exit=$?"` returns the `echo` exit status, masking the real one (the repo's own search-tools rule: judge by exit code, not output). Fix: capture `$?` into a var and assert nonzero, then separately assert the advisory evidence.
9. **06-02 first-cycle scope (grok).** A full `RunOnce` also Detaches/Drops; use backfill + EnsurePartitions ONLY as the synchronous boot gate so a first-cycle boot doesn't prune on a red deploy.
10. **06-02 dual-copy window (grok).** A DLQ replay can insert into the partitioned parent before the backfill while the legacy row still sits in `events_audit_unpartitioned` (no cross-table UNIQUE). Order Start: backfill → ensure → start projection.
11. **06-04 doc-drift ordering (codex).** If Task 3 adds the ruleset checks but the docs "may" (not "must") be re-tightened, the finished docs become false. Fix: require a final doc update + lint if checks are added.
12. **Stale 06-PATTERNS.md (grok).** It still describes `PARTITION BY RANGE (timestamp)` + ATTACH-of-old-table — contradicting the revised plans; executors read PATTERNS via `read_first`. Fix: refresh PATTERNS.md to event_ms + no-DEFAULT + Go backfill.

**LOW:**
13. **06-05 "ROOT game_id" imprecise (codex).** The value is `core.game_id` (`coreConfig.GameID` via `config.Load(…, "core")`, core.go:64), not the YAML root. Name the exact section.

### Recommendation
The design is settled. Do ONE more incorporation pass — `/gsd-plan-phase 6 --reviews` — to fold in the round-2 implementation/integration fixes (ULID helper, index/PK rename, test-helper blast radius, co-deploy/orphan ordering, mandatory ruleset checkpoints, crash-atomic detach, exit-code capture) and refresh 06-PATTERNS.md. Then re-run `/gsd-review --phase 6 --codex` to confirm, and execute. This is a completion pass, not a redesign — the loop is converging (round 1 = architecture; round 2 = implementation/integration).
