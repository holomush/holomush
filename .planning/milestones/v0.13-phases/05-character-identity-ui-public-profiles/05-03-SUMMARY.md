---
phase: 05-character-identity-ui-public-profiles
plan: 03
subsystem: api
tags: [protobuf, connectrpc, grpc, abac, charname, unicode, sveltekit]

requires:
  - phase: 05-character-identity-ui-public-profiles
    plan: 01
    provides: web/src/lib/characters/client.ts, the singleton wrapper module the create wrapper joins
  - phase: 04-shared-facade-helpers-characteraccessservice
    provides: playerGate.resolveAndGate, ownerMutationResponse, projectOwner, updateCharacterProfileMaskablePaths and its byte caps
  - phase: 02-abac-schema-vocabulary
    provides: internal/charname (Normalize, Gate.Admit, MixedScript) and the characters_normalized_name_key unique index
provides:
  - "holomush.characteraccess.v1.CharacterAccessService.CreateCharacter — the structured identity-card create returning OwnCharacter"
  - "internal/grpc/characteraccess_create.go — the handler, its two-transaction seeding and its closed code→status mapping"
  - "characterAccessCreator, the ninth required constructor dependency on CharacterAccessServer"
  - "three new authored player-facing constants in internal/grpc/auth_errors.go (blank name, no-visible-codepoint name, unassigned script)"
  - "the reshaped holomush.web.v1.WebCreateCharacterRequest/Response — the name-only success/error_message shape is gone"
  - "(*web.Handler).WebCreateCharacter relocated to internal/web/character_handlers.go and repointed at the facade"
  - "(*grpcclient.Client).CreateOwnCharacter plus cmd/holomush's characterAccessGateway adapter — the resolution of the CreateCharacter Go-identifier collision"
  - "createCharacter in web/src/lib/characters/client.ts"
affects: [05-06, 05-07, 05-08]

actuals:
  tokens: 72000
  tasks: 3
  commits: 4

tech-stack:
  added: []
  patterns:
    - "A best-effort second transaction is expressed as a func with NO error return, so the swallow is the type signature rather than a comment a later reader can 'fix'"
    - "A code→status mapping splits into a pure classifier (returns code, status, message) and a thin logging wrapper, so the whole table is exercisable as data"
    - "One cap table: a create surface reads its byte caps out of the mask surface's allowlist by path, with a meta-test asserting every seed path is a member"
    - "A Go-identifier collision between two services declaring the same RPC name is resolved at the single client that reaches both, with a three-line adapter — never by renaming an RPC"

key-files:
  created:
    - internal/grpc/characteraccess_create.go
    - internal/grpc/characteraccess_create_test.go
    - .planning/phases/05-character-identity-ui-public-profiles/deferred-items.md
  modified:
    - api/proto/holomush/characteraccess/v1/characteraccess.proto
    - api/proto/holomush/web/v1/web.proto
    - internal/grpc/characteraccess_service.go
    - internal/grpc/auth_errors.go
    - internal/web/character_handlers.go
    - internal/web/auth_handlers.go
    - internal/web/handler.go
    - internal/grpcclient/client.go
    - cmd/holomush/deps.go
    - cmd/holomush/gateway.go
    - internal/testsupport/integrationtest/harness.go
    - test/meta/character_rpc_census_test.go
    - test/meta/characteraccess_routing_census_test.go
    - web/src/lib/characters/client.ts
    - web/src/routes/(authed)/characters/+page.svelte

key-decisions:
  - "Q1 was ratified as option-a by the maintainer BEFORE this executor was dispatched: two transactions, the create authoritative. CharacterGenesisService is not widened; a profile-seeding failure is logged with the character id and the attempted paths and the RPC still returns success with the un-set keys absent. The checkpoint was therefore not re-presented."
  - "Profile-value validation runs BEFORE the create rather than after it (a deliberate deviation from the plan's ordering). An oversized value is a fixable mistake, and refusing it after the name is reserved would put it on the wrong side of Q1's swallow — the player would be told the create worked and then hit CHARACTER_NAME_TAKEN on their own character when they retried. That is Q1's own argument against option-c."
  - "seedCreatedProfile returns nothing at all. The return type is the contract: an error return would invite exactly the 'fix' the ratified decision forbids."
  - "The default arm of the code→status switch returns the IDENTICAL (Internal, msgCharacterCreateFailed) pair the CHARACTER_CREATE_FAILED arm does. That is what makes oops's deepest-code resolution (#4902) invisible to the caller, and it is pinned by a test row rather than asserted in a comment."
  - "The three CHARACTER_INVALID_NAME messages are chosen by asking the *syntax.ValidationError chain and then strings.TrimSpace of the SUBMITTED name — a decision made on the handler's own input, never by matching an error string."
  - "The CreateCharacter Go-identifier collision (CoreService and CharacterAccessService both declare it) is resolved in the gateway client, not in the web layer. (*grpcclient.Client).CreateOwnCharacter carries the facade call and a three-line cmd/holomush adapter re-exposes it under the interface name. Bending internal/web instead would have broken the routing census's second conjunct, which is exactly what proves the proxy reaches the facade rather than the core RPC it replaced. No proto method name changed."
  - "The WebCreateCharacter census row MOVED out of characterNameReachableRPCs() into characterReadSurfaceInventory() rather than being duplicated (Q6). Both paths add the same key, so a duplicate would leave the comparison green while parking a self-certifying member in a class whose doc says it exists only for surfaces a predicate cannot reach."

patterns-established:
  - "Two REDs, two commits: a descriptor-driven census row lands with the proto, an AST-driven one lands with the handler — the plan's sequencing correction, confirmed in practice"
  - "A create-path refusal table is asserted at the WIRE (status.Code plus status.Convert(err).Message()), with a separate negative spec proving no message contains any internal code string"
  - "The paired positive control: a guest-denial spec and the same fixture's non-guest success are one test, so a denial that passed because the fixture was broken cannot look like a working gate"
  - "An invisible-codepoint test input is written as \\u escapes, never raw, so the source line does not render as an empty string in every editor and diff"

requirements-completed: [IDENT-01]

coverage:
  - id: D1
    description: "A player supplies name, pronouns, concept, species, age and faction in ONE call and the response carries the created character in OwnCharacter shape, including the display name the SERVER stored"
    requirement: IDENT-01
    verification:
      - kind: unit
        ref: "internal/grpc/characteraccess_create_test.go#TestCreateCharacterSeatsTheWholeIdentityCardAndEchoesTheServerStoredName"
        status: pass
      - kind: unit
        ref: "web/src/lib/characters/client.test.ts#returns the SERVER-stored display name from createCharacter, not the submitted string"
        status: pass
      - kind: manual
        ref: "submit a create through a browser once /characters/new exists (plan 05-06) and confirm the echoed name is the normalized one"
        status: pending
    human_judgment: true
    rationale: "Every layer is asserted in isolation and the D-88 echo is pinned on both sides of the boundary, but no submission surface exists yet — /characters/new is plan 05-06. The end-to-end path cannot be exercised by a human until it does."
  - id: D2
    description: "A blank name and an invisible-codepoint-only name are both refused codes.InvalidArgument with DIFFERENT authored copy"
    requirement: IDENT-01
    verification:
      - kind: unit
        ref: "internal/grpc/characteraccess_create_test.go#TestCreateCharacterSeparatesABlankNameFromAnInvisibleOnlyName — asserts both messages AND that they differ"
        status: pass
      - kind: unit
        ref: "internal/grpc/characteraccess_create_test.go#TestCreateCharacterRendersASyntaxRefusalWithTheSyntaxCopy — the third arm"
        status: pass
    human_judgment: false
  - id: D3
    description: "Name admission is on RUNE count and the uniqueness key is the §6.1.1 normalized form; the five profile values are capped in BYTES at world.MaxNameLength by the SAME table the mask surface uses"
    requirement: IDENT-01
    verification:
      - kind: unit
        ref: "internal/grpc/characteraccess_create_test.go#TestEveryCreateProfileSeedPathIsAMaskablePath — the cap-table linkage, so a seed with no entry cannot read a zero cap silently"
        status: pass
      - kind: unit
        ref: "internal/grpc/characteraccess_create_test.go#TestCreateCharacterRefusesAnOversizedProfileValueBeforeSeatingAnything"
        status: pass
      - kind: other
        ref: "the rune/normalization rules are internal/charname's, unchanged and reached verbatim through auth.CharacterService.CreateBound — no second normalizer exists (D-88)"
        status: pass
    human_judgment: false
  - id: D4
    description: "Creating the same name twice creates exactly one character: the second call is codes.AlreadyExists whether the pre-check or the 23505 handler caught it"
    requirement: IDENT-01
    verification:
      - kind: unit
        ref: "internal/grpc/characteraccess_create_test.go#TestCreateCharacterRendersBothNameTakenProducersIdentically"
        status: pass
      - kind: manual
        ref: "create the same name twice against a live grid and confirm one character exists"
        status: pending
    human_judgment: true
    rationale: "The facade renders both producers identically, which is what this plan owns. That the DATABASE admits exactly one row under a race is characters_normalized_name_key's guarantee, established in phase 02; no test added here exercises a concurrent create."
  - id: D5
    description: "CreateCharacter runs in TWO transactions; a failed profile write returns SUCCESS with the created character and the un-set fields absent, logged with the character id and the attempted paths"
    requirement: IDENT-01
    verification:
      - kind: unit
        ref: "internal/grpc/characteraccess_create_test.go#TestCreateCharacterReturnsSuccessWhenTheProfileSeedingWriteFails"
        status: pass
      - kind: unit
        ref: "internal/grpc/characteraccess_create_test.go#TestCreateCharacterWithOnlyANameMakesNoSecondDomainCall — the mutator fails the test on call"
        status: pass
    human_judgment: false
  - id: D6
    description: "CreateCharacter carries no expected_version field, calls requireGuardedVersion nowhere, reaches the shared guest gate, and is NOT a member of the ownership census set"
    requirement: IDENT-01
    verification:
      - kind: unit
        ref: "test/meta/characteraccess_routing_census_test.go#TestCharacterAccessRoutingCensusGuestGate (member) and #TestCharacterAccessRoutingCensusOwnership (non-member, by set equality)"
        status: pass
      - kind: unit
        ref: "acceptance grep — requireGuardedVersion|ownedCharacter in characteraccess_create.go, comment lines filtered: no match"
        status: pass
      - kind: unit
        ref: "acceptance grep — expected_version inside CreateCharacterRequest: no match"
        status: pass
    human_judgment: false
  - id: D7
    description: "No returned status message is an interpolation of a wrapped error; every one is an authored constant selected by a closed switch"
    requirement: IDENT-01
    verification:
      - kind: unit
        ref: "internal/grpc/characteraccess_create_test.go#TestCreateCharacterNeverReturnsAnInternalCodeStringOnTheWire — 13 codes × 13 negative assertions each"
        status: pass
      - kind: unit
        ref: "internal/grpc/characteraccess_create_test.go#TestCreateCharacterMapsEveryCreatePathCodeToItsPinnedStatusAndAuthoredMessage — 10 rows including the wrapped-inner-oops case"
        status: pass
      - kind: unit
        ref: "acceptance grep — err.Error()/%v/, err) on any status line in characteraccess_create.go: 0"
        status: pass
    human_judgment: false
  - id: D8
    description: "holomush.core.v1.CoreService.CreateCharacter still exists, still returns a bare character_name scalar, and keeps its name-reachable census row — the telnet CREATE verb still drives it"
    requirement: IDENT-01
    verification:
      - kind: unit
        ref: "test/meta/character_rpc_census_test.go — CoreService.CreateCharacter remains in characterNameReachableRPCs(), asserted EXACT"
        status: pass
      - kind: unit
        ref: "git status --porcelain internal/telnet/ — empty across all four commits; task build compiles it"
        status: pass
    human_judgment: false
  - id: D9
    description: "WebCreateCharacter proxies the facade, forwards the header token and all six values verbatim, and passes a facade refusal through as a status rather than a body field"
    requirement: IDENT-01
    verification:
      - kind: unit
        ref: "internal/web/character_handlers_test.go#TestWebCreateCharacterForwardsTheHeaderTokenAndAllSixSubmittedValues"
        status: pass
      - kind: unit
        ref: "internal/web/character_handlers_test.go#TestWebCreateCharacterPassesAFacadeErrorThroughAsIs and #TestWebCreateCharacterReturnsUnimplementedWhenClientAbsent"
        status: pass
      - kind: unit
        ref: "test/meta/characteraccess_routing_census_test.go#TestCharacterAccessRoutingCensusWebProxies — both conjuncts, by set equality"
        status: pass
    human_judgment: false

duration: 78min
completed: 2026-08-12
status: complete
---

# Phase 05 Plan 03: CreateCharacter — the structured identity card Summary

**Character creation moved off the name-only core RPC onto the character facade: one call carrying name plus five prose values, a closed twelve-code refusal mapping where every wire message is an authored constant, and both proto surfaces reshaped as one breaking change.**

## Performance

- **Duration:** ~78 min (resumed run; the prior crashed run's Task-1 first half is commit `45b122710`)
- **Tasks:** 3 of 3 (the plan's `checkpoint:decision` was pre-ratified — see below)
- **Commits:** 4 (1 pre-existing + 3 this run)

## Crash-resume boundary

This plan was executed in two sessions. The first crashed mid-Task-1.

| State | Content |
| --- | --- |
| Landed before this run (`45b122710`) | the `characteraccess.proto` CreateCharacter declarations, all generated Go/ConnectRPC/TypeScript, the `characterAccessCreator` interface + ninth constructor parameter + every call site, the harness's real `auth.NewCharacterService`, and the DESCRIPTOR-driven census row in `character_rpc_census_test.go` |
| Left uncommitted for this run | `test/meta/characteraccess_routing_census_test.go`'s `characterGuestGateRPCs()` edit — a demonstrated RED, deliberately held back because the AST-driven census's row belongs in the same commit as the handler that turns it green |
| Owed and delivered here | the handler + mapping + tests (Task 1 remainder), the web reshape (Task 2), the client wrapper (Task 3) |

**Everything the crash-recovery brief asserted matched the disk.** One count did
not: the brief and the plan both say `rg -c 'NewCharacterAccessServer' --type go`
finds **eleven** call sites. On disk there are **twelve** before this plan's own
test file (the `test/integration/access/` tree has four, not three) and thirteen
after. The count is cosmetic — the load-bearing property, that no site was
missed, is proven by `task build` and `task test:int` compiling.

## Checkpoint: pre-ratified, not re-presented

The plan opens with `<task type="checkpoint:decision" gate="blocking">` on Q1
(the create transaction shape). **The maintainer ratified it as `option-a` —
two transactions, the create authoritative — before dispatching this executor.**
It was therefore implemented directly and not surfaced again, per the dispatch
instruction. What that ruling means in code:

- `CharacterGenesisService` is **not** widened.
- On a second-write failure the RPC returns **success** with the created
  `OwnCharacter`, the un-set keys simply absent from its `profile` map, and the
  failure logged with the character id and the attempted paths.
- Fields the client left empty are not written at all, so a name-only create
  makes no second call — asserted by a mutator double that fails the test on
  contact.

## Task Commits

| Task | Commit | What landed |
| --- | --- | --- |
| 1 (first half, prior run) | `45b122710` | proto + generated code + constructor fan-out + descriptor census row |
| 1 (remainder) | `a0c1917ac` | `characteraccess_create.go`, the three authored constants, the routing-census row, 45 test cases |
| 2 | `0c8c3ca26` | web proto reshape + regenerated bindings, handler moved and repointed, the Go-identifier collision fix, two census edits, relocated tests, roster page |
| 3 | `1d5de9264` | `createCharacter` wrapper + two vitest cases |

## RED observations

Both census gates were observed RED before their rows, and the two REDs are in
opposite directions — which is the plan's sequencing correction, confirmed.

**1. The AST-driven routing census (Task 1's row).** With `"CreateCharacter"`
added to `characterGuestGateRPCs()` and no handler body on disk,
`task test -- ./test/meta/` exited 201 with two failures:

- `TestCharacterAccessRoutingCensusGuestGate` — `CreateCharacter` missing from
  the set of facade methods reaching `s.resolveAndGate`;
- `TestCharacterAccessRoutingCensusAudiencePartition` — `CreateCharacter` named
  as an exported facade RPC carrying no audience classification.

(Captured in the prior session and carried in the dispatch brief; the row was
held uncommitted precisely so it could land with the handler.)

**2. The descriptor-driven census (Task 2's move).** With
`holomush.web.v1.WebService.WebCreateCharacter` removed from
`characterNameReachableRPCs()` and added to `characterReadSurfaceInventory()`,
but **before** `task proto` regenerated the descriptors:

```
=== FAIL: test/meta TestCharacterReturningRPCCensusMatchesTheReadSurfaceInventory
    extra (derived but not expected): [];
    missing (expected but not derived): [holomush.web.v1.WebService.WebCreateCharacter]
DONE 187 tests, 1 failure
```

The old descriptors still declared a bare `character_name` scalar, so the §3.1
type predicate could not reach it — exactly the condition that made the
hand-listed name-reachable row necessary in the first place. Regenerating turned
it green, which is the evidence that the move (not a duplication) was correct.

## Verification evidence

All run inline in this worktree, judged by exit code:

| Gate | Result |
| --- | --- |
| `task test` | **0** — 11636 tests, 4 skipped |
| `task test:int -- ./test/integration/access/` | **0** |
| `task build` | **0** |
| `task lint` | **0** |
| `task lint:proto` | **0** |
| `task fmt:check` | **0** |
| `cd web && pnpm check` | 0 errors, 6 pre-existing warnings |
| `cd web && pnpm test:unit` | `Test Files 50 passed (50)` / `Tests 489 passed (489)` |
| `git status --porcelain pkg/proto web/src/lib/connect` | empty after regeneration |

Acceptance greps, all satisfied:

- `rg -c 'rpc CreateCharacter'` → **1** in `characteraccess.proto`, **1** in `core.proto`
- `requireGuardedVersion|ownedCharacter` in `characteraccess_create.go`, comment lines filtered → **no match**
- `expected_version` inside `CreateCharacterRequest` → **no match**
- status lines interpolating `err.Error()` / `%v` / `, err)` in the create handler → **0**
- `success|error_message` inside `WebCreateCharacterResponse` → **no match**
- `WebCreateCharacter` in `internal/web/auth_handlers.go` → **no match**; `func (h *Handler) WebCreateCharacter` in `character_handlers.go` → **1**
- `webCreateCharacter|resp.success` in the roster page → **no match** (the one `resp.success` left belongs to `webSelectCharacter`, untouched)
- `try {|catch` and `playerSessionToken` in `web/src/lib/characters/client.ts` → **no match**
- `git status --porcelain internal/telnet/` → **empty**

## Files Created/Modified

**Created:** `internal/grpc/characteraccess_create.go`,
`internal/grpc/characteraccess_create_test.go`, this SUMMARY, and
`deferred-items.md`.

**Modified:** both protos and every generated artifact from them;
`internal/grpc/{characteraccess_service.go,auth_errors.go}`;
`internal/web/{character_handlers.go,auth_handlers.go,handler.go}` and four test
files; `internal/grpcclient/client.go`; `cmd/holomush/{deps.go,gateway.go}` and
`deps_test.go`; both census files; `web/src/lib/characters/client.ts` + its test;
`web/src/routes/(authed)/characters/+page.svelte`.

## Decisions Made

Beyond the pre-ratified Q1, four decisions were made in flight. Two are genuine
deviations and are recorded under **Deviations** below; two are choices the plan
left to the implementer:

- **`ownerMutationResponse` is reused for the read-back** rather than a new
  helper. Its doc says every caller reaches it only after a domain write
  committed, which is true of a create, and it already returns `codes.Internal`
  with the right reasoning for a post-commit read failure. Reusing it also keeps
  `ownedCharacter` out of the create file, which the acceptance grep requires.
- **`classifyCharacterCreateError` is split out as a pure function.** The
  mapping table is the security-relevant artifact; separating it from the
  logging wrapper makes it exercisable as data, and `gocritic` then asked for
  named results, which read better anyway.

## Deviations from Plan

### Auto-fixed / re-ordered

**1. [Rule 2 — missing critical ordering] Profile-value validation moved BEFORE the create call**

- **Found during:** Task 1, writing the handler.
- **Issue:** the plan says to collect and validate the five values "on success",
  i.e. after `CreateBound` returns. That places a *fixable, deterministic*
  refusal on the far side of Q1's log-and-swallow: an oversized `age` would seat
  a character, reserve its name, fail the seeding write, and report success with
  the field missing. The player's natural repair — resubmit with a shorter
  value — then collides with the character they just created and is told the
  name is taken. That is precisely the hazard Q1's own text uses to reject
  option-c.
- **Fix:** `collectCreateProfileSeeds` runs immediately after `resolveAndGate`
  and returns `codes.InvalidArgument` with `characterProfileFieldInvalidMessage`
  before any row exists. Authorization is still first, so the normative gate
  order is intact.
- **Files:** `internal/grpc/characteraccess_create.go`.
- **Pinned by:** `TestCreateCharacterRefusesAnOversizedProfileValueBeforeSeatingAnything`,
  whose fixture wires a creator that fails the test on contact.
- **Commit:** `a0c1917ac`.

**2. [Rule 3 — blocking] The `CreateCharacter` Go-identifier collision**

- **Found during:** Task 2, at `task build`.
- **Issue:** adding `CreateCharacter` to `web.CharacterAccessClient` made that
  interface unsatisfiable by `*grpcclient.Client`, which already carries
  `CreateCharacter(*corev1.CreateCharacterRequest)` for `web.CoreClient`. Two
  live services declare an RPC of the same name with different messages, and one
  Go type cannot hold both. The plan does not mention this.
- **Fix:** the facade call is spelled `(*Client).CreateOwnCharacter`;
  `GRPCClient` carries that name with a comment saying why; a three-line
  `characterAccessGateway` adapter in `cmd/holomush` embeds `GRPCClient` and
  shadows the promoted core method with one that forwards to
  `CreateOwnCharacter`. Neither proto method name changed.
- **Why not the other direction:** renaming the method on
  `web.CharacterAccessClient` would have broken the routing census's second
  conjunct (`bodyNamesMethod(body, "CreateCharacter")`), which is the assertion
  that proves the proxy reaches the character facade rather than the core RPC it
  replaced. The collision is an artifact of one Go client reaching two services,
  so it is resolved at that client.
- **Files:** `internal/grpcclient/client.go`, `cmd/holomush/deps.go`,
  `cmd/holomush/gateway.go`, `cmd/holomush/deps_test.go`.
- **Commit:** `0c8c3ca26`.

**3. [Rule 3 — blocking] Two test doubles needed the new interface method**

`stubCharacterAccessClient` (`internal/web/status_interceptor_test.go`) and
`mockGRPCClient` (`cmd/holomush/deps_test.go`) stopped satisfying their
interfaces. Both gained a one-line method. Commit `0c8c3ca26`.

**4. [Rule 1 — bug] `auth_handlers_logging_test.go`'s `WebCreateCharacter` row**

That table drives failures through `mockCoreClient`, which the repointed proxy no
longer consults — the row would have set an error on a client the handler never
calls and asserted a message nothing emits. Removed, with a comment naming where
the log line is now asserted instead. Commit `0c8c3ca26`.

### Not fixed here

**Eight Playwright specs drive the deleted inline create form.** Task 2 deletes
the roster's inline create card (the plan instructs this: it reads `resp.success`
off a shape that no longer exists), and the replacement page `/characters/new` is
plan **05-06**'s deliverable. Eight E2E specs locate `text=Create New Character`
and fill `input[name="characterName"]`; they will fail until 05-06 ships.

They are **not quarantined** — `.claude/rules/testing.md` scopes quarantine to
flakiness with no reproducible cause, and this cause is known and deterministic.
Recorded in full, with the file/line table and the closing plan, in
`.planning/phases/05-character-identity-ui-public-profiles/deferred-items.md`.

`E2E Test` is a required check protecting `main`, so **this MUST be closed
before the phase ships.** `task pr-prep` (fast lane) and `task test` are
unaffected and green today.

## Issues Encountered

- Three lint findings on first pass, all fixed properly rather than suppressed:
  `gocritic unnamedResult` (named the classifier's three results),
  `prealloc` (sized a slice), and `staticcheck ST1018` — a test input written as
  raw invisible codepoints. The last is worth naming: the source line rendered as
  an empty string in the editor and in `git diff`, which is the reviewer's own
  version of the bug the test covers. It is now `"\u200b\u200d\u2060"` with a
  comment saying why.

## User Setup Required

None.

## Next Phase Readiness

- **05-06** (`/characters/new`) has everything it needs: the RPC, the proxy, the
  `createCharacter` wrapper, and a facade whose refusals map onto UI-SPEC's error
  table one-for-one (`AlreadyExists` → taken, `InvalidArgument` → declined,
  `FailedPrecondition` → limit reached, `Unavailable` → corpus unavailable). It
  also inherits the E2E repair above.
- **05-05** files SPEC amendment 4 (Q3: `CHARACTER_LIMIT_REACHED` and
  `CHARACTER_NO_STARTING_LOCATION` → `codes.FailedPrecondition`), which this plan
  implements and pins by test.
- **05-07/05-08** inherit a roster page whose create affordance is already a link
  to `/characters/new`, so their rewrite no longer has to perform that deletion.

## Known Stubs

None. Every surface this plan declares is live and reached by a test.

The roster's create card links to `/characters/new`, which does not exist yet —
that is a **forward reference to plan 05-06**, not a stub: no placeholder
component, no disabled control, no "coming soon" copy was added. The link is the
final shape.

## Deferred / out of scope

- The eight Playwright specs above (plan 05-06).
- A concurrent-create race test. The facade renders both `CHARACTER_NAME_TAKEN`
  producers identically, which is what this plan owns; that the database admits
  exactly one row under a race is `characters_normalized_name_key`'s guarantee
  from phase 02 and is not re-proven here.
- `sanitizeAuthError` was deliberately **not** extended with the three new
  constants. It serves the core/telnet path, and the blank-vs-invisible split
  cannot be made there at all (it has no access to the submission). Widening it
  would have changed the telnet path in a plan whose prohibitions forbid touching
  it.

## Threat Flags

None. Every trust boundary this plan crosses is in the plan's own STRIDE
register, and no new network endpoint, auth path, file access pattern or schema
change was introduced beyond those.

Two register rows are worth marking as *actively* verified rather than merely
planned: **T-05-03-03** (the collision oracle) is asserted by a spec that puts a
colliding name in the gate error's context map and message and then asserts the
wire message contains neither; **T-05-03-04** (message leakage) by a 13×13
negative matrix over every internal code string.

## Tooling note

`requirements mark-complete` reported `table_unmatched` again — the checkboxes
tick but the traceability table does not update. Same defect plans 05-01 and
05-02 hit. Not hand-patched, per the dispatch instruction.

## Self-Check: PASSED

All four created files exist on disk and all four commit hashes resolve in
`git log --all`:

| Claim | Result |
| --- | --- |
| `internal/grpc/characteraccess_create.go` | FOUND |
| `internal/grpc/characteraccess_create_test.go` | FOUND |
| `.planning/…/05-03-SUMMARY.md` | FOUND |
| `.planning/…/deferred-items.md` | FOUND |
| `45b122710` `a0c1917ac` `0c8c3ca26` `1d5de9264` | all FOUND |
