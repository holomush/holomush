---
phase: 04-world-model-resilience-investigation-decision-f1
plan: 04
subsystem: docs
tags: [adr, world-model, event-sourcing, crud, decision-gate, model-01]

# Dependency graph
requires:
  - phase: 04-world-model-resilience-investigation-decision-f1
    provides: "f1-eventsourcing-why.md (F1 archaeology, drift-by-default) + f1-resilience-verdict.md (plan-03 OPS-05 evidence: M12 reproduced, M2 window characterized, no production emitter)"
provides:
  - "ACCEPTED MODEL-01 ADR at docs/adr/holomush-i4784-world-state-model-decision.md — Option B (CRUD-canonical + version guard + transactional outbox) in the panel-ratified STRENGTHENED shape"
  - "Phase 5 input contract named: MODEL-03 = version guard (writes AND deletes, locked-read zero-row classification, strict WORLD_CONCURRENT_EDIT surfacing, no auto-retry slice 1); MODEL-04 = transactional outbox / ordered atomic feed (one envelope per command in-transaction, locked-counter commit-ordered feed_position, single leased relay with LISTEN/NOTIFY + halt-and-alert, compile-time write-requires-envelope seam + census meta-test + lint fence)"
  - "NORMATIVE mechanism doc: docs/reviews/arch-review/2026-07-11/verification/model-01-consensus-onepager.md (unanimously panel-ratified)"
  - "INV-WORLD-ATOMIC-FEED / -DELTA-PARITY / -FEED-ORDER / -WRITER-BOUNDARY named in the ADR for Phase 5's spec to mint and bind"
affects: [Phase 5 (MODEL-02/03/04 implementation), Phase 7 (ARCH-04 — taxonomy schema registry designated the input)]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Three-model architecture panel (Antigravity/Gemini 3.1 Pro High, Codex/gpt-5.6-sol max, Fable 5) run in two rounds to ratify a mechanism shape before the human decision checkpoint — the one-pager becomes the NORMATIVE contract the ADR points to"

key-files:
  created:
    - .planning/phases/04-world-model-resilience-investigation-decision-f1/04-04-SUMMARY.md
  modified:
    - docs/adr/holomush-i4784-world-state-model-decision.md
    - docs/adr/README.md

key-decisions:
  - "DECIDED (Sean Brandt, blocking checkpoint): Option B — CRUD-canonical + optimistic concurrency + transactional outbox, in the panel-ratified strengthened shape; the consensus one-pager is NORMATIVE for the mechanism"
  - "Decision framing: future-state-first — blast radius explicitly NOT a driver; drivers were future state, best solution for the problem space, extensibility, flexibility, evolvability"
  - "The ordered complete world-change feed (not row-vs-event canonicality) is the platform's extensibility contract; evolvability inverts under event sourcing pre-1.0; scenes already provide the event-sourced timeline; coverage rot countered structurally (compile-time seam + census meta-test + delta-parity)"
  - "Option A weighed genuinely equally (D-01); Codex's Postgres-journal variant was the strongest A shape; Antigravity's JetStream-canonical variant rejected by the rest of the panel; A's unique payoffs (derivable state, time-travel, world-forking) consciously forgone"
  - "INV-WORLD-* invariants NAMED in the ADR but registration/binding deferred to Phase 5's spec per .claude/rules/invariants.md"

requirements-completed: [MODEL-01]

coverage:
  - id: D1
    description: "Committed ADR records the world-state model decision with both options weighed genuinely equally (D-01) and intent recorded (D-02)"
    requirement: MODEL-01
    verification:
      - kind: other
        ref: "docs/adr/holomush-i4784-world-state-model-decision.md — Status: Accepted; Alternatives Considered carries all 9 Option-A and 4 Option-B verified cost points; Intent section records drift-by-default / intent-unprovable-from-git; task lint:adr + rumdl exit 0"
        status: pass
    human_judgment: false
  - id: D2
    description: "The ADR names the CONCRETE mechanism Phase 5 (MODEL-03/MODEL-04) implements (phase success criterion #4)"
    requirement: MODEL-01
    verification:
      - kind: other
        ref: "ADR Decision section: MODEL-03 version guard + MODEL-04 outbox/ordered atomic feed per the NORMATIVE one-pager; rg 'MODEL-03|MODEL-04' yields 6 matches"
        status: pass
    human_judgment: false
  - id: D3
    description: "The decision was made by the human decider at a blocking checkpoint, not by the executor"
    requirement: MODEL-01
    verification:
      - kind: other
        ref: "Task 2 checkpoint resolved by Sean Brandt ('I agree with the document and the approach' — Option B ratified shape); recorded in ADR Rationale with the future-state framing and panel provenance"
        status: pass
    human_judgment: true
  - id: D4
    description: "ADR indexed and cross-linked from #4784; authorization placement recorded (threat T-04-04 mitigated)"
    requirement: MODEL-01
    verification:
      - kind: other
        ref: "docs/adr/README.md index row (holomush-i4784); #4784 comment https://github.com/holomush/holomush/issues/4784#issuecomment-4948823040; ADR Consequences: checkAccess stays pre-write, no relay/consumer bypasses ABAC"
        status: pass
    human_judgment: false

# Metrics
duration: ~90min (including panel rounds + human checkpoint)
completed: 2026-07-11
status: complete
---

# Phase 04 Plan 04: MODEL-01 World-State Model ADR Summary

**MODEL-01 DECIDED and Accepted: HoloMUSH world state stays CRUD-canonical in PostgreSQL, protected by an optimistic-concurrency version guard (MODEL-03) and publishing an ordered, complete, schema-governed world-change feed via a transactional outbox (MODEL-04) — Option B in the panel-ratified strengthened shape, with the consensus one-pager NORMATIVE for the mechanism.**

## Performance

- **Duration:** ~90 min (draft → panel rounds 1+2 → human checkpoint → finalization)
- **Completed:** 2026-07-11
- **Tasks:** 3 (draft / blocking human decision / finalize)
- **Files modified:** 2 (ADR finalized, README indexed) + 3 panel artifacts committed during the checkpoint

## The Decision (verbatim contract for Phase 5)

**Chosen option:** Option B — CRUD-canonical + optimistic concurrency + transactional outbox, in the **panel-ratified strengthened shape** defined by [`model-01-consensus-onepager.md`](../../../docs/reviews/arch-review/2026-07-11/verification/model-01-consensus-onepager.md) (NORMATIVE). The feed — not the rows — is the platform's extensibility contract. Event sourcing is NOT adopted for world state; the per-aggregate dial stays open for genuinely log-native future domains.

**Phase 5 implements** (per the one-pager's four-step slice ordering):

- **MODEL-03 — the version guard.** `version INTEGER NOT NULL DEFAULT 1` on all four world tables; version-predicated CAS **writes AND deletes** (`UPDATE`/`DELETE … WHERE id = $1 AND version = $2`); any zero-row result classified by a **locked follow-up read in the same transaction** (conflict vs concurrent-delete vs not-found — deterministic, no diagnosis race); conflicts surfaced strictly as typed `WORLD_CONCURRENT_EDIT` with **no automatic retry in the first slice**.
- **MODEL-04 — the transactional outbox / ordered atomic feed.** One envelope per successful, externally visible world command, committed **in the same transaction** as the state change; **intent-level, new-values-only payloads**; commit-ordered gap-free `feed_position` allocated from a locked per-game counter; a **single leased relay** publishing strictly in position order (`Nats-Msg-Id` = event ULID dedup) with `LISTEN/NOTIFY` wakeup and a halt-and-alert poison posture; a **compile-time write-requires-envelope seam** (`mutate(ctx, entity, expectedVersion, envelope)` — an envelope-less write is a type error) backed by a census meta-test and a lint fence; snapshot + watermark bootstrap, genesis emission at cutover, and a feed epoch/reset procedure.

**Slice order:** (1) version columns + guarded repos + strict conflict surfacing (M12 specs flip to assert surfaced conflicts); (2) outbox + envelope + positioned relay, `MoveCharacter` end-to-end + reference idempotent consumer + bootstrap harness + relay lag/halt alerting — zero product projections in Phase 5; (3) taxonomy schema registry + census meta-test + invariant registration, then mechanical emission rollout + genesis emission; (4) MODEL-02 doc downgrade (~6 sites).

## Decision provenance

- **Decider:** Sean Brandt, at the Task 2 blocking checkpoint — "I agree with the document and the approach."
- **Framing:** future-state-first at the decider's direction — blast radius of change explicitly NOT a driver; drivers were future state, best solution for the problem space, extensibility, flexibility, evolvability.
- **Panel:** Antigravity (Gemini 3.1 Pro High), Codex (gpt-5.6-sol max), Fable (Fable 5); two rounds; unanimous ratification of the strengthened B shape (pass 1: Antigravity AGREE, Fable AGREE + 2 notes folded in, Codex OBJECT with 2 blocking edits; pass 2 after amendments: all AGREE). Trail: `model-01-panel-opinions.md` (round 1, commit `53f6359e9`), `model-01-panel-round2.md` + `model-01-consensus-onepager.md` (round 2 + ratification, commit `41ced0df1`).
- **D-01 honored:** Option A got a fully costed equal hearing (all nine verified cost points); the Postgres-journal variant (Codex, round 1) was the strongest A shape; Antigravity's JetStream-canonical A variant was rejected by the rest of the panel. A's unique payoffs (byte-derivable state, time-travel, world-forking) were consciously forgone as low-value for the world-state domain.
- **Key panel reasoning adopted in the Rationale:** the complete ordered feed is the real extensibility contract; evolvability inverts under event sourcing pre-1.0 (schemas become replay-load-bearing forever); scenes already provide the platform's event-sourced timeline; coverage rot is countered structurally (compile-time seam + census meta-test + delta-parity), not by discipline.

## Artifacts

- **ADR (Accepted):** `docs/adr/holomush-i4784-world-state-model-decision.md` — decision token `holomush-i4784`
- **Index row:** `docs/adr/README.md`
- **#4784 comment:** <https://github.com/holomush/holomush/issues/4784#issuecomment-4948823040> (issue left OPEN — Phase 5 implements against this contract)
- **NORMATIVE mechanism doc:** `docs/reviews/arch-review/2026-07-11/verification/model-01-consensus-onepager.md`
- **Panel trail:** `model-01-panel-opinions.md`, `model-01-panel-round2.md` (same directory)

## Task Commits

1. **Task 1: Draft Proposed ADR (options costed equally, D-01/D-02)** - `4888e3d01` (docs)
2. **Checkpoint support: round-1 panel opinions** - `53f6359e9` (docs, committed by orchestrator during checkpoint)
3. **Checkpoint support: round-2 panel + ratified consensus one-pager** - `41ced0df1` (docs, committed by orchestrator during checkpoint)
4. **Task 2: Human decision checkpoint** - no commit (resolved by decider: Option B ratified shape)
5. **Task 3: Finalize ADR (Accepted) + README index + #4784 comment** - `ac67ebfc2` (docs)

## Decisions Made

- Final ADR title names the chosen model: "World-State Model: CRUD-Canonical with Version Guard and Ordered Atomic Feed" (filename unchanged per plan).
- The consensus one-pager is pointed to as NORMATIVE rather than duplicating the full mechanism into the ADR — the ADR carries the decision, rationale, and consequences; the one-pager carries the ratified mechanics.
- INV-WORLD-ATOMIC-FEED / -DELTA-PARITY / -FEED-ORDER / -WRITER-BOUNDARY are NAMED in the ADR Consequences but NOT registered — Phase 5's spec mints and binds them per `.claude/rules/invariants.md` (plan key_link honored).
- #4784 left open: the phase SUMMARY/verifier flow owns lifecycle; Phase 5 implements.

## Deviations from Plan

**1. [Expansion, decider-directed] Decision checkpoint ran a three-model panel before resolution**

- **Found during:** Task 2 (checkpoint)
- **Change:** the orchestrator convened a two-round, three-model panel and committed three decision-input artifacts (`53f6359e9`, `41ced0df1`) during the checkpoint; the decider then chose the panel-ratified STRENGTHENED B shape rather than plain plan-drafted Option B
- **Effect on Task 3:** the Decision section points to the one-pager as NORMATIVE and the Rationale records the panel provenance — richer than the plan's minimal option-b wording, fully consistent with the plan's acceptance criteria
- **Files modified:** docs/adr/holomush-i4784-world-state-model-decision.md
- **Commit:** `ac67ebfc2`

No other deviations — Tasks 1 and 3 executed as planned.

## Known Stubs

None — documentation-only plan; no code paths touched.

## Threat Flags

None — T-04-04 mitigated as planned (ADR Consequences records that `world.Service` `checkAccess` stays pre-write and no outbox relay or feed consumer bypasses ABAC); T-04-07 mitigated (decision made by the human decider at the blocking checkpoint).

## Next Phase Readiness

Phase 5 (MODEL-02/03/04) has its full input contract: the Accepted ADR + the NORMATIVE one-pager + the resilience suite as the permanent regression gate (D-05). Phase 7 (ARCH-04) is coordinated: the taxonomy schema registry is designated its input.

## Self-Check: PASSED

All claimed files exist on disk; all four commit hashes (`4888e3d01`, `53f6359e9`, `41ced0df1`, `ac67ebfc2`) verified in git log.
