---
phase: 06
slug: operational-hardening-assurance-gates
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-13
---

# Phase 06 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Derived from `06-RESEARCH.md` § Validation Architecture. The Per-Task
> Verification Map below is populated once `06-*-PLAN.md` task IDs exist.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` + testify (unit); Ginkgo/Gomega for `//go:build integration` |
| **Config file** | none — repo-native; migrations run under `task test:int` |
| **Quick run command** | `task test -- ./internal/eventbus/audit/... ./internal/audit/... ./cmd/holomush/...` |
| **Full suite command** | `task test:int` (Docker: Postgres + real NATS via `natstest`) |
| **Estimated runtime** | quick ~30–90s; `task test:int` several minutes (Docker) |

> **Task-runner only:** dispatch `local-check` for `task test` / `test:int` /
> `lint` / `build` / `cover` rather than running them inline (CLAUDE.md
> hook-enforced). CI-gate deliverables (OPS-03 `task lint:vuln`, QUAL-01
> codecov statuses) are validated by *observable gate behavior*, not Go tests —
> see Manual-Only Verifications.

---

## Sampling Rate

- **After every task commit:** Run the quick command scoped to the touched package.
- **After every plan wave:** Run `task test:int` (OPS-02 partition + OPS-04 DLQ replay need Docker/Postgres/NATS).
- **Before `/gsd-verify-work`:** Full suite green; `task lint:vuln` exits 0 on the bumped tree.
- **Max feedback latency:** quick < 90s; integration bounded by Docker warm-up.

---

## Per-Task Verification Map

*Populated after `/gsd-plan-phase` writes the plans (task IDs are `06-<plan>-<task>`).*

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 06-XX-XX | XX | N | OPS-0X / QUAL-01 | — | {expected behavior} | unit/integration/ci-gate | `{command}` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

### Requirement → Validation Map (from research)

| Req | Success criterion | Validation type | Observable |
|-----|-------------------|-----------------|-----------|
| OPS-02 | rows past window pruned; table bounded | integration (Postgres) | seed old/new month partitions → `RetentionWorker.RunOnce` → old DETACHed+DROPped, recent retained; `writeAuditRow` idempotency (live + replay of same event → 1 row) under composite PK |
| OPS-02 | migration reversible | integration | roll `000052` up→down on scratch DB → `events_audit` returns to un-partitioned shape |
| OPS-03 | pinned-vulnerable nats-server FAILS gate | **CI-gate behavioral** | `nats-server@v2.14.2` → `task lint:vuln` (osv-scanner leg) exits non-zero citing GHSA-q59r-vq66-pxc2; ≥v2.14.3 → exits 0 |
| OPS-03 | gate blocks merges | CI-gate observable | new `vuln:` job is a PR check; added to protect-main ruleset |
| OPS-04 | DLQ replay recovers external-NATS deployment | integration (real NATS) | `natstest`: seed `internal.<ULID>.audit.dlq.*`; wrong game → `res.Failed>0`, 0 rows; resolved/overridden game → `res.Replayed==1`, correct subject |
| OPS-04 | test is non-tautological | test-review | divergent server/CLI game_id (not `"main"` both sides); asserts failure-guard AND recovery |
| QUAL-01 | doc and enforced bar agree | doc-review / grep | `.claude/rules/testing.md` + `CLAUDE.md` no longer say "per-package >80%"; describe ruleset patch gate + project ratchet |
| QUAL-01 | project ratchet blocks coverage-lowering PR | **CI-gate behavioral** | PR dropping project% below parent (beyond threshold) → `codecov/project` FAILURE + `mergeStateStatus=BLOCKED` once ruleset-required |

---

## Wave 0 Requirements

- [ ] `internal/eventbus/audit/retention_partitions_test.go` — new `events_audit` PartitionManager Ensure/Detach/Drop (integration; Postgres).
- [ ] Rewrite `internal/eventbus/audit/dlq_replay_integration_test.go` — divergent-game replay via `natstest` (replace, don't augment).
- [ ] `osv-scanner.toml` + a behavioral check that a pinned-vulnerable `nats-server` fails `task lint:vuln`.
- [ ] Framework installs: govulncheck (present locally; pin in CI) + osv-scanner (new; pin + checksum-verify, gate behind the package-legitimacy check).

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| `codecov/project` blocks merges | QUAL-01 | Requires adding the status to the **protect-main ruleset** (repo-settings/operator action, not in-repo YAML) | After `.codecov.yml` `project` ratchet lands, a maintainer adds `codecov/project` to the ruleset's required checks; verify a coverage-lowering PR shows `mergeStateStatus=BLOCKED` |
| `vuln:` job blocks merges | OPS-03 | New CI job must be added to the ruleset's required checks | Maintainer adds the `vuln` job to protect-main required checks; verify a PR pinning `nats-server@v2.14.2` is blocked |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 90s (quick)
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
