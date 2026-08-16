---
phase: 04-shared-facade-helpers-characteraccessservice
plan: 03
subsystem: planning-artifacts
tags: [spec-amendment, roadmap, sketch-findings, docs-only]
status: complete

requires:
  - "Phase 3 D-44 (rename removed from v0.13, deferred to backlog 999.20)"
  - "04-CONTEXT D-74 (strike the RenameCharacter row), D-81 (the three sketch verdicts), D-72 (admin RPCs deferred to Phase 6)"
provides:
  - "01-SPEC §9.3 mutation surface that matches what Phase 4 will declare in the proto (no RenameCharacter member for plan 04-08's descriptor census to expect)"
  - "Recorded verdicts for all three Phase-4 ROADMAP sketch findings"
affects:
  - "04-08 (descriptor census compares SPEC §9.3 against the proto)"
  - "Phase 6 (inherits A2/A3 as ACCEPTED design, to be implemented there)"

tech-stack:
  added: []
  patterns:
    - "Value-fill discipline on tool-owned planning artifacts: heading counts measured before and after as the direct evidence that no structure was invented"

key-files:
  created:
    - .planning/phases/04-shared-facade-helpers-characteraccessservice/04-03-SUMMARY.md
  modified:
    - .planning/phases/01-portal-spec/01-SPEC.md
    - .planning/ROADMAP.md

decisions:
  - "D-74 applied: 01-SPEC §9.3 carries no CharacterAccessService.RenameCharacter row, and both prose sentences that explained creation's concurrency guard by reference to it now source that guard to §6.1.3 directly"
  - "D-81 recorded: A3 ACCEPTED (Phase 6), A2's RPC half ACCEPTED (Phase 6), admin rename census WITHDRAWN — none of the three causes Phase-4 code"
  - "The Phase-4 plan-count correction (cycle-6 L4) was already discharged by the GSD roadmap tooling; forcing the plan's literal string would have replaced a tool-written shape with a hand-invented one"

metrics:
  duration: ~20min
  completed: 2026-08-11

actuals:
  tokens: 1327
  tasks: 2
  commits: 2
---

# Phase 04 Plan 03: 01-SPEC Rename Amendment & Sketch Verdicts Summary

Struck `CharacterAccessService.RenameCharacter` from 01-SPEC §9.3 and re-sourced both
dangling prose references to §6.1.3's unique index, then recorded all three Phase-4
sketch-finding verdicts in the ROADMAP — two ACCEPTED-for-Phase-6, one WITHDRAWN, no
code written for any of them.

## What Was Built

Two planning artifacts amended; no symbols created and no source file touched.

### Task 1 — 01-SPEC §9.3 / §9.4.2 (commit `bbaec01f9`)

Three narrowly-scoped `Edit` replacements:

1. **The §9.3 table row deleted.** The `CharacterAccessService.RenameCharacter` row is
   gone; nothing replaces it. The mutation table has one fewer row.
2. **§9.3's opening paragraph repaired.** It named §6.1.3's `UNIQUE` index as what guards
   a create and then identified it as "the same index the `RenameCharacter` row below
   collides against". The trailing clause now names the deferral instead — rename left
   v0.13 on 2026-08-06 for backlog Phase 999.20 per Phase 3 D-44, recorded against
   IDENT-03 in REQUIREMENTS — so a later reader can trace why the row left (T-04-14).
   The sentence's argument is intact: the index is still what decides two concurrent
   creates.
3. **§9.4.2's prose repaired.** The paragraph kept its conclusion — creation's concurrency
   safety is already specified, it is not missing, and Phase 4 MUST NOT invent a
   version-shaped substitute — but now sources that specification to §6.1.3 directly
   rather than to "the section that specifies it for rename".

### Task 2 — ROADMAP Phase-4 sketch findings (commit `78dca4f31`)

The three D-81 verdicts appended to the **existing** `**Sketch findings**` paragraph in
the Phase 4 block. One line changed; no heading, list, table, or section added.

| Finding | Verdict | Where it lands |
| --- | --- | --- |
| **A3** — `AdminSearchCharacters` (§9.2) extended to player usernames | **ACCEPTED** as the design | Phase 6 (D-72 defers the admin RPCs). No Phase-4 code. |
| **A2's RPC half** — admin list RPC accepts a sort key for the joined `players.username` | **ACCEPTED** as the design | Phase 6. §11.3's row was already amended in Phase 2 by D-26. |
| **Admin rename census** | **WITHDRAWN** | Nowhere. Phase 3 D-44 removed rename from v0.13, so sketch 004's `Rename…` affordance is not live and sketch 009 finding #5 ("names are reserved, not permanent") is false for v0.13. |

## Heading-count evidence (the anti-structure measurement)

Measured directly on each file before and after the edit. These are the numbers the
milestone extractor keys off; all six are unchanged, which is what proves no heading of
any level was added.

| File | Metric | Pre-edit | Post-edit | Pinned in plan |
| --- | --- | --- | --- | --- |
| `01-SPEC.md` | `^#{1,6} ` total | 116 | 116 | 116 ✓ |
| `01-SPEC.md` | `^## ` | 16 | 16 | 16 ✓ |
| `01-SPEC.md` | `^### ` | 81 | 81 | 81 ✓ |
| `ROADMAP.md` | `^#{1,6} ` total | 36 | 36 | 36 ✓ |
| `ROADMAP.md` | `^### ` | 28 | 28 | 28 ✓ |
| `ROADMAP.md` | `^### Phase ` | 27 | 27 | 27 ✓ |

No drift from the pinned values — every pre-edit measurement matched the plan's literal.

**Additional live check.** Beyond the counts, the milestone phase filter was executed
against the amended ROADMAP:

```text
milestone phases in scope: 7
01-portal-spec, 01.1-migration-framework-adopt-goose-for-go-migrations,
02-abac-schema-vocabulary, 02.1-world-caller-model,
02.2-background-job-authorization-model, 03-world-character-commands,
04-shared-facade-helpers-characteraccessservice
```

All seven v0.13 phase directories still resolve inside the active milestone — the T-04-13
tampering risk (a `###` heading silently truncating the milestone scope) did not
materialize, verified by execution rather than by inspection.

## Observations the plan asked to be recorded

**No `AdminRenameCharacter` row existed at HEAD.** Confirmed by
`git show HEAD:.planning/phases/01-portal-spec/01-SPEC.md | rg -c 'AdminRenameCharacter'`
→ zero. D-81's withdrawal of the admin rename census therefore required no SPEC edit, and
no row was invented in order to delete one.

**`RenameCharacter` is gone from 01-SPEC entirely** — `rg -c` reports no matches. The
word "rename" survives elsewhere in the SPEC in contexts outside D-74's scope (§6.1.3's
heading "the index that must land with rename", §9.6's `CHARACTER_NAME_TAKEN` row, §14's
correction table). Those are not references to the struck row and the plan did not scope
them.

## Deviations from Plan

### 1. [Rule 3 — stale precondition] The Phase-4 plan-count correction was already discharged by the tooling

- **Found during:** Task 2.
- **Issue:** The plan directed changing `**Plans**: 8 plans` to `**Plans**: 9 plans`
  (cycle-6 L4, plan-time ROADMAP.md:484). At execution the line reads
  `**Plans**: 1/9 plans executed`, and no `8 plans` string exists anywhere in the
  ROADMAP.
- **Cause:** Plan 04-01 executed between plan-authoring and this run, and its
  `roadmap update-plan-progress` step rewrote the line into the executed-phase form the
  tool emits (compare the shipped phases' `6/6 plans executed`, `13/13 plans executed`).
- **Resolution:** No edit made. The criterion's substance — *the count agrees with the
  nine plans enumerated beneath it* — is satisfied, by the tool's own verb, which is
  exactly the resolution order the plan prescribed ("if the GSD roadmap tooling exposes a
  verb that updates the plan count, use it"). Forcing the literal `**Plans**: 9 plans`
  would have replaced a tool-written shape with a hand-invented one and destroyed the
  executed count, which the next `roadmap update-plan-progress` would overwrite anyway —
  the precise failure mode `.claude/rules/planning-artifacts.md` exists to prevent.
- **Files modified:** none.
- **Commit:** n/a.

### 2. [Rule 1 — defective check] Task 2's `<verify>` counts matching *lines*, not occurrences

- **Found during:** Task 2 verification.
- **Issue:** The clause `test "$(rg -c 'ACCEPTED' .planning/ROADMAP.md)" -ge 2` fails
  against a correct artifact. `rg -c` reports **matching lines**, and both ACCEPTED
  verdicts land on the same line — because the same task mandates appending to the
  existing single-line paragraph rather than adding list structure. The two constraints
  are in tension, and the check loses.
- **Resolution:** Verified by occurrence count instead, per `.claude/rules/grepping.md`
  ("count occurrences with `-o | wc -l` not `-c`"):
  `rg -o 'ACCEPTED' .planning/ROADMAP.md | wc -l` → **2**. Every other clause of the
  verify command passes as written. The artifact is correct; the check is not. No content
  was reshaped to satisfy a buggy assertion.
- **Files modified:** none.
- **Commit:** n/a.

### 3. [Rule 1 — false completion claim] `requirements mark-complete IDENT-02` reverted

- **Found during:** state updates, after task execution.
- **Issue:** The plan's frontmatter carries `requirements: [IDENT-02]`, so the executor's
  state-update step ran `requirements mark-complete IDENT-02`. It flipped the checkbox at
  `.planning/REQUIREMENTS.md:99` to `- [x]`. **IDENT-02 is "a player can edit their
  character's prose fields with server-enforced length caps" — this plan implements none
  of it.** It amended a SPEC section and recorded three ROADMAP verdicts. The caps land in
  the facade handler per D-82, in a later Phase-4 plan.
- **Secondary symptom:** the verb reported `table_unmatched: ["IDENT-02"]` and
  `write_set_complete: false` — it flipped the checkbox but could not flip the
  traceability row, leaving REQUIREMENTS internally inconsistent (checkbox complete, table
  `Pending`).
- **Resolution:** reverted with `git checkout -- .planning/REQUIREMENTS.md`; IDENT-02 is
  `- [ ]` again and the file is unmodified by this plan. The frontmatter tag records which
  requirement this amendment *serves*, not one it *delivers*; a completion mark is a claim
  about delivery. Marking it would have made the next plan's requirement audit read
  green for work nobody did — the same false-provenance failure `.claude/rules/invariants.md`
  forbids for `// Verifies:` annotations.
- **Files modified:** none (net zero — flipped, then restored).
- **Commit:** n/a — the false edit never reached a commit.

## Verification

| Check | Result |
| --- | --- |
| `rg -c 'RenameCharacter' .planning/phases/01-portal-spec/01-SPEC.md` | no matches (exit 1) ✓ |
| `rg -n '6\.1\.3' …` matches in both §9.3 opening (line 2012) and §9.4.2 (lines 2114, 2116) | ✓ |
| `git diff --stat -- …/01-SPEC.md` | 7 insertions, 8 deletions — net −1, well under the ≤8 bound ✓ |
| 01-SPEC heading counts 116 / 16 / 81 | unchanged ✓ |
| `rg -n 'WITHDRAWN' .planning/ROADMAP.md` matches inside the Phase 4 section | ✓ |
| `Phase 6` appears in the amended paragraph for both A2 and A3 | 2 occurrences on line 487 ✓ |
| ROADMAP heading counts 36 / 28 / 27 | unchanged ✓ |
| `rg -c 'ACCEPTED'` ≥ 2 | **line-count 1, occurrence-count 2** — see deviation 2 |
| Phase-4 count line agrees at nine | `**Plans**: 1/9 plans executed`; no `8 plans` anywhere ✓ (deviation 1) |
| `git diff --stat -- .planning/ROADMAP.md` | 1 line, inside the Phase 4 section ✓ |
| No file under `internal/`, `api/`, `cmd/`, `pkg/`, `web/` modified | `git diff --stat 26242a9bd..HEAD -- internal/ api/ cmd/ pkg/ web/` empty ✓ |
| Whole-plan diff | exactly the two planning artifacts ✓ |
| `.planning/REQUIREMENTS.md` unmodified by this plan | ✓ (deviation 3) |

Uncommitted Go changes under `internal/grpc/` (`sceneaccess_service.go`,
`sceneaccess_service_test.go`, `player_gate.go`) were present in the working tree during
this run — they belong to the concurrently-executing plan 04-02, not to this one. Every
`git add` here named explicit paths; none of those files was staged.

`task lint:markdown` was **not** run and does not apply: `.planning` is in its exclude
list (`rumdl check --exclude 'site,.git,.serena,.claude,.planning' .`, Taskfile.yaml:777).
No `task fmt` was run, per the concurrency constraint on this working tree.

## Known Stubs

None. This plan creates no code and leaves no placeholder.

## Threat Flags

None. The plan's own register (T-04-13 tampering with milestone scope, T-04-14 repudiation
of the SPEC amendment) is discharged above: heading counts unchanged plus a live
milestone-scope check for T-04-13, and the in-prose citation of Phase 3 D-44 and IDENT-03
for T-04-14. T-04-SC is vacuous — no package was installed.

## Self-Check: PASSED

- `.planning/phases/01-portal-spec/01-SPEC.md` — FOUND
- `.planning/ROADMAP.md` — FOUND
- `.planning/phases/04-shared-facade-helpers-characteraccessservice/04-03-SUMMARY.md` — FOUND
- commit `bbaec01f9` — FOUND
- commit `78dca4f31` — FOUND
