---
phase: 05-world-model-integrity-fixes-m2-m12
verified: 2026-07-13T16:11:03Z
status: passed
score: 4/4 must-haves verified
behavior_unverified: 0
overrides_applied: 0
re_verification:
  previous_status: none
requirements:
  - id: MODEL-02
    status: satisfied
    plans: ["05-13"]
  - id: MODEL-03
    status: satisfied
    plans: ["05-01", "05-02", "05-03", "05-04", "05-09", "05-14"]
  - id: MODEL-04
    status: satisfied
    plans: ["05-05", "05-06", "05-07", "05-08", "05-09", "05-10", "05-11", "05-12", "05-14", "05-15", "05-16"]
---

# Phase 5: World-Model Integrity Fixes (M2 / M12) Verification Report

**Phase Goal:** Implement the ADR's chosen mechanism to close last-write-wins and dual-write non-atomicity, and correct the event-sourcing docs.
**Verified:** 2026-07-13T16:11:03Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths (Success Criteria)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | Concurrent writers to the same world entity cannot silently lose an update — a version-guard conflict is detected and surfaced (M12 harness passes). [#4798] | ✓ VERIFIED | Version-predicated CAS in all four repos (`location/exit/character/object_repo.go` — `WHERE ... AND version=$N`, `version = version + 1`, `RETURNING version`); zero-row results classified via `classifyCASZeroRow` (`internal/world/postgres/helpers.go:81`) into `WORLD_CONCURRENT_EDIT` vs `*_NOT_FOUND`. Typed signal `world.ErrConcurrentEdit` / `world.CodeConcurrentEdit = "WORLD_CONCURRENT_EDIT"` (`internal/world/errors.go:16-26`). M12 spec (`test/integration/resilience/m12_lastwritewins_test.go`) genuinely asserts the surfaced code: deterministic interleave (`Expect(oopsErr.Code()).To(Equal(world.CodeConcurrentEdit))` L161), concurrent-describe zero-silent-overwrite, cross-field race, and per-aggregate-object (L375). D-05 suite is env-gated (`HOLOMUSH_RUN_QUARANTINED=1`); confirmed green upstream. |
| 2 | A world mutation and its event emission are atomic or reconciled: a NATS blip after commit cannot lose the notification (M2 closed). | ✓ VERIFIED | Transactional outbox: `OutboxStore.WriteIntent` (`internal/world/postgres/outbox_store.go:49`) inserts the finalized envelope through the ctx-bound execer (same transaction as the state write, not a second broker publish). Leased single relay (`internal/world/outbox/relay.go`) publishes strictly in `feed_position` order, `Nats-Msg-Id` = event ULID for dedup, poison-halt posture. Post-commit emit path DELETED: `internal/world/events.go` absent; zero `EmitMoveEvent`/`EVENT_EMITTER_MISSING`/`EventEmitter` residue in `internal/world/` prod. `m2_dualwrite_test.go` asserts control (exactly one envelope committed atomically, L124), flap-window (frozen broker → move succeeds + envelope committed in same tx, no `move_succeeded=true` emit failure, L152-157), no-orphan (1:1 envelope↔row, L184), and relay redelivery. `f1-resilience-verdict.md` M2 "Mechanism" paragraph corrected per D-03 (L39-48, L133-149). |
| 3 | Every doc site stating the false "state derives from replay" principle now describes the decided model; no doc claims replay-derived world state the code does not provide. | ✓ VERIFIED | Zero residual "state derives from replay" / "event sourcing" false claims in `CLAUDE.md`, `README.md`, `site/src/content/docs/contributing/`. Guard test `test/meta/world_model_doc_claim_test.go` forbids replay-derived-world-state phrasing via 4 regexes across guarded sites (CLAUDE.md, README.md, coding-standards.md, architecture.md) while positively preserving legitimate reconnect/catch-up replay language (Codex finding 17, L127-132). Meta-test green. |
| 4 | The relevant INV-WORLD-* invariants for the new guard/outbox are BOUND (not left `pending`). | ✓ VERIFIED | INV-WORLD-1..4 in `docs/architecture/invariants.yaml` all `binding: bound` with populated `asserted_by`. Bound by genuine `// Verifies:`-annotated tests: `outbox_store_test.go` (INV-WORLD-1, 30 assertions), `delta_parity_test.go` (INV-WORLD-2, 31), `relay_test.go` (INV-WORLD-3, 40), `writer_boundary_test.go` + `world_sql_fence_test.go` + `world_import_graph_test.go` + `guest_reaper_tombstone_test.go` (INV-WORLD-4). Registry meta-tests `TestEveryRegistryInvariantHasBinding`, `TestProvenanceGuard`, `TestBoundInvariantsAreGenuinelyAsserted` green (no Skip-only placeholders). |

**Score:** 4/4 truths verified (0 present, behavior-unverified)

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/world/errors.go` | Typed `WORLD_CONCURRENT_EDIT` | ✓ VERIFIED | `ErrConcurrentEdit` + `CodeConcurrentEdit` const |
| `internal/world/postgres/{location,exit,character,object}_repo.go` | Version-CAS + classifier | ✓ VERIFIED | All four carry versioned UPDATE/DELETE + `classifyCASZeroRow` |
| `internal/world/postgres/outbox_store.go` | Same-tx `WriteIntent` | ✓ VERIFIED | Inserts via ctx execer; per-game locked counter allocate |
| `internal/world/outbox/relay.go` | Leased ordered relay | ✓ VERIFIED | Fenced Lease, position order, Nats-Msg-Id dedup, poison halt |
| `internal/world/events.go` | DELETED | ✓ VERIFIED | Absent; emit path fully removed from prod |
| `test/integration/resilience/m12_lastwritewins_test.go` | Flipped to assert conflict | ✓ VERIFIED | Asserts `WORLD_CONCURRENT_EDIT` surfaced, no silent revert |
| `test/integration/resilience/m2_dualwrite_test.go` + `outbox_faultinjection_test.go` | Assert outbox atomicity | ✓ VERIFIED | Both present; atomic + fault-injection assertions |
| `docs/architecture/invariants.yaml` (INV-WORLD-1..4) | bound + asserted_by | ✓ VERIFIED | All four bound, real test files |
| `test/meta/world_model_doc_claim_test.go` | Doc-claim guard | ✓ VERIFIED | Forbids false phrasing, preserves legit replay language |
| `test/meta/world_envelope_census_test.go` | Write-command↔kind bijection (D-01) | ✓ VERIFIED | Bijection, no pending allow-list |

### Key Link Verification

| From | To | Via | Status |
|------|-----|-----|--------|
| Guarded repo write | `WORLD_CONCURRENT_EDIT` | `classifyCASZeroRow` locked follow-up read | ✓ WIRED |
| `world.Service` mutation | `outbox` row | `OutboxStore.WriteIntent` in caller tx | ✓ WIRED |
| `outbox` rows | NATS | leased `Relay` position-ordered publish | ✓ WIRED |
| INV-WORLD-1..4 | tests | `// Verifies:` + `asserted_by` | ✓ WIRED |

### Requirements Coverage

| Requirement | Source Plans | Status | Evidence |
|-------------|-------------|--------|----------|
| MODEL-02 (doc downgrade) | 05-13 | ✓ SATISFIED | Guarded doc sites corrected; guard test green |
| MODEL-03 (version guard / M12) | 05-01/02/03/04/09/14 | ✓ SATISFIED | CAS + typed conflict + flipped M12 spec |
| MODEL-04 (outbox / M2) | 05-05..12/14/15/16 | ✓ SATISFIED | Same-tx outbox + leased relay; emit path deleted |

All three requirement IDs from the plan frontmatter are accounted for and marked Complete in REQUIREMENTS.md.

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Invariant registry bindings genuine | `task test -- -run 'TestEveryRegistryInvariantHasBinding\|TestProvenanceGuard\|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/` | 7 tests, exit 0 | ✓ PASS |
| Doc-claim + census + fence + import-graph guards | `task test -- -run 'TestWorldModel\|TestDoc\|Census\|Fence\|ImportGraph' ./test/meta/ ./internal/world/...` | 20 tests, exit 0 | ✓ PASS |

Full `task test` (10220 passed) and `task test:int` were confirmed green upstream per the phase execution; the Docker/external-NATS-gated M12/M2 integration suites were not independently re-run here (env-gated), but their assertions were read and confirmed to genuinely exercise the claimed behavior.

### Anti-Patterns Found

None. No `TODO`/`FIXME`/`XXX`/placeholder markers in the verified world-model surface; no stub returns; the `events.go` emit path was removed rather than stubbed.

### Human Verification Required

None.

### Gaps Summary

No gaps. All four success criteria are satisfied by real, wired code with genuine behavioral assertions. The version guard surfaces `WORLD_CONCURRENT_EDIT` across all four aggregates; the transactional outbox commits state+envelope atomically with the post-commit emit path deleted; the false replay-derived-world-state doc principle is downgraded and fenced by a guard test; and INV-WORLD-1..4 are bound (not pending) with genuine asserting tests confirmed by the registry meta-tests.

Minor observations (non-blocking): the doc-claim guard covers 4 sites (CLAUDE.md, README.md, coding-standards.md, architecture.md) rather than the "~6" estimated in CONTEXT — this reflects the actual enumerated set of sites that carried the false claim, not a scope reduction. INV-WORLD-4's `asserted_by` lists `writer_boundary_test.go` while the multi-clause invariant is additionally proven by `world_sql_fence_test.go`, `world_import_graph_test.go`, and `guest_reaper_tombstone_test.go` (all carry `// Verifies: INV-WORLD-4`); the binding is genuine and complete.

---

_Verified: 2026-07-13T16:11:03Z_
_Verifier: Claude (gsd-verifier)_
