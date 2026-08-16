---
phase: 01-portal-spec
plan: 05
subsystem: planning-spec
tags: [spec, verification-integrity, amendments, out-of-scope, portal-10]
status: complete

requires:
  - "01-SPEC.md §§2-11 (waves 1-4) — §12 rule 5 is reconciled against §9.6.1; §14 rows 6-9 are the corrections waves 1-4 forced"
  - ".planning/REQUIREMENTS.md PORTAL-10 — the six rules' source and ordering"
  - ".planning/phases/01-portal-spec/01-CONTEXT.md D-14, D-17 — the binding mechanism and the recorded divergence"
provides:
  - "01-SPEC.md §12 — the six verification-integrity rules, copied verbatim into every v0.13 PLAN.md from Phase 2 onward"
  - "01-SPEC.md §14 — the nine amendments plus the D-14 divergence, each applied to its artifact"
  - "01-SPEC.md §15 — the milestone's out-of-scope set as a first-class numbered section"
  - "ROADMAP.md and REQUIREMENTS.md brought into agreement with the SPEC, so a downstream planner reading them directly is not planning to a superseded criterion"
affects:
  - "Phases 2-6: each PLAN.md copies §12's six-rule block and specializes it"
  - "Phase 4: census scope ownership fixed by §12.2; PORTAL-02 now says four export surfaces"
  - "Phase 6: ADMIN-06 durability boundary moved to the outbox envelope; rule 5 restated at the Phase 6 criterion"

tech-stack:
  added: []
  patterns:
    - "three-column amendment table (Artifact+superseded text / v0.13 SPEC / Rationale) per 01-PATTERNS.md §2"
    - "dated research records annotated superseded-by in place, never rewritten"
    - "absence proven with `! rg -q PATTERN FILE`, never a positive rg plus `test $? -eq 1`"

key-files:
  created: []
  modified:
    - ".planning/phases/01-portal-spec/01-SPEC.md (§12, §14, §15 authored; two drifted citations corrected)"
    - ".planning/ROADMAP.md (7 success-criteria/preamble lines restated; zero bookkeeping lines touched)"
    - ".planning/REQUIREMENTS.md (PORTAL-02, PORTAL-10 rule 5, IDENT-09, PROFILE-12, ADMIN-06)"
    - ".planning/research/SUMMARY.md (3 superseded-by annotations appended; no original text altered)"
    - ".planning/research/PITFALLS.md (2 superseded-by annotations appended; no original text altered)"

decisions:
  - "§12 rule 5 restated around the wire rather than propagated as written — the original prescription asserts the deepest chain code and passes while the wire leaks, and §12 copies it into five downstream plans"
  - "The `.claude/rules/grpc-errors.md` half of amendment 8 is recorded but deliberately NOT applied — §9.6.1 already committed to documenting current behavior without changing the rule file, and #4902 owns that edit"
  - "research/PITFALLS.md annotated despite being outside the plan's declared files_modified — it carries the same false rule-5 prescription and Phase 6 reads it under a --research-phase flag"
  - "STATE.md:60 carries the superseded rule-5 text and was NOT edited — the shared-file carve-out forbids it; escalated to the orchestrator below"

metrics:
  duration: ~50m
  tasks: 3
  commits: 4
  files: 5
  completed: 2026-08-01

actuals:
  tokens: 46000
  tasks: 3
  commits: 4
---

# Phase 01 Plan 05: Verification Integrity, Amendments and Out of Scope — Summary

Authored the SPEC's three closing normative sections and then made the four live
planning artifacts stop contradicting them — with each superseded string proven
gone by a search demonstrated red before the edit rather than asserted after it.

## What was built

| Section | Content |
| --- | --- |
| **§12 Verification Integrity** | The six rules as a numbered list 1-6 in PORTAL-10's order, each self-contained for verbatim copy, each carrying a non-vacuity clause. Plus §12.2 (binding mechanism, both rejected alternatives) and §12.3 (test plan by tier, using `.claude/rules/testing.md`'s exact tier names). |
| **§14 Amendments and Divergences** | A three-column table with **ten** data rows — nine amendments plus the D-14 grid-parity divergence — each quoting the superseded text verbatim, stating the replacement, and giving a rationale. Closes with §14.1 naming PROFILE-03/04/05 as unamended. |
| **§15 Out of Scope** | A numbered section: §15.1 carries all fifteen REQUIREMENTS exclusions with their reasons, §15.2 the four phase deferrals with the seam each leaves, §15.3 role mutation stated emphatically, §15.4 proto reservation named as hygiene with the three real carriers of the extensibility constraint. |

## Rule 5 was reconciled, not propagated

This was the plan's load-bearing risk. `.planning/REQUIREMENTS.md` PORTAL-10
rule 5 mandated *"Top-level oops code assertions via `oops.AsOops(err).Code()`;
`errutil.AssertErrorCode` chain-walks and passes on double-wrap."* Both halves
are false, re-verified at the source rather than relayed:

- `pkg/errutil/testing.go:15-20` — `AssertErrorCode` **is** `oops.AsOops(err)`
  followed by `oopsErr.Code()`. The two spellings the rule contrasts are the same
  call.
- `$(go env GOMODCACHE)/github.com/samber/oops@v1.22.0/error.go:230-235` —
  *"Code returns the error code from the **deepest** error in the chain"*,
  implemented as `getDeepestErrorCode` at `:237`. Both spellings therefore return
  the inner code on a double-wrap while the wire carries the outer one.

§12 copies its block into every v0.13 plan from Phase 2 onward, so shipping rule
5 as written would have seeded an assertion that cannot fail on the leak it
exists to catch into five downstream phases. §12.1 rule 5 now mandates the
wire-level form §9.6.1 already specifies — `status.Code(err)`, a generic
`status.Convert(err).Message()` with no internal code string in it, and a
differential assertion where the contract is indistinguishability — keeps
`errutil.AssertErrorCode` endorsed for asserting *which* internal code was
produced, and cites **#4902**. §14 row 8 carries the amendment; the four
restatement sites in ROADMAP.md and REQUIREMENTS.md were all rewritten.

## Amendment landing proofs

Task 3's gate was run against the **untouched tree at `511db010e` before any
artifact was edited** and exited **1** — PORTAL-10 rule 4 applied to this task's
own gate. Critically, its `go run ./cmd/inv-render -check` component exited **0**
in the same run, so the RED came from the two absence checks and not from a
broken tool. Post-edit the same command exits **0**.

Every absence check below is written `! rg -q PATTERN FILE`. No positive `rg`
followed by `test $? -eq N` was used anywhere, and no two exit-status comparisons
were chained.

| # | Amendment | Artifact touched | Single-line search target | Result |
| --- | --- | --- | --- | --- |
| 1 | ROADMAP Phase 4 crit 3 — owner-set visibility → game-configured tier floor | `.planning/ROADMAP.md:213` | `An owner can set any profile field` | absent ✓ (exhaustive-`switch`-with-`default: deny` clause verified still present) |
| 2 | ROADMAP Phase 5 crit 4 — owner flips → configuration change on next read | `.planning/ROADMAP.md:233` | `An owner flips a field between` | absent ✓ |
| 3 | REQUIREMENTS PROFILE-12 — not-retroactive statement re-seated | `.planning/REQUIREMENTS.md:136` | `The visibility toggle and the retirement flow` | absent ✓ (retirement half unchanged) |
| 4 | research CONFLICT 4 — UI expression superseded, tier vocabulary survives | `.planning/research/SUMMARY.md` | original clause **retained** + `SUPERSEDED` annotation follows | both present ✓ |
| 5 | `INV-PRIVACY` scope `boundary:` widened | `docs/architecture/invariants.yaml` | `gating on history reads` | absent ✓ (landed by plan 01-01; `inv-render -check` exits 0) |
| 6 | PORTAL-02 / ROADMAP Phase 1 crit 1 — three → four export surfaces | `.planning/REQUIREMENTS.md:31`, `.planning/ROADMAP.md:132` | `three existing public export surfaces` | absent from both ✓ |
| 7 | IDENT-09 — one query and **two** writers, not two writers | `.planning/REQUIREMENTS.md:97`, `.planning/research/SUMMARY.md` | ``adapters.go:38-50` and `internal/auth/character_service.go:112-121`.`` | absent from REQUIREMENTS ✓; research annotated, original retained ✓ |
| 8 | PORTAL-10 rule 5 — restated around the wire | `.planning/REQUIREMENTS.md:69`, `.planning/ROADMAP.md:119`, `:136`, `:250` | `Top-level oops code assertions` / `oops\.AsOops` | prescriptive string absent from REQUIREMENTS ✓; `oops.AsOops` absent from ROADMAP entirely ✓ |
| 9 | ADMIN-06 / ROADMAP Phase 6 crit 3 — durability boundary → outbox envelope | `.planning/REQUIREMENTS.md:152`, `.planning/ROADMAP.md:252` | `` the existing `events_audit` `` / `` writes an `events_audit` row `` | absent from both ✓ |

**A first coarse check failed and was correct to fail.** `! rg -q 'oops\.AsOops'
.planning/REQUIREMENTS.md` reported the string still live — because the restated
rule 5 deliberately *names* `oops.AsOops(err).Code()` while explaining why it does
not work. The check was refined to the prescriptive string
(`Top-level oops code assertions`, absent ✓) and every surviving occurrence in the
tree was then classified by hand rather than by pattern.

## Full-tree sweep: one uninventoried copy found, and it is out of my reach

Sweeping all of `.planning/` for each superseded string, every match resolves to
one of: `01-SPEC.md` §14's deliberate verbatim quotations; a dated phase artifact
(`01-CONTEXT.md`, `01-PATTERNS.md`, the wave PLANs and SUMMARYs — `01-05-PLAN.md`
itself contains two of the strings inside its own verify command); the archived
v0.11/v0.12 milestone tree; or my own corrective text. One exception:

> **`.planning/STATE.md:60` still carries the superseded rule-5 prescription** —
> *"top-level `oops.AsOops(err).Code()` assertions; invariant-scope discipline…"*.
> **Not edited.** My shared-file carve-out forbids touching STATE.md at all, and
> the carve-out's own instruction for this case is to stop and record it here.
> **Action for the orchestrator:** restate that clause to
> *"wire-level assertion of every opacity and authorization contract"* to match
> `ROADMAP.md:119`, or confirm STATE.md's milestone-summary text is not read as a
> directive by downstream planners.

Two further items for whoever owns the follow-up:

- **`.planning/ROADMAP.md:159`** — the wave-5 plan checkbox still reads
  *"applying the **four** amendments"*. It is nine. A plan checkbox is GSD
  bookkeeping, which the carve-out reserves to the orchestrator, so it was left
  alone.
- **`01-SPEC.md:396-402`** (wave 2's content) describes
  `WebListPublishedScenes`' `participants_snapshot` as carrying *"the same frozen
  names"*. Wave 3 established it carries **ids** today (`publish_store.go:988`
  selects `character_id`; tracked as **#4901**), and §5.4 plus §14 row 6 both
  carry that correction — but a reader of §3 alone would not see it. Flagged for
  **plan 01-06**, which owns the citation-verification sweep and completeness
  gate; not re-litigated here because §3 is committed wave-2 content.

## Citation verification

All 40 distinct `path:line` citations introduced in §§12/14/15 were resolved
against the tree rather than trusted from notes. **Two had drifted** and were
corrected in a fourth commit:

| Citation | Was | Is |
| --- | --- | --- |
| REQUIREMENTS.md PORTAL-10 block | `:49-71` | `:51-78` — this plan's own rule-5 and PORTAL-02 edits shifted it |
| `internal/admin/auth/ingame.go` *"RoleAdmin (any character)"* | `:115` (as recorded in REQUIREMENTS.md and the research summary) | `:116` |

The rest resolve exactly, including `web.proto:339`, `scene.proto:1053`,
`publish_store.go:988`, `guest_service.go:227`, `projection.go:319-331` and
`:434`, `retention_partitions.go:546`, `role_store.go:83-93`,
`errutil/testing.go:15-20`, `go.mod:32`, and
`test/meta/invariant_registry_test.go:341`.

## Deviations from Plan

### Auto-fixed / scope additions

**1. [Rule 2 — Missing critical] `.planning/research/PITFALLS.md` annotated, though outside the plan's declared `files_modified`**

- **Found during:** Task 3's full-tree sweep.
- **Issue:** PITFALLS carries the same false rule-5 prescription in two places —
  `:406-408` (Pitfall 6's fix paragraph) and the *"Looks Done But Isn't"*
  checklist. The plan scoped research annotation to `SUMMARY.md` only. Leaving a
  false prescription live in a document Phase 6 reads under a `--research-phase`
  flag would have left the amendment incomplete in exactly the way §14's own
  framing warns about.
- **Fix:** two superseded-by annotations appended; no original text altered, per
  the dated-record rule.
- **Commit:** `9257599d7`.

**2. [Rule 1 — Correctness] Two drifted `path:line` citations corrected**

- **Found during:** the citation sweep (see above).
- **Fix:** `:49-71` → `:51-78`; `ingame.go:115` → `:116`.
- **Commit:** `3a8f68415`.

### Deliberate non-applications

**`.claude/rules/grpc-errors.md` is amended in the record but not in the file.**
Its §*"Wire opacity needs TOP-LEVEL code assertions"* asserts the same false
thing as rule 5. §9.6.1 — committed in wave 4 — already states that this SPEC
*"documents current behavior and does not change the rule"*, and issue **#4902**
owns the rule-file edit. Editing it here would contradict a settled section and
push a SPEC-authoring phase into repo rule files. §14 row 8 records the amendment
and says so explicitly.

## Verification Results

| Gate | Command | Result |
| --- | --- | --- |
| Task 1 | `awk` §12 slice → `rg -c '^[0-9]\. ' ≥ 6` and `rg -c 'set equality'` | exit 0 ✓ |
| Task 2 | `awk` §14 slice → `rg -c '^\|' ≥ 7` and `rg -c 'PROFILE-03'` | exit 0 ✓ (10 data rows) |
| Task 3 pre-edit | `inv-render -check && ! rg -q … && ! rg -q …` at `511db010e` | **exit 1 — RED as required** ✓ |
| Task 3 post-edit | same command | exit 0 ✓ |
| No placeholders | `! rg -q 'Placeholder — authored by plan 01-05'` | absent ✓ |
| STATE.md untouched | `git diff --name-only .planning/STATE.md` | empty ✓ |
| ROADMAP bookkeeping untouched | diff scanned for Progress-table rows, checkboxes, status markers, wave annotations | zero such lines in the diff ✓ |

`.planning/` is excluded from `task lint:markdown`
(`Taskfile.yaml:764` — `rumdl check --exclude '…,.planning'`), and no file under
`docs/` changed, so no repo lint surface was touched.

## Known Stubs

None. §12, §14 and §15 are authored to completion; §16 is plan 01-06's.

## Commits

| Commit | Task | Message |
| --- | --- | --- |
| `3ca440fd7` | 1 | author SPEC section 12, the six verification-integrity rules |
| `511db010e` | 2 | author SPEC sections 14 and 15, amendments and out of scope |
| `9257599d7` | 3 | apply the nine SPEC amendments to their live artifacts |
| `3a8f68415` | 3 | correct two drifted citations in sections 12 and 15 |

## Self-Check: PASSED

- `.planning/phases/01-portal-spec/01-SPEC.md` — FOUND (§12 at :2169, §14 at :2468, §15 at :2534)
- `.planning/ROADMAP.md` — FOUND, 7 prose lines restated
- `.planning/REQUIREMENTS.md` — FOUND, 5 requirements restated
- `.planning/research/SUMMARY.md` — FOUND, 3 annotations, originals retained
- `.planning/research/PITFALLS.md` — FOUND, 2 annotations, originals retained
- Commits `3ca440fd7`, `511db010e`, `9257599d7`, `3a8f68415` — all FOUND in `git log`
