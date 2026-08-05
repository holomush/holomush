---
phase: 02-abac-schema-vocabulary
plan: 13
subsystem: access-control
tags: [abac, attribute-provider, derived-peers, viewer-tier, role-lookup, composition-root, d-27]

requires:
  - phase: 02-abac-schema-vocabulary
    provides: "02-03's attribute.PlayerRoleLookup type and the unregistered ViewerTierProvider, both consumed here"
provides:
  - "(*store.PostgresRoleStore).PlayerRoles — deduplicated, ordered per-player role union behind a func-field seam"
  - "attribute.WithPlayerRoleLookup — PlayerAttributeProvider option reusing 02-03's shared PlayerRoleLookup type"
  - "player.roles / player.has_roles attribute keys"
  - "attribute.CharacterOwnerResolver — consumer-side interface (ResolveOwnerScopes)"
  - "postgres.CharacterOwnerResolver / NewCharacterOwnerResolver / ResolveOwnerScopes"
  - "property.owner_player_id, .has_owner_player_id, .visible_to_players, .has_visible_to_players, .excluded_from_players, .has_excluded_from_players"
  - "ABACConfig.PlayerRoleLookup and ABACConfig.CharacterOwnerResolver, both POPULATED at the production composition root"
  - "ViewerTierProvider registered in BuildABACStack — principal.viewer.* now resolves in production"
affects: [02-07, 02-08, 02-09, 02-11, phase-04-character-access-service]

actuals:
  tokens: 61000
  tasks: 4
  commits: 4

tech-stack:
  added: []
  patterns:
    - "Derived player-keyed peers of character-keyed row fields, resolved server-side in the provider because the DSL cannot intersect two attribute lists"
    - "Per-peer derivation direction chosen so no peer can widen its own policy's effect: ALL on the permit side, ANY on the forbid side (D-27)"
    - "Func-field seam on a config struct to reach a concrete store method without widening the store's interface (the shipped PlayerKindLookup shape)"
    - "Emitted-vs-declared schema coherence gate by symmetric difference over sets derived from the code on both sides"

key-files:
  created:
    - internal/world/postgres/character_owner_resolver.go
    - internal/world/postgres/character_owner_resolver_test.go
    - internal/store/role_store_player_roles_test.go
    - internal/access/setup/setup_viewer_registration_test.go
    - internal/access/setup/setup_viewer_registration_integ_test.go
  modified:
    - internal/store/role_store.go
    - internal/store/role_store_integration_test.go
    - internal/access/policy/attribute/player.go
    - internal/access/policy/attribute/player_test.go
    - internal/access/policy/attribute/property.go
    - internal/access/policy/attribute/property_test.go
    - internal/access/setup/setup.go
    - internal/access/setup/subsystem.go
    - internal/access/setup/seed_coverage_test.go
    - test/integration/access/access_suite_test.go

key-decisions:
  - "Task 0's checkpoint:decision resolved to `no-widening-direction` — the option the planner front-loaded, and the direction 02-CONTEXT.md D-27 already records as the LOCKED Phase-2 default. No artifact amendment was owed; the plan's resume-signal only required one had `player-union` been selected."
  - "All three derived peers resolve from ONE ResolveOwnerScopes call carrying the union of every character id the row references, rather than one call per field. The scope map is keyed by player with each player's COMPLETE character set, so each field is evaluated independently against it; a combined call is behaviourally identical and strictly cheaper. Pinned by TestPropertyProviderResolvesEveryDerivedPeerFromOneResolverCall."
  - "PlayerRoles validates the player id as a ULID before touching the pool. PlayerHasRole does not, but the plan's <behavior> requires an error for malformed input, and the validation being pool-free is what lets the malformed-id path be asserted in the UNTAGGED lane where the RoleStore interface guard also lives."
  - "The RoleStore interface method-set guard is a reflection test in the untagged lane, not a grep. The plan's own acceptance criterion says the grep only sees a same-line coincidence and names the test as the authoritative form."
  - "playersWhoseEveryCharacterIsIn skips a player with an EMPTY scope set. The resolver never returns one, but a vacuous `every character is a member` would be a permit, and that is the wrong way for this branch to fail."
  - "Derived peer lists are emitted as []any, matching the shape visible_to/excluded_from already use in the same file. evalInExpr accepts both []any and []string; []any is the shape already proven in the exact operator position the 02-07 twins will use."

patterns-established:
  - "A one-fixture assertion that separates two candidate semantics: one player, two characters, one of them in BOTH visible_to and excluded_from. Absent from the permit peer, present in the forbid peer. Under the rejected rule the player appears in both, so the single row is the mechanical guard on the decision."
  - "Demonstrating a gate RED by mutating the implementation to the rejected shape and recording the non-zero exit, then restoring — not by reasoning that it would fail."

requirements-completed: [PROFILE-11, EXT-07]

coverage:
  - id: D1
    description: "PostgresRoleStore.PlayerRoles returns the deduplicated, ordered union of roles held by any character of the player; empty slice (not an error) for no roles and for an unknown player; error for a malformed player id"
    requirement: PROFILE-11
    verification:
      - kind: integration
        ref: "internal/store/role_store_integration_test.go#RoleStore/PlayerRoles (four specs: dedup union, no roles, unknown player, no cross-player leak)"
        status: pass
      - kind: unit
        ref: "internal/store/role_store_player_roles_test.go#TestPostgresRoleStorePlayerRolesRejectsAMalformedPlayerID"
        status: pass
    human_judgment: false
  - id: D2
    description: "The player-scoped lookup reaches ABACConfig as a func field, not as a new method on store.RoleStore — no implementor or fake of the interface changes"
    requirement: PROFILE-11
    verification:
      - kind: unit
        ref: "internal/store/role_store_player_roles_test.go#TestRoleStoreInterfaceMethodSetIsUnchangedByPlayerRoles + internal/access/setup/setup_viewer_registration_test.go#TestABACConfigCarriesTheTwoPlan0213Seams"
        status: pass
    human_judgment: false
  - id: D3
    description: "PlayerAttributeProvider emits roles/has_roles, omitting the value key on both unresolved paths with the witness on every path; the shared PlayerRoleLookup type is used, not a second declaration"
    requirement: PROFILE-11
    verification:
      - kind: unit
        ref: "internal/access/policy/attribute/player_test.go#TestPlayerProviderResolvesRolesPerPlayerWhenALookupIsConfigured / TestPlayerProviderOmitsRolesWhenLookupAbsentOrFails / TestPlayerAndViewerRoleLookupShareOneType / TestPlayerProviderSchema"
        status: pass
    human_judgment: false
  - id: D4
    description: "CharacterOwnerResolver returns each owning player's COMPLETE character set, skips unknown ids and NULL player_id rows, and issues no query for an empty input"
    requirement: PROFILE-11
    verification:
      - kind: integration
        ref: "internal/world/postgres/character_owner_resolver_test.go#TestCharacterOwnerResolverReturnsEachOwningPlayersCompleteCharacterSet / …SkipsUnknownCharacterIDs / …SkipsCharactersWithANullPlayerID / …IssuesNoQueryForAnEmptyInput / …ExcludesAnOrphanCharacterFromAnOwningPlayersScope"
        status: pass
    human_judgment: false
  - id: D5
    description: "Each derived peer is computed in the direction that cannot widen its policy's effect (D-27): ALL on the permit side, ANY on the forbid side"
    requirement: PROFILE-11
    verification:
      - kind: unit
        ref: "internal/access/policy/attribute/property_test.go#TestPropertyProviderDerivesTheTwoDirectionsOppositelyOnOneRow / TestPropertyProviderOwnerPlayerIDFollowsThePermitSideAllDirection / TestPropertyProviderVisibleToPlayersRequiresEveryCharacterOfThePlayer"
        status: pass
    human_judgment: false
  - id: D6
    description: "Every unresolved path omits the derived value key rather than emptying it, with the witness on every path; a resolver error omits all three together, never a partial set"
    requirement: PROFILE-11
    verification:
      - kind: unit
        ref: "internal/access/policy/attribute/property_test.go#TestPropertyProviderOmitsDerivedPeersOnEveryUnresolvedPath / TestPropertyProviderOmitsAllThreeDerivedPeersTogetherOnResolverError"
        status: pass
    human_judgment: false
  - id: D7
    description: "Derived peers come from the ROW's own fields, never from the requesting subject; the character-keyed originals are byte-identical so the shipped character-flavored seeds still resolve"
    requirement: PROFILE-11
    verification:
      - kind: unit
        ref: "internal/access/policy/attribute/property_test.go#TestPropertyProviderDerivedPeersComeFromTheRowNeverFromTheCaller / TestPropertyProviderCharacterKeyedFieldsAreUnchangedByTheDerivedPeers"
        status: pass
    human_judgment: false
  - id: D8
    description: "PropertyProvider still emits the resource attribute `name` and declares it, and every emitted key is declared in Schema()"
    requirement: PROFILE-11
    verification:
      - kind: unit
        ref: "internal/access/policy/attribute/property_test.go#TestPropertyProviderEmitsTheResourceAttributeName / TestPropertyProviderSchemaDeclaresEveryKeyItEmits"
        status: pass
    human_judgment: false
  - id: D9
    description: "BuildABACStack registers ViewerTierProvider, and both new ABACConfig seams are populated from the concrete PostgresRoleStore and the pool at the production composition root"
    requirement: EXT-07
    verification:
      - kind: integration
        ref: "internal/access/setup/setup_viewer_registration_integ_test.go#TestBuildABACStackRegistersTheViewerNamespace / …ResolvesViewerTierThroughTheRealStack / …ResolvesViewerRolesForAnAdminPlayer / …ResolvesDerivedOwnerPlayerIDThroughTheRealStack"
        status: pass
    human_judgment: false
  - id: D10
    description: "The 02-07 seeds can be written against a viewer subject whose player id is comparable to a row-derived player id — the whole point of the derivation"
    requirement: PROFILE-11
    verification:
      - kind: integration
        ref: "internal/access/setup/setup_viewer_registration_integ_test.go#TestBuildABACStackResolvesDerivedOwnerPlayerIDThroughTheRealStack (asserts bags.Subject viewer.player_id and bags.Resource property.owner_player_id in ONE request)"
        status: pass
    human_judgment: true
    rationale: "The attributes resolve and are comparable; whether the 02-07 twins actually express the SAME row semantics the character-flavored family evaluates is a claim about policies that do not exist yet. That binds in 02-07."

duration: 71min
completed: 2026-08-04
status: complete
---

# Phase 02 Plan 13: Row-Level Identity Model for the Viewer Path Summary

**Closed cross-AI review HIGH #10/#11: a `viewer:` subject is player-flavored while `owner`/`visible_to`/`excluded_from` are character-keyed, so the row's character-keyed fields are now resolved server-side into player-keyed peers — in the direction that cannot widen each policy's effect — and every provider this phase adds is registered at the production composition root.**

## Performance

- **Duration:** 71 min
- **Tasks:** 4 of 4 (one checkpoint, three TDD)
- **Files modified:** 15 (5 created, 10 modified)

## Task 0 — the checkpoint, and how it resolved

Task 0 was a `checkpoint:decision` with `gate="blocking"` asking whether the derived peers use the no-widening direction or the plain player union.

Auto mode was active (`workflow.auto_advance` is `true`, `_auto_chain_active` is `false`), and the gate is `blocking`, not `blocking-human`. Per the auto-mode checkpoint protocol the first option was selected: **`no-widening-direction`** — logged as `⚡ Auto-selected: no-widening-direction`.

This is not a guess standing in for a maintainer. `02-CONTEXT.md` D-27 (lines 89-114) already records that exact direction as the **locked** Phase-2 default, with its rationale and its recorded consequence. The option the planner front-loaded and the decision already in the artifact are the same decision. The checkpoint's `resume-signal` obliges an executor to amend `02-CONTEXT.md` D-27 and plan `02-11`'s Amendment F **only if `player-union` is selected** — so no artifact amendment was owed, and none was made.

**For the verifier:** if a maintainer wants the union instead, this is the decision to revisit, and `TestPropertyProviderDerivesTheTwoDirectionsOppositelyOnOneRow` is the single test that changes.

## Accomplishments

- **The character↔player gap is closed where it can be closed.** `seed:property-private-read` compares `resource.property.owner == principal.character.id`; `visible_to`/`excluded_from` are lists of character ids. A player id compared to a character id never matches, and a non-matching key evaluates FALSE, so every `private`, `restricted` and `admin` profile field would have been permanently invisible — no error, no failing test, fail-closed, with "the profile looks bare" as the visible symptom that §8.5.1.1 records as provoking the forbidden repair. The provider now resolves the relation once, server-side, and the policy compares player to player.
- **The derivation leans the safe way on each side, and the choice is guarded mechanically.** `owner_player_id` and `visible_to_players` use ALL; `excluded_from_players` uses ANY. One fixture — a player with two characters, one of them in *both* lists — separates the two rules: the player is ABSENT from the permit peer and PRESENT in the forbid peer. Under a plain union they appear in both.
- **`ViewerTierProvider` is registered.** 02-03 shipped it deliberately unregistered, which meant `principal.viewer.*` was absent from the bag in production. That is 01-SPEC §8.4.1's Phase-2 obligation 1 and it is now discharged, ahead of the 02-07 seeds that read it.
- **Both new seams are FILLED, not merely declared.** `subsystem.go` is the only site the concrete `*PostgresRoleStore` and the pool are reachable. A plan owning `setup.go` without it could have declared `PlayerRoleLookup` and never populated it — the failure cross-AI review HIGH #4 named.
- **One role lookup feeds both namespaces.** `viewer.roles` and `player.roles` resolve through the single `attribute.PlayerRoleLookup` 02-03 declared, so the web read path and the operator socket cannot disagree about whether the same human is an admin at the same moment.

## Task Commits

| Task | Name | Commit | Key files |
| --- | --- | --- | --- |
| 0 | Derived-peer direction checkpoint | (no commit — decision only) | — |
| 1 | Player-scoped roles behind a func-field seam | `fd5e87d09` | `internal/store/role_store.go`, `internal/access/policy/attribute/player.go` |
| 2 | Player-keyed peers for the character-keyed row fields | `6a4c3eb8b` | `internal/world/postgres/character_owner_resolver.go`, `internal/access/policy/attribute/property.go` |
| 3 | Register every provider at the production composition root | `4a7ce190a` | `internal/access/setup/setup.go`, `internal/access/setup/subsystem.go` |
| — | Fixture fix found by the full integration lane (Rule 1) | `512661239` | `internal/store/role_store_integration_test.go` |

All three implementation tasks are `type="tdd"`. Each was written test-first and observed RED before any implementation:

| Task | RED command | RED exit |
| --- | --- | --- |
| 1 | `task test -- ./internal/store/ ./internal/access/policy/attribute/` | non-zero (build failure: `rs.PlayerRoles undefined`, `undefined: WithPlayerRoleLookup` ×3) |
| 2 | `task test -- ./internal/access/policy/attribute/` | `201` (build failure: `too many arguments in call to NewPropertyProvider` ×8) |
| 3 | `task test -- ./internal/access/setup/` | `201` (`TestABACConfigCarriesTheTwoPlan0213Seams` — field absent) |

RED/GREEN landed in one commit per task rather than separate `test(...)`/`feat(...)` commits: each task's `<files>` pairs implementation with its test file as one atomic unit, and a test-only commit would not build. This matches 02-03's recorded precedent in this phase.

## `<verification_integrity>` rule 4 — gates demonstrated RED

Both required gates were **observed failing against a deliberately reverted state**, not reasoned about. The mutation was reverted immediately in each case and the suite re-run green (`RESTORED_EXIT=0`).

| Gate | Mutation | RED exit | Failure message observed |
| --- | --- | --- | --- |
| Emitted-vs-declared schema coherence | Deleted the `owner_player_id` line from `Schema()` while the provider still emits it | `201` | `Should be empty, but was [owner_player_id]` |
| Derivation direction (D-27) | Replaced the ALL branch in `playersWhoseEveryCharacterIsIn` with the plain union (ANY) | `201` | `D-27 ALL direction: a player whose OTHER character was not granted MUST NOT enter visible_to_players…` and `D-27: the row named a CHARACTER; generalizing it to the human behind a second, unnamed character is the widening the permit side declines` |

The second is the one that mattered most: the union is one token away at all times and reads as cleanup, so a demonstration that was reasoned rather than run would not have discharged it.

## Decisions Made

1. **One `ResolveOwnerScopes` call, not one per field.** The plan's action text reads "Resolve all three from ONE `ResolveOwnerScopes` call per field, so a row with a large `visible_to` list costs one query rather than N" — internally ambiguous. The implementation makes ONE call carrying the deduplicated union of every character id the row references. Because the returned map is keyed by player with each player's *complete* character set, every field is evaluated independently against that one map; the result is identical to three calls and strictly cheaper. Pinned by `TestPropertyProviderResolvesEveryDerivedPeerFromOneResolverCall`, which also asserts the call carries the union.
2. **`PlayerRoles` validates the player id as a ULID before touching the pool.** `PlayerHasRole` does not, but the plan's `<behavior>` requires an error for malformed input, and `characters.player_id` is `TEXT` — a malformed id would otherwise just match no rows. Validating pool-free is what lets the malformed-input path be asserted in the **untagged** lane, alongside the interface method-set guard.
3. **The interface guard is a reflection test, not a grep.** The plan's own acceptance criterion says the grep sees only a same-line coincidence and names the test as authoritative. `TestRoleStoreInterfaceMethodSetIsUnchangedByPlayerRoles` asserts the method set is exactly `{AddRole, GetRoles, PlayerHasRole, RemoveRole}`.
4. **A player with an empty scope set is skipped on the permit side.** The resolver never returns one, but "every character of this player is a member" is vacuously true for an empty set — and a vacuous permit is the wrong direction for that branch to fail.
5. **Derived lists are `[]any`.** `evalInExpr` accepts both `[]any` and `[]string`; `[]any` is what `visible_to`/`excluded_from` already emit in the same file, and it is the shape already proven in the exact operator position the 02-07 twins will use (`X in resource.property.<list>`).
6. **`ABACConfig.CharacterOwnerResolver` landed in Task 2, not Task 3.** Task 2 changes `NewPropertyProvider`'s signature and the plan requires every call site updated in that task — `setup.go:235` is one. Adding the field there keeps every intermediate commit building. `PlayerRoleLookup`, the viewer registration and the `subsystem.go` population stayed in Task 3 as planned.

## Deviations from Plan

### Auto-fixed issues

**1. [Rule 1 — Bug] Colliding player usernames in a new integration spec**
- **Found during:** the full-plan integration lane, after Task 3 was committed
- **Issue:** `TestStore`'s new `does not leak roles held by another player's characters` spec seeded two players with `"erin-"+p[:8]`. Two ULIDs minted in the same millisecond share their leading timestamp characters, so the second insert violated `players_username_key` and the spec failed at fixture setup rather than at its assertion.
- **Why it was not caught earlier:** the per-task `<verify>` commands for Tasks 2 and 3 do not include `./internal/store/`, so no lane compiled and ran that spec until the plan-level `<verification>` sweep. That is a real gap in the plan's per-task verify coverage, worth noting for the phase audit — the plan's own `<verification>` block does list `./internal/store/`, and it is what caught it.
- **Fix:** use the full id in the username. The three sibling specs each seed a single player and were never affected.
- **Files modified:** `internal/store/role_store_integration_test.go`
- **Commit:** `512661239`

### Files touched beyond the plan's `files_modified`

Both are additive and neither changes behaviour the plan specified:

| File | Why |
| --- | --- |
| `internal/store/role_store_integration_test.go` | `PlayerRoles`' union/dedup/empty-slice behaviour needs a database. The plan named only the untagged `role_store_player_roles_test.go`, which cannot exercise it. The four new specs joined the store's existing Ginkgo suite rather than minting a second integration file for one method. |
| `internal/access/setup/seed_coverage_test.go` | `productionRegistered` is a hardcoded mirror of "what `BuildABACStack` registers", and its own doc comment requires it stay in sync. `"viewer"` was added. No seed references the viewer namespace yet, so `validateSeedProviderCoverage`'s output — and therefore both the unit assertion and the integration drift-detector — are unchanged either way. |

Two files the plan listed in `files_modified` were also **not** created as separate artifacts: `internal/access/policy/attribute/player_test.go` and `property_test.go` were extended rather than replaced (as intended), and the Task 3 test files were named `setup_viewer_registration{,_integ}_test.go` since the plan named no test file for that task.

### Not a deviation, but worth recording

The plan's Task 2 `<action>` says to pass the character-owner resolver at `test/integration/access/access_suite_test.go` and to "pass nil only if it is not [constructible], and say so in the SUMMARY." **A real `worldpg.NewCharacterOwnerResolver(pool)` was constructible and was passed** — the suite holds the pool. That suite therefore exercises the derivation rather than the nil-resolver omit path.

## Verification

| Gate | Command | Result |
| --- | --- | --- |
| Plan `<verification>` | `task test -- ./internal/store/ ./internal/access/...` | exit 0 — 1559 tests, 1 skipped |
| Whole-repo unit | `task test` | exit 0 — 10777 tests, 4 skipped |
| Plan `<verification>` | `task test:int -- ./internal/access/... ./internal/world/postgres/ ./internal/store/ ./test/integration/access/` | exit 0 — 1899 tests, 1 skipped |
| Plan `<verification>` | `task lint` (incl. sloglint over the two new warn paths) | exit 0 |
| Project rule | `task fmt` then `task fmt:check` | exit 0, formatter edits committed |
| Task 1 `<verify>` | `task test -- ./internal/store/ ./internal/access/policy/attribute/` | exit 0 — 568 tests |
| Task 2 `<verify>` | `task test -- ./internal/access/policy/attribute/` && `task test:int -- ./internal/world/postgres/ ./test/integration/access/` | exit 0 — 320 unit, 299 integration |
| Task 3 `<verify>` | `task test -- ./internal/access/...` && `task test:int -- ./internal/access/setup/` | exit 0 — 1296 unit, 28 integration |
| AC (empty-string sentinel) | `rg -v '^\s*//' property.go \| rg -o 'owner_player_id"\] = ""' \| wc -l` | `0` |
| AC (empty-list sentinel) | `rg -v '^\s*//' property.go \| rg -o '_players"\] = \[\]any\{\}\|_players"\] = nil' \| wc -l` | `0` |
| AC (D-27 cited at the emission site) | `rg -c 'D-27' internal/access/policy/attribute/property.go` | `5` |
| AC (shared type declared once) | `rg -o 'type PlayerRoleLookup func' internal/access/policy/attribute/ \| wc -l` | `1` (in `viewer.go`) |
| AC (RoleStore interface untouched) | `git diff HEAD~N -- internal/store/role_store.go` over the interface block | no change |
| AC (every call site passes three args) | `rg -n 'NewPropertyProvider' --type go` | 2 production + 4 test, all three-arg, **including the `//go:build integration` site at `test/integration/access/access_suite_test.go`** |

The integration-tagged call site (B-9) is the one `task test` cannot compile; `task test:int -- ./test/integration/access/` exits 0, so the signature change does not surface as a break in a later wave.

## Threat mitigations applied

| Threat | Disposition | Where it landed |
| --- | --- | --- |
| T-02-82 (viewer twins keyed on character-keyed row data) | mitigate | Three player-keyed peers derived from the row; policy compares player to player. |
| T-02-83 (derived peer computed from the caller) | mitigate | Derived exclusively from `prop.Owner`/`prop.VisibleTo`/`prop.ExcludedFrom`; fixed by a comment at the emission site; `TestPropertyProviderDerivedPeersComeFromTheRowNeverFromTheCaller` asserts the emitted value is the row owner's player and not another player in scope. |
| T-02-84 (partial derived-peer set on resolver failure) | mitigate | `omitDerivedPlayerPeers` omits all three together with all three witnesses false; asserted directly. |
| T-02-85 (unresolvable owner read as ownerless) | mitigate | `owner_player_id` omitted with `has_owner_player_id=false` while the character-keyed `owner`/`has_owner` pair stays intact, so a policy can still distinguish the two cases; asserted on one fixture. |
| T-02-86 (`excluded_from_players` union direction) | accept | ANY is fail-closed for a `forbid`; recorded in `playersWithAnyCharacterIn`'s doc comment with an explicit "do NOT fix this into an ALL rule for symmetry" and the evasion it would enable. |
| T-02-89 (permit-side peer as a plain union) | mitigate | ALL direction; the one-fixture direction-pair assertion demonstrated RED under a union; `D-27` cited five times in `property.go`, including at the emission site where the drift would be written; Task 0's checkpoint resolved to the conservative option. |
| T-02-87 (unpopulated `ABACConfig` seam) | mitigate | Both fields populated at `subsystem.go`; each documents its nil consequence in `ABACConfig`; `TestBuildABACStackResolvesDerivedOwnerPlayerIDThroughTheRealStack` asserts end-to-end resolution through the stack `BuildABACStack` returns. |
| T-02-88 (emitted-but-undeclared attribute) | mitigate | `Schema()` declares all six; `TestPropertyProviderSchemaDeclaresEveryKeyItEmits` is a symmetric-difference gate, demonstrated RED. |

## Known Stubs

None. Every symbol this plan ships has a real body and a test that exercises it.

The one thing that is *declared but not yet consumed* is the derived-peer vocabulary itself: no seed policy references `owner_player_id`, `visible_to_players` or `excluded_from_players` yet. That is intended sequencing — the attributes ship before the policies that read them, exactly as 02-03 shipped `ViewerTierProvider` before this plan registered it — and plan `02-07` is where they are consumed.

## Invariant registry

No registry invariant is pinned here and no `// Verifies:` annotation was written, per `<verification_integrity>` rule 6. `INV-ACCESS-10` and `INV-ACCESS-11` stay `pending` — they bind in Phase 4, against the read path this vocabulary serves. No ad-hoc family was minted.

## ⚠️ Requirements bookkeeping — EXT-07 and PROFILE-11 were already `[x]` before this plan ran

`gsd-tools query requirements.mark-complete EXT-07 PROFILE-11` was run per this plan's `requirements:` frontmatter. Both checkboxes in `.planning/REQUIREMENTS.md` were **already** `[x]` — flipped by `02-03` in wave 2, which recorded the same gap in its own SUMMARY (§"Requirements bookkeeping").

Seven plans in this phase claim these two IDs (`02-03`, `02-07`, `02-08`, `02-09`, `02-10`, `02-11`, `02-13`) and the verb has no partial-credit model, so the first claiming plan to finish flips the box for all of them.

**This plan's genuine share** is the vocabulary under `provides:` above: the per-player role seam and its two consumers, the three derived property peers and their resolver, and the registration of every provider at the production composition root. **EXT-07 is still open** — `seed:admin-section-access` does not exist (`02-07` owns it) and the admin-section registry is `02-09`'s. **PROFILE-11 is still open** — `seed:profile-public-read` does not exist either.

The checkbox was **not** hand-reverted: `.planning/REQUIREMENTS.md` is a tool-owned parsed artifact and `.claude/rules/planning-artifacts.md` forbids hand-editing tool-owned files to work around tool behaviour. Reporting the gap is the sanctioned path.

## Next Phase Readiness

Ready. The downstream plans have what they gate on:

- **`02-07`** (viewer twin seeds) can write `principal.viewer.tier`, `principal.viewer.roles`, `resource.property.owner_player_id`, `.visible_to_players` and `.excluded_from_players` against namespaces that resolve in production. Its moved-here viewer no-WARN seed-coverage assertion belongs in that wave, where viewer-referencing seeds exist — `seed_coverage.go` scans `policy.SeedPolicies()` only, so asserting it here would pass whether or not the provider were registered.
- **`02-08`** can build its conjunction on `resource.property.name`, now pinned by a test rather than a read instruction.
- **`02-11`** (spec amendments) owes **two** amendments from this plan, on top of 02-03's Amendment F:
  1. §8.5's `property` attribute table gains six derived keys (`owner_player_id`, `has_owner_player_id`, `visible_to_players`, `has_visible_to_players`, `excluded_from_players`, `has_excluded_from_players`), with D-27's derivation direction stated — because the direction is not recoverable from the key names.
  2. §10.5 / the `player` namespace gains `roles` and `has_roles`.
- **Phase 4** inherits D-27's recorded consequence: a player holding two or more characters does not receive an `owner`/`visible_to` permit through the web viewer path unless the row names all of their characters, so the viewer path can be **narrower** than the grid for identity-keyed rows. That is fail-closed and deliberate, and widening it is a Phase-4 decision to be made with the read path in front of it.

## Self-Check: PASSED

All nine created/modified source files verified present on disk. All four commits (`fd5e87d09`, `6a4c3eb8b`, `4a7ce190a`, `512661239`) resolve via `git cat-file -e`. Working tree clean.

---
*Phase: 02-abac-schema-vocabulary*
*Completed: 2026-08-04*
