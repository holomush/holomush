<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Phase 7.6: Call Site Migration & Cleanup

> **[Back to Overview](./2026-02-06-full-abac-implementation.md)** | **[Previous: Phase 7.5](./2026-02-06-full-abac-phase-7.5.md)** | **[Next: Phase 7.7](./2026-02-06-full-abac-phase-7.7.md)**

## Task 28: Migrate to AccessPolicyEngine (atomic per-package migration)

**Spec References:** [07-migration-seeds.md#implementation-sequence](../specs/abac/07-migration-seeds.md#implementation-sequence), ADR 0014 (Direct replacement, no adapter)

**Dependencies:**

- Task 17.4 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)) (deny-overrides + integration) — AccessPolicyEngine must be fully operational before migrating call sites
- Task 22b ([Phase 7.4](./2026-02-06-full-abac-phase-7.4.md)) (resolve seed policy gaps) — seed policy coverage gaps must be resolved before migration ([Decision #94](../specs/decisions/epic7/phase-7.4/094-seed-gap-resolution-before-migration.md))
- Task 23c ([Phase 7.4](./2026-02-06-full-abac-phase-7.4.md)) (smoke test gate) — smoke tests must validate seed policy behavior before migration proceeds

**Migration Strategy:**

This task uses **5 atomic commits** (T28-pkg1 through T28-pkg5), each covering specific packages and call sites. Each commit compiles and passes `task build`. The world service package (24 call sites) is decomposed into 3 sub-commits by domain to reduce review complexity and blast radius.

**Acceptance Criteria:**

- [ ] Per-package atomic migration: DI wiring + call sites migrated together, each commit compiles and passes `task build`
- [ ] `AccessControl` replaced with `*policy.Engine` in dependency graph
- [ ] `AttributeResolver` wired with all registered providers
- [ ] `PolicyCache` wired and `Listen()` called for NOTIFY subscription
- [ ] `SessionResolver` wired
- [ ] `AuditLogger` wired
- [ ] `Bootstrap()` called at startup to seed policies
- [ ] **Security (S1):** API ingress validation added to prevent external requests from using system subject or `WithSystemSubject(ctx)` bypass mechanism
- [ ] **Security (S8 - holomush-5k1.355):** A static analysis rule or go vet check MUST verify no remaining `AccessControl.Check()` calls post-migration. CI MUST enforce this check. Migration checklist MUST include per-site verification.
- [ ] ALL **28 production call sites** migrated from `AccessControl.Check()` to `engine.Evaluate()`:
  - [ ] 3 call sites in command package (T28-pkg1)
  - [ ] 6 call sites in world: locations (T28-pkg2)
  - [ ] 11 call sites in world: exits + objects (T28-pkg3)
  - [ ] 7 call sites in world: characters + scenes (T28-pkg4)
  - [ ] 1 call site in plugin package (T28-pkg5)
- [ ] ALL **57 test call sites** in `internal/access/static_test.go` migrated to new engine
- [ ] Generated mocks regenerated with mockery
- [ ] Each call site uses `types.AccessRequest{Subject, Action, Resource}` struct
- [ ] Error handling: `Evaluate()` error → fail-closed (deny), logged via slog
- [ ] All subject strings use `character:` prefix (not legacy `char:`)
- [ ] **All migrated call sites MUST use `SubjectCharacter` constant, not `char:` prefix** (per ADR #13 prefix normalization)
- [ ] **Static analysis check for remaining `char:` prefix usage** — verify no call sites use legacy `char:` prefix after migration
- [ ] Prefix migration strategy: EventStore accepts both `char:` and `character:` prefixes during transition (backward compatibility for existing event stream names)
- [ ] Audit logs are immutable: old entries keep `char:` prefix, new entries use `character:` prefix
- [ ] Tests verify both prefix variants are accepted by EventStore during migration period
- [ ] Tests updated to mock `AccessPolicyEngine` instead of `AccessControl`
- [ ] **Per-package error-path tests:** Each migrated package MUST have tests verifying:
  1. Correct `AccessRequest` construction (subject, action, resource populated)
  2. `decision.IsAllowed()=false` handling (deny path returns error/fails operation)
  3. `Evaluate()` error handling (error != nil → fail-closed, operation denied, error logged)
- [ ] Tests pass incrementally after each package sub-commit
- [ ] 5 atomic commits: T28-pkg1, T28-pkg2, T28-pkg3, T28-pkg4, T28-pkg5
- [ ] `task test` passes after all migrations
- [ ] No commits with intentional build breakage
- [ ] Rollback strategy documented ([Decision #65](../specs/decisions/epic7/phase-7.6/065-git-revert-migration-rollback.md)): `git revert` of Task 28 commit(s) restores `AccessControl.Check()` call sites
- [ ] Database rollback procedure documented ([Decision #65](../specs/decisions/epic7/phase-7.6/065-git-revert-migration-rollback.md#database-migration-rollback-procedure)) covering all 3 migration files (000015, 000016, 000017) with down-migration order, backup requirements, and data recovery steps

**Files:**

- Modify: `cmd/holomush/main.go` (or server bootstrap file) — DI wiring
- Modify: `internal/command/dispatcher.go` — Command execution authorization
- Modify: `internal/command/rate_limit_middleware.go` — Rate limit bypass for admins
- Modify: `internal/command/handlers/boot.go` — Boot command permission check
- Modify: `internal/world/service.go` — World model operation authorization
- Modify: `internal/plugin/hostfunc/commands.go` — Plugin command execution auth
- Modify: `internal/core/broadcaster_test.go` — Test mock injection

**Key files include (non-exhaustive)** — run `grep -r "AccessControl" internal/ --include="*.go" -l` for the authoritative list.

**Migration strategy:**

Migrate per-package, with each commit including both DI wiring changes and all call site updates for that package. This ensures every commit compiles and passes `task build`.

**Compound resource decomposition:**

The codebase uses compound resource strings like `fmt.Sprintf("location:%s:characters", locationID.String())` at `internal/world/service.go:470`. These compound formats break the engine's prefix parser, which splits on the first `:` producing `type=location`, `id=01ABC:characters` — which is incorrect. During migration, decompose compound resources into separate action and resource fields:

- **OLD:** `resource = "location:<id>:characters"`
- **NEW:** `resource = "location:<id>"`, `action = "list_characters"`

Rationale: The ABAC model separates concerns — the resource identifies _what_ is being accessed, the action identifies _how_. "List characters in a location" is naturally `action=list_characters, resource=location:<id>`. This decomposition avoids parser complexity and aligns with the ABAC model.

See ADR #76 (Compound Resource Decomposition During Migration) for full decision rationale.

**Call site counts verified (as of 2026-02-07):**

- Production: 28 call sites (3 in command, 24 in world, 1 in plugin)
- Tests: 57 call sites in `internal/access/static_test.go` + ~20 in `internal/plugin/capability/enforcer_test.go` (Task 29)

### T28-pkg1: Command Package (3 call sites)

**Atomic commit:** `refactor(command): migrate command package to AccessPolicyEngine`

**Call sites (3):**

- `internal/command/dispatcher.go` (1 call)
- `internal/command/rate_limit_middleware.go` (1 call)
- `internal/command/handlers/boot.go` (1 call)

**Steps:**

1. Update DI wiring in `cmd/holomush/main.go` to wire `*policy.Engine` for command package dependencies
2. Update 3 production call sites using the migration pattern (see "Call site migration pattern" section)
3. Update tests to mock `AccessPolicyEngine` instead of `AccessControl`
4. **Per-package error-path tests:** Add tests verifying `AccessRequest` construction, `decision.IsAllowed()=false` handling, and `Evaluate()` error handling
5. Run `task test` — MUST PASS
6. Run `task build` — MUST PASS
7. Commit

### T28-pkg2: World Service - Locations (6 call sites)

**Atomic commit:** `refactor(world): migrate location operations to AccessPolicyEngine`

**Call sites (6):**

- `GetLocation` (`internal/world/service.go:74`)
- `CreateLocation` (`internal/world/service.go:94`)
- `UpdateLocation` (`internal/world/service.go:123`)
- `DeleteLocation` (`internal/world/service.go:144`)
- `FindLocationByName` (`internal/world/service.go:796`)
- `ExamineLocation` (`internal/world/service.go:655`)

**Steps:**

1. Update DI wiring in `cmd/holomush/main.go` to wire `*policy.Engine` for world package dependencies (if not done in pkg1)
2. Update 6 location-related call sites in `internal/world/service.go`
3. Update tests to mock `AccessPolicyEngine`
4. **Per-package error-path tests:** Add tests verifying correct behavior for location operations
5. Run `task test` — MUST PASS
6. Run `task build` — MUST PASS
7. Commit

### T28-pkg3: World Service - Exits & Objects (11 call sites)

**Atomic commit:** `refactor(world): migrate exit and object operations to AccessPolicyEngine`

**Call sites - Exits (5):**

- `GetExit` (`internal/world/service.go:162`)
- `CreateExit` (`internal/world/service.go:185`)
- `UpdateExit` (`internal/world/service.go:217`)
- `DeleteExit` (`internal/world/service.go:241`)
- `GetExitsByLocation` (`internal/world/service.go:279`)

**Call sites - Objects (6):**

- `GetObject` (`internal/world/service.go:295`)
- `CreateObject` (`internal/world/service.go:315`)
- `UpdateObject` (`internal/world/service.go:347`)
- `DeleteObject` (`internal/world/service.go:371`)
- `MoveObject` (`internal/world/service.go:401`)
- `ExamineObject` (`internal/world/service.go:713`)

**Steps:**

1. Update 11 call sites (5 exits + 6 objects) in `internal/world/service.go`
2. Update tests to mock `AccessPolicyEngine`
3. **Per-package error-path tests:** Add tests verifying correct behavior for exit and object operations
4. Run `task test` — MUST PASS
5. Run `task build` — MUST PASS
6. Commit

### T28-pkg4: World Service - Characters & Scenes (7 call sites)

**Atomic commit:** `refactor(world): migrate character and scene operations to AccessPolicyEngine`

**Call sites - Characters (4):**

- `GetCharacter` (`internal/world/service.go:452`)
- `GetCharactersByLocation` (`internal/world/service.go:471`) — **NOTE:** Compound resource decomposition required
- `MoveCharacter` (`internal/world/service.go:558`)
- `ExamineCharacter` (`internal/world/service.go:768`)

**Call sites - Scenes (3):**

- `AddSceneParticipant` (`internal/world/service.go:488`)
- `RemoveSceneParticipant` (`internal/world/service.go:509`)
- `ListSceneParticipants` (`internal/world/service.go:527`)

**Steps:**

1. Update 7 call sites (4 characters + 3 scenes) in `internal/world/service.go`
   - **CRITICAL:** `service.go:471` (`GetCharactersByLocation`) uses compound resource format `location:<id>:characters`. Decompose to `resource=location:<id>` with `action=list_characters` per ADR #76
2. Update tests to mock `AccessPolicyEngine`
3. **Per-package error-path tests:** Add tests verifying correct behavior for character and scene operations, including compound resource decomposition test
4. Run `task test` — MUST PASS
5. Run `task build` — MUST PASS
6. Commit

### T28-pkg5: Plugin Package & Final Wiring (1 call site + bootstrap)

**Atomic commit:** `refactor(plugin): migrate plugin host functions and complete ABAC wiring`

**Call sites (1):**

- `internal/plugin/hostfunc/commands.go` (1 call to `f.access.Check()`)

**Steps:**

1. Update 1 production call site in `internal/plugin/hostfunc/commands.go`
   - Note: The `AccessControl` field is defined in `functions.go` but used in `commands.go`
2. Update `internal/plugin/hostfunc/functions.go` to change field type from `AccessControl` to `*policy.Engine`
3. Update `internal/plugin/hostfunc/commands.go` to change `AccessControl` interface declaration to use `*policy.Engine`
4. Update `WithAccessControl()` option to accept `*policy.Engine` instead of `AccessControl` interface
5. Add `Bootstrap()` call at startup to seed policies
6. Add `PolicyCache.Listen()` for NOTIFY subscription
7. Update tests to mock `AccessPolicyEngine`
8. **Per-package error-path tests:** Add tests verifying correct behavior for plugin command execution
9. Run `task test` — MUST PASS
10. Run `task build` — MUST PASS
11. Commit

**Call site migration pattern:**

For each file, replace:

```go
// OLD:
allowed := ac.Check(ctx, subject, action, resource)
if !allowed {
    // existing denial handling
}
```

With:

```go
// NEW:
decision, err := engine.Evaluate(ctx, types.AccessRequest{
    Subject:  subject,
    Action:   action,
    Resource: resource,
})
if err != nil {
    slog.Error("access evaluation failed", "error", err)
    // Fail-closed: deny on error
}
if !decision.IsAllowed() {
    // existing denial handling
}
```

Ensure all subject strings use `character:` prefix (not legacy `char:`).

**Error-path test template:**

Every migrated package MUST include tests verifying fail-closed behavior:

```go
func TestAccessPolicyEngine_ErrorHandling(t *testing.T) {
    tests := []struct {
        name       string
        setupMock  func(*mocks.MockAccessPolicyEngine)
        expectDeny bool
        expectLog  bool
    }{
        {
            name: "deny decision prevents operation",
            setupMock: func(m *mocks.MockAccessPolicyEngine) {
                m.EXPECT().Evaluate(mock.Anything, mock.Anything).Return(
                    types.NewDecision(types.EffectDeny, "policy denied", "test-policy-123"),
                    nil,
                )
            },
            expectDeny: true,
            expectLog:  false,
        },
        {
            name: "evaluation error fails closed",
            setupMock: func(m *mocks.MockAccessPolicyEngine) {
                m.EXPECT().Evaluate(mock.Anything, mock.Anything).Return(
                    types.NewDecision(types.EffectDefaultDeny, "evaluation error", ""),
                    errors.New("attribute resolution failed"),
                )
            },
            expectDeny: true,
            expectLog:  true, // Error MUST be logged
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            mockEngine := mocks.NewMockAccessPolicyEngine(t)
            tt.setupMock(mockEngine)

            // Call the operation under test
            err := operationUnderTest(ctx, mockEngine)

            if tt.expectDeny {
                assert.Error(t, err, "operation should be denied")
            }
            if tt.expectLog {
                // Verify error was logged (implementation-specific)
            }
        })
    }
}
```

**AccessRequest construction verification:**

Each test MUST verify correct request construction:

```go
func TestAccessRequest_Construction(t *testing.T) {
    mockEngine := mocks.NewMockAccessPolicyEngine(t)

    // Capture the AccessRequest passed to Evaluate()
    var capturedRequest types.AccessRequest
    mockEngine.EXPECT().Evaluate(mock.Anything, mock.MatchedBy(func(req types.AccessRequest) bool {
        capturedRequest = req
        return true
    })).Return(types.NewDecision(types.EffectAllow, "test allowed", "test-policy"), nil)

    // Perform operation
    _ = operationUnderTest(ctx, mockEngine)

    // Verify request fields
    assert.Equal(t, "character:01ABC", capturedRequest.Subject)
    assert.Equal(t, "read", capturedRequest.Action)
    assert.Equal(t, "location:01XYZ", capturedRequest.Resource)
}
```

**Rollback Strategy:**

If serious issues are discovered after Task 28 migration, rollback is performed via `git revert` and down migrations (documented in [Decision #65](../specs/decisions/epic7/phase-7.6/065-git-revert-migration-rollback.md)):

1. **Revert Task 28 commit(s)** — This restores all 28 `AccessControl.Check()` call sites and removes `AccessPolicyEngine.Evaluate()` wiring. Each package migration commit (Package 1-4) can be reverted independently or together.
2. **Run database down migrations** — Roll back 3 migration files in reverse order (000017 → 000016 → 000015) to remove ABAC tables. See [Decision #65 Database Migration Rollback Procedure](../specs/decisions/epic7/phase-7.6/065-git-revert-migration-rollback.md#database-migration-rollback-procedure) for detailed steps including required backup procedures and down-migration order.
3. **Do NOT revert Task 29** — Task 29 removes code that still exists at Task 28. If Task 28 is reverted, Task 29's commit should not exist yet (it depends on Task 28 completion). If Task 29 has already been committed, it MUST be reverted first before reverting Task 28.

**Pre-Rollback Requirements:**

- [ ] **CRITICAL:** Full database backup taken (`pg_dump` of entire database)
- [ ] ABAC tables exported separately for faster restore if needed
- [ ] Policy data exported as CSV for manual recreation
- [ ] Backup integrity verified via test restore to temporary database

**Partial Migration Rollback:** If migration fails after N of M packages are migrated: (1) identify migrated packages via import path check (new package uses access.Check()), (2) each package migration is atomic — unmigrated packages continue using old interface, (3) resume migration from the failed package after fixing the issue.

**Rationale:** No feature flag or adapter layer exists (per Decision #36 and Decision #37 — no adapter, no shadow mode). The migration is a direct replacement with comprehensive test coverage as the safety net. Git revert provides the code rollback path, and down migrations provide the database rollback path (per [Decision #65](../specs/decisions/epic7/phase-7.6/065-git-revert-migration-rollback.md)).

---

### Task 28.5: Migration Equivalence Testing

**Spec References:** Review Finding H5 (Architectural Recommendation) — No test validates identical authorization decisions between old StaticAccessControl and new ABAC engine

**Dependencies:**

- Task 28 (migrate to AccessPolicyEngine) — all call sites must be migrated before equivalence testing validates the migration

**Acceptance Criteria:**

- [ ] Equivalence test suite created covering all ~28 production call sites
- [ ] Test validates identical decisions between `StaticAccessControl.Check()` and `AccessPolicyEngine.Evaluate()` for same inputs
- [ ] Test covers all unique subject/action/resource combinations from production call sites:
  - [ ] Command dispatcher authorization (1 call site)
  - [ ] Rate limit bypass checks (1 call site)
  - [ ] Boot command permission (1 call site)
  - [ ] World service operations (24 call sites covering all world actions)
  - [ ] Plugin command execution (1 call site)
- [ ] Test uses table-driven approach with request sets for each call site pattern
- [ ] Test instantiates both engines with identical bootstrap data (roles, permissions, policies)
- [ ] Test verifies `Check()` return value matches `decision.IsAllowed()` for every request
- [ ] Any behavioral differences documented with justification in test comments
- [ ] Test fails explicitly if decisions diverge without documented justification
- [ ] Test runs as part of `task test` during migration phase
- [ ] Test covers compound resource decomposition cases (e.g., `location:<id>:characters` → `action=list_characters, resource=location:<id>`)

**Files:**

- Create: `internal/access/migration_equivalence_test.go`

**Test Structure:**

```go
//go:build integration

package access_test

import (
    "context"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/holomush/holomush/internal/access"
    "github.com/holomush/holomush/internal/access/policy"
    "github.com/holomush/holomush/internal/access/types"
)

// TestMigrationEquivalence validates that StaticAccessControl and AccessPolicyEngine
// produce identical authorization decisions for all production call sites.
func TestMigrationEquivalence(t *testing.T) {
    ctx := context.Background()

    // Bootstrap both engines with identical data
    staticEngine := bootstrapStaticEngine(t)
    policyEngine := bootstrapPolicyEngine(t)

    tests := []struct {
        name     string
        subject  string
        action   string
        resource string
        comment  string // Document expected divergence if any
    }{
        // === Command package (3 call sites) ===

        // #1: Command dispatcher (internal/command/dispatcher.go:157)
        {
            name:     "command execution - admin executes privileged command",
            subject:  "character:admin-01ABC",
            action:   "execute",
            resource: "command:shutdown",
        },
        {
            name:     "command execution - player executes basic command",
            subject:  "character:player-01DEF",
            action:   "execute",
            resource: "command:look",
        },

        // #2: Rate limit bypass (internal/command/rate_limit_middleware.go:39)
        {
            name:     "rate limit bypass - admin bypasses rate limit",
            subject:  "character:admin-01ABC",
            action:   "execute",
            resource: "capability:rate_limit_bypass",
        },
        {
            name:     "rate limit bypass - player denied bypass",
            subject:  "character:player-01DEF",
            action:   "execute",
            resource: "capability:rate_limit_bypass",
        },

        // #3: Boot command (internal/command/handlers/boot.go:52)
        {
            name:     "boot command - admin boots player",
            subject:  "character:admin-01ABC",
            action:   "execute",
            resource: "admin.boot",
        },
        {
            name:     "boot command - player denied boot",
            subject:  "character:player-01DEF",
            action:   "execute",
            resource: "admin.boot",
        },

        // === World service - Location operations (6 call sites) ===

        // #4: GetLocation (internal/world/service.go:74)
        {
            name:     "get location - player reads location",
            subject:  "character:player-01DEF",
            action:   "read",
            resource: "location:01JKL",
        },

        // #5: CreateLocation (internal/world/service.go:94)
        {
            name:     "create location - builder creates location",
            subject:  "character:builder-01GHI",
            action:   "write",
            resource: "location:*",
        },
        {
            name:     "create location - player denied",
            subject:  "character:player-01DEF",
            action:   "write",
            resource: "location:*",
        },

        // #6: UpdateLocation (internal/world/service.go:123)
        {
            name:     "update location - builder updates location",
            subject:  "character:builder-01GHI",
            action:   "write",
            resource: "location:01JKL",
        },

        // #7: DeleteLocation (internal/world/service.go:144)
        {
            name:     "delete location - admin deletes location",
            subject:  "character:admin-01ABC",
            action:   "delete",
            resource: "location:01JKL",
        },
        {
            name:     "delete location - builder denied",
            subject:  "character:builder-01GHI",
            action:   "delete",
            resource: "location:01JKL",
        },

        // #8: FindLocationByName (internal/world/service.go:796)
        {
            name:     "find location by name - player searches locations",
            subject:  "character:player-01DEF",
            action:   "read",
            resource: "location:*",
        },

        // #9: ExamineLocation (internal/world/service.go:655)
        {
            name:     "examine location - player examines location",
            subject:  "character:player-01DEF",
            action:   "read",
            resource: "location:01JKL",
        },

        // === World service - Exit operations (5 call sites) ===

        // #10: GetExit (internal/world/service.go:162)
        {
            name:     "get exit - player reads exit",
            subject:  "character:player-01DEF",
            action:   "read",
            resource: "exit:01STU",
        },

        // #11: CreateExit (internal/world/service.go:185)
        {
            name:     "create exit - builder creates exit",
            subject:  "character:builder-01GHI",
            action:   "write",
            resource: "exit:*",
        },

        // #12: UpdateExit (internal/world/service.go:217)
        {
            name:     "update exit - builder updates exit",
            subject:  "character:builder-01GHI",
            action:   "write",
            resource: "exit:01STU",
        },

        // #13: DeleteExit (internal/world/service.go:241)
        {
            name:     "delete exit - admin deletes exit",
            subject:  "character:admin-01ABC",
            action:   "delete",
            resource: "exit:01STU",
        },

        // #14: GetExitsByLocation (internal/world/service.go:279)
        {
            name:     "get exits by location - player lists exits from location",
            subject:  "character:player-01DEF",
            action:   "read",
            resource: "location:01JKL",
        },

        // === World service - Object operations (6 call sites) ===

        // #15: GetObject (internal/world/service.go:295)
        {
            name:     "get object - player reads object",
            subject:  "character:player-01DEF",
            action:   "read",
            resource: "object:01VWX",
        },

        // #16: CreateObject (internal/world/service.go:315)
        {
            name:     "create object - builder creates object",
            subject:  "character:builder-01GHI",
            action:   "write",
            resource: "object:*",
        },

        // #17: UpdateObject (internal/world/service.go:347)
        {
            name:     "update object - builder updates object",
            subject:  "character:builder-01GHI",
            action:   "write",
            resource: "object:01VWX",
        },

        // #18: DeleteObject (internal/world/service.go:371)
        {
            name:     "delete object - admin deletes object",
            subject:  "character:admin-01ABC",
            action:   "delete",
            resource: "object:01VWX",
        },

        // #19: MoveObject (internal/world/service.go:401)
        {
            name:     "move object - player moves object",
            subject:  "character:player-01DEF",
            action:   "write",
            resource: "object:01VWX",
        },

        // #20: ExamineObject (internal/world/service.go:713)
        {
            name:     "examine object - player examines object",
            subject:  "character:player-01DEF",
            action:   "read",
            resource: "object:01VWX",
        },

        // === World service - Character operations (5 call sites) ===

        // #21: GetCharacter (internal/world/service.go:452)
        {
            name:     "get character - player reads character",
            subject:  "character:player-01DEF",
            action:   "read",
            resource: "character:01YZA",
        },

        // #22: GetCharactersByLocation (internal/world/service.go:471)
        {
            name:     "get characters by location - decomposed compound resource",
            subject:  "character:player-01DEF",
            action:   "list_characters",
            resource: "location:01JKL",
            comment:  "Compound resource decomposed per ADR #76: OLD format was location:<id>:characters",
        },

        // #23: MoveCharacter (internal/world/service.go:558)
        {
            name:     "move character - player moves own character",
            subject:  "character:player-01DEF",
            action:   "write",
            resource: "character:01DEF",
        },

        // #24: ExamineCharacter (internal/world/service.go:768)
        {
            name:     "examine character - player examines another character",
            subject:  "character:player-01DEF",
            action:   "read",
            resource: "character:01YZA",
        },

        // === World service - Scene operations (3 call sites) ===

        // #25: AddSceneParticipant (internal/world/service.go:488)
        {
            name:     "add scene participant - player adds participant",
            subject:  "character:player-01DEF",
            action:   "write",
            resource: "scene:01PQR",
        },

        // #26: RemoveSceneParticipant (internal/world/service.go:509)
        {
            name:     "remove scene participant - player removes participant",
            subject:  "character:player-01DEF",
            action:   "write",
            resource: "scene:01PQR",
        },

        // #27: ListSceneParticipants (internal/world/service.go:527)
        {
            name:     "list scene participants - player lists participants",
            subject:  "character:player-01DEF",
            action:   "read",
            resource: "scene:01PQR",
        },

        // === Plugin package (1 call site) ===

        // #28: Plugin command execution (internal/plugin/hostfunc/commands.go:119)
        {
            name:     "plugin command - player with capability",
            subject:  "character:player-01DEF",
            action:   "execute",
            resource: "plugin_command:custom_cmd",
        },
        {
            name:     "plugin command - player without capability",
            subject:  "character:player-01MNO",
            action:   "execute",
            resource: "plugin_command:admin_cmd",
        }
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Old engine
            staticResult := staticEngine.Check(ctx, tt.subject, tt.action, tt.resource)

            // New engine
            decision, err := policyEngine.Evaluate(ctx, types.AccessRequest{
                Subject:  tt.subject,
                Action:   tt.action,
                Resource: tt.resource,
            })
            require.NoError(t, err, "policy engine evaluation failed")

            policyResult := decision.IsAllowed()

            // Validate equivalence
            if tt.comment == "" {
                // No documented divergence - decisions MUST match
                assert.Equal(t, staticResult, policyResult,
                    "Decision mismatch: static=%v, policy=%v (subject=%s, action=%s, resource=%s)",
                    staticResult, policyResult, tt.subject, tt.action, tt.resource)
            } else {
                // Documented divergence - log and skip assertion
                if staticResult != policyResult {
                    t.Logf("Expected divergence: %s (static=%v, policy=%v)", tt.comment, staticResult, policyResult)
                }
            }
        })
    }
}

func bootstrapStaticEngine(t *testing.T) access.AccessControl {
    // Bootstrap StaticAccessControl with production role/permission data
    static := access.NewStaticAccessControl()
    // ... seed roles and permissions
    return static
}

func bootstrapPolicyEngine(t *testing.T) *policy.Engine {
    // Bootstrap AccessPolicyEngine with equivalent policy data
    // Use in-memory store or test database
    store := setupTestPolicyStore(t)
    cache := policy.NewMemoryCache()
    resolver := setupTestAttributeResolver(t)

    engine := policy.NewEngine(store, cache, resolver, nil)

    // Seed policies matching static engine's roles/permissions
    // Call Bootstrap() or manually insert equivalent policies

    return engine
}

func setupTestPolicyStore(t *testing.T) policy.PolicyStore {
    // Create in-memory or testcontainer PostgreSQL store
    // Populate with policies equivalent to static roles
    return nil // TODO
}

func setupTestAttributeResolver(t *testing.T) *policy.AttributeResolver {
    // Wire up attribute providers for subject roles, resource ownership, etc.
    return nil // TODO
}
```

**Implementation Notes:**

1. **Bootstrap Equivalence:** Both engines MUST be seeded with equivalent authorization data. Static engine uses roles/permissions, policy engine uses RBAC policies that grant same permissions.

2. **Compound Resource Handling:** For compound resources like `location:<id>:characters`, test BOTH:
   - Old format: `resource="location:01ABC:characters", action="read"`
   - New format: `resource="location:01ABC", action="list_characters"`

   Verify that new format produces same decision as old format.

3. **Test Timing:** Run this test AFTER Task 28 Package 1-3 migrations are complete but BEFORE Task 29 deletes old code. This validates the migration before cleanup.

4. **Failure Handling:** If decisions diverge:
   - Investigate which engine is correct per ABAC spec
   - Document justified divergences with `comment` field
   - Fix incorrect engine or migration code
   - DO NOT proceed to Task 29 until all unjustified divergences are resolved

5. **Coverage Verification:** Audit all 28 production call sites to ensure each unique subject/action/resource pattern appears in test cases. Use `grep -n "\.Check(" internal/ --include="*.go"` to enumerate call sites.

**Acceptance Gate:**

This test MUST pass with zero unjustified divergences before Task 29 proceeds. Any documented divergences MUST have architectural justification (e.g., ABAC spec clarification, intentional behavior change).

---

### Task 29: Remove StaticAccessControl, AccessControl interface, and capability.Enforcer

**Spec References:** [07-migration-seeds.md#implementation-sequence](../specs/abac/07-migration-seeds.md#implementation-sequence), ADR 0014 (Direct replacement, no adapter)

**Dependencies:**

- Task 28.5 (migration equivalence tests) — equivalence tests must pass with zero unjustified divergences before old code is removed

**Acceptance Criteria:**

- [ ] `internal/access/static.go` and `static_test.go` deleted
- [ ] `internal/access/permissions.go` and `permissions_test.go` deleted (if static-only)
- [ ] `AccessControl` interface removed from `access.go` and `internal/plugin/hostfunc/commands.go`
- [ ] `capability.Enforcer` and `capability.CapabilityChecker` removed (capabilities now seed policies)
- [ ] `internal/plugin/hostfunc/functions.go` — Remove `CapabilityChecker` field and `wrap()` capability checks (plugin capabilities now enforced via ABAC policies)
- [ ] **Security (S8 - holomush-5k1.355):** Static analysis rule added to CI via `go vet` or custom linter to detect remaining `AccessControl.Check()` calls
- [ ] Zero references to `AccessControl` in codebase (`grep` clean)
- [ ] Zero references to `StaticAccessControl` in codebase
- [ ] Zero references to `CapabilityChecker` or `capability.Enforcer` in codebase
- [ ] Zero `char:` prefix usage (all migrated to `character:`)
- [ ] `task test` passes
- [ ] `task lint` passes
- [ ] Rollback test: git-revert of Task 29 commit produces compilable code (validates ADR #65 rollback strategy)
- [ ] Rollback test: existing tests pass after git-revert of Task 29 (old AccessControl interfaces restored)
- [ ] Cross-reference: rollback strategy documented in [Decision #65](../specs/decisions/epic7/phase-7.6/065-git-revert-migration-rollback.md) (was ADR #65 — see Decision #36 for migration approach)

**Files:**

- Delete: `internal/access/static.go`
- Delete: `internal/access/static_test.go`
- Delete: `internal/access/permissions.go` (if only used by static evaluator)
- Delete: `internal/access/permissions_test.go`
- Delete: `internal/world/worldtest/mock_AccessControl.go` (generated mock)
- Delete: `internal/access/accesstest/mock.go` (generated mock)
- Modify: `internal/access/access.go` — remove `AccessControl` interface
- Delete or modify: `internal/plugin/capability/` — remove `Enforcer` (capabilities now seed policies)
- Modify: `internal/plugin/hostfunc/functions.go` — remove `CapabilityChecker` field and `wrap()` function
- Modify: `internal/plugin/hostfunc/commands.go` — remove local `AccessControl` interface declaration
- Search and remove: all `char:` prefix usage (replace with `character:`)
- Run: `mockery` to regenerate mocks for new `AccessPolicyEngine` interface

**Call Site Verification:**

This task removes OLD interfaces/implementations. Ensure all production call sites were migrated in Task 28:

- `internal/command/dispatcher.go` — migrated in Task 28 Package 1
- `internal/command/rate_limit_middleware.go` — migrated in Task 28 Package 1
- `internal/command/handlers/boot.go` — migrated in Task 28 Package 1
- `internal/world/service.go` (24 calls) — migrated in Task 28 Package 2
- `internal/plugin/hostfunc/commands.go` (1 call to `f.access.Check()`) — migrated in Task 28 Package 3

The capability enforcer (`f.enforcer.Check()` in `functions.go`) is separate from `AccessControl` and removed here.

**Step 1: Delete static access control files**

**Step 2: Remove AccessControl interface from access.go**

Keep `ParseSubject()` or migrate it to `policy.ParseEntityRef()`. Keep any utility functions still referenced.

**Step 3: Remove capability.Enforcer**

Plugin manifests are now handled by seed policies. Remove enforcer and all references.

**Step 4: Remove legacy char: prefix**

Search and replace `char:` → `character:` in all `.go` files.

**Step 5: Add static analysis check (S8 - holomush-5k1.355)**

Create a custom go vet analyzer or CI check to detect remaining `AccessControl.Check()` calls:

```bash
# Add to .github/workflows/ci.yml or equivalent
- name: Verify no remaining AccessControl.Check calls
  run: |
    if grep -r "AccessControl\.Check" internal/ --include="*.go" | grep -v "_test.go" | grep -v "^Binary"; then
      echo "ERROR: Found remaining AccessControl.Check() calls after migration"
      exit 1
    fi
```

OR add a custom go vet analyzer in `tools/migration-check/` that fails on `AccessControl.Check()` invocations.

**Step 6: Run tests**

Run: `task test`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/access/ internal/world/worldtest/ internal/plugin/capability/
git commit -m "refactor(access): remove StaticAccessControl, AccessControl interface, and capability.Enforcer"
```

---

> **[Back to Overview](./2026-02-06-full-abac-implementation.md)** | **[Previous: Phase 7.5](./2026-02-06-full-abac-phase-7.5.md)** | **[Next: Phase 7.7](./2026-02-06-full-abac-phase-7.7.md)**
