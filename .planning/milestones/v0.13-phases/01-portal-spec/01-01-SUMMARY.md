---
phase: 01-portal-spec
plan: 01
subsystem: spec
tags: [spec, abac, privacy, invariant-registry, entity-properties, profile]

requires:
  - phase: 01-portal-spec (discussion)
    provides: D-08..D-19 locked decisions in 01-CONTEXT.md; document analogs in 01-PATTERNS.md
provides:
  - ".planning/phases/01-portal-spec/01-SPEC.md — the v0.13 portal SPEC at the GSD convention location, with front matter, the RFC2119 declaration, and the complete 16-section heading skeleton"
  - "SPEC section 1 (Overview), section 7 (Profile and Media Data Model) and section 8 (Profile Visibility Model) authored to completion"
  - "SPEC section 13 opened with this slice's four invariant declarations"
  - "Four registry entries: INV-PRIVACY-9, INV-PRIVACY-10, INV-ACCESS-10, INV-ACCESS-11 — all binding: pending"
  - "Proof that docs/architecture/invariants.yaml accepts a .planning/ origin_spec — no docs/ stub is needed for the rest of v0.13"
  - "An amended INV-PRIVACY scope boundary that admits non-history read gating"
affects: [01-02, 01-03, 01-04, 01-05, 01-06, phase-2-abac-schema, phase-4-facade, phase-5-profile-ui]

actuals:
  tokens: 9972
  tasks: 3
  commits: 3

tech-stack:
  added: []
  patterns:
    - "GSD-convention SPEC location (.planning/phases/<phase>/<NN>-SPEC.md) registered as an invariant origin_spec"
    - "Viewer-tier floor as an ABAC policy family extending seed:property-*, evaluated at read time"

key-files:
  created:
    - .planning/phases/01-portal-spec/01-SPEC.md
  modified:
    - docs/architecture/invariants.yaml
    - docs/architecture/invariants.md

key-decisions:
  - "Task 1 checkpoint resolved as option-c: the four guarantees split by what each one is — disclosure to INV-PRIVACY, ABAC evaluation and its wire-shape consequence to INV-ACCESS"
  - "INV-PRIVACY boundary first sentence widened from 'gating on history reads' to 'gating on reads'; the 'Does NOT include: ABAC policy evaluation (-> INV-ACCESS)' clause preserved verbatim"
  - "All four invariants ship binding: pending with no asserted_by; asserting tests land in Phase 4"
  - "Section 8 carries a totality rule: any profile.* attribute without an explicit floor defaults to guest, never anonymous"
  - "profile.rp_preferences is named with an rp_ qualifier to avoid collision with the shipped characters.preferences settings column"

patterns-established:
  - "Posture table: one table whose rows are governed attributes and whose columns are worked game postures plus the seeded default"
  - "Recorded-divergence table (source text / SPEC / rationale) for a default that deliberately departs from a stated principle"

requirements-completed: [PORTAL-05]

duration: 20min
completed: 2026-08-01
status: complete
---

# Phase 1 Plan 01: Portal SPEC Tracer Summary

**The v0.13 portal SPEC now exists with its full section skeleton, and the document-to-registry path is proven end to end on the profile visibility slice — including the open question of whether a `.planning/` `origin_spec` survives the registry meta-tests. It does.**

## Performance

- **Duration:** ~20 min
- **Tasks:** 3/3
- **Commits:** 3

## Accomplishments

1. **Resolved the Task 1 checkpoint (option-c)** and landed its one registry consequence — the `INV-PRIVACY` boundary amendment — as its own commit, before any `INV-` id was written anywhere.
2. **Created `01-SPEC.md`** with bulleted front matter, the RFC2119 declaration, no SPDX header (matching the local `.planning/` convention), and all sixteen `## N. Title` headings in order. Sections this plan does not author carry a one-line placeholder naming the plan that fills them.
3. **Authored section 8 (Profile Visibility Model)** to completion in RFC2119 language, covering all nine required clauses (a)–(i), a posture table expressing three worked games plus the seeded v0.13 default, the recorded grid-parity divergence, and the "notably absent" statements for both the missing owner toggle and the missing editing surface.
4. **Authored section 7 (Profile and Media Data Model)** as a field table: the committed `entity_properties` shape, the complete twelve-field enumeration with the count stated in prose, the `00`–`09` media naming with database-enforced exactly-one-primary, D-08's always-public in-world description, and the empty-profile behavior.
5. **Registered four invariants** and regenerated `docs/architecture/invariants.md` from the YAML.

## Task 1 — checkpoint resolution (recorded by option id)

**Resolution: `option-c`** — split the four guarantees by what each one actually is.

| Guarantee | Scope | Id |
|---|---|---|
| Profile-reachability opacity (existence non-disclosure) | `INV-PRIVACY` | `INV-PRIVACY-9` |
| Name/pronouns hard floor (D-13) | `INV-PRIVACY` | `INV-PRIVACY-10` |
| Read-time tier-floor evaluation, deny on infra failure (D-10/D-11/D-12) | `INV-ACCESS` | `INV-ACCESS-10` |
| Per-field absence in the marshaled response (D-01) | `INV-ACCESS` | `INV-ACCESS-11` |

The `INV-PRIVACY` `boundary:` first sentence was amended from *"Privacy-relevant gating on history reads."* to *"Privacy-relevant gating on reads."* The `"Does NOT include: ABAC policy evaluation (→ INV-ACCESS), subscribe authorization (→ INV-EVENTBUS)"` clause was **kept verbatim and intact** — it is exactly what routes the tier-floor evaluation to `ACCESS`, and is why option-c is coherent rather than a fudge.

## Amendment queued for plan 01-05 — this is a FIFTH amendment, not a fourth

`01-CONTEXT.md:197-202` drafts **four** amendments (ROADMAP Phase 4 criterion 3, ROADMAP Phase 5 criterion 4, REQUIREMENTS PROFILE-12, research SUMMARY CONFLICT 4). This plan added a **fifth**. Plan 01-05 MUST carry all five; the count is no longer four.

| Artifact | Amendment |
|---|---|
| `docs/architecture/invariants.yaml` — the `INV-PRIVACY` scope record | `boundary:` first sentence widened from *"Privacy-relevant gating on history reads."* to *"Privacy-relevant gating on reads."* The exclusion clause is preserved verbatim and **MUST NOT** be widened. The `description:` enumeration was extended in the same edit so the scope record describes the entry family it now owns. Landed by plan 01-01 (commit `31176a623`). |

The row is also pre-written into `01-SPEC.md` §14 under a clearly-marked "Queued for plan 01-05 — FIVE amendments, not four" block, so 01-05 picks it up from the document rather than from this SUMMARY alone.

## Tracer result — the open question is answered

The plan flagged one unknown: whether the registry tolerates an `origin_spec` pointing at `.planning/` rather than `docs/`. **It does.** `go run ./cmd/inv-render -check` exits 0 and all five named meta-tests pass with four such entries in place. No `docs/` stub is needed, and plans 01-02 through 01-06 can register invariants against the planning SPEC directly.

Grounding: `internal/invregistry/registry.go` places no constraint on the `origin_spec` string, and `test/meta/invariant_registry_test.go` only asserts it is non-empty. The provenance guard walks `owned_paths` for residual legacy tokens; these entries are born canonical with no `legacy:` list, so it has nothing to misattribute.

## Verification

| Gate | Result |
|---|---|
| `go run ./cmd/inv-render -check` | exit 0 |
| `TestRegistrySchemaParsesOwnershipFields`, `TestEveryRegistryInvariantHasBinding`, `TestRegistryBindingChecks`, `TestProvenanceGuard*`, `TestBoundInvariantsAreGenuinelyAsserted` | exit 0 — 18 tests |
| `task lint:yaml` | exit 0 |
| Sixteen `## N.` headings, in order | 16, ordered |
| RFC2119 declaration present; no SPDX header | both confirmed |
| Section 8 tier tokens | exactly `anonymous`, `guest`, `player` — no fourth tier token (`admin`/`owner` hits are policy-source and UI-surface references, not tiers) |
| Section 13 ids vs `invariants.yaml` `id:` values | set-equal, 4 ids, each appearing exactly once in the SPEC |
| All four entries `binding: pending`, no `asserted_by` | confirmed |
| Scope-level `origin_specs` | `.planning/…/01-SPEC.md` present exactly once on each of the two scope records |
| Section 7 field enumeration | all twelve named fields present; count stated in prose; twelve distinct `profile.*` table rows |
| Section 7 terminology | no `room` / `avatar` / `user` substitutes |
| Section 7 `path:line` citations | all six resolve to the construct named |

## Deviations from Plan

### [Rule 3 — Blocking] `task lint:yaml` failed after the registry edit

- **Found during:** Task 2, before commit.
- **Issue:** `yamlfmt` (`.yamlfmt`, `max_line_length: 120`) rejected the file. The `-lint` output renders as a whole-file side-by-side diff, which initially read as a global reformat.
- **Diagnosis:** Confirmed the failure was mine, not pre-existing, by running `yamlfmt -lint` against the pre-execution file from `c34ca6954` (exit 0) versus the working file (exit 1). Then produced a real diff against a formatted copy, which scoped the change to exactly my added lines — `yamlfmt` packs folded scalars greedily and wanted its own wrapping.
- **Fix:** Rewrapped the four `summary:` blocks, then ran the sanctioned `task fmt:yaml`. Confirmed it touched no file outside the two already in this plan's scope.
- **Files:** `docs/architecture/invariants.yaml`
- **Commit:** `a66cce6a8`

### [Rule 2 — Missing critical detail] `characters.preferences` name collision

- **Found during:** Task 3, while grounding the field table.
- **Issue:** `characters.preferences` already exists as a `JSONB` owner-partitioned **settings** column (`internal/store/migrations/000045_character_preferences.up.sql:5`). PROFILE-08's OOC RP-preferences block is published profile prose — a different thing. A plain `profile.preferences` name invites a Phase-5 planner to conflate the two and write published prose into the settings column.
- **Fix:** Named the property `profile.rp_preferences` and called the collision out explicitly in §7.2 with a normative `MUST NOT`.
- **Files:** `.planning/phases/01-portal-spec/01-SPEC.md`
- **Commit:** `296023167`

### [Authored beyond the literal decision text] Totality rule for unassigned floors

- **Found during:** Task 2, writing the §8.6 posture table.
- **Issue:** D-14 names an explicit floor for seven attributes (`name`, `pronouns`, in-world description at `anonymous`; `rumors`, `currently`, preferences, `timezone` at `guest`) but §7's field set has twelve. The remaining five (`concept`, `species`, `age`, `faction`, `appearance`, `personality`, `biography`) had no stated default, which would leave Phase 2 to invent one.
- **Fix:** Rather than guess a per-field placement, stated a **totality rule**: every governed attribute MUST carry an explicit floor, and anything unassigned defaults to `guest`, never `anonymous`. This is the fail-closed direction and it forecloses the zero-value-means-allow shape EXT-03 forbids elsewhere in the milestone.
- **Note:** This is an addition, not a contradiction — D-14's named placements are reproduced exactly.

## Known Stubs

None in shipped code. `01-SPEC.md` carries eleven intentional section placeholders (§2–§6, §9–§12, §14–§16), each naming the plan that fills it, per this plan's own action. They are the planned shape of a document authored across six plans, not unwired functionality.

## Threat Flags

None. This plan authored a document and registry entries; it introduced no endpoint, auth path, file access, or schema change.

## Notes for the orchestrator

- **STATE.md and ROADMAP.md were not touched**, per instruction. `git diff c34ca6954..HEAD --name-only` returns exactly three files.
- **Filename:** written as `01-01-SUMMARY.md`, matching this plan's `<output>` block and the GSD `{phase}-{plan}-SUMMARY.md` convention. The dispatch prompt said `01-SUMMARY.md`; with six plans in this phase that name would collide, so the plan's own spelling was followed.
- **Actuals scale:** `tokens: 9972` is `chars/4` over the realized diff, per the ADR-2629 contract. The plan's `estimate.tokens: 70000` is on a different (whole-run context) scale, so the two are not directly comparable — recorded honestly rather than adjusted to narrow the apparent gap.

## Self-Check: PASSED

- Files: `01-SPEC.md`, `invariants.yaml`, `invariants.md` — all FOUND.
- Commits: `31176a623`, `a66cce6a8`, `296023167` — all FOUND.
- Working tree clean.
