# Phase 6: Operational Hardening & Assurance Gates - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-13
**Phase:** 6-operational-hardening-assurance-gates
**Areas discussed:** Retention mechanism (OPS-02), Vuln-scan gate (OPS-03), DLQ fix + test tier (OPS-04), Coverage reconciliation (QUAL-01)

---

## Retention mechanism (OPS-02)

| Option | Description | Selected |
|--------|-------------|----------|
| Time-based DELETE + config knob | Scheduled batched DELETE WHERE timestamp < cutoff; no migration; DELETE causes bloat/vacuum load at high volume | |
| Partition + wire the RetentionWorker | Migrate events_audit to partitioned; concrete PartitionManager impl; wire the (unwired) worker; O(1) DETACH/DROP; much larger change | ✓ |

**User's choice:** Partition + wire the RetentionWorker
**Notes:** Chose the scale-correct path over the pragmatic DELETE — this is a "foundation hardening" milestone, so the durable answer is preferred. Scout finding surfaced during framing: the existing RetentionWorker is partition-oriented for the ABAC `access_audit_log` table and has zero non-test callers (unwired in production); events_audit is un-partitioned. Planner flags captured in CONTEXT.md (table-swap migration strategy, new PartitionManager impl, production wiring).

---

## Vuln-scan gate (OPS-03)

| Option | Description | Selected |
|--------|-------------|----------|
| govulncheck, blocking, allowlist | Go-native, callgraph-aware (only reachable vulns); new task lint:vuln + ci.yaml job; documented allowlist | ✓ |
| govulncheck, advisory-only | Runs and reports but does not fail CI; not really a gate | |
| Broader scanner (Trivy/grype), blocking | Scans all deps incl. non-Go/transitive; noisier, more false positives, not callgraph-aware | |

**User's choice:** govulncheck, blocking, allowlist
**Notes:** nats-server v2.14.2 → ≥ v2.14.3 bump is mechanical; also check nats.go (v1.52.0) and prometheus-nats-exporter (v0.20.1). govulncheck has no native allowlist — mechanism is a research item flagged for the planner.

---

## DLQ fix + test tier (OPS-04)

| Option | Description | Selected |
|--------|-------------|----------|
| Auto-resolve from DB, --game-id override | CLI reads holomush_system_info for server's real game_id by default; --game-id flag as escape hatch | ✓ |
| Auto-resolve from DB only | Same auto-resolution, no override flag | |
| Explicit --game-id flag only | Operator passes it; can get the ULID wrong → reintroduces the mismatch | |

**User's choice:** Auto-resolve from DB, --game-id override
**Notes:** Fixes the empty-game_id → `internal.main.audit.dlq` default bug. Test tier is determined by repo rule (`.claude/rules/testing.md`): external-NATS DLQ behavior MUST use a real natstest container. Replacement test must exercise divergent server/CLI game_id (the current test uses the same "main" on both sides → tautological).

---

## Coverage reconciliation (QUAL-01)

| Option | Description | Selected |
|--------|-------------|----------|
| Enforce patch gate + correct the doc | Blocking patch-coverage on new code; rewrite doc MUST from per-package>80% to enforced project+patch reality; delete duplicate codecov.yml | ✓ |
| Correct the doc only (no new gate) | Soften MUST to match non-enforcement; zero assurance; Phase 9 depends on a real bar | |
| Enforce full per-package >80% now | Literal compliance; would block nearly all merges at 54.6% | |

**User's choice:** Enforce patch gate + correct the doc — then, after a durable-memory reconciliation, **also add a project-coverage ratchet gate.**
**Notes:** POST-DISCUSSION REFRAME: engram memory `7qhyhb3hsb`/`v5k0e4zs3s` corrected the scout's "patch isn't blocking" finding — codecov/patch @ 80% is ALREADY a hard merge gate via the protect-main *ruleset* (invisible to the classic branch-protection API the scout checked; `fail_ci_if_error:false` only affects the upload step). So the "enforce a gate" half is already satisfied. A second question was asked: (a) document real bar + correct doc + resolve conflicts, vs (b) that PLUS a project-coverage ratchet. User chose **(b)** — add a project ratchet so project% can't drop and nudges 54.6%→80% over time (gives Phase 9 a rising floor). Legacy not retroactively blocked. Planner flag: making the project status a *required* check needs it added to the ruleset (operator action), not just YAML.

## Claude's Discretion

- Retention window default (90d, mirrors ABAC denial default) — adjustable.
- Patch-coverage target number — sensible default; .codecov.yml already sets patch 80%.
- govulncheck allowlist file format — research item.
- Partition key column for events_audit (timestamp vs inserted_at).

## Deferred Ideas

None — discussion stayed within Phase 6 scope. (Gateway OOM/survival F2 #4785 already shipped as PR #4813; god-object decomposition and code-health sweep are Phases 7–9.)
