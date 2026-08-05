---
phase: 02-abac-schema-vocabulary
plan: 03
subsystem: access-control
tags: [abac, attribute-provider, entity-prefix, viewer-tier, oops, ulid]

requires:
  - phase: 02-abac-schema-vocabulary
    provides: "Ordering dependency on the 02-01 tracer only — no shared files."
provides:
  - "access.SubjectViewer / ResourceProfile / ResourceAdminSection prefixes, registered in knownPrefixes so ParseEntityRef accepts all three"
  - "access.ViewerTierAnonymous / ViewerTierGuest / ViewerTierPlayer rung tokens"
  - "access.ViewerSubject / ProfileResource / AdminSectionResource panic-on-empty constructors"
  - "attribute.ViewerTierProvider — the `viewer` attribute namespace (tier, player_id, has_player_id, roles, has_roles)"
  - "attribute.PlayerRoleLookup — the shared per-player role seam, consumed here and by plan 02-13"
  - "attribute.WithViewerRoleLookup functional option"
  - "Error codes INVALID_VIEWER_TIER and INVALID_VIEWER_PLAYER_ID"
affects: [02-07, 02-09, 02-11, 02-13, phase-04-character-access-service]

actuals:
  tokens: 7900
  tasks: 2
  commits: 2

tech-stack:
  added: []
  patterns:
    - "Rung-shaped subject namespace: a tier token travels inside the entity id, so SplitN(ref, \":\", 2) still yields a non-empty id on every rung"
    - "Shared func-typed lookup seam (PlayerRoleLookup) declared in one provider and reused by another, keeping internal/store out of the attribute package's import set"

key-files:
  created:
    - internal/access/policy/attribute/viewer.go
    - internal/access/policy/attribute/viewer_test.go
  modified:
    - internal/access/prefix.go
    - internal/access/prefix_test.go

key-decisions:
  - "The anonymous rung is the deliberate exception to the empty-identifier panic guard: viewer:anonymous is a COMPLETE subject, not a bare prefix, so ViewerSubject(ViewerTierAnonymous, \"\") returns rather than panics — and panics instead on a NON-empty identifier, which signals a caller on the wrong rung. Pinned by an assert.NotPanics so a later 'tighten the guard' change turns RED instead of breaking every anonymous read."
  - "viewer.roles resolves PER PLAYER (the union of roles held by any character of that player), matching 01-SPEC §10.5's normative verdict and the shipped PostgresRoleStore.PlayerHasRole join, so the web and the operator socket cannot give two different answers to 'is this caller an admin' for the same human at the same moment."
  - "roles is OMITTED, never emitted as []string{}, on all three unresolved paths (anonymous rung, no lookup configured, lookup error). An empty list is the list-flavored empty-string sentinel: a RESOLVED value a containsAny-shaped condition would evaluate against."
  - "A role-lookup error is fail-safe, not fatal — logged at warn with the ctx, has_roles=false, resolution continues — mirroring PlayerAttributeProvider's is_guest branch."
  - "PlayerRoleLookup is declared in viewer.go as an exported package-level type precisely so plan 02-13 reuses one signature for PlayerAttributeProvider rather than minting a second."

patterns-established:
  - "Paired positive control on every absence assertion: each omit case asserts key ABSENCE via the comma-ok map form AND resolves a configured-lookup control on the same fixture, so an absence cannot pass because the provider returns nothing."
  - "Grep-shaped sentinel guards counted with `rg -o | wc -l` and a numeric `-eq 0`, never `rg -c` (which prints nothing and exits 1 on the zero matches that mean success)."

requirements-completed: [EXT-07, PROFILE-11]

coverage:
  - id: D1
    description: "viewer:, profile: and admin_section: entity prefixes parse via ParseEntityRef and are members of knownPrefixes"
    requirement: EXT-07
    verification:
      - kind: unit
        ref: "internal/access/prefix_test.go#TestKnownPrefixes_AllConstantsCovered (three new rows) + TestParseEntityRefAcceptsViewerAnonymousSubject / …ProfileResourceNamespace / …AdminSectionResourceNamespace"
        status: pass
    human_judgment: false
  - id: D2
    description: "No call site can produce a bare prefix: ProfileResource/AdminSectionResource panic on an empty identifier, ViewerSubject panics on an empty id for the guest and player rungs and on an unrecognized tier, and the anonymous rung's no-identifier exception is pinned"
    requirement: EXT-07
    verification:
      - kind: unit
        ref: "internal/access/prefix_test.go#TestViewerSubjectPanicsOnEmptyPlayerIDForTheGuestRung / …ForThePlayerRung / …PanicsOnAnUnrecognizedTierToken / …PanicsWhenTheAnonymousRungIsHandedAnIdentifier / …DoesNotPanicForTheAnonymousRungWithNoIdentifier / TestProfileResourcePanicsOnEmptyCharacterID / TestAdminSectionResourcePanicsOnEmptySectionID"
        status: pass
    human_judgment: false
  - id: D3
    description: "ViewerTierProvider resolves all three rungs, omits player_id on the anonymous rung by key absence, and emits has_player_id on every code path"
    requirement: PROFILE-11
    verification:
      - kind: unit
        ref: "internal/access/policy/attribute/viewer_test.go#TestViewerTierProviderResolvesEachRungOfTheTierLadder / TestViewerTierProviderOmitsPlayerIDForTheAnonymousRung"
        status: pass
    human_judgment: false
  - id: D4
    description: "viewer.roles resolves per player behind an omit-don't-sentinel lookup seam, making a viewer-flavored admin read policy expressible at all"
    requirement: PROFILE-11
    verification:
      - kind: unit
        ref: "internal/access/policy/attribute/viewer_test.go#TestViewerTierProviderResolvesRolesPerPlayerWhenALookupIsConfigured / TestViewerTierProviderOmitsRolesOnEveryUnresolvedPath"
        status: pass
    human_judgment: false
  - id: D5
    description: "Schema() declares all five viewer keys, so the resolver cannot silently drop one"
    requirement: PROFILE-11
    verification:
      - kind: unit
        ref: "internal/access/policy/attribute/viewer_test.go#TestViewerTierProviderSchemaDeclaresEveryKeyItEmits"
        status: pass
    human_judgment: false
  - id: D6
    description: "The viewer tier token is server-derived — no constructor accepts a tier sourced from a request header, query parameter, cookie or request field; the trust position is fixed in ViewerSubject's doc comment"
    requirement: PROFILE-11
    verification:
      - kind: unit
        ref: "internal/access/prefix_test.go#TestViewerSubjectPanicsOnAnUnrecognizedTierToken (the mechanical half — a client-supplied string cannot reach a subject)"
        status: pass
    human_judgment: true
    rationale: "The doc-comment trust obligation binds Phase 4's facade call sites; no test in this plan can prove a caller reads the tier from session state rather than a header, because no caller exists yet. The mechanical half (unrecognized tokens panic) is tested; the discipline half is reviewable text until Phase 4 lands the facade."

duration: 34min
completed: 2026-08-04
status: complete
---

# Phase 02 Plan 03: ABAC Subject and Resource Vocabulary Summary

**Landed the three portal entity prefixes with panic-on-empty constructors and the `viewer:` attribute namespace — including the `roles` attribute without which a viewer-flavored admin read policy could reference nothing and would silently deny every caller forever.**

## Performance

- **Duration:** 34 min
- **Tasks:** 2 of 2
- **Files modified:** 4 (2 created, 2 modified)

## Accomplishments

- **Three entity prefixes registered.** `viewer:`, `profile:` and `admin_section:` join `knownPrefixes`, so `ParseEntityRef` resolves each to its type. `viewer:player:<ulid>` carries the rung and the ULID together in the id half, which is exactly what `SplitN(ref, ":", 2)` yields and what `validateRequest` requires (a non-empty id).
- **The bare-prefix hole is closed at every new constructor.** `ProfileResource("")` and `AdminSectionResource("")` panic; `ViewerSubject` panics on an empty id for the guest and player rungs and on any tier token outside the three constants. The anonymous rung's deliberate exception is pinned by an `assert.NotPanics`.
- **`ViewerTierProvider` ships before any policy references it.** An unregistered attribute namespace does not error — `principal.viewer.tier` is simply absent, every condition referencing it evaluates false, and the whole tier-floor family default-denies invisibly. Shipping the provider first makes that failure mode unreachable; plan `02-13` registers it, before `02-07` seeds anything that reads it.
- **`viewer.roles` closes the review's viewer-identity defect at the source.** The shipped admin read policy gates on `"admin" in principal.character.roles`; its viewer twin needs a peer attribute, and §8.4.1's three-key namespace has none. Without `roles` the twin could only ever deny, silently, with no failing test — the visible symptom being "admins see a bare profile" and the cheap repair being widening the policy.

## Task Commits

1. **Task 1: Three entity prefixes and their panic-on-empty constructors** — `4f66001` (feat)
2. **Task 2: ViewerTierProvider** — `b1eb75a` (feat)

Both tasks are `type="tdd"`. Each was written test-first and observed RED before any implementation:

| Task | RED command | RED exit |
| --- | --- | --- |
| 1 | `task test -- ./internal/access/` | `201` |
| 2 | `task test -- ./internal/access/policy/attribute/` | `201` |

(go-task collapses a failing `cmd:` to exit 201; the underlying failure is the compile error on the not-yet-existing symbols plus the new known-prefix table rows.)

The RED/GREEN pair landed in one commit per task rather than separate `test(...)`/`feat(...)` commits, because the plan's `<files>` pairs each implementation file with its test file as one atomic unit and a test-only commit would not build. RED was demonstrated and its non-zero exit recorded above, per `<verification_integrity>` rule 4.

## Files Created/Modified

- `internal/access/prefix.go` — `SubjectViewer`, the three `ViewerTier*` tokens, `ResourceProfile`, `ResourceAdminSection`, three `knownPrefixes` entries, and the `ViewerSubject` / `ProfileResource` / `AdminSectionResource` constructors.
- `internal/access/prefix_test.go` — three known-prefix table rows, four `ParseEntityRef` tests, the rung-ladder table, and seven panic / not-panic assertions.
- `internal/access/policy/attribute/viewer.go` — `ViewerTierProvider`, `PlayerRoleLookup`, `WithViewerRoleLookup`, `parseViewerRung`, `resolveRoles`.
- `internal/access/policy/attribute/viewer_test.go` — one subtest per `<behavior>` row, with every absence assertion paired against a positive control.

## Decisions Made

1. **The anonymous rung is exempt from the empty-identifier panic, and panics on the opposite condition.** §8.4.1 gives it no identifier, so `viewer:anonymous` is a complete subject. The guard exists to stop a *bare prefix* reaching a policy target; `viewer:anonymous` is not one. It panics instead on a non-empty identifier — a caller supplying one is on the wrong rung.
2. **`roles` is per player, not per character.** Matching §10.5's normative verdict and the shipped `PlayerHasRole` join removes a second source of truth about a trust boundary. The alternative — `principal is character` with `principal.character.roles` — would let the operator socket and the web disagree about whether the same human is an admin at the same moment.
3. **`parseViewerRung` rejects `viewer:anonymous:<anything>` with `INVALID_VIEWER_TIER`.** The plan did not specify a code for this shape. `INVALID_VIEWER_TIER` is right: in the `<tier>:<id>` form the `anonymous` token is not a valid tier, and the message says so.
4. **`ResolveSubject` errors (rather than declining) on a subject with no colon at all**, mirroring `PlayerAttributeProvider.ResolveSubject`'s `len(parts) != 2` branch exactly. Diverging would have made two providers in one package disagree about the same malformed input.
5. **`roles` is emitted as `[]string`**, matching `character.go`'s `roles` and `player.go`'s `grants`, and declared `types.AttrTypeStringList` in `Schema()`.

## SPEC deviation to record (for plan 02-11, Amendment F)

01-SPEC §8.4.1's attribute table declares **exactly three** viewer keys — `tier`, `player_id`, `has_player_id`. This plan ships **five**: `roles` (string list) and `has_roles` (bool) are added.

**Rationale for the amendment.** `seed:viewer-property-admin-read` (plan `02-07`) must twin the shipped `permit(principal is character, …) when { resource.property.visibility == "admin" && "admin" in principal.character.roles }` (`internal/access/policy/seed.go:124-128`). The viewer namespace as §8.4.1 declares it has no roles attribute at all, so `principal.viewer.roles` would resolve to nothing; a missing key is FALSE for every operator (`internal/access/policy/dsl/evaluator.go:216`), so the admin twin would deny every caller forever — silently, fail-closed, with no error and no failing test. `roles` is the attribute that makes the twin expressible at all. The `has_roles` witness follows the `has_X` convention in `.claude/rules/abac-providers.md` (always present, true or false, on every code path).

The deviation is noted in `Schema()`'s doc comment in `viewer.go`, so the amendment pass has the text it needs at the source.

## Deviations from Plan

None — plan executed as written. The four items in "Decisions Made" above are details the plan left to the implementer (an unspecified error code for one malformed shape, a no-colon branch inherited from the sibling provider, the concrete list type, and commit granularity), not departures from what it specified.

## Issues Encountered

None. Both tasks went RED → GREEN on the first implementation pass, with `task lint` and `task fmt:check` green without edits.

## Verification

| Gate | Command | Result |
| --- | --- | --- |
| Plan `<verification>` | `task test -- ./internal/access/...` | exit 0 — 1275 tests, 1 skipped |
| Plan `<verification>` | `task lint` | exit 0 |
| Project rule | `task fmt` then `task fmt:check` | exit 0, no uncommitted formatter edits |
| Task 1 `<verify>` | `task test -- ./internal/access/` | exit 0 — 139 tests |
| Task 2 `<verify>` | `task test -- ./internal/access/policy/attribute/` | exit 0 — 300 tests |
| AC (empty-string sentinel) | `rg -v '^\s*//' viewer.go \| rg -o 'player_id"\] = ""' \| wc -l` | `0` |
| AC (empty-list sentinel) | `rg -v '^\s*//' viewer.go \| rg -o 'roles"\] = \[\]string\{\}\|roles"\] = nil' \| wc -l` | `0` |

## Threat mitigations applied

| Threat | Disposition | Where it landed |
| --- | --- | --- |
| T-02-11 (client-supplied tier → elevation) | mitigate | `ViewerSubject` panics on any token outside the three constants; the doc comment fixes the trust position as server-derived-only. |
| T-02-12 (empty-string `player_id` sentinel) | mitigate | Key omitted on the anonymous rung; asserted by key presence with a paired positive control. |
| T-02-13 (unregistered `viewer` namespace) | mitigate (this plan's share) | `Schema()` declares every key the provider emits, so a registered provider cannot silently drop one. Registration itself is `02-13`'s. |
| T-02-14 (non-ULID player identifier) | mitigate | `INVALID_VIEWER_PLAYER_ID` rejects it rather than carrying it into the bag. |
| T-02-15 (player-scoped role union) | accept | Deliberately matches the shipped `PlayerHasRole` semantics; recorded in `PlayerRoleLookup`'s doc comment. Tracked upstream as issue #4899. |
| T-02-16 (role-lookup failure) | mitigate | Omits `roles`, emits `has_roles=false`, logs at warn with the ctx; does not fail resolution. |
| T-02-81 (missing `viewer.roles`) | mitigate | `roles` shipped, declared in `Schema()`, each omit branch asserted against a configured-lookup control. |

## Known Stubs

None. Every symbol this plan ships has a real body and a test that exercises it. `PlayerRoleLookup` is an unimplemented *seam*, not a stub: it is a declared func type with no production implementation yet by design — plan `02-13` owns the `PostgresRoleStore.PlayerRoles` implementation and the wiring, and the omit-don't-sentinel behavior for the unconfigured case is itself tested here.

## Invariant registry

No registry invariant is pinned here and no `// Verifies:` annotation was written, per the plan's `<verification_integrity>` rule 6. `INV-ACCESS-10` and `INV-ACCESS-11` stay `pending` — they bind in Phase 4, against the read path this vocabulary serves.

## Next Phase Readiness

Ready. The downstream plans have what they gate on:

- **`02-07`** (seed policies) can write `principal is viewer`, `resource is profile`, `resource is admin_section`, and can reference `principal.viewer.tier` and `principal.viewer.roles` against a namespace that resolves. Its moved-here `viewer` no-WARN seed-coverage assertion belongs in that wave, where viewer-referencing seeds exist.
- **`02-09`** (admin section registry) can call `access.AdminSectionResource(sectionID)`.
- **`02-11`** (spec amendments) has the Amendment F text above.
- **`02-13`** can implement `PostgresRoleStore.PlayerRoles` against `attribute.PlayerRoleLookup`, add `WithPlayerRoleLookup` to `PlayerAttributeProvider` reusing the same type, and register `ViewerTierProvider` in `BuildABACStack`.

One caveat for `02-13`: `ViewerTierProvider` is **not yet registered** in `internal/access/setup/`, so `principal.viewer.*` is absent from the bag in production today. That is intentional sequencing (registration before any seed references it), but it means no integration-tier test can exercise the viewer namespace end-to-end until `02-13` lands.

## ⚠️ Requirements bookkeeping — EXT-07 and PROFILE-11 are marked complete PREMATURELY

`gsd-tools query requirements.mark-complete EXT-07 PROFILE-11` (run per this plan's `requirements:` frontmatter, per the execute-plan workflow) flipped both checkboxes in `.planning/REQUIREMENTS.md` to `[x]`. **Neither requirement is actually satisfied yet**, and a later audit reading those boxes would be misled:

- **EXT-07** reads "`seed:admin-section-access` covers all seven sections and every future section at zero additional policy cost." That seed **does not exist**. This plan shipped only the `admin_section:` prefix and `AdminSectionResource()` that the seed will target. Plan **`02-09`** (which also claims EXT-07) owns the section registry; the seed itself is **`02-07`**'s.
- **PROFILE-11** reads "One new seed policy (`seed:profile-public-read`) permits off-location profile reads…". That seed **does not exist** either. This plan shipped the `profile:` resource prefix and the `viewer` namespace it will be evaluated against. Plans **`02-07`**, **`02-08`**, **`02-10`** and **`02-13`** all also claim PROFILE-11.

Both IDs are claimed by **seven** plans in this phase (`02-03`, `02-07`, `02-08`, `02-09`, `02-10`, `02-11`, `02-13`), and `requirements.mark-complete` has no notion of partial credit — the first claiming plan to finish flips the box for all of them. The same thing already happened to IDENT-06 in wave 1 (`[x]` while `02-06` and `02-11` still owe work), so this is the tool's established behavior in this repo, not a one-off.

**The checkbox was deliberately NOT hand-reverted.** `.planning/REQUIREMENTS.md` is a tool-owned parsed artifact, and `.claude/rules/planning-artifacts.md` forbids hand-editing tool-owned files to work around tool behavior — a local edit here would be invisible to everyone else and would be re-applied by the next claiming plan anyway. Reporting the gap is the sanctioned path.

**For the verifier and the phase audit:** treat EXT-07 and PROFILE-11 as **open** until `02-07` seeds the policies and `02-09` lands the section registry. This plan's genuine share of both is exactly the vocabulary listed under `provides:` above — the prefixes, the constructors and the `viewer` attribute namespace those policies will reference.

## Self-Check: PASSED

All four created/modified source files exist on disk; both task commits (`4f660014d`, `b1eb75a6d`) resolve in `git log`.

---
*Phase: 02-abac-schema-vocabulary*
*Completed: 2026-08-04*
