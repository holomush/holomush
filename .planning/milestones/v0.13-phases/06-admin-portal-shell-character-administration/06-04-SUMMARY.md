---
phase: 06-admin-portal-shell-character-administration
plan: 04
subsystem: api
tags: [grpc, abac, admin-portal, postgres, search, pagination, pg_trgm, protobuf]

requires:
  - phase: 06-admin-portal-shell-character-administration
    plan: 02
    provides: "mapAdminSectionError, the interceptor's fixed-SectionID arm, and the AdminPortalServerOption seam this plan extends"
  - phase: 02-abac-schema-vocabulary
    provides: "section.AdminDescriptors and the admin_section: gate the three new reads declare against"
provides:
  - "AdminListCharacters / AdminSearchCharacters / AdminGetCharacter on holomush.adminportal.v1.AdminPortalService, each gated by its own fixed-section descriptor entry"
  - "AdminCharacterSortField — a six-value closed sort enum with NO player_id value, making SPEC 11.3's Sort=No structural rather than a runtime check"
  - "AdminCharacterStatusFilter — a closed lifecycle filter, so SPEC 9.3's vocabulary does not re-enter the wire as free text"
  - "world.AdminCharacterListOptions / AdminCharacterPage / AdminCharacterRow — the domain-level shapes internal/grpc names without importing internal/world/postgres"
  - "CharacterRepository.AdminListCharacters / AdminSearchCharacters / AdminGetCharacterRow — the joined projection, the three-clause total ordering and the escaped substring predicate"
  - "grpc.AdminCharacterReader + grpc.AdminProfileReader and their With* options, wired at BOTH composition roots"
  - "migration 000057 — gin_trgm_ops indexes on characters.normalized_name and players.username"
  - "TestAdminSearchPredicatesNameOnlyTheTwoSearchableColumns — the search-column fence, proven RED against a planted description predicate"
affects: [06-05, 06.1-03, 06.1-04]

actuals:
  tokens: 71000
  tasks: 3
  commits: 4

tech-stack:
  added: []
  patterns:
    - "Bounded trusted projection: a raw repository read whose bound is membership in an already-shipped write allowlist, so read and write are symmetric by construction rather than by two lists agreeing"
    - "Structural refusal over runtime rejection: an unsortable field has NO enum value, so the illegal request is inexpressible rather than caught"
    - "Discriminating fixture: a test's fixture must be able to CONTAIN the failing case, or its set-equality is coverage in appearance only"

key-files:
  created:
    - internal/store/migrations/000057_character_admin_search_indexes.sql
    - internal/world/character_admin.go
    - internal/world/postgres/character_repo_admin.go
    - internal/world/postgres/character_repo_admin_integration_test.go
    - internal/grpc/admin_characters_read.go
    - internal/grpc/admin_characters_read_test.go
    - test/integration/access/admin_characters_read_test.go
  modified:
    - api/proto/holomush/adminportal/v1/adminportal.proto
    - api/proto/holomush/web/v1/web.proto
    - internal/admin/section/descriptor.go
    - internal/grpc/admin_service.go
    - internal/web/admin_handlers.go
    - internal/web/handler.go
    - internal/grpcclient/client.go
    - cmd/holomush/deps.go
    - cmd/holomush/deps_test.go
    - cmd/holomush/sub_grpc.go
    - internal/testsupport/integrationtest/harness.go
    - test/meta/world_sql_fence_test.go
    - site/src/content/docs/reference/grpc-api.md

key-decisions:
  - "06-04: the admin list/page types live in internal/world, not internal/world/postgres — internal/grpc must name them on its narrow reader interfaces, and it imports internal/world/postgres nowhere else"
  - "06-04: the detail read reaches PropertyRepository.ListByParent directly and filters on updateCharacterProfileMaskablePaths membership; world.Service.ListPropertiesByParent returns an EMPTY slice for a player-flavoured caller, verified at source against the whole seed corpus"
  - "06-04: AdminGetCharacterRow (the joined single-row read) replaces the plan's bare Get, because the detail message embeds the same SPEC 11.3 projection and Get does not join players — composing from it would leave player_username silently empty on the one read the edit sheet renders from"
  - "06-04: the name sort arm carries NO trailing tiebreak, because normalized_name is UNIQUE (migration 000056) and a tiebreak on itself would be an unreachable clause plus a vacuous test"
  - "06-04: LIKE-metacharacter fixtures are USERNAMES, not character names — charname's gate refuses a name containing anything but letters and spaces, so no metacharacter can ever reach normalized_name, while username is under no such gate"

requirements-completed: [ADMIN-03]

status: complete
---

# Phase 06 Plan 04: Admin Character Reads Summary

Substring search over character names and player usernames behind three ABAC-gated RPCs, with a total ordering that puts `never` last in both directions and a detail read whose bound is the twelve fields an admin may also write.

## Performance

- **Duration:** 33 min
- **Started:** 2026-08-14T14:39:06Z
- **Completed:** 2026-08-14T15:12Z
- **Tasks:** 3 completed
- **Commits:** 4

## Task Commits

1. **Task 1: Migration 000057 and the two repository reads** (TDD) — two commits:
   - `5d596505b` (test) — RED: the migration, the domain shapes, and an integration suite that does not compile
   - `aa288df75` (feat) — GREEN: the joined projection, the closed ORDER BY switch, the escaped predicate and the RepeatableRead two-statement page
2. **Task 2: The three admin read RPCs and their Web proxies** — `2bcd6c724` (feat)
3. **Task 3: Pin the search scope** — `5d05e3930` (test)

## Accomplishments

- **The detail read's stated bound is now true rather than approximately true.** The projection filters on `updateCharacterProfileMaskablePaths` membership — `rg -c '"profile\.' internal/grpc/characteraccess_write.go` is **12** — so "an admin can read exactly the fields an admin can edit" holds against **one** shipped list. Swapping the filter to `isGovernedProfileName` was demonstrated to leak `profile.image.gallery.00`.
- **The rejected ABAC path was verified at source, not taken on trust.** Every `resource is property` permit in the seed corpus is `principal is character` (6) or `principal is viewer` (6); `rg -q 'principal is player.*resource is property'` exits **non-zero** — no player-principal property permit exists at all. So `world.Service.ListPropertiesByParent` really would return an empty slice for this caller, and the D-104 silent-authorization miss is real rather than theoretical. `internal/access/` is untouched by this plan.
- **`player_id` is unsortable structurally.** There is no enum value for it, so a client cannot express the request; a set-equality test in both directions over `AdminCharacterSortField_name` fails if one is ever added. It remains an equality filter, and a paired test keeps the two §11.3 columns from being collapsed.
- **Every ordering clause was proven load-bearing by a demonstrated RED**, and one of them only after the fixture was fixed — see below.
- **`description` reaches the DETAIL message and nothing else.** The `AdminCharacter` list message has no such field, so the bulk cross-player projection cannot become a bulk prose export.

## Demonstrations Performed and Recorded

Each was planted, observed, and reverted; the working tree was verified clean afterwards.

| Planted mutation | Assertion | Observed |
|---|---|---|
| Drop `(c.last_active_at = 0)` from the ordering | both directions | **ASC FAILS, DESC still passes** — exactly the asymmetry a one-direction test cannot see |
| Drop the `c.normalized_name ASC` tiebreak | the `never`-block order | FAIL in both directions (**only after the fixture was fixed** — see deviations) |
| `COUNT(*) OVER ()` in place of the scalar count | page beyond the end | FAIL — `expected: 3, actual: 0` |
| Neutralise `escapeLikeWildcards` | the three metacharacter cases | FAIL ×3 — `a_b` matched `axb`, `100%` matched `1000`, `c\d` matched `cd` |
| Swap the profile filter to `isGovernedProfileName` | Test 9 set equality | FAIL on the **EXTRA** direction: `LEAKED non-governed value "profile.image.gallery.00"` |
| Profile reader yields an empty slice (what the rejected `world.Service` path returns) | Test 9 set equality | FAIL on the **MISSING** direction: "an empty response would satisfy the MISSING direction vacuously" |
| `OR c.description ILIKE …` in the search predicate | the new search-column fence | FAIL — names `c.description` as a §10.6 privacy-bearing prose column |

**Migration round-trip on a scratch Postgres 17 container** (`DATABASE_URL` against a throwaway container, then removed):

| Command | Exit | Indexes present | `max(version_id)` |
|---|---|---|---|
| `./holomush migrate up` | 0 | 2 | 57 |
| `./holomush migrate down` | 0 | 0 | 56 |
| `./holomush migrate up` | 0 | 2 | 57 |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] The admin list types could not live in `internal/world/postgres`**

- **Found during:** Task 1
- **Issue:** The plan puts `AdminCharacterListOptions` / `AdminCharacterPage` / `AdminCharacterRow` in `internal/world/postgres/character_repo.go`, but Task 2's `AdminCharacterReader` interface lives in `internal/grpc` and must name them. `internal/grpc` imports `internal/world/postgres` **nowhere** (its single mention is a comment at `characteraccess_write.go:545`), and adding that edge would put the gRPC layer downstream of a storage driver.
- **Fix:** The types live in `internal/world` (new `character_admin.go`), the domain package where `world.CharacterRepository` already lives and which `internal/grpc` already imports. The methods stay on `*postgres.CharacterRepository` as planned.
- **Files modified:** `internal/world/character_admin.go` (new)
- **Committed in:** `5d596505b`

**2. [Rule 2 - Missing critical functionality] The detail read composed from `Get` would ship a silently-empty `player_username`**

- **Found during:** Task 2
- **Issue:** The plan composes `AdminGetCharacter` from `CharacterRepository.Get` plus `ListByParent`. But `AdminCharacterDetail` embeds the same §11.3 projection the list carries, and `player_username` is one of its fields — `Get` does not join `players`. The detail read is the ONE read the edit sheet renders from, so a blank username there is precisely the class of failure the whole detail read exists to prevent.
- **Fix:** Added `AdminGetCharacterRow(ctx, id) (world.AdminCharacterRow, error)` — the single-row form of the SAME joined projection the two page reads use — and put it on `AdminCharacterReader` in place of `Get`. It does **not** give `CharacterRepository` a door to `entity_properties`; that prohibition is untouched.
- **Files modified:** `internal/world/postgres/character_repo_admin.go`, `internal/grpc/admin_service.go`, `internal/grpc/admin_characters_read.go`
- **Committed in:** `2bcd6c724`

**3. [Rule 1 - Bug] The tiebreak demonstration passed under the bug, because the fixture could not observe the clause**

- **Found during:** Task 1's required RED demonstration
- **Issue:** Deleting `c.normalized_name ASC` left the ordering test **GREEN**. The fixture seeded the never-active rows as `aaa…` then `zzz…`, so heap order (insertion order, which is what Postgres returns absent a tiebreak) coincided with name order. The clause was unobservable and the test was coverage in appearance only.
- **Fix:** Seed `zzz…` FIRST and `aaa…` second, so insertion order and name order DISAGREE. The deletion now fails in both directions. The reason is written into the fixture so it is not "tidied" back.
- **Files modified:** `internal/world/postgres/character_repo_admin_integration_test.go`
- **Committed in:** `aa288df75`

**4. [Rule 1 - Bug] The LIKE-metacharacter fixtures were unbuildable as character names**

- **Found during:** Task 1
- **Issue:** The plan seeds characters named `a_b`, `100%` and `c\d`. `charname`'s admission gate refuses them with `NAME_INVALID_SYNTAX` (`gate.go:155`, "letters and spaces only"), so the test could not run at all — and, more to the point, **no metacharacter can ever reach `characters.normalized_name`**.
- **Fix:** The fixtures are `players.username` values, which are under no such gate and are therefore where a stored metacharacter is actually reachable. The TERM can carry one on either arm regardless, since the operator types it and `Normalize` passes it through. Each case now seeds a matching username AND a decoy only an unescaped pattern would match, plus a preliminary assertion that every decoy is reachable — so no case can pass vacuously.
- **Files modified:** `internal/world/postgres/character_repo_admin_integration_test.go`
- **Committed in:** `aa288df75`

**5. [Rule 3 - Blocking] The gateway client seam spans four files, again**

- **Found during:** Task 2
- **Issue:** The plan names only `internal/web/admin_handlers.go`. As both 06-01 and 06-02 recorded, the seam also spans `internal/web/handler.go` (`AdminPortalClient`), `internal/grpcclient/client.go`, and `cmd/holomush/deps.go` (the `GRPCClient` interface), plus `mockGRPCClient` in `cmd/holomush/deps_test.go`.
- **Fix:** Added all three methods to all five sites. The `(*Client).Admin*Characters` forwarders are deliberately **not** oops-wrapped, for the same reason their section peers are not.
- **Files modified:** `internal/web/handler.go`, `internal/grpcclient/client.go`, `cmd/holomush/deps.go`, `cmd/holomush/deps_test.go`
- **Committed in:** `2bcd6c724`

**6. [Rule 1 - Bug] `assert.NotNil` on an empty proto map is always false**

- **Found during:** Task 2's wire test
- **Issue:** The detail wire test asserted `NotNil(detail.GetProfile())` for a character that had authored nothing. An empty proto map decodes as a nil Go map, so the assertion failed against correct behaviour.
- **Fix:** Replaced with a genuine end-to-end proof: seed a GOVERNED row and a GALLERY-SLOT row directly into `entity_properties`, re-read, and assert the governed value arrives while the gallery row does not. The wire test now discriminates the two filters just as the unit test does, and proves the read really reaches the property repository through `WithAdminProfileReader` at the harness root.
- **Files modified:** `test/integration/access/admin_characters_read_test.go`
- **Committed in:** `2bcd6c724`

## Criterion Defects Found (reported, NOT repaired)

Both are **measured-at-the-wrong-scope** defects. In each case the criterion's INTENT holds; the literal command it specifies does not, because the file's own explanatory comment contains the string being counted. Nothing was deleted from any comment to make either pass.

**1. Task 1: `rg -c 'gin_trgm_ops' 000057_…sql` is 2** — it is **3**. `rg -c` counts LINES over the whole file, and the migration's header comment names the three shipped `gin_trgm_ops` precedents in `000001_baseline.sql` in order to explain why the extension is not re-declared. Measured at the scope the criterion means — SQL statements, comments filtered — it is correct:

```
rg -v '^\s*--' internal/store/migrations/000057_character_admin_search_indexes.sql | rg -o 'gin_trgm_ops' | wc -l   →   2
```

**2. Task 2: `rg -n 'COUNT\(\*\) OVER' character_repo.go` returns zero matches** — the string is present, in the doc comment that states the total is NOT a window column and why. This is the same trap the plan itself identified as C3-40 for two other symbols and solved with a comment filter; it simply was not applied to this third criterion. Comment-filtered, it holds:

```
rg -v '^\s*//' internal/world/postgres/character_repo_admin.go | rg -q 'COUNT\(\*\) OVER'   →   exits 1 (absent from code)
```

A third, milder one: Task 1's `rg -n '"ASC"' …_integration_test.go` and its `"DESC"` twin were a weak proxy for "one table, both directions" — the case table used `descending: false/true`. Rather than report and leave it, the table gained a `direction string` field carrying the SQL keyword, which both satisfies the criterion honestly and improves the failure message. That is a fixture improvement, not a criterion repair.

## Outstanding: the ABAC domain gate did NOT run

**`/holomush-dev:review-abac` could not be invoked.** The `Task` tool is disabled in this executor session ("No such tool available: Task. Task is disabled for this session, in subagents as well as here."), so the `abac-reviewer` sub-agent cannot be dispatched. This is Task 3's `<verify><human-check>`, one of its acceptance criteria, a clause of its `<done>`, and `T-06-29`'s **mitigation of record** for a high-rated authorization-bypass-in-shape. **No verdict was produced, and none is claimed here.**

It is recorded in `.planning/WINDOWS.md` as an `unrun-verify` entry so it survives to the ship gate.

What WAS verified, manually and at source, in lieu of the adversarial pass — every one of the three things the gate was to be handed:

| Claim | Command | Result |
|---|---|---|
| The bound is twelve names | `rg -c '"profile\.' internal/grpc/characteraccess_write.go` | **12** |
| No player-principal property permit exists | `rg -q 'principal is player.*resource is property' internal/access/policy/seed.go` | exits **1** (none) |
| Every property permit's principal | `rg -o 'permit\(principal is (\w+), …, resource is property'` | 6 character, 6 viewer, **0 player** |
| The only player-principal policy is section-scoped | `rg -n 'principal is player' …/seed.go` | one hit, `resource is admin_section` (`:987`) |
| `description` does not reach the LIST message | grep of `message AdminCharacter` | absent (DETAIL only) |
| No seed policy changed | `git diff 088c6979e..HEAD -- internal/access/` | empty |

That is evidence for the design's premises. It is **not** a substitute for the adversarial review, which is exactly the thing that looks for what the author did not think to check. **This gate should be run before the phase ships.**

## Verification

| Check | Result |
|---|---|
| `task test -- ./internal/grpc/... ./internal/admin/... ./internal/web/... ./cmd/holomush/... ./test/meta/` | 2480 tests, 1 pre-existing skip |
| `task test:int -- ./internal/world/postgres/...` | 354 tests |
| `task test:int -- ./test/integration/access/...` | 24 tests |
| `task lint` | green |
| `task lint:proto` | green |
| `task lint:no-timestamptz` | exit 0 |
| `task test -- ./internal/store/... -run TestEveryDollarQuotedMigrationBodyIsWrappedInStatementBeginEnd` | pass |
| `git status --porcelain pkg/proto web/src/lib/connect site/…/grpc-api.md` after regen | clean |
| Migration up / down / up on a scratch database | 2 → 0 → 2 indexes, v57 → v56 → v57 |

## Known Stubs

None. Every field on every message this plan ships is populated from a real source; the one field that would have been a stub — `AdminCharacterDetail.character.player_username`, empty under the plan's `Get`-based composition — is the subject of deviation 2 above and is now read from the joined projection.

## Self-Check: PASSED

All seven created files exist on disk; all four commit hashes resolve.
