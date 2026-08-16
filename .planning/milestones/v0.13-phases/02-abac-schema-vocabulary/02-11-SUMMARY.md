---
phase: 02-abac-schema-vocabulary
plan: 11
subsystem: planning-artifacts
tags: [amendments, invariant-registry, validation-map, spec-maintenance, closeout, red-first]
status: complete

requires:
  - "02-02 — the sc-vs-scx divergence text (Amendment D)"
  - "02-03 — the drafted §8.4.1 viewer-key amendment (Amendment F)"
  - "02-04 — the recorded INV-WORLD-6 before/after (Amendment E)"
  - "02-07 — the shipped tier-floor count and the five viewer twins (Amendments A, G)"
  - "02-12 — the third out-of-world writer (Amendment H)"
  - "02-13 — the derived peers and D-27's direction (Amendment F)"
provides:
  - "01-SPEC.md §14 rows 11-18 — eight amendments, each applied in place and proven by a recorded single-line search"
  - "01-SPEC.md §8.5.1.1 — option 2 recorded REJECTED; D-01's five viewer twins as the settled shape"
  - "01-SPEC.md §8.5.2 — the three derived player-keyed property peers and D-27's derivation direction"
  - "01-SPEC.md §8.4.1 — the five-key viewer attribute table"
  - "01-SPEC.md §8.6 — the TWO-policy tier-floor count with its cause and re-entry condition"
  - "01-SPEC.md §11.3 — last_active_at and joined players.username sort/filter rows"
  - "01-SPEC.md §6.1.2 — the Script-vs-Script_Extensions divergence, recorded as a choice"
  - "01-SPEC.md §13 — INV-WORLD-6's corrected enumeration and the post-Phase-2 binding-state table"
  - "docs/architecture/invariants.yaml — INV-WORLD-4's writer count TWO -> THREE"
  - "02-VALIDATION.md — the filled Per-Task Verification Map (40 tasks), the 30-row test-map reconciliation, and the 49-row RED-first table"
affects:
  - "phase-04-character-access-service (reads §8.5.1.1, §8.5.2 and §13 directly)"
  - "phase-03 (inherits Rename's MUST-NOT-route-through-mutate rule)"
  - "phase-06-admin-surfaces (inherits §11.3's two new sort keys)"

tech-stack:
  added: []
  patterns:
    - "Applying an amendment in place and proving it with a search whose target lies on a SINGLE source line, then excluding the amendment table itself from the search scope — because §14's own contract requires it to quote the superseded string verbatim"
    - "A derived-count check whose failure mode is HALT-and-report, never reconcile-by-editing-the-record"
    - "Deriving a scope list from the directory listing rather than from prose, so a later-added plan is included by construction"

key-files:
  created:
    - .planning/phases/02-abac-schema-vocabulary/02-11-SUMMARY.md
  modified:
    - .planning/phases/01-portal-spec/01-SPEC.md
    - .planning/phases/02-abac-schema-vocabulary/02-VALIDATION.md
    - docs/architecture/invariants.yaml
    - docs/architecture/invariants.md

decisions:
  - "D-03's tier-floor count was VERIFIED against seed.go (2 == 2, same two entry names) and 02-CONTEXT.md was NOT edited. A check whose failure mode is 'edit the thing being verified' has no failure mode."
  - "Amendment C corrected only the 36 internal/store citations. The five plugins/core-scenes/migrations/*.up.sql citations are still accurate — that corpus was NOT part of the goose cutover and remains .up.sql/.down.sql pairs on disk — so the plan's own closeout gate is unsatisfiable as written."
  - "Amendment E was scoped to include §13's 'All eight ship binding: pending' sentence and the ninth entry §13 never allocated (INV-PRIVACY-11), because leaving them would have made §13 state two falsehoods adjacent to the one being corrected."
  - "abac-reviewer was NOT run and no verdict was fabricated: it is a repo-owned sub-agent and this executor has no agent-dispatch tool. Recorded as an OUTSTANDING blocking pre-merge gate with a named owner."
  - "ROADMAP criterion 4 already carries the D-29 deferral. The scope question is still escalated to the maintainer with three options and none selected, because the plan was written against the pre-amendment wording."

metrics:
  duration: ~105min
  completed: 2026-08-05

actuals:
  tokens: 30629
  tasks: 3
  commits: 3
---

# Phase 02 Plan 11: The Phase Amendment Pass and Validation Close-Out Summary

**Eight amendments applied in place — six in `01-SPEC.md`, one verified-not-written in `02-CONTEXT.md`, one in the invariant registry — each proven by a recorded single-line search; the validation map filled from a directory-derived plan list; and the phase gate green. One obligation is outstanding and is stated loudly rather than quietly satisfied: `abac-reviewer` did not run.**

## Performance

- **Duration:** ~105 min
- **Tasks:** 3 of 3
- **Files:** 5 (1 created, 4 modified)
- **Code shipped:** none. This plan writes only planning artifacts and the invariant registry.

## Task Commits

| Task | Name | Commit | Key files |
| --- | --- | --- | --- |
| 1 | Apply the eight amendments and prove each landed | `16ae7cca6` | `01-SPEC.md`, `docs/architecture/invariants.yaml`, `invariants.md` |
| 2 | Fill the validation map and set its status honestly | `ccc03e6c7` | `02-VALIDATION.md` |
| 3 | Route to `abac-reviewer` and run the phase gate | (no commit — review pass and gate run) | — |

---

## Reconciling the amendment set

The plan text names eight amendments (A–H). The orchestrator's briefing named ten items discovered during execution. They are **not** two independent lists — most of the briefing's items are the plan's amendments seen from the executing plan's side. Reconciled below so nothing is double-applied and nothing is dropped:

| Briefing item | Disposition |
| --- | --- |
| **1.** INV-WORLD-4 now false (third writer) | = **Amendment H**. Applied once. |
| **2.** INV-WORLD-6's "ONLY path" already falsified | = **Amendment E**. The registry half landed in `02-04`; **verified still present** and the matching SPEC-side edit applied here. |
| **3a.** §6.1.2 verdict table is a paraphrase; subset-coverage permits `{Han, Hiragana}` / `{Han, Hangul}` | Folded into **Amendment D** as its second half. |
| **3b.** `mixedscript.go` computes over `sc`, not `scx` | = **Amendment D**'s first half. Applied once. |
| **4.** Amendment F — five viewer keys, not three | = **Amendment F**'s first half. Applied once. |
| **5.** §8.5's six derived keys + D-27's direction stated explicitly; `player` namespace gains `roles`/`has_roles` | = **Amendment F**'s second half. Landed as a **new §8.5.2** plus a §10.5 note. |
| **6.** `02-10`'s Task 5 `<behavior>` contradicts its own `<action>`/`<deferral>` | **Reconciled, not amended into a SPEC section** — see below. |
| **7.** `02-RESEARCH.md`'s schema section is stale | **NOT applied — deliberately.** See below. |
| **8.** `ADMIN_SECTION_EVALUATION_FAILED` untested; inline error-code literals | **Recorded**, in `02-VALIDATION.md`'s unmet-criteria table and in the `abac-reviewer` brief. Not a SPEC amendment. |
| **9.** The `rg`-substring-gate defect class | **Recorded here** as the phase's highest-value lesson — and this plan is its fourth *and fifth* instance. |
| **10.** `Rename` MUST NOT be routed through `worldMutator.mutate()` | **Already in `Rename`'s doc comment** (`02-06`). Recorded in this SUMMARY's Phase-3 hand-off; no SPEC edit needed. |

### Two briefing items deliberately NOT applied, with reasons

**Item 6 — `02-10`'s Task 5 `<behavior>` vs `<action>` contradiction.** The contradiction is real: behaviour bullet 2 asked for an assertion that a viewer may read another character's *entity* (the read carrying `characters.description`), while the `<action>`, the `<deferral>` and an acceptance criterion all forbid it because D-29 defers that permit to Phase 4. **The executor followed the corrective text**, and `02-10`'s SUMMARY records it. Reconciling it here means **stating which text governs**, which this SUMMARY does: **the `<action>`/`<deferral>` govern; the `<behavior>` bullet was stale.** It is not amended into `01-SPEC.md` because the contradiction lives in an executed plan file, not in the SPEC — and rewriting an executed plan's task body after the fact destroys the record of what was actually run. `02-VALIDATION.md`'s row 15 carries the resulting partial coverage.

**Item 7 — `02-RESEARCH.md`'s stale schema section.** `02-RESEARCH.md:295` states *"There is **no** `status`, **no** `last_active_at`, **no** normalized-name column, and **no** unique or `LOWER()` index on `name`"*, carrying its own `[VERIFIED: … read 2026-08-03]` stamp. That was true when written and is false since `000054`/`000055`/`000056` landed on this branch. **It is deliberately not rewritten**, on the same principle §14 row 4 already applies to `.planning/research/SUMMARY.md`: a **dated research record** is annotated, never rewritten, because rewriting it destroys the ability to reconstruct why a decision was made — and the whole D-21 A→B→C sequencing argument rests on that section's premise. The staleness is recorded here and in `02-VALIDATION.md` instead. A `VERIFIED`-stamped, dated line that a reader can date-check is a weaker trap than a silently-updated one.

---

## Task 1 — the eight amendments, and the search proof for each

Each proof targets a substring lying on a **single** source line. The "superseded" searches run over `01-SPEC.md` **excluding §14's numbered rows** (`rg -v '^\| \*\*[0-9]+\.\*\*'`), because §14's own contract *requires* those rows to quote the superseded text verbatim — see the gate defect below.

| # | Amendment | Search target (single line) | Result |
| --- | --- | --- | --- |
| **A** | §8.5.1.1 — option 2 REJECTED | superseded: `or an explicit rule that term B evaluates against a co-located` | **0** ✅ |
| **A** | | landed: `Option 2 is REJECTED` | **1** ✅ |
| **A** | | five twins named: `seed:viewer-property-[a-z-]+` distinct | **5** ✅ |
| **B** | §11.3 — two new rows | landed: `^\| \`characters.last_active_at\` \| Yes \| Yes \|` | **1** ✅ |
| **B** | | landed: `^\| joined \`players.username\` \| Yes \| Yes \|` | **1** ✅ |
| **B** | | unchanged: `^\| \`characters.player_id\` \| No \| Yes \| The OOC player` | **1** ✅ |
| **C** | migration citations | superseded: `internal/store/migrations/[0-9]*_[a-z_0-9]*\.up\.sql` | **0** ✅ |
| **C** | | landed: `000001_baseline\.sql:71-80` | **2** ✅ |
| **C** | | untouched (still valid): `plugins/core-scenes/migrations/…\.up\.sql` | **5** ✅ |
| **D** | §6.1.2 — sc vs scx | landed: `Script_Extensions` | **2** ✅ |
| **D** | | landed: `can only ever see \*\*fewer\*\* scripts` | **1** ✅ |
| **E** | §13 — INV-WORLD-6 | superseded: `is the only path by which a` | **0** ✅ |
| **E** | | superseded: `ONLY` within 4 lines of `INV-WORLD-6` | **0** ✅ |
| **E** | | landed: `there are exactly \*\*TWO\*\* such paths` | **1** ✅ |
| **F** | §8.4.1 / §8.5.2 | superseded: `All three keys \*\*MUST\*\* be declared` | **0** ✅ |
| **F** | | landed: `^\| \`roles\` \| string list \|` | **1** ✅ |
| **F** | | landed: `^#### 8.5.2 The derived player-keyed property peers` | **1** ✅ |
| **F** | | landed: `Permit side — the ALL direction` / `Forbid side — the ANY direction` | **1 / 1** ✅ |
| **G** | §8.6 — tier-floor count | landed: `seed therefore emits \*\*TWO\*\* policies` | **1** ✅ |
| **G** | | landed: `\*\*Re-entry condition\.\*\*` | **1** ✅ |
| **H** | `invariants.yaml` — INV-WORLD-4 | superseded: `exactly TWO sanctioned` (whole file) | **0** ✅ |
| **H** | | landed: `exactly THREE sanctioned` | **1** ✅ |
| **H** | | `asserted_by` gains `cmd/holomush/cmd_character_name_integration_test.go` | **1** ✅ |

**§14 numbered rows: 18** (10 pre-existing + 8 new). Every new row quotes its superseded text verbatim alongside artifact and location; row 17 names **both** artifacts it touches and row 18 names `docs/architecture/invariants.yaml`.

### Amendment C — what was actually corrected

**36** `internal/store/migrations/*.up.sql` citations, across §4.1, §4.4, §5.2, §6.1.3, §6.2, §7.1, §7.3, §7.4, §8.4, §9.4.1, §10.5 and §16. Every line number was **re-derived against the current tree** and each was verified to resolve to the content the SPEC claims — not transcribed from `02-RESEARCH.md`, which the plan explicitly forbids because the tree moved again after it was written.

`000001_baseline.sql` shifted uniformly **+4** (the `-- +goose Up` header). Every citation was checked by reading the target line:

| Old | New | Content at the new line |
| --- | --- | --- |
| `:54` | `:58` | `username TEXT UNIQUE NOT NULL,` |
| `:67-76` | `:71-80` | the `characters` comment through `CREATE INDEX idx_characters_location` |
| `:71` / `:72` | `:75` / `:76` | `name` / `description` |
| `:80` / `:83-87` / `:84` | `:84` / `:87-91` / `:88` | the `default_character_id` FK / `character_roles` / its `ON DELETE CASCADE` |
| `:99` / `:143` | `:103` / `:147` | `locations.owner_id` / `objects.owner_id` |
| `:259` / `:261` / `:294` | `:263` / `:265` / `:298` | the three enum-by-`CHECK` precedents |
| `:350-371` / `:358` / `:364` | `:354-375` / `:362` / `:368` | `entity_properties`, its `visibility` CHECK, its unique constraint |

The other three files did **not** shift uniformly and each was re-derived independently:

| Old | New | Note |
| --- | --- | --- |
| `000045_character_preferences.up.sql:5` | `000045_character_preferences.sql:7` | +2 |
| `000049_world_version_guard.up.sql:20` | `000049_world_version_guard.sql:22` | **The old citation pointed at `locations`, not `characters`.** Line 20 today is the `locations` ALTER; the `characters` one the SPEC quotes is at 22. Re-derivation caught a citation that was wrong on content as well as on path. |
| `000052_events_audit_partition.up.sql:114` / `:119` | `…sql:120` / `:125` | +6 |

Bare back-references (`(\`:80\`)`, `(\`:364\`)`, `(\`:76\`)` …) in §5.2 and §16 were corrected line-by-line against their own context, not by a blanket substitution.

### Amendment G — the count was CHECKED, and it agreed, so nothing was written

Both numbers, as the plan requires them recorded:

- `internal/access/policy/seed.go` — **2** distinct `seed:profile-tier-floor-` entries: `seed:profile-tier-floor-anonymous`, `seed:profile-tier-floor-guest`.
- `02-CONTEXT.md` D-03 records — **2**, and **the same two names**.

`[ "$(rg -o 'seed:profile-tier-floor-[a-z]+' internal/access/policy/seed.go | sort -u | wc -l)" -eq 2 ]` → **passes**.

**`02-CONTEXT.md` was not modified by this plan.** `git diff --stat b94ca1770..HEAD -- 02-CONTEXT.md` is **empty**. That is the correct outcome for a *green* run: D-03 already carried the amendment as of its 2026-08-04 revision, and this task's job was to confirm the record is still true of the code that shipped — not to write it again (the C2-20 failure) and not to correct it into agreement (the B-16 failure). Had the counts disagreed, the specified behaviour was to **HALT and report both numbers**, because a verification step whose failure mode is "edit the thing being verified" launders an authorization drift into the specification and has no failure mode at all.

### Amendment E — one scope extension, stated rather than smuggled

The plan scopes Amendment E to §13's INV-WORLD-6 **text**. Two adjacent claims in the same section are falsified by the same phase, and both were corrected in the same edit:

1. *"**All eight ship `binding: pending`**"* — INV-WORLD-5 and INV-WORLD-6 are now `bound` (plan `02-04`). Preserved as a Phase-1 statement and followed by a table of the state after Phase 2.
2. §13 allocates **eight** entries; Phase 2 hand-registered a **ninth**, `INV-PRIVACY-11` (D-07, plan `02-09`), in the existing `PRIVACY` scope. §13 now names it.

Leaving either would have made §13 state two falsehoods immediately beside the one being corrected, which is the exact failure `.claude/rules/invariants.md` names — *"never leave the registry describing a guarantee the code no longer makes"* — one artifact over.

### Amendment H — the registry, and how it was regenerated

`INV-WORLD-4`'s summary now says **THREE** sanctioned out-of-world writers, naming the third (`holomush character name set`, plan `02-12`) together with the property that qualifies it: `02-06` made `CharacterRepository.Rename` emit its outbox envelope **inside its own transaction**, so the CLI's write carries an envelope even though the command discards the returned delta. `asserted_by` gains `cmd/holomush/cmd_character_name_integration_test.go`, which observes the outbox row committed alongside the renamed row — so the amended count is **proven, not merely written**.

`docs/architecture/invariants.md` was regenerated with `go run ./cmd/inv-render` and **never hand-edited**. Idempotency was verified the only way that actually proves it on an uncommitted tree: render → snapshot → render again → `diff` byte-identical. (`git diff --exit-code -- invariants.md` against HEAD is **not** an idempotency check before the commit lands — it necessarily shows the intended change. Recorded because the plan's criterion as written is ambiguous on that point.)

| Check | Result |
| --- | --- |
| `go run ./cmd/inv-render` | exit 0 |
| re-render byte-identical to the first render | ✅ idempotent |
| `git diff --numstat -- invariants.md` | `1 1` — one generated line changed, nothing else |
| `task test -- -run 'TestEveryRegistryInvariantHasBinding\|TestProvenanceGuard\|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/` | **exit 0** — 7 tests |

---

## Task 2 — the validation map

**Plan list DERIVED from the directory**, not from prose:

```
02-01 02-02 02-03 02-04 02-05 02-06 02-07 02-08 02-09 02-10 02-11 02-12 02-13
```

Thirteen plans; **twelve** are covered (all but `02-11`, whose tasks are the closeout being audited and cannot audit themselves). **40 task rows.** The earlier scope sentence said "02-01 through 02-10" while claiming to cover every task in the phase, which silently excluded `02-12`'s three tasks and `02-13`'s four.

**RESEARCH test map: 30 rows, not 31.** Both `02-VALIDATION.md`'s seed text and this plan's own text said 31. Re-derived mechanically:

```
sed -n '1224,1256p' 02-RESEARCH.md | rg '^\| ' | rg -v 'Req / criterion' | rg -v '^\|---' | wc -l   →  30
```

**29 of 30 land fully; 1 lands partially** (row 15, PROFILE-11 criterion 4's in-world-description half, by D-29's deliberate deferral). **No row is unmapped, so no GitHub issue was filed for an unmapped row.**

One row is worth naming because it was *strengthened* rather than merely satisfied: RESEARCH row 24 phrases the fourth-rung assertion against a **`player`** floor. No `player`-rung policy is seeded, so that phrasing asks the engine about a policy that does not exist and reports DENY under **both** implementations — a gate that cannot fail, wearing the clothes of the sharpest gate in the phase. `02-08` asserts at the **`guest`** floor (where `"spectator" >= "guest"` is TRUE in Go byte order and a policy actually exists) *and* at `anonymous`, so the property proven is "a newly appended token clears **nothing**".

**RED-first table: 49 demonstrations**, count **derived per plan** (02-01→3, 02-02→7, 02-03→2, 02-04→2, 02-05→9, 02-06→1, 02-07→4, 02-08→4, 02-09→7, 02-10→2, 02-12→3, 02-13→5). The earlier draft named four in its action and demanded three in its acceptance — two numbers that disagreed with each other and both of which under-counted by an order of magnitude. **Every row records an observed non-zero exit or failure verdict; none is written in the conditional past tense.**

**`nyquist_compliant: true` is an observation, not an aspiration.** Docker was available for every plan that needed it (`02-01`, `02-05`, `02-06`, `02-12` each record it explicitly), every plan-level `task test:int` exited 0, and this plan re-ran the full lane at the phase gate: **exit 0, 11,469 tests, 7 skipped, 155.2s**. No integration criterion was left unproven for want of an environment.

**Threat-id collision, recorded not repaired.** `T-02-NN` ids are **not** globally unique across this phase: `T-02-06`, `T-02-07`, `T-02-42`, `T-02-78`, `T-02-79`, `T-02-89`, `T-02-95`, `T-02-103` and `T-02-104` each name **different** components in different plans, and `T-02-42` names two different threats inside `02-07` alone. A reference is meaningful only qualified by its plan. Renumbering ids already cited across twelve SUMMARYs would break more than it fixes.

---

## ⚠️ OUTSTANDING BLOCKING GATE — `abac-reviewer` did NOT run, and no verdict was fabricated

**This is the one acceptance criterion of this plan that is NOT met, and it must not be read as met.**

`abac-reviewer` is a repo-owned sub-agent (`.claude/agents/abac-reviewer.md`, `model: opus`, `permissionMode: plan`, read-only by construction). Invoking it requires an agent-dispatch tool. **This executor is itself a sub-agent and has no such tool** — its tool set is `Read`, `Write`, `Edit`, `Bash`, `Skill` and the Context7 MCP pair. The `/holomush-dev:review-abac` skill was invoked and resolved correctly to "dispatch `@agent-abac-reviewer`", at which point the dispatch could not be performed.

**What was deliberately NOT done:** I did not review the phase myself and record the result as the gate's verdict. The reviewer's value is that it is adversarial and independent; the author of the diff self-grading it is not that gate, and a fabricated READY is strictly worse than a recorded absence — it would close the gate permanently with nothing behind it.

**Owner:** the parent/orchestrator session, or a human, before the phase merges. D-05 makes this **mandatory**, not discretionary: `abac-reviewer` identified the §8.5.1.1 residual at the Phase 1 hand-off gate and is named by decision as the reviewer of its resolution.

**The brief it needs is written and ready to re-use verbatim:**

1. **D-02's rejection of §8.5.1.1 option 2** — does the viewer-flavored-twins shape close the residual **without trading one hole for another**? Five `seed:viewer-property-*` read twins shipped; `seed:property-owner-write` deliberately has none, asserted as the stronger claim that no `seed:viewer-*` policy carries `write` or `delete`.
2. **The term-A/term-B separation by action token** (`read_profile_attribute` vs `read`, both against `property:<id>`) — does it actually keep the two families in separate evaluations, given `combineDecisions` combines permits **disjunctively**?
3. **Open Question 1's resolution** — two `seed:profile-public-read-*` entries under one conceptual name; only `seed:profile-public-read-property` shipped, with a bare-`resource` single policy explicitly rejected and the `resource is character` half deferred by D-29.

Plus, flagged for its attention:

- The derived player-keyed peers (`owner_player_id` / `visible_to_players` / `excluded_from_players`) computed **ALL** on the permit side and **ANY** on the forbid side (D-27), and `02-10`'s control-corpus decorator.
- `internal/admin/section`'s D-06 gate-then-distinguish ordering, and its two recorded residuals: **`ADMIN_SECTION_EVALUATION_FAILED` has no test** (driving it needs a stub engine the suite deliberately forbids; Phase 4 owns the seam) and the error codes are **inline `oops.Code("…")` literals** rather than exported constants, so Phase 4 will spell them again rather than import them. Both are documented deviations, not defects.
- The D-29 deferral, recorded in `seed.go` as an absence with its four reasons.

**`crypto-reviewer` is NOT triggered, and that is a stated absence rather than a silent skip.** Nothing in this phase touches `internal/eventbus/crypto/`, `internal/eventbus/codec/`, `internal/eventbus/history/dispatcher.go`, `cold_postgres.go`, `internal/plugin/event_emitter.go::Emit`, `internal/eventbus/audit/projection.go`, a plugin manifest `crypto.emits` declaration, or a migration on `crypto_keys` / `events_audit`. Verified against the phase diff.

---

## 🔔 MAINTAINER DECISION ITEM — ROADMAP success criterion 4

**Not resolved here. This is a scope decision about a locked artifact, not a planning judgement.**

`02-CONTEXT.md` **D-29** defers `seed:profile-public-read-character` — the `resource is character` permit that would carry `characters.description` — to Phase 4, because it also gates `world.Service.GetCharacter`, whose `characterToProto` projection returns `PlayerId` and `LocationId`, and whose `principal is character` test admits **every ephemeral guest**. So **criterion 4's `entity_properties` half is discharged in Phase 2 and its in-world-description half is not.** The same split applies to PROFILE-11.

**Observed state, recorded because it changes what is actually being asked.** `.planning/ROADMAP.md`'s criterion 4 **already carries the deferral**: it reads *"An off-location viewer can read a character's **public properties** where `seed:player-character-colocation` previously **denied** it … The **in-world-description half is deferred to Phase 4** by D-29"*. This plan was written against the **pre-amendment** wording ("public properties **and in-world description**"), so the escalation it mandates is aimed at a sentence that has since moved. The question is therefore narrower than the plan assumed — *confirm* rather than *decide* — but it is still not mine to close, so all three options stand:

1. **Reword criterion 4 to the property half**, with the description half moving to Phase 4's criteria. *(The ROADMAP's current text appears to already be this option.)*
2. **Leave criterion 4 as-is** and accept that Phase 2 partially discharges it, recorded in the milestone audit.
3. **Re-open D-29** — which the phase would then need to re-plan around, since the projection narrowing it depends on is Phase 4 work.

**None is selected.** `.planning/ROADMAP.md` was **not** edited by this plan in either direction: the orchestrator owns ROADMAP updates through `gsd roadmap`, and inventing or amending structure in a tool-owned parsed file silently changes what the tool sees (`.claude/rules/planning-artifacts.md`).

**Criterion 1 is explicitly NOT in the same position and MUST NOT be touched.** D-30 records that with all three of its parts shipped, criterion 1's "rejected server-side" is true for concurrent writers **and** for the pre-existing corpus, so **its wording needs no amendment**. D-29 makes no such statement about criterion 4, which is exactly why one is escalated and the other is left alone.

---

## Deviations from Plan

### 1. [Plan defect] This plan's own closeout gate is unsatisfiable as written — the fourth instance of the phase's recurring defect class

The plan's `<verify>` and its first acceptance criterion are:

```
[ "$(rg -o 'migrations/[0-9]*_[a-z_]*\.up\.sql' 01-SPEC.md | wc -l)" -eq 0 ]
```

**It counts 6 after the amendment lands, and it cannot reach 0 without destroying correct content.** Two independent causes, both required by the plan's own instructions:

| Cause | Count | Why it is correct as-is |
| --- | --- | --- |
| `plugins/core-scenes/migrations/*.up.sql` citations | **5** | The plugin migration corpus was **not** part of Phase 01.1's goose cutover. `ls plugins/core-scenes/migrations/` shows `.up.sql` / `.down.sql` **pairs** on disk today. These citations are **accurate**; "correcting" them would make them wrong. Amendment C's own action text — *"The goose cutover collapsed **every** `.up.sql`/`.down.sql` pair"* — is over-broad. |
| §14 row 13's verbatim quotation of the superseded string | **1** | §14's own contract **requires** each row to quote its superseded text verbatim: *"The quoting is not decoration: it is what forces the search that catches a string still live somewhere nobody inventoried."* The gate counts that quote. |

**Resolved by narrowing the gate to what it actually means**, and recording both forms:

```bash
# The gate as written — counts what the plan itself requires to exist
rg -o 'migrations/[0-9]*_[a-z_]*\.up\.sql' 01-SPEC.md | wc -l          #  6

# The honest form: internal/store only, excluding §14's numbered rows
rg -v '^\| \*\*[0-9]+\.\*\*' 01-SPEC.md \
  | rg -o 'internal/store/migrations/[0-9]*_[a-z_0-9]*\.up\.sql' | wc -l  #  0  ✅
rg -v '^\| \*\*[0-9]+\.\*\*' 01-SPEC.md \
  | rg -o 'plugins/core-scenes/migrations/[0-9]*_[a-z_0-9]*\.up\.sql' | wc -l  #  5  (expected)
```

Note what the plan got **right**, and what that proves: it correctly diagnosed the `rg -c` failure mode (C2-9) and correctly used `rg -o … | wc -l` with a numeric `-eq`. **The remaining defect is orthogonal to the one it fixed** — the counting mechanics are sound and the *predicate* is wrong. That is worth naming, because "we already fixed the grep gates" is precisely the belief that lets the next one through.

### 2. [Plan defect, self-inflicted] Two of my own gates were disarmed by my own prose — instances five and six

Twice, writing a sentence asserting a trigger string is absent **introduced** it:

- `02-VALIDATION.md` asserted *"No row reads «the conditional-past phrase»"* — and spelled the phrase to do it. Gate: **2**.
- The fix explained the resolution by quoting the gate command, **including the needle**. Gate: **1**.

Resolved on the third pass by naming the property without spelling it (`02-09`'s stronger resolution, which passes **file-wide** rather than comment-stripped). Final count: **0**.

This is `02-10`'s Deviation 1 reproduced twice inside the very document written to record it. That is not embarrassing so much as diagnostic: **the trap is not carelessness, it is structural.** Any gate that matches a bare substring anywhere in a document is disarmed by any mention of its own trigger — most reliably by the sentence whose purpose is to state the trigger is gone.

### 3. `abac-reviewer` could not be dispatched

See the outstanding-gate section above. Task 3's `<action>` and two of its acceptance criteria are **not met**; no verdict was fabricated in their place.

### 4. `task test:int` was run inline rather than dispatched through `local-check`

The plan requires dispatching `task test:int` to the `local-check` agent, calling an inline invocation "a hard failure rather than a style deviation". **That instruction targets the parent session.** `.claude/rules/subagent-briefing.md` states that sub-agents *"run `task test`/`lint`/`build` inline in their own context (they are exempt from the offload deny)"*, and this executor's own briefing says the same in as many words. Dispatching is also not available to me for the reason in Deviation 3. Run inline: **exit 0, 11,469 tests, 7 skipped, 155.2s.** Read from the **exit code**, never from a matched output string.

### 5. Scope observations recorded rather than fixed

Three falsities were found adjacent to amended text and **deliberately left**, because each is a Phase-1-tense statement immediately followed by the sentence that dates it:

- §4.1: *"There is no status column on `characters` today"* — followed by *"Phase 2 adds this one."*
- §16.2: *"the `characters` table as it stands: **seven** columns, no `status`"* — the baseline `CREATE TABLE` has **six**, and `status` now exists. The count was already wrong at Phase 1; the `status` clause became wrong with `000054`.

Correcting them is not Amendment C (which is about citations) and would expand this pass's blast radius into narrative Phase-1 text. Recorded here so the next reader finds them explained rather than discovering them.

Also observed and out of scope: `internal/store/migrations/000049_world_version_guard.sql`'s own header comment cites `000001_baseline.up.sql:269` — the same staleness, inside a migration file rather than in `01-SPEC.md`.

---

## The lesson worth carrying forward

**Acceptance criteria built on `rg` substring counts keep being self-defeating in this phase — six instances now, and the count is the finding.**

| # | Plan | The gate | Why it could not pass |
| --- | --- | --- | --- |
| 1 | `02-07` | `rg -o 'resource is character' seed.go \| wc -l -eq 0` | Counted **3 before and after**: shipped policies end with `resource is character)` and `resource is character_directory` matches the substring. Satisfying it literally meant **deleting shipped policies**. |
| 2 | `02-08` | `rg -o 'policytest\|createSeedEngine' \| wc -l -eq 0`, file-wide | The same plan **required the file to document why it avoids those symbols**. |
| 3 | `02-10` | `rg -o '## Remediation verdict' \| wc -l -ge 1` | Satisfied by prose **stating the section was deliberately absent**. |
| 4 | `02-11` | `rg -o 'migrations/…\.up\.sql' \| wc -l -eq 0` | Counts 5 **correct** plugin citations plus §14's **required** verbatim quote. |
| 5–6 | `02-11` | `rg -io '<conditional-past phrase>' \| wc -l -eq 0` | Disarmed twice by the sentence asserting the phrase's absence, and once more by the sentence explaining the fix. |

The counting mechanics are now well understood in this repo and are **not** the remaining problem: anchor to line start, count with `rg -o … | wc -l`, compare numerically with `-eq`, and never `rg -c` (which prints nothing and exits 1 on exactly the zero matches that mean success). **The remaining problem is the predicate.** Three rules earned the hard way:

1. **Ask what the gate would cost to satisfy.** If the cheapest way to reach 0 is deleting correct content, the predicate is wrong — not the content. (Instances 1 and 4.)
2. **A gate over a document that must also DISCUSS the guarded thing is self-contradictory as written.** Resolve it by naming the property instead of spelling it — which passes file-wide and is strictly stronger than comment-stripping. (Instances 2, 5, 6.)
3. **Anchor to structure, not to substrings.** `^## Remediation verdict$` matches a heading; `## Remediation verdict` matches a sentence about one. Prefer a compiled/parsed target over a text match whenever one exists — `02-07` replaced an unsatisfiable grep with `TestNoPhase2SeedIntroducesACharacterResourceTypePermit`, which is immune to **both** of the grep's failure modes. (Instance 3.)

---

## Verification

| Gate | Command | Result |
| --- | --- | --- |
| Plan `<verify>` (Task 3) | `task pr-prep` — **inline** | **exit 0**, `status=pass`, `lane=fast` (result file at `…/runs/20260805T200604Z-96065.result`) |
| Whole-repo integration | `task test:int` | **exit 0** — 11,469 tests, 7 skipped, 155.2s |
| Invariant meta-tests | `task test -- -run 'TestEveryRegistryInvariantHasBinding\|TestProvenanceGuard\|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/` | **exit 0** — 7 tests |
| Render | `go run ./cmd/inv-render` | exit 0; re-render byte-identical (idempotent) |
| Project rule | `task fmt` then `task fmt:check` | **exit 0**; formatter edits committed with their tasks |
| Task 1 AC | internal/store `.up.sql`, §14 rows excluded | **0** |
| Task 1 AC | `rg -n 'option 2' 01-SPEC.md` shows it rejected; five twins named | ✅ / **5** |
| Task 1 AC | §11.3 `player_id` row byte-identical | ✅ |
| Task 1 AC | `rg -A 4 'INV-WORLD-6' \| rg -o 'ONLY' \| wc -l` (body) | **0** |
| Task 1 AC | §8.4.1 five rows incl. `roles` / `has_roles` | ✅ |
| Task 1 AC | D-03 count vs `seed.go` | **2 == 2**, same two names |
| Task 1 AC | `git diff --stat 02-CONTEXT.md` — D-03 NOT modified | **empty** ✅ |
| Task 1 AC | §14 numbered rows | **18** (10 + 8) |
| Task 1 AC | INV-WORLD-4: `exactly TWO sanctioned` / `exactly THREE sanctioned` | **0** / **1** |
| Task 2 AC | `rg -o 'pending — allocated when' 02-VALIDATION.md \| wc -l` | **0** |
| Task 2 AC | task rows / RED-first rows | **40** / **49** |
| Task 2 AC | conditional-past phrase, file-wide | **0** |
| Task 2 AC | `rg -n 'criterion 4' 02-VALIDATION.md` | matches (4×) |
| Task 3 AC | `git status` after `task fmt` | **clean** |
| Task 3 AC | `[ci skip]` or equivalent in this plan's commits | **0** |
| Threat T-02-70 | `git diff --stat ROADMAP.md STATE.md` from Tasks 1–3 | **empty** — no version-bearing or check-marked `###` heading added anywhere |

Every pass/fail above was read from the **exit code**, never from a matched output string.

---

## Threat mitigations applied

| Threat | Disposition | Where it landed |
| --- | --- | --- |
| T-02-67 (unapplied amendment) | mitigate | All eight applied **in place** and proven by a recorded single-line search over a scope that excludes §14's own required quotations. §8.5.1.1 no longer presents option 2 as live. |
| T-02-68 (unreviewed authorization surface) | **NOT CLOSED** | `abac-reviewer` could not be dispatched from this context. **Recorded as an outstanding blocking pre-merge gate with a named owner and a ready-to-reuse brief — not silently skipped, and not closed by a fabricated verdict.** |
| T-02-69 (projected rather than observed RED demonstrations) | mitigate | All **49** demonstrations verified as recording an observed non-zero exit or failure verdict; none written in the conditional past tense. |
| T-02-70 (invented structure in a parsed planning file) | mitigate | This plan wrote only `01-SPEC.md`, `02-VALIDATION.md` and the invariant registry. `git diff --stat` on `ROADMAP.md` and `STATE.md` from Tasks 1–3 is **empty**. `02-CONTEXT.md` was in scope but correctly **not** written to. |
| T-02-71 (overstated compliance flag) | mitigate | `nyquist_compliant: true` set from an observed full integration run (exit 0, 11,469 tests), with every unproven criterion named in the unmet-criteria table rather than absorbed into the flag. |
| T-02-72 (`[ci skip]` on a ship-note commit) | mitigate | **0** occurrences across this plan's commits, verified over subjects and bodies. |

---

## Known Stubs

None. This plan ships no code.

Two things are **outstanding rather than stubbed**, each with a named owner:

| Item | Owner |
| --- | --- |
| `abac-reviewer`'s verdict on the phase's authorization surface (D-05, **mandatory** before merge) | the parent/orchestrator session or a human — brief written above |
| ROADMAP criterion 4's scope confirmation | the maintainer — three options stated, none selected |

---

## Invariant registry

This plan **registers no new invariant** and writes **no `// Verifies:` annotation**. It amends **one existing entry's text** — `INV-WORLD-4`, TWO → THREE out-of-world writers — which is an **edit, not a renumber**: the id, the scope and the `legacy:` list are unchanged, because canonical ids are referenced from tests and other specs. `asserted_by` gained one entry, so the amended count is asserted rather than merely written. No ad-hoc family was minted (`rg -o 'INV-PORTAL-|INV-NAME-|INV-ADMIN-' invariants.yaml | wc -l` → 0).

**Phase 2's final registry state:** three entries `bound` — `INV-WORLD-5` and `INV-WORLD-6` (plan `02-04`), `INV-PRIVACY-11` (plan `02-09`) — and five staying `pending` **with no `asserted_by`**: `INV-ACCESS-10`, `INV-ACCESS-11`, `INV-ACCESS-12`, `INV-PRIVACY-9` and `INV-PRIVACY-10` all bind in Phase 4, and `INV-WORLD-7` in Phase 4 with its commands landing in Phase 3. All sit in existing scopes.

## Requirements bookkeeping

`.planning/REQUIREMENTS.md` is **internally inconsistent for this whole phase, and was NOT hand-edited.** `requirements.mark-complete` has no partial-credit model and seven plans share `EXT-07` / `PROFILE-11`, so the first claiming plan to finish (`02-03`, wave 2) flipped both checkboxes for all of them. Every subsequent plan's run returned `{"updated": false, "table_unmatched": [...], "write_set_complete": false}`. The result: **checkboxes read `[x]` while the traceability-table rows read `Pending`** — the two halves of one artifact disagree, and **the table is the half that happens to be right**.

Recording the state plainly, as the briefing directs: `IDENT-06`, `IDENT-07`, `IDENT-08` and `IDENT-09` are genuinely discharged by this phase. `EXT-07` is **not** fully discharged (the endpoint-level denial test is Phase 4's per D-08; EXT-04's registry ↔ descriptor census is Phase 6's per §12.2). `PROFILE-11` is **not** fully discharged (the `characters.description` half is Phase 4's per D-29). `.planning/REQUIREMENTS.md` is a tool-owned parsed artifact and `.claude/rules/planning-artifacts.md` forbids hand-editing tool-owned files to work around tool behaviour; reporting the gap is the sanctioned path.

## Phase hand-off

- **Phase 3** inherits one rule already carried in `Rename`'s doc comment and repeated here because it is easy to undo: **`CharacterRepository.Rename` MUST NOT be routed through `worldMutator.mutate()`.** `Rename` writes its own outbox envelope inside its own transaction, so routing it through the mutator emits **two** envelopes per rename. Phase 3 also owns `last_active_at`'s write seam — which **MUST NOT** hook `RefreshConnection` (`internal/session/session.go:485`), or every character becomes a hot write per lease interval.
- **Phase 4** inherits `seed:profile-public-read-character` and the projection narrowing that must land with it; D-27's recorded consequence that the viewer path can be **narrower** than the grid for identity-keyed rows; the endpoint-level §10.2 denial test; the seam for the untested `ADMIN_SECTION_EVALUATION_FAILED` path; and the binding of `INV-ACCESS-10/11/12`, `INV-PRIVACY-9/10` and `INV-WORLD-7` against the read path and its **marshaled response**.
- **Phase 6** inherits §11.3's two new sort keys, the seven section ids and their descriptors, and EXT-04's census.
- **Before merge:** `abac-reviewer` (blocking), and the criterion-4 confirmation.

## Self-Check: PASSED

Files verified present on disk:

- `.planning/phases/01-portal-spec/01-SPEC.md` — FOUND
- `.planning/phases/02-abac-schema-vocabulary/02-VALIDATION.md` — FOUND
- `docs/architecture/invariants.yaml` — FOUND
- `docs/architecture/invariants.md` — FOUND

Commits verified via `git cat-file -e`:

- `16ae7cca6` — FOUND
- `ccc03e6c7` — FOUND

`02-CONTEXT.md` verified **unmodified** by this plan (the correct outcome for a green Amendment G run). `.planning/ROADMAP.md` and `.planning/STATE.md` verified unmodified by Tasks 1–3. Working tree clean before this document.

---
*Phase: 02-abac-schema-vocabulary*
*Completed: 2026-08-05*
