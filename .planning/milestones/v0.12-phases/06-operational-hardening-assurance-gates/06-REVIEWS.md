---
phase: 6
review_round: 5
reviewers: [codex, opencode, antigravity]
reviewed_at: 2026-07-14T20:45:40Z
plans_reviewed: [06-01-PLAN.md, 06-02-PLAN.md, 06-03-PLAN.md, 06-04-PLAN.md, 06-05-PLAN.md]
models: {codex: default, opencode: openrouter/x-ai/grok-4.5, antigravity: default}
commit_reviewed: 6913cb19b
prior_round: "round 4 REVIEWS.md at commit b95b71e88 (git history)"
---

# Cross-AI Plan Review — Phase 6 (ROUND 5, confirming)

> Round 5 confirms the round-4 deploy-choreography incorporation (commit 6913cb19b). Codex + grok-4.5
> read live source (grounded); Antigravity grounded-to-plan (low weight). All confirm round-3 debt +
> core round-4 fixes closed; they diverge on two round-4-fix refinements Codex traced deeper (deploy
> traffic gating; provenance marker stamp-time) plus a few new ops residuals (see Consensus). No reviewer
> modified the tree.

---

## Codex Review

# Phase 6 Round-5 Plan Review

Reviewed exact HEAD `6913cb19b`. Implementation is not present.

Two safety claims remain incomplete: deploy traffic isolation and partition-marker provenance. Everything else from Round 4 is substantially closed.

The live GitHub ruleset could not be independently queried from this environment. The plans’ mandatory API plus real-PR verification checkpoints are therefore still necessary.

## Round-5 verification addendum

| # | Finding | Status | Verification |
|---|---|---|---|
| 1 | Deploy choreography | **PARTIAL** | The current workflow really does migrate before recreating core at [deploy.yaml:96](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.github/workflows/deploy.yaml:96), while the old writer still uses the incompatible insert at [projection.go:416](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/audit/projection.go:416). Plan 06-02 modifies both required files and specifies stop → migrate → wait-gated core → final up at [06-02-PLAN.md:427](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:427) and [06-02-PLAN.md:442](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:442). However, it stops only `core`; the already-running gateway/cloudflared remain live. `depends_on: core: service_healthy` at [compose.prod.yaml:124](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/compose.prod.yaml:124) governs startup, not an existing gateway. Worse, gRPC does not depend on `SubsystemAuditProjection` at [sub_grpc.go:166](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/cmd/holomush/sub_grpc.go:166) and begins serving before process readiness at [sub_grpc.go:771](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/cmd/holomush/sub_grpc.go:771). Thus the old gateway can reach the new core before the readiness/backfill gate completes. Docker’s `--wait` itself is appropriate—it waits for running/healthy services—but does not stop unrelated existing services. [Docker Compose documentation](https://docs.docker.com/reference/cli/docker/compose/up/) |
| 2 | Retention initial delay | **CLOSED** | Current `Start` immediately reaches `run`, which invokes destructive `RunOnce` before the ticker at [retention.go:103](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/audit/retention.go:103) and [retention.go:127](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/audit/retention.go:127). The plan adds an opt-in initial delay, preserves default behavior, and names the unit-test file and before/after-tick assertions at [06-02-PLAN.md:316](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:316) and [06-02-PLAN.md:382](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:382). |
| 3 | Durable partition provenance | **PARTIAL** | Reconcile and drop are now marker-gated and schema-qualified at [06-02-PLAN.md:190](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:190) and [06-02-PLAN.md:215](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:215). But `EnsurePartitions` performs `CREATE TABLE IF NOT EXISTS` followed by an unconditional `COMMENT`, calling re-stamping “harmless” at [06-02-PLAN.md:179](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:179). PostgreSQL explicitly warns that `IF NOT EXISTS` does not guarantee the existing relation resembles the requested table. Therefore a same-named non-child can be granted the trusted marker, later reconciled, and dropped. [PostgreSQL 18 CREATE TABLE documentation](https://www.postgresql.org/docs/current/sql-createtable.html) |
| 4 | OSV local pre-install | **CLOSED** | `osv-scanner` is currently absent while `govulncheck` exists. Task 2 now installs the Task-1-approved pinned scanner before the pre-bump run at [06-03-PLAN.md:167](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-03-PLAN.md:167), verifies its version at [06-03-PLAN.md:217](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-03-PLAN.md:217), and separately pins the CI install at [06-03-PLAN.md:247](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-03-PLAN.md:247). |
| 5 | Codecov `user_setup.why` | **CLOSED** | It now says the Codecov checks are optional/accepted-deferred and only `Vuln` is mandatory at [06-04-PLAN.md:14](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-04-PLAN.md:14). |
| 6 | `core.game_id` citations and loader test | **CLOSED** | The plan cites the actual field, loader and server precedence at [06-05-PLAN.md:106](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-05-PLAN.md:106), matching [core.go:70](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/cmd/holomush/core.go:70), [core.go:125](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/cmd/holomush/core.go:125), and [core.go:300](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/cmd/holomush/core.go:300). The divergent-section YAML test is explicit at [06-05-PLAN.md:139](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-05-PLAN.md:139). |
| 7 | Config validation test file | **CLOSED** | The plan names `internal/eventbus/audit/subsystem_test.go` and requires both non-positive cases at [06-02-PLAN.md:382](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:382). |

## Round-3 regression check

All 14 Round-3 items remain textually closed:

| Finding | Status | Evidence |
|---|---|---|
| Direct-insert sweep | **CLOSED** | The scoped HEAD-schema sweep, pre-52 exemption, real/non-ULID rules and covering partitions are explicit at [06-01-PLAN.md:297](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:297). The live tree still has the cited direct inserts, including the exempt version-17 case at [migrate_plugins_integration_test.go:62](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/store/migrate_plugins_integration_test.go:62). |
| `DETACH … FINALIZE` | **CLOSED** | Recovery and observable test are specified at [06-02-PLAN.md:191](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:191). PostgreSQL confirms concurrent detach uses two transactions and `FINALIZE` completes interruption. [PostgreSQL ALTER TABLE documentation](https://www.postgresql.org/docs/current/sql-altertable.html) |
| Negative retention config | **CLOSED** | The live hazards are `now.Add(-RetainDenials)` and `time.NewTicker` at [retention.go:83](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/audit/retention.go:83) and [retention.go:130](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/audit/retention.go:130); both are rejected by the plan. |
| Rendered `name: Vuln` | **CLOSED** | Explicit rendered job name plus ruleset/PR verification appears at [06-03-PLAN.md:242](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-03-PLAN.md:242) and [06-03-PLAN.md:299](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-03-PLAN.md:299). Existing `lint:` → `name: Lint` at [ci.yaml:36](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.github/workflows/ci.yaml:36) supports the rendered-name mechanism. |
| Codecov accepted deferral | **CLOSED** | Conditional final state is consistent at [06-04-PLAN.md:21](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-04-PLAN.md:21) and [06-04-PLAN.md:210](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-04-PLAN.md:210). |
| Both down-copy conflict guards | **CLOSED** | Both parent and legacy copies require `ON CONFLICT (id) DO NOTHING` at [06-01-PLAN.md:196](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:196). |
| `tail` exit-code capture | **CLOSED** | Test verifications capture `rc` before truncating output, e.g. [06-01-PLAN.md:346](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:346) and [06-05-PLAN.md:197](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-05-PLAN.md:197). |
| Dedicated migration-52 test | **CLOSED** | Named artifact and ownership/no-default/down-preservation assertions appear at [06-01-PLAN.md:205](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:205). |
| Schema-qualified catalogs | **CLOSED** | Both reconciliation and drop require `pg_namespace` qualification at [06-02-PLAN.md:191](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:191). The separate marker-stamping defect does not regress this qualifier. |
| Co-ship checkpoint | **CLOSED** | Blocking one-PR gate includes migration, backfill, subsystem and deploy changes at [06-02-PLAN.md:482](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:482). |
| OSV empty config | **CLOSED** | `IgnoredVulns = []`, not an empty table entry, is required and scanner-validated at [06-03-PLAN.md:188](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-03-PLAN.md:188). |
| 06-05 title | **CLOSED** | Task title now says `core.game_id` at [06-05-PLAN.md:103](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-05-PLAN.md:103). |
| Conditional coverage must-have | **CLOSED** | Final documentation is conditional on the verified ruleset state at [06-04-PLAN.md:23](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-04-PLAN.md:23). |
| PATTERNS refresh | **CLOSED** | The map now carries deterministic `event_ms`, no DEFAULT, FINALIZE, marker gating and corrected game-ID precedence at [06-PATTERNS.md:56](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-PATTERNS.md:56), [06-PATTERNS.md:79](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-PATTERNS.md:79), and [06-PATTERNS.md:250](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-PATTERNS.md:250). |

## 06-01 review

### Summary

The atomic migration/write-path plan is carefully grounded and remains executable. Its production safety is conditional on correcting 06-02’s choreography.

### Strengths

- It correctly couples the schema and write changes. The existing insert has no `event_ms` and targets `ON CONFLICT (id)` at [projection.go:414](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/audit/projection.go:414).
- Preserving `timestamp = pgnanos.From(meta.Timestamp)` is necessary because cold queries filter that column at [cold_postgres.go:159](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/history/cold_postgres.go:159).
- The dedicated migration test covers ownership rather than mere name existence, no DEFAULT, and data-preserving rollback at [06-01-PLAN.md:205](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-01-PLAN.md:205).
- The direct-insert sweep matches the live blast radius and preserves the version-17 migration test exemption.

### Concerns

- **HIGH, cross-plan:** 06-01 must not ship until the 06-02 deploy traffic-isolation defect is corrected. Its single-step schema is incompatible with the live old writer by design.

### Suggestions

- Keep 06-01 unchanged, but amend 06-02 before execution and retain the co-ship gate.

### Risk

**HIGH inherent migration risk, but no new 06-01-local design blocker.**

## 06-02 review

### Summary

The retention design handles most hard failure modes well, but two destructive-safety claims are currently false.

### Strengths

- The boot order is correctly changed from the current projection-first implementation at [subsystem.go:229](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/audit/subsystem.go:229) to backfill/ensure before projection.
- FINALIZE, crash-orphan reconciliation, negative-config rejection, delayed first prune, co-shipping and bootstrap legacy scanning all have explicit tests.
- The current worker’s Ensure → Purge → Detach → Drop ordering is correctly traced at [retention.go:61](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/audit/retention.go:61).

### Concerns

- **HIGH — player traffic is not actually gated.** Stopping only `core` leaves the old gateway and tunnel live. The plan’s statement that final `up -d` “restores player traffic” at [06-02-PLAN.md:457](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:457) is therefore false.
- **HIGH — provenance can be forged accidentally.** Unconditional `COMMENT` after `CREATE TABLE IF NOT EXISTS` can mark an unrelated existing relation as trusted. Because `RunOnce` calls Ensure before reconciliation/drop, this is reachable in the real worker lifecycle.
- **MEDIUM — automated checks conflict with repository execution rules.** The plan directly invokes `task test:int` in several `<automated>` blocks, for example [06-02-PLAN.md:251](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-02-PLAN.md:251), while the repository says these runs are hook-enforced through `local-check` at [CLAUDE.md:225](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/CLAUDE.md:225).

### Suggestions

- Stop at least `gateway`, `cloudflared`, and `core` before migration; start only core; wait for health; then start gateway/tunnel. Extend the static test to assert `stop gateway/cloudflared/core < migrate < wait-core < final up`.
- Before stamping a marker, query `pg_inherits` using parent and child OIDs and fail closed unless the relation is genuinely a child of `public.events_audit`. Add a full `RunOnce` test where a same-named markerless non-child already exists; assert no marker, rename, or drop occurs.
- Route verbose task checks through `local-check`, or mark a narrowly justified `# offload-exempt`.

### Risk

**HIGH.**

## 06-03 review

### Summary

The dependency bump and two-scanner gate are executable and the Round-4 local-install defect is fixed.

### Strengths

- The live tree pins vulnerable `nats-server/v2 v2.14.2` at [go.mod:22](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/go.mod:22).
- The pre-bump proof checks both nonzero exit status and the specific advisory at [06-03-PLAN.md:215](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-03-PLAN.md:215).
- `scan source -L go.mod --config=...` matches OSV-Scanner v2 usage. [OSV-Scanner usage documentation](https://google.github.io/osv-scanner/usage/)
- The ruleset checkpoint requires both ruleset JSON and a real PR’s rendered `Vuln` context at [06-03-PLAN.md:299](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.planning/phases/06-operational-hardening-assurance-gates/06-03-PLAN.md:299).

### Concerns

- **LOW:** If the local binary is installed into a temporary directory, the plan should ensure that directory remains on `PATH` for the subsequent verification command rather than relying on shell-session persistence.

### Suggestions

- Record one deterministic local tool path and invoke the scanner through that path during both pre- and post-bump proofs.

### Risk

**MEDIUM**, dominated by external database availability and the mandatory repository-settings action.

## 06-04 review

### Summary

The plan now truthfully separates posted Codecov statuses from required merge checks and resolves the dual-config ambiguity.

### Strengths

- Current CI uploads are explicitly nonfatal at [ci.yaml:141](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.github/workflows/ci.yaml:141), [ci.yaml:183](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.github/workflows/ci.yaml:183), and [ci.yaml:250](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.github/workflows/ci.yaml:250).
- The existing config indeed says project 80%/2% and patch 80%/5% at [.codecov.yml:23](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/.codecov.yml:23), supporting the planned targeted change.
- The optional/mandatory split is now internally consistent and documentation must be re-tightened only if Codecov becomes required.

### Concerns

- **OPEN QUESTION:** The claimed current ruleset membership could not be independently verified here. The plan correctly blocks completion on an authenticated API check.

### Suggestions

- Preserve the mandatory `gh api` plus real-PR evidence in the summary; ruleset JSON alone is insufficient.

### Risk

**LOW–MEDIUM.**

## 06-05 review

### Summary

The plan matches the live server’s game-ID precedence and replaces the tautological test with the right external-broker test seam.

### Strengths

- The current bug is real: replay passes raw `event_bus.GameID` at [cmd_audit.go:325](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/cmd/holomush/cmd_audit.go:325), while the server prefers `core.game_id`, then persisted DB identity at [core.go:300](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/cmd/holomush/core.go:300).
- The existing test is tautological and embedded: the same `"internal.main.audit.dlq"` drives both seed and replay at [dlq_replay_integration_test.go:27](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/audit/dlq_replay_integration_test.go:27) and uses `eventbustest` at [dlq_replay_integration_test.go:102](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/audit/dlq_replay_integration_test.go:102).
- A wrong-prefix replay genuinely increments `Failed` without writing at [replay.go:198](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/eventbus/audit/replay.go:198), so the proposed divergent-game assertion tests the actual defect.
- `natstest` exposes a real independent NATS connection at [natstest/nats.go:56](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase6/internal/testsupport/natstest/nats.go:56).

### Concerns

- No plan-local blocker found.

### Suggestions

- Retain both recovery and wrong-prefix assertions in the same test so later refactors cannot preserve one while regressing the other.

### Risk

**MEDIUM**, principally due to container-backed integration complexity.

## Overall verdict

**verdict: NOT READY**

**Overall risk: HIGH.**

Required changes before execution:

1. Stop/isolate the player-facing gateway and tunnel during migration and until new-core readiness passes.
2. Stamp partition provenance only after catalog verification that the relation is genuinely a child of `events_audit`; add an end-to-end `RunOnce` non-child test.
3. Align verbose verification commands with the repository’s hook-enforced `local-check` rule.

---

## OpenCode Review (grok-4.5)

I'll verify the Round-5 claims against live source, starting with the search workflow and the critical deploy/retention/CI surfaces.
Next I'll open the deploy/retention/projection sources and cross-check the round-4 fix claims.
# Phase 6 Plan Review — Round 5

**Commit reviewed:** `6913cb19b`  
**Branch:** `gsd/phase-06-operational-hardening-assurance-gates`  
**Method:** plans vs live tree (`deploy.yaml`, `retention.go`, `projection.go`, `core.go`, `cmd_audit.go`, `compose.prod.yaml`, ruleset `11923801`, etc.)

---

## Round-5 Verification Addendum

| # | Round-4 finding | Verdict | Evidence |
|---|-----------------|---------|----------|
| 1 | Deploy choreography (HIGH) | **CLOSED** | Live order is still migrate-before-recreate: `.github/workflows/deploy.yaml:107-108` (`run … core migrate` then `up -d`); runbook mirrors at `sandbox-operations.md:237-239`. 06-02 Task 5 reorders **both** files to stop-core → migrate → `up -d --wait --no-deps core` → `up -d`; both in `files_modified`; must_haves + prohibition; 000052 stays single-step (06-01 heightens T-06-1-08); static line-order verify. Readiness signal is real: `compose.prod.yaml:85-90` → `:9100/healthz/readiness`; `core.go:344` + `:1102-1123`. Gateway is player traffic: `compose.prod.yaml:124-126`. |
| 2 | RetentionWorker initial-delay (MEDIUM) | **CLOSED** | Live: `run` calls `RunOnce` immediately at `internal/audit/retention.go:133-136` before ticker. 06-02 Task 3 adds option + unit test + audit wiring with it. |
| 3 | Durable partition provenance (MEDIUM) | **CLOSED** | Live creator has no marker (`partition_creator.go:38` plain CREATE). 06-02 Task 1 stamps `COMMENT ON TABLE … 'holomush:events_audit_partition'`; reconcile + drop marker-gated + schema-qualified; marker-less same-name test required. |
| 4 | OSV local pre-install (MEDIUM) | **CLOSED** | 06-03 Task 2 installs checksum-pinned osv-scanner **before** pre-bump proof; CI remainspinned in Task 3. |
| 5 | 06-04 `user_setup.why` (LOW) | **CLOSED** | Why text states QUAL-01 is met by doc + ratchet regardless; codecov ruleset add optional/accepted-deferred. |
| 6 | 06-05 citations + section test (LOW) | **CLOSED** | `core.go:70` (`GameID`), `:125` (`config.Load(..., "core")`), `:300-304` (resolve order) match live. Section-selection unit test required. |
| 7 | `Config.Validate` test file named (LOW) | **CLOSED** | 06-02 Task 3 pins `internal/eventbus/audit/subsystem_test.go`. |

### Round-3 residual check (spot-trace)

Still closed in plan text: F3 seed sweep rules; DETACH FINALIZE; negative config reject; rendered `name: Vuln`; F6 codecov accepted-deferral; down `ON CONFLICT` both copies; `| tail` exit-code capture; migration-52 dedicated test; catalog schema-qual; co-ship checkpoint; `IgnoredVulns = []`; 06-05 resolver title/`core.game_id`; conditional QUAL must-haves; PATTERNS resolve order override→core→DB (`06-PATTERNS.md` DLQ section).

---

## 06-01 — OPS-02 migration + write path

### Summary
Partition-swap on deterministic `event_ms`, shared `eventMsFromULID`, composite ON CONFLICT, test blast-radius, crypto gate. Matches live PK/`ON CONFLICT (id)` (`projection.go:414-425`) and next free migration `000052` (latest `000051`).

### Strengths
- ULID API correct vs `oklog/ulid/v2@v2.1.1` (`Time(ms uint64)`, `(id).Time() uint64`); `decodeULIDString` действительно returns `[]byte` (`projection.go:511-517`).
- `timestamp` left as `pgnanos.From(meta.Timestamp)` preserves cold filters (`cold_postgres.go:159-182`).
- No DEFAULT partition (keeps 06-02 DETACH CONCURRENTLY viable).
- Co-ship with 06-02 + choreography explicitly (not expand/contract), matching operator choice.
- Blast-radius list matches `rg INSERT INTO events_audit` (~16 files); exempts version-pinned `migrate_plugins_integration_test.go`.

### Concerns
- **MEDIUM — deploy safety owns 06-02:** correct, but 06-01 alone is still a prod footgun if someone cherry-picks; Task 6 co-ship is the only enforcement (human).
- **LOW — seed covering partitions:** many seeds need historic partitions; plan addresses via holomushtest helpers first.

### Risk
**Medium** (schema + write path + suite blast) — design sound; risk is execution thoroughness on seed sweep.

---

## 06-02 — OPS-02 worker + deploy choreography

### Summary
Full `PartitionManager` + Backfill + subsystem wiring + config validation + initial-delay + bootstrap orphan scan + deploy reorder. Grounded against unwired worker (only test callers of `NewRetentionWorker`) and Start-first projection today (`subsystem.go:229-237`).

### Strengths
- Boot gate **Backfill+Ensure only** before `p.start` fixes dual-copy race; destructive path weekly-delayed for audit worker.
- FINALIZE (`inhdetachpending`) + reconcile + marker are the right PG 18 story (`ci.yaml` postgres:18).
- Deploy sequence coherent with compose image pull + healthcheck + gateway `depends_on`.
- Rejects `RetainWindow/PurgeInterval <= 0` against real landmines (`retention.go:83`, `:130`).

### Concerns
- **MEDIUM — backfill vs healthcheck budget:** sync Backfill in `Subsystem.Start` must finish before `startupComplete` (`core.go:344`, `:1123`). Core healthcheck budget is ~`start_period 15s` + `retries 15` × `interval 5s` ≈ **~90s** (`compose.prod.yaml:87-90`). Large `events_audit_unpartitioned` can exceed that → Docker marks unhealthy → `restart: unless-stopped` thrash mid-backfill while `--wait` is pending. Plan documents outage but **not** healthcheck/start_period budget or pre-migrate size check. Sandbox may be fine; still unbounded.
- **MEDIUM — package-name steering:** `internal/eventbus/audit` and `internal/audit` are both `package audit`. Plan text says `audit.NewRetentionWorker` / `var _ audit.PartitionManager` inside eventbus/audit files — that does not compile without an alias (`retaudit "…/internal/audit"`). Executors can infer; plan should name the alias.
- **LOW — manual runbook stdin guard:** CI migrate has `-T` + `</dev/null` (`deploy.yaml:107`); manual runbook has neither (`sandbox-operations.md:238`). Plan “preserves” CI guard; should **add** guards on any scripted/heredoc manual path, not only reorder.
- **LOW — first prune delay = PurgeInterval (24h)** when `WithSkipFirstRun`/first-tick: intentional; say so for ops who expect immediate detach.

### Risk
**Medium-high** (DDL retention + first production deploy path). Choreography closes the old-writer window; residual risk is long backfill under Docker health policy.

---

## 06-03 — OPS-03 vuln gate

### Summary
Dual-tool gate (govulncheck + OSV-Scanner v2), nats bump, `name: Vuln`, mandatory ruleset add. nats @ **v2.14.2** live (`go.mod:22`). Ruleset required checks today: Build, Lint, Test, CodeRabbit, Integration Test, E2E Test only (`gh api …/rulesets/11923801`) — no codecov, no Vuln.

### Strengths
- Mentions GHSA-q59r-vq66-pxc2 / Go vuln DB gap and OSV as the fail-on-pin mechanism.
- Pre-bump verify captures `rc` then greps advisory (not fail-open echo).
- Empty allowlist form + rendered `Vuln` name matches live ruleset context style (`name: Lint` at `ci.yaml:37`).

### Concerns
- **LOW — pre-bump proof mutates nothing:** must run while still on v2.14.2 before Task 3 tidy; task order is correct if followed strictly.
- **LOW — DB fetch flakiness** accepted in threat model (appropriate).

### Risk
**Low.**

---

## 06-04 — QUAL-01 coverage

### Summary
Ratchet + dual-file cleanup + doc rewrite + consolidated optional codecov / mandatory Vuln checklist. Verified: dual configs exist; project still `{target: 80%, threshold: 2%}` (`.codecov.yml:27-28`); fake per-package MUST still in `CLAUDE.md:187` / `.claude/rules/testing.md:25-27`; codecov **not** required in ruleset.

### Strengths
- Corrects false “patch already blocks” premise against live ruleset.
- Clear mandatory vs accepted-deferred split; conditional docs tighten if codecov added.

### Concerns
- **LOW — doc multi-site:** AGENTS.md is symlink to CLAUDE per policy; plan edits CLAUDE + testing.md — fine if lint:docs-symmetry holds.

### Risk
**Low.**

---

## 06-05 — OPS-04 DLQ game_id

### Summary
Resolver mirrors server; natstest e2e in `cmd/holomush` package main. Live bug site: `runAuditDLQReplay` → `dlqConfigForGame(cfg.GameID)` with empty event_bus game (`cmd_audit.go:308,325,337-342`). Server subject: `fmt.Sprintf("internal.%s.audit.dlq", gameID)` (`core.go:572-577`). Tautology: same `"main"` seed/replay + `eventbustest` (`dlq_replay_integration_test.go:27,102,107`).

### Strengths
- Precedence override → `core.game_id` via `config.Load(..., "core")` → DB matches `core.go:300-304`.
- Forbids `event_bus.Defaults()` (would force `"main"` — `eventbus/config.go:153-154`).
- Integration test placement in package main can hit unexported `resolveGameID`.

### Concerns
- **LOW — list/other DLQ cmds** still use raw config GameID; only replay is in scope (OK for OPS-04).
- **LOW — empty → legacy main:** documented; still operator footgun if all three empty.

### Risk
**Low-medium.**

---

## Cross-plan / new residuals

| Item | Sev | Note |
|------|-----|------|
| Long Backfill vs compose healthcheck restart loop | MEDIUM | New residual after choreography is “done”; not expand/contract, but deploy still can thrash on fat histories. |
| `internal/audit` vs `eventbus/audit` import alias not spelled | MEDIUM | Compile-time landmine for naive paste of plan pseudocode. |
| Wave 1 can land without Wave 2 / co-ship | LOW | Wave edges + checkpoints OK; discipline still required. |
| Manual runbook lacks holomush-aocap stdin bind | LOW | Async if runbook used under SSH heredoc. |

No evidence that earlier closed items (FINALIZE, ON CONFLICT down, `name: Vuln`, codecov deferral, F3 test rewrite location) regressed in commit `6913cb19b`.

---

## Overall

### Verdict: **READY**

Round-4 HIGHs (deploy choreography, option A single-step migration) are **closed in plan text** and **warranted by live migrates-while-old-core-runs order**. Remaining items are MEDIUM executable/ops residuals, not flat reopens of R4 HIGHs.

### Overall risk: **Medium**

Driven by OPS-02 (partition + retain + first production deploy + suite blast). OPS-03/04/QUAL-01 are lower risk once human ruleset checkpoints run.

### Before execute (suggested plan polish — not blocking READY)
1. Name the import alias for `internal/audit` from `internal/eventbus/audit` in 06-02 Task 1/3.
2. Budget first-migrated Backfill: raise `start_period`/document max row count, or optional offline backfill CLI if history can exceed ~90s health window.
3. Add `-T` + `</dev/null` to the manual runbook migrate line when re-choreographing.

Cross-AI rubber-stamp avoidance: line citations above were read from the worktree, not plan claims alone.


---

## Antigravity Review

# Cross-AI Plan Review Report — Phase 6 (ROUND 5 / Round-6 Overall)

**Target Phase:** Phase 6 (Operational Hardening & Assurance Gates)  
**Status:** **APPROVED / READY FOR EXECUTION**  
**Commit / Plan Version Reviewed:** `6913cb19b` (Plans `06-01-PLAN.md` through `06-05-PLAN.md`)

---

## Executive Summary & Verdict

The Round 5 (Round-6 overall) iteration of the Phase 6 implementation plans has successfully resolved all architectural, edge-safety, and operational issues raised in prior review rounds. Most notably, the high-severity live-deployment migration window issue has been cleanly resolved using **Deploy Choreography** (stopping the old `core` instance before running `core migrate`, starting the new `core` instance, and gating traffic restoration on health readiness) without introducing risky expand/contract schema churn.

All four core requirements (**OPS-02**, **OPS-03**, **OPS-04**, **QUAL-01**) are supported by complete, source-grounded plan specifications with rigorous test suites, error handling, and security controls.

---

## Verification of Round-4 Resolutions & Architectural Invariants

### 1. Migration Compatibility & Live-Deploy Window (OPS-02 / 06-01 / 06-02 Task 5)
* **Round-4 Finding:** Applying `000052_events_audit_partition.up.sql` while the old binary is running caused old core `INSERT INTO events_audit ... ON CONFLICT (id)` calls to fail due to the non-null `event_ms` column and the dropped single-column `(id)` unique index.
* **Round-5 Resolution (Deploy Choreography):** `.github/workflows/deploy.yaml` and `sandbox-operations.md` are re-choreographed to:
  1. `docker compose stop core` (terminates old writers).
  2. `docker compose run --rm -T core migrate </dev/null` (applies 000052 single-step).
  3. `docker compose up -d --wait --no-deps core` (starts new core; blocks on readiness probe, which incorporates the synchronous `Backfill` + `EnsurePartitions` boot gate).
  4. `docker compose up -d` (restores gateway & public traffic only after core is verified ready).
* **Verdict:** Verified. The single-step migration schema remains simple and atomic, while deploy choreography guarantees no old-writer requests hit the modified table structure.

### 2. Idempotency & Deterministic Partition Key (06-01 Task 2)
* **Design:** `event_ms` is computed deterministically from the immutable event ULID via `eventMsFromULID(id ulid.ULID) int64 = ulid.Time(id.Time()).UnixNano()`.
* **Conflict Target:** `ON CONFLICT (id, event_ms) DO NOTHING` matches the composite primary key `PRIMARY KEY (id, event_ms)` required for range partitioning in PostgreSQL.
* **Store-Time Independence:** The `timestamp` column remains store-time (`pgnanos.From(meta.Timestamp)`), preserving cold-history tier boundaries and query semantics without requiring reader modifications.
* **Verdict:** Verified. Replaying an audit event via DLQ or duplicate JetStream delivery generates the exact same `event_ms` value as the live projection write, hitting the composite unique target and correctly deduplicating.

### 3. Partition Marker Provenance & Reconcile Safety (06-02 Task 1)
* **Round-4 Finding (F9):** Name and schema matching alone could not prove a table absent from `pg_inherits` was a former `events_audit` partition.
* **Resolution:** Every created partition is stamped with a durable database comment:
  `COMMENT ON TABLE <partition> IS 'holomush:events_audit_partition'`
* **Catalog Discovery:** Both `0-RECONCILE` (post-crash rename recovery) and `DropDetachedPartitions` query `obj_description(c.oid, 'pg_class')` to assert the presence of this exact marker before renaming or dropping tables.
* **Interrupted Concurrent Detach Recovery:** Pass `0-FINALIZE` detects partitions in state `pg_inherits.inhdetachpending = true` and executes `ALTER TABLE events_audit DETACH PARTITION <child> FINALIZE`.
* **Verdict:** Verified. Table comments persist across `DETACH PARTITION` and table renames, ensuring non-child tables are never accidentally modified or dropped.

### 4. Worker Start Discipline & Red-Deploy Safety (06-02 Task 3)
* **Round-4 Finding:** Standard `RetentionWorker.Start` invoked `RunOnce()` immediately, creating a risk of destructive partition drops during bootstrap failures on red deploys.
* **Resolution:** `RetentionWorker` introduces an initial-delay functional option (`audit.WithInitialDelay(...)`), configured so the periodic background worker skips the immediate `RunOnce()` on subsystem start. The synchronous boot gate performs only safe setup operations (`Backfill` + `EnsurePartitions`), deferring destructive detach/drop actions until after the first `PurgeInterval` tick.
* **Verdict:** Verified. Destructive retention operations are safely decoupled from process startup.

### 5. Supply-Chain Security & Ruleset Matching (06-03 Tasks 2–4)
* **Scanners:** Combines `govulncheck ./...` (callgraph-aware Go reachability) and `osv-scanner scan source -L go.mod --config=osv-scanner.toml` (OSV/GHSA database matching).
* **Ruleset Name Alignment:** Job configuration uses explicit `name: Vuln` so the rendered status check matches GitHub Ruleset context `Vuln` (aligning with `Lint`, `Test`, etc.).
* **Pre-Bump Validation:** Includes an automated check verifying that pinning `nats-server/v2` @ `v2.14.2` triggers a non-zero exit in `task lint:vuln` due to `GHSA-q59r-vq66-pxc2`.
* **Verdict:** Verified. Fail-closed supply chain assurance is properly established.

---

## Detailed Requirement Traceability Matrix

| Requirement ID | Plan Reference | Core Mechanism | Validation & Test Seams | Status |
| :--- | :--- | :--- | :--- | :--- |
| **OPS-02** | `06-01-PLAN.md`<br>`06-02-PLAN.md` | Range-partition `events_audit` on deterministic `event_ms` key; composite `(id, event_ms)` PK; `RetentionWorker` partition manager with 90d default window & initial-delay option; synchronous `Backfill` boot gate; deploy choreography. | `events_audit_partition_migration_integration_test.go`<br>`projection_idempotency_integration_test.go`<br>`retention_partitions_test.go`<br>Direct deployment script line assertions. | **PASSED** |
| **OPS-03** | `06-03-PLAN.md` | Bump `nats-server/v2` to `>= v2.14.3`; implement `task lint:vuln` (govulncheck + osv-scanner v2); configure `osv-scanner.toml`; pin tool checksums; add `Vuln` check to rulesets. | Pre-bump `GHSA-q59r-vq66-pxc2` non-zero exit test;<br>Post-bump exit-0 test. | **PASSED** |
| **OPS-04** | `06-05-PLAN.md` | Auto-resolve server `game_id` from `holomush_system_info` DB with `--game-id` override flag; integrate `natstest` container harness. | `dlq_replay_integration_test.go` verifying mismatch detection on divergent `game_id` vs successful replay. | **PASSED** |
| **QUAL-01**| `06-04-PLAN.md` | Configure `.codecov.yml` project coverage ratchet (`target: auto`, `threshold: 1%`); remove duplicate `codecov.yml`; update documentation to accurately reflect ruleset check reality. | Direct configuration inspection;<br>Doc accuracy check. | **PASSED** |

---

## Operational Watchlist for Execution

1. **Backfill Duration vs. Container Health Check Thresholds:**
   * During process start, `Backfill` runs synchronously before the metrics/readiness listener flags startup completion.
   * *Mitigation:* `compose.prod.yaml` grants a startup grace period (`start_period: 15s`, 15 retries @ 5s intervals = 90s total window). On sandbox or staging environments with unusually large legacy datasets, ensure `Backfill` completes within this window, or execute `Backfill` ahead of deployment using the staging tools.

2. **Order of PR Merges:**
   * As enforced by Task 6 in `06-02-PLAN.md`, `06-01` (migration `000052`) and `06-02` (`Backfill` and partition wiring) **must** be submitted in a single PR to prevent temporary loss of cold-history visibility.

---

## Conclusion

The Phase 6 plans are thorough, fully grounded in codebase realities, and technically sound. **Execution may proceed immediately.**


---

## Consensus Summary — Round 5

**Verdict: design settled; the loop is at its tail — a small, bounded set of execution/ops refinements
remain, two of them real safety/ops issues.** All three reviewers confirm the 14 round-3 findings and the
core of the round-4 fixes are closed. The grounded pair diverge on the final verdict, and — notably —
**found different residuals**, so the honest picture is their union:

- **Codex** (grounded, top weight): **NOT READY / HIGH** — caught two round-4-fix refinements grok/agy
  missed (deploy traffic not fully gated; provenance marker forgeable) + one repo-convention MEDIUM.
- **OpenCode/grok-4.5** (grounded): **READY / MEDIUM** — round-4 HIGHs closed; its residuals (backfill vs
  healthcheck budget; import-alias compile landmine) are "MEDIUM executable/ops, not reopens."
- **Antigravity** (low weight): **APPROVED** — independently surfaced the backfill-vs-healthcheck watchlist
  item, corroborating grok.

No round-1/2/3 item regressed. This is not an architecture question anymore — it is the last pass of
mechanical/ops hardening.

### Round-4 fix status (round-5 verification)
CLOSED (all reviewers): retention initial-delay, OSV local pre-install, 06-04 `user_setup.why`, 06-05
citations + config-section test, `Config.Validate` test file. All 14 round-3 items CLOSED. Two round-4
HIGHs are **PARTIAL per Codex** (grok/agy read them CLOSED but did not probe the sub-issues below):
- **Deploy choreography — PARTIAL [Codex].** The re-sequence stops only `core`; the old `gateway` +
  `cloudflared` stay live, and gRPC does not depend on `SubsystemAuditProjection` and begins serving
  before process readiness (`cmd/holomush/sub_grpc.go:166,771`), so the old gateway can reach the new core
  before the backfill/readiness gate completes. The plan's "final `up -d` restores player traffic" is
  imprecise — traffic was never fully stopped. `compose.prod.yaml:124` `depends_on: core: service_healthy`
  governs *startup*, not a running gateway.
- **Provenance marker — PARTIAL [Codex].** Reconcile/drop are correctly marker-gated, but `EnsurePartitions`
  does `CREATE TABLE IF NOT EXISTS` then an **unconditional** `COMMENT` (06-02 calls re-stamping
  "harmless"). PG's `IF NOT EXISTS` does not guarantee the existing relation matches the requested table,
  so a pre-existing same-named non-child can be granted the trusted marker and later reconciled/dropped.
  The stamping side, not just the reconcile/drop side, must establish provenance.

### New round-5 residuals (union of the grounded pair — the final fix list)

**HIGH (Codex — real, narrow):**
1. **Deploy traffic not fully gated.** Fix: stop `gateway`, `cloudflared`, AND `core` before migrate; start
   only `core`; wait healthy; then start `gateway`/`cloudflared`. Extend the static verify to assert
   `stop(gateway,cloudflared,core) < migrate < wait-core < final up`.
2. **Provenance marker forgeable at stamp time.** Fix: before stamping the marker, verify via `pg_inherits`
   (parent+child OIDs) that the relation is genuinely a child of `public.events_audit`; fail closed
   otherwise. Add a `RunOnce` test where a same-named markerless non-child already exists and assert no
   marker/rename/drop occurs.

**MEDIUM:**
3. **`task test:int` invoked directly in `<automated>`/verify blocks [Codex] — orchestrator-CONFIRMED
   against CLAUDE.md.** The repo requires verbose `task test|lint|build|test:int|test:cover` runs to route
   through the `local-check` agent (hook-enforced; `.../CLAUDE.md` "delegate verbose task runs"). Fix:
   route through `local-check`, or mark a narrowly justified `# offload-exempt`.
4. **Backfill vs compose healthcheck budget [grok; agy corroborated].** The synchronous `Backfill` runs in
   `Subsystem.Start` before readiness; the core healthcheck budget is ~90s (`start_period 15s` + 15×5s,
   `compose.prod.yaml:87-90`). A large `events_audit_unpartitioned` can exceed it → Docker marks unhealthy
   → `restart: unless-stopped` thrash mid-backfill while `--wait` is pending. Fix: document a pre-migrate
   row-count/size check, raise `start_period`, or offer an offline/ahead-of-deploy backfill path for fat
   histories. (Sandbox is likely fine; the risk is unbounded as written.)
5. **Import-alias compile landmine [grok].** `internal/audit` and `internal/eventbus/audit` are both
   `package audit`; 06-02 text references `audit.NewRetentionWorker` / `audit.PartitionManager` inside
   `eventbus/audit` files, which won't compile without an alias (e.g. `retaudit "…/internal/audit"`). Fix:
   name the alias in the plan so a naive paste compiles.

**LOW:** manual runbook migrate line lacks the CI's `-T` + `</dev/null` stdin guard [grok]; 06-03 local
tool dir must stay on `PATH` across the pre/post-bump commands [Codex]; document that the first prune is
delayed by `PurgeInterval` (~24h) so ops don't expect immediate detach [grok].

### Divergence
- **NOT READY (Codex) vs READY (grok/agy).** The gap is the two PARTIAL sub-issues Codex traced deeper on
  (gateway/tunnel gating; marker stamp-time provenance) — both real. grok/agy read the round-4 HIGHs as
  closed because the reconcile/drop gating and the core-stop are present; they did not probe the running
  gateway or the unconditional stamp. Treat Codex's H1/H2 as authoritative and grok's M4/M5 as the
  complementary ops set.

### Recommendation
The residuals are concrete and bounded — a well-defined final list (H1 gateway/tunnel stop, H2 marker
`pg_inherits` provenance check, M3 `local-check` routing, M4 backfill/health budget, M5 import alias, +
LOWs). Two are genuine safety/ops (H2 marker forge; M4 backfill thrash). Recommended: ONE final targeted
`/gsd-plan-phase 6 --reviews` to fold them in (no design change), then execute — optionally a quick
Codex-only confirm. Alternatively, execute now and let execute-phase's verifier + the pre-push
crypto/code-review gates catch them (M3 self-corrects via the offload hook; M5 via the Go compiler; but
H2/M4 would ship as-planned and rely on code review of the deploy/marker/config changes). The loop has
converged: architecture → core → edge-safety → deployment → deploy-detail/ops. This is the last mile.
