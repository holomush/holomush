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

---

## Gap closure (post-verification pass, 2026-08-01)

`01-VERIFICATION.md` scored the phase 5/5 on must-haves and found three gaps. This
pass closed exactly those three. It is a targeted correction, not a replan: no
section was re-authored, no settled decision re-litigated, and `STATE.md` /
`ROADMAP.md` were not touched.

### Gap 1 — one spelling of the governed attribute

§8.6's tier-floor table named `profile.preferences`; §7.2, §9.5 and §10.6 all name
`profile.rp_preferences`. Corrected §8.6 to `profile.rp_preferences`. A
document-wide sweep found no other occurrence in the SPEC.

Two other files carry the bare string and were **deliberately left alone**: they
are dated records where the rejected name is the *subject* of the sentence —
`01-01-SUMMARY.md:133` (the rationale for why the `rp_` qualifier exists at all)
and `01-VERIFICATION.md` (the gap report). Rewriting either would destroy the
record of why the name was chosen. The negative assertion is therefore scoped to
the SPEC.

### Gap 2 — `expected_version` was unimplementable for `CreateCharacter`

§9.3 asserted the guard universally over a table whose first row is
`CreateCharacter`, and §9.4.2 mandated rejection of an absent-or-zero value — so
every legal create request was rejectable. The rule is now scoped to **mutations
that target an existing character row**, with creation excluded and its reason
stated: `expected_version` is an optimistic-concurrency predicate against a row
that already exists, and a create has nothing to be stale against. Creation's
concurrency safety comes from §6.1.3's `UNIQUE` index on the stored normalized
name — the same index `RenameCharacter` collides against, surfacing as
`CHARACTER_NAME_TAKEN`.

The alternative "creates pass `expected_version = 0` meaning must-not-exist" was
**not** taken: it contradicts §9.4.1's "zero is not a legal input" and would have
required a decision this pass was not authorized to make.

Applied at five sites — the three the verifier named, plus two more that carried
the same universal form and would have reintroduced the contradiction:

| Site | Change |
| --- | --- |
| §9.3 preamble | Scoped to existing-row mutations; names the create exclusion and its §6.1.3 guard |
| §9.3 `CreateCharacter` row | Exclusion marked **in the table cell**, not only in prose |
| §9.4 lead-in *(not verifier-named)* | Same universal sentence, scoped |
| §9.4.1 | "each mutation request message" → "each **guarded** mutation request message" |
| §9.4.2 | Defines *guarded mutation*; states the exclusion, its reason, and what guards a create instead |
| §9.6 error table *(not verifier-named)* | `CHARACTER_VERSION_REQUIRED` scoped; noted unreachable from `CreateCharacter` |
| §13 `INV-WORLD-7` | Version clauses scoped; **same-transaction clause deliberately left universal** |
| `invariants.yaml` `INV-WORLD-7` summary | Same scoping, same wording split |

**The same-transaction obligation was deliberately NOT scoped.** Creates emit
through the transactional outbox exactly like every other mutation; only the
*version guard* excludes creation. Both the SPEC declaration and the registry
summary now state that split explicitly, so a future reader cannot infer that
creation escapes `INV-WORLD-1`.

`INV-WORLD-7` **keeps its id** — renumbering is a registry migration, not an
edit. Verified: `binding: pending`, no `asserted_by`, and the eight-id set for
this phase is byte-identical to the pre-fix set.

### Gap 3 — amendment row 6's superseded count in the research corpus

`.planning/research/SUMMARY.md:352` still described the read-surface inventory as
covering "the three existing public export surfaces". Annotated superseded-in-part
in the style rows 4 and 7 already used on this same dated record — original text
left intact, blockquote appended, closing attribution line. Placed at the end of
the Phase-1 block rather than mid-paragraph, matching row 4's shape (blank line
before and after) rather than fracturing the `Delivers:`/`Addresses:`/`Avoids:`
run.

The annotation names all four surfaces. The first draft misnamed the three
research-known ones (guessed `WebGetPublicCharacter` / `WebGetPublicCharacterList`);
corrected against §3.3's actual table to `WebExportScene`,
`WebGetPublicSceneArchive` (`web.proto:345`) and `WebDownloadPublicSceneArchive`,
with `WebListPublishedScenes` (`web.proto:339`) as the fourth.

### Verification

Every assertion was run twice — against the pre-fix state (materialized read-only
via `git show HEAD:<path>`; `git stash` is forbidden in a worktree because the
stash stack is shared across worktrees) and against the working tree. An
assertion counts only if it goes **RED pre-fix and GREEN post-fix**; one that is
already true pre-fix is reported as vacuous and fails the harness.

| Assertion | Result |
| --- | --- |
| `! rg -q 'profile\.preferences'` over the SPEC | RED → GREEN |
| Same, backtick-anchored | RED → GREEN |
| §9.3 preamble no longer states the universal form | RED → GREEN |
| §9.4 lead-in no longer states the universal form | RED → GREEN |
| `INV-WORLD-7` (SPEC) no longer states the universal form | RED → GREEN |
| `INV-WORLD-7` (registry) no longer states the universal form | RED → GREEN |
| A row-6 superseded-by marker exists in `research/SUMMARY.md` | RED → GREEN |
| That marker sits within 20 lines of the superseded count | RED → GREEN |

Two assertion-design traps the harness caught, both of which would have produced
a **false green** if the checks had only been run post-fix:

1. **Line-wrap.** `three existing public export surfaces` never matches: the
   source hard-wraps between `export` and `surfaces`, and `rg` is line-based with
   multiline off. The anchor was shortened to `three existing public export`. A
   post-fix-only run would have "passed" while testing nothing.
2. **Annotation style vs. a bare negative.** Rows 4/7 keep the superseded line
   intact, so `! rg -q '<superseded phrase>'` is the *wrong* assertion — it would
   demand a rewrite the amendment style forbids. An **adjacency** assertion is
   used instead: the phrase's line number is located, and the marker must appear
   within the following 20 lines. Stated here because the brief asked which form
   was used and why.

The two-run harness also caught a genuine defect in this pass's own work: the
first placement of the gap-3 blockquote split the Phase-1 paragraph block, which
the adjacency check surfaced as a failure before commit.

`! rg -q PATTERN FILE` is used throughout. The `cmd; test $? -eq 1` shape is
**not** used anywhere — it passes when the string survives.

Gate results, all judged by exit code (never by grepping stdout):

| Gate | Exit |
| --- | --- |
| `go run ./cmd/inv-render -check` | 0 |
| `task lint:yaml` (after `task fmt:yaml` reflowed the summary to 120 cols) | 0 |
| `task lint:markdown` | 0 |
| `task test -- -run 'TestEveryRegistryInvariantHasBinding\|TestProvenanceGuard\|TestBoundInvariantsAreGenuinelyAsserted\|TestInvariantRegistry\|TestScope' ./test/meta/` | 0 |
| Gap harness (16 checks) | 0 |

`docs/architecture/invariants.md` was **regenerated** via `go run ./cmd/inv-render`,
never hand-edited inside its generated regions.

### Observation left open (non-blocking, not in scope for this pass)

`.planning/research/SUMMARY.md:354` ("the full new RPC surface with
`expected_version` on every mutation request") and item 6 at `:323` carry the same
universal phrasing gap 2 corrected in the SPEC. They were **not** annotated: §14
records amendments the SPEC makes to *sibling* artifacts, and the create carve-out
is a correction to the SPEC's own normative text discovered by verification, with
no §14 row to cite. Minting one would misuse the table. Flagged here so a later
pass can decide deliberately rather than inherit it silently.

## ABAC review closure

A follow-up `abac-reviewer` pass over **§8 (Profile Visibility Model)** returned
four blocking findings and two non-blocking ones. All six are closed below. The
diagnosis in every blocking case was the same shape: §8 specified the visibility
**policy** correctly and completely, then stopped one level short of the **ABAC
mechanism** — leaving a paragraph a Phase-2..6 implementer would otherwise invent,
where each plausible invention fails **open** against the shipped deny-overrides
engine. §8.1, §8.3, §8.9–§8.12 were sound and are unchanged.

**Specification text only.** No Go, proto, SQL or UI was written. `STATE.md` and
`ROADMAP.md` were not touched. `docs/architecture/invariants.yaml` was **not**
edited — see the INV-PRIVACY-9 note below — so no `inv-render` regeneration and no
registry meta-test run was required.

### The four blocking findings

**F1 — tier "clears" was unspecified, and the DSL's only string ordering is
lexicographic.** §8.2 said only that a viewer clears a floor "when the viewer's own
tier is at or above it," never translated to a DSL form. `compareStrings`
(`internal/access/policy/dsl/evaluator.go:185-201`) implements `>=` as Go byte
order; `anonymous < guest < player` holds by **alphabetical accident**, and
`spectator` / `unverified` / `visitor` all sort *above* `player` — so §8.2's own
"adding a fourth rung later is an append" would grant a new token top clearance
silently. **Closed by new §8.2.1:** explicit set membership
(`principal.viewer.tier in ["guest", "player"]` for a `guest` floor) is mandated;
ordinal string comparison on the tier is forbidden **normatively**; a numeric rank
attribute is rejected with its own rationale (a second source of truth for one
ladder, failing open the same way). The cost — editing N clearing sets per new rung
— is stated as the feature: the new token clears **nothing** until each floor is
explicitly re-decided. A Phase-2 test obligation appends a synthetic fourth token
sorting lexicographically above `player` and asserts it does not clear a `player`
floor, **demonstrated RED against an ordinal-comparison implementation** first.

**F2 — the glob catch-all was a permit that never inspects the row.** §8.6's last
table row (*"every other `profile.*` field"*) plus the totality rule mandated a
name-keyed catch-all that reads no `visibility` / `visible_to` / `excluded_from`.
The engine is deny-overrides (`combineDecisions`,
`internal/access/policy/engine.go:591-611`): **any** satisfied permit permits, so
that row would have been **additive to** — not conjunctive with — the shipped
row-keyed `seed:property-*` family. Combined with `seed:property-owner-write`
(`internal/access/policy/seed.go:128-133`, owner writes **any** property) and
§7.1's open namespace (a new field is an `INSERT`), any `profile.` row carrying
`visibility='private'` or `'admin'` would publish to every guest on the open web;
only `restricted` + `excluded_from` survives, being the family's one `forbid`.
**Closed with both remedies**, not one: new **§8.5.1** states the composition is
**conjunctive** and ANDed by the caller across **two** evaluations (explicitly not
one evaluation with two permits); **§8.6** replaces the catch-all with **exact
enumeration** of every §7.2 field and every §7.3 media name, matched as whole
strings, with globs/prefixes/wildcards forbidden in the family.

The **totality rule is deliberately flipped** and the flip is explained in place as
a correction rather than left to read as drift: it previously said an unassigned
`profile.*` attribute defaults to `guest`. Over an **open** namespace reached by a
**name-keyed permit**, `guest` is *more permissive than the engine's own
default-deny* — the wrong direction. It now reads: every governed attribute MUST
carry an explicit floor, and an attribute with no floor **is denied, not
defaulted**. The original intent (adding a field must never silently publish it) is
preserved verbatim; only the mechanism meant to carry it is fixed.

Two table rows were also made byte-exact, which the new whole-string matching rule
makes load-bearing: `pronouns` → `profile.pronouns` (§7.2's actual property name)
and `name` → name (`characters.name`, a column). The eleven media names are stated
to be a **closed enumerable set**, not a pattern — `profile.image.gallery.10` is
explicitly not a member and is therefore denied, which is the §7.3 exact-bytes rule
carried through to authorization.

**F3 — the viewer principal was never specified, and the engine rejects an
identity-less subject.** `validateRequest`
(`internal/access/policy/engine.go:550-573`) and `CanPerformAction` (`:418-425`)
both reject a subject not in `type:id` form with non-empty parts
(`INVALID_ENTITY_REF`); an `anonymous` viewer has no such identity. **Closed by new
§8.4.1**, which fixes the subject form per rung — `viewer:anonymous`,
`viewer:guest:<player-ulid>`, `viewer:player:<player-ulid>` — and shows all three
parse under `SplitN(subject, ":", 2)`. Both likely implementer resolutions are
named and rejected with citations: `character:<id>` activates
`seed:admin-full-access` (`seed.go:104-109`) and `seed:player-character-colocation`
(`seed.go:50-55`), bypassing the ladder; `player:<id>` carries the crypto-operator
grant axis and has no anonymous representation. Because every shipped seed is
`principal is character` / `plugin` / `system`, a `viewer:` subject matches **no**
existing policy — which is what makes the default-deny floor genuinely the floor.

The namespace and provider are grounded in the package's real conventions rather
than invented: `principal.viewer.tier` is supplied by a new `ViewerTierProvider`
whose `Namespace()` returns `"viewer"`, shaped after `PlayerAttributeProvider`
(`internal/access/policy/attribute/player.go:80-107`), with the resolver's
merge-time namespacing cited (`resolver.go:52-74`). Per
`.claude/rules/abac-providers.md` / ADR holomush-ti1b, `player_id` is **omitted**
on the anonymous rung (never `""`) with a `has_player_id` witness present on every
path, and all keys must be declared in `Schema()` or the resolver drops them
(`abac_rejected_provider_attributes_total`). The tier MUST be server-derived, never
client-supplied. Three Phase-2 obligations are stated: register the provider in
`BuildABACStack` (`internal/access/setup/setup.go:108-262`) and confirm
`warnOnMissingSeedCoverage` does not WARN for `viewer`; add `SubjectViewer` to the
constants **and** to `knownPrefixes` (`internal/access/prefix.go:12-61`, since
`ParseEntityRef` rejects unlisted prefixes); ship a `ViewerSubject` constructor.

**F4 — the reachability facet had no ABAC expression.** §8.6 listed a *profile
reachability* row and §8.7 evaluates it before any per-field floor, but §8.4/§8.5
define the family only over `resource is property` keyed by attribute name.
Reachability is neither. **Closed by new §8.4.2**: resource `profile:<character_id>`
(a distinct resource type), action `read`, principal the `viewer:` subject, seeded
policy `seed:profile-reachable`, with the policy text given verbatim. It needs no
`profile`-namespace provider — `resource is profile` is a target match on
`parseEntityType` (`engine.go:542-548`). Reusing `character:<id>` + `read` is
rejected with its seed citations. The section states reachability is **evaluated
independently** — its own `Evaluate` call, never derived from per-field results —
and names why: deriving it from "did any field clear its floor" pins it at
`anonymous` under the seeded defaults (`name` is at `anonymous`, so something always
clears), the §8.7 not-found-equivalent can never fire, and **INV-PRIVACY-9 binds to
a gate that cannot deny**. Evaluation order (reachability first, DENY short-circuits
per-field) is fixed. A Phase-2 obligation adds `ResourceProfile` to the prefix
constants and `knownPrefixes`.

**INV-PRIVACY-9 was checked and left unchanged, id and summary both.** Its registry
summary (`docs/architecture/invariants.yaml:2120-2126`) pins the *guarantee* — a
below-floor profile returns a not-found-equivalent indistinguishable from a
nonexistent character. §8.4.2 specifies the *mechanism* that guarantee now rests
on; it does not change what is guaranteed, so the summary remains accurate and no
renumbering (a registry migration) was warranted.

### The two non-blocking items, recorded

Recorded as explicit obligations; **not** redesigned around.

1. **Census "no more" half for the name-reachable class (§3.2).** §3.2 itself
   concedes the descriptor predicate cannot reach bare-string identity fields, so a
   literal implementation is RED day one and the natural repair — union both sides
   — makes the class **self-certifying**. §3.2 now states the seeding mechanically
   for both sides (expected: both categories from §3.3; derived: the descriptor
   walk **unioned with** the same explicitly-named list), states plainly that the
   "no more" half does not hold over that class while the type-reachable class
   keeps both halves, and adds a standing **Phase-4 obligation** that a new
   scalar-identity field requires a manual §3.3 row in the same change.
2. **§8.8's hard floor is unenforceable against a `source='admin'` row.** Under
   deny-overrides an admin-authored forbid beats the seeded permit. It fails
   **closed** (the viewer sees less), so it is not a disclosure hole — but
   INV-PRIVACY-10 is phrased as a system guarantee while resting on **operator
   discipline**. §8.8 now says so plainly, and notes §8.12 ships no editing surface,
   so the only way to author such a row in v0.13 is a direct `access_policies`
   write.

### Verification

41 assertions, run as a two-pass harness and judged **by exit code only**. The
pre-fix state was extracted read-only with `git show HEAD:<path>` (no `git stash`,
which is prohibited in a worktree) and the harness run against it **first**.

| Run | Result |
| --- | --- |
| RED, against pre-fix content from `HEAD` | **41/41 fail** — every presence anchor absent, both removal anchors present |
| GREEN, against the working tree | **41/41 pass**, exit 0 |

A wholly-RED first pass is the point: an assertion never observed failing cannot
distinguish a fix from a no-op.

Trap-avoidance carried forward from the earlier gap pass:

- **Section slicing, not whole-file grep.** Presence anchors are checked against an
  `awk`-extracted slice of the owning subsection (`### 8.2.1` … `### 8.3`, etc.),
  with an empty slice counted as a failure. Without it, `profile.biography` would
  match its §7.2 row and pass while §8.6 said nothing — the substring trap in its
  most convincing form, since the string genuinely exists in the file.
- **Line-wrap awareness.** `rg` is line-based with multiline off, so every anchor
  phrase was placed to sit on one source line (`sorts lexicographically above`,
  `is denied, not defaulted`, `evaluated independently`).
- **Absence anchors distinguished from their supersession prose.** The removal
  check is the fixed string ``**MUST** default to `guest` ``; the new supersession
  paragraph deliberately writes "would default to `guest`" so the corrected text
  cannot satisfy the assertion that its predecessor is gone.
- `! rg -qF PATTERN FILE` is used for absence throughout. The `cmd; test $? -eq 1`
  shape appears nowhere — it passes when the string survives.

| Gate | Exit |
| --- | --- |
| Closure harness, RED pass against `HEAD` content | 1 (expected) |
| Closure harness, GREEN pass against working tree | 0 |
| `task lint:markdown` | 0 (`.planning` is excluded from its scope; run for repo-wide regression only) |

`docs/architecture/invariants.yaml` and `docs/architecture/invariants.md` are
untouched, so `cmd/inv-render` and the registry meta-tests were not in scope for
this pass.
