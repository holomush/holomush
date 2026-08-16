# Phase 4 — Handoff (state as of 2026-08-10)

Facts only. No conclusions, no recommendations, no fixes applied beyond what is listed as committed.

---

## 1. Where we are

| | |
|---|---|
| Phase | 4 — Shared Facade Helpers & `CharacterAccessService` |
| Phase status (STATE.md) | `Planned` |
| Plans | 9, in 6 waves, all committed |
| Branch | `v013-phase-03`, **167 commits ahead of `origin/main`**, upstream is `origin/main` |
| Working tree | clean except untracked `.gsd/` |
| Open PR | none |
| Commands run this session | `/gsd-plan-phase 4`, then `/gsd-plan-review-convergence 4 --codex` |
| Convergence loop | ran **5 review cycles** (`--max-cycles 3`, extended twice by the user); **not converged** |
| Execution | **not started** — no code has been written for this phase |

---

## 2. Commits this session (oldest → newest)

```
11bc517bc docs(phase-4): add validation strategy
1b24f1e7b docs(04): create phase plan
0da6c5010 docs(04): create phase plan
3e92419f3 docs(04): add pattern map
9fa4d252d docs: cross-AI review for phase 4                     ← review cycle 1
3f06a8447 docs(04): incorporate cross-AI review feedback into plans
9887805fd docs: cross-AI review cycle 2 for phase 4             ← review cycle 2
2a568cd2b docs(04): incorporate cycle-2 review feedback into plans
494e91256 docs: cross-AI review cycle 3 for phase 4             ← review cycle 3
18411efad docs(04): correct citation drift flagged in review cycle 3
5f2a31f0a docs(04): resolve cycle-3 census and mutate-seam findings
66b41c330 docs: cross-AI review cycle 4 for phase 4             ← review cycle 4
b97d049a1 docs(04): close description-validation gap (#4954) and census partition
764cd047b docs: cross-AI review cycle 5 for phase 4             ← review cycle 5 (HEAD)
```

Prior session ended at `5b8004651` (`docs(state): record phase 4 context session`).

---

## 3. Artifacts on disk

`.planning/phases/04-shared-facade-helpers-characteraccessservice/`

| File | Origin |
|---|---|
| `04-CONTEXT.md` | `/gsd-discuss-phase 4`, prior session. Decisions **D-69…D-83**, LOCKED. |
| `04-DISCUSSION-LOG.md` | prior session |
| `04-RESEARCH.md` | `gsd-phase-researcher`, 1308 lines. Contains `## Validation Architecture`. |
| `04-VALIDATION.md` | plan-phase §5.5 from template; `status: draft`, `nyquist_compliant: false` |
| `04-PATTERNS.md` | `gsd-pattern-mapper` |
| `04-01…04-09-PLAN.md` | `gsd-planner` (9 files) |
| `04-REVIEWS.md` | 1878 lines, 5 appended cycles at `:9`, `:532`, `:1004`, `:1184`, `:1556` |
| `04-HANDOFF.md` | this file |

The milestone SPEC is **outside** this directory at `.planning/phases/01-portal-spec/01-SPEC.md` and is normative for Phase 4. The phase-dir SPEC glob does not find it.

---

## 4. Plan map

| Plan | Wave | Tasks | Est. tokens | `depends_on` | Scope |
|---|---|---|---|---|---|
| 04-01 | 1 | 3 | 82000 | — | tracer: anonymous name+description read end-to-end; proto + regen; ABAC seeds; facade; web proxy; viewer-identity seam |
| 04-02 | 1 | 2 | 44000 | — | `playerGate` extraction (`resolveAndGate` + `ownedCharacter`); gate error matrix |
| 04-03 | 1 | 2 | 25000 | — | 01-SPEC amendment |
| 04-04 | 2 | 3 | 76000 | 04-01 | viewer-filtered property slice; marshaled-bytes absence; tier switch; remaining proto |
| 04-09 | 2 | 2 | 80000 | 04-01 | domain property write (`UpdateCharacterProfileAttributes`), taxonomy kind, mutator closure |
| 04-05 | 3 | 3 | 60000 | 04-01, 04-02, 04-04 | owner audience; `INV-ACCESS-15` |
| 04-06 | 4 | 3 | 82000 | 04-01, 04-02, 04-04, 04-05, 04-09 | write path (profile attrs + `characters.description`); domain validation fix (#4954) |
| 04-07 | 5 | 3 | 88000 | 04-01, 04-04, 04-05, 04-06 | `ListCharacterDirectory`; `WebListAllCharacters` deletion; §9.2 directory gate |
| 04-08 | 6 | 3 | 82000 | 04-02, 04-05, 04-06, 04-07 | routing census + descriptor census |

All estimates carry `confidence: low` (calibration reports 0 samples).

---

## 5. Review history

Reviewer: Codex CLI (`codex-cli 0.147.0`), lane `--codex`, source-grounding pass on.

| Cycle | Commit | HIGH | Actionable non-HIGH | Total unresolved |
|---|---|---|---|---|
| 1 | `9fa4d252d` | 5 | 3 | 8 |
| 2 | `9887805fd` | 2 | 4 | 6 |
| 3 | `494e91256` | 2 | 0 | 2 |
| 4 | `66b41c330` | 3 | 3 | 6 |
| 5 | `764cd047b` | 3 | 5 | 8 |

Cycles 1–4 findings were each verified **FULLY RESOLVED** by the following cycle, except cycle 4's, whose dispositions cycle 5 recorded as: NEW-HIGH-1 fully resolved, NEW-HIGH-2 fully resolved, NEW-HIGH-3 **partially resolved**, MEDIUM fully resolved, LOW(`skip`) fully resolved, LOW(comment-filter) **partially resolved**.

Cycle 5 recorded that a fix created a new defect in each of the five cycles.

An internal `gsd-plan-checker` also ran after each replan (5 passes total): 2 blockers → 0; then 2 blockers → 0; then 4 warnings → 0; then 1 warning → 0; then 1 warning → 0.

---

## 6. Open findings from cycle 5 (verbatim, unresolved)

All three HIGHs are in **`04-08` Task 2** (wave 6).

**H1 — `04-08:485` and `04-08:355-360` are mutually unsatisfiable. Marked BLOCKS EXECUTION.**
Cycle 4 added "check them in as a **literal set**"; the same task's acceptance criteria still carry the untouched pre-cycle-4 line "the two public-audience RPC names appear in the file **only inside the comment**". With `autonomous: true` the executor either declines to write the literal (deleting the NEW-HIGH-3 fix) or writes it and reports its own criterion failed. Reviewer notes: one-line fix.

**H2 — the partition's universe filter fails OPEN; four handler shapes escape. Marked BLOCKS `04-08` Task 2.**
The filter (`04-08:346-352`: exactly 2 params / first `context.Context`; exactly 2 results / second `error`) silently excludes rather than fails closed. Escapes recorded: (1) server-streaming `(req, stream) error` — a shape already shipped twice in the same package at `internal/grpc/server.go:559` and `subscribe_handler.go:272`; (2) a method promoted from the bare embedded `playerGate` (`04-02:105`); (3) value receivers (plan says pointer-only; precedent at `world_envelope_census_test.go:146-150` accepts both); (4) generated-`Unimplemented` promotion (recorded as inert). Reviewer's stated fix: invert the default — universe = every exported method, require each to land in owner ∪ public ∪ a checked-in `nonHandlerMethods` literal.

**H3 — the public-audience literal is a one-line bypass of the partition, and `04-08:33` asserts it is not.**
The owner bucket has a derived counterpart; the public bucket has none. A new ungated handler goes RED in the extra group, and the cheapest green is adding its name to `publicAudienceRPCs`, after which all four assertions pass. `04-08:33` and `:414` are true for *moving an existing* owner member, false for a new one. Reviewer's stated fix: derive public membership from the shipped `bodyReferencesSelector` (both public handlers reference `s.resolveViewerIdentity`, `04-01:306` / `04-07:328`, and must not reference the guest gate).

**Actionable non-HIGH (5):**

- **M1 (`04-06`)** — the "Consumers that inherit the new rejection" paragraph (`:407-420`) is factually wrong; the criterion at `:474`/`:675` is vacuous; `<reversibility>` `:488-493` is not evidence-backed. Stated fix: reword to "two compile-time-coupled surfaces, neither reached in production today"; restate the criterion as a compile check.
- **L1 (`04-06`)** — `validation.go:191` permits `'\r'` as well, but the plan says "newline and tab" in **seven** places including `:371` (the `read_first` summary of that function). A `\r` control-character test would be permanently red. Stated fix: "newline, carriage return, or tab" in those seven places.
- **L2 (`04-08`)** — the cycle-4 comment filter reached three counting criteria but missed `:152` (unfiltered, expects 2 — false-GREEN direction) and `:477` (false-RED only).
- **L3 (`04-08`)** — `:481`'s banned-token grep `exempt|Exempt|allowlist` is case-fragile; `EXEMPT`, `Allowlist`, `AllowList`, `allow_list` all pass. Stated fix: `rg -i 'exempt|allow[-_ ]?list'`.
- **L4 (ROADMAP)** — `.planning/ROADMAP.md:484` says "**Plans**: 8 plans"; the Phase-4 block enumerates 9. In scope for `04-03`, which already carries `.planning/ROADMAP.md` in `files_modified`.

Cycle 5 also recorded: waves 1–5 carry no unresolved findings.

---

## 7. Gate state (re-run at handoff time, all green)

| Gate | Result |
|---|---|
| Requirements coverage | 7/7 — IDENT-02, IDENT-02a, PROFILE-03, PROFILE-04, PROFILE-05, PROFILE-10, EXT-06 |
| `check.decision-coverage-plan` (BLOCKING) | passed, 15/15 (D-69…D-83) |
| `gap-analysis-plan-post` | 22/22 items covered |
| Plan file tails | all 9 end at `</output>` |
| `frontmatter.validate` / `verify.plan-structure` | valid on every edited plan at each pass |
| `getMilestonePhaseFilter` | resolves 7 phase dirs; Phase 4 present; milestone scope not truncated |

---

## 8. GitHub issues

| Issue | State | Relationship |
|---|---|---|
| **#4954** | OPEN | Filed this session. `world.Service.UpdateCharacterDescription` writes descriptions with no validation. `service.go:862` is a bare `char.Description = description`; `Character.SetDescription` (`character.go:102`), which validates, has zero production callers. `04-06` Task 2 closes it in-phase; cited 13× in that plan. |
| #4937 | OPEN | Pre-existing, `awaiting-precursor`. Re-run the PROFILE-11 exposure audit against a populated corpus. D-77 says no new Phase-4 audit is authored. |
| #4949 | OPEN | Pre-existing. `.claude/rules/grpc-errors.md:58,:65` prescribes a non-compiling `oops.AsOops(err).Code()` with inverted semantics. |

---

## 9. Errors of record (authored by the orchestrator, corrected or outstanding)

1. **Research premise, corrected.** The Phase-4 research brief asserted that `ValidateDescription` "already runs on" the description-write path and that IDENT-02a therefore needed no new capability. This is false at HEAD; it propagated into `04-06` and was caught in review cycle 4. Now GH #4954 and `04-06` Task 2.
2. **"Two live consumers" paragraph, outstanding (M1).** Text written into `04-06` Task 2's `<action>` asserted `internal/plugin/hostfunc/cap_property.go:140` and `internal/command/types.go:70` are live production consumers inheriting the tightened validation. Verified at HEAD: `internal/command/types.go:70` is a declaration with **zero call sites**, no `@desc` command exists, and `NewPropertyCapability` (`cap_property.go:47`) has **zero production callers**. Both are compile-time couplings. The acceptance criterion built on the paragraph is a non-gate. **Not yet corrected.**
3. **Control-character set, outstanding (L1).** The same paragraph and six other places say `ValidateDescription` permits "newline and tab". `hasControlCharsExceptWhitespace` (`validation.go:191`) permits `\n`, `\r`, **and** `\t`. The function's own error message at `:107` says "(except newline/tab)" and is itself incomplete. **Not yet corrected.**
4. **`rg -h` misuse, corrected in-session.** The plan-phase §13 requirements-coverage gate was translated from GSD's `grep -h` snippet. In ripgrep `-h` is `--help`, not `--no-filename`; the pipeline printed help, matched nothing, and reported a false "0/7 requirements uncovered". Corrected to `--no-filename`; the real result is 7/7. Captured as engram memory `7j08gehwz1`.
5. **`harness.go:1191` → `:1197` propagation, corrected on second attempt.** A `perl` substitution assumed the citation was contiguous (`harness.go:1191`); the actual text is `` `harness.go:876`, exposed by `AccessEngine()` at `:1191` ``, so it did not match. Caught by review cycle 3 and fixed in `18411efad`.

> **Addendum (2026-08-11):** Items 2 (M1) and 3 (L1) above were corrected at HEAD by commit `7fb1334a5` (`docs(04): incorporate cycle-6 review findings into plans`), together with the rest of the cycle-6 findings resolved in that same commit. The "**Not yet corrected**" markers above are preserved as history of record; they no longer describe HEAD.

---

## 10. Repo/tooling facts established this session

- `01-SPEC.md` supplies **neither** `## Edge Coverage` nor `## Prohibitions` (`spec-section` helper returns `supplied:false` for both), so plan-phase's spec-less probe fallback fired for both sections. 17 edge items surfaced → 13 authored + 1 flagged + 1 n/a + 2 unresolved (PROFILE-04 and PROFILE-10, both `unclassified`).
- `gsd-tools intel api-surface` produced `symbolCount: 0`; the empty surface was omitted from the planner prompt.
- Detectors that did not fire: `assumption-delta scan` (`detected:false`), `api-coverage` (`detected:false`), schema-gate (no JS-ORM patterns; this repo uses goose SQL migrations).
- The `drift` plan:pre gate reported `action_required: true`, `directive: warn`, `blocking: false` — 661 structural elements since last mapping. Non-blocking; not acted on.
- `workflow.auto_advance` is `true` in `.planning/config.json`. Auto-advance to execute-phase was declined by the user at the end of plan-phase.
- Per engram `jqtc8rvwzm`: GSD worktree isolation is unavailable in this harness, and a `PreToolUse` guard blocks sequential `Agent` dispatch unless `gsd-tools query dispatch-isolation --force-isolation none --phase 4 --plan N` is called **once per plan** (9 times for this phase). Not yet exercised.
- `internal/grpc/sceneaccess_service.go` carries **24** `s.resolveAndGate(` and **21** `s.ownedCharacter(` call sites.
- `NewSceneAccessServer` has **three** direct construction sites: `cmd/holomush/sub_grpc.go:826`, `internal/testsupport/integrationtest/harness.go:1178`, `internal/testsupport/integrationtest/session.go:719`.
- `test/meta` has no transitive Go dependency on `internal/grpc` (`go list -deps -test`).

---

## 11. Engram memories written this session

| short_id | Category | Subject |
|---|---|---|
| `hdeqhzkqa8` | decision | Phase 4 planned — 9 plans / 6 waves, gate results, the three research corrections, the two checker blockers and their root cause |
| `7j08gehwz1` | gotcha | `rg -h` is `--help`, not `--no-filename`; GSD's `grep -h` snippets must not be transliterated |

Neither has been updated with the convergence-loop outcome.

---

## 12. Not done

- No code written; no plan executed.
- The 3 HIGH and 5 actionable findings in §6 are **unresolved and uncommitted** — no fix for any of them exists on disk.
- `04-VALIDATION.md` still `status: draft`, `nyquist_compliant: false`, and its Per-Task Verification Map still carries `TBD` task IDs (populated by `/gsd-validate-phase`, not plan-phase).
- Branch not pushed; no PR opened.
- `abac-reviewer` / `crypto-reviewer` gates not run (they fire pre-push, and the phase has no code yet). Research recorded that `crypto-reviewer` does not apply to this phase's surface and `abac-reviewer` does (new seeds in `04-01`, `04-05`, `04-07`).
