# Phase 6: Operational Hardening & Assurance Gates - Context

**Gathered:** 2026-07-13
**Status:** Ready for planning

<domain>
## Phase Boundary

Close the remaining operational Highs and stand up the CI assurance gates the
later phases depend on. Four fixed requirements (from `.planning/ROADMAP.md`
§Phase 6 and `.planning/REQUIREMENTS.md`):

- **OPS-02 (F4 #4786):** bound `events_audit` growth so the table cannot grow
  unbounded.
- **OPS-03 (F8 #4790):** remediate the `nats-server` CVE (≥ v2.14.3) **and** add
  a vuln-scan CI gate so a vulnerable dependency is caught, not merged blind.
- **OPS-04 (F3 #4787):** fix the audit-DLQ replay CLI for its external-NATS
  target (the `game_id` split bridge) and replace its tautological coverage test
  with a genuine recovery assertion.
- **QUAL-01 (F7 #4804):** reconcile the coverage policy — the documented bar and
  the enforced bar must agree.

Discussion clarified **how** to implement each; the requirement set is fixed and
NOT open for planning to re-scope.
</domain>

<decisions>
## Implementation Decisions

### OPS-02 — events_audit retention
- **D-01 (mechanism):** **Partition `events_audit` + wire the RetentionWorker.**
  Chosen over the simpler time-based `DELETE` path for scale-correctness (O(1)
  partition DETACH/DROP vs. O(n) DELETE + vacuum load). This is the deliberately
  larger option — the planner MUST account for its full cost (see flags below).
- **D-02 (config knob):** the retention window MUST be **configurable** (add an
  audit/event-bus retention config section), not hardcoded. Default window:
  **90 days** (mirrors the ABAC `RetentionConfig.RetainDenials` default). The
  value is operator-adjustable; 90d is a starting default, not a locked
  requirement.

> **⚠ Planner flags for OPS-02 (grounded by scout):**
> - The existing `RetentionWorker` (`internal/audit/retention.go:40-148`) is
>   **partition-oriented and serves the ABAC `access_audit_log` table**, and has
>   **zero non-test callers — it appears unwired in production.** "Extend the
>   existing worker" therefore means *first making it actually run*, plus adding
>   an `events_audit`-specific `PartitionManager` impl (today only
>   `BootstrapPartitionCreator` exists, `internal/access/policy/bootstrap.go:17`).
> - `events_audit` is currently a **single un-partitioned table**
>   (`000009_create_events_audit.up.sql`). Postgres cannot partition a table
>   in-place — the migration needs a **table-swap / rename + copy-existing-rows**
>   strategy, and must obey `.claude/rules/database-migrations.md` (idempotent,
>   paired up/down, no long-running backfill inside the migration — existing-row
>   copy may need a separate one-shot).
> - Prune key column: `timestamp TIMESTAMPTZ` (`:8`) vs. `inserted_at` (`:15`) —
>   planner to pick the partition key.

### OPS-03 — vuln-scan gate
- **D-03 (dependency bump):** bump `nats-server/v2` from **v2.14.2 → ≥ v2.14.3**
  (`go.mod:22`). Also check `nats.go` (`v1.52.0`) and
  `prometheus-nats-exporter` (`v0.20.1`) against the same advisories.
- **D-04 (gate):** **`govulncheck`, blocking, with an allowlist.** New
  `task lint:vuln` + a `ci.yaml` job. govulncheck is Go-native and
  callgraph-aware (fails only on *reachable* vulns → low false-positive noise) —
  the Go-ecosystem standard. Accepted/unreachable CVEs handled via a documented
  allowlist mechanism.

> **⚠ Planner flag for OPS-03:** govulncheck has **no native allowlist** — the
> mechanism is a research item (e.g. a wrapper script filtering accepted GHSA
> IDs, or `osv-scanner` with a config file). The gate MUST fail closed on an
> unlisted reachable vuln. Slot the new job into `.github/workflows/ci.yaml`
> alongside the existing `task lint` / `test:cover` / `test:int` jobs.

### OPS-04 — DLQ replay + non-tautological test
- **D-05 (game_id fix):** **auto-resolve the server's persisted `game_id` from
  the DB** (`holomush_system_info`; the CLI's pool is already open) as the
  default, with an explicit **`--game-id` flag as an override/escape hatch.**
  Directly fixes the bug where the CLI leaves `event_bus.game_id` empty
  (`cmd_audit.go:143-149`), defaulting the DLQ subject to `internal.main.audit.dlq`
  and mismatching the server's persisted subject.
- **D-06 (test tier):** the replacement test MUST exercise **divergent
  server/CLI `game_id`** (the F3 mismatch path the current test never hits). Per
  `.claude/rules/testing.md`, external-NATS DLQ-against-a-real-broker behavior
  MUST use a **real NATS container via `internal/testsupport/natstest`** — not
  the embedded `eventbustest` harness.

> **⚠ Planner flag for OPS-04:** the current
> `TestReplayDLQRestoresDeadLetterToAuditTable`
> (`internal/eventbus/audit/dlq_replay_integration_test.go:97`) is tautological
> — it seeds and replays with the **same** game `"main"` on both sides
> (`:27`, `:107`), so the prefix always matches and the F3 mismatch is never
> exercised. It proves only happy-path idempotent restore. Replace, don't
> augment.

### QUAL-01 — coverage reconciliation
- **D-07 (direction):** **enforce a blocking PATCH-coverage gate + correct the
  doc.** New code is held to a patch-coverage bar (blocking); the legacy 54.6%
  is **not** retroactively blocked. Rewrite the doc MUST from the fictional
  "per-package > 80%" (`CLAUDE.md` + `.claude/rules/testing.md:25`) to the
  actually-enforced **project + patch** reality codecov measures.
- **D-08 (cleanup):** resolve the **two conflicting codecov files** — `.codecov.yml`
  (full config: project 80%/patch 80%) and `codecov.yml` (375 B, ignore-only).
  Delete/merge the duplicate.

> **⚠ Planner flag for QUAL-01:** `.codecov.yml` already sets `patch target 80%`,
> but `ci.yaml` uploads with `fail_ci_if_error: false` and codecov's *blocking*
> is via **branch protection (a GitHub repo setting, not in-repo)**. "Enforce"
> therefore has two implementation shapes to choose between: (a) make the
> codecov patch **status a required check** via branch-protection settings
> (operator action, not code), or (b) add a **CI job that computes patch
> coverage and fails** in-repo. Pick one explicitly; don't leave it as "codecov
> will block" when nothing currently does. Doc MUST is *per-package* while
> codecov measures *project/patch* — the doc rewrite MUST close that semantic
> mismatch, not just the number.

### Claude's Discretion
- Retention window default (90d) and patch-coverage target number — sensible
  defaults chosen; adjust if research surfaces a better value.
- govulncheck allowlist file format — research item, planner's call.
- Partition key column for `events_audit` (`timestamp` vs `inserted_at`).
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase-6 source findings (2026-07-11 architecture review)
- `docs/reviews/arch-review/2026-07-11/issue-plan.md` — **richest**; per-finding
  acceptance criteria: I6/F4 `:38`, I7/F3 `:39`, I10/F8 `:42`, I15 vuln-gate
  `:61`, I3/F7 `:27`, I4/F3-test `:28`; issue→number map `:96-102`.
- `docs/reviews/arch-review/2026-07-11/REPORT.md` — F2/F4/F8 operational Highs;
  F3/F6/F7 assurance-gap theme (F4 detail `:176`).
- `docs/reviews/arch-review/2026-07-11/findings/d7-data.md` — F4 (retention).
- `docs/reviews/arch-review/2026-07-11/findings/d6-reliability.md` — F3 (DLQ).
- `docs/reviews/arch-review/2026-07-11/findings/d9c-deps.md` — F8 (NATS CVE).
- `docs/reviews/arch-review/2026-07-11/findings/d9a-testing-ci.md` — F7 (coverage).
- `docs/reviews/arch-review/2026-07-11/verification/skeptic-d6-dlq.md` — DLQ skeptic pass.

### Requirements / roadmap
- `.planning/REQUIREMENTS.md:26-45` — OPS-02/03/04, QUAL-01.
- `.planning/ROADMAP.md:129-143` — Phase 6 goal + success criteria.

### GitHub issues
- #4786 (F4 retention), #4790 (F8 NATS CVE + gate), #4787 (F3 DLQ replay),
  #4804 (F7 coverage). Query with `gh issue view <n> -R holomush/holomush`.

### Repo rules (apply during implementation)
- `.claude/rules/database-migrations.md` — OPS-02 partition migration.
- `.claude/rules/testing.md` — OPS-04 external-NATS test tier; QUAL-01 coverage.
- `.claude/rules/event-conventions.md` / `event-interfaces.md` — audit/DLQ context.
</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/audit/retention.go` — `RetentionWorker` (`:40-148`),
  `RetentionConfig`/`DefaultRetentionConfig` (`:23-29`, hardcoded 90d/7d/24h),
  `PartitionManager` interface (`:31-38`). Reuse the worker shape; add an
  `events_audit` PartitionManager impl + production wiring.
- `internal/access/policy/bootstrap.go:17` — `BootstrapPartitionCreator`, the
  only existing PartitionManager (bootstrap-subset; not a full purge/detach impl).
- `internal/eventbus/audit/replay.go:75` — `ReplayDLQ` (ephemeral ordered
  consumer, idempotent `writeAuditRow` `ON CONFLICT (id) DO NOTHING`);
  `replayOne`/`originalSubject` `:192-260` is the mismatch site.
- `cmd/holomush/cmd_audit.go` — `runAuditDLQReplay`, `dlqConfigForGame` `:337-343`,
  empty-game_id default `:143-149`. `cmd/holomush/core.go:300-304,567` — where
  the server persists the DLQ subject from `game_id`.
- `.codecov.yml` — existing project/patch 80% config (make it actually block).
- `.github/workflows/ci.yaml` — the CI gate (jobs: `task lint`, `test:cover`,
  `test:int`, e2e). New vuln + coverage gates slot here.

### Established Patterns
- `internal/testsupport/natstest` — real single-node NATS container harness,
  mandated for external-NATS DLQ behavior (OPS-04 test).
- Migrations: `internal/store/migrations/` — sequential, paired up/down,
  idempotent; `000009_create_events_audit.up.sql` is the target table.

### Integration Points
- RetentionWorker → production lifecycle wiring (currently absent).
- New `task lint:vuln` → `Taskfile.yaml` + `ci.yaml` job.
- CLI DLQ game_id resolution → `holomush_system_info` DB read.
</code_context>

<specifics>
## Specific Ideas

- OPS-02 explicitly takes the **harder, scale-correct** path (partition + wire),
  not the pragmatic DELETE — this is a "foundation hardening" milestone, so the
  durable answer is preferred over the quick one.
- OPS-04 keeps operator burden at zero (auto-resolve) while retaining a manual
  override — no operator should need to know a game_id ULID for the happy path.
</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within Phase 6 scope. (Gateway OOM/survival F2 #4785
already shipped as PR #4813; the god-object decomposition and code-health sweep
are Phases 7–9.)
</deferred>

---

*Phase: 6-operational-hardening-assurance-gates*
*Context gathered: 2026-07-13*
