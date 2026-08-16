---
phase: 05-character-identity-ui-public-profiles
plan: 01
subsystem: api
tags: [connectrpc, grpc, protobuf, postgres, sveltekit, svelte5, abac, ginkgo, vitest]

requires:
  - phase: 04-shared-facade-helpers-characteraccessservice
    provides: CharacterAccessServer, the shared playerGate (resolveAndGate + ownedCharacter), ownedCharacterForMutation, projectOwner, and both character census gates
  - phase: 02-abac-schema-vocabulary
    provides: characters.status lifecycle column and world.Selectable (INV-WORLD-5)
provides:
  - "holomush.characteraccess.v1.CharacterAccessService.SetDefaultCharacter and its ListMyCharactersResponse-shaped response"
  - "holomush.web.v1.WebService.WebSetDefaultCharacter proxy pair"
  - "auth.PlayerRepository.UpdateDefaultCharacter — the ONLY write path to players.default_character_id"
  - "CharacterAccessServer.ownerRoster — the single owner-roster projection ListMyCharacters and SetDefaultCharacter share"
  - "default_character_id on CheckPlayerSessionResponse and WebCheckSessionResponse"
  - "web/src/lib/characters/client.ts — the character flow layer every later Phase 5 web plan imports"
  - "the `Make default` control and `Default` badge on /characters"
affects: [05-02, 05-03, 05-04, 05-05, 05-06, 05-07, 05-08]

actuals:
  tokens: 70600
  tasks: 3
  commits: 3

tech-stack:
  added: []
  patterns:
    - "A typed facade RPC whose write targets a players row carries NO expected_version, and says so in the handler doc so the omission is not 'fixed'"
    - "One shared unexported roster projection (ownerRoster) rather than a second projectOwner loop per response shape"
    - "$lib/<domain>/client.ts as the single Connect entry point per web domain, copying $lib/scenes/client.ts"

key-files:
  created:
    - web/src/lib/characters/client.ts
    - web/src/lib/characters/client.test.ts
    - internal/web/character_handlers_test.go
  modified:
    - api/proto/holomush/characteraccess/v1/characteraccess.proto
    - api/proto/holomush/web/v1/web.proto
    - api/proto/holomush/core/v1/core.proto
    - internal/grpc/characteraccess_write.go
    - internal/grpc/characteraccess_owner.go
    - internal/grpc/auth_handlers.go
    - internal/auth/player.go
    - internal/auth/postgres/player_repo.go
    - internal/web/character_handlers.go
    - internal/web/auth_handlers.go
    - internal/grpcclient/client.go
    - cmd/holomush/deps.go
    - test/meta/character_rpc_census_test.go
    - test/meta/characteraccess_routing_census_test.go
    - test/integration/access/character_write_test.go
    - web/src/routes/(authed)/+layout.ts
    - web/src/routes/(authed)/characters/+page.svelte

key-decisions:
  - "SetDefaultCharacter carries no expected_version and never calls requireGuardedVersion — the target is a players row, which has no version column to predicate a CAS on (D-89)"
  - "A retired character is refused codes.FailedPrecondition with its own literal, not the uniform ownership message: ownership was already proven, so a lookup-shaped refusal would misreport a working rule and buy no opacity (Q4)"
  - "The non-playable predicate is world.Selectable, the ONE selectability predicate (INV-WORLD-5), not a comparison against `retired` — a fourth lifecycle value is excluded by the same code path"
  - "ListMyCharacters' projection loop was extracted into ownerRoster and shared, so D-90's 'never a struct literal' holds by construction rather than by two loops agreeing"
  - "default_character_id was ADDED to CheckPlayerSessionResponse and WebCheckSessionResponse — the plan's claim that WebCheckSessionResponse already carried it was wrong (web.proto:567 is WebAuthenticatePlayerResponse), and without it the initial Default badge has no server-side source"

patterns-established:
  - "Wire-level refusal assertions: status.Code(err) + status.Convert(err).Message(), never errutil.AssertErrorCode or oops.AsOops(err).Code() (both resolve the DEEPEST chain code under samber/oops v1.22.0, #4902)"
  - "Two collapsed refusals are compared IN ONE test body, so an edit giving either its own literal fails there rather than passing as two green specs"
  - "A denial spec ships with its permitted twin on the SAME fixture value (PORTAL-10 rule 2)"

requirements-completed: [IDENT-05]

coverage:
  - id: D1
    description: "A player's default character can be set end to end: players.default_character_id holds the target's ULID, written through a session-gated, ownership-checked typed RPC"
    requirement: IDENT-05
    verification:
      - kind: integration
        ref: "test/integration/access/character_write_test.go#D1: an owner's request moves players.default_character_id and answers with the whole roster"
        status: pass
      - kind: integration
        ref: "internal/auth/postgres/player_repo_test.go#TestPlayerRepositoryUpdateDefaultCharacterWritesOnlyThatColumn"
        status: pass
    human_judgment: false
  - id: D2
    description: "The write is narrow: password_hash, email, failed_attempts and locked_until are unchanged across it, and a repeat set is idempotent"
    requirement: IDENT-05
    verification:
      - kind: integration
        ref: "test/integration/access/character_write_test.go#D4: the write leaves password_hash, email, failed_attempts and locked_until untouched"
        status: pass
      - kind: integration
        ref: "test/integration/access/character_write_test.go#D2: setting the character that is already the default succeeds and leaves the column byte-identical"
        status: pass
    human_judgment: false
  - id: D3
    description: "Every refusal path is asserted at the wire with a paired positive control, and no returned message leaks an internal code string"
    requirement: IDENT-05
    verification:
      - kind: unit
        ref: "internal/grpc/characteraccess_write_test.go#TestSetDefaultCharacterDeniesAGuestAndPermitsTheSameFixturesOwner"
        status: pass
      - kind: unit
        ref: "internal/grpc/characteraccess_write_test.go#TestSetDefaultCharacterCollapsesAnUnparseableAndANotOwnedIdOntoOneOutcome"
        status: pass
      - kind: unit
        ref: "internal/grpc/characteraccess_write_test.go#TestSetDefaultCharacterRefusesARetiredCharacterAndPermitsTheActiveSibling"
        status: pass
      - kind: unit
        ref: "internal/grpc/characteraccess_write_test.go#TestSetDefaultCharacterWireMessagesCarryNoInternalCodeString"
        status: pass
      - kind: integration
        ref: "test/integration/access/character_write_test.go#D5, D6"
        status: pass
    human_judgment: false
  - id: D4
    description: "Both set-equality census gates carry the new rows and were observed RED before them"
    verification:
      - kind: unit
        ref: "task test -- ./test/meta/ (TestCharacterReturningRPCCensusMatchesTheReadSurfaceInventory, TestCharacterAccessRoutingCensus{GuestGate,Ownership,WebProxies,AudiencePartition})"
        status: pass
    human_judgment: false
  - id: D5
    description: "The gateway proxy forwards the header token and the character id verbatim and names its paired facade RPC"
    verification:
      - kind: unit
        ref: "internal/web/character_handlers_test.go#TestWebSetDefaultCharacterForwardsTheHeaderTokenAndTheCharacterIdVerbatim"
        status: pass
    human_judgment: false
  - id: D6
    description: "web/src/lib/characters/client.ts exists as the single Connect entry point, and no wrapper sends a session token"
    verification:
      - kind: unit
        ref: "web/src/lib/characters/client.test.ts#the character flow layer (7 tests)"
        status: pass
    human_judgment: false
  - id: D7
    description: "A signed-in player clicks `Make default` on /characters and the `Default` badge moves without a page reload, with the status line reading `{name} is now your default character.`"
    requirement: IDENT-05
    verification: []
    human_judgment: true
    rationale: "The visual outcome — that the badge MOVES, that the label does not change while in flight, and that the status line reads correctly — is a rendered-UI judgment. No Playwright spec covers /characters' default control in this plan; vitest covers the flow layer beneath it, not the render."

duration: 29min
completed: 2026-08-12
status: complete
---

# Phase 05 Plan 01: Default Character, End to End — Summary

**A `SetDefaultCharacter` RPC crossing proto → generated descriptors → both set-equality census gates → a narrow single-column `players` UPDATE → the `CharacterAccessService` facade → the gateway proxy → a new SvelteKit character flow layer → a working `Make default` control, closing IDENT-05's write path to a column that was read at login and cleared on retire but never set.**

## Performance

- **Duration:** 29 min
- **Started:** 2026-08-12T20:09:03Z
- **Completed:** 2026-08-12T20:38:00Z
- **Tasks:** 3
- **Files modified:** 38 (18 hand-written, 20 generated)

## Accomplishments

- `players.default_character_id` finally has a writer, and it is a narrow `UPDATE players SET default_character_id, updated_at WHERE id` rather than the full-row `Update` that would rewrite `password_hash` from a stale read with no version guard.
- The tracer proved the phase's whole chain works: `task proto && task web:generate` regenerates cleanly, both census gates go RED the instant a descriptor registers, and the routing census's four gates all fire on an unclassified handler.
- Every refusal path is asserted on what the caller received, each with a permitted twin on the same fixture, and no returned message carries an internal code string.
- `web/src/lib/characters/client.ts` is now the single Connect entry point the remaining seven Phase 5 web plans import.

## Task Commits

1. **Task 1 (tracer): End-to-end "a player's default character can be set"** — `92f20606d` (feat)
2. **Task 2: Facade denial paths with paired positive controls and wire-level assertions** — `27f677c9f` (test)
3. **Task 3: The character flow layer and the roster's Make default control** — `4e5aaf375` (feat)

Task 1 landed as ONE commit rather than a proto commit followed by an implementation commit. The proto declaration alone does not build: adding a method to `WebService` makes `*web.Handler` stop satisfying `webv1connect.WebServiceHandler`, so a proto-only commit leaves `task build` red and breaks `git bisect`. The plan's requirement that the generated output ship in the same change as the proto is satisfied; the atomicity is wider than the plan sketched, for a reason the plan could not have known without running the build.

## RED observations (PORTAL-10 rule 4)

Each gate was demonstrated RED against the pre-fix state before the fix landed. Verbatim:

**1. The RPC census, after `task proto` and before the inventory rows:**

```
the RPCs whose response carries character data do not equal the §3 read-surface
inventory. … extra (derived but not expected):
[holomush.characteraccess.v1.CharacterAccessService.SetDefaultCharacter
 holomush.web.v1.WebService.WebSetDefaultCharacter];
missing (expected but not derived): []
```

**2. The routing census, after the facade handler and before its rows** — three gates, all RED:

```
TestCharacterAccessRoutingCensusGuestGate         extra: [SetDefaultCharacter]
TestCharacterAccessRoutingCensusOwnership         extra: [SetDefaultCharacter]
TestCharacterAccessRoutingCensusAudiencePartition extra: [SetDefaultCharacter]
```

**3. The web-proxy census, after the proxy and before its row:**

```
TestCharacterAccessRoutingCensusWebProxies        extra: [WebSetDefaultCharacter]
```

**4. The integration spec, against a build where the handler did not exist.** The handler was temporarily renamed so the generated `UnimplementedCharacterAccessServiceServer` stub served the RPC; all six specs failed with `rpc error: code = Unimplemented desc = method SetDefaultCharacter not implemented` (D6 additionally failed on its message assertion). The handler was then restored and all six pass.

**5. The retired-character guard (Task 2 acceptance criterion).** With `if !world.Selectable(char.Status)` neutered, `TestSetDefaultCharacterRefusesARetiredCharacterAndPermitsTheActiveSibling/refuses_the_retired_character_with_FailedPrecondition_and_writes_nothing` failed — and it failed by REACHING the player repository (`mock: I don't know what to return because the method call was unexpected`), which is the stronger failure: the guard's absence lets a retired default actually be written, not merely mis-statused.

## Files Created/Modified

- `api/proto/holomush/characteraccess/v1/characteraccess.proto` — `SetDefaultCharacter` + its request/response pair
- `api/proto/holomush/web/v1/web.proto` — `WebSetDefaultCharacter` proxy pair; `default_character_id` on `WebCheckSessionResponse`
- `api/proto/holomush/core/v1/core.proto` — `default_character_id` on `CheckPlayerSessionResponse`
- `internal/auth/player.go`, `internal/auth/postgres/player_repo.go` — the `UpdateDefaultCharacter` interface method and its narrow implementation
- `internal/grpc/characteraccess_write.go` — the handler, `characterNotPlayableMessage`, `codeCharacterNotPlayable`
- `internal/grpc/characteraccess_owner.go` — `ownerRoster`, extracted from `ListMyCharacters` and shared
- `internal/grpc/auth_handlers.go` — `CheckPlayerSession` now forwards the default off the player row it already loaded
- `internal/web/character_handlers.go`, `internal/web/auth_handlers.go`, `internal/grpcclient/client.go`, `cmd/holomush/deps.go` — the proxy and its client plumbing
- `test/meta/character_rpc_census_test.go`, `test/meta/characteraccess_routing_census_test.go` — five new inventory rows
- `test/integration/access/character_write_test.go` — six real-Postgres specs (D1–D6)
- `internal/auth/postgres/player_repo_test.go` — the narrow-write repository specs
- `internal/grpc/characteraccess_write_test.go`, `internal/web/character_handlers_test.go` — the wire-level refusal suites
- `web/src/lib/characters/client.ts` + `client.test.ts` — the character flow layer
- `web/src/routes/(authed)/+layout.ts`, `web/src/routes/(authed)/characters/+page.svelte` — the default id's propagation and the control

## Decisions Made

- **`SetDefaultCharacter` carries no `expected_version` and never calls `requireGuardedVersion`.** The handler doc says so explicitly, because the omission looks exactly like the bug a reviewer would fix. Optimistic concurrency here is a property of the `characters` aggregate; this write targets a `players` row with no version column and no CAS to predicate on.
- **A retired character is refused `codes.FailedPrecondition` with its own literal** rather than the uniform ownership message the RESEARCH Q4 note recommended. Ownership has already been proven at that point and the player is looking at the card on their own roster, so a "no such character on your roster" answer would report a working rule as a lookup failure while buying no opacity — existence was never in question. Recorded as a deliberate deviation from RESEARCH Q4 in the plan; implemented as planned.
- **The predicate is `world.Selectable`, not `char.Status != StatusRetired`.** `world.StatusIdle` ships with no transition into it; a `!= retired` test would fail open the moment one lands.
- **`ownerRoster` was extracted and shared** rather than writing a second projection loop for the new response. Two loops agree until one of them gains a field.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 — Blocking] The plan's grounding for the initial `Default` badge is wrong; the field it names does not exist**

- **Found during:** Task 3
- **Issue:** The plan's Q2 corollary states the initial default "comes from `default_character_id` on `WebCheckSessionResponse` (`web.proto:567`), which the `(authed)` layout already fetches". Line 567 is `WebAuthenticatePlayerResponse`. Neither `WebCheckSessionResponse` nor the core `CheckPlayerSessionResponse` it proxies carried the field at all — `svelte-check` caught it as `Property 'defaultCharacterId' does not exist on type 'WebCheckSessionResponse'`. Since `OwnCharacter` deliberately carries no is-default flag (D-90), there was no server-side source for the badge, and the phase must-have "the `Default` badge moves to that card" was unreachable.
- **Fix:** Added `string default_character_id = 5` to both `CheckPlayerSessionResponse` and `WebCheckSessionResponse`, populated in `CoreServer.CheckPlayerSession` from the `player` row that handler ALREADY loads and forwarded verbatim by `Handler.WebCheckSession`. The plan's actual intent — "adding **no** round trip" — is preserved exactly. Neither RPC moves a census row: both are already inventory members via `CharacterSummary`, and the added field is a scalar id, not a new character-shaped message.
- **Files modified:** `api/proto/holomush/core/v1/core.proto`, `api/proto/holomush/web/v1/web.proto`, `internal/grpc/auth_handlers.go`, `internal/web/auth_handlers.go`, and their generated Go/TypeScript
- **Verification:** `pnpm check` 0 errors; `task test -- ./test/meta/ ./internal/grpc/ ./internal/web/ ./cmd/holomush/` green; `task lint:proto` green
- **Committed in:** `4e5aaf375`

**2. [Rule 3 — Blocking] Five implementations of the widened interfaces needed the new method**

- **Found during:** Task 1
- **Issue:** Adding `UpdateDefaultCharacter` to `auth.PlayerRepository` and `SetDefaultCharacter` to `web.CharacterAccessClient` / `cmd/holomush.GRPCClient` broke every hand-rolled double: `fakePlayerRepo` (`internal/bootstrap/admin_test.go`), `mockPlayerRepoForReset` (`internal/auth/reset_service_logging_test.go`), `stubCharacterAccessClient` (`internal/web/status_interceptor_test.go`), `mockGRPCClient` (`cmd/holomush/deps_test.go`), plus the `GRPCClient` interface itself.
- **Fix:** Added the method to each double and to `cmd/holomush/deps.go`'s interface; regenerated `internal/auth/mocks/mock_PlayerRepository.go` with `mockery`.
- **Files modified:** the five files above
- **Verification:** `task build` and `task test` green
- **Committed in:** `92f20606d`

**3. [Rule 3 — Blocking] `gocritic paramTypeCombine` on the new repository signature**

- **Found during:** Task 1
- **Issue:** `task lint` flagged `func(ctx context.Context, id ulid.ULID, characterID ulid.ULID) error`, the signature the plan spells out verbatim.
- **Fix:** Combined to `(ctx context.Context, id, characterID ulid.ULID)` on both the interface and the implementation. Behaviourally identical; no `//nolint` added and `.golangci.yaml` untouched.
- **Files modified:** `internal/auth/player.go`, `internal/auth/postgres/player_repo.go`
- **Verification:** `task lint` exit 0
- **Committed in:** `92f20606d`

### Plan criteria that could not be satisfied as literally written

Neither is a code defect; both are arithmetic/grep slips in the plan's acceptance criteria, recorded so a verifier re-running them is not misled.

- **Task 1, criterion 6:** "`rg -n 'requireGuardedVersion' internal/grpc/characteraccess_write.go | rg -v '^[0-9]+:\s*//'` shows exactly two hits". It shows **three**: the two call sites (in `UpdateCharacterProfile` and `UpdateCharacterDescription`, exactly as the criterion intends) plus the function's own `func requireGuardedVersion(...)` declaration line, which is not a comment. The same grep returns three at HEAD~3 too, so the count was wrong before this plan ran. The property the criterion is protecting — exactly two call sites, in those two handlers, none in `SetDefaultCharacter` — holds.
- **Task 3, criterion 2:** "`rg -n 'createClient' web/src/lib/characters/client.ts` returns exactly one occurrence". It returns **two**: the `import { createClient }` line and the call. `$lib/scenes/client.ts`, the file the plan mandates copying, has the same two. The property — exactly one `createClient(` invocation — holds (`rg -c 'createClient\(' → 1`).

### Shape adjustment inside Task 3

The plan asks the click handler to "re-render the cards from the returned roster". The returned roster is `OwnCharacter[]`; `/characters` renders `web.v1.CharacterSummary` cards, which carry the session overlay (`hasActiveSession`, `sessionStatus`, `lastPlayedAt`) that `OwnCharacter` deliberately does not. Rebuilding the cards from the response verbatim would silently drop that overlay. The Q2 join of both reads is plan 05-08's sectioned rewrite. So the handler uses the returned roster for **membership and order** — re-keying the local `CharacterSummary` list by the server's id sequence — and keeps each card's locally-read session fields. Server truth governs which cards exist and in what order; nothing is invented client-side.

---

**Total deviations:** 3 auto-fixed (3 blocking) + 1 shape adjustment
**Impact on plan:** Deviation 1 is the only one with scope: two proto messages gained one scalar field each so the plan's own stated data source would exist. It is additive, moves no census row, and preserves the "no extra round trip" property the plan was actually asserting. No scope creep otherwise.

## Issues Encountered

- **The web-proxy census's two conjuncts are genuinely independent gates and both had to be observed.** The guest-gate/ownership/partition trio goes RED when the FACADE handler lands; the proxy gate only goes RED once the PROXY lands. Running them as one batch would have collapsed two distinct RED observations into one and made the second unfalsifiable.
- **`git stash` is prohibited in worktrees**, so the two "observe RED against the pre-fix state" demonstrations were done by a targeted rename/neuter and an immediate restore, each verified back to green before the commit.

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- `web/src/lib/characters/client.ts` is ready to import. It deliberately has **no** `createCharacter` wrapper: those generated types arrive with plan 05-03, whose Task 3 adds the wrapper.
- Plan 05-03 should budget for **11** `NewCharacterAccessServer` call sites, not the 8 `05-RESEARCH.md` Pitfall 12 names. This plan added no constructor argument, so that count is unchanged by it.
- The `/characters` inline-create branch is untouched and still belongs to plan 05-08, which owns the sectioned rewrite and must preserve this plan's `defaultCharacterId` wiring and its `data.defaultCharacterId` source.
- **Open:** UI-SPEC's `populated | roster ordering within Playable` row remains unresolved (sketch 008 open question 4). This plan renders in whatever order the server returns and asserts nothing about it; the `IDENT-05/ordering` truth is a `verification: backstop` marker, deliberately unproven.

## Known Stubs

None. No hardcoded empty values, placeholder text, or unwired components were introduced.

## Threat Flags

None. Every surface this plan added is inside the plan's own `<threat_model>`: the new RPC and its proxy are T-05-01-01 through T-05-01-06. The `default_character_id` field added to `CheckPlayerSessionResponse` / `WebCheckSessionResponse` (deviation 1) discloses, to the already-authenticated session owner, one id from their own player row — the same value the same caller receives from `WebAuthenticatePlayer` today and can read back from the roster they own. No new trust boundary and no new audience.

---
*Phase: 05-character-identity-ui-public-profiles*
*Completed: 2026-08-12*

## Self-Check: PASSED

All key files verified present on disk and all three task commits verified in
`git log`:

- FOUND: `web/src/lib/characters/client.ts`, `web/src/lib/characters/client.test.ts`,
  `internal/web/character_handlers_test.go`, `internal/grpc/characteraccess_write.go`,
  `internal/auth/postgres/player_repo.go`, `test/integration/access/character_write_test.go`
- FOUND: `92f20606d`, `27f677c9f`, `4e5aaf375`
- `git status --porcelain pkg/proto web/src/lib/connect` is EMPTY (generated output committed)
- `task test` exit 0 · `task test:int -- ./test/integration/access/` exit 0 ·
  `task lint` exit 0 · `task lint:proto` exit 0 · `task fmt:check` exit 0 ·
  `pnpm test:unit` 465/465 · `pnpm check` 0 errors
