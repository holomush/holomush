<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Remove `session.MemStore` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `dev-flow:subagent-driven-development` (recommended) or `dev-flow:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Spec:** [docs/superpowers/specs/2026-05-23-remove-session-memstore-design.md](../specs/2026-05-23-remove-session-memstore-design.md)
**Design bead:** `holomush-9mxr`

**Goal:** Delete `internal/session/memstore.go` (552 lines) + `internal/session/memstore_test.go` (856 lines) and migrate all ~86 test sites in 13 files to a new `sessiontest.NewStore(t)` helper backed by the existing shared-Postgres testcontainer infrastructure.

**Architecture:** Single PR, 5 logical phases (matching the spec's 5-commit structure). Each phase ends in a working state where `task test ./...` passes. Phase 1 (consolidation) is pure refactor; phases 2-4 carry out the migration + deletion; phase 5 documents the convention shift. The new `sessiontest` package establishes a deliberate exception to the repo's "SharedPostgres tests MUST be `//go:build integration`" convention.

**Tech Stack:** Go 1.23, `testcontainers-go`, `pgx/v5`, `testify`, `samber/oops`, existing `test/testutil/postgres.go` helpers (`SharedPostgres`, `FreshDatabase`).

---

## Files Affected

**New:**

- `internal/testsupport/sessiontest/store.go` — `NewStore(t *testing.T) session.Store` helper
- `internal/testsupport/sessiontest/store_test.go` — TDD test for the helper

**Deleted:**

- `internal/session/memstore.go` (552 lines)
- `internal/session/memstore_test.go` (856 lines)

**Modified — test conversion (13 files, 86 sites):**

| File | Sites today | Pattern |
|---|---|---|
| `internal/grpc/auth_handlers_test.go` | 60 | Consolidate first (Phase 1), then convert |
| `internal/grpc/focus/coordinator_test.go` | 4 | Direct convert |
| `internal/grpc/focus/leave_by_target_test.go` | 3 | Direct convert |
| `internal/grpc/focus/prefs_test.go` | 2 | Direct convert |
| `internal/grpc/focus/restore_test.go` | 1 | Direct convert |
| `internal/grpc/dispatcher_test.go` | 3 | Direct convert |
| `internal/grpc/test_helpers_test.go` | 3 | Direct convert |
| `internal/grpc/server_helpers_test.go` | 1 | Direct convert |
| `internal/grpc/location_follow_test.go` | 2 | Direct convert |
| `internal/command/handlers/resetpassword_test.go` | 2 | Direct convert |
| `internal/command/handlers/plugin_admin_test.go` | 1 | Direct convert |
| `internal/auth/session_ownership_test.go` | 1 | Direct convert |
| `test/integration/phase1_5_test.go` | 3 | Replace with integrationtest harness store (incongruous MemStore-in-integration usage) |

**Modified — survival rewire:**

- `internal/session/focus_mutator_test.go` (49 lines) — FocusMutator-specific assertions stay; the store-fixture path is rewired since MemStore is gone

**Modified — verification:**

- `internal/store/session_store_test.go` (new or extended) — INV-M-3 explicit test for `client_type` rejection

**Modified — documentation:**

- `CLAUDE.md` (Testing section)
- `site/docs/contributing/integration-tests.md`

---

## Phase 1 — Test consolidation (still MemStore)

Pure refactor. Reduces ~86 sites to ~50 via table-driven patterns. Each consolidated case MUST carry a `// replaces:` comment listing the pre-consolidation test functions it absorbs (per spec INV-M-4). No behavior change.

### Canonical table-driven pattern (referenced by all consolidation tasks)

Every consolidation task in Phase 1 applies the same pattern. Tasks 1-5 each enumerate which pre-consolidation tests to merge and where to insert the result; they do not re-derive the pattern. The pattern:

```go
// replaces: TestPreconsolidationA, TestPreconsolidationB, TestPreconsolidationC
func Test<RPC>_<GroupName>(t *testing.T) {
    ctx := context.Background()

    tests := []struct {
        name      string
        setup     func(srv *CoreServer, mocks *<TestMocks>)  // configure mocks per row
        wantSuccess bool                                       // for Success:false handlers
        wantErr     bool                                       // for return-error handlers
        wantStatusCode codes.Code                              // for gRPC-status-translated rows
        wantErrCode    string                                  // for oops.Code rows
    }{
        // one row per pre-consolidation test, copying its setup verbatim
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            sessionStore := session.NewMemStore()  // becomes sessiontest.NewStore(t) in Phase 3
            srv := &CoreServer{sessionStore: sessionStore}
            mocks := &<TestMocks>{}
            tt.setup(srv, mocks)

            resp, err := srv.<RPC>(ctx, &corev1.<RPC>Request{ /* fixed inputs */ })

            switch {
            case tt.wantErr:
                require.Error(t, err)
                if tt.wantStatusCode != codes.OK {
                    statusErr, ok := status.FromError(err)
                    require.True(t, ok)
                    assert.Equal(t, tt.wantStatusCode, statusErr.Code())
                }
            default:
                require.NoError(t, err)
                assert.Equal(t, tt.wantSuccess, resp.GetSuccess())
            }
        })
    }
}
```

**Pattern application rules:**

1. **One consolidated test per (RPC, kind-of-failure) group.** Don't merge happy-path with error-paths; happy-paths assert payload contents and have different shapes.
2. **Copy mock-setup code verbatim from the pre-consolidation tests.** Do NOT re-derive what mocks return — the existing tests are the source of truth.
3. **The `// replaces:` comment is mandatory.** It names every pre-consolidation function the new test absorbs. INV-M-4's attestation depends on this.
4. **Drop fields from the struct that no row uses.** The struct shown above is illustrative — pick the subset your RPC actually exercises.

### Task 1: Establish the canonical table-driven pattern in `AuthenticatePlayer` tests

**Files:**

- Modify: `internal/grpc/auth_handlers_test.go:60-177` (5 tests collapse to 2)

The 5 `TestAuthenticatePlayer_*` functions exercise: success, invalid credentials, service-not-configured, session-repo-not-configured, session-repo-create-fails. The happy path differs structurally (it asserts response payload contents); the four error paths share shape (set up mock returning error → call RPC → assert specific error or Success:false).

- [ ] **Step 1: Identify the four error-path tests for consolidation**

  Functions to merge:
  - `TestAuthenticatePlayer_InvalidCredentials` (auth.ErrNotFound from authService)
  - `TestAuthenticatePlayer_ServiceNotConfigured` (authService is nil)
  - `TestAuthenticatePlayer_SessionRepoNotConfigured` (playerSessionRepo is nil)
  - `TestAuthenticatePlayer_SessionRepoCreateFails` (sessionRepo.Create returns error)

  `TestAuthenticatePlayer_Success` stays separate (asserts response payload).

- [ ] **Step 2: Write the consolidated table-driven test**

  Insert after `TestAuthenticatePlayer_Success`, before the previous `TestAuthenticatePlayer_InvalidCredentials`:

  ```go
  // replaces: TestAuthenticatePlayer_InvalidCredentials,
  //           TestAuthenticatePlayer_ServiceNotConfigured,
  //           TestAuthenticatePlayer_SessionRepoNotConfigured,
  //           TestAuthenticatePlayer_SessionRepoCreateFails
  func TestAuthenticatePlayer_ErrorPaths(t *testing.T) {
      ctx := context.Background()

      tests := []struct {
          name           string
          setupAuthSvc   func(*mockAuthService)
          authSvcNil     bool
          sessionRepoNil bool
          setupRepo      func(*authmocks.MockPlayerSessionRepository)
          wantSuccess    bool
          wantErr        bool
      }{
          {
              name: "invalid credentials returns Success=false",
              setupAuthSvc: func(s *mockAuthService) {
                  s.authenticatePlayerFunc = func(_ context.Context, _, _, _, _ string) (string, *auth.Player, error) {
                      return "", nil, auth.ErrNotFound
                  }
              },
          },
          {
              name:       "nil authService returns Success=false",
              authSvcNil: true,
          },
          {
              name:           "nil playerSessionRepo returns Success=false",
              sessionRepoNil: true,
          },
          {
              name: "session repo create failure returns Success=false",
              setupAuthSvc: func(s *mockAuthService) {
                  s.authenticatePlayerFunc = func(_ context.Context, _, _, _, _ string) (string, *auth.Player, error) {
                      return "token", &auth.Player{ID: ulid.Make()}, nil
                  }
              },
              setupRepo: func(r *authmocks.MockPlayerSessionRepository) {
                  r.EXPECT().Create(mock.Anything, mock.Anything).Return(errors.New("db down"))
              },
          },
      }

      for _, tt := range tests {
          t.Run(tt.name, func(t *testing.T) {
              sessionStore := session.NewMemStore()  // converted to sessiontest.NewStore(t) in Phase 3
              srv := &CoreServer{sessionStore: sessionStore}

              if !tt.authSvcNil {
                  authSvc := newMockAuthService(t)
                  if tt.setupAuthSvc != nil {
                      tt.setupAuthSvc(authSvc)
                  }
                  srv.authService = authSvc
              }
              if !tt.sessionRepoNil {
                  repo := authmocks.NewMockPlayerSessionRepository(t)
                  if tt.setupRepo != nil {
                      tt.setupRepo(repo)
                  }
                  srv.playerSessionRepo = repo
              }

              resp, err := srv.AuthenticatePlayer(ctx, &corev1.AuthenticatePlayerRequest{
                  Username: "alice", Password: "pw",
              })
              if tt.wantErr {
                  require.Error(t, err)
                  return
              }
              require.NoError(t, err)
              assert.Equal(t, tt.wantSuccess, resp.GetSuccess())
          })
      }
  }
  ```

- [ ] **Step 3: Delete the four superseded test functions**

  Remove `TestAuthenticatePlayer_InvalidCredentials` (lines ~110-134), `TestAuthenticatePlayer_ServiceNotConfigured` (~136-154), `TestAuthenticatePlayer_SessionRepoNotConfigured` (~156-177), `TestAuthenticatePlayer_SessionRepoCreateFails` (~1423-1446). Keep `TestAuthenticatePlayer_Success` (~60-108).

- [ ] **Step 4: Run package tests, verify pass**

  Run: `task test -- ./internal/grpc/`
  Expected: PASS. New `TestAuthenticatePlayer_ErrorPaths` runs 4 subtests; `TestAuthenticatePlayer_Success` unchanged. Total `TestAuthenticatePlayer_*` count drops from 5 to 2.

- [ ] **Step 5: Commit checkpoint (do not push)**

  Working state. Hold the commit for the consolidation roll-up at end of Phase 1.

---

### Task 2: Consolidate `CheckPlayerSession` error-translation tests

**Files:**

- Modify: `internal/grpc/auth_handlers_test.go` (5 tests collapse to 2)

The 5 functions are: `TestCheckPlayerSession` (happy path), `TestCheckPlayerSessionAuthFailureTranslatesToCodesUnauthenticated`, `TestCheckPlayerSessionInfraFailureNotTranslated`, `TestCheckPlayerSessionPopulatesPlayerIDIsGuestAndCharactersOnSuccess`, `TestCheckPlayerSessionWrapsCharacterLookupFailureAsCharacterLookupFailed`, `TestCheckPlayerSessionWrapsPlayerLookupFailureAsPlayerLookupFailed`.

Pattern: happy path stays separate (`TestCheckPlayerSession`, `TestCheckPlayerSessionPopulatesPlayerIDIsGuestAndCharactersOnSuccess` — both assert response payload). The three error-translation tests collapse to one table.

- [ ] **Step 1: Identify the three error-translation tests for consolidation**

  Merge into `TestCheckPlayerSession_ErrorTranslation`:
  - `TestCheckPlayerSessionAuthFailureTranslatesToCodesUnauthenticated` (auth fail → codes.Unauthenticated)
  - `TestCheckPlayerSessionInfraFailureNotTranslated` (infra fail → NOT codes.Unauthenticated)
  - `TestCheckPlayerSessionWrapsPlayerLookupFailureAsPlayerLookupFailed` (player lookup fail → PLAYER_LOOKUP_FAILED)
  - `TestCheckPlayerSessionWrapsCharacterLookupFailureAsCharacterLookupFailed` (char lookup fail → CHARACTER_LOOKUP_FAILED)

- [ ] **Step 2: Write the consolidated test following the pattern from Task 1**

  Use the same shape as Task 1's `TestAuthenticatePlayer_ErrorPaths`: a `tests []struct` slice with a `setup` func and `wantCode`/`wantStatusCode` assertions per row. Place the `// replaces:` comment listing all four pre-consolidation test names.

  Reference the existing test bodies (~1273-1419) for setup details — copy the mock-builder code per row, do not re-derive it.

- [ ] **Step 3: Delete the four superseded test functions**

  Remove the four pre-consolidation tests. Keep `TestCheckPlayerSession` and `TestCheckPlayerSessionPopulatesPlayerIDIsGuestAndCharactersOnSuccess` unchanged.

- [ ] **Step 4: Run package tests, verify pass**

  Run: `task test -- ./internal/grpc/`
  Expected: PASS.

- [ ] **Step 5: Commit checkpoint**

---

### Task 3: Consolidate `CreatePlayer` + `CreateCharacter` tests

**Files:**

- Modify: `internal/grpc/auth_handlers_test.go` (9 tests collapse to 4)

Group A — CreatePlayer (5 → 2):

- Keep: `TestCreatePlayer_Success`
- Merge into `TestCreatePlayer_ErrorPaths`: `TestCreatePlayer_ServiceNotConfigured`, `TestCreatePlayer_UsernameTaken`, `TestCreatePlayerReturnsGenericMessageForUnknownError`, `TestCreatePlayerReturnsSanitizedMessageForUsernameTaken`

Group B — CreateCharacter (4 → 2):

- Keep: `TestCreateCharacter_Success`
- Merge into `TestCreateCharacter_ErrorPaths`: `TestCreateCharacter_InvalidSession`, `TestCreateCharacter_NotConfigured`, `TestCreateCharacterReturnsSanitizedMessageForNameTaken`

- [ ] **Step 1: Write `TestCreatePlayer_ErrorPaths`**

  Apply the table-driven pattern from Task 1. Each row sets up the relevant mock failure mode (nil authService, ErrNotFound, generic error, sanitized-message-expected) and asserts the resulting Success flag + error message contents per row's expectation.

  Comment: `// replaces: TestCreatePlayer_ServiceNotConfigured, TestCreatePlayer_UsernameTaken, TestCreatePlayerReturnsGenericMessageForUnknownError, TestCreatePlayerReturnsSanitizedMessageForUsernameTaken`

- [ ] **Step 2: Delete the four superseded CreatePlayer error tests**

- [ ] **Step 3: Write `TestCreateCharacter_ErrorPaths`**

  Same pattern, three rows. Comment: `// replaces: TestCreateCharacter_InvalidSession, TestCreateCharacter_NotConfigured, TestCreateCharacterReturnsSanitizedMessageForNameTaken`

- [ ] **Step 4: Delete the three superseded CreateCharacter error tests**

- [ ] **Step 5: Run package tests, verify pass**

  Run: `task test -- ./internal/grpc/`
  Expected: PASS. `TestCreatePlayer_*` count: 5 → 2. `TestCreateCharacter_*` count: 4 → 2.

- [ ] **Step 6: Commit checkpoint**

---

### Task 4: Consolidate `ListCharacters` + `ListPlayerSessions` + `ResolvePlayerSession` + `ConfirmPasswordReset` tests

**Files:**

- Modify: `internal/grpc/auth_handlers_test.go` (14 tests collapse to 8)

Apply the table-driven pattern from Task 1 in four micro-groups (one consolidated test per group). Per-group `// replaces:` comments name the merged functions.

| Group | Keep | Merge into `*_ErrorPaths` |
|---|---|---|
| ListCharacters | `TestListCharacters_Success` | `_InvalidSession_ReturnsError`, `_NotConfigured_ReturnsError`, `_LocationLookupFailure_OmitsLocation`, `_ResolvesLocationName` |
| ListPlayerSessions | `TestListPlayerSessionsReturnsCallersOwnSessionsWithIsCurrentFlag` | `_ReturnsEmptyForInvalidToken`, `_ReturnsEmptyForExpiredSession` |
| ResolvePlayerSession | (keep all 3 — distinct success paths) | (none) |
| ConfirmPasswordReset | `TestConfirmPasswordReset_Success` | `_InvalidToken`, `ReturnsSanitizedMessageForInvalidToken` |

- [ ] **Step 1: Write `TestListCharacters_ErrorAndDerivationPaths`** (4 rows)
- [ ] **Step 2: Delete the 4 superseded ListCharacters tests**
- [ ] **Step 3: Write `TestListPlayerSessions_AuthFailurePaths`** (2 rows)
- [ ] **Step 4: Delete the 2 superseded ListPlayerSessions tests**
- [ ] **Step 5: Write `TestConfirmPasswordReset_InvalidTokenPaths`** (2 rows)
- [ ] **Step 6: Delete the 2 superseded ConfirmPasswordReset tests**
- [ ] **Step 7: Run package tests, verify pass**

  Run: `task test -- ./internal/grpc/`
  Expected: PASS.

- [ ] **Step 8: Commit checkpoint**

---

### Task 5: Consolidate `Logout` + `RevokePlayerSession` + `RevokeOtherPlayerSessions` tests

**Files:**

- Modify: `internal/grpc/auth_handlers_test.go` (13 tests collapse to 7)

| Group | Keep | Merge into `*_ErrorPaths` |
|---|---|---|
| Logout | `_Success`, `LogoutEmitsSessionEndedForEachChildGameSession`, `LogoutFanoutContinuesAfterIndividualSessionErrors`, `LogoutProceedsWithoutFanoutWhenGetByTokenHashFails`, `LogoutProceedsWithoutFanoutWhenListByPlayerSessionFails` (fanout cases are structurally distinct) | `_SessionNotFound`, `_NotConfigured` |
| RevokePlayerSession | `_RevokesOwnOtherSession` | `_RejectsForeignSession`, `_RejectsInvalidTargetID`, `_RejectsInvalidToken` |
| RevokeOtherPlayerSessions | `_KeepsCallerDeletesRest`, `_SucceedsWithNoOtherSessions` | `_RejectsInvalidToken` (alone, stays as-is — only 1 error case) |

- [ ] **Step 1: Write `TestLogout_ErrorPaths`** (2 rows)
- [ ] **Step 2: Delete the 2 superseded Logout tests**
- [ ] **Step 3: Write `TestRevokePlayerSession_RejectsPaths`** (3 rows)
- [ ] **Step 4: Delete the 3 superseded Revoke tests**
- [ ] **Step 5: Run package tests, verify pass**

  Run: `task test -- ./internal/grpc/`
  Expected: PASS. Final auth_handlers test count: 60 → ~35.

- [ ] **Step 6: Commit checkpoint**

---

### Task 6: Roll up Phase 1 commits into a single commit

**Files:** (none modified — VCS housekeeping)

The Tasks 1-5 checkpoint commits are squashed into one named commit for the eventual PR.

- [ ] **Step 1: Verify consolidation result**

  Run: `rg -c "^func Test" internal/grpc/auth_handlers_test.go`
  Expected: ~35 (down from 60).

  Run: `rg "^// replaces:" internal/grpc/auth_handlers_test.go | wc -l`
  Expected: matches the count of `*_ErrorPaths` table-driven tests added (8-9).

- [ ] **Step 2: Squash Phase 1 checkpoints**

  Per the jj:jujutsu skill: squash all checkpoint commits introduced by Tasks 1-5 into the first one, then describe with:

  ```text
  test(grpc): consolidate auth_handlers tests via table-driven patterns (holomush-9mxr)

  Reduces auth_handlers_test.go from 60 to ~35 test functions by merging
  error-path variants into table-driven *_ErrorPaths tests. Each surviving
  test carries a // replaces: comment naming the pre-consolidation
  functions it absorbs (per spec INV-M-4).

  Pure refactor — no behavior change. Still uses MemStore; conversion to
  sessiontest.NewStore lands in Phase 3.
  ```

- [ ] **Step 3: Verify single commit exists**

  Run: `jj log -r 'trunk()..@' -T 'change_id.short() ++ " " ++ description.first_line() ++ "\n"'`
  Expected: exactly one commit since `main` for Phase 1.

---

## Phase 2 — Add `sessiontest.NewStore` helper

TDD: write the test that asserts the helper's invariants, watch it fail, implement.

### Task 7: TDD the `sessiontest.NewStore` helper

**Files:**

- Create: `internal/testsupport/sessiontest/store.go`
- Create: `internal/testsupport/sessiontest/store_test.go`

The helper wraps `testutil.SharedPostgres(t)` + `testutil.FreshDatabase(t, env)` + `pgxpool.New` + `store.NewPostgresSessionStore(pool)`. Per spec F5: **no** `//go:build integration` tag — this package is the deliberate exception.

- [ ] **Step 1: Create the package directory**

  Run: `mkdir -p internal/testsupport/sessiontest`

- [ ] **Step 2: Write the failing test**

  Create `internal/testsupport/sessiontest/store_test.go`:

  ```go
  // SPDX-License-Identifier: Apache-2.0
  // Copyright 2026 HoloMUSH Contributors

  package sessiontest_test

  import (
      "context"
      "testing"

      "github.com/oklog/ulid/v2"
      "github.com/stretchr/testify/assert"
      "github.com/stretchr/testify/require"

      "github.com/holomush/holomush/internal/session"
      "github.com/holomush/holomush/internal/testsupport/sessiontest"
  )

  // TestNewStore_ReturnsUsableSessionStore verifies the helper returns a
  // session.Store backed by real Postgres and that basic Set/Get round-trips
  // work (proves the pool wiring + schema migrations applied).
  func TestNewStore_ReturnsUsableSessionStore(t *testing.T) {
      store := sessiontest.NewStore(t)
      ctx := context.Background()

      info := &session.Info{
          ID:            "test-session-1",
          CharacterID:   ulid.Make(),
          CharacterName: "TestChar",
          Status:        session.StatusActive,
      }
      require.NoError(t, store.Set(ctx, info.ID, info))

      got, err := store.Get(ctx, info.ID)
      require.NoError(t, err)
      assert.Equal(t, info.CharacterName, got.CharacterName)
  }

  // TestNewStore_IsolatedBetweenCalls verifies INV-M-2: each NewStore call
  // returns a store backed by a fresh database. State from a prior call MUST
  // NOT be visible in a subsequent call.
  func TestNewStore_IsolatedBetweenCalls(t *testing.T) {
      ctx := context.Background()

      storeA := sessiontest.NewStore(t)
      info := &session.Info{
          ID:            "iso-test",
          CharacterID:   ulid.Make(),
          CharacterName: "InA",
          Status:        session.StatusActive,
      }
      require.NoError(t, storeA.Set(ctx, info.ID, info))

      storeB := sessiontest.NewStore(t)
      _, err := storeB.Get(ctx, info.ID)
      require.Error(t, err, "INV-M-2: storeB MUST NOT see state from storeA")
  }
  ```

- [ ] **Step 3: Run test, verify FAIL**

  Run: `task test -- ./internal/testsupport/sessiontest/`
  Expected: FAIL with `package sessiontest is not in std` or `undefined: sessiontest.NewStore`.

- [ ] **Step 4: Implement the helper**

  Create `internal/testsupport/sessiontest/store.go`:

  ```go
  // SPDX-License-Identifier: Apache-2.0
  // Copyright 2026 HoloMUSH Contributors

  // Package sessiontest provides a Postgres-backed session.Store helper for
  // unit and integration tests. It is the deliberate exception to the repo
  // convention that SharedPostgres-using tests carry //go:build integration:
  // unit tests in internal/grpc/, internal/grpc/focus/,
  // internal/command/handlers/, and internal/auth/ exercise session-touching
  // handler logic and require a real session.Store. Per the holomush-9mxr
  // design spec, this package replaces the deleted internal/session.MemStore.
  //
  // Docker is required at test runtime. Developers without Docker will see
  // testcontainers container-start errors, not compile failures — the
  // helper imports compile fine without Docker.
  package sessiontest

  import (
      "context"
      "testing"

      "github.com/jackc/pgx/v5/pgxpool"
      "github.com/stretchr/testify/require"

      "github.com/holomush/holomush/internal/session"
      "github.com/holomush/holomush/internal/store"
      "github.com/holomush/holomush/test/testutil"
  )

  // NewStore returns a session.Store backed by a fresh Postgres database
  // on the shared test container. The database is dropped via t.Cleanup
  // when the test ends (registered by testutil.FreshDatabase). Each call
  // returns a fully isolated store.
  func NewStore(t *testing.T) session.Store {
      t.Helper()

      env := testutil.SharedPostgres(t)
      connStr := testutil.FreshDatabase(t, env)

      pool, err := pgxpool.New(context.Background(), connStr)
      require.NoError(t, err, "sessiontest.NewStore: connect to fresh test database")
      t.Cleanup(pool.Close)

      return store.NewPostgresSessionStore(pool)
  }
  ```

- [ ] **Step 5: Run test, verify PASS**

  Run: `task test -- ./internal/testsupport/sessiontest/`
  Expected: PASS (both `TestNewStore_ReturnsUsableSessionStore` and `TestNewStore_IsolatedBetweenCalls`).

- [ ] **Step 6: Commit**

  Per the jj:jujutsu skill, describe the change:

  ```text
  feat(testsupport): add sessiontest.NewStore helper (holomush-9mxr)

  New package internal/testsupport/sessiontest exporting NewStore(t),
  which returns a session.Store backed by a fresh database on the
  shared Postgres testcontainer.

  Deliberately omits //go:build integration tag — this is the design
  exception per spec F5. Docker is required at runtime; absence
  surfaces as container-start errors, not compile failures.

  No callers yet; conversion lands in Phase 3.
  ```

---

## Phase 3 — Convert all `session.NewMemStore()` call sites

Replace `session.NewMemStore()` with `sessiontest.NewStore(t)` everywhere. Drift bugs surface as test failures during this phase; fix inline per the spec's "Known drift surfaces" subsection.

### Task 8: Convert `internal/grpc/auth_handlers_test.go`

**Files:**

- Modify: `internal/grpc/auth_handlers_test.go` (~35 sites after Phase 1)

- [ ] **Step 1: Add the sessiontest import**

  In the import block, add:

  ```go
  "github.com/holomush/holomush/internal/testsupport/sessiontest"
  ```

- [ ] **Step 2: Replace every `session.NewMemStore()` with `sessiontest.NewStore(t)`**

  Search-and-replace across the file. Where `t` is not in scope (the prior `session.NewMemStore()` call sites all live inside test functions with a `t *testing.T` parameter — verify this assumption holds for any subtest setup helpers).

  Note: callsites that used `session.NewMemStore()` outside of `*testing.T` scope MUST be promoted to take a `*testing.T` parameter.

- [ ] **Step 3: Remove the `session.NewMemStore` import if no other uses remain**

  Run: `rg "session\.NewMemStore" internal/grpc/auth_handlers_test.go`
  Expected: no matches. If the `session` package is only imported for `NewMemStore`, remove the import too (but `session.Status*`, `session.Info`, etc. likely keep it live — leave the import if any usage remains).

- [ ] **Step 4: Run package tests**

  Run: `task test -- ./internal/grpc/`
  Expected behavior: tests pass, OR a drift bug surfaces.

  **Drift triage:** Cross-reference any failure against the spec's "Known drift surfaces" subsection:
  - `ReattachCAS` on missing session: MemStore returned `SESSION_NOT_FOUND` error; Postgres returns `false, nil`. If a test asserted on the error, change the assertion to assert on `(false, nil)`. If the test exercised production paths that pre-`Get` (per `server.go:826`), the divergence is masked and the test works as-is.
  - Other failures: read the failing assertion, compare MemStore vs PostgresSessionStore behavior for that method, fix the test (or, if the test exposed a real Postgres bug, fix `PostgresSessionStore`).

- [ ] **Step 5: Commit checkpoint (hold for Phase 3 roll-up)**

---

### Task 9: Convert `internal/grpc/focus/*_test.go`

**Files:**

- Modify: `internal/grpc/focus/coordinator_test.go` (4 sites)
- Modify: `internal/grpc/focus/leave_by_target_test.go` (3 sites)
- Modify: `internal/grpc/focus/prefs_test.go` (2 sites)
- Modify: `internal/grpc/focus/restore_test.go` (1 site)

Apply the same conversion pattern as Task 8 to each file.

- [ ] **Step 1: Convert `coordinator_test.go`** — add `sessiontest` import; replace `session.NewMemStore()` → `sessiontest.NewStore(t)` × 4.
- [ ] **Step 2: Convert `leave_by_target_test.go`** — same pattern × 3.
- [ ] **Step 3: Convert `prefs_test.go`** — same pattern × 2.
- [ ] **Step 4: Convert `restore_test.go`** — same pattern × 1.
- [ ] **Step 5: Run package tests**

  Run: `task test -- ./internal/grpc/focus/`
  Expected: PASS, or drift triage per Task 8 Step 4.

- [ ] **Step 6: Commit checkpoint**

---

### Task 10: Convert remaining `internal/grpc/` test files

**Files:**

- Modify: `internal/grpc/dispatcher_test.go` (3 sites)
- Modify: `internal/grpc/test_helpers_test.go` (3 sites)
- Modify: `internal/grpc/server_helpers_test.go` (1 site)
- Modify: `internal/grpc/location_follow_test.go` (2 sites)

- [ ] **Step 1: Convert all four files** (same pattern as Task 8/9).
- [ ] **Step 2: Run package tests**

  Run: `task test -- ./internal/grpc/`
  Expected: PASS.

- [ ] **Step 3: Commit checkpoint**

---

### Task 11: Convert `internal/command/handlers/` and `internal/auth/` test files

**Files:**

- Modify: `internal/command/handlers/resetpassword_test.go` (2 sites)
- Modify: `internal/command/handlers/plugin_admin_test.go` (1 site)
- Modify: `internal/auth/session_ownership_test.go` (1 site)

- [ ] **Step 1: Convert all three files.**
- [ ] **Step 2: Run package tests**

  Run: `task test -- ./internal/command/handlers/ ./internal/auth/`
  Expected: PASS.

- [ ] **Step 3: Commit checkpoint**

---

### Task 12: Fix `test/integration/phase1_5_test.go` (incongruous MemStore usage)

**Files:**

- Modify: `test/integration/phase1_5_test.go:497` and the 2 other MemStore sites in the file

The file is an integration test (`//go:build integration`) but uses MemStore as the session store, which contradicts the test's purpose. Per the spec's commit-3 description ("routes through the real store path"), the preferred conversion target is `internal/testsupport/integrationtest.Start(t)`. Use `sessiontest.NewStore(t)` only as a fallback if the test wires a `CoreServer` directly via constructor injection in a way that doesn't fit the integrationtest harness shape.

- [ ] **Step 1: Read the existing usage in context**

  Run: `rg -B5 -A10 "session.NewMemStore" test/integration/phase1_5_test.go`
  Read the surrounding `CoreServer` setup at `test/integration/phase1_5_test.go:497` to determine whether `integrationtest.Start(t)` fits cleanly (preferred) or whether `sessiontest.NewStore(t)` is the smaller delta (fallback).

- [ ] **Step 2: Convert (preferring integrationtest.Start)**

  If `integrationtest.Start(t)` fits: replace the `CoreServer` literal construction with a `ts := integrationtest.Start(t)` call and use `ts.<accessors>` to interact with the running stack.

  If the existing test shape resists that conversion (e.g., the test asserts on internal `CoreServer` field state that the harness doesn't expose): replace `session.NewMemStore()` with `sessiontest.NewStore(t)` and leave the rest of the test structure intact. Document the choice in the commit message.

- [ ] **Step 3: Run the integration test**

  Run: `task test:int -- ./test/integration/`
  Expected: PASS (or the specific suite passes; the full suite is acceptable).

- [ ] **Step 4: Commit checkpoint**

---

### Task 13: Rewire `internal/session/focus_mutator_test.go`

**Files:**

- Modify: `internal/session/focus_mutator_test.go` (49 lines)

This test exercises `FocusMutator` invariants using `session.FocusMutator` directly (no `session.Store` instantiation). Plan-time audit (probe-extract of `focus_mutator_test.go:1-49`) confirmed zero `MemStore` references in the file — the test stands alone when MemStore is deleted in Phase 4. This task verifies that audit still holds and runs the test in isolation as a smoke check before deletion.

- [ ] **Step 1: Re-verify zero MemStore references**

  Run: `rg "MemStore|NewMemStore" internal/session/focus_mutator_test.go`
  Expected: no matches.

- [ ] **Step 2: Run the test in isolation**

  Run: `task test -- -run TestFocusMutator ./internal/session/`
  Expected: PASS.

- [ ] **Step 3: Commit checkpoint**

---

### Task 14: Phase 3 commit roll-up

**Files:** (none modified — VCS housekeeping)

- [ ] **Step 1: Verify all conversion sites complete**

  Run: `rg "session\.NewMemStore" .`
  Expected: matches ONLY in `internal/session/memstore.go` (the constructor) and `internal/session/memstore_test.go` (the package-local tests). All caller sites converted.

- [ ] **Step 2: Run full unit suite**

  Run: `task test`
  Expected: PASS.

- [ ] **Step 3: Run integration suite**

  Run: `task test:int`
  Expected: PASS.

- [ ] **Step 4: Squash Phase 3 checkpoints**

  Squash Tasks 8-13 into one commit:

  ```text
  refactor: convert session.Store unit tests from MemStore to sessiontest.NewStore (holomush-9mxr)

  Replaces session.NewMemStore() with sessiontest.NewStore(t) across 12
  test files (~50 sites after Phase 1 consolidation). Drift bugs found
  and addressed inline.

  Also fixes test/integration/phase1_5_test.go's incongruous MemStore-
  inside-integration-test usage by routing through the real store path.

  focus_mutator_test.go survives unchanged; its assertions exercise
  FocusMutator semantics that live in session.go, not memstore.go.
  ```

---

## Phase 4 — Delete `MemStore`

### Task 15: Pre-deletion verification

**Files:** (read-only audit)

- [ ] **Step 1: Confirm INV-M-3 has test coverage**

  Run: `rg "validClientTypes|invalid client_type" internal/store/session_store_test.go 2>/dev/null`

  If no match, add a test to `internal/store/session_store_test.go` covering the `AddConnection` rejection path:

  ```go
  func TestPostgresSessionStore_AddConnectionRejectsInvalidClientType(t *testing.T) {
      mock, err := pgxmock.NewPool()
      require.NoError(t, err)
      defer mock.Close()

      s := store.NewPostgresSessionStore(mock)
      err = s.AddConnection(context.Background(), &session.Connection{
          ID:         ulid.Make(),
          SessionID:  "sess-1",
          ClientType: "websocket",  // not in validClientTypes
        })

      require.Error(t, err)
      assert.Contains(t, err.Error(), "invalid client_type")
  }
  ```

  (If the test file does not exist, create it; follow `player_session_store_test.go` for the package + imports template.)

- [ ] **Step 2: Run the verification test**

  Run: `task test -- ./internal/store/`
  Expected: PASS.

- [ ] **Step 3: Commit the INV-M-3 test if added**

---

### Task 16: Delete `MemStore` files

**Files:**

- Delete: `internal/session/memstore.go`
- Delete: `internal/session/memstore_test.go`

- [ ] **Step 1: Delete the implementation file**

  Run: `rm internal/session/memstore.go`

- [ ] **Step 2: Delete the package-local test file**

  Run: `rm internal/session/memstore_test.go`

- [ ] **Step 3: Verify no residual MemStore references**

  Run: `rg "MemStore" internal/`
  Expected: no matches.

  Run: `rg "MemStore" .`
  Expected: no matches outside of `docs/` (the spec + plan reference it historically — that's fine; only `internal/` is the production scope).

- [ ] **Step 4: Run full unit + integration suite**

  Run: `task test`
  Then: `task test:int`
  Expected: BOTH PASS.

- [ ] **Step 5: Commit**

  ```text
  chore(session): delete MemStore (holomush-9mxr)

  Removes internal/session/memstore.go (552 lines) and
  internal/session/memstore_test.go (856 lines). session.Store now
  has exactly one implementation: store.PostgresSessionStore.

  Satisfies INV-M-1 (one impl) and the rg "MemStore" internal/
  acceptance gate (no matches).

  focus_mutator_test.go is unaffected — it tests FocusMutator
  semantics from session.go, not MemStore.
  ```

---

## Phase 5 — Document the convention shift

### Task 17: Update `CLAUDE.md` Testing section

**Files:**

- Modify: `CLAUDE.md` (Testing section)

- [ ] **Step 1: Locate the Testing section**

  Run: `rg -n "^## Testing" CLAUDE.md`
  Read the section and the surrounding paragraphs that document the `task test` / `task test:int` split and the integration build tag.

- [ ] **Step 2: Add a paragraph about the sessiontest exception**

  Insert after the existing "Always-on rule" table, before the next subsection:

  ```markdown
  ### Session-store testing (Docker required)

  Tests in `internal/grpc/`, `internal/grpc/focus/`, `internal/command/handlers/`,
  and `internal/auth/` that exercise `session.Store`-touching handler logic
  require Docker for `task test`. These tests use the
  `internal/testsupport/sessiontest.NewStore(t)` helper, which is the
  deliberate exception to the "SharedPostgres tests MUST be `//go:build integration`"
  convention. See [docs/superpowers/specs/2026-05-23-remove-session-memstore-design.md](docs/superpowers/specs/2026-05-23-remove-session-memstore-design.md)
  for rationale.

  | Requirement | Description |
  | --- | --- |
  | **MUST** use `sessiontest.NewStore(t)` | For any test that needs a `session.Store` — never roll your own. |
  | **MUST NOT** add `//go:build integration` | The package is the deliberate exception. |
  | **MUST** have Docker running locally | Failures without Docker surface as testcontainers container-start errors at test-runtime. |
  ```

- [ ] **Step 3: Run markdown lint**

  Run: `task lint:markdown`
  Expected: PASS.

- [ ] **Step 4: Commit checkpoint**

---

### Task 18: Update `site/docs/contributing/integration-tests.md`

**Files:**

- Modify: `site/docs/contributing/integration-tests.md`

- [ ] **Step 1: Locate the section discussing SharedPostgres + integration build tag**

  Run: `rg -n "SharedPostgres|//go:build integration" site/docs/contributing/integration-tests.md`
  Read the surrounding context to find the natural insertion point.

- [ ] **Step 2: Add a callout about the sessiontest exception**

  Insert a short subsection mirroring the CLAUDE.md addition but with cross-references appropriate for the public-facing docs. Link to the spec at `docs/superpowers/specs/2026-05-23-remove-session-memstore-design.md`.

- [ ] **Step 3: Run docs build to verify links**

  Run: `task docs:build`
  Expected: PASS (no broken-link errors for the new spec link).

- [ ] **Step 4: Commit Phase 5 roll-up**

  Squash Tasks 17-18 if separated:

  ```text
  docs: note Docker requirement for session-store unit tests (holomush-9mxr)

  Documents the convention exception: internal/testsupport/sessiontest
  is unit-tagged but requires Docker, deliberately breaking the
  "SharedPostgres = //go:build integration" repo pattern.

  Updates CLAUDE.md Testing section and
  site/docs/contributing/integration-tests.md with cross-references
  to the design spec.
  ```

---

## Final verification (pre-push gate)

- [ ] **Step 1: Run the full PR-prep pipeline**

  Run: `task pr-prep`
  Expected: GREEN. This mirrors CI; never approximate by running subsets.

- [ ] **Step 2: Verify the four acceptance criteria**

  | Check | Command | Expected |
  |---|---|---|
  | INV-M-1 | `rg "_ session\.Store = " internal/` | One match (`store/session_store.go`) |
  | INV-M-2 | (covered by `sessiontest/store_test.go` `TestNewStore_IsolatedBetweenCalls`) | Test passes |
  | INV-M-3 | (covered by `store/session_store_test.go` `TestPostgresSessionStore_AddConnectionRejectsInvalidClientType`) | Test passes |
  | INV-M-4 | (covered by `// replaces:` comment chain across Phase 1 consolidations) | Reviewer attests via `jj diff @--..@-` against Phase 1's first commit |
  | Acceptance | `rg "MemStore" internal/` | No matches |
  | Acceptance | `rg "session\.NewMemStore" .` | No matches |

- [ ] **Step 3: Push (per landing-the-plane)**

  Follow `.claude/rules/landing-the-plane.md` — pre-push rebase per the `jj:jujutsu` skill, set bookmark, `jj git push --branch memstore-removal`.

- [ ] **Step 4: Open the PR**

  Reference design spec + plan + bead `holomush-9mxr` in the PR description.

---

## Out-of-scope follow-ups

Per the spec, these are NOT addressed:

- `session.Store.ListByPlayer` TODO (interface-level question)
- Per-package pool sharing if inner-loop slowdown is unacceptable
- A genuine `storetest.Run(t, ctor)` conformance suite if a third `session.Store` impl is ever proposed

<!-- adr-capture: sha256=75ada270209e6d44; session=brainstorm-9mxr; ts=2026-05-23T13:17:32Z; adrs=holomush-bozv -->
