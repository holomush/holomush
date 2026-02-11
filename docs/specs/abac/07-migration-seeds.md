<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

## Replacing Static Roles

**Design decision:** HoloMUSH has no production releases. The static
`AccessControl` system from Epic 3 is replaced entirely by `AccessPolicyEngine`.
There is no backward-compatibility adapter, no shadow mode, and no incremental
migration. All call sites switch to `Evaluate()` directly. This simplifies the
design and eliminates an entire class of complexity (normalization helpers,
migration adapters, shadow mode metrics, cutover criteria). See
[decision #36](../decisions/epic7/phase-7.6/036-direct-replacement-no-adapter.md)
and [decision #37](../decisions/epic7/phase-7.6/037-no-shadow-mode.md)
in the decisions log.

### Seed Policies

The seed policies define the default permission model. They use the ABAC
engine's full capabilities (attribute-based conditions, `enter` action for
location control) rather than replicating the static system's limitations.

**Seed version tracking:** Each seed policy is tracked with a `seed_version`
field in the database (see [Policy Storage](05-storage-audit.md#schema)). The
initial version is 1. Server upgrades that ship updated seed text with an
incremented version number automatically update the corresponding seed policy
during bootstrap. The DSL examples below represent version 1 of each seed
policy.

```text
// seed:player-self-access (seed_version: 1)
permit(principal is character, action in ["read", "write"], resource is character)
when { resource.id == principal.id };

// seed:player-location-read (seed_version: 1)
permit(principal is character, action in ["read"], resource is location)
when { resource.id == principal.location };

// seed:player-character-colocation (seed_version: 1)
permit(principal is character, action in ["read"], resource is character)
when { resource.location == principal.location };

// seed:player-object-colocation (seed_version: 1)
permit(principal is character, action in ["read"], resource is object)
when { resource.location == principal.location };

// seed:player-stream-emit (seed_version: 1)
permit(principal is character, action in ["emit"], resource is stream)
when { resource.name like "location:*" && resource.location == principal.location };

// seed:player-movement (seed_version: 1)
// Intentionally unconditional — movement is allowed by default for all
// characters. Admins restrict specific locations via forbid policies
// (deny-overrides ensures forbid always wins over this permit).
permit(principal is character, action in ["enter"], resource is location);

// seed:player-basic-commands (seed_version: 1)
permit(principal is character, action in ["execute"], resource is command)
when { resource.name in ["say", "pose", "look", "go"] };

// seed:builder-location-write (seed_version: 1)
permit(principal is character, action in ["write", "delete"], resource is location)
when { principal.role in ["builder", "admin"] };

// seed:builder-object-write (seed_version: 1)
permit(principal is character, action in ["write", "delete"], resource is object)
when { principal.role in ["builder", "admin"] };

// seed:builder-commands (seed_version: 1)
permit(principal is character, action in ["execute"], resource is command)
when { principal.role in ["builder", "admin"]
    && resource.name in ["dig", "create", "describe", "link"] };

// seed:admin-full-access (seed_version: 1)
permit(principal is character, action, resource)
when { principal.role == "admin" };

// seed:property-public-read (seed_version: 1)
// Public properties: readable by characters in the same location as the parent
permit(principal is character, action in ["read"], resource is property)
when { resource.visibility == "public"
    && principal.location == resource.parent_location };

// seed:property-private-read (seed_version: 1)
// Private properties: readable only by owner
permit(principal is character, action in ["read"], resource is property)
when { resource.visibility == "private"
    && resource.owner == principal.id };

// seed:property-admin-read (seed_version: 1)
// Admin properties: readable only by admins
permit(principal is character, action in ["read"], resource is property)
when { resource.visibility == "admin" && principal.role == "admin" };

// System properties (visibility == "system") are protected by default-deny.
// No seed policy grants access to them, so they remain inaccessible to all
// characters (including admins). This is intentional — system properties are
// reserved for internal use by the platform itself, not player access.
// We rely on default-deny instead of an explicit forbid policy because:
// 1. Under deny-overrides conflict resolution, a forbid would block even
//    seed:admin-full-access (permit), locking admins out permanently.
// 2. Default-deny still provides full audit attribution (effect=default_deny
//    is logged), so the forbid comment about "audit attribution" was incorrect.

// seed:property-owner-write (seed_version: 1)
// Property owners can write and delete their properties
permit(principal is character, action in ["write", "delete"], resource is property)
when { resource.owner == principal.id };

// seed:property-restricted-visible-to (seed_version: 1)
// Restricted properties: readable by characters in the visible_to list
permit(principal is character, action in ["read"], resource is property)
when { resource.visibility == "restricted"
    && resource has visible_to
    && principal.id in resource.visible_to };

// seed:property-restricted-excluded (seed_version: 1)
// Restricted properties: denied to characters in the excluded_from list
forbid(principal is character, action in ["read"], resource is property)
when { resource.visibility == "restricted"
    && resource has excluded_from
    && principal.id in resource.excluded_from };

// seed:player-exit-use (seed_version: 1)
// Exit usage: allow characters to use exits (target matching only)
// Full exit attribute resolution deferred (see holomush-5k1.422)
permit(principal is character, action in ["use"], resource is exit);
```

The comment preceding each policy IS the deterministic name used during
bootstrap (e.g., `seed:player-self-access`). Each name is prefixed with
`seed:` to prevent collision with admin-created policies. Property visibility
policies (`seed:property-*`) are also defined in [Visibility Seed Policies](03-property-model.md#visibility-seed-policies)
with additional implementation context. Exit policies (`seed:player-exit-use`)
use target matching only — full attribute resolution is deferred (see [Decision #88](../decisions/epic7/phase-7.3/088-exit-scene-provider-stubs.md)).

### Bootstrap Sequence

On first startup (or when the `access_policies` table is empty), the server
MUST seed policies automatically:

1. Server startup detects empty `access_policies` table
2. Server inserts all seed policies via `PolicyStore.Create()` with `system`
   subject context (NOT via `policy create` commands, which require ABAC
   evaluation that isn't yet available)
3. The `system` subject bypasses policy evaluation entirely (step 1 of the
   evaluation algorithm), so no chicken-and-egg problem exists
4. Subsequent policy changes require `execute` permission on `command:policy*`
   resources via normal ABAC evaluation (granted to admins by the seed policies)

**System context mechanism:** The bootstrap process uses an explicit context
marker: `ctx := access.WithSystemSubject(context.Background())`. PolicyStore
CRUD methods (Create, Update, Delete) **MUST** check for this context marker
and bypass authorization when present. When the system context marker is
detected, PolicyStore **MUST NOT** call `Evaluate()` for permission checks.
This pattern is explicit, not implicit — callers must deliberately wrap the
context to signal system-level operations.

```go
// Example bootstrap usage
ctx := access.WithSystemSubject(context.Background())
err := policyStore.Create(ctx, seedPolicy)
// PolicyStore.Create checks access.IsSystemContext(ctx)
// and skips Evaluate() call if true
```

The seed process is idempotent — policies are inserted with deterministic names
(e.g., `seed:player-self-access`). The bootstrap checks
`WHERE name = ? AND source = 'seed'`. If a seed policy with that name and
source already exists, it is skipped. If a policy exists with the seed name but
`source != 'seed'` (e.g., an admin accidentally used a `seed:` name), the
bootstrap logs a warning and skips (does not overwrite admin customizations).
The `seed:` prefix is reserved for system use — the `policy create` command
MUST reject names starting with `seed:` to prevent admins from accidentally
colliding with seed policies.

**Seed policy upgrades:** Seed policies track a `seed_version` integer field
(default: 1). Server upgrades that ship updated seed text with an incremented
version number **MUST** automatically update the corresponding seed policy
during bootstrap. The bootstrap process compares `seed_version` in the
shipped seed definition against the stored policy's `seed_version`. If the
shipped version is greater, the policy's `dsl_text` and `compiled_ast` are
updated, and `seed_version` is incremented. The `change_note` field is
populated with `"Auto-upgraded from seed v{N} to v{N+1} on server upgrade"`.
This enables automatic security patches and bug fixes for seed policies
without manual intervention.

**Opt-out mechanism:** Operators **MAY** disable automatic seed upgrades via
the `--skip-seed-migrations` server startup flag. When this flag is set,
the bootstrap process skips seed version comparisons and does not update
existing seed policies, regardless of version mismatch. This preserves admin
customizations to seed policies (via `policy edit`) but prevents automatic
security patching. Operators using this flag **MUST** manually track seed
policy updates and apply fixes via `policy edit` or explicit migrations.

**Rollback mechanism:** If a buggy seed policy locks out admins (e.g., by
removing `execute` permission on `command:policy*` required to edit policies),
operators **MAY** use the `--force-seed-version=N` startup flag to force
downgrade to a specific seed version. The bootstrap process treats this flag
as the "current" shipped version and downgrades stored policies if their
`seed_version > N`. Emergency recovery SQL: `UPDATE access_policies SET enabled = false WHERE source = 'seed' AND seed_version > N;` disables all seed
policies newer than version N, allowing manual fix via `policy edit`. Operators
**SHOULD** test seed policy changes in staging environments before deploying to
production to avoid lockout scenarios.

**Version mismatch detection:** The `policy seed status` admin command
compares installed seed policies against shipped seed definitions and
reports version discrepancies. Example output:

```text
> policy seed status
seed:player-self-access       v1 (current: v2 available) — OUTDATED
seed:admin-full-access        v2 (current: v2)           — UP TO DATE
seed:location-visibility      v1 (current: v1)           — UP TO DATE
```

If any seed policy has `installed_version < shipped_version`, the command
prints a summary line: `"2 seed policies outdated — restart without
--skip-seed-migrations to auto-upgrade"`. If `--skip-seed-migrations` is NOT
set but seed policies are outdated (should not occur in normal operation),
the server logs a CRITICAL-level message during bootstrap:
`"Seed policy version mismatch detected: {policy_name} installed v{N},
shipped v{M} — restart to apply auto-upgrade"`. This ensures operators are
alerted to configuration drift.

**Seed verification:** Implementation **MUST** include two verification
mechanisms:

1. **CLI flag `--validate-seeds`:** A startup flag that boots the DSL
   compiler, validates all seed policy DSL text, and exits with a
   success/failure status without starting the server. This enables
   pre-deployment verification and CI integration (e.g.,
   `holomush --validate-seeds` in the build pipeline).

2. **`policy seed verify` admin command:** Compares installed seed
   policies against the shipped seed text and highlights differences.
   This enables operators to discover when they are running with
   modified seeds and whether a shipped fix applies to their customized
   version.

Seed policy fixes **SHOULD** be shipped as explicit migration files with
before/after diffs and a human-readable change note.

**Migration testing requirements:** Each seed policy **MUST** have an
integration test suite with at least three scenarios: allowed operation,
denied operation, and edge case (e.g., missing attribute, boundary condition).
Direct replacement of ~28 call sites without shadow mode creates migration
risk — a single seed policy bug can cause platform-wide authorization
failures. No call site migration **MAY** proceed without passing integration
tests for all affected seed policies. Coverage target: 100% of seed policies
tested before Phase 7.3 cutover.

**Security requirement (S8 - holomush-5k1.355):** Migration of ~28 production
call sites from `AccessControl.Check()` to `AccessPolicyEngine.Evaluate()`
requires static analysis verification. A static analysis rule or go vet check
MUST verify no remaining `AccessControl.Check()` calls post-migration. CI MUST
enforce this check. Migration checklist MUST include per-site verification to
prevent privilege escalation from incorrect migration.

#### Seed Policy Migrations

When a server version needs to fix a seed policy bug or update seed policy
logic, the fix MUST be delivered via a migration script that calls
`PolicyStore.UpdateSeed(ctx, name, dsl)`. This method bypasses the normal
collision check and updates the seed policy in-place, even if it has been
customized by admins.

**UpdateSeed behavior:**

1. Check if a policy with the given name exists and has `source = 'seed'`.
2. If the policy does not exist or `source != 'seed'`, the migration MUST fail
   with an error.
3. If the policy's current DSL matches the new DSL exactly (byte-for-byte), the
   update is skipped (idempotent).
4. If the policy has been customized by admins (detected by comparing the stored
   DSL against the previously shipped seed text from the prior version), log a
   **warning** and skip the update. The warning MUST include the policy name,
   the fact that it was customized, and a recommendation to review the change
   manually.
5. Otherwise, update the policy's DSL and compiled condition, log at INFO level
   with the policy name and change note, and invalidate the policy cache.

**Customization detection:** The migration script embeds both the **old seed
text** (from the version being upgraded from) and the **new seed text** (from
the version being upgraded to). During migration, `UpdateSeed` compares the
stored policy DSL against the old seed text. If they match, the policy is
unchanged from the shipped default and the update proceeds. If they differ,
the policy was customized and the update is skipped with a warning.

**Example migration:**

```go
// 2026-02-10-fix-player-self-access.go
// NOTE: This is a hypothetical version 1→2 migration example.
// The current seed policy already includes both "read" and "write" actions.
// This example shows how a migration would add "write" if upgrading from
// a hypothetical v1 that only had "read".
func up(ctx context.Context, store PolicyStore) error {
    oldDSL := `permit(principal is character, action in ["read"], resource is character)
when { resource.id == principal.id };`
    newDSL := `permit(principal is character, action in ["read", "write"], resource is character)
when { resource.id == principal.id };`

    return store.UpdateSeed(ctx, "seed:player-self-access", oldDSL, newDSL,
        "Add 'write' action to player-self-access seed policy")
}
```

**Integration test requirement:** Tests MUST verify that `UpdateSeed` correctly
skips customized seeds and logs the appropriate warning.

### Wildcard Resource Mapping

The static role system from Epic 3 used wildcard resource patterns to grant permissions across entire resource types (e.g., `location:*` granted access to all locations). The ABAC migration maps these wildcards to type-level target matching using the `resource is <type>` pattern.

| Old Wildcard Pattern | ABAC Equivalent        | Seed Policy Reference                                                              | Explanation                                                                                                                                                                     |
| -------------------- | ---------------------- | ---------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `location:*`         | `resource is location` | `seed:player-location-read`, `seed:player-movement`, `seed:builder-location-write` | Type-level target matching: `resource is location` matches any location entity, regardless of ID                                                                                |
| `exit:*`             | `resource is exit`     | `seed:player-exit-use`                                                             | Type-level target matching: `resource is exit` matches any exit entity                                                                                                          |
| `object:*`           | `resource is object`   | `seed:player-object-colocation`, `seed:builder-object-write`                       | Type-level target matching: `resource is object` matches any object entity                                                                                                      |
| `command:*`          | `resource is command`  | `seed:player-basic-commands`, `seed:builder-commands`                              | Type-level target matching: `resource is command` matches any command resource by name                                                                                          |
| `stream:*`           | `resource is stream`   | `seed:player-stream-emit`                                                          | Type-level target matching: `resource is stream` matches any event stream; builder rules may further constrain by stream name pattern (e.g., `resource.name like "location:*"`) |
| `*` (all resources)  | No target clause       | `seed:admin-full-access`                                                           | Omitting the target clause matches any resource type; combined with role conditions, grants global permissions                                                                  |

**Migration principle:** Any static role policy that granted `action` on `<type>:*` becomes a seed policy with `action` on `resource is <type>`, plus additional conditions to replicate the old system's behavior (e.g., role checks for builder/admin permissions, location filters for player colocation rules).

### Implementation Sequence

1. **Phase 7.1 (Policy Schema):** Create DB tables and policy store.

2. **Phase 7.2 (DSL & Compiler):** Build DSL parser using
   [participle](https://github.com/alecthomas/participle) (struct-tag parser
   generator), evaluator, and `PolicyCompiler`. Participle is preferred over
   `goyacc` (requires separate `.y` grammar file and manual AST mapping) and
   hand-rolled recursive descent (more code, harder to maintain) because its
   struct-tag approach generates Go AST structs directly from grammar
   annotations, eliminating the mapping layer. **Note:** [Decision #41](../decisions/epic7/phase-7.2/041-ll1-parser-disambiguation.md) specifies
   LL(1) disambiguation as the grammar design intent (one-token lookahead to
   resolve ambiguities). Participle uses PEG-style ordered-choice semantics
   which achieve the same disambiguation effect — the first matching alternative
   is selected. Implementers MUST verify disambiguation behavior with test cases
   regardless of parser choice. Mandate fuzz testing for all parser entry
   points. Unit test with table-driven tests.

3. **Phase 7.3 (Policy Engine):** Build `AccessPolicyEngine`, attribute
   providers, and audit logger. Replace `AccessControl` with
   `AccessPolicyEngine` in dependency injection. Update all call sites to use
   `Evaluate()` directly. All call sites MUST use `character:` prefix
   (via `SubjectCharacter` constant from T6, Phase 7.1) per
   [ADR #13](../decisions/epic7/phase-7.1/013-subject-prefix-normalization.md).
   The engine MUST reject `char:` prefix with a clear error.

4. **Phase 7.4 (Seed & Bootstrap):** Seed policies on first startup. Verify
   with integration tests.

5. **Phase 7.5 (Locks & Admin):** Build lock system, admin commands, property
   model.

6. **Phase 7.6 (Cleanup):** Remove `StaticAccessControl`, `AccessControl`
   interface, `capability.Enforcer`, and all related code. Remove legacy
   `char:` prefix handling and `@`-prefixed command name support from the
   codebase. **Note:** The `@` prefix removal MUST happen before or
   concurrently with Phase 7.4 seed policy creation, since seed policies
   reference command names without the `@` prefix (e.g., `"dig"` not
   `"@dig"`). If Phase 7.6 cleanup is deferred, seed policies MUST use
   the current `@`-prefixed names and be updated when the prefix is removed.

**Call site inventory** (packages to update from `AccessControl` to
`AccessPolicyEngine`):

| Package                                  | Usage                               |
| ---------------------------------------- | ----------------------------------- |
| `internal/command/dispatcher`            | Command execution authorization     |
| `internal/command/rate_limit_middleware` | Rate limit bypass for admins        |
| `internal/command/handlers/boot`         | Boot command permission check       |
| `internal/world/service`                 | World model operation authorization |
| `internal/plugin/hostfunc/commands`      | Plugin command execution auth       |
| `internal/core/broadcaster` (test)       | Test mock injection                 |

This list is derived from the current codebase. Run `grep -r "AccessControl"
internal/ --include="*.go"` to get the current inventory before starting
phase 7.3.

### Plugin Capability Migration

The current `capability.Enforcer` handles plugin permissions separately. Under
ABAC, plugin manifests become seed policies. The Enforcer is removed alongside
`StaticAccessControl` in phase 7.6.
