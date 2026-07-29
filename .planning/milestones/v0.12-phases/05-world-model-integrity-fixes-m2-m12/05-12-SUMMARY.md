---
phase: 05-world-model-integrity-fixes-m2-m12
plan: 12
subsystem: world-model / invariant registry — INV-WORLD scope + 4 bound invariants
tags: [invariant-registry, INV-WORLD, delta-parity, writer-boundary, atomic-feed, feed-order, census, MODEL-04]
requires: [05-05, 05-07, 05-09, 05-11, 05-15, 05-16]
provides:
  - "INV-WORLD registry scope + four binding:bound entries INV-WORLD-1..4 (numeric ids; ADR symbolic names as summary/legacy)"
  - "INV-WORLD-1=ATOMIC-FEED, INV-WORLD-2=DELTA-PARITY, INV-WORLD-3=FEED-ORDER, INV-WORLD-4=WRITER-BOUNDARY"
  - "internal/world/outbox/delta_parity_test.go — real-row integration proof that the manifest EQUALS the MutationDelta (INV-WORLD-2)"
  - "internal/world/outbox/writer_boundary_test.go — reader-view compile fence (INV-WORLD-4)"
  - "census out-of-service producer assertions (character-genesis 05-15 + character-reaping 05-16 emit declared kinds)"
affects: []
tech-stack:
  added: []
  patterns: [numeric-id-with-legacy-symbolic-alias, pending-scope-with-bound-entries, real-row-delta-parity-integration, go-ast-local-const-kind-resolution]
key-files:
  created:
    - internal/world/outbox/delta_parity_test.go
    - internal/world/outbox/writer_boundary_test.go
  modified:
    - docs/architecture/invariants.yaml
    - docs/architecture/invariants.md
    - internal/world/outbox/relay_test.go
    - internal/world/postgres/outbox_store_test.go
    - test/meta/world_sql_fence_test.go
    - test/meta/world_import_graph_test.go
    - test/meta/world_envelope_census_test.go
decisions:
  - "INV-WORLD scope registered as status:pending — internal/world/service.go + service_test.go carry pre-existing FOREIGN bare INV-1/INV-2 tokens (the holomush-72ou per-property-ABAC design) that the provenance residual-walk (bareInvRE \\bINV-\\d+\\b over a migrated scope's owned_paths) would misattribute to INV-WORLD. The four INV-WORLD-1..4 entries are nonetheless binding:bound (a scope's status is orthogonal to its entries' binding field; success criterion 4 requires the ENTRIES be bound, which they are)."
  - "Canonical NUMERIC ids INV-WORLD-1..4; the ADR/one-pager SYMBOLIC names carried as summary text + a legacy: alias (Codex finding 3: the //Verifies parser at invariant_registry_test.go:163 is `INV-[A-Z]+-\\d+` — a symbolic id would never bind). The shared parser/tooling was NOT modified."
  - "INV-WORLD-2 (DELTA-PARITY) binds to a REAL-ROW integration test that commits a location DELETE cascading its exits AND a bidirectional exit Create, then proves the emitted envelope manifest EQUALS the returned MutationDelta with before/after versions equal to the rows' actual version transition — presence-insufficient, so not the false-green class the invariant rule warns about."
  - "INV-WORLD-1 (ATOMIC-FEED) binds to the ALWAYS-RUN state+envelope atomicity test in internal/world/postgres/outbox_store_test.go (normal test:int lane, NOT the quarantine-gated resilience suite), covering rollback/commit/forced-outbox-failure — not an envelope-only rollback (round-3 MEDIUM)."
  - "INV-WORLD-4 (WRITER-BOUNDARY) asserted_by lists FOUR tests incl. the D-06 guest-reaper tombstone regression (05-16), so the guest FK-cascade deletion hole cannot regrow while the bound tests still pass (round-6 R6-4)."
  - "The census out-of-service producer assertions land in 05-12 (not 05-11) to avoid the wave-10 file race — 05-12 depends_on BOTH 05-15 and 05-16, so both internal/auth producer files are guaranteed present; a go/ast pass resolves each producer's LOCAL Kind const (internal/auth MUST NOT import internal/world/outbox) and asserts outbox.IsDeclared."
metrics:
  duration: ~14min
  tasks: 3
  files: 9
  completed: 2026-07-13
status: complete
---

# Phase 5 Plan 12: Register + Bind the Four INV-WORLD Invariants Summary

Registers the brand-new `INV-WORLD` invariant-registry scope and mints + BINDS the
four world-model integrity invariants the MODEL-01 ADR named — using the canonical
NUMERIC ids the registry `// Verifies:` parser recognizes, with the ADR's symbolic
names preserved as `summary` + `legacy` provenance. Each is `binding: bound` against
a genuinely-asserting test, satisfying ROADMAP success criterion 4 (the invariants
are bound, not pending).

**id ↔ name mapping:** INV-WORLD-1 = ATOMIC-FEED · INV-WORLD-2 = DELTA-PARITY ·
INV-WORLD-3 = FEED-ORDER · INV-WORLD-4 = WRITER-BOUNDARY.

## What was built

**Task 1 — register the scope + write the delta-parity + writer-boundary binding tests.**
Added the `INV-WORLD` scope to `docs/architecture/invariants.yaml` (origin: the ADR
`holomush-i4784` + the panel-ratified consensus one-pager; owned_paths over the world
subtrees) and four `- id: INV-WORLD-1..4` entries. The scope is `status: pending`
(not `migrated`) — a deliberate deviation (Rule 3): `internal/world/service.go` and
`service_test.go` carry pre-existing FOREIGN bare `INV-1`/`INV-2` tokens from the
`holomush-72ou` per-property-ABAC design, which the provenance residual-walk
(`bareInvRE = \bINV-\d+\b` over a migrated scope's owned files) would misattribute to
INV-WORLD. A pending scope skips that walk while still letting its entries be `bound`.
Wrote two binding tests: `delta_parity_test.go` (integration, real-row: a location
DELETE that DB-cascades its exits + a bidirectional exit Create → asserts the emitted
manifest EQUALS the returned `MutationDelta` and the rows' actual version transition),
and `writer_boundary_test.go` (reflection: `world.Service` holds only reader views, so
no envelope-less write is directly callable).

**Task 2 — annotate all binding sites, flip to bound, regenerate + confirm.**
Added numeric `// Verifies: INV-WORLD-N` annotations to each genuinely-asserting site:
INV-WORLD-1 → the always-run `TestOutboxStoreStateAndEnvelopeAtomicity`; INV-WORLD-3 →
the relay order + halt-on-poison + same-position-skip-marker (no-wire-gap) tests;
INV-WORLD-2 → the two delta-parity tests; INV-WORLD-4 → `writer_boundary_test.go` +
`test/meta/world_sql_fence_test.go` (AST SQL fence incl. entity_properties + migration
files) + `test/meta/world_import_graph_test.go` (composition allowlist) + the 05-16
`guest_reaper_tombstone_test.go` (D-06, whose annotation 05-16 already added). Flipped
all four entries to `binding: bound` with `asserted_by`, regenerated `invariants.md`,
and confirmed `TestEveryRegistryInvariantHasBinding` / `TestProvenanceGuard` /
`TestBoundInvariantsAreGenuinelyAsserted` all pass — proving the numeric ids actually
bind (a symbolic id would have silently failed here).

**Task 3 — extend the census with the out-of-service producer assertions.**
`test/meta/world_envelope_census_test.go` gains `TestWorldEnvelopeCensusOutOfServiceProducers`:
a go/ast pass over each sanctioned out-of-world producer resolves the LOCAL Kind const
it stamps (`internal/auth` MUST NOT import `internal/world/outbox`, so producers name
their kind via a local literal) and asserts it is a DECLARED taxonomy kind — the
character-genesis service (05-15 → `character_genesis`) and the character-reaping
service (05-16/D-06 → `character_deleted`, the SAME kind `DeleteCharacter` emits, a
sanctioned multi-producer). Moved here from 05-11 to avoid the wave-10 file race
(05-12 depends_on both creating plans); the 05-11 in-Service bijection is unchanged.

## Verification

- `task test -- -run 'TestEveryRegistryInvariantHasBinding|TestProvenanceGuard|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/` — green (7 tests).
- `task test -- ./test/meta/` — green (93 tests: registry + sql-fence + import-graph + census, incl. the new out-of-service producer subtests).
- `task test:int -- -run 'DeltaParity|GuestReaperTombstone|Census' ./internal/world/outbox/ ./test/integration/auth/ ./test/meta/` — green (9 tests; delta-parity real-row proof + the D-06 INV-WORLD-4 guest-deletion binding).
- `go run ./cmd/inv-render -check` — no stale diff (invariants.md regenerated).
- `grep -c 'INV-WORLD' docs/architecture/invariants.yaml` → 16 (≥4).
- `task lint` — exit 0 (incl. the invariants render-check). `task build:all` — exit 0.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] WORLD scope cannot be `status: migrated` — foreign residual tokens.**
- **Found during:** Task 1 (planning the owned_paths + scope status).
- **Issue:** the plan's literal instruction to give WORLD `owned_paths` over
  `internal/world/**` would, under `status: migrated`, make the provenance
  residual-walk (`checkProvenance` / `bareInvRE`) fail: `internal/world/service.go`
  and `service_test.go` carry pre-existing FOREIGN bare `INV-1`/`INV-2` tokens from
  the `holomush-72ou` per-property-ABAC design (unrelated to the world registry).
- **Fix:** registered the scope as `status: pending`. A pending scope declares its
  ownership + is partition-checked, but is skipped by the residual-walk and the refs
  ownership check. The four INV-WORLD-1..4 entries are still `binding: bound` — scope
  status is orthogonal to an entry's `binding` field, and ROADMAP success criterion 4
  requires the ENTRIES be bound (they are). Documented in the scope's `boundary` text.
- **Files:** docs/architecture/invariants.yaml.
- **Commit:** 4e8d2be48 (Task 1).

**2. [Rule 3 - Blocking] delta_parity integration test needs a TestMain in the outbox package.**
- **Found during:** Task 1 (making delta-parity a real-row "committed mutation" proof).
- **Issue:** `internal/world/outbox` had only in-memory unit tests (no testcontainer
  harness), but INV-WORLD-2 must prove the manifest equals the delta from ACTUAL rows.
- **Fix:** added a `//go:build integration` `TestMain` (Postgres testcontainer +
  migrator + pool, mirroring the postgres_test harness) inside `delta_parity_test.go`;
  it is compiled only under the integration build, so the package's unit tests are
  unaffected. `task test:int` runs `./...`, so the new integration test is picked up.
- **Files:** internal/world/outbox/delta_parity_test.go.
- **Commit:** 4e8d2be48 (Task 1).

No architectural changes, no auth gates, no checkpoints. The registry parser/tooling
was NOT modified (Codex finding 3 / prohibition).

### Scope / tracking notes

- **Requirement `MODEL-04` NOT marked complete here.** MODEL-04 spans
  05-09/05-10/05-11/05-15/05-16 and the phase-completion verifier; this plan lands the
  invariant registration + binding. Final marking is deferred to phase completion
  (mirrors 05-11/05-15/05-16).
- **Plan counter.** The sequential orchestrator owns wave ordering; the coarse
  Current-Plan counter may lead the actual 05-12 completion — noted for reconciliation
  (same caveat as 05-09/05-10/05-11).

## Known Stubs

None. All four INV-WORLD-N are `binding: bound` against genuinely-asserting tests; the
registry meta-tests are green; `invariants.md` is regenerated with no drift.

## Threat Flags

None. This plan adds registry entries + test files only — no new network endpoint,
auth path, or trust-boundary schema change. T-05-35 (fabricated binding) is mitigated
by `TestBoundInvariantsAreGenuinelyAsserted` + the presence-insufficient delta-parity
assertion; T-05-36 (stale invariants.md) by the regenerate + drift check.

## Self-Check: PASSED

- FOUND: internal/world/outbox/delta_parity_test.go, internal/world/outbox/writer_boundary_test.go
- FOUND commits: 4e8d2be48 (Task 1), 1ce2d60f1 (Task 2), 6c9f16f70 (Task 3)
- GREEN: registry meta-tests (7), ./test/meta (93), integration binding lane (9), task lint (0), task build:all (0), inv-render -check (0)
- VERIFIED: 4 INV-WORLD-N entries binding:bound with genuine // Verifies sites; invariants.md regenerated (no drift); census reaping/genesis producer assertions land here (05-12 depends_on 05-15+05-16)
