---
phase: 03
slug: platform-hardening-deployment-scaling
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-10
---

# Phase 03 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Derived from `03-RESEARCH.md` § Validation Architecture. This phase is
> **verification-and-extension**: the crypto/cluster substrate already ships;
> most CLUSTER-NN proofs are integration-tier against a real external-NATS
> testcontainer (embedded NATS is deliberately NOT sufficient for
> external-mode-specific behavior — D-06 amends the "embedded is correct at
> every tier" rule).

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testify` (unit) + Ginkgo/Gomega (`//go:build integration`) |
| **Config file** | none — repo drives everything through `task` targets |
| **Quick run command** | `task test -- ./internal/eventbus/... ./internal/config/... ./internal/eventbus/audit/...` |
| **Full suite command** | `task test:int` (external-NATS tier joins the existing `//go:build integration` runner via `./...`; needs Docker) |
| **Estimated runtime** | ~unit < 90s · integration several min (Docker testcontainers) |

> Per-package coverage MUST exceed 80% (`task test:cover`). External-mode
> behavior MUST use a real NATS container, never the `eventbustest` embedded
> harness (D-06).

---

## Sampling Rate

- **After every task commit:** Run the quick command scoped to the touched package
- **After every plan wave:** Run `task test:int` (external-NATS integration tier)
- **Before `/gsd-verify-work`:** `task pr-prep` fast lane green; `task pr-prep:full` for the int/E2E surface
- **Max feedback latency:** ~90 seconds (unit); integration is out-of-band (Docker)

---

## Per-Task Verification Map

> Populated during planning / Wave 0 once the planner assigns `03-<plan>-<task>`
> IDs. Every task MUST carry an automated `<verify>` command or a Wave 0
> dependency; the rows below are the CLUSTER-NN → validation-mode anchors from
> `03-RESEARCH.md` § Validation Architecture.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 03-XX-XX | XX | 1 | CLUSTER-01 | T-03-* / — | external-mode boot dials NATS; `mode: external` + no URL fails config validation; unreachable NATS refuses boot (fail-closed) | unit + integration | `task test -- ./internal/config/... ./internal/eventbus/...` | ❌ W0 | ⬜ pending |
| 03-XX-XX | XX | 2 | CLUSTER-02 | T-03-* / — | boot self-check rejects an over-scoped server account; external verification script proves a non-server principal is denied publish/subscribe on `events.>`/`audit.>`/`internal.>` | integration + script | `task test:int` | ❌ W0 | ⬜ pending |
| 03-XX-XX | XX | 2 | CLUSTER-03 | — | per-replica NATS conn to one external testcontainer; KEK/DEK rotation acks N-of-N; probe-and-pill on a hung replica → cluster proceeds N-1 (binds INV-CLUSTER-1/2/4/9) | integration | `task test:int` | ❌ W0 | ⬜ pending |
| 03-XX-XX | XX | 2 | CLUSTER-04 | T-03-* / — | on final delivery attempt the message is published to the DLQ stream then Term'd; if DLQ publish fails, Nak (never dropped); DLQ replay re-drives entries through the projection write | unit + integration | `task test -- ./internal/eventbus/audit/...` then `task test:int` | ❌ W0 | ⬜ pending |
| 03-XX-XX | XX | 3 | CLUSTER-05 | — | runbook covers provision→creds→configure→cutover→scoping-verify→DLQ inspect/replay→rollback; docs build green | docs build | `task docs:build` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] External-NATS testcontainer harness (single external node) usable from the `//go:build integration` tier — the substrate for CLUSTER-02/03/04 integration proofs
- [ ] `compose.cluster.yaml` overlay (NATS JetStream + 2nd core replica) — the multi-process E2E smoke substrate (D-14)
- [ ] Existing `eventbustest` embedded harness stays correct for all non-external-specific tests

*Existing `task test` / `task test:int` infrastructure covers execution; Wave 0 adds the external-NATS container seam.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| `game.holomush.dev` cutover to external NATS | CLUSTER-05 | Deferred operational task (D-15) — out of scope this phase; the runbook is the deliverable, not the live migration | Follow the runbook against a scratch external cluster; not a CI gate this phase |

*All in-scope phase behaviors have automated verification (integration tier + docs build); only the deferred live-sandbox migration is manual.*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references (external-NATS container harness)
- [ ] No watch-mode flags
- [ ] Feedback latency < 90s (unit)
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
