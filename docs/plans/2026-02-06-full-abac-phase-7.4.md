<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Phase 7.4: Seed Policies & Bootstrap

> **[Back to Overview](./2026-02-06-full-abac-implementation.md)** | **[Previous: Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)** | **[Next: Phase 7.5](./2026-02-06-full-abac-phase-7.5.md)**

## Task 22: Define seed policy constants

**Spec References:** Replacing Static Roles > Seed Policies (lines 2984-3061)

**Acceptance Criteria:**

- [ ] All 16 seed policies defined as `SeedPolicy` structs (15 permit, 1 forbid)
- [ ] All seed policies compile without error via `PolicyCompiler`
- [ ] Each seed policy name starts with `seed:`
- [ ] Each seed policy has `SeedVersion: 1` field for upgrade tracking
- [ ] No duplicate seed names
- [ ] DSL text matches spec exactly (lines 2990-3054)
- [ ] Default deny behavior provided by EffectDefaultDeny (no matching policy = denied), not an explicit forbid policy
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/seed.go`
- Test: `internal/access/policy/seed_test.go`

**Step 1: Write failing tests**

- All 16 seed policies compile without error via `PolicyCompiler`
- Each seed policy name starts with `seed:`
- Each seed policy source is `"seed"`
- No duplicate seed names
- DSL text matches spec exactly (lines 2990-3054)

**Step 2: Implement**

```go
// internal/access/policy/seed.go
package policy

// SeedPolicy defines a system-installed default policy.
type SeedPolicy struct {
    Name        string
    Description string
    DSLText     string
    SeedVersion int // Default 1, incremented for upgrades
}

// SeedPolicies returns the complete set of 16 seed policies (15 permit, 1 forbid).
// Default deny behavior is provided by EffectDefaultDeny (no matching policy = denied).
func SeedPolicies() []SeedPolicy {
    return []SeedPolicy{
        {
            Name:        "seed:player-self-access",
            Description: "Characters can read and write their own character",
            DSLText:     `permit(principal is character, action in ["read", "write"], resource is character) when { resource.id == principal.id };`,
            SeedVersion: 1,
        },
        {
            Name:        "seed:player-location-read",
            Description: "Characters can read their current location",
            DSLText:     `permit(principal is character, action in ["read"], resource is location) when { resource.id == principal.location };`,
            SeedVersion: 1,
        },
        {
            Name:        "seed:player-character-colocation",
            Description: "Characters can read co-located characters",
            DSLText:     `permit(principal is character, action in ["read"], resource is character) when { resource.location == principal.location };`,
            SeedVersion: 1,
        },
        {
            Name:        "seed:player-object-colocation",
            Description: "Characters can read co-located objects",
            DSLText:     `permit(principal is character, action in ["read"], resource is object) when { resource.location == principal.location };`,
            SeedVersion: 1,
        },
        {
            Name:        "seed:player-stream-emit",
            Description: "Characters can emit to co-located location streams",
            DSLText:     `permit(principal is character, action in ["emit"], resource is stream) when { resource.name like "location:*" && resource.location == principal.location };`,
            SeedVersion: 1,
        },
        {
            Name:        "seed:player-movement",
            Description: "Characters can enter any location (restrict via forbid policies)",
            DSLText:     `permit(principal is character, action in ["enter"], resource is location);`,
            SeedVersion: 1,
        },
        {
            Name:        "seed:player-basic-commands",
            Description: "Characters can execute basic commands",
            DSLText:     `permit(principal is character, action in ["execute"], resource is command) when { resource.name in ["say", "pose", "look", "go"] };`,
            SeedVersion: 1,
        },
        {
            Name:        "seed:builder-location-write",
            Description: "Builders and admins can create/modify/delete locations",
            DSLText:     `permit(principal is character, action in ["write", "delete"], resource is location) when { principal.role in ["builder", "admin"] };`,
            SeedVersion: 1,
        },
        {
            Name:        "seed:builder-object-write",
            Description: "Builders and admins can create/modify/delete objects",
            DSLText:     `permit(principal is character, action in ["write", "delete"], resource is object) when { principal.role in ["builder", "admin"] };`,
            SeedVersion: 1,
        },
        {
            Name:        "seed:builder-commands",
            Description: "Builders and admins can execute builder commands",
            DSLText:     `permit(principal is character, action in ["execute"], resource is command) when { principal.role in ["builder", "admin"] && resource.name in ["dig", "create", "describe", "link"] };`,
            SeedVersion: 1,
        },
        {
            Name:        "seed:admin-full-access",
            Description: "Admins have full access to everything",
            DSLText:     `permit(principal is character, action, resource) when { principal.role == "admin" };`,
            SeedVersion: 1,
        },
        {
            Name:        "seed:property-public-read",
            Description: "Public properties readable by co-located characters",
            DSLText:     `permit(principal is character, action in ["read"], resource is property) when { resource.visibility == "public" && principal.location == resource.parent_location };`,
            SeedVersion: 1,
        },
        {
            Name:        "seed:property-private-read",
            Description: "Private properties readable only by owner",
            DSLText:     `permit(principal is character, action in ["read"], resource is property) when { resource.visibility == "private" && resource.owner == principal.id };`,
            SeedVersion: 1,
        },
        {
            Name:        "seed:property-admin-read",
            Description: "Admin properties readable only by admins",
            DSLText:     `permit(principal is character, action in ["read"], resource is property) when { resource.visibility == "admin" && principal.role == "admin" };`,
            SeedVersion: 1,
        },
        {
            Name:        "seed:property-visible-to",
            Description: "Properties with visible_to lists: only listed characters can read",
            DSLText:     `permit(principal is character, action in ["read"], resource is property) when { resource has visible_to && principal.id in resource.visible_to };`,
            SeedVersion: 1,
        },
        {
            Name:        "seed:property-excluded-from",
            Description: "Exclude specific characters from seeing a property",
            DSLText:     `forbid(principal is character, action in ["read"], resource is property) when { resource has excluded_from && principal.id in resource.excluded_from };`,
            SeedVersion: 1,
        },
    }
}
```

(Note: 16 seed policies listed above: 15 permit policies for standard access patterns, plus 1 forbid policy for excluded_from visibility (per lines 1245-1271, 1299-1325). Default deny behavior is provided by EffectDefaultDeny.)

**Step 3: Run tests, commit**

```bash
git add internal/access/policy/seed.go internal/access/policy/seed_test.go
git commit -m "feat(access): define seed policies"
```

---

### Task 23: Bootstrap sequence

**Spec References:** Bootstrap Sequence (lines 3062-3177), Seed Policy Migrations (lines 3178-3229)

**Acceptance Criteria:**

- [ ] Uses `access.WithSystemSubject(context.Background())` to bypass ABAC for seed operations
- [ ] Per-seed name-based idempotency check via `policyStore.Get(ctx, seed.Name)`
- [ ] Skips seed if policy exists with same name and `source="seed"` (already seeded)
- [ ] Logs warning and skips if policy exists with same name but `source!="seed"` (admin collision)
- [ ] New seeds inserted with `source="seed"`, `seed_version=1`, `created_by="system"`
- [ ] Seed version upgrade: if shipped `seed_version > stored.seed_version`, update `dsl_text`, `compiled_ast`, and `seed_version`
- [ ] Upgrade populates `change_note` with `"Auto-upgraded from seed v{N} to v{N+1} on server upgrade"`
- [ ] Respects `--skip-seed-migrations` flag to disable automatic upgrades
- [ ] `PolicyStore.UpdateSeed(ctx, name, oldDSL, newDSL, changeNote)` method for migration-delivered seed fixes
- [ ] `UpdateSeed()` checks if policy exists with `source='seed'`; fails if not
- [ ] `UpdateSeed()` skips if stored DSL matches new DSL (idempotent)
- [ ] `UpdateSeed()` skips with warning if stored DSL differs from old DSL (customized by admins)
- [ ] `UpdateSeed()` updates DSL, compiled AST, logs info, invalidates cache if uncustomized
- [ ] Policy store's `IsNotFound(err)` helper: either confirmed as pre-existing or added to Task 6 ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md)) (policy store) acceptance criteria
- [ ] All tests pass via `task test`

**Dependencies:**

- Task 6 ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md)) (prefix constants and access control types) — provides `access.WithSystemSubject()` and `access.IsSystemContext()` helpers

**Files:**

- Create: `internal/access/policy/bootstrap.go`
- Test: `internal/access/policy/bootstrap_test.go`

**Step 1: Write failing tests**

- Empty `access_policies` table → all seeds created with `source="seed"`, `seed_version=1`
- Existing seed policy (same name, `source="seed"`) → skipped (idempotent)
- Existing non-seed policy (same name, `source!="seed"`) → skipped, warning logged
- Seed version upgrade (shipped version > stored version) → policy updated with new DSL and incremented version
- `--skip-seed-migrations` flag → no version upgrades, only new seed insertions

**Step 2: Implement**

```go
// internal/access/policy/bootstrap.go
package policy

import (
    "context"
    "encoding/json"
    "fmt"
    "log/slog"

    "github.com/holomush/holomush/internal/access"
    policystore "github.com/holomush/holomush/internal/access/policy/store"
    "github.com/samber/oops"
)

// BootstrapOptions controls bootstrap behavior.
type BootstrapOptions struct {
    SkipSeedMigrations bool // Disable automatic seed version upgrades
}

// Bootstrap seeds the policy table with system policies.
// Called at server startup with system subject context (bypasses ABAC).
// Idempotent: checks each seed policy by name before insertion.
// Supports seed version upgrades unless opts.SkipSeedMigrations is true.
func Bootstrap(ctx context.Context, policyStore policystore.PolicyStore, compiler *PolicyCompiler, logger *slog.Logger, opts BootstrapOptions) error {
    // Use system subject context to bypass ABAC during bootstrap
    ctx = access.WithSystemSubject(ctx)

    for _, seed := range SeedPolicies() {
        // Per-seed idempotency check: query by name
        existing, err := policyStore.Get(ctx, seed.Name)
        if err != nil && !policystore.IsNotFound(err) {
            return oops.With("seed", seed.Name).Wrap(err)
        }

        if existing != nil {
            // Policy with this name exists
            if existing.Source != "seed" {
                // Admin created a policy with a seed: name — log warning, skip
                logger.Warn("seed name collision with non-seed policy, skipping",
                    "name", seed.Name,
                    "source", existing.Source)
                continue
            }

            // Existing seed policy — check for version upgrade
            if !opts.SkipSeedMigrations && existing.SeedVersion != nil && seed.SeedVersion > *existing.SeedVersion {
                // Capture old version before updating
                oldVersion := *existing.SeedVersion

                // Upgrade: compile new DSL and update stored policy
                compiled, _, err := compiler.Compile(seed.DSLText)
                if err != nil {
                    return oops.With("seed", seed.Name).With("version", seed.SeedVersion).Wrap(err)
                }
                compiledJSON, _ := json.Marshal(compiled)

                existing.DSLText = seed.DSLText
                existing.Effect = compiled.Effect
                existing.CompiledAST = compiledJSON
                existing.SeedVersion = &seed.SeedVersion
                existing.ChangeNote = fmt.Sprintf("Auto-upgraded from seed v%d to v%d on server upgrade", oldVersion, seed.SeedVersion)

                if err := policyStore.Update(ctx, existing); err != nil {
                    return oops.With("seed", seed.Name).With("version", seed.SeedVersion).Wrap(err)
                }

                logger.Info("upgraded seed policy",
                    "name", seed.Name,
                    "from_version", oldVersion,
                    "to_version", seed.SeedVersion)
            }

            // Already seeded at current or higher version, skip
            continue
        }

        // New seed policy: compile and insert
        compiled, _, err := compiler.Compile(seed.DSLText)
        if err != nil {
            return oops.With("seed", seed.Name).Wrap(err)
        }
        compiledJSON, _ := json.Marshal(compiled)

        err = policyStore.Create(ctx, &policystore.StoredPolicy{
            Name:        seed.Name,
            Description: seed.Description,
            Effect:      compiled.Effect,
            Source:      "seed",
            DSLText:     seed.DSLText,
            CompiledAST: compiledJSON,
            Enabled:     true,
            SeedVersion: &seed.SeedVersion,
            CreatedBy:   "system",
        })
        if err != nil {
            return oops.With("seed", seed.Name).Wrap(err)
        }

        logger.Info("created seed policy", "name", seed.Name, "version", seed.SeedVersion)
    }

    return nil
}
```

**Implementation Notes:**

- `SeedPolicies()` must return policies with `SeedVersion` field (default 1)
- `store.PolicyStore.Get(ctx, name)` retrieves policy by name, returns `IsNotFound` error if absent
- `access.WithSystemSubject(ctx)` marks context as system-level operation
- `PolicyStore.Create/Update` checks `access.IsSystemContext(ctx)` and bypasses `Evaluate()` when true
- Upgrade logic compares shipped `seed.SeedVersion` against stored `existing.SeedVersion`
- `--skip-seed-migrations` server flag sets `opts.SkipSeedMigrations=true`
- Legacy policies without `SeedVersion` (nil) will not be upgraded; future enhancement may treat nil as version 0
- `--force-seed-version=N` flag enables rollback (future enhancement, see [07-migration-seeds.md#bootstrap-sequence](../specs/abac/07-migration-seeds.md#bootstrap-sequence), was spec lines 3121-3129)

**Step 3: Run tests, commit**

```bash
git add internal/access/policy/bootstrap.go internal/access/policy/bootstrap_test.go
git commit -m "feat(access): add seed policy bootstrap with version upgrades"
```

**Spec Deviations:**

| What                              | Deviation                                                                                                                                     | Rationale                                                                                                                                                                 |
| --------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `UpdateSeed()` signature          | `UpdateSeed(ctx, name, oldDSL, newDSL, changeNote)` — 5 params vs spec's 3-param `UpdateSeed(ctx, name, dsl)` signature                      | `oldDSL` enables CAS (compare-and-swap) semantics for safe concurrent updates; `changeNote` provides audit trail context for migration-delivered fixes                    |

---

### Task 23b: CLI flag --validate-seeds

> **Note:** This task was moved from Phase 7.7 (Task 35 ([Phase 7.7](./2026-02-06-full-abac-phase-7.7.md))) to Phase 7.4 to enable CI validation during later phases. Only depends on Task 23 (compiler and seed definitions).

**Spec References:** Seed Policy Validation (lines 3132-3177)

**Acceptance Criteria:**

- [ ] CLI flag `--validate-seeds` added to server startup
- [ ] Flag behavior: boot DSL compiler, validate all seed policy DSL text, exit with success/failure status
- [ ] Does NOT start the server (validation-only mode)
- [ ] Exit code 0 on success, non-zero on failure
- [ ] Logs validation results: "All N seed policies valid" or "Validation failed: {errors}"
- [ ] Enables CI integration: `holomush --validate-seeds` in build pipeline
- [ ] All tests pass via `task test`

**Files:**

- Modify: `cmd/holomush/main.go` (add flag and validation logic)
- Test: `cmd/holomush/main_test.go`

**TDD Test List:**

- `--validate-seeds` flag present → validation mode activated
- All valid seeds → exit 0, log success
- Invalid seed DSL → exit non-zero, log errors with line/column
- Validation mode → server does NOT start
- CI integration test: run in build pipeline, verify exit codes

**Step 1: Write failing tests**

Test that `--validate-seeds` flag activates validation-only mode and exits with appropriate status codes.

**Step 2: Implement**

Add flag parsing and validation logic in `cmd/holomush/main.go`. Use the DSL compiler from Task 12 ([Phase 7.2](./2026-02-06-full-abac-phase-7.2.md)) to validate all seed policy DSL text from Task 22.

**Step 3: Run tests, commit**

```bash
git add cmd/holomush/main.go cmd/holomush/main_test.go
git commit -m "feat(cmd): add --validate-seeds CLI flag for CI integration"
```

---

> **[Back to Overview](./2026-02-06-full-abac-implementation.md)** | **[Previous: Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)** | **[Next: Phase 7.5](./2026-02-06-full-abac-phase-7.5.md)**
