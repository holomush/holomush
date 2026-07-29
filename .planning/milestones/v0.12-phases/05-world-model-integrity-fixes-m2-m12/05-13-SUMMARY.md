---
phase: 05-world-model-integrity-fixes-m2-m12
plan: 13
subsystem: docs
tags: [docs, model-02, event-sourcing, meta-test, world-model]
requires:
  - "ADR holomush-i4784 (decided world-state model)"
provides:
  - "corrected doc sites describing the decided model (event-driven + append-only audit log)"
  - "regression-guard meta-test against re-introducing the false replay-derived-world-state claim"
affects:
  - CLAUDE.md
  - AGENTS.md (via symlink)
  - README.md
  - site/src/content/docs/contributing/reference/coding-standards.md
  - site/src/content/docs/contributing/explanation/architecture.md
tech-stack:
  added: []
  patterns:
    - "file-read + regex-assert meta-test (mirrors test/meta/depguard_config_test.go)"
key-files:
  created:
    - test/meta/world_model_doc_claim_test.go
  modified:
    - CLAUDE.md
    - README.md
    - site/src/content/docs/contributing/reference/coding-standards.md
    - site/src/content/docs/contributing/explanation/architecture.md
decisions:
  - "index.mdx:41 ('Replay what happened, audit the history, debug problems') is legitimate audit-log language matching the decided model — PRESERVED (Open Question 4 resolved: no .mdx site carries the false claim)."
  - "README.md:22 'session persistence and event replay' PRESERVED (Codex finding 17 — reconnect/catch-up language, not world-state reconstruction)."
  - "Corrected doc text carries NO 'replay'/'derived from event'/'event sourcing' phrasing (positive model only + ADR reference) so the whole-file meta-test scan is a strict guard without over-matching the real client-catch-up language."
metrics:
  duration: ~20m
  completed: 2026-07-13
  tasks: 2
  files: 5
status: complete
---

# Phase 5 Plan 13: MODEL-02 Doc Downgrade + Regression Guard Summary

Downgraded the false "event sourcing / current state derives from replay" principle
at its four documentation sites to the decided model — **event-driven with an
append-only audit log** (world state canonical in PostgreSQL) — and added a doc-claim
meta-test so the corrected claim cannot silently regress.

## What Was Built

- **Task 1 — doc corrections (4 sites):**
  - `CLAUDE.md:274` — the "Event sourcing … state derives from replay" bullet rewritten to
    "Event-driven with an append-only audit log … world state is canonical in PostgreSQL",
    referencing `docs/adr/holomush-i4784-world-state-model-decision.md`. Propagates to
    `AGENTS.md` via the relative symlink (verified intact; `task lint:docs-symmetry` green).
  - `README.md:18` — "event-sourced architecture" → "event-driven architecture with an
    append-only audit log". `README.md:22` ("session persistence and event replay")
    PRESERVED per Codex finding 17.
  - `coding-standards.md` — `### Event Sourcing` section → `### Event-Driven Architecture`;
    "State is derived from event replay" replaced with the canonical-PostgreSQL + audit-log
    statement + ADR reference.
  - `architecture.md` — three spots: the "Persistence — All actions are stored and
    replayable" list item → "recorded in an append-only audit log"; the tech-stack table
    row "Append-only, replayable" → "Append-only audit/notification log"; the
    `### Event Sourcing` design principle → `### Event-Driven Architecture` with
    canonical-DB + ADR reference. Line 74 ("Replay — Reconnecting clients catch up from
    their last seen event") left INTACT.
- **Task 2 — regression guard:** `test/meta/world_model_doc_claim_test.go` (3 tests):
  - `TestWorldModelDocsDoNotClaimReplayDerivedState` — a SET of semantic patterns per
    section, not one exact old sentence.
  - `TestWorldModelDocsStateDecidedModel` — corrected sites contain the decided-model
    markers ("append-only"; architectural sites also carry the ADR id `holomush-i4784`).
  - `TestLegitimateClientCatchupReplayIsPreserved` — pins the real client-catch-up /
    Subscribe replay language so a mechanical "delete every 'replay'" future edit is caught.
  - TDD: verified RED against the pre-correction docs (2 failures — false-claim pattern
    matched CLAUDE.md, "append-only" missing), GREEN after the fix.

## Verification

- `task fmt` — clean, no mutations (edits already well-formed); committed state has no
  uncommitted fmt output.
- `task lint:markdown` — Success, no issues (741 + 84 files).
- `task lint:docs-symmetry` — AGENTS.md → CLAUDE.md symlink intact.
- `task test -- ./test/meta/` — 96 tests pass (includes the 3 new doc-claim tests).
- Re-grep of `derives from replay` / `event.sourc` over the four guarded sites — CLEAN;
  the preserved real replay language (`README.md:22`, `architecture.md:74`) confirmed present.

## Deviations from Plan

None. Plan executed as written. TDD ordering applied (meta-test authored + proven RED
before the doc fix; committed docs-then-test so every commit's `task test` is green).

## Known Stubs

None.

## Self-Check: PASSED

- `test/meta/world_model_doc_claim_test.go` — FOUND
- Modified doc sites — FOUND (CLAUDE.md, README.md, coding-standards.md, architecture.md)
- Commits `99fe9ca31` (docs), `dc395f98c` (test) — present on branch
