<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

> **[Back to Overview](./2026-02-06-full-abac-implementation.md)** | **[Previous: Phase 7.5](./2026-02-06-full-abac-phase-7.5.md)** | **[Next: Phase 7.7](./2026-02-06-full-abac-phase-7.7.md)**

## Phase 7.6: Call Site Migration & Cleanup

### Task 28: Migrate to AccessPolicyEngine (atomic per-package migration)

**Spec References:** Replacing Static Roles > Implementation Sequence (lines 3175-3230), ADR 0014 (Direct replacement, no adapter)

**Acceptance Criteria:**

- [ ] Per-package atomic migration: DI wiring + call sites migrated together, each commit compiles and passes `task build`
- [ ] `AccessControl` replaced with `*policy.Engine` in dependency graph
- [ ] `AttributeResolver` wired with all registered providers
- [ ] `PolicyCache` wired and `Listen()` called for NOTIFY subscription
- [ ] `SessionResolver` wired
- [ ] `AuditLogger` wired
- [ ] `Bootstrap()` called at startup to seed policies
- [ ] ALL **28 production call sites** migrated from `AccessControl.Check()` to `engine.Evaluate()`:
  - [ ] 24 call sites in `internal/world/service.go`
  - [ ] 1 call site in `internal/command/dispatcher.go`
  - [ ] 1 call site in `internal/command/rate_limit_middleware.go`
  - [ ] 1 call site in `internal/command/handlers/boot.go`
  - [ ] 1 call site in `internal/plugin/hostfunc/commands.go`
- [ ] ALL **57 test call sites** in `internal/access/static_test.go` migrated to new engine
- [ ] Generated mocks regenerated with mockery
- [ ] Each call site uses `types.AccessRequest{Subject, Action, Resource}` struct
- [ ] Error handling: `Evaluate()` error → fail-closed (deny), logged via slog
- [ ] All subject strings use `character:` prefix (not legacy `char:`)
- [ ] Prefix migration strategy: EventStore accepts both `char:` and `character:` prefixes during transition (backward compatibility for existing event stream names)
- [ ] Audit logs are immutable: old entries keep `char:` prefix, new entries use `character:` prefix
- [ ] Tests verify both prefix variants are accepted by EventStore during migration period
- [ ] Tests updated to mock `AccessPolicyEngine` instead of `AccessControl`
- [ ] **Per-package error-path tests:** Each migrated package MUST have tests verifying:
  1. Correct `AccessRequest` construction (subject, action, resource populated)
  2. `decision.IsAllowed()=false` handling (deny path returns error/fails operation)
  3. `Evaluate()` error handling (error != nil → fail-closed, operation denied, error logged)
- [ ] Tests pass incrementally after each package migration
- [ ] Committed per package (dispatcher, world, plugin)
- [ ] `task test` passes after all migrations
- [ ] No commits with intentional build breakage
- [ ] Rollback strategy documented (Decision #65): `git revert` of Task 28 commit(s) restores `AccessControl.Check()` call sites

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

**Call site counts verified (as of 2026-02-07):**

- Production: 28 call sites (3 in command, 24 in world, 1 in plugin)
- Tests: 57 call sites in `internal/access/static_test.go` + ~20 in `internal/plugin/capability/enforcer_test.go` (Task 29)

**Package 1: internal/command (3 call sites)**

1. Update DI wiring in `cmd/holomush/main.go` to wire `*policy.Engine` for command package dependencies
2. Update 3 production call sites:
   - `internal/command/dispatcher.go` (1 call)
   - `internal/command/rate_limit_middleware.go` (1 call)
   - `internal/command/handlers/boot.go` (1 call)
3. Update tests to mock `AccessPolicyEngine` instead of `AccessControl`
4. Run `task test` — MUST PASS
5. Run `task build` — MUST PASS
6. Commit: `"refactor(command): migrate dispatcher to AccessPolicyEngine"`

**Package 2: internal/world (24 call sites)**

1. Update DI wiring in `cmd/holomush/main.go` to wire `*policy.Engine` for world package dependencies
2. Update 24 production call sites in `internal/world/service.go`
3. Update tests to mock `AccessPolicyEngine`
4. Run `task test` — MUST PASS
5. Run `task build` — MUST PASS
6. Commit: `"refactor(world): migrate WorldService to AccessPolicyEngine"`

**Package 3: internal/plugin (1 call site)**

1. Update DI wiring in `cmd/holomush/main.go` to wire `*policy.Engine` for plugin package dependencies
2. Update 1 production call site:
   - `internal/plugin/hostfunc/commands.go` (1 call to `f.access.Check()`)
   - Note: The `AccessControl` field is defined in `functions.go` but used in `commands.go`
3. Update `internal/plugin/hostfunc/functions.go` to change field type from `AccessControl` to `*policy.Engine`
4. Update `internal/plugin/hostfunc/commands.go` to change `AccessControl` interface declaration to use `*policy.Engine`
5. Update `WithAccessControl()` option to accept `*policy.Engine` instead of `AccessControl` interface
6. Update tests to mock `AccessPolicyEngine`
7. Run `task test` — MUST PASS
8. Run `task build` — MUST PASS
9. Commit: `"refactor(plugin): migrate host functions to AccessPolicyEngine"`

**Package 4: Final wiring (bootstrap and NOTIFY subscription)**

1. Add `Bootstrap()` call at startup to seed policies
2. Add `PolicyCache.Listen()` for NOTIFY subscription
3. Run `task test` — MUST PASS
4. Run `task build` — MUST PASS
5. Commit: `"refactor(access): complete AccessPolicyEngine bootstrap and cache subscription"`

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
                    types.NewDecision(types.EffectForbid, "policy denied", "test-policy-123"),
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
                    types.NewDecision(types.DefaultDeny, "evaluation error", ""),
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

If serious issues are discovered after Task 28 migration, rollback is performed via `git revert` (documented in Decision #65 of the design decisions document):

1. **Revert Task 28 commit(s)** — This restores all 28 `AccessControl.Check()` call sites and removes `AccessPolicyEngine.Evaluate()` wiring. Each package migration commit (Package 1-4) can be reverted independently or together.
2. **Do NOT revert Task 29** — Task 29 removes code that still exists at Task 28. If Task 28 is reverted, Task 29's commit should not exist yet (it depends on Task 28 completion). If Task 29 has already been committed, it MUST be reverted first before reverting Task 28.

**Rationale:** No feature flag or adapter layer exists (per Decision #36 and Decision #37 — no adapter, no shadow mode). The migration is a direct replacement with comprehensive test coverage as the safety net. Git revert provides the rollback path (per Decision #65).

---

### Task 29: Remove StaticAccessControl, AccessControl interface, and capability.Enforcer

**Spec References:** Replacing Static Roles > Implementation Sequence (lines 3175-3230), ADR 0014 (Direct replacement, no adapter)

**Acceptance Criteria:**

- [ ] `internal/access/static.go` and `static_test.go` deleted
- [ ] `internal/access/permissions.go` and `permissions_test.go` deleted (if static-only)
- [ ] `AccessControl` interface removed from `access.go` and `internal/plugin/hostfunc/commands.go`
- [ ] `capability.Enforcer` and `capability.CapabilityChecker` removed (capabilities now seed policies)
- [ ] `internal/plugin/hostfunc/functions.go` — Remove `CapabilityChecker` field and `wrap()` capability checks (plugin capabilities now enforced via ABAC policies)
- [ ] Zero references to `AccessControl` in codebase (`grep` clean)
- [ ] Zero references to `StaticAccessControl` in codebase
- [ ] Zero references to `CapabilityChecker` or `capability.Enforcer` in codebase
- [ ] Zero `char:` prefix usage (all migrated to `character:`)
- [ ] `task test` passes
- [ ] `task lint` passes

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

**Step 5: Run tests**

Run: `task test`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/access/ internal/world/worldtest/ internal/plugin/capability/
git commit -m "refactor(access): remove StaticAccessControl, AccessControl interface, and capability.Enforcer"
```

---


---

> **[Back to Overview](./2026-02-06-full-abac-implementation.md)** | **[Previous: Phase 7.5](./2026-02-06-full-abac-phase-7.5.md)** | **[Next: Phase 7.7](./2026-02-06-full-abac-phase-7.7.md)**
