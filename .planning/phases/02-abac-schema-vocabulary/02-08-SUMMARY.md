---
phase: 02-abac-schema-vocabulary
plan: 08
subsystem: access-control
tags: [abac, profile-visibility, conjunction, tier-floor, d-04, d-27, red-first, privacy]

requires:
  - phase: 02-abac-schema-vocabulary
    provides: "02-07's two tier-floor policies, seed:profile-reachable, the five viewer twins, the read_profile_attribute action token, and internal/testsupport/abactest; 02-03's viewer prefixes and ViewerSubject; 02-13's derived player-keyed property peers"
provides:
  - "internal/access/profilevis — the caller-side helper that performs §8.5.1's conjunction: two Evaluate calls ANDed in Go, never one evaluation"
  - "profilevis.Evaluator.Reachable — §8.4.2's independent, prior reachability evaluation against profile:<character-id>"
  - "profilevis.Evaluator.AttributeVisible — term A (read_profile_attribute) AND term B (read) against the same property:<id>"
  - "profilevis.Evaluator.VisibleAttributes — reachability-first, abort-on-infra-failure, order-independent published set"
  - "The §8.2.1 fourth-rung gate, observed RED against an ordinal clearing test on the GUEST floor"
  - "D-04's additive-permit regression, observed RED with term B dropped"
  - "§8.6's totality suite, observed RED against a like \"profile.*\" prefix match"
affects: [02-09, 02-10, 02-11, phase-04-character-access-service]

actuals:
  tokens: 13700
  tasks: 3
  commits: 3

tech-stack:
  added: []
  patterns:
    - "A caller-side conjunction over a disjunctive-permit engine, with the reasoning for the two-call shape carried in the package doc comment because the collapse into one call reads as an optimization and its failure mode has no symptom"
    - "Both terms ALWAYS evaluated, never short-circuited on a term-A denial — it makes 'exactly two evaluations' an unconditional property a test can pin, and it stops a term-B infrastructure failure being masked as an ordinary 'withheld' (§8.10)"
    - "Three outcomes from one engine answer — permit, policy denial, infrastructure failure — where the infra outcome arrives in TWO shapes: a non-nil error, AND a DENY decision carrying an infra: policy id with a NIL error (degraded mode, session resolution)"
    - "Choosing the RED discriminator by what the corpus actually seeds, not by what the ladder describes: an assertion phrased against an unseeded rung denies under both implementations and is a gate that cannot fail"
    - "A provider double that OMITS a key, written locally because the shared double always emits it — the difference between an unresolved LHS and an empty-string sentinel is the property under test"
    - "Asserting that a forbid-side fixture SATISFIES the permit before the forbid denies it, so the subtest cannot pass with the forbid derivation deleted"

key-files:
  created:
    - internal/access/profilevis/profilevis.go
    - internal/access/profilevis/profilevis_test.go
    - internal/access/profilevis/tierfloor_test.go
  modified: []

key-decisions:
  - "AttributeVisible does NOT short-circuit on a term-A denial. Both terms are always evaluated. Two reasons: it makes the 'exactly two evaluations' property unconditional rather than holding only on the permitting path, and §8.10 forbids masking — short-circuiting would report a term-B infrastructure failure as an ordinary 'withheld'."
  - "§8.7's not-found-equivalent is signalled as its own Go outcome (ErrProfileUnreachable, code PROFILE_VISIBILITY_UNREACHABLE), deliberately distinct from ErrEvaluationFailed. A caller that cannot tell them apart renders an outage as a missing character. The wire-level indistinguishability §8.7 mandates binds in Phase 4."
  - "The two engine calls in AttributeVisible go through one unexported `evaluate` helper rather than two inline Evaluate call sites. See Deviations — the plan's acceptance criterion counts call sites."
  - "The `policytest` and `createSeedEngine` grep gates are satisfied LITERALLY (both count 0), which cost the package comments the right to spell those identifiers. See Deviations."

patterns-established:
  - "Recording a mutation-based RED demonstration with the mutation, the exit code, and the OBSERVED WRONG VERDICT — not 'the test would have failed'. Three such demonstrations in this plan, each reverted and re-verified byte-identical to HEAD before the commit."
  - "Pinning a consumer-defined interface to the concrete production type in the TEST file (`var _ PolicyEvaluator = (*policy.Engine)(nil)`), so a signature drift breaks the doubles at compile time while production stays decoupled from the concrete engine."

requirements-completed: []

coverage:
  - id: D1
    description: "A profile attribute publishes only when BOTH terms permit; the conjunction is two separate Evaluate calls ANDed by the caller, never one evaluation containing two permits"
    requirement: PROFILE-11
    verification:
      - kind: unit
        ref: "internal/access/profilevis/profilevis_test.go#TestAttributeVisibleIssuesExactlyTwoEvaluationsSeparatedByTheActionToken / TestAttributeVisiblePublishesOnlyWhenBothTermsPermit (all four combinations)"
        status: pass
    human_judgment: false
  - id: D2
    description: "A synthetic fourth tier token sorting lexicographically above guest clears NEITHER shipped floor, demonstrated RED against an ordinal-comparison implementation"
    requirement: PROFILE-11
    verification:
      - kind: unit
        ref: "internal/access/profilevis/tierfloor_test.go#TestASyntheticFourthRungClearsNeitherShippedTierFloor (demonstrated RED — spectator cleared BOTH floors under the ordinal form)"
        status: pass
    human_judgment: false
  - id: D3
    description: "D-04: a profile.* row at visibility='private' whose name's floor the viewer clears is ABSENT, demonstrated RED with term B removed"
    requirement: PROFILE-11
    verification:
      - kind: unit
        ref: "internal/access/profilevis/profilevis_test.go#TestARowTheViewerClearsTheTierFloorForIsStillWithheldWhenTheRowItselfDenies (demonstrated RED — the private row PUBLISHED)"
        status: pass
    human_judgment: false
  - id: D4
    description: "§8.6 totality: a profile.* name appearing in no §8.6 row is denied at the highest rung and public visibility, demonstrated RED against a prefix match"
    requirement: PROFILE-11
    verification:
      - kind: unit
        ref: "internal/access/profilevis/profilevis_test.go#TestAProfileNameInNoSpec86RowIsDeniedRatherThanDefaulted (demonstrated RED — gallery.10 PUBLISHED under a like \"profile.*\" match)"
        status: pass
    human_judgment: false
  - id: D5
    description: "Reachability is evaluated FIRST as its own independent Evaluate call, and a DENY short-circuits before any per-field evaluation"
    requirement: PROFILE-11
    verification:
      - kind: unit
        ref: "internal/access/profilevis/profilevis_test.go#TestReachableIssuesExactlyOneEvaluationAgainstTheProfileResource / TestVisibleAttributesPerformsNoPerAttributeEvaluationWhenReachabilityDenies"
        status: pass
    human_judgment: false
  - id: D6
    description: "An infrastructure failure in either term resolves DENY and aborts the whole call, never masked as 'this viewer sees nothing'"
    requirement: PROFILE-11
    verification:
      - kind: unit
        ref: "internal/access/profilevis/profilevis_test.go#TestAttributeVisiblePropagatesAnEvaluationErrorRatherThanReportingWithheld (both shapes: non-nil error, and infra: decision with nil error) / TestVisibleAttributesAbortsTheWholeCallWhenAnyEvaluationFails"
        status: pass
    human_judgment: false
  - id: D7
    description: "Submission order does not change any attribute's published-or-withheld verdict"
    requirement: PROFILE-11
    verification:
      - kind: unit
        ref: "internal/access/profilevis/profilevis_test.go#TestVisibleAttributesReturnsTheSamePermittedSetWhenTheInputOrderIsReversed"
        status: pass
    human_judgment: false
  - id: D8
    description: "§8.8's hard floor holds in the shape D-02 protects: an anonymous viewer receives a public profile.pronouns"
    requirement: PROFILE-11
    verification:
      - kind: unit
        ref: "internal/access/profilevis/profilevis_test.go#TestAnAnonymousViewerReachingAProfileReceivesItsPublicPronouns"
        status: pass
    human_judgment: false
  - id: D9
    description: "D-27's permit-side ALL direction and forbid-side ANY direction are both exercised, with the forbid asserted on the DISCRIMINATING fixture"
    requirement: PROFILE-11
    verification:
      - kind: unit
        ref: "internal/access/profilevis/profilevis_test.go#TestARestrictedRowIsPermittedOnlyWhenEveryCharacterOfTheViewingPlayerIsNamed / TestTheExcludedFromForbidIsWhatDeniesARowWhoseVisibleToPermitDoesFire / TestTheDiscriminatingForbidFixtureSatisfiesThePermitBeforeTheForbidDeniesIt"
        status: pass
    human_judgment: false
  - id: D10
    description: "The suite asserts, before any floor assertion, that the corpus contains exactly the two seed:profile-tier-floor- entries"
    requirement: PROFILE-11
    verification:
      - kind: unit
        ref: "internal/access/profilevis/tierfloor_test.go#TestTheSeedCorpusCarriesExactlyTwoTierFloorPoliciesAtTheAnonymousAndGuestRungs"
        status: pass
    human_judgment: false

duration: 20min
completed: 2026-08-04
status: complete
---

# Phase 02 Plan 08: The §8.5.1 Conjunction Evaluator and Its Three RED-First Gates Summary

**`internal/access/profilevis` performs §8.5.1's conjunction as two Evaluate calls ANDed in Go, and ships the three gates that make the shape mechanically enforceable — the fourth-rung gate observed clearing both floors under an ordinal test, D-04's private row observed publishing with term B dropped, and `profile.image.gallery.10` observed publishing under a prefix match.**

## Performance

- **Duration:** 20 min
- **Tasks:** 3 of 3
- **Files modified:** 3 (all created)
- **Tests:** 47 in the package; 10909 repo-wide

## Task Commits

| Task | Name | Commit | Key files |
| --- | --- | --- | --- |
| 1 | The conjunction evaluator | `7109e214c` | `internal/access/profilevis/profilevis.go`, `profilevis_test.go` |
| 2 | The fourth-rung gate, RED against an ordinal implementation | `2e2bc4bf0` | `internal/access/profilevis/tierfloor_test.go` |
| 3 | D-04's additive-permit regression and §8.6's totality rule | `ec812fe7c` | `internal/access/profilevis/profilevis_test.go` |

Task 1 was written test-first against a stub implementation and observed RED as **19 assertion failures across 23 tests** — genuine behavioural failures rather than a compile error, because a brand-new package's "undefined symbol" RED proves nothing about the assertions. Tasks 2 and 3 are test-only over already-shipped behaviour, so their RED is the mutation-based demonstration below, which is the form `<verification_integrity>` rule 4 actually requires of them.

## `<verification_integrity>` rule 4 — the three gates, demonstrated RED

**All three were OBSERVED failing against a deliberately reverted state.** Each mutation was reverted immediately, and `git diff --exit-code` confirmed the mutated file byte-identical to HEAD before the corresponding commit.

### Gate 1 — the fourth rung, against an ordinal clearing test

**Mutation** (`internal/access/policy/seed.go`, both tier-floor policies):

```
principal.viewer.tier in ["anonymous", "guest", "player"]  →  principal.viewer.tier >= "anonymous"
principal.viewer.tier in ["guest", "player"]               →  principal.viewer.tier >= "guest"
```

**Command:** `task test -- -run TierFloor ./internal/access/profilevis/` → **exit 1** (`DONE 7 tests, 1 failure`)

```
=== FAIL: internal/access/profilevis TestASyntheticFourthRungClearsNeitherShippedTierFloor (0.19s)
    tierfloor_test.go:176:
        	Error:      	Should be false
        	Messages:   	a newly appended rung MUST NOT clear the guest floor: "spectator" sorts above
        	            	"guest" in Go byte order, so an ordinal clearing test would hand it clearance
        	            	on the day the token is added
    tierfloor_test.go:184:
        	Error:      	Should be false
        	Messages:   	a newly appended rung MUST NOT clear the anonymous floor either
```

**What was observed:** `spectator` cleared **both** shipped floors. The two paired positive controls in the same test — `guest` clears the guest floor, `anonymous` clears the anonymous floor — stayed green, so the failure is diagnostic of the clearing *test's shape* rather than of a broken family.

**The discriminator is the GUEST floor, not a player floor** (cross-AI review C2-11, confirmed). Plan `02-07` seeds no `player`-rung policy, so an assertion phrased against one asks the engine about a policy that does not exist and reports DENY under *both* implementations — a gate that cannot fail, wearing the clothes of the sharpest gate in the phase. `"spectator" >= "guest"` is TRUE in Go byte order, and a `guest`-rung policy actually exists, so it discriminates cleanly. The same assertion is made at the `anonymous` rung, so the property proven is "a newly appended token clears **nothing**", not "it fails to clear one particular rung".

### Gate 2 — D-04's additive permit, with term B dropped

**Mutation** (`internal/access/profilevis/profilevis.go`, `AttributeVisible`) — the exact repair §8.5.1.1 warns about:

```go
return clearsFloor && rowPermits, nil   →   _ = rowPermits
                                            return clearsFloor, nil
```

**Command:** `task test -- -run 'TierFloorFor|Spec86|Restricted|ExcludedFrom|ARowThe' ./internal/access/profilevis/` → **exit 201** (`DONE 13 tests, 7 failures`)

```
=== FAIL: …TestARowTheViewerClearsTheTierFloorForIsStillWithheldWhenTheRowItselfDenies/
          a_private_row_is_absent_because_the_viewer_is_not_the_owning_player
        	expected: false
        	actual  : true
        	Messages: the row is owned by a character of a DIFFERENT player, so the derived
        	          owner peer does not match

=== FAIL: …/an_admin_row_is_absent_because_the_viewer_holds_no_admin_role
        	expected: false
        	actual  : true

=== FAIL: …TestTheExcludedFromForbidIsWhatDeniesARowWhoseVisibleToPermitDoesFire
        	Error:    Should be false
        	Messages: the forbid twin denies a row whose permit twin fires — D-27's ANY direction
```

**What was observed:** the `visibility='private'` row **published**. So did the `visibility='admin'` row, the `excluded_from` forbid case, and both D-27 denials — five separate exposures from one two-line change, every one of them a row the viewer had no business seeing. The `public` paired control stayed green throughout, which is precisely why this failure would be invisible in production: dropping term B relieves no visible symptom on the public path and opens the private one silently.

### Gate 3 — §8.6's totality rule, against a prefix match

**Mutation** (`internal/access/policy/seed.go`, `seed:profile-tier-floor-guest`): the 22-name literal list replaced by `resource.property.name like "profile.*"`.

**Command:** `task test -- -run 'Spec86' ./internal/access/profilevis/` → **exit 201** (`DONE 4 tests, 3 failures`)

```
=== FAIL: …TestAProfileNameInNoSpec86RowIsDeniedRatherThanDefaulted/
          the_twelfth_gallery_name_is_denied_—_the_eleven_media_names_are_a_closed_set
        	expected: false
        	actual  : true
        	Messages: §7.3 fixes eleven exact names; index 10 is not a member and no glob covers it

=== FAIL: …/an_arbitrary_unenumerated_profile_name_is_denied
        	expected: false
        	actual  : true
        	Messages: §7.1 makes a new profile field an INSERT, so the namespace is open — a residual
        	          permit would publish a name nobody has considered
```

**What was observed:** `profile.image.gallery.10` **published**, and so did `profile.somethingnobodyconsidered`. The `.09` paired control stayed green, which is what makes the failure about *enumeration* rather than about the media names generally.

## Accomplishments

- **The conjunction is two calls, and the reason lives where a future reader will hit it.** The package doc comment names `combineDecisions`, the disjunctive-permit property, and the exact exposure a single evaluation creates. Someone looking at two `Evaluate` calls against one resource will be tempted to unify them; the comment is there because the call-count test alone tells them *that* they broke something, not *why* the shape existed.

- **The action token is consumed, not decided.** Term A is `read_profile_attribute`, term B is `read`, both against `property:<id>`. `02-07` fixed that separator; this plan asserts it — including `assert.NotEqual` on the two actions, because two calls that both matched both families would silently reduce to the additive shape and the call-count assertion alone would not catch it.

- **No short-circuit on a term-A denial, deliberately.** The obvious optimization — skip term B when the floor already denied — would make "exactly two evaluations" hold only on the permitting path, and, worse, would report a term-B *infrastructure failure* as an ordinary "withheld". §8.10 forbids exactly that masking. Both terms always run; their errors are `errors.Join`ed.

- **The infra outcome is caught in both of its shapes.** The obvious one is a non-nil error from `Evaluate`. The subtle one is a DENY decision carrying an `infra:`-prefixed policy id **with a nil error** — which is what the engine returns for degraded mode (`infra:degraded-mode`) and for session resolution failures. Treating that second shape as an ordinary denial would render a profile as legitimately sparse during an outage. `world.Service.checkAccess` distinguishes them the same way; this follows that precedent rather than inventing one.

- **Reachability cannot be derived from field results, and the test proves the derivation is absent rather than merely unlikely.** `TestVisibleAttributesPerformsNoPerAttributeEvaluationWhenReachabilityDenies` asserts **zero** per-attribute calls, with a paired control asserting two when reachability permits. Without that ordering, §8.6's seeded defaults pin reachability permanently at `anonymous` (`profile.pronouns` sits there, so something always clears), §8.7's not-found-equivalent can never fire, and **INV-PRIVACY-9 would bind in Phase 4 to a gate that cannot deny** — a false-green arrived at by construction.

- **The fixtures are character-keyed and the peers are derived, as the review demanded.** Every row fixture writes **character** ids into `OwnerCharacterID`, `VisibleTo` and `ExcludedFrom`. `abactest.PropertyFixture` has no field for `owner_player_id` / `visible_to_players` / `excluded_from_players` — by construction — so the double derives them from a fixture character→player map. **No player id was written into a character-keyed field at any point.** `rg -n 'owner_player_id|visible_to_players|excluded_from_players' profilevis_test.go` shows those keys in exactly two places: the comment explaining the ALL/ANY split, and `TestTheDiscriminatingForbidFixtureSatisfiesThePermitBeforeTheForbidDeniesIt`'s assertions about derived output. Never in fixture construction.

- **The forbid-side fixture is the discriminating one, not the natural-looking one (B-15).** For viewing player P holding {C1, C2}, the obvious fixture — `visible_to = [C2]`, `excluded_from = [C1]` — is a **test that cannot fail**: D-27's ALL direction is unsatisfied, so `visible_to_players` is empty, so the permit twin never matches, so default-deny fires whether or not the forbid derivation exists at all. The shipped fixture is `visible_to = [C1, C2]` **and** `excluded_from = [C1]`, so the permit genuinely fires and deny-overrides makes the forbid the only thing standing between the viewer and the row. A companion test reads `abactest.DerivePlayerPeers`' output directly and asserts both that the discriminating fixture populates `visible_to_players`, and that the near-miss fixture does **not** — so the reason the near-miss is wrong is itself pinned, and a later simplification that reaches for it turns that test RED.

- **An absent `tier` is tested as absent, not as empty.** `abactest.ViewerProvider` always writes the key, so an empty `Tier` yields `tier: ""` — an empty-string *sentinel*, whose behaviour is `compareStrings` on `""`, not the unresolved-LHS behaviour §8.2.1 cares about. A four-line local provider omits the key entirely, with its schema pinned to `abactest.ViewerSchemaKeys` so it stays a real provider shape.

## Deviations from Plan

### 1. [Rule 3 — Blocking] The two grep gates cost the package comments the right to spell two identifiers

**Found during:** Task 2, checking the acceptance criteria before committing.

Two criteria are literal, file-wide counts with no comment-stripping:

```
[ "$(rg -o 'policytest'      internal/access/profilevis/ | wc -l)" -eq 0 ]
[ "$(rg -o 'createSeedEngine' internal/access/profilevis/ | wc -l)" -eq 0 ]
```

The plan's own `<action>` text simultaneously instructs the file to explain *why* neither is used — "Do NOT reach for `createSeedEngine` … Do NOT use `policytest.GrantEngine`, `AllowAllEngine` or `DenyAllEngine`". Writing that explanation with the identifiers in it trips both gates. `02-07` hit the same collision and resolved it by counting comment-stripped and recording the deviation.

**Resolved the other way here: the gates are satisfied literally (both count `0`), and the explanations were rephrased to locate the symbols without spelling them** — "the canonical decision engine doubles that live beside the policy engine — the grant, allow-all and deny-all fakes" and "the equivalent unexported engine builder declared in `internal/access/policy/seed_smoke_test.go`". Nothing was lost: both are unambiguously locatable, and a grep gate the executor knowingly breaks is a broken gate.

Worth naming for the phase audit as a **plan-authoring pattern**, not a one-off: a criterion phrased as a file-wide `rg` count over a package that is *also* required to document why it avoids the thing being counted is self-contradictory as written. Either the count is comment-stripped or the documentation names the symbol descriptively; both plans in this phase have now hit it.

### 2. [Rule 3 — Blocking] `AttributeVisible`'s two evaluations go through one helper, so the literal call-site count is 1

**Found during:** Task 1.

The plan's criterion reads: *"`AttributeVisible`'s body contains exactly two `Evaluate` call sites."* The shipped body contains **two `e.evaluate(...)` call sites** (`profilevis.go:169-170`), which funnel into a single `e.Engine.Evaluate(ctx, req)` inside the unexported `evaluate` helper (`profilevis.go:246`). A literal `rg 'Engine\.Evaluate\('` over the package returns **1**, not 2.

**The property the criterion protects is intact and is asserted behaviourally rather than syntactically.** `TestAttributeVisibleIssuesExactlyTwoEvaluationsSeparatedByTheActionToken` asserts the recording double received exactly **two** requests, with distinct actions, against the same resource — which is strictly stronger than a grep over the body, because it would also catch two call sites that happened to issue the same request.

**The extraction is load-bearing, not cosmetic.** The three-outcome collapse — permit / policy denial / infrastructure failure, where the infra outcome has two distinct shapes — must be identical for both terms. Inlining it twice is where the two terms drift: the easy mistake is to handle `decision.IsInfraFailure()` on one term and forget it on the other, which reintroduces §8.10's masking on exactly one half of the conjunction, silently.

The plan's companion criterion is satisfied as written: `rg -n 'Evaluate\('` over the package shows every call taking `(ctx, req)` with `req` built by `types.NewAccessRequest`, and **no positional three-argument form appears anywhere** in the package or its tests.

### 3. The `<human-check>` verification items are discharged in this SUMMARY

Both Task 2's and Task 3's `<verify>` blocks carry a `<human-check>` asking that the recorded failures be pasted here. They are, in full, above — with the mutation, the command, the exit code, and the observed wrong verdict for each of the three gates. Task 2's `<human-check>` text says "showing `spectator` clearing the `player` floor"; that phrasing predates the same plan's own C2-11 correction, which replaced the player floor with the guest floor precisely because no `player`-rung policy is seeded. **The observation recorded is `spectator` clearing the `guest` and `anonymous` floors**, which is what the corrected gate asserts and what the `<action>` text mandates.

## Requirements bookkeeping — PROFILE-11

This plan's frontmatter claims `PROFILE-11`. `gsd-tools query requirements.mark-complete PROFILE-11` was run per the workflow. As `02-07` and `02-03` both recorded, the checkbox at `REQUIREMENTS.md:173` was **already** `[x]` (flipped by `02-03` in wave 2), and the verb has no partial-credit model — seven plans in this phase claim this ID.

`.planning/REQUIREMENTS.md` is a tool-owned parsed artifact, so nothing was hand-edited (`.claude/rules/planning-artifacts.md`). The traceability-table row discrepancy `02-07` reported (checkbox `[x]` while the table row still reads `Pending`) is unchanged by this run and remains flagged for the phase audit as a tool-behaviour gap.

**This plan's genuine share of PROFILE-11:** the *behavioural proof* of the tier-floor conjunction over `entity_properties` rows. The seeds themselves are `02-07`'s; the `characters.description` half is deferred to Phase 4 by D-29; the exposure audit is `02-10`'s and remains this phase's merge gate.

## Threat mitigations applied

| Threat | Disposition | Where it landed |
| --- | --- | --- |
| T-02-45 (`AttributeVisible` collapsing to one evaluation) | mitigate | Two `e.evaluate` calls ANDed in Go; the recording double asserts exactly two requests with distinct actions against one resource; the package doc comment carries the `combineDecisions` reasoning at the site a future "optimizer" will read. |
| T-02-46 (term B dropped as a "fix" for empty profiles) | mitigate | D-04's regression, **observed RED with term B dropped** — the `private` row published, along with four other exposures. |
| T-02-47 (ordinal tier comparison) | mitigate | The `spectator` gate, **observed RED against the ordinal form on the `guest` floor**, asserted at every rung the corpus seeds, with paired positive controls. |
| T-02-48 (wildcard over unenumerated names) | mitigate | The `profile.image.gallery.10` gate plus an arbitrary unenumerated name, **observed RED against a `like "profile.*"` match**, paired with `.09`. |
| T-02-49 (reachability derived from field results) | mitigate | `Reachable` is a prior independent `Evaluate`; the zero-per-attribute-calls assertion with a paired control proves the DENY short-circuits before any field is touched. |
| T-02-50 (infra failure read as empty) | mitigate | Both infra shapes propagate and abort; `VisibleAttributes` returns a nil map rather than a partially-populated one, following `ListPropertiesByParent`'s third branch. Asserted directly, including the nil-error `infra:` decision shape. |
| T-02-51 (§8.8 hard floor regressed to option 2) | mitigate | The explicit positive: an anonymous viewer receives a public `profile.pronouns`, paired with a guest-floor negative on the same rung. |
| T-02-96 (fixture reshaped to match a faulty policy) | mitigate | Character-keyed fixtures with provider-derived peers throughout; the same-player / different-player / one-of-two triple only passes if the derivation is real; the derived keys appear only in assertions about derived output, never in fixture construction. |

## Known Stubs

None. Every symbol this plan ships has a real body and a test that exercises it.

One thing is **shipped but not yet consumed**, and that is deliberate per the plan's `<objective>`: `profilevis` has no production caller in Phase 2, which ships no UI and no RPCs. Phase 4's `CharacterAccessService` is its consumer. Shipping the helper here is what gives D-04's mandated regression test something to test and makes §8.5.1.1's prohibition enforceable rather than advisory — the alternative, deferring to Phase 4, leaves the prohibition enforceable only by review.

## Invariant registry

No registry invariant is pinned here and no `// Verifies:` annotation was written, per `<verification_integrity>` rule 6. `INV-ACCESS-10`, `INV-ACCESS-11` and `INV-PRIVACY-9` stay `pending` — §13 places their binding in Phase 4, against the read path and its marshaled response. Annotating this helper's tests would bind them to a surface that is not the one the invariants describe, which is exactly the false-green the binding ratchet exists to catch. What this plan ships is INV-PRIVACY-9's **precondition**: a reachability gate that can actually deny. No ad-hoc invariant family was minted.

## Verification

| Gate | Command | Result |
| --- | --- | --- |
| Task 1 `<verify>` | `task test -- ./internal/access/profilevis/` | exit 0 |
| Task 2 `<verify>` | `task test -- -run TierFloor ./internal/access/profilevis/` | exit 0 — 7 tests |
| Task 3 `<verify>` | `task test -- ./internal/access/profilevis/` | exit 0 — 47 tests |
| Plan `<verification>` | `task test` (whole repo) | exit 0 — 10909 tests, 4 skipped |
| Plan `<verification>` | `task lint` | exit 0 |
| Project rule | `task fmt` then `task fmt:check` | exit 0; formatter edits committed with their tasks |
| AC (no canned-decision engine) | `rg -o 'policytest' internal/access/profilevis/ \| wc -l` | `0` |
| AC (no package-private builder) | `rg -o 'createSeedEngine' internal/access/profilevis/ \| wc -l` | `0` |
| AC (no player-rung assertion, comments stripped) | `rg -v '^\s*//' tierfloor_test.go \| rg -o 'seed:profile-tier-floor-player' \| wc -l` | `0` |
| AC (no positional Evaluate) | `rg -n 'Evaluate\('` over the package | every call is `(ctx, req)`; `req` from `types.NewAccessRequest` in both files |
| AC (derived peers never in fixtures) | `rg -n 'owner_player_id\|visible_to_players\|excluded_from_players' profilevis_test.go` | 2 comment lines + 6 assertion lines on derived output; **0 in fixture construction** |
| Restoration after gate 1 | `git diff --exit-code internal/access/policy/seed.go` | exit 0 (byte-identical to HEAD) |
| Restoration after gates 2 & 3 | `git diff --exit-code seed.go profilevis.go` | exit 0 (byte-identical to HEAD) |

`task test:int` was **not** run for this plan and is not required by it: `profilevis` and `abactest` are both untagged-lane packages needing no database, and this plan touches no `//go:build integration` file. Worth noting against `02-07`'s Deviation 3 — that finding was about a plan that changed seed-policy *behaviour* without naming the seed-behaviour integration suite. This plan changes no seed policy; all three mutations were reverted byte-identical before their commits, and the whole-repo `task test` above confirms nothing else regressed.

## Next Phase Readiness

Ready.

- **`02-09`** (admin section registry) has `abactest.NewSeedEngine` and `seed:admin-section-access`, and now a worked precedent for the paired-positive-control shape over a real engine.
- **`02-10`** (the exposure audit) is unaffected by this plan and **remains this phase's merge gate**: the phase MUST NOT merge before `.planning/phases/02-abac-schema-vocabulary/02-AUDIT-RESULT.md` exists and is non-empty.
- **`02-11`** owes D-05's §8.5.1.1 amendment recording option 2 as rejected and D-01 as settled. This plan is where the §8.5.1.1 finding's *resolution* is proven, so the amendment can now cite `internal/access/profilevis` and the D-04 regression as the mechanical guard.
- **`abac-reviewer`** (`/holomush-dev:review-abac`) MUST review this diff **together with `02-07`'s** before the phase merges — D-05 routes the §8.5.1.1 finding to it, and the resolution spans both plans: `02-07` gave term B a shape that can match, this plan installs the gate that goes RED if anyone drops it.
- **Phase 4** inherits `profilevis.Evaluator` as `CharacterAccessService`'s consumption surface. Two things it must carry forward: §8.7's not-found-equivalent is signalled here as `ErrProfileUnreachable` and must become a wire shape indistinguishable from a nonexistent character; and INV-ACCESS-11 / INV-PRIVACY-9 bind against that projection, not against this helper.

## Self-Check: PASSED

All three created files verified present on disk. All three commits (`7109e214c`, `2e2bc4bf0`, `ec812fe7c`) resolve via `git cat-file -e`. `internal/access/policy/seed.go` and `internal/access/profilevis/profilevis.go` both verified byte-identical to HEAD after every mutation-based RED demonstration. Working tree clean at the time of writing.

---
*Phase: 02-abac-schema-vocabulary*
*Completed: 2026-08-04*
