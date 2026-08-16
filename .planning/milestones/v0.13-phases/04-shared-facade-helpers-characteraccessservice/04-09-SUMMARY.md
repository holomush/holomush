---
phase: 04-shared-facade-helpers-characteraccessservice
plan: 09
subsystem: api
tags: [world-model, abac, outbox, entity-properties, optimistic-concurrency, tdd]

requires:
  - phase: 04-shared-facade-helpers-characteraccessservice
    provides: "04-01's world.Service shape (GetCharacterDescription, the read_description gate) that this plan's sibling command sits beside"
  - phase: 01-portal-spec
    provides: "01-SPEC §7.1/§7.2 (the twelve profile.* names, rows not columns), §9.3 (the ABAC write-on-character gate), §8.6 (the per-attribute tier floor)"
provides:
  - "world.Service.UpdateCharacterProfileAttributes — the domain command 04-06's facade calls to write a character's profile prose"
  - "outbox.KindCharacterProfileUpdate + its registry entry (AppSchemaVersion 2 -> 3)"
  - "worldMutator.updateCharacterProfileAttributes — the first production property writer, CAS-first then create/update/delete"
  - "world.BuildCharacterProfileUpdatePayload — names-only, erasure-safe profile payload"
  - "A transactional fake stack (rollback-capable transactor + in-memory property store + append-only outbox) reusable by any world spec that must prove commit-or-roll-back"
affects: [04-06, 04-07, 05-character-identity-ui]

actuals:
  tokens: 12000
  tasks: 2
  commits: 3

tech-stack:
  added: []
  patterns:
    - "Aggregate-level CAS as the lock for a child-table write: the version-guarded character update runs FIRST inside the closure, so a stale caller is refused before any property row is touched and the partition computed outside the transaction stays valid"
    - "Names-only envelope payload for player-authored prose — erasure-safe by construction"

key-files:
  created:
    - internal/world/mutator_profile_test.go
    - internal/world/service_profile_test.go
  modified:
    - internal/world/outbox/taxonomy.go
    - internal/world/service.go
    - internal/world/mutator.go
    - internal/world/payloads.go

key-decisions:
  - "The gate is ABAC `write` on character:<id>, never the property resource: a create has no property id and PropertyProvider.ResolveResource fails closed on a row that does not exist, so a property-resource gate would default-deny the FIRST write of every profile attribute"
  - "Every created row sets Visibility explicitly to \"public\" — PropertyRepository.Create passes p.Visibility straight into the INSERT and applies no defaulting, so the column DEFAULT never applies and an empty string fails the CHECK constraint"
  - "The envelope payload carries the changed attribute NAMES, never their values — profile prose is player-authored personal content and the taxonomy rule is new-values-only AND erasure-safe"
  - "The partition reads outside the write transaction, which is safe because the character CAS is the aggregate lock and this command is the only production property writer"
  - "IDENT-02 NOT marked complete: its server-enforced length caps belong to the facade handler (01-SPEC §9.3), not to this world command"

patterns-established:
  - "Rollback-capable test transactor: snapshot the fake store + outbox, restore on error — proves 'neither the row nor the envelope survives' as a statement about STATE, not about a call counter"
  - "Paired ABAC proof over the SHIPPED seed corpus (abactest.NewSeedEngine + attribute.NewCharacterProvider) so the permit under test is seed:player-self-access itself, with the deny and the positive control on one fixture"

requirements-completed: []

coverage:
  - id: D1
    description: "world.Service.UpdateCharacterProfileAttributes writes a character's profile.* attribute rows and exactly one character_profile_update envelope in the same transaction"
    verification:
      - kind: unit
        ref: "internal/world/service_profile_test.go#TestWorldServiceUpdateCharacterProfileAttributesFirstWrite"
        status: pass
      - kind: unit
        ref: "internal/world/mutator_profile_test.go#TestWorldServiceUpdateCharacterProfileAttributesExecutorSeam"
        status: pass
    human_judgment: false
  - id: D2
    description: "The attribute rows and their envelope commit or roll back together; a forced failure mid-closure leaves neither behind"
    verification:
      - kind: unit
        ref: "internal/world/mutator_profile_test.go#TestWorldServiceUpdateCharacterProfileAttributesExecutorSeam/rolls_back_both_the_attribute_row_and_the_envelope"
        status: pass
    human_judgment: false
  - id: D3
    description: "The CAS guard carries the CALLER's expected version (INV-WORLD-7); a stale version aborts with WORLD_CONCURRENT_EDIT, distinct from a not-found"
    verification:
      - kind: unit
        ref: "internal/world/mutator_profile_test.go#TestWorldServiceUpdateCharacterProfileAttributesExecutorSeam/aborts_on_a_stale_expected_version"
        status: pass
      - kind: unit
        ref: "internal/world/service_profile_test.go#TestWorldServiceUpdateCharacterProfileAttributesGuards"
        status: pass
    human_judgment: false
  - id: D4
    description: "The write surface is closed at the domain layer: a name outside 01-SPEC §7.2's twelve is rejected before any read or write"
    verification:
      - kind: unit
        ref: "internal/world/service_profile_test.go#TestWorldServiceUpdateCharacterProfileAttributesGuards/rejects_an_attribute_name_outside_the_twelve"
        status: pass
    human_judgment: false
  - id: D5
    description: "The write is gated by ABAC write on character:<id>, resolved against the shipped seed:player-self-access permit, with a paired positive control"
    verification:
      - kind: unit
        ref: "internal/world/service_profile_test.go#TestWorldServiceUpdateCharacterProfileAttributesSeedAuthorization"
        status: pass
    human_judgment: false
  - id: D6
    description: "The new kind is registered across all five census surfaces and the compile-time write fence is untouched"
    verification:
      - kind: unit
        ref: "task test -- -run 'Census|ReaderViews' ./test/meta/ ./internal/world/"
        status: pass
    human_judgment: false

duration: 17min
completed: 2026-08-11
status: complete
---

# Phase 04 Plan 09: World Character Profile-Attribute Write Summary

**`world.Service.UpdateCharacterProfileAttributes` — a caller-version-guarded, ABAC-gated write of the twelve `profile.*` `entity_properties` rows, routed through the existing `mutate()` seam so the rows and their single `character_profile_update` envelope commit or roll back together.**

## Performance

- **Duration:** 17 min
- **Started:** 2026-08-11T14:58:32Z
- **Completed:** 2026-08-11T15:15:40Z
- **Tasks:** 2
- **Files modified:** 6 (2 created, 4 modified)

## Accomplishments

- The world layer can now write character profile prose. This is genuinely new domain code — before it, the only production use of the property repository was a `ListByParent` read and the delete-cascade `DeleteByParent`, so this is the **first production property writer**.
- All five census surfaces landed in ONE commit, so all three census directions are green at every commit rather than only at the end: the taxonomy constant, its `registry` entry, the private `kind*` constant, the `writeCommands` descriptor, and the `s.mutator`-routing `Service` method the descriptor names.
- The compile-time write fence is intact and **unrelaxed**: `Service.propertyRepo` stays `PropertyReader`, the write-capable `PropertyRepository` is reachable only from the executor, and `fence_test.go` is untouched (still 6 reader-view rows, no `Profile`/`mutator` mention).
- The census is satisfied by REGISTRATION, not by an exemption: `rg -n 'UpdateCharacterProfileAttributes' test/meta/` returns no match at all.

## Task Commits

1. **Task 1 (tracer, TDD RED)** — `948056fc` (test) — the failing executor-seam and first-write specs
2. **Task 1 (tracer, TDD GREEN)** — `757a0320` (feat) — all five census surfaces + the closure builder + the command, in one commit
3. **Task 2** — `c71a402b` (test) — the contract matrix and the three doc comments

_Task 2 required no production behavior change: its seven behaviors were already satisfied by Task 1's implementation, which is what a pinning task looks like when the tracer was built to the full contract. Its production half is the doc comments._

## Files Created/Modified

- `internal/world/outbox/taxonomy.go` — `KindCharacterProfileUpdate` + registry entry + `characterProfilePayload`; `AppSchemaVersion` 2 → 3
- `internal/world/service.go` — private `kindCharacterProfileUpdate`, the closed `profileAttributeNames` set, and `UpdateCharacterProfileAttributes` (guard chain, `checkAccess`, partition, `s.mutator` routing in its own body)
- `internal/world/mutator.go` — the `writeCommands` descriptor row and the `updateCharacterProfileAttributes` closure builder
- `internal/world/payloads.go` — `CharacterProfileUpdateChangePayload` + `BuildCharacterProfileUpdatePayload`
- `internal/world/mutator_profile_test.go` — the transactional fake stack (rollback transactor, in-memory property store, append-only outbox) and the three executor-seam specs
- `internal/world/service_profile_test.go` — first-write/per-field, partition arms, guards, and the seeded-corpus ABAC pair

## Decisions Made

- **The gate is on the character, not the property.** `access.PropertyResource` takes a property id a not-yet-created row does not have, and resolving `resource.property.owner` requires `PropertyProvider.ResolveResource` to fetch the row, which fails closed with `PROPERTY_FETCH_FAILED` when there is none. A property-resource gate would default-deny the FIRST write of every attribute. The created rows still carry `Owner`, so `seed:property-owner-write` remains available as defense in depth for a later per-row path.
- **Payload carries names, not values.** Profile prose is player-authored personal content; the taxonomy's payload rule is new-values-only AND erasure-safe. A consumer learns THAT a profile changed and which fields, and reads current values through the authorized path where the tier floor still applies.
- **The partition reads outside the transaction, deliberately.** The character-row CAS is the aggregate's lock: a concurrent profile write that would invalidate the partition must have bumped the character version and is refused before any property row is touched. This command is the only production property writer, so nothing else can move a profile row without moving that version. Recorded in the method doc so a later reader does not "fix" it into an in-transaction re-read.
- **A profile-only write bumps `characters.version`.** The profile is part of the character aggregate and the aggregate carries exactly one optimistic-concurrency token. Documented on the method as intended, not accidental.

## Deviations from Plan

### 1. [Convention] The payload type and builder landed in `payloads.go`, not in the plan's `files_modified`

- **Found during:** Task 1
- **Issue:** The plan's `files_modified` lists only `taxonomy.go`, `mutator.go`, `service.go` and the two test files, but every other envelope payload type and `Build*Payload` function in package `world` lives in `payloads.go`.
- **Fix:** Added `CharacterProfileUpdateChangePayload` + `BuildCharacterProfileUpdatePayload` to `payloads.go` rather than inlining a one-off payload type in `service.go`. CLAUDE.md's "MUST match conventions" governs; a reader looking for a payload builder looks in `payloads.go`.
- **Files modified:** `internal/world/payloads.go`
- **Verification:** `task test -- ./internal/world/...`, `task lint`, `task build` all green.
- **Committed in:** `757a0320`

### 2. [Plan-internal inconsistency] Row construction lives in `service.go`, so one acceptance criterion's grep targets the wrong file

- **Found during:** Task 1
- **Issue:** Task 1's `<action>` says the Service "partition[s] the requested changes into creates, updates and deletes" and the closure "appl[ies] the property rows" — so the fully-specified rows are BUILT during the partition, in `service.go`. But the acceptance criterion reads `rg -n 'Visibility:' internal/world/mutator.go` shows an explicit non-empty literal at the create site. Those cannot both hold.
- **Fix:** Followed the `<action>` (the normative text) and kept construction in `service.go`, where the reader view that computes the partition already lives. The criterion's underlying property — no field left to a zero value, no reliance on a database default — is satisfied and is asserted more strongly than a grep could: `service_profile_test.go` asserts the STORED row's `Visibility == "public"`, `Owner` non-nil and equal to the character ULID, `ParentType`, `ParentID`, and a non-zero `ID`.
- **Corrected criterion:** `rg -n 'Visibility:' internal/world/service.go` shows the explicit `"public"` literal at the create site.
- **Committed in:** `757a0320`

### 3. [Scope] `requirements: [IDENT-02]` deliberately NOT marked complete

- **Found during:** Task 2
- **Issue:** The plan's frontmatter claims IDENT-02, but IDENT-02 is "a player can edit their character's prose fields with **server-enforced length caps**". 01-SPEC §9.3 assigns those caps to `CharacterAccessService.UpdateCharacterProfile` — the facade RPC (04-06/04-07) — not to the world command. This plan ships the name-set validation, not the caps.
- **Fix:** `requirements-completed: []`. `requirements mark-complete` was NOT run. This matches 04-02 and 04-03, which both reverted the same auto-flip. The method's doc comment states explicitly that caps are the facade's, so a later reader does not add a second divergent cap here.
- **Verification:** 01-SPEC:2020 — "Server-enforced length caps (IDENT-02)" appears on the facade RPC row.

### 4. [Lint] Fixture parameters trimmed to keep `task lint` green at every commit

- **Found during:** Task 1
- **Issue:** `profileTxFixture` was written with `action string` and `seed ...world.EntityProperty` parameters intended for Task 2, and `unparam` flagged both as always receiving the same value / nil.
- **Fix:** Trimmed the fixture to `(t, subjectID, charID)`; Task 2 seeds the returned store directly via `seedProfileRow`. No `//nolint` was added — the finding was real for the commit it appeared in.
- **Committed in:** `757a0320`

---

**Total deviations:** 4 (1 convention, 1 plan-internal inconsistency, 1 scope-honesty, 1 lint). No scope creep; no security-relevant auto-fixes were needed.

## Issues Encountered

- **04-04 running concurrently on the same working tree.** Every `git add` was scoped to explicit paths from this plan's own `files_modified`. 04-04's in-flight edits to `internal/grpc/characteraccess_profile_test.go`, `api/proto/holomush/characteraccess/v1/characteraccess.proto` and `web/src/lib/connect/.../characteraccess_pb.ts` were observed unstaged in the working tree and deliberately left alone; none of them was touched, staged or reverted by this plan. No file under `internal/grpc/`, `internal/web/`, `api/proto/`, `pkg/proto/` or `web/` was modified here.
- **A formatter hook re-ran after two edits**, once orphaning the `CharacterLifecycleChangePayload` doc comment onto the newly inserted type. Caught by reading the region back and re-ordering the two declarations before any commit.

## Threat Flags

None. The dispositions in the plan's threat register were all implemented as written:

| Threat | Disposition | Where |
|---|---|---|
| T-04-23 widening `propertyRepo` | mitigate | fence untouched; `propertyRepo` still `PropertyReader`; `TestServiceHoldsOnlyReaderViews` green |
| T-04-24 absent/zero expected version | mitigate | rejected before any read; the test asserts the repository was never called |
| T-04-25 concurrent profile mutation | mitigate | caller's version is the CAS guard; the stale test's mock is armed only for the caller's value, so a freshly-read guard fails as an unexpected call |
| T-04-26 name outside the closed set | mitigate | rejected at the domain method before any read or write |
| T-04-27 state change without its envelope | mitigate | rollback test asserts neither the row nor the envelope survives |
| T-04-28 declared kind with no producer | mitigate | all five surfaces in one commit; all three census directions green |

## Known Stubs

None. Every surface this plan declares is wired and exercised; no test is skipped and every `<verify>` was run.

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- **04-06's facade has a domain command to call.** `world.Service.UpdateCharacterProfileAttributes(ctx, caller, characterID, expectedVersion, map[string]string)` — property names are the full `profile.<field>` strings from 01-SPEC §7.2, and the empty string clears a field.
- **04-06 still owns IDENT-02's length caps.** The domain method validates the NAME set only, by design.
- **The twelve names are duplicated at the facade by design** (defense in depth). `profileAttributeNames` is intentionally unexported so the facade allowlist is a genuine second gate rather than an alias of the first.
- **Integration coverage was not re-run here.** `task test` does not compile `//go:build integration` files; the wave gate should run `task test:int` since this plan added a shipped world command.

## Self-Check: PASSED

- Files created exist on disk: `internal/world/mutator_profile_test.go`, `internal/world/service_profile_test.go`, this SUMMARY.
- Commits exist in history: `948056fc`, `757a0320`, `c71a402b`.
- `task test -- ./internal/world/...` exit 0 (999 tests); `task test -- -run 'Census|ReaderViews' ./test/meta/ ./internal/world/` exit 0; `task lint` exit 0; `task build` exit 0.

---
*Phase: 04-shared-facade-helpers-characteraccessservice*
*Completed: 2026-08-11*
