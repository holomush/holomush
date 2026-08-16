---
phase: 02-abac-schema-vocabulary
plan: 07
subsystem: access-control
tags: [abac, seed-policies, viewer-tier, profile-visibility, admin-section, d-27, d-29, test-support]

requires:
  - phase: 02-abac-schema-vocabulary
    provides: "02-03's viewer/profile/admin_section prefixes and the viewer namespace; 02-13's registered providers and the three derived player-keyed property peers"
provides:
  - "seed:profile-tier-floor-anonymous / seed:profile-tier-floor-guest — term A of §8.5.1's conjunction, carrying §8.2.1's clearing tests verbatim over literal §8.6 name lists"
  - "The read_profile_attribute action token — §8.5.1's term-A/term-B separator"
  - "seed:profile-reachable — §8.4.2's reachability policy, transcribed verbatim"
  - "Five viewer read twins keyed on the derived player peers and principal.viewer.roles (D-01); seed:property-owner-write has none"
  - "seed:profile-public-read-property — the additive PROFILE-11 widening, guarded on parent_type == \"character\""
  - "seed:admin-section-access — player-flavored, resource-TYPE-scoped (EXT-07)"
  - "internal/testsupport/abactest — a real-engine builder over the full seed corpus via the exported NewCompiler → NewCache → Reload → NewEngine path, plus schema-pinned viewer/player doubles and a property double that DERIVES the three player-keyed peers"
  - "An attribute-reference coverage gate: every attribute the ten new seeds reference resolves against a registered provider's declared schema"
affects: [02-08, 02-09, 02-10, 02-11, phase-04-character-access-service, phase-06-admin-surfaces]

actuals:
  tokens: 96000
  tasks: 4
  commits: 4

tech-stack:
  added: []
  patterns:
    - "A distinct ACTION TOKEN as the separator between two policy families that share a resource type, so a caller-ANDed conjunction cannot silently collapse into the engine's disjunctive permit combining"
    - "Spec table transcribed into a test as the ORACLE, never derived from the artifact under test, with a conditional guard that is green while its antecedent is false and turns RED the moment the omission's cause disappears"
    - "Attribute-reference coverage as a compile-time-shaped gate: extract every DSL reference and resolve it against the real providers' declared schemas, because an unsupplied reference denies forever with no error and no failing behavioural test"
    - "A test-support double that DERIVES a value rather than accepting it, paired with a differential test against the real implementation — schema parity alone would let a suite assert a decision it never exercises"
    - "An external test package (`package policy_test`) in the package's own directory to consume a testsupport helper that imports the package under test, where an in-package test file would be an import cycle"

key-files:
  created:
    - internal/access/policy/seed_profile_visibility_test.go
    - internal/access/policy/seed_profile_smoke_test.go
    - internal/testsupport/abactest/abactest.go
    - internal/testsupport/abactest/abactest_test.go
  modified:
    - internal/access/policy/seed.go
    - internal/access/policy/seed_test.go
    - internal/access/policy/bootstrap_test.go
    - internal/access/setup/buildabacstack_seed_coverage_integ_test.go
    - test/integration/access/seed_policies_test.go

key-decisions:
  - "Task 2's checkpoint:decision resolved to `author-now-gate-on-audit` — the option the planner front-loaded. Auto mode was active (workflow.auto_advance true) and the gate is `blocking`, not `blocking-human`. D-11's posture, which the resume-signal also asked to confirm, is already LOCKED in 02-CONTEXT.md; accepting it is not a new decision. The merge gate stands: the phase MUST NOT merge before 02-AUDIT-RESULT.md exists and is non-empty."
  - "The plan's D-29 acceptance criterion — a file-wide grep asserting ZERO `resource is character` in non-comment lines of seed.go — is unsatisfiable against this file and always was. Replaced with a compiled-target gate. See Deviations."
  - "createSeedEngine could NOT be reimplemented as a call into abactest.NewSeedEngine: Go rejects an in-package test file importing a package that depends on the package under test. The B-6 closure it was meant to buy is bought by an external test package in the same directory. See Deviations."
  - "S3 in test/integration/access asserted the PRE-widening posture and now asserts the post-widening one, with a new S3b paired control proving the widening's parent_type guard is load-bearing rather than decorative."

patterns-established:
  - "Constructing an excluded literal (fmt.Sprintf(\"profile.image.gallery.%d\", 10)) rather than writing it, so a test can name the member §8.6 excludes without seeding the string where a grep gate would find it."
  - "Recording a deferral AT THE SITE with its reasons, ordered so the reasoning paragraph that must not appear to justify the deferred shape sits AFTER the shape it discusses — the grep gate reads a -B window."

requirements-completed: []

coverage:
  - id: D1
    description: "One tier-floor policy per rung that has at least one seeded §8.6 member, each carrying §8.2.1's clearing test verbatim ANDed with the literal list of that rung's §8.6 names"
    requirement: PROFILE-11
    verification:
      - kind: unit
        ref: "internal/access/policy/seed_profile_visibility_test.go#TestSeededTierFloorPoliciesCoverExactlyTheSpec86NamesAtEachRung / TestSeededTierFloorPoliciesCarryTheVerbatimSpec821ClearingTest / TestExactlyTheRungsWithASeededMemberCarryATierFloorPolicy"
        status: pass
    human_judgment: false
  - id: D2
    description: "D-03's re-entry condition is guarded: a player-rung tier-floor policy is required exactly when §8.6 seeds a name at that rung"
    requirement: PROFILE-11
    verification:
      - kind: unit
        ref: "internal/access/policy/seed_profile_visibility_test.go#TestAPlayerRungTierFloorPolicyIsRequiredExactlyWhenSpec86SeedsAName"
        status: pass
    human_judgment: false
    rationale: "Green today because its antecedent is false. The assertion that MATTERS fires only when someone raises a §8.6 row to `player`; until then it proves the omission is still correct, not that the required policy would be written."
  - id: D3
    description: "No tier-floor policy uses an ordinal tier comparison, a numeric rank, or a glob/prefix/wildcard over the profile namespace; the eleven media names are enumerated and the twelfth is not"
    requirement: PROFILE-11
    verification:
      - kind: unit
        ref: "internal/access/policy/seed_profile_visibility_test.go#TestNoTierFloorPolicyUsesAnOrdinalTierComparison / TestNoTierFloorPolicyMatchesAProfileNameByAnythingButAWholeString / TestTheElevenMediaNamesAreEnumeratedAndTheTwelfthIsNot"
        status: pass
    human_judgment: false
  - id: D4
    description: "The read_profile_attribute action separates term A from term B — exactly the tier-floor family carries it"
    requirement: PROFILE-11
    verification:
      - kind: unit
        ref: "internal/access/policy/seed_profile_visibility_test.go#TestTheTierFloorFamilyOwnsTheReadProfileAttributeActionAlone"
        status: pass
    human_judgment: false
  - id: D5
    description: "seed:profile-reachable is §8.4.2's policy verbatim and reads no resource attributes"
    requirement: PROFILE-11
    verification:
      - kind: unit
        ref: "internal/access/policy/seed_profile_visibility_test.go#TestSeedProfileReachableIsTranscribedFromSpec842"
        status: pass
    human_judgment: false
  - id: D6
    description: "Five viewer read twins exist, mirroring their originals' effect and actions; seed:property-owner-write has none and no viewer seed carries a write action"
    requirement: PROFILE-11
    verification:
      - kind: unit
        ref: "internal/access/policy/seed_profile_visibility_test.go#TestExactlyTheFiveReadSideRowKeyedPoliciesHaveAViewerTwin / TestSeedPropertyOwnerWriteHasNoViewerTwin / TestEachViewerTwinMirrorsItsOriginalsEffectAndActions"
        status: pass
    human_judgment: false
  - id: D7
    description: "No viewer twin references a character-keyed row field; the identity-bearing twins key on the derived player peers and principal.viewer.roles, and the restricted twins keep their `resource has` guard retargeted"
    requirement: PROFILE-11
    verification:
      - kind: unit
        ref: "internal/access/policy/seed_profile_visibility_test.go#TestNoViewerTwinReferencesACharacterKeyedRowField / TestTheIdentityBearingViewerTwinsKeyOnTheDerivedPlayerPeers / TestTheRestrictedViewerTwinsKeepTheirHasGuardRetargeted"
        status: pass
    human_judgment: false
  - id: D8
    description: "Every attribute any new policy references is supplied by a registered provider, resolved against the REAL providers' declared schemas"
    requirement: PROFILE-11
    verification:
      - kind: unit
        ref: "internal/access/policy/seed_profile_visibility_test.go#TestEveryAttributeAnewSeedReferencesIsSuppliedByARegisteredProvider (demonstrated RED against a deliberately misspelled attribute)"
        status: pass
    human_judgment: false
  - id: D9
    description: "seed:profile-public-read-property is additive and guarded on parent_type; the shipped colocation and public-read policies are untouched at their existing SeedVersion"
    requirement: PROFILE-11
    verification:
      - kind: unit
        ref: "internal/access/policy/seed_profile_visibility_test.go#TestSeedProfilePublicReadPropertyIsAnAdditivePermitGuardedOnCharacterRows / TestTheShippedRowKeyedFamilyIsUntouchedByTheWidening"
        status: pass
      - kind: integration
        ref: "test/integration/access/seed_policies_test.go#S3 (post-widening ALLOW) + S3b (the parent_type guard, demonstrated RED without it)"
        status: pass
    human_judgment: false
  - id: D10
    description: "No Phase-2 seed introduces a character-resource-type permit (D-29), and seed:profile-public-read-character does not exist"
    requirement: PROFILE-11
    verification:
      - kind: unit
        ref: "internal/access/policy/seed_profile_visibility_test.go#TestNoPhase2SeedIntroducesACharacterResourceTypePermit"
        status: pass
    human_judgment: false
  - id: D11
    description: "seed:admin-section-access is player-flavored and scoped by resource TYPE with no enumerated id, carrying both read and write"
    requirement: EXT-07
    verification:
      - kind: unit
        ref: "internal/access/policy/seed_profile_visibility_test.go#TestSeedAdminSectionAccessIsTypeScopedAndPlayerFlavored"
        status: pass
    human_judgment: false
    rationale: "Structural only. That the policy actually PERMITS an admin and DENIES a non-admin over the seven registered sections is plan 02-09's, with its paired positive controls; this plan deliberately ships no denial tests."
  - id: D12
    description: "The whole seed corpus compiles and loads against the real engine through the exported path"
    requirement: EXT-07
    verification:
      - kind: unit
        ref: "internal/access/policy/seed_profile_smoke_test.go#TestTheWholeSeedCorpusCompilesAndLoadsAgainstTheRealEngine + internal/testsupport/abactest/abactest_test.go#TestNewSeedEngineLoadsTheWholeCorpusThroughTheExportedPath (demonstrated RED against a malformed DSL string)"
        status: pass
    human_judgment: false
  - id: D13
    description: "Each provider double's schema equals its real counterpart's, and the property double derives the three player-keyed peers the way the real provider derives them"
    requirement: PROFILE-11
    verification:
      - kind: unit
        ref: "internal/testsupport/abactest/abactest_test.go#TestEachDoubleDeclaresExactlyItsRealCounterpartsSchemaKeys / TestTheDerivedPeersAgreeWithTheRealPropertyProvider / TestThePropertyDoubleExposesNoSetterForTheDerivedPeers"
        status: pass
    human_judgment: false
  - id: D14
    description: "The seed-coverage sweep is silent about the viewer namespace, with a paired positive control establishing the corpus references it"
    requirement: EXT-07
    verification:
      - kind: integration
        ref: "internal/access/setup/buildabacstack_seed_coverage_integ_test.go#TestBuildABACStackSeedCoverageIsSilentAboutTheViewerNamespace (demonstrated RED with the ViewerTierProvider registration removed)"
        status: pass
    human_judgment: false

duration: 118min
completed: 2026-08-04
status: complete
---

# Phase 02 Plan 07: Profile-Visibility, Viewer-Twin and Admin-Section Seed Policies Summary

**Ten new seed policies land as one reviewable artifact — the two viewer-tier floors, profile reachability, the five viewer read twins keyed on 02-13's derived player peers, the PROFILE-11 property widening, and the type-scoped admin-section gate — with the `resource is character` permit D-29 defers recorded as an absence rather than an omission.**

## Performance

- **Duration:** 118 min
- **Tasks:** 4 of 4 (one checkpoint, three execute)
- **Files modified:** 9 (4 created, 5 modified)

## Task Commits

| Task | Name | Commit | Key files |
| --- | --- | --- | --- |
| 1 | Two tier-floor policies and profile reachability | `b928a5d04` | `internal/access/policy/seed.go`, `seed_profile_visibility_test.go` |
| 2 | Confirm the seed:profile-public-read widening posture | (no commit — decision only) | — |
| 3 | Viewer read twins and the PROFILE-11 widening | `1d3a8ad0d` | `internal/access/policy/seed.go` |
| 4 | Admin-section access, the shared engine builder, the coverage sweep | `cc377d20e` | `internal/testsupport/abactest/`, `internal/access/setup/buildabacstack_seed_coverage_integ_test.go` |
| — | S3/S3b: the widening's intended behaviour change (Rule 1) | `8a5dc3408` | `test/integration/access/seed_policies_test.go` |

Each task was written test-first and observed RED before implementation:

| Task | RED command | RED exit |
| --- | --- | --- |
| 1 | `task test -- ./internal/access/policy/` | `201` (13 failures — every tier-floor and reachability assertion) |
| 3 | `task test -- ./internal/access/policy/` | `201` (the five twins and the widening absent) |
| 4 | `task test -- ./internal/access/policy/` | `201` (`seed:admin-section-access` absent) |

RED/GREEN landed in one commit per task rather than separate `test(...)` / `feat(...)` commits, matching 02-03's and 02-13's recorded precedent in this phase: the plan pairs each seed with its assertions as one atomic unit and a test-only commit would fail the corpus-inventory guards.

## Task 2 — the checkpoint, and how it resolved

A `checkpoint:decision` with `gate="blocking"`, asking whether to author both `seed:profile-public-read-*` permits now and gate the merge on plan 02-10's audit.

Auto mode was active (`workflow.auto_advance` is `true`, `_auto_chain_active` is `false`) and the gate is `blocking`, not `blocking-human`, so the first option was selected: **`author-now-gate-on-audit`** — logged as `⚡ Auto-selected: author-now-gate-on-audit`.

This is not a guess standing in for a maintainer. The checkpoint's `resume-signal` also asks the operator to confirm D-11's posture — that the fix for any exposed row is that row's `visibility`, never a narrowed policy — and **D-11 is already LOCKED in `02-CONTEXT.md`**. What the checkpoint genuinely settles is *timing*, and the front-loaded option is the one that keeps `seed.go` under a single plan's ownership and lets 02-10 write the paired control its criterion-4 proof needs.

**The merge gate stands and is this plan's obligation to state, not 02-10's alone:** the phase MUST NOT merge until `.planning/phases/02-abac-schema-vocabulary/02-AUDIT-RESULT.md` exists and is non-empty. The permits are authored; the audit has not run.

Note also that the count the checkpoint speaks of — "both permits" — is **one** permit in the shipped artifact. D-29 removed the character half from this phase before execution (see below).

## Accomplishments

- **The two families stay in separate evaluations, and the mechanism is a distinct action token.** §8.5.1's conjunction is only safe if term A and term B cannot both match one evaluation. If both carried `read` against the same `property:<id>`, `combineDecisions` returns the first satisfied permit and the caller's AND would silently reduce to the additive shape §8.5.1.1 exists to prevent — publishing `visibility='private'` rows to every viewer that clears the name's floor. The tier floors carry `read_profile_attribute`; nothing else in the corpus does, and a test pins that.
- **The clearing tests are set membership, and the reason is recorded where it will be read.** `spectator`, `unverified` and `visitor` all sort lexicographically above `player` in Go byte order, so a `>=` test would hand a newly appended fourth rung the highest clearance in the system on the day the token is added, with no policy edit anywhere.
- **The viewer twins compare player to player.** The shipped row semantics are character-keyed; a `viewer:` subject is player-flavored. The earlier draft compared `principal.viewer.player_id` against `resource.property.owner` — which never matches, and a non-matching key is FALSE, so every `private`, `restricted` and `admin` field would have gone permanently invisible: fail-closed, no error, no failing behavioural test, with "the profile looks bare" as the visible symptom §8.5.1.1 records as provoking the forbidden repair. The twins now key on 02-13's derived peers, and a test rejects any reference to the three bare character-keyed field names (matching on the EXACT key, so the legal `owner_player_id` is not swept in by its shared prefix).
- **D-27's derivation direction is recorded at the site, because the DSL text cannot show it.** The twins read identically to their character-flavored originals. A reviewer asking "does the viewer path widen access across a player's alternate characters?" has nowhere to look but the block comment, so it states the ALL/ANY split, says the plain union was declined for the permit side, and cites D-27.
- **The attribute-reference coverage gate closes the whole defect class, not the one instance.** A policy naming an attribute nothing supplies denies forever, silently — and no behavioural test can distinguish that from a policy that is correctly denying. Every reference in all ten new seeds is now resolved against the REAL providers' declared schemas (not a transcription, which would go stale in the safe-looking direction), and the gate was observed RED against a deliberately misspelled `principal.viewer.teir`.
- **`abactest` builds through the exported path, which is strictly better than what it replaces.** `createSeedEngine` hand-installs a snapshot past the compiler; `abactest.NewSeedEngine` compiles every seed through `Cache.Reload`, so a seed whose DSL does not compile fails at construction. It needs no database, so 02-08 and 02-09 stay in the untagged lane.
- **The property double DERIVES rather than accepts.** Schema parity proves a key is *declared*; it says nothing about how the value was *derived*. A double with a `VisibleToPlayers` setter would let 02-08's privacy suite assert D-27 while never exercising it. The double takes character-keyed inputs plus a character→player map, computes the peers in D-27's two directions with an INDEPENDENT implementation, and a differential test drives one fixture through both it and the real `attribute.PropertyProvider`.

## `<verification_integrity>` rule 4 — gates demonstrated RED

All three required gates were **observed failing against a deliberately reverted state**, not reasoned about. Each mutation was reverted immediately and the suite re-run green.

| Gate | Mutation | RED exit | What was observed |
| --- | --- | --- | --- |
| Corpus-compiles smoke | Dropped the closing brace from `seed:admin-section-access`'s `when` clause | `201` | `policy cache reload: compile "seed:admin-section-access" (id=abactest-seed-59): parse error: … unexpected token "when" (expected ";")` — surfacing at `Reload`, which is the point of building through the exported path |
| Attribute-reference coverage | `principal.viewer.tier` → `principal.viewer.teir` on the anonymous floor | `201` | `seed:profile-tier-floor-anonymous references principal.viewer.teir, which the real viewer provider does NOT declare … Supplied keys: [has_player_id has_roles player_id roles tier]` |
| Seed-coverage `viewer` no-WARN | Removed `resolver.RegisterProvider(viewerProvider)` from `BuildABACStack` | `201` | The WARN fired naming all seven viewer-referencing seeds; three specs went RED including the new one |

A fourth, not required but earned: **S3b's `parent_type` guard**. Dropping `&& resource.property.parent_type == "character"` from the widening turns S3b RED (`201`), so the guard is proven load-bearing rather than decorative — without it the widening would publish public properties on *locations* off-location too.

The three substantive behavioural RED demonstrations this family needs — the ordinal-comparison fourth-rung gate, the term-B-removed additive-permit gate, and the prefix-match totality gate — belong to plan `02-08` and are recorded there. This plan ships no denial tests, so `<verification_integrity>` rule 2's pairing obligation is **not** satisfied here by the absence of the tests it governs.

## Deviations from Plan

### 1. [Rule 3 — Blocking] The D-29 acceptance criterion is unsatisfiable as written; replaced with a stronger gate

**Found during:** Task 3.

The plan's criterion and its `<prohibitions>` verification were:

```
[ "$(rg -v '^\s*//' internal/access/policy/seed.go | rg -o 'resource is character' | wc -l)" -eq 0 ]
```

That count is **3 before this plan and 3 after**, and always was:

| Line | Policy | Why it matches |
| --- | --- | --- |
| 16 | `seed:player-self-access` | `…, resource is character)` |
| 28 | `seed:player-character-colocation` | `…, resource is character)` |
| 319 | `seed:directory-list-characters` | `resource is character_directory` — the substring |

Satisfying it literally would mean deleting shipped policies. The two character-typed ones are **conditioned** (self-identity, colocation), so neither is the unconditional shape D-29 defers, and the third is a different resource type the grep cannot distinguish.

**What D-29 actually forbids is seeding a NEW one**, so the gate is stated on the **compiled target** instead: `TestNoPhase2SeedIntroducesACharacterResourceTypePermit` pins the set of seed policies whose resource clause is the `character` type to exactly `{seed:player-self-access, seed:player-character-colocation}`, and separately asserts `seed:profile-public-read-character` does not exist. It goes RED by name on any addition, and it is immune to both of the grep's failure modes — `character_directory` is a different type, and `resource == "character:*"` is an exact-match clause.

**D-29 itself was honoured in full.** No `resource is character` permit was added in any form, narrowed or not.

The two *other* D-29 criteria were satisfiable and pass as written:
- `rg -n 'D-29' seed.go` matches (the deferral is recorded at the site with its four reasons).
- `rg -B 6 'resource is character' seed.go | rg -o 'D-11' | wc -l` is `0` — the deferral comment names the shape it defers, and the "this is NOT an instance of D-10/D-11" paragraph is placed strictly *after* that mention so it never enters the grep's `-B` window.

### 2. [Rule 3 — Blocking] `createSeedEngine` cannot delegate to `abactest` — import cycle

**Found during:** Task 4.

The plan directed: *"Keep `createSeedEngine` where it is and reimplement it as a thin call into `abactest.NewSeedEngine`, so there is one builder rather than two that can diverge."*

That does not compile. `seed_smoke_test.go` is `package policy`; `abactest` imports `internal/access/policy`; Go rejects an in-package test file importing a package that depends on the package under test. Verified empirically before working around it:

```
imports github.com/holomush/holomush/internal/testsupport/abactest from seed_smoke_test.go
imports github.com/holomush/holomush/internal/access/policy from abactest.go: import cycle not allowed in test
```

This is the same class of finding as the plan's own B-6 (cycle 1 moved the *name* across a package boundary while the *mechanism* stayed behind it) — one layer further out.

**The intent was preserved, not dropped.** The B-6 closure the delegation was meant to buy — *"if the exported path could not build an equivalent engine, `internal/access/policy`'s own existing tests would go RED here rather than three downstream plans discovering it"* — is bought by a new **external test package** in the same directory, `internal/access/policy/seed_profile_smoke_test.go` (`package policy_test`), which is exempt from the cycle rule. `task test -- ./internal/access/policy/` therefore still goes RED if `abactest.NewSeedEngine` cannot load the corpus.

`createSeedEngine` is left unchanged, so the plan's criterion `rg -n 'func createSeedEngine' -A 5 … shows it delegating` does **not** hold. Two builders now exist. They cannot silently diverge in the way that matters — both load the same `SeedPolicies()` corpus, and the external smoke test fails if the exported path stops working — but this is a real residual worth naming for the verifier.

### 3. [Rule 1 — Bug] S3 asserted the pre-widening posture

**Found during:** the plan-level `task test:int` sweep, after Task 4 was committed.

`test/integration/access/seed_policies_test.go`'s `S3: denies reading a public property on a different-location parent` seeds a `parent_type='character'`, `visibility='public'` row on a character in another location and asserted `EffectDefaultDeny`. `seed:profile-public-read-property` now permits it.

**This is the behaviour change D-10/D-11 specify, not a regression:** *"an off-location character in-game can read public properties it previously could not… `public` means public on the grid as well as the web; the colocation restriction was the anomaly."* S3 is the pre-existing assertion of the old posture.

- S3 now asserts `EffectAllow`, with the D-10/D-11 rationale and the pointer to 02-10's audit at the site.
- **S3b added as the paired control**, so S3 cannot be read as "public is now readable from anywhere, full stop": a public property on a different-location **LOCATION** parent is still denied, because the widening is guarded on `parent_type == "character"`. Demonstrated RED with the guard removed.

**Why the per-task verify did not catch it:** every task's `<verify>` is `task test -- ./internal/access/policy/`, which does not compile `//go:build integration` files. The plan's `<verification>` block does not name `./test/integration/access/` either — only the plan-level whole-suite `task test:int` found it. Worth the phase audit's attention: a plan that changes seed-policy *behaviour* should name the seed-behaviour integration suite in at least one task's verify.

### 4. Files touched beyond the plan's `files_modified`

Each is a corpus-inventory guard that MUST move with any seed addition; leaving them stale would have left `task test` red at every intermediate commit.

| File | Why |
| --- | --- |
| `internal/access/policy/seed_test.go` | `TestSeedPoliciesCount` (49 → 59), `TestSeedPoliciesEffectDistribution` (40 → 49 permit, 9 → 10 forbid), `TestSeedPoliciesExpectedNames` (+10 names), `TestSeedPoliciesForbidPoliciesAreExpected` (+1) |
| `internal/access/policy/bootstrap_test.go` | `TestBootstrapSetsCorrectPolicyEffect` carries a second copy of the expected-forbid inventory |
| `test/integration/access/seed_policies_test.go` | Deviation 3 above |

Two files the plan listed in `files_modified` were **not** modified, both consequences of Deviation 2: `internal/access/policy/seed_smoke_test.go` (left unchanged — the local `viewerProvider`/`playerProvider` doubles the plan asked for would have to be sourced from `abactest`, which is what the cycle forbids; the smoke assertion lives in the external test package instead). The plan's `abactest.go` and the seed-coverage integration test were both created/modified as specified.

## Requirements bookkeeping — EXT-07 and PROFILE-11

`gsd-tools query requirements.mark-complete PROFILE-11 EXT-07` was run per this plan's `requirements:` frontmatter. Both checkboxes in `.planning/REQUIREMENTS.md` were **already** `[x]`, flipped by `02-03` in wave 2 — seven plans in this phase claim these two IDs and the verb has no partial-credit model. `.planning/REQUIREMENTS.md` is a tool-owned parsed artifact, so it was **not** hand-reverted (`.claude/rules/planning-artifacts.md`); reporting the gap is the sanctioned path.

**New detail this run surfaced, not reported by `02-03` or `02-13`:** the verb returned `{"updated": false, "table_unmatched": ["PROFILE-11", "EXT-07"]}`. The checkboxes at `REQUIREMENTS.md:173,241` are `[x]`, but the traceability-table rows at `:369,385` still read **`Pending`** — the two halves of the same artifact disagree, and the verb could not reconcile the table. So an auditor reading the checkboxes sees "done" while an auditor reading the table sees "pending", and **the table is the one that happens to be right**. Neither was hand-edited. Flagging it for the phase audit as a tool-behaviour gap rather than a state to repair locally.

**This plan's genuine share, stated so the audit can size what remains:**

- **EXT-07 — this plan closes its seed half.** `seed:admin-section-access` now exists, player-flavored and type-scoped. The **admin section registry itself is `02-09`'s**, and the endpoint-level denial tests are Phase 4's (D-08). EXT-07 is **not** fully discharged here.
- **PROFILE-11 — this plan closes its `entity_properties` half.** `seed:profile-public-read-property` exists. Its **`characters.description` half is NOT discharged in Phase 2**: D-29 defers `seed:profile-public-read-character` to Phase 4, to land with the projection narrowing that makes it safe. This is a **scope change**, recorded here, in `seed.go` at the site, and in `02-CONTEXT.md` D-29 — not a naming choice. The behavioural proof of the tier-floor conjunction is `02-08`'s and the exposure audit is `02-10`'s.

## D-03 deviation — two tier-floor policies, not three

D-03 as **originally written** mandated three tier-floor policies, one per rung. **Two shipped.**

**Cause** — an empty seeded set plus a grammar constraint, not a scope reduction:
1. §8.6's seeded-default column places every governed row at `anonymous` or `guest`. The `player` rung has **no** seeded member.
2. The DSL's list grammar is `'[' @@ (',' @@)* ']'` (`internal/access/policy/dsl/ast.go`), which requires at least one literal. An empty `in []` does not parse.

So a third policy cannot be written at all without inventing a member, which would be worse than the recorded omission. The **shape** D-03 mandates — one policy per rung, literal name lists, set-membership clearing — is unchanged; only the count follows from the seeded data.

**Re-entry condition:** the moment §8.6 seeds any name at the `player` rung, the third policy becomes both writable and required. `TestAPlayerRungTierFloorPolicyIsRequiredExactlyWhenSpec86SeedsAName` is green while the antecedent is false and turns RED at exactly that moment. The clearing test the absent policy would carry (`principal.viewer.tier in ["player"]`) is transcribed into `seed.go`'s block comment so it does not have to be re-derived.

`02-CONTEXT.md`'s D-03 already carries this amendment as of the cycle-2 fix pass. **Plan `02-11` asserts the count recorded there equals the count actually in `seed.go`** — this entry is the evidence for that check, not a substitute for it. The count in `seed.go` is **2**.

## Artifacts produced — ten seed policies

`seed:profile-tier-floor-anonymous`, `seed:profile-tier-floor-guest`, `seed:profile-reachable`, `seed:viewer-property-public-read`, `seed:viewer-property-private-read`, `seed:viewer-property-admin-read`, `seed:viewer-property-restricted-visible-to`, `seed:viewer-property-restricted-excluded`, `seed:profile-public-read-property`, `seed:admin-section-access`.

**NOT produced, deliberately:** `seed:profile-public-read-character` — the `resource is character` permit — deferred to Phase 4 by D-29.

## Threat mitigations applied

| Threat | Disposition | Where it landed |
| --- | --- | --- |
| T-02-37 (tier-floor family placed in the same evaluation) | mitigate | The `read_profile_attribute` action separates the families; `TestTheTierFloorFamilyOwnsTheReadProfileAttributeActionAlone` pins that exactly the tier floors carry it. The caller-side AND and D-04's regression test are `02-08`'s. |
| T-02-38 (ordinal tier comparison) | mitigate | Explicit set membership, transcribed verbatim; `TestNoTierFloorPolicyUsesAnOrdinalTierComparison` plus the grep gate. The fourth-rung behavioural RED is `02-08`'s. |
| T-02-39 (wildcard over the profile namespace) | mitigate | Whole-string matching only; `TestNoTierFloorPolicyMatchesAProfileNameByAnythingButAWholeString` rejects `like` and `containsAll`/`containsAny` in the family, and the media-name test pins the closed set. |
| T-02-40 (viewer twin keyed on an omitted attribute) | mitigate | `player_id` omitted on the anonymous rung by 02-03's provider; the `abactest` double reproduces the omission and `TestNewSeedEngineOmitsPlayerIDOnTheAnonymousRung` asserts it with a paired positive control. |
| T-02-95 (viewer twin keyed on a MISMATCHED attribute) | mitigate | The transcribed mapping table implemented; `TestNoViewerTwinReferencesACharacterKeyedRowField` (exact-key) and `TestTheIdentityBearingViewerTwinsKeyOnTheDerivedPlayerPeers` (its positive control); and the attribute-reference coverage gate demonstrated RED. |
| T-02-41 (`seed:profile-public-read-property` widening) | mitigate (partial) | D-11's remedy recorded at the site; S3/S3b pin the new posture and the `parent_type` guard. **The gate is 02-10's audit and it has NOT run — the phase must not merge before `02-AUDIT-RESULT.md` exists.** |
| T-02-42 (unconditional `resource is character` read permit) | mitigate | Not seeded (D-29); `TestNoPhase2SeedIntroducesACharacterResourceTypePermit` pins the character-typed set; the deferral comment names the shape and forbids the D-10/D-11 justification, positioned so the grep gate stays valid. |
| T-02-42 (viewer write permit) | mitigate | No twin for `seed:property-owner-write`; `TestSeedPropertyOwnerWriteHasNoViewerTwin` asserts no `seed:viewer-*` policy carries `write` or `delete`, which is stronger than asserting one name's absence. |
| T-02-43 (wrong admin principal type) | mitigate | `principal is player` against `principal.player.roles`, asserted on the compiled target. Non-vacuity is `02-09`'s paired controls. |
| T-02-44 (per-id admin scoping) | mitigate | Resource-TYPE scoping; the test asserts `ResourceExact` is nil AND no `admin_section:` literal appears in the DSL. |

## Known Stubs

None. Every symbol this plan ships has a real body and a test that exercises it.

Two things are *shipped but not yet consumed*, both intended sequencing:

1. The ten seeds have **no behavioural proof in this plan** — that is `02-08`'s (tier floors, the conjunction, §8.6 totality) and `02-09`'s (admin sections), by the plan's own `<verification_integrity>` rule 2. What is proven here is that they compile, load, reference only supplied attributes, and carry the structure the SPEC and the locked decisions require.
2. `internal/testsupport/abactest` has one consumer today (this plan's smoke test). Plans `02-08`, `02-09` and `02-10` are its intended consumers.

## Invariant registry

No registry invariant is pinned here and no `// Verifies:` annotation was written, per `<verification_integrity>` rule 6. `INV-ACCESS-10` and `INV-ACCESS-11` stay `pending` — §13 places their binding in Phase 4, against the read path this policy family serves. That split is deliberate and is not a coverage gap to be closed by annotating a compile-only smoke test. No ad-hoc invariant family was minted.

## Verification

| Gate | Command | Result |
| --- | --- | --- |
| Plan `<verification>` | `task test -- ./internal/access/policy/` | exit 0 |
| Task 4 `<verify>` | `task test -- ./internal/access/policy/ ./internal/testsupport/abactest/` | exit 0 — 466 tests |
| Whole-repo unit | `task test` | exit 0 |
| Whole-repo integration | `task test:int` | exit 0 — 11315 tests, 7 skipped |
| Task 4 `<verify>` | `task test:int -- ./internal/access/setup/` | exit 0 — 29 tests |
| Plan `<verification>` | `task lint` | exit 0 |
| Project rule | `task fmt` then `task fmt:check` | exit 0, formatter edits committed |
| AC (tier-floor names, live code) | `rg -v '^\s*//' seed.go \| rg -o 'seed:profile-tier-floor-[a-z]+' \| sort -u \| wc -l` | `2` |
| AC (player floor, live code) | same pipeline, `-player` | `0` |
| AC (ordinal tier comparison) | `rg -o 'principal.viewer.tier (>=\|>\|<\|<=)' seed.go \| wc -l` | `0` |
| AC (wildcard / prefix) | `rg -o 'profile\.\*\|…\|startsWith\|hasPrefix' seed.go \| wc -l` | `0` |
| AC (gallery 00-09 distinct) | `rg -o 'profile\.image\.gallery\.0[0-9]' seed.go \| sort -u \| wc -l` | `10` |
| AC (gallery index 10) | `rg -o 'profile\.image\.gallery\.10' seed.go \| wc -l` | `0` |
| AC (viewer twins distinct) | `rg -o 'seed:viewer-property-[a-z-]+' seed.go \| sort -u \| wc -l` | `5` |
| AC (viewer write twin) | `rg -o 'seed:viewer-property-owner-write' seed.go \| wc -l` | `0` |
| AC (D-11 in the `resource is character` -B window) | `rg -B 6 'resource is character' seed.go \| rg -o 'D-11' \| wc -l` | `0` |
| AC (new entries location-gated) | `rg -A 12 'seed:(viewer-property\|profile-public-read)[a-z-]*' seed.go \| rg -o 'principal.character.location' \| wc -l` | `0` |
| AC (D-27 cited in the twin block) | `rg -c 'D-27' seed.go` | `4` |
| AC (no literal admin_section id in a DSL) | `rg -o 'DSLText.*admin_section:' seed.go \| wc -l` | `0` |
| AC (abactest uses the exported path) | `rg -o 'NewCompiler\|NewCache\|Reload\|NewEngine' abactest.go \| sort -u` | all four |
| AC (no cross-package unexported access) | `rg -v '^\s*//' abactest.go \| rg -o '\.snapshot\|\.mu\.Lock' \| wc -l` | `0` |
| AC (no policytest) | comment-stripped over the three files | `0` |
| AC (no database) | `task test -- ./internal/testsupport/abactest/` | exit 0 |
| AC (D-29, replaced form) | `TestNoPhase2SeedIntroducesACharacterResourceTypePermit` | pass |

Two criteria are counted **comment-stripped** where the plan wrote them file-wide, because the plan's own required documentation trips them: `abactest.go`'s package doc must explain *why* it does not touch `Cache.snapshot`/`Cache.mu` and *why* it does not reach for `policytest`, and those explanations are the only matches. Code-only counts are `0` for both.

## Next Phase Readiness

Ready, with one gate outstanding.

- **`02-08`** has the tier-floor family, the conjunction's two action tokens, the five twins, and `abactest.NewSeedEngine` + the three doubles to assert them against a real engine with no database. The property double derives the D-27 peers, so its privacy suite exercises the decision rather than restating it.
- **`02-09`** has `seed:admin-section-access` and `abactest`; it owns the registry and the paired-control denial suite over the seven sections.
- **`02-10`** has the widening to audit, and the control corpus it needs (the permits exist, so a run excluding them is expressible). **Its audit is the merge gate for this phase.**
- **`02-11`** owes, on top of 02-03's Amendment F and 02-13's two: the §8.6 amendment recording D-03's two-policy count, and D-05's §8.5.1.1 amendment recording option 2 as rejected and D-01 as settled. It also asserts the D-03 count in `02-CONTEXT.md` equals the count in `seed.go` — which is **2**.
- **`abac-reviewer`** (`/holomush-dev:review-abac`) MUST be routed this diff before the phase merges (D-05), with the Open Question 1 naming deviation and the D-29 deferral.
- **Phase 4** inherits `seed:profile-public-read-character` and the projection narrowing that must land with it, plus D-27's recorded consequence that the viewer path can be narrower than the grid for identity-keyed rows.

## Self-Check: PASSED

All four created files verified present on disk; all five modified files present. All four commits (`b928a5d04`, `1d3a8ad0d`, `cc377d20e`, `8a5dc3408`) resolve via `git cat-file -e`. Working tree clean at the time of writing. All ten seed policy names verified present in `internal/access/policy/seed.go`.

---
*Phase: 02-abac-schema-vocabulary*
*Completed: 2026-08-04*
