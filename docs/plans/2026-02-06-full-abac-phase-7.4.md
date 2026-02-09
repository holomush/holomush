<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Phase 7.4: Seed Policies & Bootstrap

> **[Back to Overview](./2026-02-06-full-abac-implementation.md)** | **[Previous: Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)** | **[Next: Phase 7.5](./2026-02-06-full-abac-phase-7.5.md)**
>
> **Prerequisites:** Task 16a, Task 16b, and Task 18 from Phase 7.3 must complete before Phase 7.4 tasks begin (T16a→T23, T16b→T23, and T18→T23 dependencies). T16a provides StreamProvider and CommandProvider, which are referenced by seed policies `seed:player-stream-emit` and `seed:player-basic-commands`. T16b provides PropertyProvider, which enables `property.*` attribute resolution for seed policies `seed:property-public-read`, `seed:property-private-read`, `seed:property-admin-read`, `seed:property-system-forbid`, `seed:property-owner-write`, `seed:property-restricted-visible-to`, and `seed:property-restricted-excluded`.

## Task 22: Define seed policy constants

**Spec References:** [07-migration-seeds.md#seed-policies](../specs/abac/07-migration-seeds.md#seed-policies) (was lines 2984-3061)

**Acceptance Criteria:**

- [ ] All 18 seed policies defined as `SeedPolicy` structs (17 permit, 1 forbid)
- [ ] All seed policies compile without error via `PolicyCompiler`
- [ ] Each seed policy name starts with `seed:`
- [ ] Each seed policy has `SeedVersion: 1` field for upgrade tracking
- [ ] No duplicate seed names
- [ ] DSL text matches spec exactly [07-migration-seeds.md#seed-policies](../specs/abac/07-migration-seeds.md#seed-policies) (was lines 2990-3054)
- [ ] Default deny behavior provided by EffectDefaultDeny (no matching policy = denied), not an explicit forbid policy
- [ ] Seed policy coverage validated against all 28 production call sites (see [Appendix A](#appendix-a-seed-policy-coverage-matrix))
- [ ] All tests pass via `task test`

**Dependencies:**

- Task 21a ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)) (remove @ prefix from command names) — seed policies reference bare command names (e.g., `"say"`, `"dig"`) which require @-prefix removal first

**Files:**

- Create: `internal/access/policy/seed.go`
- Test: `internal/access/policy/seed_test.go`

**Step 1: Write failing tests**

- All 18 seed policies compile without error via `PolicyCompiler` (17 permit, 1 forbid)
- Each seed policy name starts with `seed:`
- Each seed policy source is `"seed"`
- No duplicate seed names
- DSL text matches spec exactly [07-migration-seeds.md#seed-policies](../specs/abac/07-migration-seeds.md#seed-policies) (was lines 2990-3054)

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

// SeedPolicies returns the complete set of 18 seed policies (17 permit, 1 forbid).
// Default deny behavior is provided by EffectDefaultDeny (no matching policy = denied).
// See ADR 087 for rationale on default-deny instead of explicit forbid for system properties.
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
            Name:        "seed:player-exit-use",
            Description: "Characters can use exits for navigation",
            DSLText:     `permit(principal is character, action in ["use"], resource is exit);`,
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
            Name:        "seed:property-owner-write",
            Description: "Property owners can write and delete their properties",
            DSLText:     `permit(principal is character, action in ["write", "delete"], resource is property) when { resource.owner == principal.id };`,
            SeedVersion: 1,
        },
        {
            Name:        "seed:property-restricted-visible-to",
            Description: "Restricted properties: readable by characters in the visible_to list",
            DSLText:     `permit(principal is character, action in ["read"], resource is property) when { resource.visibility == "restricted" && resource has visible_to && principal.id in resource.visible_to };`,
            SeedVersion: 1,
        },
        {
            Name:        "seed:property-restricted-excluded",
            Description: "Restricted properties: denied to characters in the excluded_from list",
            DSLText:     `forbid(principal is character, action in ["read"], resource is property) when { resource.visibility == "restricted" && resource has excluded_from && principal.id in resource.excluded_from };`,
            SeedVersion: 1,
        },
    }
}
```

(Note: 18 seed policies listed above: 17 permit policies for standard access patterns, plus 1 forbid policy (seed:property-restricted-excluded for restricted property exclusion). System properties are protected by default-deny instead of an explicit forbid (see ADR 087 - under deny-overrides conflict resolution, a forbid would block seed:admin-full-access, locking admins out permanently).)

**Step 3: Run tests, commit**

```bash
git add internal/access/policy/seed.go internal/access/policy/seed_test.go
git commit -m "feat(access): define seed policies"
```

---

### Task 22b: Resolve seed policy coverage gaps

> **Note:** Elevated from a T22 acceptance criterion to a dedicated task per [Decision #94](../specs/decisions/epic7/phase-7.4/094-elevate-seed-policy-gap-resolution.md). With direct replacement (no adapter/shadow mode), unresolved gaps cause immediate functional regressions at T28 migration.

**Spec References:** [07-migration-seeds.md#seed-policies](../specs/abac/07-migration-seeds.md#seed-policies) (was lines 2984-3061), [Appendix A](#appendix-a-seed-policy-coverage-matrix)

**Acceptance Criteria:**

- [ ] **G1 resolved:** `seed:player-exit-read` policy added — players can read exits in their current location (covers call site #10 GetExit)
- [ ] **G2 resolved:** `seed:builder-exit-write` policy added — builders can create/update/delete exits (covers call sites #11-#13 CreateExit, UpdateExit, DeleteExit)
- [ ] **G3 resolved:** `list_characters` action covered by expanding `seed:player-location-read` to include `list_characters`, OR a dedicated `seed:player-location-list-characters` policy added (covers call site #22 GetCharactersByLocation)
- [ ] **G4 resolved:** Scene access policies added — `seed:player-scene-participant` (read+write own scenes) and/or `seed:player-scene-read` (read scenes in current location) (covers call sites #25-#27)
- [ ] **G5 documented:** Plugin command gap documented as intentional — plugins MUST define their own ABAC policies at install time; default-deny is correct baseline
- [ ] **G6 documented:** MoveObject gap evaluated against game design requirements — either `seed:player-object-move` policy added or documented as intentional builder-only behavior
- [ ] Seed policy count updated in T22 and T23 to reflect additions
- [ ] Updated seed policies compile without error via `PolicyCompiler`
- [ ] Coverage matrix in Appendix A updated to reflect resolved gaps
- [ ] All tests pass via `task test`

**Dependencies:**

- Task 22 (seed policy constants) — gap resolution requires the base seed policies to exist first

**Files:**

- Modify: `internal/access/policy/seed.go` (add new seed policies for G1-G4, optionally G6)
- Modify: `internal/access/policy/seed_test.go` (test new seed policies)

**Step 1: Write failing tests**

- New seed policies (G1-G4) compile without error via `PolicyCompiler`
- Each new seed policy name starts with `seed:`
- Each new seed policy has `SeedVersion: 1`
- No duplicate seed names after additions
- Coverage matrix has no Medium/High severity gaps remaining

**Step 2: Implement**

Add missing seed policies based on Appendix A gap analysis recommendations:

1. **G1+G2 (Exits):** Add `seed:player-exit-read` and `seed:builder-exit-write`
2. **G3 (list\_characters):** Expand `seed:player-location-read` to include `list_characters` action, or add a dedicated policy
3. **G4 (Scenes):** Add `seed:player-scene-participant` and `seed:player-scene-read`
4. **G5 (Plugin commands):** Document in plugin developer guide — no seed policy change
5. **G6 (MoveObject):** Evaluate and either add `seed:player-object-move` or document as intentional

**Step 3: Update Appendix A coverage matrix, run tests, commit**

```bash
git add internal/access/policy/seed.go internal/access/policy/seed_test.go
git commit -m "feat(access): resolve seed policy coverage gaps G1-G4"
```

---

### Task 23: Bootstrap sequence

**Spec References:** [07-migration-seeds.md#bootstrap-sequence](../specs/abac/07-migration-seeds.md#bootstrap-sequence) (was lines 3062-3177), [07-migration-seeds.md#seed-policy-migrations](../specs/abac/07-migration-seeds.md#seed-policy-migrations) (was lines 3178-3229)

**ADR References:** [091-bootstrap-creates-initial-partitions.md](../specs/decisions/epic7/phase-7.4/091-bootstrap-creates-initial-partitions.md)

**Acceptance Criteria:**

- [ ] Creates initial `access_audit_log` partitions before seed policy insertion (per ADR #91)
- [ ] At least 3 initial monthly partitions created (current month + 2 future months)
- [ ] Partition naming follows spec convention: `access_audit_log_YYYY_MM`
- [ ] Partition creation is idempotent (skips partitions that already exist via `IF NOT EXISTS`)
- [ ] Partition creation failure is fatal (bootstrap aborts with clear error)
- [ ] Uses `access.WithSystemSubject(context.Background())` to bypass ABAC for seed operations
- [ ] Per-seed name-based idempotency check via `policyStore.Get(ctx, seed.Name)`
- [ ] Skips seed if policy exists with same name and `source="seed"` (already seeded)
- [ ] Logs warning and skips if policy exists with same name but `source!="seed"` (admin collision)
- [ ] New seeds inserted with `source="seed"`, `seed_version=1`, `created_by="system"`
- [ ] Seed version upgrade: if shipped `seed_version > stored.seed_version`, update `dsl_text`, `compiled_ast`, and `seed_version`
- [ ] Upgrade populates `change_note` with `"Auto-upgraded from seed v{N} to v{N+1} on server upgrade"`
- [ ] Respects `--skip-seed-migrations` flag to disable automatic upgrades
- [ ] Bootstrap handles `PolicyCompiler` compilation errors gracefully during seed insertion and upgrades
- [ ] **Seed compilation/install failure is fatal — server exits with error (ADR #92)**
- [ ] `PolicyStore.UpdateSeed(ctx, name, oldDSL, newDSL, changeNote)` method for migration-delivered seed fixes
- [ ] `UpdateSeed()` checks if policy exists with `source='seed'`; fails if not
- [ ] `UpdateSeed()` skips if stored DSL matches new DSL (idempotent)
- [ ] `UpdateSeed()` skips with warning if stored DSL differs from old DSL (customized by admins)
- [ ] `UpdateSeed()` updates DSL, compiled AST, logs info, invalidates cache if uncustomized
- [ ] Policy store's `IsNotFound(err)` helper: either confirmed as pre-existing or added to Task 7 ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md)) (policy store) acceptance criteria
- [ ] All tests pass via `task test`
- [ ] After bootstrap completes, `SELECT COUNT(*) FROM access_policies WHERE source='seed'` returns expected seed count matching number of defined seeds in Task 22
- [ ] Spot-check: at least one bootstrap integration test verifies a specific seed policy's `name`, `dsl_text`, and `effect` match expected values
- [ ] Bootstrap test verifies no duplicate seed policies exist (`SELECT name, COUNT(*) FROM access_policies WHERE source='seed' GROUP BY name HAVING COUNT(*) > 1` returns zero rows)
- [ ] After bootstrap completes, admin subject can successfully execute policy edit operations via `policyStore.Update()` (verifies `seed:admin-full-access` policy works for policy management; integration test coverage deferred to Task 27a/27b which define policy edit handlers)

**Dependencies:**

- Task 7 ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md)) (policy store interface and PostgreSQL implementation) — provides `PolicyStore` interface for seed policy bootstrap operations

**Files:**

- Create: `internal/access/policy/bootstrap.go`
- Test: `internal/access/policy/bootstrap_test.go`

**Step 1: Write failing tests**

- Bootstrap creates initial audit log partitions (current month + 2 future months)
- Partition creation is idempotent (re-running bootstrap does not fail on existing partitions)
- Partition naming follows convention: `access_audit_log_YYYY_MM`
- Empty `access_policies` table → all seeds created with `source="seed"`, `seed_version=1`
- Existing seed policy (same name, `source="seed"`) → skipped (idempotent)
- Existing non-seed policy (same name, `source!="seed"`) → skipped, warning logged
- Seed version upgrade (shipped version > stored version) → policy updated with new DSL and incremented version
- `--skip-seed-migrations` flag → no version upgrades, only new seed insertions
- Malformed seed DSL during bootstrap → compilation error handled gracefully, bootstrap fails with clear error
- Partial bootstrap failure → some seeds compile successfully, some don't; bootstrap fails but successful seeds are not committed

**Step 2: Implement**

```go
// internal/access/policy/bootstrap.go
package policy

import (
    "context"
    "encoding/json"
    "fmt"
    "log/slog"
    "time"

    "github.com/holomush/holomush/internal/access"
    policystore "github.com/holomush/holomush/internal/access/policy/store"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/samber/oops"
)

// BootstrapOptions controls bootstrap behavior.
type BootstrapOptions struct {
    SkipSeedMigrations bool // Disable automatic seed version upgrades
}

// Bootstrap seeds the policy table with system policies and creates
// initial audit log partitions.
// Called at server startup with system subject context (bypasses ABAC).
// Idempotent: checks each seed policy by name before insertion;
// partition creation uses IF NOT EXISTS.
// Supports seed version upgrades unless opts.SkipSeedMigrations is true.
func Bootstrap(ctx context.Context, pool *pgxpool.Pool, policyStore policystore.PolicyStore, compiler *PolicyCompiler, logger *slog.Logger, opts BootstrapOptions) error {
    // Use system subject context to bypass ABAC during bootstrap
    ctx = access.WithSystemSubject(ctx)

    // Step 1: Create initial audit log partitions (ADR #91).
    // PostgreSQL rejects INSERTs into unpartitioned parent tables, so
    // partitions MUST exist before any audit log writes occur.
    if err := createAuditLogPartitions(ctx, pool, logger); err != nil {
        return oops.Errorf("failed to create audit log partitions: %w", err)
    }

    // Step 2: Seed policies
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

// createAuditLogPartitions creates initial monthly partitions for the
// access_audit_log table. Creates current month + 2 future months.
// Idempotent: uses CREATE TABLE IF NOT EXISTS to skip existing partitions.
// Called before seed policy insertion to ensure audit log writes succeed.
// See ADR #91 for rationale on bootstrap vs migration.
func createAuditLogPartitions(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) error {
    now := time.Now()
    for i := 0; i < 3; i++ {
        month := now.AddDate(0, i, 0)
        start := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
        end := start.AddDate(0, 1, 0)
        name := fmt.Sprintf("access_audit_log_%d_%02d", start.Year(), start.Month())
        sql := fmt.Sprintf(
            "CREATE TABLE IF NOT EXISTS %s PARTITION OF access_audit_log FOR VALUES FROM ('%s') TO ('%s')",
            name, start.Format("2006-01-02"), end.Format("2006-01-02"),
        )
        if _, err := pool.Exec(ctx, sql); err != nil {
            return oops.With("partition", name).Wrap(err)
        }
        logger.Info("ensured audit log partition exists", "partition", name,
            "from", start.Format("2006-01-02"), "to", end.Format("2006-01-02"))
    }
    return nil
}
```

**Implementation Notes:**

- `createAuditLogPartitions()` runs BEFORE seed policy insertion to ensure audit log writes succeed during bootstrap (ADR #91)
- Partition creation uses `CREATE TABLE IF NOT EXISTS` for idempotency — safe to re-run on every server startup
- The same partition naming convention (`access_audit_log_YYYY_MM`) is used by T19b (audit retention/partition management) at runtime
- `Bootstrap()` now accepts `*pgxpool.Pool` parameter for direct SQL partition creation (seed policies still use `PolicyStore` interface)
- `SeedPolicies()` must return policies with `SeedVersion` field (default 1)
- `store.PolicyStore.Get(ctx, name)` retrieves policy by name, returns `IsNotFound` error if absent
- `access.WithSystemSubject(ctx)` marks context as system-level operation
- `PolicyStore.Create/Update` checks `access.IsSystemContext(ctx)` and bypasses `Evaluate()` when true
- Upgrade logic compares shipped `seed.SeedVersion` against stored `existing.SeedVersion`
- `--skip-seed-migrations` server flag sets `opts.SkipSeedMigrations=true`
- Legacy policies without `SeedVersion` (nil) will not be upgraded; future enhancement may treat nil as version 0
- `--force-seed-version=N` flag enables rollback (future enhancement, see [07-migration-seeds.md#bootstrap-sequence](../specs/abac/07-migration-seeds.md#bootstrap-sequence), was spec lines 3121-3129)
- **Bootstrap failures are fatal (ADR #92):** Any seed policy compilation or
  installation error causes `Bootstrap()` to return an error, which MUST cause
  the server to exit with a non-zero code and descriptive error message. No
  `--skip-seed-install` flag or degraded startup mode is supported. Seed
  policy failures are configuration errors equivalent to missing database
  connections or invalid TLS certificates.

**Step 3: Run tests, commit**

```bash
git add internal/access/policy/bootstrap.go internal/access/policy/bootstrap_test.go
git commit -m "feat(access): add seed policy bootstrap with version upgrades"
```

**Spec Deviations:**

| What                     | Deviation                                                                                                               | Rationale                                                                                                                                              |
| ------------------------ | ----------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `UpdateSeed()` signature | `UpdateSeed(ctx, name, oldDSL, newDSL, changeNote)` — 5 params vs spec's 3-param `UpdateSeed(ctx, name, dsl)` signature | `oldDSL` enables CAS (compare-and-swap) semantics for safe concurrent updates; `changeNote` provides audit trail context for migration-delivered fixes |

---

### Task 23b: CLI flag --validate-seeds

> **Note:** This task was moved from Phase 7.7 (Task 36 ([Phase 7.7](./2026-02-06-full-abac-phase-7.7.md))) to Phase 7.4 to enable CI validation during later phases. Only depends on Task 23 (compiler and seed definitions).

**Spec References:** [07-migration-seeds.md#bootstrap-sequence](../specs/abac/07-migration-seeds.md#bootstrap-sequence) (was lines 3132-3177, seed verification section)

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

### Task 23c: Smoke Test Gate

> **Note:** This task gates the irreversible migration step (Task 28, [Phase 7.6](./2026-02-06-full-abac-phase-7.6.md)). Positioned between T23b (seed validation) and T28 (call site migration) to exercise common auth patterns with the ABAC engine before replacing all production call sites.

**Purpose:** Verify that seed policies correctly authorize common player, builder, and admin operations using the full ABAC engine before committing to migration. This gate ensures no functional regressions are introduced by the transition from static role checks to policy-driven evaluation.

**Acceptance Criteria:**

- [ ] Smoke test implementation covers minimum scenarios (see below)
- [ ] All smoke tests pass with expected permissions (Allow for authorized, Deny for unauthorized)
- [ ] Tests exercise the full evaluation pipeline (AttributeResolver → PolicyEngine → deny-overrides)
- [ ] Tests run against a real test server with seeded policies
- [ ] Tests validate that unauthorized operations are denied (negative cases)
- [ ] All tests pass via `task test`

**Minimum Scenarios:**

| Scenario                             | Subject             | Action    | Resource                     | Expected Result |
| ------------------------------------ | ------------------- | --------- | ---------------------------- | --------------- |
| **Basic Player Commands**            |                     |           |                              |                 |
| Player executes "say"                | character (player)  | `execute` | command (say)                | Allow           |
| Player executes "look"               | character (player)  | `execute` | command (look)               | Allow           |
| Player moves to adjacent location    | character (player)  | `enter`   | location (destination)       | Allow           |
| Player uses exit                     | character (player)  | `use`     | exit                         | Allow           |
| Player reads co-located character    | character (player)  | `read`    | character (co-located)       | Allow           |
| Player executes "dig" (unauthorized) | character (player)  | `execute` | command (dig)                | Deny            |
| **Builder Operations**               |                     |           |                              |                 |
| Builder executes "dig"               | character (builder) | `execute` | command (dig)                | Allow           |
| Builder creates location             | character (builder) | `write`   | location                     | Allow           |
| Builder creates exit                 | character (builder) | `write`   | exit                         | Allow           |
| Builder describes object             | character (builder) | `write`   | object                       | Allow           |
| **Admin Operations**                 |                     |           |                              |                 |
| Admin executes "shutdown"            | character (admin)   | `execute` | command (shutdown)           | Allow           |
| Admin reads any location             | character (admin)   | `read`    | location (any)               | Allow           |
| Admin deletes location               | character (admin)   | `delete`  | location                     | Allow           |
| Admin creates policy                 | character (admin)   | `write`   | policy                       | Allow           |
| **Negative Cases (Deny Expected)**   |                     |           |                              |                 |
| Player deletes location              | character (player)  | `delete`  | location                     | Deny            |
| Builder executes admin-only command  | character (builder) | `execute` | command (admin-only)         | Deny            |
| Player writes co-located object      | character (player)  | `write`   | object (co-located, unowned) | Deny            |

**Dependencies:**

- Task 23b ([CLI --validate-seeds](#task-23b-cli-flag---validate-seeds)) — seed policies are validated before smoke tests exercise them
- Task 17.4 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)) — AccessPolicyEngine is operational
- Task 18 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)) — policy cache is functional
- Task 22b ([Resolve seed policy gaps](#task-22b-resolve-seed-policy-coverage-gaps)) — seed policies cover all required actions

**Files:**

- Create: `test/integration/access/smoke_test.go`

**Implementation Notes:**

- Use Ginkgo/Gomega for BDD-style test organization
- Test server setup:
  1. Spin up PostgreSQL testcontainer
  2. Run migrations (schema + bootstrap)
  3. Create test characters with player/builder/admin roles
  4. Initialize AccessPolicyEngine with full provider chain
- Tests MUST call `AccessPolicyEngine.Evaluate()` directly (not via service layer wrappers)
- Each test case verifies `Decision.Effect` matches expected Allow/Deny
- Failures MUST block T28 migration start — smoke test gate is a hard dependency

**Step 1: Write failing tests**

- Test setup: bootstrap seeds, create test subjects (player/builder/admin characters), initialize engine
- Each minimum scenario above as a separate `It()` block
- Verify `Decision.Effect == EffectAllow` for authorized cases
- Verify `Decision.Effect == EffectDeny` for unauthorized cases

**Step 2: Implement**

Add test file `test/integration/access/smoke_test.go`:

```go
//go:build integration

package access_test

import (
    "context"
    "testing"

    . "github.com/onsi/ginkgo/v2"
    . "github.com/onsi/gomega"

    "github.com/holomush/holomush/internal/access"
    "github.com/holomush/holomush/internal/access/abac"
    policystore "github.com/holomush/holomush/internal/access/policy/store"
    // ... other imports
)

var _ = Describe("ABAC Smoke Test Gate", func() {
    var (
        ctx    context.Context
        engine *abac.AccessPolicyEngine
        player, builder, admin *world.Character
    )

    BeforeEach(func() {
        // Setup: testcontainer, migrations, bootstrap, engine init
        // Create test characters with appropriate roles
    })

    Describe("Basic Player Commands", func() {
        It("player executes 'say'", func() {
            req := access.AccessRequest{
                Subject:  "character:" + player.ID.String(),
                Action:   "execute",
                Resource: "command:say",
            }
            decision, err := engine.Evaluate(ctx, req)
            Expect(err).NotTo(HaveOccurred())
            Expect(decision.Effect).To(Equal(access.EffectAllow))
        })

        It("player executes 'dig' (unauthorized)", func() {
            req := access.AccessRequest{
                Subject:  "character:" + player.ID.String(),
                Action:   "execute",
                Resource: "command:dig",
            }
            decision, err := engine.Evaluate(ctx, req)
            Expect(err).NotTo(HaveOccurred())
            Expect(decision.Effect).To(Equal(access.EffectDeny))
        })

        // ... remaining player scenarios
    })

    Describe("Builder Operations", func() {
        It("builder executes 'dig'", func() {
            req := access.AccessRequest{
                Subject:  "character:" + builder.ID.String(),
                Action:   "execute",
                Resource: "command:dig",
            }
            decision, err := engine.Evaluate(ctx, req)
            Expect(err).NotTo(HaveOccurred())
            Expect(decision.Effect).To(Equal(access.EffectAllow))
        })

        // ... remaining builder scenarios
    })

    Describe("Admin Operations", func() {
        It("admin executes 'shutdown'", func() {
            req := access.AccessRequest{
                Subject:  "character:" + admin.ID.String(),
                Action:   "execute",
                Resource: "command:shutdown",
            }
            decision, err := engine.Evaluate(ctx, req)
            Expect(err).NotTo(HaveOccurred())
            Expect(decision.Effect).To(Equal(access.EffectAllow))
        })

        // ... remaining admin scenarios
    })

    Describe("Negative Cases (Deny Expected)", func() {
        It("player deletes location", func() {
            req := access.AccessRequest{
                Subject:  "character:" + player.ID.String(),
                Action:   "delete",
                Resource: "location:" + testLocationID.String(),
            }
            decision, err := engine.Evaluate(ctx, req)
            Expect(err).NotTo(HaveOccurred())
            Expect(decision.Effect).To(Equal(access.EffectDeny))
        })

        // ... remaining negative cases
    })
})

func TestABACSmoke(t *testing.T) {
    RegisterFailHandler(Fail)
    RunSpecs(t, "ABAC Smoke Test Gate Suite")
}
```

**Step 3: Run tests, commit**

```bash
git add test/integration/access/smoke_test.go
git commit -m "test(access): add smoke test gate for Phase 7.4→7.6 transition"
```

---

## Appendix A: Seed Policy Coverage Matrix

This appendix maps each of the 28 production call sites (documented in Task 28.5, [Phase 7.6](./2026-02-06-full-abac-phase-7.6.md)) to the seed policies that authorize them. Every call site MUST have at least one applicable seed policy, or rely on default-deny for intentional denial.

### Command Package (3 call sites)

| # | Call Site                                         | Action    | Resource Type | Applicable Seed Policies                                                                                                            | Notes                                                                       |
| - | ------------------------------------------------- | --------- | ------------- | ----------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------- |
| 1 | `dispatcher.go:157` — command execution           | `execute` | `command`     | `seed:player-basic-commands` (say/pose/look/go), `seed:builder-commands` (dig/create/describe/link), `seed:admin-full-access` (all) | Unlisted commands: default-deny for players; admins covered by full-access  |
| 2 | `rate_limit_middleware.go:39` — rate limit bypass | `execute` | `capability`  | `seed:admin-full-access`                                                                                                            | Only admins bypass rate limits; players denied via default-deny             |
| 3 | `handlers/boot.go:52` — boot command              | `execute` | `admin.boot`  | `seed:admin-full-access`                                                                                                            | Admin-only operation; no specific seed policy — relies on admin full-access |

### World Service — Location Operations (6 call sites)

| # | Call Site                             | Action   | Resource Type | Applicable Seed Policies                                | Notes                                              |
| - | ------------------------------------- | -------- | ------------- | ------------------------------------------------------- | -------------------------------------------------- |
| 4 | `service.go:74` — GetLocation         | `read`   | `location`    | `seed:player-location-read`, `seed:admin-full-access`   | Player reads own current location; admin reads any |
| 5 | `service.go:94` — CreateLocation      | `write`  | `location`    | `seed:builder-location-write`, `seed:admin-full-access` | Builder/admin only                                 |
| 6 | `service.go:123` — UpdateLocation     | `write`  | `location`    | `seed:builder-location-write`, `seed:admin-full-access` | Builder/admin only                                 |
| 7 | `service.go:144` — DeleteLocation     | `delete` | `location`    | `seed:builder-location-write`, `seed:admin-full-access` | Builder/admin write+delete; admin full-access      |
| 8 | `service.go:796` — FindLocationByName | `read`   | `location`    | `seed:player-location-read`, `seed:admin-full-access`   | Player scoped to current location; admin reads any |
| 9 | `service.go:655` — ExamineLocation    | `read`   | `location`    | `seed:player-location-read`, `seed:admin-full-access`   | Player examines current location                   |

### World Service — Exit Operations (5 call sites)

| #  | Call Site                             | Action   | Resource Type | Applicable Seed Policies                              | Notes                                                                                                                                            |
| -- | ------------------------------------- | -------- | ------------- | ----------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| 10 | `service.go:162` — GetExit            | `read`   | `exit`        | `seed:admin-full-access`                              | **GAP**: No player-level seed policy for reading exits. Players rely on default-deny unless exit is in current location (see Gap Analysis below) |
| 11 | `service.go:185` — CreateExit         | `write`  | `exit`        | `seed:admin-full-access`                              | **GAP**: No `seed:builder-exit-write` policy. Builders cannot create exits unless admin. See Gap Analysis                                        |
| 12 | `service.go:217` — UpdateExit         | `write`  | `exit`        | `seed:admin-full-access`                              | **GAP**: Same as #11 — no builder-level exit write policy                                                                                        |
| 13 | `service.go:241` — DeleteExit         | `delete` | `exit`        | `seed:admin-full-access`                              | **GAP**: Same as #11 — no builder-level exit delete policy                                                                                       |
| 14 | `service.go:279` — GetExitsByLocation | `read`   | `location`    | `seed:player-location-read`, `seed:admin-full-access` | Resource is location (not exit), so location-read applies                                                                                        |

### World Service — Object Operations (6 call sites)

| #  | Call Site                        | Action   | Resource Type | Applicable Seed Policies                                  | Notes                                                                                                                                  |
| -- | -------------------------------- | -------- | ------------- | --------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| 15 | `service.go:295` — GetObject     | `read`   | `object`      | `seed:player-object-colocation`, `seed:admin-full-access` | Player reads co-located objects                                                                                                        |
| 16 | `service.go:315` — CreateObject  | `write`  | `object`      | `seed:builder-object-write`, `seed:admin-full-access`     | Builder/admin only                                                                                                                     |
| 17 | `service.go:347` — UpdateObject  | `write`  | `object`      | `seed:builder-object-write`, `seed:admin-full-access`     | Builder/admin only                                                                                                                     |
| 18 | `service.go:371` — DeleteObject  | `delete` | `object`      | `seed:builder-object-write`, `seed:admin-full-access`     | Builder/admin write+delete                                                                                                             |
| 19 | `service.go:401` — MoveObject    | `write`  | `object`      | `seed:builder-object-write`, `seed:admin-full-access`     | **NOTE**: Players cannot move objects unless builder/admin — may need `seed:player-object-move` if players should pick up/drop objects |
| 20 | `service.go:713` — ExamineObject | `read`   | `object`      | `seed:player-object-colocation`, `seed:admin-full-access` | Player examines co-located object                                                                                                      |

### World Service — Character Operations (5 call sites)

| #  | Call Site                                  | Action            | Resource Type | Applicable Seed Policies                                                                                   | Notes                                                                                                                           |
| -- | ------------------------------------------ | ----------------- | ------------- | ---------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| 21 | `service.go:452` — GetCharacter            | `read`            | `character`   | `seed:player-self-access` (own), `seed:player-character-colocation` (co-located), `seed:admin-full-access` |                                                                                                                                 |
| 22 | `service.go:471` — GetCharactersByLocation | `list_characters` | `location`    | `seed:admin-full-access`                                                                                   | **GAP**: No seed policy covers `list_characters` action for players. Compound resource decomposed per ADR #76. See Gap Analysis |
| 23 | `service.go:558` — MoveCharacter           | `write`           | `character`   | `seed:player-self-access` (own character), `seed:admin-full-access`                                        | Player writes own character; movement also requires `seed:player-movement` (enter location) + `seed:player-exit-use` (use exit) |
| 24 | `service.go:768` — ExamineCharacter        | `read`            | `character`   | `seed:player-self-access` (own), `seed:player-character-colocation` (co-located), `seed:admin-full-access` |                                                                                                                                 |

### World Service — Scene Operations (3 call sites)

| #  | Call Site                                 | Action  | Resource Type | Applicable Seed Policies | Notes                                                                  |
| -- | ----------------------------------------- | ------- | ------------- | ------------------------ | ---------------------------------------------------------------------- |
| 25 | `service.go:488` — AddSceneParticipant    | `write` | `scene`       | `seed:admin-full-access` | **GAP**: No player-level seed policy for scene write. See Gap Analysis |
| 26 | `service.go:509` — RemoveSceneParticipant | `write` | `scene`       | `seed:admin-full-access` | **GAP**: Same as #25                                                   |
| 27 | `service.go:527` — ListSceneParticipants  | `read`  | `scene`       | `seed:admin-full-access` | **GAP**: No player-level seed policy for scene read. See Gap Analysis  |

### Plugin Package (1 call site)

| #  | Call Site                                   | Action    | Resource Type    | Applicable Seed Policies | Notes                                                                                                                                   |
| -- | ------------------------------------------- | --------- | ---------------- | ------------------------ | --------------------------------------------------------------------------------------------------------------------------------------- |
| 28 | `hostfunc/commands.go:119` — plugin command | `execute` | `plugin_command` | `seed:admin-full-access` | **GAP**: No seed policy for `plugin_command` resource type. Plugin commands rely on admin full-access or default-deny. See Gap Analysis |

### Gap Analysis

The following call sites have no player/builder-level seed policy and rely solely on `seed:admin-full-access` or default-deny:

| Gap | Call Sites                                   | Resource Type                  | Missing Policy                             | Severity   | Recommendation                                                                                                                                                                                          |
| --- | -------------------------------------------- | ------------------------------ | ------------------------------------------ | ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| G1  | #10 (GetExit)                                | `exit` (read)                  | `seed:player-exit-read`                    | **Medium** | Players need to read exits to navigate. `seed:player-exit-use` covers `use` action but not `read`. Add `seed:player-exit-read` or extend colocation policy to exits                                     |
| G2  | #11-#13 (CreateExit, UpdateExit, DeleteExit) | `exit` (write/delete)          | `seed:builder-exit-write`                  | **Medium** | Builders can write locations and objects but not exits. Exits are typically created alongside locations. Add `seed:builder-exit-write`                                                                  |
| G3  | #22 (GetCharactersByLocation)                | `location` (`list_characters`) | Player policy for `list_characters` action | **High**   | Compound resource decomposition created a new action not covered by any seed policy. `seed:player-location-read` only covers `read` action. Add `list_characters` to location-read or create new policy |
| G4  | #25-#27 (Scene operations)                   | `scene` (read/write)           | `seed:player-scene-access`                 | **Medium** | Scenes are a core RP feature. Players need to join/leave/view scenes. Add scene access policies                                                                                                         |
| G5  | #28 (Plugin commands)                        | `plugin_command` (execute)     | `seed:player-plugin-commands`              | **Low**    | Plugin commands are game-specific. Individual plugins SHOULD define their own ABAC policies at install time. Default-deny is correct baseline; document that plugin authors must create policies        |
| G6  | #19 (MoveObject)                             | `object` (write)               | `seed:player-object-move`                  | **Low**    | If players should be able to pick up/drop objects, a separate policy is needed. Current `seed:builder-object-write` only covers builders. May be intentional — evaluate game design requirements        |

**Summary:** 22 of 28 call sites are fully covered by seed policies. 6 gaps identified (G1-G6). Gaps G1-G4 are functional issues that will block normal gameplay if not addressed before migration. G5-G6 are design decisions that may be intentional.

**Recommended Actions:**

1. **G1+G2 (Exits):** Add `seed:player-exit-read` and `seed:builder-exit-write` to Task 22 seed policies (raises count from 18 to 20)
2. **G3 (list_characters):** Expand `seed:player-location-read` to include `list_characters` action, OR add a dedicated `seed:player-location-list-characters` policy
3. **G4 (Scenes):** Add `seed:player-scene-participant` (read+write own scenes) and `seed:player-scene-read` (read scenes in current location)
4. **G5 (Plugin commands):** Document in plugin developer guide that plugins MUST define their own ABAC policies. No seed policy change needed
5. **G6 (MoveObject):** Defer to game design review — if players can pick up/drop, add policy; if builder-only, document as intentional

---

> **[Back to Overview](./2026-02-06-full-abac-implementation.md)** | **[Previous: Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)** | **[Next: Phase 7.5](./2026-02-06-full-abac-phase-7.5.md)**
