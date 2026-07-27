---
phase: 09-test-quality-code-health-sweep
plan: 16
subsystem: test-quality
tags: [QUAL-04, session-lifecycle, registry, meta-test, bijection]
status: complete
requires:
  - 09-12 (registry, marker convention, shape test, single parser)
  - 09-13 (detach/reaper/post-TTL rows)
  - 09-14 (quit/logout/tmux-reattach rows, GuestPlayer seam)
  - 09-15 (move-arrival and wifi-blip rows; final 32-spec registry state)
provides:
  - "test/meta/session_matrix_registry_test.go — five guards: shape (all five disposition counts pinned), marker well-formedness, marker placement, spec-row<->marker bijection, pointer resolution"
  - "test/session-matrix.yaml — reconciled, sentinel-free, with the one uncovered cell citing issue #4863"
affects: []
tech-stack:
  added: []
  patterns:
    - "Pin EVERY disposition's population, not just the excusing one: a bijection that only compares the covered set is satisfiable by relabelling the uncovered row"
    - "Anchor a marker regex at BOTH ends and treat near-misses as failures, not as absences — an unseen claim reads identically to an unwritten one"
    - "Tie a coverage marker to something that runs (next non-comment line opens an It(), or the marker is a comment parked anywhere"
    - "A negative control must fail EXACTLY its intended guard; NC1's first form tripped two guards and had to be rebuilt to attribute the one under test"
key-files:
  created: []
  modified:
    - test/meta/session_matrix_registry_test.go
    - test/session-matrix.yaml
decisions:
  - "No invariant-registry entry and no binding annotation — the quarantine bijection this guard models is likewise unregistered, and a fabricated binding is the false-green the rule forbids"
  - "The absence check for that annotation greps this file for the literal form, so the file's own comment names it descriptively — writing the literal would have killed the check on the commit that introduced it"
  - "Pinned all five disposition counts rather than only not-applicable: relabelling is the cheapest way to make an unbacked spec claim disappear"
  - "Extended pointer resolution to `spec` rows, not only `covered_by` — both are claims about text in a file, and a spec citation that drifted on rename would otherwise age into fiction unchecked"
  - "Scanned ALL Go sources, not only *_test.go: a marker in production code claims a cell just as loudly, and has no It( under it, so the placement guard catches it"
  - "Did NOT introduce an `uncovered` disposition — `planned` with `owed_by: unassigned` + `blocked_on` + issue #4863 already is the honest marking, and a sixth kind would have been a new name for an existing state"
  - "Left the yamlfmt sentinel leak (#4864) unfixed at source: the fix is a new repo-wide lint gate, outside this plan's two files"
metrics:
  duration: 62m
  tasks: 2
  files: 2
  completed: 2026-07-26
---

# Phase 09 Plan 16: Locking the Session-Lifecycle Matrix Summary

The matrix is now a checked claim. A row cannot say `spec` without a spec, a marker
cannot claim a row the registry does not record as covered, and neither side can be
made to agree with the other by shrinking it. Eight seeded breakages were each
observed failing the guard they targeted, and each was reverted.

## Final registry state

| disposition | count | payload |
|---|---:|---|
| `spec` | 32 | `spec: {file, container, name}` |
| `covered-elsewhere` | 2 | `covered_by: {file, container, name}` |
| `planned` | 1 | `owed_by: unassigned`, `blocked_on`, `issue: 4863` |
| `not-applicable` | 10 | `reason` |
| `not-implementable-from-harness-defaults` | 3 | `gap_notes`, `issue: 4862` |
| **total** | **48** | one row per position of the 12×4 grid |

**Marker ↔ `spec`-row bijection: 32 ↔ 32**, exact, no duplicates on either side, over
two demonstrably NON-EMPTY sets. **Session integration lane: 42 `It` specs, all passed**
(read from `-ginkgo.json-report`, not from `gotestsum --format pkgname`, which prints no
spec counts and so cannot detect a spec silently vanishing). The plan's bar was fifteen.

The counts are unchanged from 09-15's handover: **no row changed disposition in this
plan**, and in particular no `not-applicable` count moved. The single `planned` row is
still `reattach-cas.multi-session`.

## What the lock catches

Five guards, all named `TestSessionMatrix*` so the plan's scoped `-run` selects them:

| Guard | Catches |
|---|---|
| `RegistryShape` | 48 rows; unique ids; exactly one disposition per row; the **population of every disposition**; an untracked not-implementable row |
| `MarkerCandidateLinesAreEitherMarkersOrDocumentation` | a near-miss marker — trailing-comment form, wrong case, trailing prose — that a substring match would silently drop |
| `EveryMarkerSitsDirectlyAboveTheSpecItClaims` | a marker parked in a helper, a non-spec file, or production code |
| `SpecRowsAndInCodeMarkersAreBijective` | orphan row, orphan marker, duplicate marker, and a marker naming a row of an **excluded** disposition |
| `CitedSpecTextAppearsInTheCitedFile` | a `spec` or `covered_by` citation whose file is missing or whose container/name text is not in it |

### What it explicitly does NOT catch

Stated here and in the file's own header, because a green run must not be read as more
than it is:

- **The truth of a row's prose.** `notes`, `reason` and `gap_notes` are free text. A row
  whose disposition and marker agree can still describe the world wrongly. That stayed a
  human-review property, and it is why the administrator-boot notes were re-verified by
  hand below rather than left to the guard.
- **A partial spec.** The bijection proves a spec with that marker exists; it cannot
  prove the spec asserts everything the cell's title implies. This is the documented
  limit of the invariant registry's own `TestBoundInvariantsAreGenuinelyAsserted`, and it
  applies here identically. Two rows say so in their own notes (`move-arrival.multi-session`
  and the move rows' production-pipeline caveat, issue #4788).
- **A marker inside the guard file itself.** `test/meta/session_matrix_registry_test.go`
  is excluded from the walk because its regex literal is marker-shaped by construction.
  Same self-exclusion as the quarantine guard it models.

## The three anti-satisfaction defences

The named threat (T-09-16-02) is that the cheapest way to make a bijection pass is to
remove one side. Three things stop it, and each was demonstrated:

1. **The grid is pinned at 48 rows** — deleting a row fails before the bijection runs.
2. **Every disposition's population is pinned**, not just `not-applicable`. Relabelling an
   unbacked `spec` row as an excuse now fails the count check *and* the bijection *and*
   the pointer count — three guards, one of which names the disposition whose population
   moved (NC6 below).
3. **The exclusion is symmetric.** The registry side contributes only `spec` rows; the
   marker side contributes everything found. So a marker naming a `not-applicable`,
   `planned` or `not-implementable` row fails loudly with that row's disposition in the
   message, rather than being quietly ignored — which is what would let a known-uncoverable
   cell be upgraded to covered by adding a comment (NC1b).

## Load-bearing demonstrations — eight controls, every one observed failing

Every guard was seen failing before it was accepted. Attribution is by construction:
the table records **which** guards failed, not merely that the binary exited non-zero
(the 09-09 trap, where an unrelated panic was mistaken for a guard firing).

| # | Break applied | Guards that failed | Message |
|---|---|---|---|
| NC1 | marker for `admin-boot.web-char` above `var _ = 1` | placement **and** bijection | named the file:line and `"var _ = 1"` |
| NC1b | same marker, correctly placed above a real `It(` | **bijection only** | `admin-boot.web-char (at …:81) names a row dispositioned "not-implementable-from-harness-defaults"; a marker cannot upgrade a non-spec row` |
| NC2 | deleted the `reaper-sweep.telnet` marker | **bijection only** | `these rows declare disposition: spec but no spec … carries their marker: reaper-sweep.telnet` |
| NC3 | duplicate marker for `fresh-select.web-guest` | **bijection only** | `row "fresh-select.web-guest" is claimed by 2 markers (…:113, …:81)` |
| NC4a | near-miss token `// matrix-row-x: …` | **none — by design** | not a candidate; the row's real marker still had to be present, so the safe direction |
| NC4b | `// oops matrix-row: fresh-select.telnet trailing prose` | **well-formedness only** | named file:line and the offending text |
| NC5 | corrupted a `covered_by.file` path | **pointer resolution only** | `row "reattach-select.multi-session" cites covered_by.file "…tests.go", which cannot be read` |
| NC6 | downgraded `reaper-sweep.telnet` `spec` → `not-applicable` | **shape, bijection and pointer resolution** | `expected: …"not-applicable":10,…"spec":32 / actual: …"not-applicable":11,…"spec":31` |
| NC7 | superstring id `fresh-select.telnet-extra` | **bijection only** | anchored regex captured the whole id; the real row went orphan |
| NC8 | appended a bogus 49th `spec` row | **shape, bijection and pointer resolution** | `MUST carry one row per position in the 12x4 matrix` |

NC1's first form is instructive and is recorded rather than discarded: it tripped **two**
guards, so it proved *something* failed rather than proving the bijection fired. It was
rebuilt as NC1b — the same bogus marker, correctly placed — so exactly one guard failed
and the failure is attributable.

NC4a is the substring-trap control (09-11's `Skip(` inside `NotSkip(`). `matrix-row-x:`
does not contain the candidate token, so it is not seen — and that is the **safe**
direction: it claims nothing, and the row it shadows still needs its real marker. The
dangerous direction, a superstring *id*, is NC7 and fails.

## The administrator-boot row: verified truthful, no fix needed

Round-2 review found this row mis-dispositioned twice, so the plan asked for its notes to
be re-checked. They are correct as written. All three cells (`web-char`, `.telnet`,
`.multi-session`) cite `resetpassword --kick`
(`internal/command/handlers/resetpassword.go:197-218`) as a real, capability-gated
administrator session-boot path that **does exist**, and name both gaps: the semantic one
(`DeleteByCharacter` emits nothing, so no `RecordBootedSession` / `session_ended`, issue
#4862) and the wiring one (`RegisterAdmin` panics on five dependencies the harness does
not wire).

Checked two ways, because a line-wise `rg` cannot see a phrase split across a YAML fold:

| Check | Result |
|---|---|
| positive control — `rg -c 'resetpassword' test/session-matrix.yaml` | **5** |
| negative needle, line-wise — `rg -in 'no (administrator[- ])?boot\|no entry point'` | 0 (exit 1) |
| negative needle, file flattened to one line | 0 (exit 1) |
| **positive control for the flattened needle** — same scan with the probe string `"there is no administrator boot entry point in this tree"` appended | **1** |

The last row is what makes the two zeros evidence of absence rather than evidence of a
needle that never matches anything. No wording change was required.

## The one uncovered cell, now followable

`reattach-cas.multi-session` is the matrix's single genuinely uncovered position:
`disposition: planned`, `owed_by: unassigned`, `blocked_on` naming the missing
per-connection detach seam. It had no issue number, so it was visible but not followable.

**Filed issue #4863** — *"test harness: no per-connection detach — session-matrix cell
reattach-cas.multi-session is uncoverable"* — and the row now cites it.

**No `uncovered` disposition was introduced.** The plan offered one if a cell needed it.
`planned` + `owed_by: unassigned` + `blocked_on` + an issue already says exactly that,
and a sixth kind would have been a new name for an existing state — the kind of churn
that makes a registry harder to read without making it more honest. The row's notes now
say in full that this is the one uncovered cell and that it MUST stay expressible: a
guard admitting only `spec` rows would force a false claim onto it.

## Deviations from Plan

**1. [Rule 1 — bug] `task fmt` was writing formatter sentinels into the registry's prose.**

- **Found during:** Task 2, reading the registry.
- **Issue:** `test/session-matrix.yaml` carried **29** occurrences of
  `#magic___^_^___line` appended to the last sentence of `notes:`, `reason:`,
  `gap_notes:` and `blocked_on:` blocks — e.g. *"…dimension to range over.
  #magic___^_^___line"*. This is yamlfmt's internal blank-line-retention sentinel
  (`.yamlfmt` sets `retain_line_breaks: true`). A blank line placed directly after a
  `>-` block scalar gets absorbed **into** the scalar during yamlfmt's round trip, so the
  injected sentinel is no longer a comment and is never stripped. Committed by earlier
  plans, which all ran `task fmt`. In an artifact whose entire purpose is trustworthy
  prose about coverage, the formatter was editing the claim.
- **First fix was wrong, and `task fmt` proved it:** stripping the token and restoring the
  blank line (the file's own style elsewhere) made `task fmt` put all 29 straight back.
  The only fix that survives the formatter is to remove the blank line as well.
- **Fix:** stripped the token and the offending blank line together. Verified: 0 tokens
  remain, 0 `+` lines in the diff contain the token, 29 `-` lines do, and `task fmt` run
  twice more produces a byte-identical file. Only the token and the blank lines changed —
  every removed line contained the token.
- **Root cause filed as issue #4864** with a reproducer and three candidate remedies. It
  is **not fixed here**: the smallest honest fix is a repo-wide lint gate on the token,
  which is outside this plan's two files and is a new build gate.

**2. [Rule 1 — bug] Task 1's parser-count criterion was already false before this plan.**

- **Issue:** the criterion is `rg -c 'session-matrix.yaml' test/meta/session_matrix_registry_test.go`
  returns **1**, to show 09-12's parser was reused rather than duplicated. It returned
  **7** at HEAD, before any edit of mine — the string appears in doc comments and failure
  messages, not only at the parse site. The needle counts mentions, so it could not
  distinguish one parser from seven. Same class as 09-15's deviation 2 and the phase's
  known defect (m).
- **Fix:** replaced with the criterion's stated intent, checked precisely:
  `rg -c '^func loadSessionMatrixRows'` returns **1**, and exactly **one** construction of
  the registry path (`filepath.Join(findRepoRoot(t), "test", "session-matrix.yaml")`)
  exists. The parser is reused; both new guards call `loadSessionMatrixRows`.

**3. [Rule 1 — bug, in my own work] My doc comment killed the no-fabricated-binding check.**

- **Found during:** Task 1 acceptance checks.
- **Issue:** the criterion `rg -c 'Verifies: INV-'` must return no matches. My header
  comment explained the deliberate absence by *quoting the literal form*, taking the count
  to 1 with no annotation present. The guard would have been dead from this commit onward.
  This is exactly 09-15's deviation 2, reproduced.
- **Fix:** reworded to name the annotation descriptively, and said in the comment **why**
  the literal is kept out. The needle returns 0 (exit 1), and a positive control confirms
  it still finds real annotations elsewhere in the tree
  (`test/integration/session/session_list_active_by_location_test.go` and two others).

**4. [Rule 2] Extended the pointer check to `spec` rows, which the plan scoped to `covered_by`.**

The plan asks for pointer resolution on `covered-elsewhere` rows only. But a `spec` row's
citation is the same kind of claim — a file, a container and a name — and 32 of them
would otherwise have gone unchecked, free to drift the moment a spec was renamed. Both
are checked, and the check asserts it examined **34** pointers (32 + 2) so a registry
whose pointers all failed to decode cannot sail through having checked nothing.

**5. [Rule 2] Added two guards the plan did not name: marker well-formedness and placement.**

The plan specifies the bijection, the pointer check and the shape check. Neither of those
sees a marker the extraction regex does not match, and none of them cares where a marker
sits. Without well-formedness a typo'd marker is invisible, and invisible reads exactly
like unwritten. Without placement the bijection is satisfiable by a comment parked
anywhere in the tree — a coverage claim backed by nothing. Both are demonstrated by NC4b
and NC1.

## Verification

| Gate | Result |
|---|---|
| Plan's Task 1 verify command, verbatim | exit 0; `rg -c '^--- PASS: TestSessionMatrix'` = **5** |
| `task test -- ./test/meta/` | exit 0 — **107 tests** (was 103; +4 new guards) |
| `task test` | exit 0 — **10416 tests**, 4 skipped (was 10412) |
| `task test:int -- ./test/integration/session/...` | exit 0 |
| Session specs actually registered | **42 `It` specs, all passed** (Ginkgo JSON report) |
| `task lint` | exit 0 |
| `task fmt` | exit 0; mutations committed; file byte-stable across three runs |
| Registry shape | 48 rows — 32 spec / 2 covered-elsewhere / 1 planned / 10 n/a / 3 not-implementable |
| Marker ↔ registry bijection | **32 ↔ 32**, no duplicates, both sets non-empty |
| `#magic___^_^___line` in the tree | **0** (was 29, all in this one file) |
| Invariant-binding literal in the guard file | 0 (exit 1), positive-controlled elsewhere in the tree |
| `func findRepoRoot` / `skipDirs = map` redeclared in the guard file | 0 each (exit 1) — shared helpers reused |
| Commit deletions | none (`git diff --diff-filter=D` empty for both commits) |
| Working tree | clean |

The scoped `task test:int -- ./test/integration/session/...` form worked again — an eighth
confirmation that the claim in `plan-review-learnings.md` that `test:int` ignores `--`
args is false. It also accepts a passthrough `-ginkgo.json-report=` flag.

## Self-Check: PASSED

- `test/meta/session_matrix_registry_test.go` — FOUND (modified)
- `test/session-matrix.yaml` — FOUND (modified)
- Commit `32cceb672` — FOUND
- Commit `3852ce084` — FOUND
- Issue #4862 — confirmed OPEN, cited by the three not-implementable rows
- Issue #4863 — filed by this plan for the one uncovered cell
- Issue #4864 — filed by this plan for the yamlfmt sentinel leak

## Known Stubs

None introduced. Every negative control was reverted and its absence verified; the tree is
clean and both commits are green.

Two deferred items, both filed rather than hidden:

- **#4863** — the per-connection-detach harness seam. The matrix's one uncovered cell
  depends on it. Recorded in the registry as `planned` / `owed_by: unassigned` /
  `blocked_on` / `issue: 4863` — a first-class state, not a silent gap.
- **#4864** — the yamlfmt sentinel leak. Cleaned out of this file; nothing yet stops the
  next author reintroducing it in any YAML. The suggested remedy is a one-line lint gate.

## Note on QUAL-04

QUAL-04 was marked **Complete** by 09-15. This plan does not change that assessment; it
makes it enforceable. The requirement's coverage claim is now machine-checked rather than
asserted: 47 of 48 positions carry a spec with a verified marker, a resolved
covered-elsewhere pointer, or a committed non-applicability, and the 48th carries a named
blocker and a filed issue.
