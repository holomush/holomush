---
phase: 01-portal-spec
verified: 2026-08-06T14:13:18Z
status: passed
score: 5/5 must-haves verified
behavior_unverified: 0
overrides_applied: 0
re_verification:
  previous_status: gaps_found
  previous_score: 5/5
  verified_at_tree: e6f36284a
  gaps_closed:
    - "The SPEC is internally consistent about the governed profile attribute names"
    - "Every mutation request message carries `expected_version` and the rule is implementable as written"
    - "Every amendment's superseded text is dead in every artifact a downstream planner reads"
  gaps_remaining: []
  regressions: []
  note: >-
    All three closures landed inside commit 9ca0bb3e1 itself — the same squash
    that carries the stale report. The prior VERIFICATION.md was written before
    the 2026-08-01 gap-closure pass and never regenerated, so its `gaps_found`
    frontmatter described a tree state that no commit ever held. This run
    re-derived every gap independently against HEAD rather than trusting either
    the prior report or the UAT record, and additionally regression-checked all
    five truths against Phase 2's 410-line amendment pass to `01-SPEC.md`
    (2d9bdab52).
---

# Phase 1: Portal SPEC Verification Report

**Phase Goal:** Produce the committed portal SPEC that fixes the audience/message shape, character data and lifecycle model, per-field privacy model, media-ready profile schema, and the full new RPC surface — the precondition PROJECT.md's Out-of-Scope entry demanded, satisfied rather than waived.
**Verified:** 2026-08-06T14:13:18Z (tree `e6f36284a`)
**Status:** passed
**Re-verification:** Yes — after gap closure. Replaces the 2026-08-01 report whose `gaps_found` status was stale on arrival.

The deliverable is a specification document. Verification is goal-backward against the document's normative content and against the artifacts the SPEC claims to have amended — not against runtime behavior.

## Gap Closure (the reason this run exists)

Each of the three recorded gaps was re-derived from the tree. Multiline search (`rg -U`) was used for every prose claim, because claims in these artifacts wrap across lines and a single-line search returns a false negative in both directions.

| # | Gap | Verdict | Evidence |
| --- | --- | --- | --- |
| 1 | §8.6 named `profile.preferences`, which does not exist in §7.2's field set | **CLOSED** | `rg -U 'profile\.\s*\n?\s*preferences'` over `01-SPEC.md` returns **zero** matches. All five occurrences read `profile.rp_preferences` (`01-SPEC.md:1135`, `:1138`, `:1743`, `:2191`, `:2561`). The former divergence site is now `01-SPEC.md:1743`, inside §8.6 (heading at `:1729`) — the posture table's row reads `` `profile.rp_preferences` `` and its seeded default is unchanged at `guest`. §7.2's deliberate naming-collision note at `:1138` still stands, so the qualifier is still explained rather than merely spelled. |
| 2 | §9.3 + §9.4.2 made `CreateCharacter` unimplementable; carve-out required in the SPEC **and** in INV-WORLD-7 | **CLOSED — both halves** | SPEC side, five sites, mutually consistent: §9.3's preamble now reads "Every mutation **that targets an existing character row** carries `expected_version`… `CreateCharacter` is the one exclusion" and names §6.1.3's `UNIQUE` index as what guards a create instead (`:2006-2013`); the table row carries the same verdict inline (`:2018`); §9.4's opening restates the scoping (`:2059-2061`); §9.4.2 defines *guarded mutation* and states `CreateCharacter` **MUST NOT** carry the field at all, so there is no ignored field for a client to read meaning into (`:2098-2103`); §9.6's error table scopes `CHARACTER_VERSION_REQUIRED` to guarded mutations and marks it "Not reachable from `CreateCharacter`" (`:2217`). Registry side: `docs/architecture/invariants.yaml:5111-5120` carries the carve-out **and** preserves the same-transaction obligation universally ("The same-transaction obligation is NOT scoped that way -- EVERY v0.13 character mutation RPC, creation included"). §13's prose declaration (`01-SPEC.md:3070-3086`) states the identical split. `docs/architecture/invariants.md:482` is the regenerated match. |
| 3 | Amendment row 6's superseded count still live and un-annotated in `.planning/research/SUMMARY.md` | **CLOSED** | The annotation is at `.planning/research/SUMMARY.md:360-372`, immediately below the claim block at `:352-353`, in the **same form** rows 4 and 7 used (`:272`, `:307`) — `> **SUPERSEDED IN PART — see .planning/phases/01-portal-spec/01-SPEC.md §14, row 6.**`, naming `WebListPublishedScenes` (`web.proto:339`) as the fourth surface, stating the deliverable itself is unchanged, and signed "Annotated 2026-08-01 by the phase-1 gap-closure pass." The superseded count at `:352-353` is intentionally left verbatim — the gap's own remedy was **annotate, not rewrite**, which is also 01-05-PLAN's prohibition ("a dated research artifact is annotated as superseded, never silently rewritten"). Confirmed via `rg -U 'three\s+existing\s+public\s+export\s+surfaces'`, which finds the claim wrapped across `:352-353` where a single-line search finds nothing. The gap's second item was marked "Optionally" and is **not** required for closure; scored as optional. |

**Provenance of the closures.** `git show 9ca0bb3e1:…` confirms all three fixes were already present in the Phase-1 commit: the `CreateCharacter` carve-out string, zero `profile.preferences` matches, and the row-6 annotation. The gap-closure pass ran before the squash-merge; only the report was left behind.

## Regression Check Against Phase 2

Phase 2 (`2d9bdab52`) amended `01-SPEC.md` by +410 lines and `invariants.yaml` by +48. Every must-have was re-checked against the amended document, not assumed to survive.

| Check | Result |
| --- | --- |
| Phase-2 self-amendments recorded, not silent | §14 gains rows **11–18** with a preamble stating rows 1–10 amend siblings while 11–17 amend this SPEC's own sections and 18 amends the registry (`:3121-3130`). Two of the eight are flagged as *divergences* rather than corrections (rows 14, 17), matching row 10's precedent. |
| Row 12 applied (§11.3 sort/filter table) | **Applied.** The table now carries **six** rows: the original four plus `characters.last_active_at` and joined `players.username`, each annotated with its origin row and safety rationale. The `characters.player_id` "never an ordering" verdict is byte-identical and the new username row explicitly states it does **not** supersede it. PORTAL-09's verdict itself (§11.1 `:2671`) is unchanged: "**No.** No v0.13 surface sorts, filters, groups, or counts on a profile field." |
| Row 15 applied (§13 binding state) | **Applied.** The Phase-1 sentence "All eight shipped `binding: pending` at Phase 1" is *preserved as a Phase-1 statement* (`:3089`) and followed by a dated table of the state after Phase 2 (`:3095-3116`). |
| §13 ↔ registry set equality | **Holds at nine.** §13 and `invariants.yaml` both carry exactly `INV-ACCESS-10/11/12`, `INV-PRIVACY-9/10/11`, `INV-WORLD-5/6/7` with `origin_spec: .planning/phases/01-portal-spec/01-SPEC.md`. §13 also *mentions* INV-WORLD-4, which is a pre-existing entry amended by row 18, not an allocation. |
| §13 binding table ↔ actual registry state | **Exact match, entry for entry.** Registry reads `bound` for INV-WORLD-5, INV-WORLD-6, INV-PRIVACY-11 and `pending` for INV-ACCESS-10/11/12, INV-PRIVACY-9/10, INV-WORLD-7 — identical to the table at `:3103-3107`. No entry is over- or under-claimed. |
| Truth 1 survives | §2.2's three distinct messages intact (`:68`, `:112-114`); §2.7's no-visibility-hints-on-the-wire prohibition intact (`:2664`-area, §2.7 heading); the fourth export surface `WebListPublishedScenes` still enumerated at `:433` and cross-listed at `:760`. |
| Truth 2 survives | Retire's name-effect column still reads "**No.** The name stays reserved." (`:572`); `RetireCharacter`'s §9.3 row repeats it (`:2022`); `purge` MUST NOT be wired to a player-facing affordance (`:591`). |
| Truth 3 survives | §6's opening still reads "TWO SEPARATE POLICIES… **MUST NOT** share an implementation, a validator, or a normalization function" (`:829-831`, wrapped — found only with `rg -U`). §7.1's column/row split intact. |
| Truth 5 survives | §12.1 still states rules **1–6** in PORTAL-10's order — census with set equality, paired positive control, marshaled-bytes assertions, gates demonstrated RED, wire-level opacity assertion, invariant-scope discipline. |

**No regressions found.** Phase 2's edits to the Phase-1 deliverable are additive, dated, and each recorded in §14 with its cause.

## Goal Achievement

### Observable Truths (ROADMAP Success Criteria)

| # | Truth | Status | Evidence |
| --- | --- | --- | --- |
| 1 | Audience matrix (public/owner/admin) with a distinct message shape per audience, absence-not-hiding, backed by a read-surface inventory including all **four** existing public export surfaces | ✓ VERIFIED | §2.1 fixes exactly three audiences; §2.2 mandates `PublicCharacter`/`OwnCharacter`/`AdminCharacter` as distinct proto messages with per-audience projectors (`:112-114`) and rejects the one-message-with-`optional` alternative because a forgotten keyword marshals `""`, which is *present*; §2.7 forbids any visibility hint on the wire and puts enforcement in absence from the marshaled bytes; §3.3 inventories the host-owned rows and names `WebListPublishedScenes` as the **fourth** export surface (`:433`), with §5.2 cross-listing it (`:760`). |
| 2 | Lifecycle as three distinct operations; retire MUST NOT release the name; name-capture inventory with historical-vs-live verdicts | ✓ VERIFIED | §4.4 tables `retire` / `idle-out` / `purge` with reversibility and name-effect columns (`:572`), states `purge` is not a state and MUST NOT be player-reachable (`:591`). §5.1 rule 1 makes the verdict a write-path test; §5.2 inventories the `historical` and `live` sites; §5.3 cross-lists all four export surfaces against their capture sites. |
| 3 | Profile/media as `entity_properties` rows with intrinsics staying columns; character-name and username normalization as two separate policies | ✓ VERIFIED | §7.1 splits columns (`name`, `description`, `status`, `version`) from rows and grounds the no-DDL claim on the shipped `UNIQUE(parent_type,parent_id,name)`. §7.2 fixes the `profile.*` field set — all governed names now consistent (gap 1). §7.3 fixes `profile.image.primary` plus zero-padded `gallery.00..09`. §6 opens with the two-policies prohibition and gives each its own threat model (`:829-842`). |
| 4 | `expected_version` on every mutation; role mutation an explicit exclusion; sorting/filtering answered with a verdict | ✓ VERIFIED (gap 2 closed) | §9.4 fixes `expected_version` as an `int32` scalar per guarded request message, rejects absent-or-zero with `CHARACTER_VERSION_REQUIRED`, and **normatively excludes `CreateCharacter`** at five mutually consistent sites with the `UNIQUE`-index substitute named — the rule is now implementable as written. §10.8 excludes role mutation alongside impersonation, break-glass identifiers and a raw DB console, each with a reason. §11.1 answers PORTAL-09 "**No**" with three ordered reasons and §11.3 bounds the permitted surface to six intrinsic/joined columns, none of them profile-bearing. |
| 5 | Six verification-integrity rules mandated as binding acceptance criteria every later phase inherits | ✓ VERIFIED | §12 opens by stating it is the one section written to be read somewhere else, grounds itself in v0.12's seventeen catalogued cannot-fail verifications, and §12.1 states rules 1–6 in PORTAL-10's order, each with a non-vacuity clause. §12.2 fixes the binding mechanism as verbatim copy into every v0.13 `PLAN.md`. §12.3 assigns a tier per rule. |

**Score:** 5/5 truths verified (0 present, behavior-unverified)

### Requirements Coverage

Requirement IDs were taken from PLAN frontmatter and cross-referenced against `.planning/REQUIREMENTS.md`.

| Requirement | Declared in | Discharged where | Status |
| --- | --- | --- | --- |
| PORTAL-01 | `01-02-PLAN.md` | §2.1–§2.3 | ✓ SATISFIED |
| PORTAL-02 | `01-02-PLAN.md` | §3.1–§3.5 (four export surfaces; requirement text itself amended to "**four**", `REQUIREMENTS.md:31-34`) | ✓ SATISFIED |
| PORTAL-03 | `01-03-PLAN.md` | §5.1–§5.5 | ✓ SATISFIED |
| PORTAL-04 | `01-03-PLAN.md` | §4.1–§4.5 | ✓ SATISFIED |
| PORTAL-05 | `01-01-PLAN.md` | §7.1–§7.5, §8 | ✓ SATISFIED |
| PORTAL-06 | `01-04-PLAN.md` | §9.1–§9.7 (carve-out closed) | ✓ SATISFIED |
| PORTAL-07 | `01-03-PLAN.md` | §6.1–§6.3 | ✓ SATISFIED |
| PORTAL-08 | `01-04-PLAN.md` | §10.8, §15.3 | ✓ SATISFIED |
| PORTAL-09 | `01-04-PLAN.md` | §11.1–§11.4 | ✓ SATISFIED |
| PORTAL-10 | `01-05-PLAN.md`, `01-06-PLAN.md` | §12.1–§12.3 | ✓ SATISFIED |

Union of plan-declared ids = `{PORTAL-01..10}`, exactly. `.planning/REQUIREMENTS.md:337-346` maps PORTAL-01..10 to Phase 1 and maps nothing else to it. **No orphaned requirement, no unclaimed id.**

### Prohibitions (must-NOT checks, judgment tier)

All 20 prohibitions declared across the six plans were checked against the SPEC. All hold; each is a *negative* verdict — the prohibited thing did not happen.

| Plan | Prohibition (abbrev.) | Verdict |
| --- | --- | --- |
| 01-01 | No per-character-pair / per-viewer-identity / IC-knowledge visibility model | ✓ Not present. §8 models visibility as a game-wide per-attribute tier floor only. |
| 01-01 | Seeded default MUST NOT be presented as strict grid-parity | ✓ Divergence surfaced twice — §8.11 and §14 row 10. |
| 01-01 | No per-character admin visibility override | ✓ Absent; §8.1 states the prohibition on any per-character or per-player control (`§8.1:6-7`). |
| 01-01 | Visibility decision MUST NOT leave the ABAC engine | ✓ §8.4/§8.5 seat it in the ABAC policy family; §8.5.2 (Phase 2) extends it in the same engine. |
| 01-02 | No per-field visibility hint/mask/map on the wire | ✓ §2.7 states it as a MUST and names absence from the marshaled bytes as the enforcement. |
| 01-02 | No character-returning RPC outside the census | ✓ §2.6 / §3.4 census with set equality as the sole gate. |
| 01-03 | Retire MUST NOT release the name nor be a hard delete | ✓ `:572` — "No. The name stays reserved."; `:2022` repeats it on the RPC row. |
| 01-03 | No mass rewrite/backfill of captured names | ✓ §5 states there is no update path into immutable payloads or the plugin scene log. |
| 01-03 | Irreversible delete MUST NOT be player-reachable | ✓ `:591` and `:2596`. |
| 01-04 | No role mutation, impersonation, break-glass id, or raw DB console | ✓ All four excluded with reasons in §10.8. |
| 01-04 | No v0.13 surface sorts/filters on a privacy-bearing profile field | ✓ §11.1 verdict "No"; §11.3's six permitted fields are all intrinsic columns or the joined username. |
| 01-04 | Delete path not player-facing, not conflated with admin disable | ✓ `:2596`. |
| 01-05 | Proto `reserved` ranges MUST NOT be claimed as discharging extensibility | ✓ `:3230` records it as hygiene only; `:3270` states the MUST NOT. |
| 01-05 | No vacuous satisfaction of a copied verification rule | ✓ Every §12.1 rule carries its own non-vacuity clause. |
| 01-05 | Amendment MUST NOT delete the research record | ✓ All four annotations in `.planning/research/SUMMARY.md` (`:165`, `:272`, `:307`, `:360`) are in-place `> SUPERSEDED` blocks; the original text survives verbatim above each. |
| 01-06 | Pointer edit MUST NOT falsely claim the orphan check walks the planning tree | ✓ The diff adds prose stating the SPEC glob is **outside** that walk root and MUST be hand-registered; the pre-existing walk-root sentence is untouched. |
| 01-06 | MUST NOT rewrite links to historical design specs | ✓ Only a path glob and one paragraph added to `.claude/rules/invariants.md`; no link rewrites. |
| 01-06 | No silently dropped citation | ✓ §16 records 189 citations with the six corrections named. |

No prohibition is unverified or flagged. All were checkable by direct inspection of the deliverable, and the end-of-phase human checkpoint (`01-UAT.md`, 14/14 pass) independently covered the judgment-bearing ones.

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| --- | --- | --- | --- |
| Registry meta-tests green after Phase 2's bindings | `go test ./test/meta/` | `ok … 2.881s` | ✓ PASS |
| Generated invariants doc not stale | `go run ./cmd/inv-render -check` | exit 0 | ✓ PASS |
| Gap 1 closed — governed attribute name | `rg -U 'profile\.\s*\n?\s*preferences' 01-SPEC.md` | zero matches | ✓ PASS |
| Gap 2 closed — carve-out in registry | `rg -A22 'INV-WORLD-7' invariants.yaml` | carve-out present at `:5115-5117` | ✓ PASS |
| Gap 3 closed — annotation present | `rg -n 'SUPERSEDED' .planning/research/SUMMARY.md` | four annotations incl. row 6 at `:360` | ✓ PASS |
| Amendment row 6 superseded string dead in planner-read artifacts | `rg -U 'three\s+existing\s+public\s+export'` over ROADMAP / REQUIREMENTS / STATE | zero matches | ✓ PASS |
| §13 ↔ registry binding-state agreement | per-id `binding:` read out of the yaml | 3 bound / 6 pending, matches §13's table exactly | ✓ PASS |

Probe execution: not applicable — this phase declares no probes and ships no executable artifact.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| --- | --- | --- | --- | --- |
| — | — | `rg '\b(TBD\|FIXME\|XXX)\b'` over the whole phase directory returns only this report's own prose | — | — |

Issues #4899–#4902 are referenced by design as tracked, deliberate mismatches — not debt markers.

### Human Verification

Discharged. `01-UAT.md` records **14/14 pass, 0 issues, 0 pending**, `status: complete`, with tests 12–14 explicitly targeting the three gaps above (`verifies_gap: 1/2/3`). This run re-derived those three independently rather than inheriting the UAT's verdict, and reached the same conclusion from the tree. One deferred follow-up is recorded there — a generic character-property-update RPC, backlogged to a later milestone, explicitly "fine as-is for v0.13" — which is a scope note, not an open item against this phase.

### Gaps Summary

None. The phase goal is achieved.

All five ROADMAP success criteria hold against the document as it stands at `e6f36284a`, after Phase 2's amendment pass rather than merely as authored. All ten PORTAL requirements are genuinely discharged. All twenty declared prohibitions hold. The three defects the 2026-08-01 report raised are closed at named lines, in both the SPEC and the sibling artifacts each one reached: the governed attribute name is uniform across §7.2 / §8.6 / §9.5 / §10.6; the `expected_version` rule is implementable, with `CreateCharacter`'s exclusion stated normatively in the SPEC and propagated into INV-WORLD-7's registry summary and its regenerated companion; and amendment row 6's superseded count is annotated in the research record in the same form rows 4 and 7 used, with the dated original preserved.

### Observations (non-blocking)

- `01-UAT.md` is **untracked in git**. Its content is complete and internally consistent, but until it is committed the human-verification evidence for this phase lives only in the working tree. Bundle it with the phase artifacts.
- `.planning/REQUIREMENTS.md:337-346` still shows PORTAL-01..10 as `Pending`, and the `- [ ]` checkboxes at `:27-60` are unticked. Orchestrator bookkeeping against a phase whose deliverable is complete — not a defect in the deliverable, and outside this agent's write scope.
- `.planning/REQUIREMENTS.md:76` rule 6 still enumerates only (`ACCESS`, `PRIVACY`) while §13 also uses `WORLD`. Carried forward from the prior report: not a contradiction, since the requirement says "an existing scope" and `WORLD` is one, but the parenthetical reads as exhaustive and no amendment row corrected it.
- `.planning/research/SUMMARY.md:42` still carries an un-annotated paraphrase of the two-versus-three-writers claim that row 7 corrected at `:307`. This was the prior report's explicitly **optional** item and is scored as optional; it does not affect the verdict.

---

_Verified: 2026-08-06T14:13:18Z at tree `e6f36284a`_
_Verifier: Claude (gsd-verifier) — re-verification after gap closure_
