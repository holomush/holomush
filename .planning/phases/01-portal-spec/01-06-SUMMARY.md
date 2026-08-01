---
phase: 01-portal-spec
plan: 06
subsystem: spec
tags: [spec, citations, grounding-trace, invariants, documentation-convention, gsd]

requires:
  - phase: 01-portal-spec (plans 01-01 .. 01-05)
    provides: "01-SPEC.md sections 1-15 authored; the nine amendments applied; the eight registry entries seeded"
provides:
  - "01-SPEC.md section 16 — the grounding trace, stamped with the ref and date the citation sweep ran against"
  - "A citation-verified SPEC: every path:line citation resolves to exactly one tracked file at a named commit"
  - "Section 3 reconciled with section 5.4 — participants_snapshot is no longer described as frozen names"
  - "A finished Status: line describing the complete document"
  - "The D-19 spec-location pointer edit: both CLAUDE.md passages plus the invariants rule"
  - "The completeness gate result: 10/10 PORTAL requirements, 5/5 roadmap criteria, zero placeholders, bidirectional invariant set equality"
affects: [phase-02, phase-03, phase-04, phase-05, phase-06, gsd-plan-checker, gsd-verifier]

actuals:
  tokens: 7700
  tasks: 3
  commits: 3

tech-stack:
  added: []
  patterns:
    - "Citation gate with a non-empty extraction floor — a `| while read` loop over an empty set reports the same green as one that checked everything"
    - "Suffix-unique citation resolution — a shorthand basename that matches two tracked files is a gate failure, not a style choice"
    - "Grounding trace grouped by what each citation grounds, with an explicit statement of what it deliberately does not ground"

key-files:
  created: []
  modified:
    - .planning/phases/01-portal-spec/01-SPEC.md
    - CLAUDE.md
    - .claude/rules/invariants.md

key-decisions:
  - "Raised the citation gate's extraction floor from 15 to 40 distinct cited files — 15 is far below the observed 57 and would not detect a mostly-broken extraction regex"
  - "Strengthened the gate from `test -f` to suffix-unique resolution, because the SPEC legitimately uses context-established shorthand (`web.proto:503`) that `test -f` cannot resolve at all"
  - "Expanded the four ambiguous shorthand citations rather than mass-expanding all shorthand — readability of the §3 inventory tables is worth preserving where the shorthand is unique"
  - "Reconciled §3 toward §5.4 by naming the id-vs-name mismatch and stating explicitly that census membership is unaffected — the census obligation is restated, not weakened"
  - "§16 states the three classes of claim it deliberately does not ground (forward references, rule files cited by heading, external standards) rather than leaving their absence to be read as an oversight"

patterns-established:
  - "Grounding trace: opens with ref + date + an explicit point-in-time disclaimer, groups citations by what they ground, closes with what it does not cover"
  - "Pointer edits to a claim stated twice must change both sites in one commit; a partial edit leaves the file contradicting itself with no signal"

requirements-completed: [PORTAL-10]

coverage:
  - id: D1
    description: "Every path:line citation in 01-SPEC.md resolves to exactly one tracked file, checked at a named commit"
    requirement: "PORTAL-10"
    verification:
      - kind: other
        ref: "citation gate: extract -> resolve exactly-or-suffix-unique over `git ls-files`, floor >= 40 distinct files; 57 resolved, exit 0"
        status: pass
    human_judgment: false
  - id: D2
    description: "Section 16 grounding trace: ref, date, counts, point-in-time disclaimer, grouped citation list, and a stated non-coverage section"
    requirement: "PORTAL-10"
    verification:
      - kind: other
        ref: "rg -c '^## [0-9]+\\. ' 01-SPEC.md == 16; no placeholder line survives"
        status: pass
    human_judgment: true
    rationale: "Whether the trace is walkable by a reviewer without reading the SPEC is a judgement no check makes."
  - id: D3
    description: "D-19 pointer edit applied at both CLAUDE.md passages and the invariants rule, with the walk-root prose byte-identical"
    verification:
      - kind: other
        ref: "rg -c '\\.planning/phases' CLAUDE.md >= 2; git show HEAD~:.claude/rules/invariants.md diff over the walk-root passage is empty; task lint:markdown, lint:docs-symmetry, lint:docs-paths-sync all exit 0"
        status: pass
    human_judgment: false
  - id: D4
    description: "Completeness gate: 16 sections, zero placeholders, 10/10 PORTAL requirements traced, 5/5 roadmap criteria traced, bidirectional invariant set equality, all pending with no asserted_by"
    requirement: "PORTAL-10"
    verification:
      - kind: unit
        ref: "task test -- -run 'TestRegistrySchemaParsesOwnershipFields|TestEveryRegistryInvariantHasBinding|TestRegistryBindingChecks|TestProvenanceGuard|TestBoundInvariantsAreGenuinelyAsserted|TestOwnedPathsPartition' ./test/meta/ — 29 tests, ok"
        status: pass
      - kind: other
        ref: "go run ./cmd/inv-render -check — exit 0"
        status: pass
    human_judgment: false

duration: 42min
completed: 2026-08-01
status: complete
---

# Phase 1 Plan 06: Grounding Trace, Citation Sweep and Spec-Location Pointer Summary

**Every citation in the portal SPEC was opened against the tree and six were found drifted; §3 no longer contradicts §5.4 about what `participants_snapshot` stores; and the repo's own spec-location convention now points at where this SPEC actually lives.**

## Performance

- **Duration:** ~42 min
- **Tasks:** 3 of 3
- **Files modified:** 3

## Accomplishments

- Swept **189 distinct path-bearing citations** (220 occurrences) plus **20 bare `:N` continuations** across **56 files** in §§1–15, opening every one against the tree. Six were corrected; none was removed.
- Authored **§16 Grounding Trace** — stamped with commit `5817edab` and 2026-08-01, grouped by what each citation grounds, with an explicit statement that citations are point-in-time and a closing section naming what the trace deliberately does **not** ground.
- Reconciled the **§3 / §5.4 contradiction** the wave-5 handoff flagged: §3 described `participants_snapshot` as frozen names, which is false today. §3 now names the id-vs-name mismatch (#4901) and states explicitly that census membership is unaffected either way.
- Applied the **D-19 pointer edit** at both `CLAUDE.md` sites and `.claude/rules/invariants.md`, leaving the orphan-check walk-root prose byte-identical.
- Ran the completeness gate: **16/16 sections**, **zero placeholders**, **10/10 PORTAL requirements** traced, **5/5 roadmap criteria** traced, and **bidirectional set equality** between §13 and the registry.

## Task Commits

1. **Task 1a: Citation corrections + §3/§5.4 reconciliation** — `5817edab` (docs)
2. **Task 1b: §16 grounding trace + Status line** — `d46f85cb` (docs)
3. **Task 2: D-19 spec-location pointer edit** — `d522bf6a` (docs)

Task 3 (the completeness gate) produced verification records rather than a source edit; its results are the three tables below.

## Files Created/Modified

- `.planning/phases/01-portal-spec/01-SPEC.md` — six citation corrections, the §3 reconciliation, §16 authored, `Status:` line rewritten
- `CLAUDE.md` — both spec-location passages (Documentation Structure, Spec-Driven Development)
- `.claude/rules/invariants.md` — frontmatter `paths:` glob plus one sentence in the escape-hatch prose

---

## Task 1 — the citation sweep

### Extraction

Citations were extracted mechanically from the document with
`rg -o '`[a-zA-Z0-9_./-]+\.(go|sql|proto|yaml|md|ts|lua|mod):[0-9]+(-[0-9]+)?`'`, then resolved
against `git ls-files` either exactly or as a **unique suffix**. Each resolved range was printed
and read against the claim the surrounding SPEC prose makes.

| Measure | Count |
| --- | --- |
| Distinct path-bearing citations in §§1–15 | 189 |
| Occurrences of those in the prose | 220 |
| Bare `` `:N` `` continuations (path named immediately before) | 20 |
| Distinct cited files | 56 |
| **Total distinct citations swept** | **209** |

### Dispositions — the three counts sum to the total

| Disposition | Count |
| --- | --- |
| Resolved unchanged | **203** (183 path-bearing + all 20 bare continuations) |
| Corrected | **6** |
| Removed | **0** |
| **Total** | **209** |

### The six corrections, with before and after

| # | Before | After | Why it was wrong |
| --- | --- | --- | --- |
| 1 | `web.proto:832-834` | `web.proto:832-835` | The prose names `repeated GameEvent`; that field is declared at line **835**. Lines 832–834 are the message declaration and the field's doc comment. The classic off-by-N landing on a comment. |
| 2 | `web/src/lib/nav/sections.ts:41-47` | `web/src/lib/nav/sections.ts:35-47` | §10.1 quotes two phrases from the file — *"exhaustive key type for any per-section map"* (line 46) and *"a section without an icon then fails to compile rather than crashing the rail at runtime"* (lines **38–39**). The second lies **outside** the cited range; 35–47 covers the doc comment both come from. |
| 3 | `world.proto:81` | `world/v1/world.proto:81` | Two `world.proto` files exist (`api/proto/holomush/world/v1/` and `api/proto/holomush/plugin/host/v1/`). The bare basename resolves to neither uniquely. |
| 4 | `world.proto:157-160` | `world/v1/world.proto:157-160` | Same ambiguity. |
| 5 | `world.proto:177-181` | `world/v1/world.proto:177-181` | Same ambiguity. |
| 6 | `01-CONTEXT.md:163-169` | `.planning/phases/01-portal-spec/01-CONTEXT.md:163-169` | Two `01-CONTEXT.md` files exist (`.planning/phases/01-portal-spec/` and `.planning/milestones/v0.11-phases/01-channels-subsystem/`). |

Corrections 3–6 are **ambiguity**, not drift: each landed on the right content when read in
context, but a mechanical checker could not resolve them, and the SPEC is read mechanically by
five downstream phases. The chosen expansions match the disambiguation idiom already in the same
table (`plugin/host/v1/world.proto:...`) rather than expanding every shorthand and widening the
inventory tables.

### Spot-verification of executable targets and exported identifiers

Every citation naming a runnable target or an exported symbol was checked to exist, not merely to
be plausible:

| Kind | Verified |
| --- | --- |
| Taskfile targets | `test`, `test:int`, `test:e2e` (§12.3) — all present in `Taskfile.yaml` |
| Go symbols | `PropertyProvider.ResolveResource`, `CharacterRepository.Update` / `.Delete`, `writeAuditRow`, `ReadSceneMetaForSnapshot`, `AssertOperatorAdmin`, `PostgresRoleStore.PlayerHasRole`, `world.Service.DeleteCharacter` / `.UpdateCharacterDescription` / `.ListPropertiesByParent`, `NormalizeCharacterName`, `ValidateCharacterName`, `SceneAccessServer.UpdateScene`, `updateSceneMaskablePaths`, `errutil.AssertErrorCode`, `world.ErrConcurrentEdit`, `CodeConcurrentEdit`, `visibleSections`, `SECTIONS` / `SectionId` |
| Proto methods | all 30 inventoried `rpc` declarations opened at their cited line |
| Dependency pin | `github.com/samber/oops v1.22.0` at `go.mod:32` |

No invented symbol, no nonexistent task, and no logical-rather-than-on-disk path was found.

### The gate, and why its floor was raised

The plan's Task-1 gate carried a floor of `-ge 15` before its loop, because
`rg … | while read …` reports success when extraction returns zero citations — a loop iterating
zero times is indistinguishable from every citation resolving. That floor is preserved and
**raised to 40**, with two changes recorded here as deliberate:

1. **Floor 15 → 40.** The real count is **57 distinct cited paths**. A floor of 15 would pass an
   extraction regex that had broken badly enough to find only a quarter of the document's
   citations. 40 sits below the observed value with headroom for future consolidation, and far
   enough above zero that a silent extraction failure cannot slip through.
2. **`test -f` → exactly-or-suffix-unique resolution.** The plan's literal gate tests
   `test -f "$f"` after stripping the line number. That fails on the SPEC's legitimate
   context-established shorthand (`web.proto:503`, `core.proto:742`, `PITFALLS.md:89-100`) — there
   is no `./web.proto`. Resolving by unique suffix accepts the shorthand **and** fails on
   ambiguity, which is strictly stronger than the original: it is what caught corrections 3–6,
   which `test -f` would have reported as clean file-does-not-exist failures without
   distinguishing them from a genuinely invented path.

Final gate result: `all 57 cited files resolve to exactly one tracked file`, exit 0.

### The §3 / §5.4 reconciliation

The wave-5 handoff flagged that §3 (authored by wave 2) described `participants_snapshot` as
*"the same frozen names"*, which wave 3 established is false today: `publish_store.go:988` writes
`SELECT character_id`, and the type comment at `:956-960` says *"Name resolution is a follow-up"*.
§5.4 and §14 row 6 already carried the correction; §3 alone did not, so the document contradicted
itself.

Three edits in §3.3, all toward §5.4 and none weakening the census:

- The public-export-surfaces preamble now says each publishes *a denormalized character identity* —
  a name by proto contract, an id in implementation — and adds a paragraph stating that **which of
  the two the column holds does not affect census membership**, that no row may be dropped on the
  argument that it publishes ids today, and that §5.4 explains why that argument fails
  prospectively too.
- The `WebGetPublicSceneArchive` row's note names ids-today / names-by-contract with the #4901
  reference, keeping "a later privacy change cannot reach this snapshot under either".
- The `WebListPublishedScenes` paragraph said "the same frozen names"; it now says "the same
  frozen participant column in bulk form … under the identical §5.4 id-versus-name caveat".

The audience verdict, the fourth-surface finding, and the census obligation are unchanged.

---

## Task 2 — the D-19 pointer edit

Three scoped edits, no file rewritten.

| Site | Change |
| --- | --- |
| `CLAUDE.md` § Documentation Structure (line 22) | Adds `.planning/phases/<phase>/<NN>-SPEC.md` as *"GSD milestone specs — where new specs go"*; re-labels `docs/specs/` + `docs/superpowers/specs/` as **historical** — still authoritative for what they describe, not where new work lands. |
| `CLAUDE.md` § Spec-Driven Development (line 52) | Same claim in that passage's own words. The RFC2119 clause, the invariant-registry capture clause, and the do-not-mint-ad-hoc-families clause all survive verbatim. |
| `.claude/rules/invariants.md` | Frontmatter `paths:` gains `".planning/phases/**/*-SPEC.md"`; the escape-hatch prose gains **one** sentence noting a planning-tree SPEC is likewise outside the walk root and must be hand-registered. |

### The prohibitions, verified

| Prohibition | Verification |
| --- | --- |
| Do not widen the orphan-check prose to claim the planning tree is walked | Both walk-root passages diffed byte-identical against `HEAD~`. The new sentence is a **new paragraph** after them, not an edit to them. |
| The prose must still match the code | `test/meta/invariant_registry_test.go:341` still reads `filepath.Join(root, "docs", "superpowers", "specs")` — the exact root the prose names. |
| No retirement-sweep work (#4900) | `git diff --name-only` for this task shows exactly `CLAUDE.md` and `.claude/rules/invariants.md`. None of the six files that link to a *specific* historical design spec (`CLAUDE.md:16`, `event-conventions.md`, `event-interfaces.md`, `branding.md`, `invariants.md:31`, `references/invariants-detail.md`) was touched. |
| Both CLAUDE.md sites, not one | `rg -c '\.planning/phases' CLAUDE.md` = 2, at lines 22 and 52. |
| The glob must match this SPEC | `.planning/phases/**/*-SPEC.md` matches `.planning/phases/01-portal-spec/01-SPEC.md` (verified by `fnmatch`). |

Docs lints run and green: `task lint:markdown` (746 + 84 files, no issues), `task lint:docs-symmetry`
(AGENTS.md → CLAUDE.md symlink intact), `task lint:docs-paths-sync`.

---

## Task 3 — the completeness gate

### Section completeness

`rg -c '^## [0-9]+\. ' 01-SPEC.md` = **16**. The heading list was diffed against the skeleton
committed by plan 01-01 and is **byte-identical in titles and order**. A search for
`Placeholder|placeholder — |authored by plan|filled by plan` returns **zero** matches: no
placeholder line naming a filling plan survives anywhere in the document.

### Requirement traceability — 10 / 10

| Requirement | Discharged by |
| --- | --- |
| PORTAL-01 — audience matrix, distinct shape per audience | §2.1, §2.2, §2.3, §2.7 |
| PORTAL-02 — read-surface inventory, every character-returning RPC | §3 (§3.1–§3.5), with the binding mechanism in §2.6 |
| PORTAL-03 — name-capture inventory with a verdict per site | §5 (§5.1–§5.5) |
| PORTAL-04 — lifecycle states; retire / idle-out / purge distinct | §4 (§4.1–§4.5) |
| PORTAL-05 — profile/media model as `entity_properties` rows | §7 (§7.1–§7.5) |
| PORTAL-06 — RPC surface with `expected_version` on every mutation | §9 (§9.1–§9.7), concurrency contract at §9.4 |
| PORTAL-07 — character-name and username normalization as two policies | §6 (§6.1–§6.3) |
| PORTAL-08 — role mutation excluded from character administration | §10.8, §15.3, mechanism at §10.6 |
| PORTAL-09 — sorting/filtering verdict | §11 (§11.1–§11.4) |
| PORTAL-10 — verification-integrity rules binding on every plan | §12 (§12.1–§12.3), rule 6 landing in §13 |

No unmapped requirement.

### Roadmap Phase 1 success criteria — 5 / 5

| Criterion | Discharged by |
| --- | --- |
| 1 — audience matrix + absence-not-emptiness + inventory incl. all **four** public export surfaces | §2.1–§2.3, §2.7, §3.3 (the three-row table plus the `WebListPublishedScenes` fourth-surface paragraph) |
| 2 — three distinct lifecycle operations, retire MUST NOT release the name, name-capture verdicts | §4.4, §4.5, §5.2 |
| 3 — profile/media model with intrinsics staying columns; two separate normalization policies | §7.1, §7.2, §7.3, §6 |
| 4 — `expected_version` on every mutation; role mutation excluded; sort/filter answered | §9.3 + §9.4, §10.8 + §15.3, §11.1 |
| 5 — six verification-integrity rules binding on every later plan | §12.1, §12.2 |

No unmapped criterion.

### Invariant correspondence — set equality in both directions

| Direction | Result |
| --- | --- |
| Declared in §13 | `INV-ACCESS-10`, `INV-ACCESS-11`, `INV-ACCESS-12`, `INV-PRIVACY-9`, `INV-PRIVACY-10`, `INV-WORLD-5`, `INV-WORLD-6`, `INV-WORLD-7` (8) |
| Registered with `origin_spec: .planning/phases/01-portal-spec/01-SPEC.md` | the same 8 |
| Declared-but-not-registered | **none** |
| Registered-but-not-declared | **none** |
| **Sets equal** | **yes** |

All eight carry `binding: pending` and **none** carries an `asserted_by` list, as §13 requires —
their asserting tests do not exist until Phase 2 or Phase 4.

### Final verification runs

| Check | Result |
| --- | --- |
| `go run ./cmd/inv-render -check` | exit 0 (no generated-companion drift) |
| `task test -- -run 'TestRegistrySchemaParsesOwnershipFields\|TestEveryRegistryInvariantHasBinding\|TestRegistryBindingChecks\|TestProvenanceGuard\|TestBoundInvariantsAreGenuinelyAsserted\|TestOwnedPathsPartition' ./test/meta/` | 29 tests, `ok` |
| Citation gate | 57 cited paths, all resolving to exactly one tracked file, exit 0 |
| `task lint:markdown` / `lint:docs-symmetry` / `lint:docs-paths-sync` | all exit 0 |

---

## Decisions Made

1. **Citation gate floor raised 15 → 40, and the resolution strengthened.** Recorded above with
   the reasoning. The floor exists to catch a silent extraction failure, and 15 against a real 57
   would not have caught one.
2. **Ambiguous shorthand expanded; unique shorthand left alone.** Mass-expanding every
   `web.proto:503` to a full path would widen the §3 inventory tables materially for no gain: they
   already resolve to exactly one tracked file. Only the four genuinely ambiguous ones changed.
3. **§3 reconciled toward §5.4, with the census obligation restated rather than assumed.** The
   risk in this edit is that a reader takes "it stores ids, not names" as grounds to drop a row
   from the census expected set. §3.3 now forecloses that in its own words.
4. **§16 records what it does not ground.** Forward references to Phase-2-through-6 artifacts,
   rule files cited by heading rather than line, and the external Unicode standards are named as
   deliberate non-citations — otherwise their absence from the trace reads as an omission.

## Deviations from Plan

### Auto-fixed issues

**1. [Rule 1 — Bug] Duplicate `sections.ts` citation introduced by my own correction**

- **Found during:** Task 1b (recounting citations after authoring §16)
- **Issue:** Correcting `web/src/lib/nav/sections.ts:41-47` → `:35-47`, I added the corrected
  citation as a parenthetical while the original `41-47` remained in the preceding clause, leaving
  §10.1 citing the same construct twice at two different ranges.
- **Fix:** Replaced the original `41-47` in place and removed the parenthetical.
- **Files modified:** `.planning/phases/01-portal-spec/01-SPEC.md`
- **Verification:** Set-diff of the pre-edit and post-edit citation sets shows exactly 6 removed
  and 6 added — the six corrections and nothing else.
- **Committed in:** `d46f85cb`

**2. [Rule 1 — Bug] §16's stated counts were measured over the wrong scope**

- **Found during:** Task 1b
- **Issue:** The first draft of §16's opening stated the sweep counts as if they described the
  whole document, but §16 itself restates ~83 citations, so those counts had become wrong the
  moment §16 was written.
- **Fix:** Scoped the counts explicitly to §§1–15 and added the sentence *"This section restates a
  subset of those citations and introduces no new one"* — then verified that claim by set-diff
  (§16's citation set ⊆ §§1–15's set, modulo two written with a longer but equivalent path).
- **Files modified:** `.planning/phases/01-portal-spec/01-SPEC.md`
- **Committed in:** `d46f85cb`

**3. [Rule 1 — Bug] `Status:` line miscounted §14**

- **Found during:** Task 1b
- **Issue:** The rewritten `Status:` line said "the ten amendments of §14". §14 has ten rows, but
  row 10 is a recorded **divergence**, not an amendment — the count the SPEC itself uses is nine.
- **Fix:** Restated as "§14's nine amendments … and its one divergence".
- **Committed in:** `d46f85cb`

---

**Total deviations:** 3 auto-fixed (3 × Rule 1). All three were defects in this plan's own edits,
caught by re-running the mechanical checks after each change rather than trusting the edit.
**Impact on plan:** none. No scope creep; no plan task altered.

## Issues Encountered

**The plan's literal Task-1 gate cannot pass on this document.** `sed 's/:.*//'` followed by
`test -f` fails on every context-established shorthand citation (`web.proto:503`,
`PITFALLS.md:89-100`, `core.proto:742`), of which the SPEC has many by deliberate style — the
§3 and §5 inventory tables would be unreadable at full paths. Resolved by strengthening the gate
to exactly-or-suffix-unique resolution, which accepts the shorthand and additionally fails on
ambiguity. The stronger form is what surfaced corrections 3–6.

## Notes for the Orchestrator

- **`STATE.md` and the ROADMAP bookkeeping were not touched**, per the carve-out. Nothing there
  looked wrong.
- **Two open issues are referenced by the finished SPEC and remain open by design:** **#4901**
  (the `participants_snapshot` proto-vs-implementation mismatch, now referenced from §3 as well as
  §5.4 and §14 row 6) and **#4902** (the `.claude/rules/grpc-errors.md` top-level-oops-code claim,
  whose rule-file half §14 row 8 deliberately does not apply). **#4899** (per-player vs
  per-character role semantics) and **#4900** (the `docs/superpowers/` retirement sweep) are
  likewise referenced and deferred.
- **The D-19 edit changes a repo-wide convention.** `CLAUDE.md` is symlinked from `AGENTS.md`, so
  both AI-tooling entry points pick it up with no second edit; `task lint:docs-symmetry` confirms
  the symlink is intact.

## Self-Check: PASSED

Files claimed created/modified, verified present:

- `.planning/phases/01-portal-spec/01-SPEC.md` — FOUND
- `.planning/phases/01-portal-spec/01-06-SUMMARY.md` — FOUND
- `CLAUDE.md` — FOUND
- `.claude/rules/invariants.md` — FOUND

Commits claimed, verified in `git log`:

- `5817edab` — FOUND (citation corrections + §3 reconciliation)
- `d46f85cb` — FOUND (§16 grounding trace + Status line)
- `d522bf6a` — FOUND (D-19 pointer edit)
