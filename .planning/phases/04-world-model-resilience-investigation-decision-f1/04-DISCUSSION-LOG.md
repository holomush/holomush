# Phase 4: World-Model Resilience Investigation & Decision (F1) - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-11
**Phase:** 4-world-model-resilience-investigation-decision-f1
**Areas discussed:** ADR leaning, Harness fidelity, Harness disposition, Corruption verdict scope

---

## ADR leaning (MODEL-01)

| Option | Description | Selected |
|--------|-------------|----------|
| Lean B, evidence can overturn | Enter with F1-recommended (B) CRUD-canonical + optimistic-concurrency + transactional-outbox as working hypothesis; still consider (A), harness can overturn | |
| Genuinely open A vs B | Weigh (A) build real event sourcing and (B) CRUD-canonical equally, no starting lean; research costs out (A) | ✓ |
| Pre-decide B | Treat CRUD-canonical as decided; harness only characterizes risk | |

**User's choice:** Genuinely open A vs B
**Notes:** Explicitly overrides the F1 doc's recommended lean toward (B). The ADR and research must give building real event sourcing a fair, fully-costed hearing — not treat CRUD as foregone.

---

## Harness fidelity (OPS-05)

| Option | Description | Selected |
|--------|-------------|----------|
| Real NATS + 2 in-process replicas | Real NATS JetStream container (natstest) + Postgres testcontainer + two in-process CoreServer replicas sharing broker/DB (external-NATS integration tier) | ✓ |
| Full 2-process / docker-compose | Two real OS-process replicas via docker-compose; highest fidelity, slower/flakier | |
| Lighter simulated concurrency | Single process, forced concurrent writes, client-side simulated flap; cheapest, no true two-replica separation | |

**User's choice:** Real NATS + 2 in-process replicas
**Notes:** Matches `.claude/rules/testing.md` — multi-replica/external-broker behavior must use a real NATS container, not shared embedded `eventbustest`.

---

## Harness disposition

| Option | Description | Selected |
|--------|-------------|----------|
| In-tree, opt-in/nightly | Kept in-tree but excluded from gating CI; opt-in via quarantine mechanism (HOLOMUSH_RUN_QUARANTINED); documented, reproducible; standing MODEL-03 regression check | ✓ |
| Permanent gating test | Add to required CI integration suite; strongest guarantee, real flakiness risk | |
| Throwaway artifact | Run once, capture verdict + repro in ADR/evidence, remove harness | |

**User's choice:** In-tree, opt-in/nightly
**Notes:** CONCERNS.md flags two-replica chaos as CI-resource-sensitive (ConcurrentUp / holomush-pqzv quarantine entry). Planner to decide exact opt-in seam (dedicated env flag vs. quarantine marker + test/quarantine.yaml row).

---

## Corruption verdict scope

| Option | Description | Selected |
|--------|-------------|----------|
| Prove M12, characterize M2 | Empirically reproduce actual M12 last-write-wins corruption (or prove it can't occur); for M2, prove the broker-flap race window exists without forcing an observable lost move | ✓ |
| Empirically demonstrate both | Force an observable documented failure for BOTH M12 and M2 (a move that commits to DB but loses its notification on a flap) | |
| M12 only | Scope to M12; treat M2 as already-established by F1, defer empirical demonstration | |

**User's choice:** Prove M12, characterize M2
**Notes:** M2's mechanism is already well-established in the F1 archaeology (events are a post-commit notification, so a NATS blip loses the notification while the DB write persists).

---

## Claude's Discretion

- ADR file number/slug (next `docs/adr/NNNN-<slug>.md`).
- Which world entities the harness exercises (locations/exits/characters/objects) and the concrete concurrent-command pair used to trigger M12.
- Whether the ADR proposes an explicit (A)-vs-(B) decision framework / scoring rubric.

## Deferred Ideas

None — discussion stayed within phase scope.
