---
phase: 02-abac-schema-vocabulary
verified: 2026-08-05T21:40:00Z
status: passed
score: 5/5 roadmap success criteria verified
behavior_unverified: 0
overrides_applied: 0
requirements:

  - id: IDENT-06
    status: discharged

  - id: IDENT-07
    status: discharged

  - id: IDENT-08
    status: discharged

  - id: IDENT-09
    status: discharged

  - id: PROFILE-11
    status: partial_by_decision
    discharged: "entity_properties half"
    deferred: "characters.description half — Phase 4, D-29"

  - id: EXT-07
    status: discharged_to_phase_ceiling
    note: "policy contract fully met; endpoint-level denial proof deferred to Phase 4 by D-08 (Phase 2 ships no RPCs by design)"
deferred:

  - truth: "PROFILE-11's characters.description half (the `resource is character` permit)"
    addressed_in: "Phase 4"
    evidence: "02-CONTEXT.md D-29; ROADMAP criterion 4 text already carries the deferral; absence is pinned by TestNoPhase2SeedIntroducesACharacterResourceTypePermit"

  - truth: "§10.2's endpoint-level admin-section denial test"
    addressed_in: "Phase 4"
    evidence: "02-CONTEXT.md D-08 — Phase 2 ships no RPCs, so the endpoint form is unwritable; helper-level form shipped"

  - truth: "EXT-04 registry ↔ authorization-descriptor census"
    addressed_in: "Phase 6"
    evidence: "ROADMAP Phase 6 Requirements list includes EXT-04; 01-SPEC.md:2375 assigns it there"

  - truth: "INV-ACCESS-10/11/12, INV-PRIVACY-9/10, INV-WORLD-7 bindings"
    addressed_in: "Phase 3/4"
    evidence: "all remain binding: pending with NO asserted_by — verified in docs/architecture/invariants.yaml"
human_verification:

  - test: "Dispatch `abac-reviewer` over the phase's authorization surface"
    expected: "A READY / NOT READY verdict on internal/access/, internal/admin/section/, and the seed family"
    why_human: "Repo-owned sub-agent requiring agent dispatch; D-05 makes it MANDATORY before merge. The 02-11 executor could not dispatch it and correctly refused to fabricate a verdict."

  - test: "Reconcile .planning/REQUIREMENTS.md's two disagreeing halves"
    expected: "Checkboxes and traceability-table rows agree, and PROFILE-11 is not recorded as fully closed"
    why_human: "Tool-owned parsed artifact; `requirements.mark-complete` has no partial-credit model. Must not be hand-edited."

  - test: "Re-run 02-10's exposure audit against a populated corpus before Phase 4's description widening"
    expected: "Per-row verdict machinery adjudicates at least one real row"
    why_human: "The audited sandbox corpus had zero entity_properties rows, so the verdict machinery is structurally verified but behaviourally unexercised."
---

# Phase 2: ABAC & Schema Vocabulary — Verification Report

**Phase Goal** (`.planning/ROADMAP.md:204`): Land the authorization vocabulary, name policy, and schema primitives every later phase gates on — `admin_section:` + `seed:admin-section-access`, `seed:profile-public-read`, the character lifecycle column, and the normalized-name unique index — with no UI and no new RPCs.

**Verified:** 2026-08-05 · **Status:** passed · **Re-verification:** No — initial verification

**Verification stance:** every claim below was checked against the tree, not against SUMMARY prose. Where a SUMMARY and the code disagreed, the code won.

---

## Goal Achievement — the five ROADMAP success criteria

| # | Criterion | Status | Evidence |
|---|-----------|--------|----------|
| 1 | Confusable / NFKC / `Cf` / mixed-script AND regex-block-list rejection, server-side, at create **and** rename | ✓ VERIFIED | `internal/charname/gate.go:155-193` runs syntax → mixed-script → block list → skeleton lookup, emitting `NAME_INVALID_SYNTAX` / `NAME_BLOCKED` / `NAME_CONFUSABLE` / `NAME_SKELETON_UNVERIFIABLE`. Create paths: `internal/auth/character_service.go:129`, `internal/auth/guest_service.go:250`. Rename path: `internal/world/postgres/character_repo.go:205-212` accepts only `charname.Admitted`, whose sole constructor is `(*charname.Gate).Admit`; the in-tree rename caller `cmd/holomush/cmd_character_name.go:335` calls `Admit` with `ExcludingCharacter`. Behaviour proven at `test/integration/charname/name_confusable_test.go:181,222` and `name_blocklist_test.go:141-179`, each with a paired admit-control. |
| 2 | Two concurrent claims of one normalized name cannot both succeed; gate **demonstrated RED** pre-index; pre-existing duplicates resolved by a one-shot job first | ✓ VERIFIED | Unique index at `internal/store/migrations/000056_character_normalized_name_unique.sql` — `SET NOT NULL` **before** `CREATE UNIQUE INDEX`, which makes the backfill a hard precondition (NULLs are distinct for uniqueness). One-shot Go job: `internal/store/migrations/000055_backfill_character_normalized_names.go:83`, halting on collision. Concurrency spec: `test/integration/charname/name_uniqueness_test.go:304`. **RED observed, not asserted** — `02-12-SUMMARY.md:155-161` records exit 201 with the verbatim failure (`Expected an error to have occurred. Got: <nil>`) against the schema staged at `000055`; the inverted case ships permanently as `name_uniqueness_test.go:412`. A deliberate mis-staging was also run to prove the harness can tell a setup error from the demonstration (`02-12-SUMMARY.md:176-190`). |
| 3 | Player usernames still reject non-ASCII; existing regex **pinned**, not re-implemented | ✓ VERIFIED | `internal/auth/player.go:31` unchanged (`^[a-zA-Z][a-zA-Z0-9_]*$`). Pin at `internal/auth/player_test.go:376-415`: non-ASCII, Cyrillic, fullwidth, `Cf`, leading-digit and length fixtures, **each paired** with the `alaric_01` positive control in the same subtest. Backed by an AST import guard (`player_test.go:417+`) so the character pipeline cannot reach the username path. |
| 4 | Off-location viewer reads a character's public properties where colocation previously denied; ships only after the exposure audit. **In-world-description half deferred to Phase 4 by D-29** | ✓ VERIFIED (against the current, amended wording) | Policy: `internal/access/policy/seed.go:764` — `seed:profile-public-read-property`, additive permit guarded on `parent_type == "character"`. Behaviour: `test/integration/access/seed_policies_test.go:418` (S3 — previously DENY, now ALLOW, real engine + real Postgres) with paired control S3b at `:430` proving a different-location **location** parent is still denied. Shipped policies untouched (`seed_profile_visibility_test.go:646`). Audit: `02-AUDIT-RESULT.md` — real sandbox kopia restore `7e48a9b592c2e0d302a5da3cf0171835`, **0 rows** exposed. The deferred half is not merely absent but *pinned* absent by `seed_profile_visibility_test.go:692`. |
| 5 | `seed:admin-section-access` permits admin, denies builder / plain player / guest across all seven sections, each denial paired, id list by set equality, eighth section needs no new policy | ✓ VERIFIED | Policy: `internal/access/policy/seed.go:798` — scoped by **resource type** (`resource is admin_section`), which is what buys "every future section at zero cost". Seven ids: `internal/admin/section/registry.go:103-109`. Denial suite: `internal/admin/section/gate_test.go:114-152` — iterated from `All()` (never hard-coded), 7 permits + 21 denials, each denial preceded by the paired `assertAdminPassesTheGate` control on the **same** section, against a **real** seed engine (`engineFor`, `:59` — explicitly not a double). Set equality: `internal/admin/section/registry_test.go:44-54` (ordered equality, which is strictly stronger). Eighth section: `gate_test.go:159` — an admin reaches `DENY_ADMIN_SECTION_UNREGISTERED` for an id no policy enumerates. |

**Score: 5/5 criteria verified.**

---

## Requirements Coverage

> **`.planning/REQUIREMENTS.md` is internally inconsistent and its checkboxes are not evidence.** All six IDs read `[x]` at lines 114/118/121/124/173/241 while all six traceability rows read `Pending` at lines 353-356/369/385. Cause: seven plans share `EXT-07`/`PROFILE-11`; the first plan to finish flipped the boxes and every later `requirements.mark-complete` returned `write_set_complete: false`. Each ID below is adjudicated against **code and tests**, not against either half of that artifact.

| Requirement | Status | Evidence |
|---|---|---|
| **IDENT-06** — NFKC + `Cf` stripping + confusable/mixed-script rule | ✓ **DISCHARGED** | `internal/charname/pipeline.go:100` (`Normalize`), `mixedscript.go`, `skeleton.go` + generated `confusables_table_gen.go`. Enforced at the gate (`gate.go:155-193`), persisted to `normalized_name` / `name_skeleton` / `name_skeleton_unicode_version` (migration `000054`). Per-row Unicode version (D-23) makes a stale subset queryable. |
| **IDENT-07** — configurable regex block list, server-side, create **and** rename | ✓ **DISCHARGED** | `internal/charname/blocklist/` — compiled snapshot (`cache.go`), two-signal poller (`poller.go`), lifecycle subsystem (`subsystem.go:74`) **wired into production** at `cmd/holomush/core.go:452`. Seeded setting `core.character.name.blocklist` at `000054`. Live-edit behaviour proven at `test/integration/charname/name_blocklist_test.go:210` (observed within one poll interval by a gate constructed *before* the edit) and fail-safe at `:179` (a poll failure does not silently disable the list). |
| **IDENT-08** — username ASCII-only **regression guard**, not new validation | ✓ **DISCHARGED** | See criterion 3. `internal/auth/player.go:31` is untouched by this phase's diff; the requirement is discharged by pinning, exactly as its text demands. |
| **IDENT-09** — unique index on stored normalized name, lands before/with `Rename`; duplicates resolved by a one-shot job first | ✓ **DISCHARGED** | See criterion 2. Migration triple `000054`→`000055`→`000056` with the load-bearing statement order documented in-file. Resolution CLI `holomush character name duplicates` / `set` at `cmd/holomush/cmd_character_name.go`. The requirement's own "two writers → three with Rename" arithmetic is now reflected in the registry (see INV-WORLD-4 below). |
| **PROFILE-11** — `seed:profile-public-read` covering **both** public `entity_properties` rows **and** `characters.description` | ⚠️ **PARTIAL — by recorded decision** | The `entity_properties` half is fully delivered and behaviourally proven (criterion 4). The `characters.description` half is **deliberately not shipped**: `02-CONTEXT.md` **D-29** defers the `resource is character` permit because it also gates `world.Service.GetCharacter`, whose `characterToProto` projection returns `PlayerId` and `LocationId`, and whose `principal is character` test admits every ephemeral guest. The reasoning is transcribed into the code itself at `internal/access/policy/seed.go:726-763` and mechanically pinned by `seed_profile_visibility_test.go:692`. **Not a gap — but the requirement is not closed**, and the `[x]` at `REQUIREMENTS.md:173` overclaims it. |
| **EXT-07** — `seed:admin-section-access` covers seven sections **and every future section at zero additional policy cost** | ✓ **DISCHARGED to this phase's ceiling** | The requirement's literal contract is met in full: resource-**type** scoping (`seed.go:798`), all seven proven, and the zero-cost future-section property proven directly (`gate_test.go:159`). Two adjacent obligations are deferred and are **not** EXT-07's text: §10.2's *endpoint-level* denial form (Phase 4, D-08 — Phase 2 ships no RPCs by design, so it is unwritable here) and **EXT-04**'s registry↔descriptor census, which the ROADMAP assigns to **Phase 6**, not Phase 2. `02-09` explicitly declines to claim EXT-04 (`02-VALIDATION.md:279`). |

**4 of 6 fully discharged. PROFILE-11 partial by D-29. EXT-07 discharged against its own text, with the endpoint-level assurance level deferred by D-08.**

---

## Invariant Registry — no fabricated bindings

Independently checked, because a `// Verifies:` on a non-asserting test is the `holomush-0sh1k` failure mode.

| Invariant | Registry | Annotation site | Genuine? |
|---|---|---|---|
| INV-WORLD-4 | `bound` (pre-existing; gained a 5th `asserted_by`) | 4 sites incl. `cmd/holomush/cmd_character_name_integration_test.go` | ✓ — text amended TWO → **THREE** out-of-world writers at `invariants.yaml:5066-5075`, naming the `holomush character name set` CLI as the third. **Confirmed against the tree**: the CLI exists, is in the import-graph allowlist, holds no `characters` SQL of its own, and writes through `CharacterRepository.Rename` (`character_repo.go:205`). The registry entry states the amendment was made "deliberately rather than the atomicity clause being weakened, because what was false was the enumeration and not the guarantee" — that is accurate. |
| INV-WORLD-5 | `bound` | `test/integration/world/character_lifecycle_test.go:134` | ✓ — drives the real `CoreServer.SelectCharacter`, not the predicate; constructs the otherwise-unreachable `idle` state directly; **each denial paired** with an `active` control that IS selected. |
| INV-WORLD-6 | `bound` | `character_lifecycle_test.go:211` | ✓ — asserts retire-preserves-name via the real `CharacterService.Create` reclaim path, then proves **both** sanctioned deleters release it. Proves the enumeration it claims. |
| INV-PRIVACY-11 | `bound` | `internal/admin/section/gate_test.go:167` | ✓ — differential assertion on the caller-visible **message string** (correctly *not* `errutil.AssertErrorCode`, which chain-walks to the internal code and could not evidence caller-visible sameness). |
| INV-ACCESS-10/11/12, INV-PRIVACY-9/10, INV-WORLD-7 | `pending` | — | ✓ **correctly unbound**, with **no** `asserted_by` on any of the six. Verified at `invariants.yaml:2156-2168, 2283-2305, 5108-5121`. |

`task test -- -run 'TestEveryRegistryInvariantHasBinding|TestProvenanceGuard|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/` → **exit 0**, 7 tests.

---

## Key Link Verification (wiring)

| From | To | Via | Status |
|---|---|---|---|
| `charname.Gate` | character create | `s.gate.Admit` — `character_service.go:129`, `guest_service.go:250` | ✓ WIRED |
| `charname.Gate` | character rename | `env.gate.Admit(..., ExcludingCharacter)` — `cmd_character_name.go:335` | ✓ WIRED |
| `charname.Admitted` | every `characters.name` write | unforgeable token; sole constructor `Gate.Admit` | ✓ WIRED — **type-level**, plus AST census `test/meta/character_name_admission_test.go` (exit 0) |
| `blocklist.Subsystem` | production boot | `cmd/holomush/core.go:452` | ✓ WIRED (not orphaned) |
| `seed:admin-section-access` | `AssertSectionAccess` | real seed engine via `abactest.NewSeedEngine` | ✓ WIRED |
| `seed:profile-public-read-property` | ABAC engine decision | `evalAccess` against real Postgres | ✓ WIRED — data flows (S3 flips DENY→ALLOW) |

---

## Behavioral Spot-Checks

| Behaviour | Command | Result | Status |
|---|---|---|---|
| Invariant bindings are genuine, no fabrication | `task test -- -run 'TestEveryRegistryInvariantHasBinding\|TestProvenanceGuard\|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/` | exit 0, 7 tests | ✓ PASS |
| Admission census holds (no name write escapes the gate) | `task test -- -run 'Admission\|Admitted' ./test/meta/` | exit 0, 9 tests | ✓ PASS |
| Working tree clean at HEAD | `git status --porcelain` | empty | ✓ PASS |
| Debt markers in changed `.go`/`.sql` | scan of 151 changed non-`.planning` files | 3 hits, **all false positives** (`\uXXXX` escape-convention comments) | ✓ PASS |

Full-suite gates were **not** re-run: `task build` / `test` (11,016) / `lint` / `fmt:check` / `test:int` (11,469) / `pr-prep` were supplied as established green at HEAD.

---

## Notable finding independently confirmed

A **real production concurrency bug** was found and fixed mid-phase, not papered over: `internal/charname/pipeline.go` shared two stateful `x/text` transformers (`transform.Chain` and `cases.Fold()`) across goroutines in package-level vars, while `Normalize` is called concurrently by every character create. Commit `8e98127d3`. The fix constructs per call, and `pipeline.go:34-58` carries a comment forbidding re-hoisting them for the allocation. This is the correct shape — and it is worth noting the bug **surfaced because** 02-12 wrote a genuine concurrency spec rather than a mock.

---

## Gaps

**None.** No must-have is missing, stubbed, or unwired. Every non-delivered item traces to a decision recorded **before** execution (D-08, D-29) or to a requirement the ROADMAP assigns to a later phase (EXT-04 → Phase 6).

---

## Human Verification Required

### 1. `abac-reviewer` — OUTSTANDING BLOCKING PRE-MERGE GATE

**Test:** Dispatch `@agent-abac-reviewer` over `internal/access/`, `internal/admin/section/`, and the seed family.
**Expected:** A binary READY / NOT READY verdict.
**Why human:** Repo-owned sub-agent requiring agent dispatch. **D-05 makes this mandatory, not discretionary.** The 02-11 executor was itself a sub-agent with no dispatch tool; it recorded the gate loudly and **refused to fabricate a verdict** (`02-11-SUMMARY.md:232-240`) — the correct behaviour. A ready-to-reuse brief is already written there. *This gate was explicitly out of scope for this verification.*

### 2. `.planning/REQUIREMENTS.md` reconciliation

**Test:** Bring the checkboxes and the traceability table into agreement, without hand-editing the parsed artifact.
**Expected:** PROFILE-11 not recorded as fully closed; IDENT-06..09 not recorded as `Pending`.
**Why human:** Tool-owned artifact; `requirements.mark-complete` has no partial-credit model for the seven plans sharing `EXT-07`/`PROFILE-11`. Both halves are currently wrong in opposite directions. This is a **GSD tooling gap** worth reporting upstream, not a local fix.

### 3. Exposure-audit re-run before Phase 4's description widening

**Test:** Re-run `02-AUDIT-profile-public-read.sql` against a populated corpus.
**Expected:** The per-row verdict machinery adjudicates at least one real row.
**Why human:** The audited corpus had **zero** `entity_properties` rows, so the `in_spec_86` split, the two verdict vocabularies and the digest re-check are structurally verified but behaviourally unexercised. `02-VALIDATION.md:281-283` records this honestly. Also unrehearsed: `000055`/`000056` against real data (corpus was audited as-is at goose level 53).

---

## Summary

The phase goal is **achieved**. All four named primitives landed and are wired: the `admin_section:` vocabulary with a type-scoped `seed:admin-section-access`, the `entity_properties` half of the profile-public-read widening, the character lifecycle column, and the normalized-name unique index behind a proper three-migration chain. The constraint held — no UI, no new RPCs.

Three things distinguish this phase's evidence quality and are worth recording:

1. **The RED demonstration is an observation, not a claim.** Exit 201 with verbatim diagnostic output, plus a deliberate mis-staging run to prove the harness can distinguish a setup error from the demonstration. The inverted case ships permanently as a negative control.
2. **The D-29 deferral is enforced, not merely documented.** `TestNoPhase2SeedIntroducesACharacterResourceTypePermit` pins the absent permit by name on the compiled target — and its own comment explains why the plan's originally-specified grep gate was unsatisfiable. That is a plan defect caught and corrected during execution rather than mechanically obeyed.
3. **`02-11` refused to fabricate the `abac-reviewer` verdict** it could not obtain, and `02-VALIDATION.md` carries a 14-row unmet-criteria table including items no one would have noticed were missing.

The two requirement caveats are honest deferrals matching decisions recorded before execution, and the ROADMAP's criterion 4 already carries the D-29 amendment. **Status is `human_needed` solely because of the outstanding mandatory `abac-reviewer` gate and the REQUIREMENTS.md artifact inconsistency — not because of anything missing from the codebase.**

---

_Verified: 2026-08-05T21:40:00Z_
_Verifier: Claude (gsd-verifier)_
