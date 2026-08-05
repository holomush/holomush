---
phase: 02-abac-schema-vocabulary
plan: 02
subsystem: character-identity
tags: [unicode, uts39, mixed-script, abac, regression-guard, ast]
status: complete

requires:
  - charname.Gate — the composition point plan 02-01 established
  - charname.Normalize / internal/charname/syntax
provides:
  - charname.MixedScript — the §6.1.2 Mechanism A verdict
  - charname.ScriptSet — the sorted, neutral-excluded script set
  - error codes NAME_MIXED_SCRIPT and NAME_UNASSIGNED_SCRIPT
  - the IDENT-08 username regression pin
  - two directional charname↔auth separation guards (go/parser)
affects:
  - internal/charname (Gate.Check gained a step; Normalize gained a second message)
  - internal/world (validation_test.go gained regression rows; validation.go untouched)
  - internal/auth (player_test.go only; player.go untouched)
  - cmd/internal/gen-confusables (emits gofmt-formatted output)

tech-stack:
  added: []
  patterns:
    - closed-enumeration verdict table expressed as subset-of-family membership
    - fail-closed on a Unicode-version gap rather than treating it as neutral
    - go/parser guards over parsed import declarations, each with a synthetic-fixture control
    - non-vacuity probes paired with every Empty/NotContains assertion

key-files:
  created:
    - internal/charname/mixedscript.go
    - internal/charname/mixedscript_test.go
  modified:
    - internal/charname/gate.go
    - internal/charname/gate_test.go
    - internal/charname/pipeline.go
    - internal/charname/pipeline_test.go
    - internal/world/validation_test.go
    - internal/auth/player_test.go
    - test/integration/charname/name_confusable_test.go
    - cmd/internal/gen-confusables/main.go
    - internal/charname/confusables_table_gen.go

decisions:
  - Permitted CJK families are matched by CONTAINMENT, not "Latin plus one of" — {Han, Hiragana} and {Han, Hangul} are ordinary non-Latin names and requiring Latin would breach the plan's prohibition
  - The separation guard is directional and file-scoped by design; a package-wide ban would be RED by design when plan 02-06 lands Gate.Admit in character_service.go
  - The empty-normal-form split keeps ONE error code and varies only the message, so callers matching on the code are unaffected
  - The generated confusables table is now gofmt-formatted inside the generator, because HEAD was gofumpt-dirty and `task fmt:check` was red

metrics:
  duration: ~95min
  completed: 2026-08-04

actuals:
  tokens: 11600
  tasks: 3
  commits: 4
---

# Phase 02 Plan 02: Mixed-Script Restriction and the Username Pin Summary

Cross-script splicing is closed at the gate before any database round trip, and
the two name policies §6 separates are now mechanically prevented from reaching
each other — by two guards scoped so that plan `02-06`'s legitimate
`internal/auth → internal/charname` admission calls stay green.

## What was built

**`charname.ScriptSet`** resolves a name's script set from stdlib
`unicode.Scripts`, excluding `Common` and `Inherited` as script-neutral and
returning the result sorted and deduplicated, so a verdict — and the message a
refused submitter reads — cannot depend on the order the scripts happened to
appear in.

**`charname.MixedScript`** applies §6.1.2's eight-row table as a closed
enumeration: a set of size ≤ 1 permits, the three CJK families permit by
containment, everything else is `NAME_MIXED_SCRIPT`. Expressed as explicit set
membership, never as a script count — a count re-derives the table in a second
place and the two copies then drift.

**The Unicode-version gap fails closed.** A rune in no `unicode.Scripts` range
is refused with `NAME_UNASSIGNED_SCRIPT`. Go's tables are Unicode **15.0.0**
(probed, not assumed) while this package's confusables data is **17.0.0**;
treating the gap as neutral would let a genuinely two-script name read as
single-script, which is the exact splice Mechanism A exists to catch.

**`Gate.Check` gained step 3**, between the syntactic rule and the skeleton
query, so a mixed-script name never reaches the corpus — asserted structurally
via the lookup double's call counter, not inferred from the verdict.

**The empty-normal-form message split.** A submission of only invisibles now
reads *"that name contains no visible characters"* while a genuinely blank one
still reads *"please enter a character name"*. The code stays shared because the
server-side fact is identical, so callers matching on it keep working.

**IDENT-08** is discharged by a pin plus two `go/parser` guards. No username
validation was written; `internal/auth/player.go` is byte-identical across all
four commits.

## The one judgement call worth reading

The plan's permitted-set text reads "`{Latin}` unioned with a non-empty subset of
`{Han, Hiragana, Katakana}`" — Latin **required**. Implemented literally, a name
written in Han + Hiragana (an entirely ordinary Japanese name, and one of the
most common shapes in the writing system) would be **rejected**, as would
Han + Hangul. That collides head-on with this plan's own prohibition: *no rule
added here may reject a non-Latin name that its Latin-script equivalent would
pass.*

So the families are matched by **containment** — a name's scripts must be a
subset of `{Latin, Han, Hiragana, Katakana}`, `{Latin, Han, Bopomofo}`, or
`{Latin, Han, Hangul}`. Every one of §6.1.2's eight rows still lands exactly as
the SPEC writes it; the only sets this admits beyond the plan's phrasing are the
Latin-free CJK combinations, which are precisely the augmented script sets UTS
#39's own Moderately Restrictive profile covers. The widening is toward the
standard, not away from it. A `Han + Hiragana` permitted row is in the suite so
the choice is asserted rather than assumed.

## Gates demonstrated RED (PORTAL-10 rule 4)

| Gate | Mutation / pre-fix state | Exit | Observed |
|---|---|---|---|
| `MixedScript` at the gate | pre-Task-1 `Gate.Check` (no mixed-script step) | **1** | `An error is expected but got nil` — the gate **accepted** `раypal` |
| `MixedScript` unit surface | `charname.MixedScript` / `ScriptSet` undefined | **1** | 9 compile errors |
| Empty-normal-form split | pre-fix single message | **201** | `Should not be: "name normalizes to the empty string"` |
| charname → auth guard | synthetic package with a planted `internal/auth` import | *in-suite* | the planted file is flagged, the clean one is not |
| player.go → charname guard | synthetic `player.go` calling `charname.Normalize` via a same-file helper | *in-suite* | `charname` detected in the call graph |
| Whole-script confusable spec | seeded name changed to a non-colliding one | **201** | 3 Ginkgo specs: `Expected an error to have occurred` |
| `task fmt:check` (pre-fix) | HEAD as committed by 02-01 | *non-empty* | `gofumpt -l .` listed `confusables_table_gen.go` |

The two separation guards are demonstrated against **synthetic fixtures**, never
against the real tree: planting an import in real `internal/charname` code and
reverting it would prove the guard fires only by briefly committing the very edge
it forbids. Those demonstrations are permanent subtests, not a mutation someone
has to remember to re-run.

**The username rows cannot be shown RED**, and are not claimed to be. They pin
behaviour that already passes; that is what a regression pin is. What makes them
non-vacuous is the pairing, not a RED: every rejection runs the accepting
`alaric_01` control in the same subtest, so `err != nil` cannot be satisfied by a
validator that has started rejecting everything.

Both real-tree guard assertions additionally carry a **non-vacuity probe** — the
`internal/charname` walk must find `github.com/samber/oops` imports, and the
`ValidateUsername` call-graph walk must reach `usernameRegex` — so an `Empty` or
`NotContains` cannot pass because the walk found nothing.

## Verification

| Check | Result |
|---|---|
| `task test` (full untagged suite) | **exit 0** — 10,628 tests, 4 skipped |
| `task lint` | **exit 0** |
| `task fmt:check` | **exit 0** (was red at HEAD) |
| `task test:int` (full) | **exit 0** — 11,054 tests, 7 skipped |
| `task test:int -- ./test/integration/charname/...` | **exit 0** |
| `task test -- -run 'MixedScript\|ScriptSet' ./internal/charname/` | **exit 0** — 38 tests (criterion: ≥ 14) |
| `git diff HEAD~4 --stat internal/auth/player.go` | empty — the rule is pinned, not edited |
| `rg -o 'internal/auth' internal/charname/ --type go -g '!*_test.go' \| wc -l` | **0** |
| `Script_Extensions` outside comments in `mixedscript.go` | **0** |

Every pass/fail above was read from the **exit code**, never from a matched
output string.

## Deviations from Plan

### Auto-fixed issues

**1. [Rule 3 — Blocking] `task fmt` reformats the generated confusables table, and `task fmt:check` was already red at HEAD**

- **Found during:** Task 1, running the plan-mandated `task fmt`
- **Issue:** `cmd/internal/gen-confusables` wrote its rendered source directly
  with `os.WriteFile`, with no `go/format` pass. `render` emits one map entry per
  line with a single space after each colon, but gofmt aligns a contiguous map
  literal's values to the widest key in the run, and this table mixes 4-digit and
  5-digit keys. So `gofumpt -l .` listed the file on a **clean** tree — meaning
  `task fmt:check`, and every pr-prep lane depending on it, was red at HEAD from
  plan 02-01. Worse, the two tools fought: `task fmt` fixed the formatting and
  the next generator run silently reverted it, so 02-01's drift gate
  (`task --force generate:confusables && git diff --exit-code`) was comparing
  unformatted output against an unformatted commit and would have started failing
  the moment anyone ran `task fmt`.
- **Fix:** Run the rendered source through `go/format` before writing, so
  "generated" and "formatted" are the same bytes. Regenerated from the pinned
  Unicode 17.0.0 input.
- **Verified:** 6,565 entries and `UnicodeVersion = "17.0.0"` unchanged (the diff
  is alignment only); a second generator run is byte-identical to the first
  (idempotent); the generator's output is byte-identical to `task fmt`'s output;
  `task fmt:check` exits 0.
- **Files:** `cmd/internal/gen-confusables/main.go`,
  `internal/charname/confusables_table_gen.go`
- **Commit:** `21d9e5721`

**2. [Rule 1 — Bug] Three specs named "whole-script homoglyph" used Latin+Cyrillic fixtures**

- **Found during:** Task 1, after wiring `MixedScript` into `Gate.Check`
- **Issue:** Two 02-01 gate specs and one integration spec — including one whose
  `Describe` block is literally titled *"Whole-script homoglyph of an existing
  character name"* — submitted `Аlaric` and `раypal`, which are
  **mixed**-script, not whole-script. Mechanism A now refuses those one step
  earlier. One spec failed outright; the other two would have kept passing while
  silently testing the mixed-script path instead of the confusable path they were
  written to prove — the more dangerous outcome, because nothing would have said
  so.
- **Fix:** Retargeted at a genuine whole-script homoglyph — seeded `Cocoa`,
  submitted `сосоа` (every letter Cyrillic; U+0441,
  U+043E and U+0430 map to `c`, `o` and `a` in the committed confusables table,
  so both sides skeleton to `cocoa`). This is what §6.1.2 says Mechanism B is
  *for*: Mechanism A permits a single-script name, and the skeleton catches it.
- **Verified:** mutating the seeded name to a non-colliding one turns three
  integration specs red (exit 201), so the retargeted fixture genuinely
  adjudicates against real Postgres rather than passing by construction.
- **Files:** `internal/charname/gate_test.go`,
  `test/integration/charname/name_confusable_test.go`
- **Commit:** `d5b072b4f`

**3. [Rule 1 — Bug] A fixture of mine smuggled a Latin letter into a "wholly Cyrillic" row**

- **Found during:** Task 1, first GREEN run
- **Issue:** The script-neutrality row `"Иван O'Иван"` contains an ASCII `O` —
  a Latin letter. The row asserted "permitted" and the implementation correctly
  refused it. The fixture was wrong, not the code.
- **Fix:** `"Иван А-Петр"`, every letter Cyrillic, with a comment recording the
  trap so it is not reintroduced.
- **Commit:** `d5b072b4f`

### Deliberate departures from the plan text

**A. Task 2's regex criterion is verified at the regex's real location.**

The criterion reads `rg -n 'characterNameRegex' internal/world/validation.go`.
That symbol no longer exists: plan 02-01 extracted the rules into the D-28
dependency-free leaf, and the literal now lives at
`internal/charname/syntax/syntax.go:47` as `nameRegex`. The criterion's *intent*
— "the regex is unchanged by this phase" — is verified, and more strongly than
the grep would have: `git diff HEAD~4 --stat` is **empty** for both
`internal/charname/syntax/syntax.go` and `internal/world/validation.go`, so the
shape rule is byte-identical, not merely still present.

**B. The permitted CJK families are matched by containment.** See "The one
judgement call worth reading" above.

**C. The gate's mixed-script step runs AFTER the syntactic rule, not between
Normalize and it.** The plan says "after `Normalize` and before the skeleton
lookup", which both placements satisfy. Running it after the syntactic rule keeps
the script set free of the punctuation and digit noise a raw submission carries,
and preserves the existing ordering assertions. One consequence is stated in the
code rather than left to be discovered: because the syntactic rule admits only
`\p{L}` and Go's `regexp` draws that from the same Unicode 15 tables, an
unassigned-script rune is already refused one layer up, so
`NAME_UNASSIGNED_SCRIPT` is defense in depth at the gate. It is unit-tested
directly on `MixedScript`, where it is reachable.

**D. Task 1 was committed as two commits.** The generator fix is a distinct
logical change from the mixed-script rule and CLAUDE.md requires atomic commits
(one logical change each). Splitting keeps the 8,888-line generated-table
realignment out of the diff a reviewer reads for Mechanism A.

## Note on `actuals.tokens`

`11600` counts the **authored** diff (46,400 chars / 4). The full four-commit
diff is 260,579 chars, but 214,179 of those are the mechanical realignment of the
generated confusables table — 4,444 lines that gained one space each. Counting
them would report ~65k against a 72k estimate and look like a near-perfect
projection, which would be a flattering fiction: no judgement was spent on those
bytes. The honest read is that the authored work came in well under estimate and
the surprise cost was a pre-existing formatting defect, not the mixed-script rule.

## Known Stubs

None. `NAME_UNASSIGNED_SCRIPT` is unreachable *through `Gate.Check`* today
(deviation C), but it is a real, tested branch on a real exported function, not a
placeholder — and it is the branch that keeps Mechanism A correct the day the
syntactic rule is relaxed or the stdlib tables move.

One test fixture is knowingly time-bound and says so at its definition: the
`U+105C0 TODHRI LETTER A` row goes red when Go's tables reach Unicode 16. That is
the P-12 version-skew drift signal the row exists to raise, not a flake; the
comment tells the next reader to re-point the fixture rather than delete the row.

## Threat Flags

None. Every security-relevant surface this plan introduces is already enumerated
in its threat register as T-02-06 … T-02-10. The one change outside that register
— formatting the generator's output — alters no input handling, no digest check
and no parsing; the SHA-256 pin on the downloaded input is untouched.

## Invariants

This plan pins no registry invariant and writes no `// Verifies:` annotation, as
its verification-integrity section requires. `docs/architecture/invariants.yaml`
is unmodified. The sc-vs-scx divergence is recorded in `mixedscript.go`'s header
comment and is queued for the §14 amendment pass in plan `02-11` — it is a
documented approximation, not a guarantee, and registering it as an invariant
would claim more than the code delivers.

## Self-Check: PASSED

Both new files exist on disk; all four commits (`21d9e5721`, `d5b072b4f`,
`ce9dd0ff7`, `3a137f7dd`) resolve in `git log`; working tree clean before this
document.
