# Phase 5: World-Model Integrity Fixes (M2 / M12) - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-12
**Phase:** 5-World-Model Integrity Fixes (M2 / M12)
**Areas discussed:** Emission scope, Conflict UX depth, WR-01 handling, Delivery/PR structure

> Note: the MODEL-01 ADR (`holomush-i4784`) + consensus one-pager are NORMATIVE — the
> mechanism was not re-opened. Discussion covered only the scope-boundary calls the ADR
> deferred to "Phase 5's spec."

---

## Emission scope (MODEL-04 slice 3)

| Option | Description | Selected |
|--------|-------------|----------|
| Full rollout (as one-pager) | Wire the outbox envelope through every world write command (~15-20 types) + registry + census meta-test | ✓ |
| MoveCharacter vertical + registry, defer bulk | Prove the mechanism on MoveCharacter, build registry/census scaffold, defer per-command rollout | |
| Let planner decide from ADR | Let census + write-requires-envelope seam force completeness | |

**User's choice:** Full rollout (as one-pager)
**Notes:** Matches NORMATIVE slice 3 literally; the write-requires-envelope seam + census meta-test structurally forbid leaving commands un-migrated. Largest slice — plan waves accordingly.

---

## Conflict UX depth (MODEL-03)

| Option | Description | Selected |
|--------|-------------|----------|
| Typed error at service boundary only | Return typed `WORLD_CONCURRENT_EDIT` from `world.Service`; telnet/web presentation is a separate slice | ✓ |
| Include telnet + web surfacing | Ship the full mapping table + user-visible affordances in Phase 5 | |
| Let planner decide from ADR | Defer the boundary call to planning | |

**User's choice:** Typed error at service boundary only
**Notes:** Phase 5 proves the integrity mechanic; presentation wiring deferred to a UX slice. Error-code registration stays in scope.

---

## WR-01 handling (pre-existing resilience test finding)

| Option | Description | Selected |
|--------|-------------|----------|
| Fold into slice 2 | Slice 2 deletes the emit path + rewrites m2_dualwrite to assert new outbox behavior; WR-01 dissolves with the code it described | ✓ |
| Standalone pre-Phase-5 fix | Correct the assertion + evidence doc as an isolated PR before Phase 5 | |
| Let planner decide from ADR | Note in CONTEXT; let planning sequence it | |

**User's choice:** Fold into slice 2
**Notes:** The post-commit emit path (`EmitMoveEvent`/`EVENT_EMITTER_MISSING`) is deleted in slice 2; the wrong-mechanism assertion (`EVENT_EMIT_FAILED` vs the real `EVENTBUS_PUBLISH_EXPIRED/FAILED` go-retry accident) disappears with it. Planner must also correct the M2 "Mechanism" paragraph in `f1-resilience-verdict.md`.

---

## Delivery / PR structure

| Option | Description | Selected |
|--------|-------------|----------|
| Slice-per-PR | Each slice its own reviewable PR | |
| One phase PR | Whole phase lands as one PR after all slices verify | ✓ |
| Let planner decide | Let plan-phase choose | |

**User's choice:** One phase PR
**Notes:** Slices remain the internal wave/commit ordering; no slice-per-PR ceremony. Crypto/abac gates apply to the whole diff at push (likely neither triggers — world-model + migrations).

## Claude's Discretion

- Internal wave decomposition within each slice, migration numbering, Go package placement of the outbox relay + mutation wrapper, and the reference-consumer shape.

## Deferred Ideas

- Conflict-surfacing UX slice (telnet message + web retry affordance for `WORLD_CONCURRENT_EDIT`).
- Compare-before-retry conflict semantics (telemetry-gated, later).
- Product feed consumers/projections (Phase 5 ships only the reference consumer).
- ARCH-04 unified event-model collapse (Phase 7).
- Real event sourcing / world-state rebuild (permanently forgone).
