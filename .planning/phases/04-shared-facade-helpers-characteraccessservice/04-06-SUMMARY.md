---
phase: 04-shared-facade-helpers-characteraccessservice
plan: 06
subsystem: internal/grpc
tags: [grpc, mutation, field-mask, optimistic-concurrency, validation, abac, ginkgo, tdd]
status: complete

requires:
  - phase: 04-shared-facade-helpers-characteraccessservice
    provides: "04-01's CharacterAccessServer + characterAccessWorldReader; 04-02's playerGate (ownedCharacter, resolveAndGate) and the phase gate error matrix; 04-04's OwnCharacter proto surface; 04-05's projectOwner and ownedProfileAttributes; 04-09's world.Service.UpdateCharacterProfileAttributes"
  - phase: 01-portal-spec
    provides: "§7.2 (the twelve prose profile names), §9.4/§9.5 (the update-mask contract and the version guard), §9.6 (the error surface)"
provides:
  - "CharacterAccessServer.UpdateCharacterProfile — the masked prose profile edit with byte-measured caps"
  - "CharacterAccessServer.UpdateCharacterDescription — the in-world description edit over the shipped world command"
  - "characterAccessWorldMutator — the facade's ONE mutate-side seam (exactly two methods); worldMutator field + constructor parameter"
  - "CharacterAccessServer.ownedCharacterForMutation — the mutation surface's ownership mapping over the shared gate (04-08 Task 2 must accept this name)"
  - "updateCharacterProfileMaskablePaths — the closed twelve-path facade allowlist"
  - "world.CodeCharacterInvalid — the domain's typed value-rejection code, now cross-package"
  - "test/integration/access/character_write_test.go — 13 Ginkgo specs over both mutations against the real world.Service"
affects: [04-07, 04-08, 05-character-identity-ui]

actuals:
  tokens: 96000
  tasks: 3
  commits: 3

tech-stack:
  added: []
  patterns:
    - "Allowlist entries carrying their own accessor and cap, so each of the twelve §7.2 names is written down exactly once — a parallel switch would be a second table keyed by the same names"
    - "Facade-side validation that DELEGATES the shared rules to the domain validator and adds only the narrower per-field cap, making a facade/domain disagreement about UTF-8 and control characters impossible by construction"
    - "A documenting test that asserts the LIMIT of a guard (last-write-wins inside the TOCTOU window), so a narrowing cannot be mistaken for a guarantee"
    - "Fail-on-call domain double as the proof that a handler path returns before any write"

key-files:
  created:
    - internal/grpc/characteraccess_write.go
    - internal/grpc/characteraccess_write_test.go
    - test/integration/access/character_write_test.go
  modified:
    - internal/grpc/characteraccess_service.go
    - internal/grpc/characteraccess_owner_test.go
    - internal/grpc/characteraccess_profile_test.go
    - internal/grpc/characteraccess_viewer_test.go
    - internal/world/service.go
    - internal/world/service_test.go
    - internal/world/errors.go
    - cmd/holomush/sub_grpc.go
    - internal/testsupport/integrationtest/harness.go
    - test/integration/access/character_profile_read_test.go
    - .planning/REQUIREMENTS.md

key-decisions:
  - "update_mask paths are the §7.2 property names verbatim (`profile.pronouns`), not the bare proto field names. 01-SPEC §9.5 rule 2 settles it by example: '`profile` MUST NOT reach `profile.rp_preferences`'."
  - "The allowlist map's VALUE carries the request accessor and the cap. A map[string]struct{} plus a parallel path→getter switch would write the twelve names twice and would also make the plan's own 12-occurrence count criterion unsatisfiable."
  - "Issue #4954 was real: world.Service.UpdateCharacterDescription performed no validation whatsoever. Closed in the domain via char.SetDescription, with a behavioral RED captured first."
  - "world.CodeCharacterInvalid is an exported constant, unlike its LOCATION_INVALID / OBJECT_INVALID siblings, because it crosses a package boundary exactly as CodeConcurrentEdit does."
  - "The description path's version guard is a TOCTOU NARROWING and is documented and TESTED as such (issue #4956 filed for option (b)); the profile path's is a genuine CAS and only IT carries the exactly-one-of-two-writers spec."
  - "The integration fixture places the character in a location, because 04-05 deviation 6's colocation consequence would otherwise make every post-write profile projection vacuously empty."

patterns-established:
  - "Post-write response re-resolves the character so the client receives the BUMPED version as its next expected_version rather than guessing it"
  - "Two rejection messages (facade cap vs domain refusal) kept distinct so 'which layer rejected this' is readable in a log without either message naming a field"

requirements-completed: [IDENT-02, IDENT-02a]

coverage:
  - id: D1
    description: "An owner edits prose profile fields through a typed RPC under a closed exact-string mask allowlist; an unlisted path or a container prefix is rejected"
    requirement: "IDENT-02"
    verification:
      - kind: unit
        ref: "internal/grpc/characteraccess_write_test.go#TestUpdateCharacterProfileRejectsAMaskPathOutsideTheAllowlist, #TestUpdateCharacterProfileRejectsAContainerPrefixRatherThanExpandingIt"
        status: pass
      - kind: integration
        ref: "test/integration/access/character_write_test.go#W9, #W11"
        status: pass
    human_judgment: false
  - id: D2
    description: "Prose profile caps are byte-measured, reuse the shipped world constants, and are enforced server-side before any store work"
    requirement: "IDENT-02"
    verification:
      - kind: unit
        ref: "#TestUpdateCharacterProfileEnforcesByteMeasuredCapsAtTheBoundary, #TestUpdateCharacterProfileMeasuresCapsInBytesNotRunes, #TestUpdateCharacterProfileRejectsMalformedProseIsBehaviorNine"
        status: pass
      - kind: integration
        ref: "test/integration/access/character_write_test.go#W10 (no row written)"
        status: pass
    human_judgment: false
  - id: D3
    description: "An over-cap, invalid-UTF-8 or control-character in-world description is rejected server-side and never reaches the characters column (issue #4954)"
    requirement: "IDENT-02a"
    verification:
      - kind: unit
        ref: "internal/world/service_test.go#TestWorldService_UpdateCharacterDescription (five rejection subtests + the at-cap acceptance)"
        status: pass
      - kind: integration
        ref: "test/integration/access/character_write_test.go#W2, #W3, #W4, #W5 — against the REAL world.Service, asserting the row is UNCHANGED"
        status: pass
    human_judgment: false
  - id: D4
    description: "The description write reaches the shipped world.Service.UpdateCharacterDescription, inheriting its same-transaction outbox emission — exactly one envelope per state change"
    requirement: "IDENT-02a"
    verification:
      - kind: unit
        ref: "#TestUpdateCharacterDescriptionReachesTheShippedWorldCommand"
        status: pass
      - kind: integration
        ref: "test/integration/access/character_write_test.go#W1 (row changed, version bumped, outbox count == 1)"
        status: pass
    human_judgment: false
  - id: D5
    description: "An absent or zero expected_version is rejected at the RPC boundary before any domain call, on BOTH mutations"
    verification:
      - kind: unit
        ref: "#TestUpdateCharacterProfileRejectsAnUnguardedWriteBeforeAnyDomainCall, #TestUpdateCharacterDescriptionRejectsAnUnguardedWriteBeforeAnyDomainCall (fail-on-call double)"
        status: pass
      - kind: integration
        ref: "test/integration/access/character_write_test.go#W8"
        status: pass
    human_judgment: false
  - id: D6
    description: "For the PROFILE write the guard is a genuine CAS: of two mutations carrying the same expected_version exactly one succeeds and the loser is surfaced as Aborted, never retried"
    verification:
      - kind: unit
        ref: "#TestUpdateCharacterProfileSurfacesAStaleVersionAsAbortedWithoutRetrying (asserts the caller's version reaches the domain verbatim)"
        status: pass
      - kind: integration
        ref: "test/integration/access/character_write_test.go#W13 — one success, one Aborted, the loser's value absent from the row"
        status: pass
    human_judgment: false
  - id: D7
    description: "For the DESCRIPTION write the guard is a TOCTOU narrowing, not a guarantee, and the limit is pinned rather than overclaimed"
    verification:
      - kind: unit
        ref: "#TestUpdateCharacterDescriptionRejectsAStaleVersionBeforeTheDomainCall (stale detected) and #TestUpdateCharacterDescriptionDoesNotDetectAWriterInsideTheReadToReReadWindow (window NOT detected; cites issue #4956)"
        status: pass
    human_judgment: false
  - id: D8
    description: "Both mutations deny a non-owner with CHARACTER_NOT_OWNED at PermissionDenied, uniformly across unparseable / no-such-row / not-owned, while the gate's Internal survives"
    verification:
      - kind: unit
        ref: "#TestUpdateCharacterProfileDeniesEveryOwnershipCauseUniformly (three-way message equality), #TestUpdateCharacterDescriptionDeniesEveryOwnershipCauseUniformly, #TestUpdateCharacterProfilePropagatesTheGatesInternalFailureVerbatim"
        status: pass
    human_judgment: false
  - id: D9
    description: "The facade reaches the domain through ONE narrow two-method seam satisfied by the same *world.Service the read seam receives — no new production type, no third construction site, D-79 fence intact"
    verification:
      - kind: other
        ref: "characterAccessWorldMutator has exactly two method lines; `rg -v '^\\s*//' internal/grpc/characteraccess_write.go | rg -o 's\\.worldMutator\\.' | wc -l` == 2; `rg -n 'CharacterAccessServer' internal/testsupport/integrationtest/session.go` == no match; `rg -n 'characterRepo|CharacterRepository|propertyRepo' internal/grpc/characteraccess_write.go` == no match; task build green"
        status: pass
    human_judgment: false
  - id: D10
    description: "An empty mask is a no-op success placed after ownership, and an empty description clears the column with the public profile omitting the field"
    verification:
      - kind: unit
        ref: "#TestUpdateCharacterProfileTreatsAnEmptyMaskAsANoOpSuccessAfterOwnership (both arms), #TestUpdateCharacterDescriptionAcceptsAnEmptyDescription"
        status: pass
      - kind: integration
        ref: "test/integration/access/character_write_test.go#W6 (public read omits), #W12 (erasure removes the row)"
        status: pass
    human_judgment: false

duration: 68min
completed: 2026-08-11
---

# Phase 04 Plan 06: The owner's two edit surfaces Summary

**An owner can now edit the twelve prose `profile.*` fields and the in-world `characters.description` over typed RPCs, both guarded by `expected_version` and both capped server-side in bytes — and closing IDENT-02a's half required first fixing a shipped domain command that validated nothing at all (issue #4954).**

## Performance

- **Duration:** 68 min
- **Started:** 2026-08-11T16:31:24Z
- **Completed:** 2026-08-11T17:39:00Z
- **Tasks:** 3 completed
- **Files:** 14 (3 created, 11 modified)

## Task Commits

1. **Task 1 — the mutate seam + `UpdateCharacterProfile`** — `77cf30388`
2. **Task 2 — close the description-validation gap in the domain (#4954)** — `0511f7873`
3. **Task 3 — `UpdateCharacterDescription` + the integration spec** — `d09712b04`

## RED demonstrated (TDD)

### Task 1 — compile RED

The whole test file was authored and run before any production change:

```text
=== FAIL: internal/grpc  (0.00s)
FAIL	github.com/holomush/holomush/internal/grpc [build failed]

=== Errors
internal/grpc/characteraccess_write_test.go:179:116: too many arguments in call to NewCharacterAccessServer

	have (*ownerWorldReader, *recordingWorldMutator, *failOnCallProfileVisibility, ...)
	want (characterAccessWorldReader, characterAccessProfileVisibility, auth.PlayerSessionRepository, auth.PlayerRepository, auth.CharacterRepository)

internal/grpc/characteraccess_write_test.go:293:19: undefined: characterNotOwnedMessage
internal/grpc/characteraccess_write_test.go:576:17: undefined: updateCharacterProfileMaskablePaths
```

### Task 2 — BEHAVIORAL RED, verbatim, taken BEFORE the fix

This is the one the plan demanded, and it is not a compile error. The test compiled against HEAD, ran, and **the write went through**:

```text
=== FAIL: internal/world TestWorldService_UpdateCharacterDescription/rejects_a_description_one_byte_past_the_cap_before_any_write (0.00s)
    mock.go:361:
        assert: mock: I don't know what to return because the method call was unexpected.
        	Either do Mock.On("Update").Return(...) first, or remove the Update() call.
        	This method was unexpected:
        		Update(context.backgroundCtx,*world.Character)
        		1: &world.Character{ID:…, Name:"Alice", Description:"aaaa…aaaa" (4001 bytes), …, Version:5}
        	at: [.../worldtest/mock_CharacterRepository.go:516 .../mutator.go:290 .../mutator.go:211
        	     .../mutator.go:289 .../service.go:907 .../service_test.go:537]

=== FAIL: … /rejects_a_multi-byte_value_under_the_RUNE_cap_but_over_the_BYTE_cap_before_any_write
=== FAIL: … /rejects_invalid_UTF-8_before_any_write            (Description:"\xff\xfeA")
=== FAIL: … /rejects_an_ANSI_escape_before_any_write           (Description:"\x1b[31mdanger")
=== FAIL: … /rejects_a_BEL_before_any_write                    (Description:"ring\aring")

DONE 12 tests, 6 failures in 0.243s
```

The stack is the whole proof: `service.go:907` → `mutator.go:289` → `characterWriter.Update`, with a 4001-byte value in flight and no validator anywhere on the path.

**The run is also non-vacuous in the other direction.** The authorization-ordering subtest ("an unauthorized caller gets the authorization error, never the validation error") was GREEN under HEAD and stayed green after the fix, so the five failures are specific to the missing validation rather than a broken fixture.

### Task 3 — integration non-vacuity probe

Ginkgo suites report to `gotestsum` as a single Go test, so "13 specs added, suite green" is not by itself evidence the specs ran. One assertion was temporarily inverted (`outboxCount()` expected 99):

```text
• [FAILED] IDENT-02/IDENT-02a … [It] W1: an owner's edit changes the characters row and commits exactly one envelope
  [FAILED] TEMPORARY non-vacuity probe
  Expected <int>: 1 to equal <int>: 99

FAIL! -- 90 Passed | 1 Failed | 0 Pending | 1 Skipped
```

90 passed against 77 before the file, i.e. all 13 new specs executed. The probe was reverted before the commit.

## Accomplishments

### Issue #4954 — a shipped domain command that validated nothing

The plan's scope-addition paragraph turned out to be exactly right, and the finding is worth restating because an earlier revision of this plan asserted the opposite ("the description write inherits that path's validation"):

| Layer | At HEAD | Now |
| --- | --- | --- |
| `world.Service.UpdateCharacterDescription` | `char.Description = description` — bare field assignment | `char.SetDescription(description)`, error wrapped as `CodeCharacterInvalid` |
| `worldMutator.updateCharacter` | forwards `char` verbatim | unchanged |
| `CharacterRepository.Update` | binds `char.Description` into the UPDATE | unchanged |
| `ValidateDescription` | reachable only from `Character.Validate` and `Character.SetDescription`, **neither on this path** | reached via the setter |
| `Character.SetDescription` | **zero production callers**, despite its own doc comment saying application code must use it | one |

So ROADMAP criterion 4 ("over-cap input rejected server-side") was **unmet at HEAD for IDENT-02a**, and no amount of facade work would have met it — the plan explicitly forbids the handler from re-implementing the check. The fix follows `Service.UpdateLocation`'s shape exactly, including its **order**: validation after `checkAccess` and after the read, so an unauthorized caller cannot use the command as a rules oracle.

The narrow setter was used rather than `char.Validate()` deliberately: `Validate()` also re-checks the **stored** name through `ValidateCharacterName`, so a legacy or guest-provisioned name would make an unrelated description edit fail.

### One seam, two commands, no new production type

`characterAccessWorldMutator` carries exactly `UpdateCharacterDescription` and `UpdateCharacterProfileAttributes`. `*world.Service` satisfies both, and it is the **same value** `characterAccessWorldReader` already receives — so the write surface cost one constructor argument and zero new construction sites.

Widening `characterAccessWorldReader` instead would have needed no wiring at all, which is exactly why it was rejected: the type's name and its 04-01 contract both say READER.

**The D-79 compile fence is intact and, if anything, more load-bearing.** The fence is the *absence* of `PropertyRepository.ListByParent` / `PropertyReader.ListByParent` from every interface the facade holds, so it is unaffected by how many interfaces there are. `rg -n 'characterRepo|CharacterRepository|propertyRepo' internal/grpc/characteraccess_write.go` returns nothing: the profile write reaches `entity_properties` rows only through 04-09's domain command.

### The two guards are NOT the same guard, and the plan is honest about it

| Path | Domain signature | Guard | Property |
| --- | --- | --- | --- |
| `UpdateCharacterProfile` | takes `expectedVersion int` (04-09) | caller's version threaded into a version-predicated UPDATE | **genuine CAS** — exactly one of two writers succeeds (W13) |
| `UpdateCharacterDescription` | takes **no** version; re-reads and guards on its own `char.Version` | facade compares the caller's version against the one it read | **TOCTOU narrowing** — a stale form is caught, a writer inside the window is not |

The narrowing is pinned by `TestUpdateCharacterDescriptionDoesNotDetectAWriterInsideTheReadToReReadWindow`, which asserts the **last-write-wins** outcome rather than a guarantee the code does not have. Option (b) — extending the domain signature — is filed as **issue #4956** and cited in both the handler doc comment and the test. There is deliberately **no** two-writer spec for the description RPC.

### Exactly one layer owns each field

| Field family | Cap enforced by | Why |
| --- | --- | --- |
| the twelve `profile.*` prose fields | the FACADE handler | D-82; 04-09's domain command deliberately does not cap them |
| `characters.description` | the DOMAIN | it has a shipped validator; a facade copy would be the divergent-cap pair 04-09 warns against |

The facade's own check **delegates** the shared rules: `validateProfileValue` calls `world.ValidateDescription` (UTF-8, control characters, the long cap) and adds only the narrower 100-byte cap on top. A facade/domain disagreement about "is `\r` a control character" is therefore impossible by construction rather than by two implementations agreeing.

### The colocation trap 04-05 flagged was real, and was handled rather than discovered

04-05 deviation 6 records that a **public**-visibility row is unreadable by its own character's subject when that character has **no location**, because `seed:property-public-read` is colocation-gated. 04-09 creates every profile row with `Visibility: "public"`. The integration fixture therefore places the character in a location, and the file says so at the top. Without it, W9's post-write projection assertion (`resp.GetCharacter().GetProfile()` contains `profile.pronouns`) would have been vacuously empty and looked like a facade bug.

## Acceptance-criteria corrections

One of this plan's acceptance greps was defective as written. It was **corrected rather than satisfied by reshaping the artifact**, per the phase's standing protocol.

| Criterion as written | Why it is defective | Corrected form | Result |
| --- | --- | --- | --- |
| Task 1: "`rg -n 'HandleCommand\|sendCommand' internal/grpc/characteraccess_write.go internal/web/character_handlers.go` returns no match" | The new file's own header comment states the prohibition it is being checked for — "the command path (`HandleCommand` / `sendCommand`) is reserved for human conversational verbs" — so the criterion goes RED exactly when the code is correct AND well documented. This is the same rationale-comment vector the plan's own 12-count criterion added comment-filtering to close; that filtering was simply not applied to this bullet. | `rg -n 'HandleCommand\|sendCommand' internal/grpc/characteraccess_write.go internal/web/character_handlers.go \| rg -v ':\s*//'` | **no match** — no CODE line in either file references the command dispatcher |

Every other criterion passed as written, including the two that were most at risk: the comment-filtered `"profile.` count is exactly **12**, and the comment-filtered `s.worldMutator.` count is exactly **2**. Neither file contains a trailing `//` comment or a `/* */` block carrying either literal, so both counts are honest rather than accidentally green (`rg -n '.\s+//.*"profile\.|/\*'` and its `s.worldMutator.` twin both return no match).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `NewCharacterAccessServer` has SIX call sites, not the two the plan names**

- **Found during:** Task 1
- **Issue:** The plan's `files_modified` names `cmd/holomush/sub_grpc.go` and `internal/testsupport/integrationtest/harness.go`, and its acceptance criterion says "both call sites". Four more exist and the tree does not compile without them — the same discovery 04-05 recorded as its own deviation 1: `internal/grpc/characteraccess_owner_test.go:137`, `internal/grpc/characteraccess_profile_test.go` (×2), `test/integration/access/character_profile_read_test.go:122`.
- **Fix:** The two production-shaped sites pass `worldSvc` / `s.worldSvc` — the same `*world.Service` they already pass for the reader, exactly as the plan requires. The three `internal/grpc` test sites pass `&recordingWorldMutator{t: t, failOnCall: true}`, a double that **fails the test on call**: the public-read and owner-read paths must never mutate, so that property is now enforced continuously by every spec in those files rather than by one spec. The integration read spec passes the real `worldSvc`.
- **Files modified:** the four above
- **Commit:** `77cf30388`

**2. [Rule 3 - Blocking] `govet shadow` and `gocritic sloppyReassign` disagree about `if err = …`**

- **Found during:** Tasks 1 and 2
- **Issue:** `if err := f(); err != nil` inside a function that already has an `err` trips `govet shadow`; rewriting it to `if err = f()` then trips `gocritic sloppyReassign`. Neither can be satisfied by the other's fix.
- **Fix:** Distinct names at each site (`versionErr`, `valueErr`, `writeErr`, `profileErr`, `setErr`), which satisfies both and reads better at the call site than either. No `//nolint` was added — both findings were real for the commits they appeared in.
- **Commits:** `77cf30388`, `0511f7873`

### Scope / design decisions

**3. `update_mask` paths are the §7.2 property names, not the bare proto field names**

The plan's action says "the twelve section 7.2 profile field paths"; the proto's fields are flat (`pronouns`, `concept`, …), which would have suggested bare names. **01-SPEC §9.5 rule 2 settles it by example** — *"`profile` MUST NOT reach `profile.rp_preferences`"* — and so does the plan's own behavior list ("an entry naming only the container is rejected"), which presupposes a container. Paths are therefore `profile.pronouns` etc., identical to the domain's `profileAttributeNames` keys, and the path→attribute mapping is the identity.

**4. The allowlist map's value carries the accessor and the cap, rather than being `map[string]struct{}`**

The plan says to copy `updateSceneMaskablePaths`' shape. Its shape is *a package-level map keyed by the exact path string* — kept. But the scene facade forwards its mask verbatim downstream, whereas this handler must **read a value per path**, which needs a path→getter mapping. A `map[string]struct{}` plus a parallel switch would write the twelve names **twice** (a divergence waiting to happen: a path with no accessor, or an accessor with no entry, would both be possible) and would also make the plan's own "exactly 12 occurrences of `\"profile.`" criterion unsatisfiable. The value type `profileMaskField{maxBytes, value}` keeps each name written exactly once and lets `TestUpdateCharacterProfileMaskablePathsAreExactlyTheTwelve` assert that every entry has both.

**5. `world.CodeCharacterInvalid` is an exported constant, unlike its siblings**

The plan says to wrap with `oops.Code("CHARACTER_INVALID")`, matching `LOCATION_INVALID` / `OBJECT_INVALID`, which are bare literals. Those are package-internal; this one is **not** — the character-access facade matches on it to render `codes.InvalidArgument`, and a cross-package code with two spellings drifts. That is precisely why `CodeConcurrentEdit` next to it is a constant. Confirmed first that `CHARACTER_INVALID` is unused in the tree (`CHARACTER_INVALID_NAME` is a different, auth-side code).

**6. The two-writer concurrency spec is issued SEQUENTIALLY**

The plan says to point "the existing two-replica concurrency shape" at the profile RPC. Both writers carry the **same** `expected_version` — which is the shape a CAS decides — and are issued in sequence: the first commit moves the row's version, so the second's predicate no longer matches. This asserts exactly the required property (one success, one `Aborted`, and the loser's value **absent from the row**) while being deterministic rather than racing two goroutines for a guarantee that does not depend on timing.

**7. `requirements mark-complete` half-wrote, as forecast**

`IDENT-02` and `IDENT-02a` are **genuinely delivered** — the caps are server-enforced, byte-measured, reuse `world.MaxNameLength` / `world.MaxDescriptionLength`, and are proven at the integration tier against the real service with the stored row asserted unchanged. This is the first plan in the phase to claim them; 04-02, 04-03, 04-04, 04-05 and 04-09 all correctly declined because none shipped a mutation surface or a cap.

The tool returned `updated: true` with `table_unmatched: [IDENT-02, IDENT-02a]` and a `write_set` showing `checkbox: applied: true` but `traceability: applied: false` — leaving `REQUIREMENTS.md` internally inconsistent (boxes ticked, rows still `Pending`). Verified with `git diff` (2 lines changed, both checkboxes). The two traceability cells were then filled **by hand in the exact shape the tool writes elsewhere** (`| IDENT-02 | Phase 4 | Complete |`, matching the `IDENT-04` / `PROFILE-03` rows) — a value-fill in an existing row, not invented structure.

`PROFILE-04`'s pre-existing checkbox/row split was left alone as instructed.

---

**Total deviations:** 2 auto-fixed (both Rule 3) + 5 recorded decisions. No scope creep; the one scope ADDITION (the #4954 domain fix) was mandated by the plan.

## Verification

| Gate | Result |
| --- | --- |
| `task test -- -run 'UpdateCharacterProfile\|Mask\|Cap' ./internal/grpc/` | green, 39 tests |
| `task test -- -run 'UpdateCharacterDescription' ./internal/grpc/` | green |
| `task test -- -run 'TestWorldService_UpdateCharacterDescription\|TestServiceHoldsOnlyReaderViews' ./internal/world/` | green, 13 tests |
| `task test -- ./internal/world/` | green |
| `task test -- ./internal/plugin/... ./internal/command/...` | green, 3494 tests — the compile-and-fixture check on #4954's two couplings |
| `task test -- ./internal/grpc/` | green, 638 tests |
| `task test` (whole repo) | **exit 0**, 11521 tests, 4 skipped |
| `task test:int -- ./test/integration/access/...` | green |
| `task test:int` (whole repo) | **exit 0**, 11991 tests, 7 skipped |
| `task lint` | exit 0 |
| `task build` | exit 0 |

**The known #4955 rate-limiter flake did not fire.** The full `task test:int` run had zero failures, so nothing had to be excused.

## Known Stubs

None. Every surface this plan declares is wired and exercised; no test is skipped and every `<verify>` was run.

Two absences are deliberate and stated at their sites:

- **No facade-side description cap.** `UpdateCharacterDescription`'s body performs no length or encoding check — the domain owns that rule (Task 2). A second check here would be the divergent-cap defect, not extra safety.
- **No two-writer concurrency spec for the description RPC.** That property is FALSE on that path; asserting it would either flake or pass for the wrong reason. The documenting test asserts the true (weaker) behavior instead.

## Issues Encountered

- **A missing handler does not fail to compile.** `CharacterAccessServer` embeds `UnimplementedCharacterAccessServiceServer`, so `srv.UpdateCharacterDescription(...)` compiled before the handler existed and would have returned `codes.Unimplemented` at runtime. The Task 3 RED was therefore a single `undefined: characterValueInvalidMessage` rather than a missing-method error. Specs asserting concrete status codes still caught it; a spec asserting only "an error occurred" would not have.
- **Ginkgo + `gotestsum --format pkgname` reports a whole suite as `1 test`,** which makes "the specs are green" indistinguishable from "the specs did not run". Hence the deliberate non-vacuity probe recorded above.

## Threat Flags

None. Every surface this plan adds is inside its own threat register, and the one register entry marked **unmitigated at HEAD** is now closed:

| Threat | Disposition | Where |
| --- | --- | --- |
| T-04-05 mask-driven unadvertised mutation | mitigate | closed twelve-path exact-string allowlist; container prefix rejected (W11) |
| T-04-19 absent/zero expected_version | mitigate | rejected at the boundary on both RPCs, asserted with a fail-on-call double |
| T-04-20 concurrent PROFILE mutation | mitigate | caller's version threaded into the domain CAS; W13 asserts one success / one Aborted |
| T-04-29 concurrent DESCRIPTION mutation | **accept** (as planned) | narrowing implemented, limit pinned by a documenting test, option (b) filed as #4956 |
| T-04-06 structural write through the command parser | mitigate | typed RPCs only; no code line in the facade or the web character handlers references the dispatcher |
| T-04-30 mutation-surface ownership denial | mitigate | uniform `PermissionDenied` across all three causes, asserted as a message equality |
| T-04-31 infra failure masked as a denial | mitigate | `ownedCharacterForMutation` propagates `codes.Internal` verbatim; pinned by its own test |
| T-04-21 malformed `profile.*` prose | mitigate | byte caps + delegated UTF-8/control rules; boundary asserted at the cap and one byte past |
| T-04-33 malformed `characters.description` | **mitigate (was unmitigated at HEAD)** | issue #4954 closed in the domain; behavioral RED captured; boundary asserted at the integration tier with the row unchanged |
| T-04-22 state change without its envelope | mitigate | both writes route through the same-transaction outbox seam; W1 and W9 assert exactly one envelope |

## Notes for the Phase Gate

- **04-08 Task 2 MUST teach its ownership predicate the name `ownedCharacterForMutation`.** Both write RPCs reference `s.ownedCharacterForMutation`, never `s.ownedCharacter`, so a census that knows only the base name is either permanently RED against correct code or green only by dropping the phase's two write RPCs out of criterion 1's set-equality proof. The wrapper is declared on `*CharacterAccessServer` in `internal/grpc/characteraccess_write.go` and its body calls `s.ownedCharacter(` — both verified.
- **04-07 adds handlers over a constructor that now takes SIX arguments** and a facade that holds three narrow interfaces (`characterAccessWorldReader`, `characterAccessWorldMutator`, `characterAccessProfileVisibility`).
- **Issue #4956 is open** and is the follow-up for the description path's TOCTOU window. When it lands, `TestUpdateCharacterDescriptionDoesNotDetectAWriterInsideTheReadToReReadWindow` must be **replaced**, not deleted.
- **Issue #4954 was a genuine pre-existing codebase bug**, not a plan defect, and it is closed by `0511f7873`. It should be closed on GitHub once this phase ships.
- **The `AppSchemaVersion` bump and the `character_profile_update` kind are 04-09's**, not this plan's; this plan only calls the command.

## User Setup Required

None — no external service configuration required.

## Self-Check: PASSED

- `internal/grpc/characteraccess_write.go` — FOUND
- `internal/grpc/characteraccess_write_test.go` — FOUND
- `test/integration/access/character_write_test.go` — FOUND
- commit `77cf30388` — FOUND in `git log --oneline --all`
- commit `0511f7873` — FOUND
- commit `d09712b04` — FOUND
- `task test`, `task test:int`, `task lint`, `task build` all exit 0 on the committed tree
