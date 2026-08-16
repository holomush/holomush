---
phase: 1
slug: portal-spec
# status lifecycle: draft (seeded by plan-phase) → validated (set by validate-phase §6)
# audit-milestone §5.5 distinguishes NOT-VALIDATED (draft) from PARTIAL (validated + nyquist_compliant: false) (#2117)
status: validated
nyquist_compliant: false
wave_0_complete: true
created: 2026-08-16
validated: 2026-08-16
---

# Phase 1 — Validation Strategy

> Reconstructed retroactively by `/gsd-validate-phase 1` at `e124617b4`. This
> phase shipped without a VALIDATION.md; the contract below is derived from its
> 6 PLAN/SUMMARY pairs, `01-VERIFICATION.md`, and `01-UAT.md`.

**This is a specification phase, and its validation contract says so.** Phase 1
produced no production code. Its deliverables are `01-SPEC.md` (16 sections,
~3600 lines), nine new `docs/architecture/invariants.yaml` entries (all shipped
`binding: pending`) plus the regenerated `invariants.md`, two `CLAUDE.md` pointer
edits, one `.claude/rules/invariants.md` edit, and superseded-annotations across
`ROADMAP.md`, `REQUIREMENTS.md`, and the research artifacts.

Most rows below are therefore **doc-assertions or manual review**, and several
SPEC contracts acquired real tests only *later*, in Phases 2/4/6. Those are
recorded as COVERED (downstream) with the binding test named. No test row here
was manufactured to fill the table.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` + `testify` for the one automated surface this phase owns (the invariant-registry meta-tests); `rg` doc-assertions for the rest |
| **Config file** | `Taskfile.yaml` — `test` (untagged lane), `lint:markdown` (`:782`), `lint:docs-symmetry` (`:836`), `lint:docs-paths-sync` (`:841`) |
| **Quick run command** | `task test -- ./test/meta/` |
| **Full suite command** | `task test` + `go run ./cmd/inv-render -check` |
| **Notable absence** | **No lane validates `01-SPEC.md` itself.** No meta-test walks `.planning/`, by design — §12.2 states that no meta-test asserts on planning-document markdown. |

---

## Sampling Rate

- **After every task commit:** `task test -- ./test/meta/` when the task touched
  `invariants.yaml`; `task lint` when it touched markdown.
- **Before `/gsd-verify-work`:** `go run ./cmd/inv-render` (regenerate) then
  `task test -- ./test/meta/`, so a hand-edited generated region fails loudly.
- **Max feedback latency:** seconds — this phase's automated surface is small.

---

## Per-Task Verification Map

| Requirement / Criterion | Task | Plan | Behavior | Test Type | Automated Command | Evidence File:Line | Verdict |
|---|---|---|---|---|---|---|---|
| PORTAL-05 (§7/§8 profile + visibility model) | T1–T3 | 01-01 | SPEC fixes the `profile.*` field set, the tier floor, and gallery slots `00..09` | doc-assertion | `rg -c 'profile\.rp_preferences' 01-SPEC.md` → 5 | `01-SPEC.md:1135,1138,1743,2191,2561` | MANUAL-BY-DESIGN |
| PORTAL-01/02 (§2/§3 audience matrix + census) | T1–T2 | 01-02 | 29 character-returning RPCs, one audience verdict each; the census is the sole gate | meta (bound in Phase 4) | `task test -- -run 'CharacterReturningRPCCensus' ./test/meta/` | `test/meta/character_rpc_census_test.go:309,350,376,404` | ✅ COVERED (downstream) |
| PORTAL-03/04 (§4/§5 lifecycle + name capture) | T1–T2 | 01-03 | Exhaustive status switch; retire preserves the name | integration (bound in Phase 3) | `task test:int -- ./test/integration/world/` | `test/integration/world/character_lifecycle_test.go:134` (INV-WORLD-5), `:213` (INV-WORLD-6) | ✅ COVERED (downstream) |
| PORTAL-07 (§6 two normalization policies) | T3 | 01-03 | Character-name and username normalization never share an implementation | doc-assertion | none | `01-SPEC.md:829-831` (found only with `rg -U` — the claim wraps a line) | MANUAL-BY-DESIGN |
| PORTAL-06 (§9 `expected_version`, CreateCharacter carve-out) | T1 | 01-04 | An int32 scalar per guarded request; create is excluded | doc-assertion | `rg -n 'expected_version' 01-SPEC.md` | `01-SPEC.md:2018,2059-2061,2098-2103,2217`; registry `invariants.yaml:5304-5318` (`binding: pending`) | MANUAL-BY-DESIGN |
| PORTAL-08 (§10 seven-section admin registry) | T2 | 01-04 | Exactly 7 ids; auth descriptor mandatory | unit (bound in Phase 6) | `task test -- ./internal/admin/section/` | `registry_test.go:44,123,249`; `descriptor_test.go:74` | ✅ COVERED (downstream) |
| PORTAL-08 (§10.5 admin gate is per-player) | T2 | 01-04 | A wire-shape commitment | human judgment | none | `01-UAT.md:15-19` — test 1, `result: pass`, `source: human` | MANUAL-BY-DESIGN |
| PORTAL-09 (§11 no sort/filter on profile fields) | T3 | 01-04 | Verdict "No" plus a bounded sort surface | doc-assertion | `rg -q 'MUST NOT'` over §11 | `01-SPEC.md:2671` | MANUAL-BY-DESIGN |
| PORTAL-10 (§12 six rules stated 1–6) | T1 | 01-05 | Rules numbered, each carrying a non-vacuity clause | doc-assertion | `rg -n '^### 12\.1' 01-SPEC.md` | `01-SPEC.md:2805-2830` | ✅ COVERED |
| **PORTAL-10 (§12.2 binding — verbatim copy into every v0.13 PLAN.md)** | T1 | 01-05 | Every downstream PLAN carries the rules block | none | `rg -l 'Census with set equality' .planning/phases/*/[0-9]*-PLAN.md` | Phase 02: **13/13**. Phases 03/04/05/06: **0 of 27** | ⚠️ **PARTIAL** — mechanism abandoned; see Audit |
| PORTAL-10 (§14 amendments applied, research annotated) | T2–T3 | 01-05 | Superseded strings dead in the planner artifacts | doc-assertion | `rg -U 'three\s+existing\s+public\s+export' .planning/ROADMAP.md .planning/REQUIREMENTS.md` → exit 1 | `REQUIREMENTS.md:31-34` now reads "**four**"; `research/SUMMARY.md` carries 4 `SUPERSEDED` blocks | ✅ COVERED |
| PORTAL-10 (§16 every `path:line` citation resolves) | T1 | 01-06 | 57 files resolved at the authoring commit | one-shot shell gate | **none in-tree** | 63 distinct cited files today; **1 unresolved** | ⚠️ **PARTIAL** — gate has rotted; see Audit |
| PORTAL-10 (D-19 pointer edit, both passages) | T2 | 01-06 | `CLAUDE.md` + the invariants rule updated | doc-assertion | `rg -c '\.planning/phases' CLAUDE.md` → ≥2 | `CLAUDE.md:22,52`; `.claude/rules/invariants.md:16,91` | ✅ COVERED |
| Registry hygiene (9 entries, pending, no `asserted_by`) | all | all | Schema, provenance, and binding guards hold | unit (meta) | `task test -- -run 'TestEveryRegistryInvariantHasBinding\|TestRegistryBindingChecks\|TestProvenanceGuard\|TestBoundInvariantsAreGenuinelyAsserted\|TestOwnedPathsPartition\|TestRegistrySchemaParsesOwnershipFields' ./test/meta/` + `go run ./cmd/inv-render -check` | `test/meta/invariant_registry_test.go:70,113,232,379,857,1128` | ✅ COVERED |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

None. This phase authored a specification and registry entries; the only
automated surface it touches (`test/meta/invariant_registry_test.go`,
`cmd/inv-render`) already existed. No framework install, no scaffolding.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions | Outcome |
|----------|-------------|------------|-------------------|---------|
| §10.5 per-player admin gate; §9 RPC-surface completeness | PORTAL-08, PORTAL-06 | Judgment calls a mechanical check cannot make — whether a wire shape is *right*, not whether it is *present* | Human review of the SPEC sections against the requirement text | ✅ **Discharged** — `01-UAT.md:15-26`, tests 1 and 2, both `source: human`, `result: pass`, with a recorded deferred follow-up at `:133-139` |
| §16 grounding-trace walkability | PORTAL-10 | Whether a citation *supports* its claim is a reading task, not a resolution task | Spot-check a sample of cited `path:line` anchors | ✅ **Discharged** — 3 of 189 citations spot-checked at `01-UAT.md:28-40`. See the Audit for the separate *resolution* rot. |
| All 20 declared prohibitions | PORTAL-10 | Negative verdicts ("the SPEC does not permit X") are checkable only by reading | Adjudicate each prohibition against the SPEC text | ✅ **Discharged** — `01-VERIFICATION.md:105-124` |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify, a doc-assertion, or a recorded manual discharge
- [x] Sampling continuity: not applicable — no code lane
- [x] Wave 0 covers all MISSING references (none owed)
- [x] No watch-mode flags
- [x] Registry entries shipped `pending` with no fabricated `asserted_by`
- [ ] `nyquist_compliant: true` set in frontmatter — **withheld**, see Audit

**Approval:** validated 2026-08-16 as **PARTIAL** by `/gsd-validate-phase 1`.

---

## Validation Audit 2026-08-16

| Metric | Count |
|--------|-------|
| Gaps found | 2 |
| Resolved | 0 |
| Escalated | 2 |

Reconstructed from artifacts — this phase had **no VALIDATION.md**. `task test`
PASS (11852 ok / 4 skipped) on this tree covers the registry meta-tests; the two
downstream bindings are green via CI `success` at `07ca74c46`.

**Escalated 1 — §12.2's binding mechanism was silently abandoned.** PORTAL-10
rule 6 does not merely state six rules; §12.2 specifies *how* they bind: a
**verbatim copy of the rules block into every v0.13 `PLAN.md`**. Phase 02 honored
it in **13 of 13** plans. Phases 03, 04, 05, and 06 carry it in **0 of 27**.

The rules' *substance* survived — those phases built real set-equality censuses
(`test/meta/characteraccess_routing_census_test.go:496,530,626,681`), which is
what the rules were for. But the mechanism Phase 1 mandated stopped being
followed after Phase 2 and nothing noticed, because **nothing watches**: §12.2
itself rules out a meta-test over planning markdown. A binding that depends on
every future author remembering it is a convention, not a binding. Recorded here
rather than filed, because the correct fix is a planning-process decision
(re-affirm the copy, or replace §12.2 with something enforceable), not a code
change.

**Escalated 2 — the §16 citation gate has already rotted, and nothing catches
it.** `01-SPEC.md` cites `internal/store/migrations/000001_baseline.up.sql`. That
file no longer exists: Phase **01.1**'s goose adoption renamed it to
`000001_baseline.sql`. 62 of 63 cited files still resolve. §16.8
(`01-SPEC.md:3307`) predicts exactly this decay and accepts it — which is why no
issue is filed — but the gate that proved the citations was a **one-shot shell
pipeline run during plan 01-06**, present in neither `scripts/` nor
`Taskfile.yaml`. Re-running it is manual forever, so the accepted decay is also
an unmeasured one.

**Registry state has moved past this phase's own report, in the right
direction.** `01-VERIFICATION.md:60` records 3 bound / 6 pending. Today **5 are
bound** (`INV-PRIVACY-9`, `INV-PRIVACY-11`, `INV-ACCESS-10`, `INV-WORLD-5`,
`INV-WORLD-6`) and 4 pending (`INV-PRIVACY-10`, `INV-ACCESS-11`, `INV-ACCESS-12`,
`INV-WORLD-7`). All five `asserted_by` files exist and carry genuine
`// Verifies:` annotations — **no fabricated bindings**, which is the failure
mode `.claude/rules/invariants.md` exists to prevent.

**Stale bookkeeping, already known.** `.planning/REQUIREMENTS.md:364-373` still
lists PORTAL-01..10 as `Pending` despite the phase passing. Flagged non-blocking
at `01-VERIFICATION.md:163`; it is the milestone-wide traceability defect tracked
upstream as #4974 / #4966, not a Phase 1 omission.
