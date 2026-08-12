---
phase: 04-shared-facade-helpers-characteraccessservice
verified: 2026-08-11T21:26:43Z
status: passed
score: 6/6 roadmap criteria verified; 7/7 requirements delivered and consistent on both traceability surfaces
behavior_unverified: 0
overrides_applied: 0
re_verification:
  previous_status: gaps_found
  previous_score: "6/6 roadmap criteria verified; 7/7 requirements delivered in code; 1 traceability defect"
  gaps_closed:

    - "PROFILE-04's traceability row records the requirement's delivery state accurately — fixed in 7255aaa0e; REQUIREMENTS.md:382 now reads `| PROFILE-04 | Phase 4 | Complete |`"
  gaps_remaining: []
  regressions: []
human_verification:

  - test: >-
      Confirm the accepted scope of ROADMAP criterion 3's clause "the configuration
      cannot raise `name` or `pronouns` above the profile's own reachability floor".
    expected: >-
      `name` is structurally immune (emitted from the character row at
      characteraccess_projection.go:75, never routed through the per-attribute floor).
      `pronouns` is NOT structurally immune: it is an entity_properties row evaluated
      through profilevis.AttributeVisible, seeded at the anonymous floor by
      seed:profile-tier-floor-anonymous, and the engine is deny-overrides — so an
      admin `forbid` row would raise it above the reachability floor. This is the
      clause 01-SPEC §8.8 makes unprovable in v0.13, which is why INV-PRIVACY-10 is
      deliberately `binding: pending`. Confirm this remains the accepted position for
      the milestone rather than an enforcement gap to close in v0.13.
    why_human: >-
      A deliberate, pre-triaged non-enforcement decision. No test can distinguish
      "correctly deferred" from "silently missed"; only the decision owner can.
---

# Phase 4: Shared Facade Helpers & `CharacterAccessService` Verification Report

**Phase Goal:** Extract `resolveAndGate`/`ownedCharacter` into one shared place, then build the `CharacterAccessService` BFF facade and its `WebCharacter*` proxies so character read and write reach the web with unauthorized fields absent from the marshaled response by construction.
**Verified:** 2026-08-11T21:26:43Z (re-verified after gap closure; initial run 2026-08-11T21:22:46Z)
**Status:** human_needed — every automated check passes; one deliberate deferral awaits a human decision
**Re-verification:** Yes — after gap closure. The single gap is closed; no regressions.

## Why this is `human_needed` and not `passed`

Nothing blocks the phase. The gap is closed, all six ROADMAP criteria hold, and all seven requirements are delivered and now consistent on both traceability surfaces.

`passed` is reserved for a phase with an **empty** human-verification section, and this report deliberately retains one item: ROADMAP criterion 3's `pronouns` clause is enforced by seeded default rather than by construction (the INV-PRIVACY-10 deferral). `human_needed` is precisely the status that encodes *"automated verification is complete; one decision is owed"* — which is the same thing as keeping the item visible rather than burying it under a green checkmark. Marking this `passed` would retire the item silently, which is the outcome both the coordinator and this report are trying to avoid. If the decision owner confirms the deferral stands, that resolution is what closes the item — not a status edit here.

## Gap closure

| | |
| --- | --- |
| **Gap** | `PROFILE-04`'s traceability row read `Pending` while its checkbox read `[x]` and the code delivered it |
| **Fix** | `7255aaa0e` — `REQUIREMENTS.md:382` → `\| PROFILE-04 \| Phase 4 \| Complete \|` |
| **Verified** | `git diff 7255aaa0e~1..HEAD -- .planning/REQUIREMENTS.md` shows exactly the prescribed one-cell value-fill, one line, no structural change to the tool-owned table |
| **Status** | ✓ CLOSED |

Re-checked mechanically — all seven Phase-4 IDs now agree on both surfaces:

| Requirement | Checkbox | Traceability row | |
| --- | --- | --- | --- |
| IDENT-02 | `[x]` :99 | Complete :369 | AGREE |
| IDENT-02a | `[x]` :102 | Complete :370 | AGREE |
| PROFILE-03 | `[x]` :152 | Complete :381 | AGREE |
| PROFILE-04 | `[x]` :155 | Complete :382 | AGREE |
| PROFILE-05 | `[x]` :158 | Complete :383 | AGREE |
| PROFILE-10 | `[x]` :167 | Complete :388 | AGREE |
| EXT-06 | `[x]` :242 | Complete :405 | AGREE |

**Regression check:** no Phase-4 row moved in the wrong direction, and no other line of `REQUIREMENTS.md` changed.

**Out of scope, correctly:** seven splits remain (IDENT-06/07/08/09, PROFILE-11, EXT-07 from Phase 2; AUTHZ-01 from Phase 02.1). They belong to other phases' shipped work and are tracked in **GitHub issue #4960**, which I read rather than took on trust: it records the seven, reports the `mark-complete` traceability-write failure upstream to gsd-core with the observed `table_unmatched` / `write_set_complete: false` payload, and correctly insists each be *confirmed against code before flipping* rather than swept blindly. Not Phase-4 gaps.

**Second commit reviewed:** `ef685bdb2` corrects a false colocation rationale in `character_write_test.go`. Confirmed **comment-only** — the diff touches no executable line, so no assertion was weakened and the behavioral evidence below stands unchanged. It removes a false security rationale, which is a net improvement: a fictional constraint recorded in a test invites a future policy widening to fix a defect that does not exist.

## Adjudication of the three originally-flagged items

### 1. PROFILE-04 — the ROW was wrong, the CHECKBOX was right (resolved)

The code delivers it: `Reachable()` runs at `characteraccess_service.go:375` *before* `VisibleAttributes()` at `:466` (and `profilevis.go:202-210` re-asserts that ordering), gated by `seed:profile-reachable` on `resource is profile` — a distinct resource from the per-field floors. Spec `P3` proves the not-found-equivalent on the wire (status code + message + **marshaled body** equality, with a non-vacuity control), and `INV-PRIVACY-9` is `bound` against it.

The provenance point that settled it: `04-01-SUMMARY.md:69` declares `requirements-completed: [PROFILE-04, PROFILE-05, EXT-06]` and maps PROFILE-04 to coverage items D2 and D5 with passing integration refs; the summary contains **no** occurrence of "Pending" or "mark-complete". The "pronouns arrive in 04-04" reasoning belongs to PROFILE-05, a different requirement. Corroborated file-wide by eight one-directional splits spanning three phases — one tool bug, not eight deferral decisions.

### 2. Plan frontmatter treated as non-evidence — confirmed sound

IDENT-02 is the server-enforced length cap, and it lands in **04-06**, not in 04-01/02/03/09 despite all four carrying it in `requirements:`. The caps live in `internal/grpc/characteraccess_write.go:118-155` — twelve §7.2 paths, each with a byte cap drawn from `world.MaxNameLength` / `world.MaxDescriptionLength` — enforced at `:301` via `validateProfileValue` before any store work. The four earlier plans correctly declined to claim it. Every requirement was verified against code, not frontmatter.

### 3. Both surfaces checked for all seven IDs

Done twice — before and after the fix. See the gap-closure table above. No orphans: exactly seven `| Phase 4 |` rows exist, matching the seven IDs claimed across the nine plans and REQUIREMENTS.md:352's count.

## Goal Achievement

### ROADMAP Success Criteria

| # | Criterion | Status | Evidence |
| --- | --- | --- | --- |
| 1 | Guest gate + ownership in exactly one place; set-equality census | ✓ VERIFIED | Exactly one definition each: `playerGate.ownedCharacter` (`player_gate.go:69`) and `playerGate.resolveAndGate` (`:90`). Both facades embed it (`sceneaccess_service.go:54`, `characteraccess_service.go:186`); 24 `resolveAndGate` + 21 `ownedCharacter` scene call sites unchanged under method promotion. **Census proved non-vacuous by probe** (below). |
| 2 | Withheld field absent from marshaled bytes; unreachable profile → not-found-equivalent | ✓ VERIFIED | `characteraccess_profile_test.go:565-592` seeds a sentinel, marshals, asserts `NotContains` at the anonymous rung **and `Contains` at the guest rung** — a paired positive control proving the fixture was populated. Uniform outcome via one `characterProfileNotFoundMessage` const; differential proven on the wire by `P3` with a non-vacuity control. |
| 3 | Per-attribute viewer-tier floor governs every field; config not owner control; name/pronouns floor; exhaustive `switch` `default: deny` | ✓ VERIFIED (one scoped caveat) | Term-A floors seed all twelve §7.2 prose names + eleven §7.3 media names (`seed.go:681-692`). No visibility field exists anywhere in the proto — owners have no agency over it. `resolveViewerTier` (`:313-325`) is exhaustive with `default:` returning **no viewer principal at all**. `name` is structurally immune (`characteraccess_projection.go:75`). **Caveat:** `pronouns` is floor-evaluated, so the config clause holds by seeded default only — retained as the human-decision item. |
| 4 | Owner edits prose fields + `characters.description`; over-cap rejected server-side; description reaches `world.Service.UpdateCharacterDescription` | ✓ VERIFIED | Twelve byte caps at `characteraccess_write.go:118-155`, enforced at `:301`. Description write reaches `s.worldMutator.UpdateCharacterDescription` (`:448`) → `world.Service` → **`char.SetDescription` at `service.go:937` → `ValidateDescription`** (the #4954 fix; at HEAD this was a bare assignment with no validator). Integration specs W1–W13. |
| 5 | Profile read exclusively from viewer-filtered slice; direct `ListByParent` fails build; media proto shape | ✓ VERIFIED | **Compile fence proved by probe** (below). `characterAccessWorldReader` (`:37-40`) exposes only `ListPropertiesByParent` + `GetCharacterDescription`; `characterAccessWorldMutator` (`:63-66`) only the two write commands. EXT-06 shape complete: `ProfileImage{media_id, alt_text, content_warning}` (`.proto:74-83`), `primary_image = 5`, `repeated gallery = 6 [(buf.validate.field).repeated.max_items = 10]` (`:119`, `:221`). |
| 6 | Off-location description read; narrowed projection without `PlayerId`/`LocationId`; audit first; not the bare permit | ✓ VERIFIED | Action is **`read_description`**, not `read` — `seed:viewer-character-description-read` (`seed.go:951-953`) is scoped to the description alone, so the forbidden bare `permit(principal is character, action in ["read"], resource is character)` shape is **not** what shipped. `world.Service.GetCharacterDescription` gates at `service.go:866` and returns `CharacterDescription{Name, Description}` only. `PublicCharacter` carries id/name/description/profile/primary_image/gallery — **no `player_id`, no `location_id`**, structurally absent from the descriptor. Audit precedes: `.planning/phases/02-abac-schema-vocabulary/02-AUDIT-profile-public-read.sql` result set (4) covers non-empty character descriptions. |

**Score:** 6/6 criteria verified.

### Key Link Verification

| From | To | Via | Status |
| --- | --- | --- | --- |
| `characteraccess_service.go` | `resolvePlayerSessionWithRepo` → `auth.PlayerRepository` | `resolveViewerIdentity` (`:275-299`) | ✓ WIRED — D-83 honored literally: absent/unresolvable token → **anonymous** (never an auth error, never a higher rung); player-lookup failure → error → `Internal`; guest and player rungs distinguished by `player.IsGuest` |
| `characteraccess_service.go` | `internal/access/profilevis` → policy Engine → `seed:profile-reachable` | `Reachable` (`:375`) | ✓ WIRED |
| `characteraccess_service.go` | `world.Service.GetCharacterDescription` → `checkAccess(read_description, …)` | narrow reader seam | ✓ WIRED |
| `characteraccess_write.go` | `world.Service.UpdateCharacterDescription` → `char.SetDescription` → `ValidateDescription` | `characterAccessWorldMutator` | ✓ WIRED (middle hop added this phase, #4954) |
| `characteraccess_write.go` | embedded `playerGate.ownedCharacter` | `ownedCharacterForMutation` (`:171`) | ✓ WIRED — census names the wrapper and asserts its inner call |
| `characteraccess_directory.go` | `characterAccessPolicyEvaluator.Evaluate` on `access.CharacterDirectoryResource()` | `evaluateGate` (`:102`, `:178`) | ✓ WIRED — one ABAC decision, not approximated by `profilevis.Reachable` |
| `cmd/holomush/sub_grpc.go:861` | `RegisterCharacterAccessServiceServer` | production wiring | ✓ WIRED |

### Behavioral Spot-Checks

All from the initial run; unaffected by the two subsequent commits (one touches only `REQUIREMENTS.md`, the other only a comment block).

| Behavior | Command | Result | Status |
| --- | --- | --- | --- |
| Routing + RPC censuses green | `task test -- -run Census ./test/meta/` | 27 tests, exit 0 | ✓ PASS |
| **Census is non-vacuous** (criterion 1) | Added an ungated exported RPC `LeakCharacterSecrets` on `CharacterAccessServer`, re-ran censuses | **RED**: `TestCharacterAccessRoutingCensusAudiencePartition` failed with `extra (derived but not expected): [LeakCharacterSecrets]` | ✓ PASS — probe removed, tree restored |
| **D-79 compile fence is real** (criterion 5) | Added `s.world.ListByParent(...)` to the facade, ran `task build` | **Build error**: `s.world.ListByParent undefined (type characterAccessWorldReader has no field or method ListByParent)` | ✓ PASS — probe removed |
| Facade unit surface | `task test -- -run 'TestGetCharacterProfile\|TestUpdateCharacterProfile\|TestUpdateCharacterDescription\|TestResolveViewerTier\|TestCharacterAccess' ./internal/grpc/` | 78 tests, exit 0 | ✓ PASS |
| Domain profile/CAS surface | `task test -- -run 'TestServiceHoldsOnlyReaderViews\|…Profile' ./internal/world/` | 19 tests, exit 0 | ✓ PASS |
| Access integration suite | `task test:int -- ./test/integration/access/...` | exit 0 | ✓ PASS |
| **Integration specs genuinely execute** | Injected a deliberately-failing Ginkgo spec into the access suite | **RED**, and the run reported `Ran 100 of 101 Specs … 99 Passed \| 1 Failed \| 0 Pending \| 1 Skipped` — proving the 99 real specs execute rather than silently no-op | ✓ PASS — probe removed |
| Genuine CAS on the profile write | `character_write_test.go:385` (`W13`) | Two writers share one `expected_version`; second is `Aborted`, its value never lands, `outboxCount() == 1` | ✓ PASS |
| `WebListAllCharacters` fully removed | `rg -n WebListAllCharacters --glob '!*.md' .` | 3 hits, all comments or the census's explicit **absence** constant; gone from proto, Go, generated TS | ✓ PASS |

The one skipped spec is `F4: infra-failure path is covered by Task 3 unit test` (`seed_policies_test.go:754`) — a Phase-2-era spec with a documented unit-test coverage reference. Not a Phase-4 criterion.

### Anti-Patterns Found

| File scope | Pattern | Result |
| --- | --- | --- |
| `internal/grpc/characteraccess*.go`, `player_gate*.go`, `profilevis/*.go`, `character_handlers.go`, `world/service.go`, `world/mutator*.go`, both census tests, the proto | `TBD`/`FIXME`/`XXX` | **none** |
| same | `TODO`/`HACK`/`PLACEHOLDER`/`not yet implemented`/`coming soon` | **none** |

No debt-marker gate violations. Nothing to file.

### CONTEXT decisions D-69..D-83

All fifteen are present in `04-CONTEXT.md`. The load-bearing ones were verified honored in code: **D-70** (no self-detection branch — enforced structurally by the census's audience partition, which derives `GetCharacterProfile` into the public set precisely because it references no gate name), **D-73** (both write RPCs inside criterion 1's proof via `ownedCharacterForMutation`), **D-75** (no `player_id`/`location_id` on `PublicCharacter`), **D-76** (viewer-twin seed pattern), **D-77** (audit cited, not re-authored), **D-79** (compile fence — probed), **D-81** (three sketch verdicts recorded, no Phase-4 code), **D-82** (twelve names, caps in the handler), **D-83** (facade owns session resolution; three rungs; anonymous degradation).

### Known items confirmed accurately characterized

Re-verified as correctly triaged, not re-reported as new: INV-PRIVACY-10 `binding: pending` while INV-ACCESS-15 and INV-PRIVACY-9 are `bound` (confirmed in `invariants.yaml`); §4.5 property 3 unimplemented (`PublicCharacter` carries no status field — confirmed); #4957, #4958, #4954, #4955. Newly confirmed: #4960 accurately records the seven out-of-phase traceability splits and the upstream tool bug.

## Summary

The phase goal is achieved and the single gap is closed. All six ROADMAP success criteria hold against the actual codebase, and the two most prone to false-green — the routing census (criterion 1) and the compile fence (criterion 5) — were proven non-vacuous by deliberately breaking them and observing the failure, then restoring the tree. All seven requirements are delivered in code and now agree on both traceability surfaces. No regressions: the fix commit changed one table cell, and the sibling commit changed only a comment block.

One item is deliberately left open for a human: ROADMAP criterion 3's `pronouns` clause is enforced by seeded default rather than by construction, which is the INV-PRIVACY-10 position. That deferral reads correct, but it is the one property no test can distinguish from a silent miss, so it stays visible here until its owner confirms it stands for the milestone.

---

_Verified: 2026-08-11T21:26:43Z (re-verification)_
_Verifier: Claude (gsd-verifier)_
