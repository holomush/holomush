---
phase: 01-portal-spec
verified: 2026-08-01T00:00:00Z
status: gaps_found
score: 5/5 must-haves verified
behavior_unverified: 0
overrides_applied: 0
gaps:
  - truth: "The SPEC is internally consistent about the governed profile attribute names"
    status: partial
    reason: "§8.6 — the table the SPEC calls 'the whole configuration surface' — names a governed attribute `profile.preferences` that does not exist in §7.2's field set. The real name is `profile.rp_preferences`, and §7.2 goes out of its way to say the `rp_` qualifier exists 'specifically so the two cannot be conflated by name alone'. §10.6's allowlist and §9.5 both use the correct name; §8.6 is the sole divergence, in the one table Phase 2 seeds policy from, in a document that states attribute names are compared as exact bytes (§7.3)."
    artifacts:
      - path: ".planning/phases/01-portal-spec/01-SPEC.md"
        issue: "Line 1278 reads `profile.preferences`; §7.2 line 1071, §9.5 line 1613 and §10.6 line 1968 read `profile.rp_preferences`."
    missing:
      - "Correct §8.6 line 1278 to `profile.rp_preferences`."
  - truth: "Every mutation request message carries `expected_version` and the rule is implementable as written"
    status: partial
    reason: "§9.3 line 1460 states 'Every row below is a mutation, and every mutation request message carries `expected_version` per §9.4', and `CharacterAccessService.CreateCharacter` is the first row (line 1465). §9.4.2 then mandates that a mutation request whose `expected_version` is absent or zero MUST be rejected with `CHARACTER_VERSION_REQUIRED` at the RPC boundary. A create has no pre-existing row and therefore no version to supply; the row is created at `version = 1` by the migration's DEFAULT. Read literally, Phase 4 cannot ship a working `CreateCharacter` — every legal create request is rejected. The SPEC carves out no exception, and the same tension is carried into INV-WORLD-7 ('every new character mutation RPC carries `expected_version` on its request, rejects an absent or zero value')."
    artifacts:
      - path: ".planning/phases/01-portal-spec/01-SPEC.md"
        issue: "§9.3 line 1460-1465 and §9.4.2 line 1537 together make `CreateCharacter` unimplementable; §13 INV-WORLD-7 line 2462-2472 inherits the same universal phrasing."
      - path: "docs/architecture/invariants.yaml"
        issue: "INV-WORLD-7's summary repeats the universal 'every v0.13 character mutation RPC carries expected_version' phrasing with no create carve-out."
    missing:
      - "State explicitly whether `CreateCharacter` is exempt from `expected_version` (it creates the row it would guard), or specify what a create sends and what the guard means there."
      - "Propagate the same carve-out into INV-WORLD-7's registry summary so the invariant matches the rule it pins."
  - truth: "Every amendment's superseded text is dead in every artifact a downstream planner reads"
    status: partial
    reason: "§14's own preamble makes this the standard: 'an amendment row whose superseded string is still live in a sibling artifact' is named as the recurring review failure the section exists to prevent. Amendment row 6 (three → four public export surfaces) was applied to `.planning/REQUIREMENTS.md` and `.planning/ROADMAP.md` — both verified clean — but `.planning/research/SUMMARY.md:352` still says the SPEC delivers an inventory 'including the three existing public export surfaces', with no superseded-by annotation. Rows 4 and 7 annotated that same dated research record in place, so the precedent exists and row 6 simply did not follow it. `.planning/research/SUMMARY.md` is listed in 01-CONTEXT.md's canonical references as 'read first' for downstream agents."
    artifacts:
      - path: ".planning/research/SUMMARY.md"
        issue: "Line 352 carries the superseded count of three, unannotated. Line 42's lead paragraph likewise carries an un-annotated paraphrase of the claim row 7 corrected at line 304 ('`Rename` doubles the writers into')."
    missing:
      - "Add a superseded-in-part annotation at `.planning/research/SUMMARY.md:352` in the same form rows 4 and 7 used."
      - "Optionally annotate line 42's paraphrase of the two-versus-three-writers claim."
---

# Phase 1: Portal SPEC Verification Report

**Phase Goal:** Produce the committed portal SPEC that fixes the audience/message shape, character data and lifecycle model, per-field privacy model, media-ready profile schema, and the full new RPC surface — the precondition PROJECT.md's Out-of-Scope entry demanded, satisfied rather than waived.
**Verified:** 2026-08-01
**Status:** gaps_found
**Re-verification:** No — initial verification

The deliverable is a specification document. Verification is therefore goal-backward against the document's normative content and against the artifacts the SPEC claims to have amended — not against runtime behavior.

## Goal Achievement

### Observable Truths (ROADMAP Success Criteria)

| # | Truth | Status | Evidence |
| --- | --- | --- | --- |
| 1 | Audience matrix (public/owner/admin) with a distinct message shape per audience, absence-not-hiding, backed by a read-surface inventory including all **four** existing public export surfaces | ✓ VERIFIED | §2.1 fixes exactly three audiences (`01-SPEC.md:51-63`); §2.2 mandates `PublicCharacter`/`OwnCharacter`/`AdminCharacter` as distinct proto messages and rejects the one-message-with-`optional` alternative on the grounds that a forgotten keyword marshals `""` which is *present* (`:65-104`); §2.7 forbids any visibility hint on the wire (`:238-255`); §3.3 inventories 24 host-owned rows, the `WebListAllCharacters` split, and the export surfaces — with `WebListPublishedScenes` named as the **fourth** at `:406-414`. §2.6 fixes the census key as the exact fully-qualified `package.Service.Method` string with set-equality semantics and a symmetric-difference diff. |
| 2 | Lifecycle as three distinct operations; retire MUST NOT release the name; name-capture inventory with historical-vs-live verdicts | ✓ VERIFIED | §4.4 tables `retire` / `idle-out` / `purge` with reversibility and name-effect columns, states `purge` is not a state, and gives retire-keeps-the-name two independent sufficient reasons (`:537-581`). §5.2 inventories seven `historical` capture sites and five `live` ones, plus an explicit not-a-member table (`:687-721`). §5.1 rule 1 makes the verdict a **write-path** test, which is what correctly classifies `GameEvent.actor` (recomputed on every read, from frozen bytes) as `historical` (`:700`). §5.3 cross-lists all four export surfaces against their capture sites. |
| 3 | Profile/media as `entity_properties` rows with intrinsics staying columns; character-name and username normalization as two separate policies | ✓ VERIFIED | §7.1 splits columns (`name`, `description`, `status`, `version`) from rows and grounds the no-DDL claim on `UNIQUE(parent_type,parent_id,name)` at `000001_baseline.up.sql:364` (`:1018-1046`). §7.2 fixes twelve `profile.*` fields; §7.3 fixes `profile.image.primary` plus zero-padded `gallery.00..09` and derives exactly-one-primary from the shipped constraint. §6 opens with "TWO SEPARATE POLICIES… MUST NOT share an implementation, a validator, or a normalization function" and gives each its own threat model (`:802-814`). |
| 4 | `expected_version` on every mutation; role mutation an explicit exclusion; sorting/filtering answered with a verdict | ✓ VERIFIED (see gap 2) | §9.3 line 1460 asserts the universal carriage across nine mutation rows; §9.4.1 fixes it as `int32` transcribed from `000049_world_version_guard.up.sql:20`; §9.4.2 rejects absent-or-zero against the live `Version > 0` affordance at `character_repo.go:82-85`. Role mutation excluded in §10.8 and again emphatically in §15.3, with the ADMIN-04 allowlist named as the enforcing mechanism. §11.1 answers PORTAL-09 **"No"** with three ordered reasons and §11.3 enumerates the one permitted surface and its four intrinsic columns. |
| 5 | Six verification-integrity rules mandated as binding acceptance criteria every later phase inherits | ✓ VERIFIED | §12.1 states rules 1–6 in PORTAL-10's own order, each carrying a **non-vacuity clause** naming what a fake satisfaction looks like (`:2211-2345`). §12.2 fixes the binding mechanism as verbatim copy into every v0.13 `PLAN.md` with `gsd-plan-checker` verifying presence *and* specialization, records numbering stability as part of the contract, and names both rejected alternatives with reasons. §12.3 assigns a tier per rule and explicitly denies that `E2E` satisfies rules 1, 3 or 5. |

**Score:** 5/5 truths verified (0 present, behavior-unverified)

### Requirements Coverage

| Requirement | Source | Discharged where | Status |
| --- | --- | --- | --- |
| PORTAL-01 | audience matrix + per-audience message | §2.1–§2.3 | ✓ SATISFIED |
| PORTAL-02 | read-surface inventory | §3.1–§3.5 (four export surfaces, membership rules, mechanical predicate) | ✓ SATISFIED |
| PORTAL-03 | name-capture inventory | §5.1–§5.5 | ✓ SATISFIED |
| PORTAL-04 | lifecycle states + retire-keeps-name | §4.1–§4.5 | ✓ SATISFIED |
| PORTAL-05 | profile/media data model | §7.1–§7.5, §8 | ✓ SATISFIED |
| PORTAL-06 | RPC surface + `expected_version` | §9.1–§9.7 | ✓ SATISFIED (gap 2) |
| PORTAL-07 | two name-normalization policies | §6.1–§6.3 | ✓ SATISFIED |
| PORTAL-08 | role-mutation exclusion | §10.8, §15.3 | ✓ SATISFIED |
| PORTAL-09 | sorting/filtering verdict | §11.1–§11.4 | ✓ SATISFIED |
| PORTAL-10 | six verification-integrity rules | §12.1–§12.3 | ✓ SATISFIED |

No orphaned requirement: `.planning/REQUIREMENTS.md:288-297` maps PORTAL-01..10 to Phase 1 and nothing else.

### CONTEXT.md Decision Fidelity (19 locked decisions)

The six highest-risk decisions — those the user chose over the assistant's recommendation — were checked individually.

| Decision | Required | Found | Status |
| --- | --- | --- | --- |
| D-04 | Census with set equality is the **sole** gate; struct-literal lint consciously NOT mandated | §2.6 states the census is "the SOLE mandated enforcement gate", and the Notably-absent block explicitly declines the lint, calls a later lint "an increment, not a correction", and instructs the reviewer to catch any PR that adds a second gate then relaxes the census (`:226-236`). Repeated in §15.2. | ✓ |
| D-06 | All three lifecycle values ship, paired with exhaustive-`switch`/`default: deny` **and** a direct-construction `idle` test | Both halves present and explicitly paired: §4.2 ships `('active','retired','idle')` with `idle` marked unreachable; §4.3 rule 1 mandates the exhaustive switch and rule 2 mandates the direct-construction test, with the fail-open argument spelled out against research CONFLICT 4 (`:506-535`). INV-WORLD-5 carries both halves into the registry. | ✓ |
| D-09 | **No** player/character agency over web profile visibility | §8.1 states it as a prohibition, not an omission, and adds a Notably-absent block ruling out `profile.*.visibility`, `SetProfileFieldVisibility`, and any visibility column in a v0.13 form (`:1161-1179`). No owner-facing toggle appears anywhere in the document. | ✓ |
| D-12 | Read-time evaluation on attribute name, NOT stamped rows | §8.5 mandates read-time evaluation and forbids stamping, with the backfill argument and the Phase-2 confirmation obligation grounded at `property.go:80-86` (`:1240-1262`). Reinforced as §11.2's second reason. | ✓ |
| D-14 | Seeded default puts `description` at **anonymous**, and the SPEC **MUST state the divergence explicitly** | §8.6 seeds the in-world description at `anonymous` (`:1275`). The divergence is stated twice: §8.11 as its own section with the floor-not-ceiling rationale (`:1352-1363`), and §14 row 10 so a reviewer scanning only the amendments table still finds it (`:2520`). | ✓ |
| D-15 | Model + seeded defaults only; no editing surface in v0.13 | §8.12 plus a Notably-absent block naming `config` as the future tenant and instructing the reviewer to catch an early editor (`:1365-1377`). Repeated in §15.2. | ✓ |

The remaining thirteen (D-01/02/03/05/07/08/10/11/13/16/17/18/19) were each located and match: separate proto messages (§2.2), breaking-change posture (§2.5), the `WebListAllCharacters` split (§2.4), single CHECK-constrained column (§4.1), retired-but-visible (§4.5), description always public (§7.4), game-wide per-attribute floor (§8.3), ABAC family with `source='admin'` (§8.4), name/pronouns hard floor (§8.8), one document at the GSD location, verbatim-copy binding (§12.2), the ACCESS/PRIVACY/WORLD allocation (§13), and the spec-location pointer edit.

### Internal Consistency

| Check | Result |
| --- | --- |
| `participants_snapshot` id-vs-name reconciliation between §3 and §5 (the wave-6 fix) | **Genuinely reconciled in both places.** §3.3's preamble at `:384-398` states the mismatch and that census membership is unaffected either way; the `WebGetPublicSceneArchive` row at `:403` reads "character **ids** as implemented today, **names** by proto contract (§5.4, issue #4901)"; §5.2 line `:701` points at §5.4 with "what this column stores today is not what its proto contract says"; §5.4 carries the full record with both sides cited. No surviving contradiction. |
| §3.3's heading "The three existing public export surfaces" (`:384`) versus the four-surface count | Not a defect — the heading groups the three research-known surfaces and the prose at `:406-414` names the fourth explicitly, saying "research enumerated three; the tree carries four". |
| Governed attribute names across §7.2 / §8.6 / §9.5 / §10.6 | **One mismatch — see gap 1.** |
| `expected_version` universality versus the create case | **Incoherent as written — see gap 2.** |

### Amendments — recorded versus APPLIED

§14's preamble commits to application, not recording. Each superseded string was searched in its own artifact.

| Row | Artifact | Superseded string | Result |
| --- | --- | --- | --- |
| 1 | `.planning/ROADMAP.md` Phase 4 crit 3 | "An owner can set any profile field" | **Absent.** Replaced by the game-configured tier floor, with the exhaustive-`switch` clause retained (`ROADMAP.md:213`). |
| 2 | `.planning/ROADMAP.md` Phase 5 crit 4 | "An owner flips a field between public and private" | **Absent.** Restated around a configuration change on next load (`ROADMAP.md:233`). |
| 3 | `.planning/REQUIREMENTS.md` PROFILE-12 | "The visibility toggle and the retirement flow" | **Absent.** Re-seated onto the retirement flow and the profile-authoring surface, with an amendment note (`REQUIREMENTS.md:147-150`). |
| 4 | `.planning/research/SUMMARY.md` CONFLICT 4 | "ship `public` and `private` in the v0.13 UI" | **Annotated in place**, not rewritten (`SUMMARY.md:271-280`). |
| 5 | `docs/architecture/invariants.yaml` INV-PRIVACY boundary | "Privacy-relevant gating on history reads." | **Applied**; the exclusion clause preserved verbatim; generated companion regenerated. |
| 6 | REQUIREMENTS.md PORTAL-02 + ROADMAP Phase 1 crit 1 | "three existing public export surfaces" | **Absent from both** (ROADMAP crit 1 now reads "all **four**… the fourth is `WebListPublishedScenes`"). **But live and unannotated at `.planning/research/SUMMARY.md:352` — gap 3.** |
| 7 | REQUIREMENTS.md IDENT-09 + research item 3 | "Adding `Rename` doubles the writers into that race" | **Annotated in place** at `SUMMARY.md:306-315`, naming `guest_service.go:227` as the second writer. A paraphrase survives un-annotated at `SUMMARY.md:42` (observation). |
| 8 | PORTAL-10 rule 5 | "Top-level oops code assertions" / `oops.AsOops(err).Code()` as top-level | **The false form is gone from all four artifacts.** `rg -F` returns zero matches in `.planning/ROADMAP.md`, `.planning/REQUIREMENTS.md`, `.planning/STATE.md` and the SPEC's own normative text. REQUIREMENTS.md rule 5 (`:69-75`) now prescribes the wire assertion and records that both spellings resolve the deepest code; STATE.md `:60-62` carries the correction; SPEC §12.1 rule 5 and §9.6.1 state the wire-level rule with the empirical verification against the pinned `oops v1.22.0`. The `.claude/rules/grpc-errors.md` half is deliberately not applied and the row says so — confirmed: that file is unmodified on this branch. |
| 9 | REQUIREMENTS.md ADMIN-06 + ROADMAP Phase 6 crit 3 | `events_audit` row written "in the same transaction" | **Applied.** `ROADMAP.md:252` now puts the durability boundary at the outbox envelope and names the asynchronous projection as the only writer. |
| 10 | *Divergence* | grid-parity | **Recorded twice** (§8.11 and §14 row 10). |

### Invariant Registry Integrity

| Check | Result |
| --- | --- |
| §13's eight ids exist in `docs/architecture/invariants.yaml` | ✓ INV-PRIVACY-9 (`:2120`), INV-PRIVACY-10 (`:2127`), INV-ACCESS-10/11/12 (`:2235`, `:2242`, `:2249`), INV-WORLD-5/6/7 (`:5029`, `:5038`, `:5046`). Set equality holds — eight in §13, eight in the registry, no extras. |
| All `binding: pending`, none carrying `asserted_by` | ✓ Verified in the yaml diff; §13 states "All eight ship `binding: pending`" and the meta-test that rejects a pending entry carrying `asserted_by` passes. |
| No fabricated `// Verifies:` for the new ids | ✓ `rg 'Verifies: (INV-ACCESS-1[012]\|INV-PRIVACY-9\|INV-PRIVACY-10\|INV-WORLD-[567])' -g '*.go'` → exit 1, zero matches. |
| No new scope minted | ✓ ACCESS / PRIVACY / WORLD only, per D-18 and rule 6. |
| `docs/architecture/invariants.md` not stale | ✓ Regenerated in the same change; `go run ./cmd/inv-render -check` exits 0. |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| --- | --- | --- | --- |
| Registry meta-tests green after hand-registration | `go test ./test/meta/ -run 'TestEveryRegistryInvariantHasBinding\|TestProvenanceGuard\|TestBoundInvariantsAreGenuinelyAsserted\|TestInvariant'` | `ok … 0.945s` | ✓ PASS |
| Generated invariants doc not stale | `go run ./cmd/inv-render -check` | exit 0 | ✓ PASS |
| No fabricated invariant bindings | `rg -c 'Verifies: (new ids)' -g '*.go'` | exit 1 (no matches) | ✓ PASS |
| Superseded amendment strings dead | `rg -F` per row against ROADMAP / REQUIREMENTS / STATE | absent for rows 1, 2, 3, 6, 8 | ✓ PASS |

Probe execution: not applicable — this phase declares no probes and ships no executable artifact.

### Scope Discipline

No migration, no proto change, no policy code, no UI. The complete non-`.planning/` diff against `main` is four files, all in scope:

| File | Why in scope |
| --- | --- |
| `CLAUDE.md` | D-19 spec-location pointer, applied at both passages that name spec locations. |
| `.claude/rules/invariants.md` | D-19 — adds the `.planning/phases/**/*-SPEC.md` glob and one sentence about the walk root; the walk-root prose itself is byte-identical. |
| `docs/architecture/invariants.yaml` | D-18 hand-registration plus the amendment-5 boundary edit. |
| `docs/architecture/invariants.md` | Regenerated companion, never hand-edited. |

No scope creep found.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| --- | --- | --- | --- | --- |
| — | — | No unreferenced `TBD` / `FIXME` / `XXX` in any phase artifact or repo file touched | — | — |

Issues #4899–#4902 are referenced by design as tracked, deliberate mismatches, per the verification brief — not counted as debt markers.

### Gaps Summary

The phase goal is achieved: all five ROADMAP success criteria are met against the document, all ten PORTAL requirements are genuinely discharged rather than name-dropped, all nineteen locked decisions are honored including the six the user chose over the assistant's recommendation, the nine amendments were applied to their own artifacts rather than merely recorded, PORTAL-10's false rule 5 is gone from every artifact a planner reads, and the registry entries are hand-registered correctly with no fabricated bindings.

Three defects remain, none of which defeats the goal but each of which would produce exactly the downstream divergence this SPEC exists to prevent:

1. **`profile.preferences` in §8.6 versus `profile.rp_preferences` everywhere else.** Low blast radius in practice — §8.6's totality rule defaults an unassigned `profile.*` attribute to `guest`, which is the same floor the mis-named row assigns — but it is a name mismatch in the one table Phase 2 seeds policy from, inside a document that insists attribute names compare as exact bytes.

2. **`CreateCharacter` carries `expected_version` with no carve-out.** This is the substantive one. §9.3's universal statement plus §9.4.2's reject-absent-or-zero rule make every legal create request rejectable at the RPC boundary, because a create has no row whose version could be supplied. A Phase-4 implementer must either invent an exception the SPEC does not sanction or ship a broken create. INV-WORLD-7 inherits the same phrasing into the registry.

3. **Amendment row 6's superseded count survives in the research corpus.** `.planning/research/SUMMARY.md:352` still describes the read-surface inventory as covering "the three existing public export surfaces", unannotated — while rows 4 and 7 annotated that same dated record. §14's own preamble names a superseded string still live in a sibling artifact as the recurring review failure it exists to prevent.

### Observations (non-blocking)

- `.planning/REQUIREMENTS.md:76` rule 6 enumerates only (`ACCESS`, `PRIVACY`) while §13 also uses `WORLD`. Not a contradiction — the requirement says "an existing scope" and `WORLD` is one — but the parenthetical reads as exhaustive and §14 did not amend it.
- `.planning/STATE.md:8` still reads `stopped_at: Phase 1 context gathered` and `:38` `Status: Executing Phase 1`. Orchestrator bookkeeping, not the phase deliverable.
- The SPEC is 2898 lines across sixteen sections with a mechanically-swept grounding trace (§16: 189 distinct citations, six corrected, none removed). Spot-checks of `character_repo.go:82-85`, `000049_world_version_guard.up.sql:20`, `role_store.go:83-93` and `sceneaccess_service.go:843-880` all resolved as claimed.

---

_Verified: 2026-08-01_
_Verifier: Claude (gsd-verifier)_
