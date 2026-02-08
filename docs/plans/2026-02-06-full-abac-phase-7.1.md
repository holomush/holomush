<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

> **[Back to Overview](./2026-02-06-full-abac-implementation.md)** | **[Next: Phase 7.2](./2026-02-06-full-abac-phase-7.2.md)**

## Phase 7.1: Policy Schema (Database Tables + Policy Store)

> **Note:** Migration numbers in this phase (000015, 000016, 000017) are relative to the current latest migration `000014_aliases`. If other migrations merge before this work, these numbers MUST be updated to avoid collisions.

### Task 0: AST Serialization Spike

**Purpose:** Validate that participle-generated AST nodes can survive JSON serialization round-trips BEFORE implementing the policy storage and compiler. This spike prevents discovering storage model failures at Task 12 ([Phase 7.2](./2026-02-06-full-abac-phase-7.2.md)) after 11 tasks are complete.

**Spec References:** Policy DSL > Grammar (lines 737-946), Policy Storage > Schema (lines 1973-2114)

**Acceptance Criteria:**

- [ ] Parse sample policy DSL string into participle AST
- [ ] Marshal AST to JSON using `json.Marshal`
- [ ] Unmarshal JSON back to AST using `json.Unmarshal`
- [ ] Compare original AST to round-tripped AST (deep equality check)
- [ ] If round-trip fails, document alternative serialization approach (custom MarshalJSON/UnmarshalJSON or switch to protobuf)
- [ ] Spike findings documented in commit message or inline comments
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/dsl/ast_spike_test.go` (temporary spike test file)

**Implementation Steps:**

**Step 1: Write spike test**

```go
// internal/access/policy/dsl/ast_spike_test.go
package dsl_test

import (
    "encoding/json"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestAST_JSONRoundTrip_Spike(t *testing.T) {
    // Sample DSL policy
    dslText := `permit(principal is character, action in ["read"], resource is location) when { resource.id == principal.location };`

    // Step 1: Parse DSL to AST (use parser once Task 8 AST types exist)
    // For spike: define minimal AST types inline or use placeholder struct

    // Step 2: Marshal to JSON
    jsonBytes, err := json.Marshal(ast)
    require.NoError(t, err, "AST marshaling should succeed")

    // Step 3: Unmarshal back to AST
    var roundTripped PolicyAST
    err = json.Unmarshal(jsonBytes, &roundTripped)
    require.NoError(t, err, "AST unmarshaling should succeed")

    // Step 4: Compare
    assert.Equal(t, ast, roundTripped, "Round-tripped AST must match original")
}
```

**Step 2: Define minimal AST types for spike**

Create just enough AST structure to represent a simple policy. Use participle struct tags if available, or plain structs if not. Focus on proving the round-trip works.

**Step 3: Run test and verify**

```bash
task test
```

Expected: Test PASSES — AST round-trips correctly through JSON.

If test FAILS: Document the failure mode (unexported fields, interface types, pointer semantics) and propose fix (custom MarshalJSON, protobuf, etc).

**Step 4: Commit spike findings**

```bash
git add internal/access/policy/dsl/ast_spike_test.go
git commit -m "spike(access): validate AST JSON serialization round-trip

Confirms participle AST nodes survive json.Marshal/Unmarshal.
[If failure: describe issue and proposed fix]"
```

**Step 5: Clean up or keep**

- If spike PASSES: Keep test file as regression test, proceed to Task 1
- If spike FAILS: Document findings, update Task 7 and Task 8 ([Phase 7.2](./2026-02-06-full-abac-phase-7.2.md)) to use alternative serialization

**Notes:**

- This spike is intentionally minimal — just enough to prove/disprove the storage model
- If participle ASTs use unexported fields or interface types that break JSON, we discover it NOW instead of at Task 12 ([Phase 7.2](./2026-02-06-full-abac-phase-7.2.md))
- Alternative serialization approaches: custom `MarshalJSON`/`UnmarshalJSON`, protobuf, gob encoding

---

### Task 1: Create access\_policies migration

**Spec References:** Policy Storage > Schema (lines 1973-2114)

**Acceptance Criteria:**

- [ ] `access_policies` table matches spec schema exactly (columns, types, constraints, CHECK values)
- [ ] `access_policy_versions` table created with foreign key to `access_policies`
- [ ] Partial index `idx_policies_enabled` on `enabled = true`
- [ ] Up migration applies cleanly; down migration reverses it
- [ ] Column `source` CHECK constraint includes all four values: `seed`, `lock`, `admin`, `plugin`

**Files:**

- Create: `internal/store/migrations/000015_access_policies.up.sql`
- Create: `internal/store/migrations/000015_access_policies.down.sql`

**Step 1: Write the up migration**

```sql
-- internal/store/migrations/000015_access_policies.up.sql
CREATE TABLE access_policies (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL UNIQUE,
    description  TEXT,
    effect       TEXT NOT NULL CHECK (effect IN ('permit', 'forbid')),
    source       TEXT NOT NULL DEFAULT 'admin'
                 CHECK (source IN ('seed', 'lock', 'admin', 'plugin')),
    dsl_text     TEXT NOT NULL,
    compiled_ast JSONB NOT NULL,
    enabled      BOOLEAN NOT NULL DEFAULT true,
    seed_version INTEGER DEFAULT NULL,
    created_by   TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    version      INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX idx_policies_enabled ON access_policies(enabled) WHERE enabled = true;

CREATE TABLE access_policy_versions (
    id          TEXT PRIMARY KEY,
    policy_id   TEXT NOT NULL REFERENCES access_policies(id) ON DELETE CASCADE,
    version     INTEGER NOT NULL,
    dsl_text    TEXT NOT NULL,
    changed_by  TEXT NOT NULL,
    changed_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    change_note TEXT,
    UNIQUE(policy_id, version)
);
```

**Step 2: Write the down migration**

```sql
-- internal/store/migrations/000015_access_policies.down.sql
DROP TABLE IF EXISTS access_policy_versions;
DROP TABLE IF EXISTS access_policies;
```

**Step 3: Verify migration applies**

Run: `task test` (migrations are tested via integration tests)

**Step 4: Commit**

```bash
git add internal/store/migrations/000015_access_policies.*
git commit -m "feat(access): add access_policies and access_policy_versions tables"
```

---

### Task 2: Create access\_audit\_log migration

**Spec References:** Policy Storage > Schema (lines 1973-2114), Policy Storage > Audit Log Serialization (lines 2161-2179)

**Acceptance Criteria:**

- [ ] `access_audit_log` table uses `PARTITION BY RANGE (timestamp)` from day one
- [ ] Composite PRIMARY KEY (id, timestamp) includes partition key per PostgreSQL requirement
- [ ] SQL comment documents PK deviation from spec and rationale
- [ ] At least 3 initial monthly partitions created (current month + 2 future months)
- [ ] Partition naming follows spec convention: `access_audit_log_YYYY_MM`
- [ ] BRIN index on `timestamp` with `pages_per_range = 128`
- [ ] Subject + timestamp DESC index for per-subject queries
- [ ] Denied-only partial index for denial analysis queries
- [ ] `effect` CHECK constraint includes: `allow`, `deny`, `default_deny`, `system_bypass`
- [ ] Up migration applies cleanly; down migration reverses it
- [ ] Note added flagging spec update needed for PK inconsistency

**Files:**

- Create: `internal/store/migrations/000016_access_audit_log.up.sql`
- Create: `internal/store/migrations/000016_access_audit_log.down.sql`

**Step 1: Write the up migration**

The spec requires monthly range partitioning from day one (retrofitting is impractical at 10M rows/day). Use BRIN index on timestamp for efficient time-range scans.

```sql
-- internal/store/migrations/000016_access_audit_log.up.sql
CREATE TABLE access_audit_log (
    id              TEXT NOT NULL,
    timestamp       TIMESTAMPTZ NOT NULL DEFAULT now(),
    subject         TEXT NOT NULL,
    action          TEXT NOT NULL,
    resource        TEXT NOT NULL,
    effect          TEXT NOT NULL CHECK (effect IN ('allow', 'deny', 'default_deny', 'system_bypass')),
    policy_id       TEXT,
    policy_name     TEXT,
    attributes      JSONB,
    error_message   TEXT,
    provider_errors JSONB,
    duration_us     INTEGER,
    -- DEVIATION FROM SPEC: Composite PK required because PostgreSQL partitioned
    -- tables MUST include the partition key (timestamp) in the primary key.
    -- Spec Policy Storage > Audit Log Serialization (line ~2015) defines "id TEXT PRIMARY KEY" which is technically
    -- incorrect for partitioned tables. This needs to be corrected in the spec.
    PRIMARY KEY (id, timestamp)
) PARTITION BY RANGE (timestamp);

-- Partitions created dynamically by Go migration code (see below)

CREATE INDEX idx_audit_log_timestamp ON access_audit_log USING BRIN (timestamp)
    WITH (pages_per_range = 128);
CREATE INDEX idx_audit_log_subject ON access_audit_log(subject, timestamp DESC);
CREATE INDEX idx_audit_log_denied ON access_audit_log(effect, timestamp DESC)
    WHERE effect IN ('deny', 'default_deny');
```

```go
// Dynamic partition creation in Go migration
// Creates current month + 2 future months
func createPartitions(tx *sql.Tx) error {
    now := time.Now()
    for i := 0; i < 3; i++ {
        month := now.AddDate(0, i, 0)
        start := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
        end := start.AddDate(0, 1, 0)
        name := fmt.Sprintf("access_audit_log_%d_%02d", start.Year(), start.Month())
        sql := fmt.Sprintf(
            "CREATE TABLE %s PARTITION OF access_audit_log FOR VALUES FROM ('%s') TO ('%s')",
            name, start.Format("2006-01-02"), end.Format("2006-01-02"),
        )
        if _, err := tx.Exec(sql); err != nil {
            return err
        }
    }
    return nil
}
```

> **Note:** Partition creation uses dynamic Go migration code that generates boundaries based on deployment date. The SQL above shows the table schema only; actual partition creation happens in Go.

**Step 2: Write the down migration**

```sql
-- internal/store/migrations/000016_access_audit_log.down.sql
DROP TABLE IF EXISTS access_audit_log;
```

**Step 3: Commit**

```bash
git add internal/store/migrations/000016_access_audit_log.*
git commit -m "feat(access): add access_audit_log table with monthly range partitioning"
```

**NOTE:** The spec (Policy Storage > Audit Log Serialization, line ~2015) defines `id TEXT PRIMARY KEY`, but PostgreSQL partitioned tables require the partition key (`timestamp`) to be included in the primary key. The implementation correctly uses `PRIMARY KEY (id, timestamp)`. **Action required:** Update spec to reflect this PostgreSQL constraint.

---

### Task 3: Create entity\_properties migration

**Spec References:** Policy Storage > Schema (lines 1973-2114), ADR 0013 (Properties as first-class entities)

**Acceptance Criteria:**

- [ ] `entity_properties` table matches spec schema (all columns, types, constraints)
- [ ] `visibility` CHECK includes all five levels: `public`, `private`, `restricted`, `system`, `admin`
- [ ] Unique constraint on `(parent_type, parent_id, name)`
- [ ] Parent index on `(parent_type, parent_id)` for efficient lookups
- [ ] `visibility_restricted_requires_lists` CHECK constraint ensures restricted visibility has non-NULL visible_to and excluded_from
- [ ] `visibility_non_restricted_nulls_lists` CHECK constraint ensures non-restricted visibility has NULL lists
- [ ] `idx_properties_owner` partial index on owner column where owner IS NOT NULL
- [ ] Up migration applies cleanly; down migration reverses it

**Files:**

- Create: `internal/store/migrations/000017_entity_properties.up.sql`
- Create: `internal/store/migrations/000017_entity_properties.down.sql`

**Step 1: Write the up migration**

```sql
-- internal/store/migrations/000017_entity_properties.up.sql
CREATE TABLE entity_properties (
    id            TEXT PRIMARY KEY,
    parent_type   TEXT NOT NULL,
    parent_id     TEXT NOT NULL,
    name          TEXT NOT NULL,
    value         TEXT,
    owner         TEXT,
    visibility    TEXT NOT NULL DEFAULT 'public'
                  CHECK (visibility IN ('public', 'private', 'restricted', 'system', 'admin')),
    flags         JSONB DEFAULT '[]',
    visible_to    JSONB DEFAULT NULL,
    excluded_from JSONB DEFAULT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT entity_properties_parent_name_unique UNIQUE(parent_type, parent_id, name),
    CONSTRAINT visibility_restricted_requires_lists
        CHECK (visibility != 'restricted'
            OR (visible_to IS NOT NULL AND excluded_from IS NOT NULL)),
    CONSTRAINT visibility_non_restricted_nulls_lists
        CHECK (visibility = 'restricted'
            OR (visible_to IS NULL AND excluded_from IS NULL))
);

CREATE INDEX idx_entity_properties_parent ON entity_properties(parent_type, parent_id);
CREATE INDEX idx_properties_owner ON entity_properties(owner) WHERE owner IS NOT NULL;
```

**Step 2: Write the down migration**

```sql
-- internal/store/migrations/000017_entity_properties.down.sql
DROP TABLE IF EXISTS entity_properties;
```

**Step 3: Commit**

```bash
git add internal/store/migrations/000017_entity_properties.*
git commit -m "feat(access): add entity_properties table for first-class property model"
```

---

### Task 4a: EntityProperty type and PropertyRepository

> **Note:** This task was originally Task 25 ([Phase 7.5](./2026-02-06-full-abac-phase-7.5.md)) in Phase 7.5, but moved to Phase 7.1 because PropertyProvider (Task 15 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md))) depends on PropertyRepository existing. The entity_properties migration (Task 3) creates the table, and this task creates the Go types and repository interface/implementation.

> **Scope:** This task creates the new types (EntityProperty + PropertyRepository interface + PostgreSQL implementation) with full CRUD operations and validation logic. Tasks 4b and 4c handle integrating property lifecycle with WorldService.

**Spec References:** Property Model (lines 1097-1294), ADR 0013 (Properties as first-class entities)

**Acceptance Criteria:**

- [ ] `EntityProperty` struct: ID, ParentType, ParentID, Name, Value, Owner, Visibility, Flags, VisibleTo, ExcludedFrom, timestamps
- [ ] `EntityProperty.ID` uses `ulid.ULID` to match existing world model convention (Location, Character, Object all use ulid.ULID)
- [ ] `PropertyRepository` interface: `Create`, `Get`, `ListByParent`, `Update`, `Delete`, `DeleteByParent`
- [ ] CRUD operations round-trip all fields correctly
- [ ] Visibility defaults: `restricted` → auto-set `visible_to=[owner]`, `excluded_from=[]`
- [ ] `visible_to` max 100 entries; `excluded_from` max 100 entries → error if exceeded
- [ ] No overlap between `visible_to` and `excluded_from` → error
- [ ] Parent name uniqueness → error on duplicate `(parent_type, parent_id, name)`
- [ ] `DeleteByParent(ctx, parentType, parentID)` deletes all properties for the given parent entity (for cascade deletion when parent entities are deleted)
- [ ] Follows existing repository pattern from `internal/world/postgres/location_repo.go`
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/world/property.go` (EntityProperty type + PropertyRepository interface)
- Create: `internal/world/postgres/property_repo.go` (PostgreSQL implementation)
- Test: `internal/world/postgres/property_repo_test.go`

**Step 1: Write failing tests (Task 4a)**

- Create property → round-trips all fields
- Get by ID
- List by parent (type + ID)
- Update property (value, visibility, flags)
- Delete property
- Delete by parent (type + ID) → deletes all properties for that parent
- Visibility defaults: `restricted` → auto-set `visible_to=[owner]`, `excluded_from=[]`
- Constraints: `visible_to` max 100 entries, `excluded_from` max 100 entries
- No overlap between `visible_to` and `excluded_from` → error
- Parent name uniqueness → error on duplicate

**Step 2: Implement**

```go
// internal/world/property.go
package world

// EntityProperty is a first-class property attached to a world entity.
type EntityProperty struct {
    ID           ulid.ULID
    ParentType   string // "character", "location", "object"
    ParentID     ulid.ULID
    Name         string
    Value        *string
    Owner        *string
    Visibility   string // "public", "private", "restricted", "system", "admin"
    Flags        []string
    VisibleTo    []string
    ExcludedFrom []string
    CreatedAt    time.Time
    UpdatedAt    time.Time
}

// PropertyRepository manages entity properties.
type PropertyRepository interface {
    Create(ctx context.Context, p *EntityProperty) error
    Get(ctx context.Context, id ulid.ULID) (*EntityProperty, error)
    ListByParent(ctx context.Context, parentType, parentID string) ([]*EntityProperty, error)
    Update(ctx context.Context, p *EntityProperty) error
    Delete(ctx context.Context, id ulid.ULID) error
    DeleteByParent(ctx context.Context, parentType, parentID string) error
}
```

Follow existing repository patterns from `internal/world/postgres/location_repo.go`.

**Step 3: Run tests, commit**

```bash
git add internal/world/property.go internal/world/postgres/property_repo.go
git add internal/world/postgres/property_repo_test.go
git commit -m "feat(world): add EntityProperty type and PostgreSQL repository"
```

---

### Task 4b: WorldService deletion methods

> **Note:** This task creates the missing `DeleteCharacter()` method and ensures all three deletion methods exist in WorldService before Task 4c adds property cascade logic to them.

> **Implementation Note:** `WorldService.DeleteCharacter()` does not currently exist in `internal/world/service.go` and MUST be created as part of this task's scope. `DeleteObject()` and `DeleteLocation()` already exist and are not modified in this task.

> **Scope:** This task creates the missing deletion method with proper transaction handling and tests. Task 4c will add property cascade deletion to all three methods.

**Spec References:** Entity Properties — lifecycle on parent deletion (lines 2070-2113)

**Acceptance Criteria:**

- [ ] `WorldService.DeleteCharacter(ctx context.Context, id string) error` method created
- [ ] DeleteCharacter uses transaction handling (via `s.tx.WithTransaction`)
- [ ] DeleteCharacter calls `s.characterRepo.Delete(ctx, id)` to remove the character
- [ ] DeleteCharacter includes proper error wrapping with oops
- [ ] Tests cover: successful deletion, transaction rollback on error, character not found error
- [ ] All tests pass via `task test`

**Files:**

- Modify: `internal/world/service.go` (DeleteCharacter method)
- Test: `internal/world/service_test.go` (DeleteCharacter tests)

**Step 1: Write failing tests (Task 4b)**

- `WorldService.DeleteCharacter()` deletes a character by ID
- DeleteCharacter handles character not found error
- DeleteCharacter uses transaction (rollback on error)

**Step 2: Implement DeleteCharacter**

Add to `internal/world/service.go`:

```go
func (s *WorldService) DeleteCharacter(ctx context.Context, id string) error {
    return s.tx.WithTransaction(ctx, func(ctx context.Context) error {
        if err := s.characterRepo.Delete(ctx, id); err != nil {
            return oops.With("operation", "delete_character").With("character_id", id).Wrap(err)
        }
        return nil
    })
}
```

**Step 3: Run tests, commit**

```bash
task test
git add internal/world/service.go internal/world/service_test.go
git commit -m "feat(world): add DeleteCharacter method to WorldService"
```

---

### Task 4c: Property cascade deletion and lifecycle

> **Note:** This task integrates the PropertyRepository (from Task 4a) with WorldService deletion methods (from Task 4b) to ensure properties are cleaned up when parent entities are deleted.

> **Implementation Note:** This task modifies all three deletion methods (`DeleteCharacter`, `DeleteObject`, `DeleteLocation`) to add property cascade deletion calls via `PropertyRepository.DeleteByParent()`.

> **Scope:** This task adds property cascade deletion to existing deletion methods, adds an orphan cleanup goroutine, and implements startup integrity checks.

**Spec References:** Entity Properties — lifecycle on parent deletion (lines 2070-2113)

**Acceptance Criteria:**

- [ ] Property lifecycle on parent deletion: cascade delete in same transaction as parent entity deletion
- [ ] `WorldService.DeleteCharacter()` → `PropertyRepository.DeleteByParent("character", charID)` in same transaction (called before character deletion)
- [ ] `WorldService.DeleteObject()` → `PropertyRepository.DeleteByParent("object", objID)` in same transaction (called before object deletion)
- [ ] `WorldService.DeleteLocation()` → `PropertyRepository.DeleteByParent("location", locID)` in same transaction (called before location deletion)
- [ ] Orphan cleanup goroutine: runs on configurable timer (default: daily) to detect orphaned properties (parent entity no longer exists)
- [ ] Orphan cleanup: detected orphans logged at WARN level on first discovery
- [ ] Orphan cleanup: configurable grace period (default: 24h, configured via `world.orphan_grace_period` in server YAML)
- [ ] Orphan cleanup: orphans persisting across two consecutive runs are actively deleted with batch `DELETE` and logged at INFO level with count
- [ ] Startup integrity check: count orphaned properties on server startup
- [ ] Startup integrity check: if orphan count exceeds configurable threshold (default: 100), log at ERROR level but continue starting (not fail-fast)
- [ ] All tests pass via `task test`

**Files:**

- Modify: `internal/world/service.go` (add property cascade deletion to DeleteCharacter, DeleteObject, DeleteLocation)
- Create: `internal/world/property_lifecycle.go` (orphan cleanup goroutine, startup integrity check)
- Test: `internal/world/service_test.go` (cascade deletion tests)
- Test: `internal/world/property_lifecycle_test.go` (orphan cleanup tests)

**Step 1: Write failing tests (Task 4c)**

- `WorldService.DeleteCharacter()` deletes all properties for that character
- `WorldService.DeleteObject()` deletes all properties for that object
- `WorldService.DeleteLocation()` deletes all properties for that location
- Cascade deletion happens in same transaction (rollback on error)
- Orphan cleanup goroutine identifies and deletes orphaned properties
- Startup integrity check logs orphan count at WARN level

**Step 2: Add property cascade deletion**

Modify `internal/world/service.go`:

```go
func (s *WorldService) DeleteCharacter(ctx context.Context, id string) error {
    return s.tx.WithTransaction(ctx, func(ctx context.Context) error {
        // Delete properties first
        if err := s.propertyRepo.DeleteByParent(ctx, "character", id); err != nil {
            return oops.With("operation", "delete_character_properties").Wrap(err)
        }
        // Then delete character
        if err := s.characterRepo.Delete(ctx, id); err != nil {
            return oops.With("operation", "delete_character").With("character_id", id).Wrap(err)
        }
        return nil
    })
}
```

Add similar property cascade deletion logic to existing `DeleteObject()` and `DeleteLocation()` methods.

**Step 3: Implement orphan cleanup**

Create `internal/world/property_lifecycle.go`:

```go
// StartPropertyLifecycleManager starts background goroutine for orphan cleanup
func (s *WorldService) StartPropertyLifecycleManager(ctx context.Context, interval time.Duration) {
    go s.cleanupOrphansLoop(ctx, interval)
}

func (s *WorldService) cleanupOrphansLoop(ctx context.Context, interval time.Duration) {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            if err := s.cleanupOrphanedProperties(ctx); err != nil {
                errutil.LogError(s.logger, "orphan cleanup failed", err)
            }
        }
    }
}

func (s *WorldService) cleanupOrphanedProperties(ctx context.Context) error {
    // Query for properties where parent entity no longer exists
    // Delete orphaned properties
    // Log count at WARN level if orphans found
}

func (s *WorldService) StartupIntegrityCheck(ctx context.Context) error {
    orphanCount, err := s.countOrphanedProperties(ctx)
    if err != nil {
        return err
    }
    if orphanCount > 0 {
        s.logger.Warn("orphaned properties detected", "count", orphanCount)
    }
    return nil
}
```

**Step 4: Run tests, commit**

```bash
task test
git add internal/world/service.go internal/world/property_lifecycle.go
git add internal/world/service_test.go internal/world/property_lifecycle_test.go
git commit -m "feat(world): add property cascade deletion and orphan cleanup"
```

---

### Task 5: Define core types (AccessRequest, Decision, Effect, PolicyMatch, AttributeBags)

**Spec References:** Core Interfaces (lines 195-512) — AccessRequest, Decision, Effect, PolicyMatch, AttributeBags

**Acceptance Criteria:**

- [ ] `Effect` enum has exactly 4 values: `DefaultDeny`, `Allow`, `Deny`, `SystemBypass`
- [ ] `Effect.String()` returns spec-mandated strings: `default_deny`, `allow`, `deny`, `system_bypass`
- [ ] `PolicyEffect` type defined with `PolicyEffectPermit`/`PolicyEffectForbid` constants
- [ ] `NewDecision()` enforces Allowed invariant: `Allow` and `SystemBypass` → true, all others → false
- [ ] `Decision.allowed` field is unexported to prevent invariant bypass (security: prevents `Decision{Allowed: true, Effect: EffectDeny}`)
- [ ] `Decision.IsAllowed()` accessor method returns the authorization result
- [ ] `Decision.Validate()` method checks invariant at engine return boundary
- [ ] `AccessRequest` has `Subject`, `Action`, `Resource` string fields
- [ ] `Decision` includes `allowed` (unexported), `Effect`, `Reason`, `PolicyID`, `Policies`, `Attributes`
- [ ] `AttributeBags` has `Subject`, `Resource`, `Action`, `Environment` maps
- [ ] `AttributeSchema` type defined for use by compiler and resolver
- [ ] `AttrType` enum defined: `String`, `Int`, `Float`, `Bool`, `StringList`
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/types/types.go`
- Test: `internal/access/policy/types/types_test.go`

**Step 1: Write failing tests for Effect.String() and Decision invariants**

```go
// internal/access/policy/types/types_test.go
package types

import (
    "testing"

    "github.com/stretchr/testify/assert"
)

func TestEffect_String(t *testing.T) {
    tests := []struct {
        name     string
        effect   Effect
        expected string
    }{
        {"default deny", EffectDefaultDeny, "default_deny"},
        {"allow", EffectAllow, "allow"},
        {"deny", EffectDeny, "deny"},
        {"system bypass", EffectSystemBypass, "system_bypass"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            assert.Equal(t, tt.expected, tt.effect.String())
        })
    }
}

func TestDecision_Invariant(t *testing.T) {
    tests := []struct {
        name    string
        effect  Effect
        allowed bool
    }{
        {"allow is allowed", EffectAllow, true},
        {"deny is not allowed", EffectDeny, false},
        {"default deny is not allowed", EffectDefaultDeny, false},
        {"system bypass is allowed", EffectSystemBypass, true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            d := NewDecision(tt.effect, "", "")
            assert.Equal(t, tt.allowed, d.IsAllowed())
        })
    }
}
```

**Step 2: Run tests to verify they fail**

Run: `task test`
Expected: FAIL — package and types don't exist

**Step 3: Implement types**

```go
// internal/access/policy/types/types.go
package types

// Effect represents the type of decision.
type Effect int

const (
    EffectDefaultDeny  Effect = iota // No policy matched
    EffectAllow                      // Permit policy satisfied
    EffectDeny                       // Forbid policy satisfied
    EffectSystemBypass               // System subject bypass
)

func (e Effect) String() string {
    switch e {
    case EffectDefaultDeny:
        return "default_deny"
    case EffectAllow:
        return "allow"
    case EffectDeny:
        return "deny"
    case EffectSystemBypass:
        return "system_bypass"
    default:
        return "unknown"
    }
}

// AccessRequest contains all information needed for an access decision.
type AccessRequest struct {
    Subject  string // "character:01ABC", "plugin:echo-bot", "system"
    Action   string // "read", "write", "delete", "enter", "execute", "emit"
    Resource string // "location:01XYZ", "command:dig", "property:01DEF"
}

// Decision represents the outcome of a policy evaluation.
type Decision struct {
    Allowed    bool
    Effect     Effect
    Reason     string
    PolicyID   string
    Policies   []PolicyMatch
    Attributes *AttributeBags
}

// NewDecision creates a Decision with the Allowed invariant enforced.
func NewDecision(effect Effect, reason, policyID string) Decision {
    return Decision{
        allowed:  effect == EffectAllow || effect == EffectSystemBypass,
        Effect:   effect,
        PolicyID: policyID,
        Reason:   reason,
    }
}

// PolicyMatch records a single policy's evaluation result.
type PolicyMatch struct {
    PolicyID      string
    PolicyName    string
    Effect        Effect
    ConditionsMet bool
}

// AttributeBags holds the resolved attributes for a request.
type AttributeBags struct {
    Subject     map[string]any
    Resource    map[string]any
    Action      map[string]any
    Environment map[string]any
}

// AttrType identifies the type of an attribute value.
type AttrType int

const (
    AttrTypeString AttrType = iota
    AttrTypeInt
    AttrTypeFloat
    AttrTypeBool
    AttrTypeStringList
)

// PolicyEffect represents the declared intent of a policy (permit or forbid).
// This is distinct from Effect, which represents the engine's evaluation decision.
type PolicyEffect string

const (
    PolicyEffectPermit PolicyEffect = "permit" // Policy grants access
    PolicyEffectForbid PolicyEffect = "forbid" // Policy denies access
)

// ToEffect converts a PolicyEffect to an Effect for evaluation.
// Permit → EffectAllow, Forbid → EffectDeny.
func (pe PolicyEffect) ToEffect() Effect {
    switch pe {
    case PolicyEffectPermit:
        return EffectAllow
    case PolicyEffectForbid:
        return EffectDeny
    default:
        return EffectDefaultDeny
    }
}

// AttributeSchema registry for validating attribute types.
// Used by PolicyCompiler (Task 12) and AttributeResolver (Task 14).
// Note: This is a minimal stub. Task 6 will add namespaces field and methods.
// Task 13 will add full implementation.
type AttributeSchema struct {
    // Fields added in Task 6
}
```

> **Implementation Note (Bug TD4):** Use typed string constants for enum-like string fields to add compile-time safety at zero cost. Specifically:
> - `StoredPolicy.Source` → define `type PolicySource string` with constants `PolicySourceSeed`, `PolicySourceLock`, `PolicySourceAdmin`, `PolicySourcePlugin`
> - `EntityProperty.Visibility` → define `type PropertyVisibility string` with constants `PropertyVisibilityPublic`, `PropertyVisibilityPrivate`, `PropertyVisibilityRestricted`, `PropertyVisibilitySystem`, `PropertyVisibilityAdmin`
> - `EntityProperty.ParentType` → define `type EntityType string` with constants `EntityTypeCharacter`, `EntityTypeLocation`, `EntityTypeObject`
>
> This prevents typos like `"publc"` or `"seed "` (trailing space) from compiling, while maintaining string serialization compatibility with JSON/database fields.

**Step 4: Run tests to verify they pass**

Run: `task test`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/access/policy/
git commit -m "feat(access): add core ABAC types (AccessRequest, Decision, Effect, AttributeBags)"
```

---

### Task 6: Define subject/resource prefix constants and parser

**Spec References:** Core Interfaces > AccessRequest (lines 283-325), Session Subject Resolution (lines 326-392)

**Acceptance Criteria:**

- [ ] Subject prefixes defined: `character:`, `plugin:`, `system`, `session:`
- [ ] Resource prefixes defined: `location:`, `object:`, `command:`, `property:`, `stream:`
- [ ] `ParseEntityRef()` correctly parses all prefix types (table-driven tests)
- [ ] `system` parses to type `"system"` with empty ID
- [ ] `stream:location:01XYZ` parses to type `"stream"`, ID `"location:01XYZ"`
- [ ] Unknown prefix returns `INVALID_ENTITY_REF` error code (oops)
- [ ] Empty string returns `INVALID_ENTITY_REF` error code
- [ ] Legacy `char:01ABC` prefix returns `INVALID_ENTITY_REF` error code
- [ ] Session error code constants defined: `infra:session-invalid`, `infra:session-store-error`
- [ ] `access.WithSystemSubject(ctx)` stores system subject marker in context
- [ ] `access.IsSystemContext(ctx)` retrieves system subject marker from context
- [ ] System context helpers tested with table-driven tests
- [ ] All tests pass via `task test`

**Files:**

- Extend: `internal/access/policy/types/types.go` (add NamespaceSchema and extend AttributeSchema implementation)
- Create: `internal/access/policy/prefix.go`
- Test: `internal/access/policy/prefix_test.go`
- Create: `internal/access/context.go` (system context helpers)
- Test: `internal/access/context_test.go`

**Dependencies:** Requires Task 5 completion (core types must exist before extending)

**Step 1: Extend shared types (NamespaceSchema, AttributeSchema methods)**

> **Design note:** Task 5 created the base `AttributeSchema` and `AttrType` types. This task extends those types with `NamespaceSchema` and adds stub methods to `AttributeSchema` for use by the policy compiler (Task 12 ([Phase 7.2](./2026-02-06-full-abac-phase-7.2.md))) and attribute resolver (Task 14 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md))).

```go
// internal/access/policy/types/types.go
// ADD to existing file created in Task 5

// NamespaceSchema defines the attributes in a namespace.
type NamespaceSchema struct {
    Attributes map[string]AttrType
}

// ADD to existing AttributeSchema type:
func NewAttributeSchema() *AttributeSchema {
    return &AttributeSchema{
        namespaces: make(map[string]*NamespaceSchema),
    }
}

func (s *AttributeSchema) Register(namespace string, schema *NamespaceSchema) error {
    // Implementation in Task 12
    return nil
}

func (s *AttributeSchema) IsRegistered(namespace, key string) bool {
    // Implementation in Task 12
    return false
}
```

**Step 2: Write failing tests for prefix parsing**

```go
// internal/access/policy/prefix_test.go
package policy_test

import (
    "testing"

    "github.com/holomush/holomush/internal/access/policy"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestParseEntityRef(t *testing.T) {
    tests := []struct {
        name       string
        input      string
        wantType   string
        wantID     string
        wantErr    bool
        wantErrMsg string
    }{
        {"character", "character:01ABC", "character", "01ABC", false, ""},
        {"plugin", "plugin:echo-bot", "plugin", "echo-bot", false, ""},
        {"system", "system", "system", "", false, ""},
        {"session", "session:web-123", "session", "web-123", false, ""},
        {"location", "location:01XYZ", "location", "01XYZ", false, ""},
        {"object", "object:01DEF", "object", "01DEF", false, ""},
        {"command", "command:say", "command", "say", false, ""},
        {"property", "property:01GHI", "property", "01GHI", false, ""},
        {"stream", "stream:location:01XYZ", "stream", "location:01XYZ", false, ""},
        {"unknown prefix", "bogus:123", "", "", true, "unknown entity prefix"},
        {"empty string", "", "", "", true, "empty entity reference"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            typ, id, err := policy.ParseEntityRef(tt.input)
            if tt.wantErr {
                require.Error(t, err)
                assert.Contains(t, err.Error(), tt.wantErrMsg)
                return
            }
            require.NoError(t, err)
            assert.Equal(t, tt.wantType, typ)
            assert.Equal(t, tt.wantID, id)
        })
    }
}
```

**Step 3: Run tests to verify they fail**

Run: `task test`
Expected: FAIL

**Step 4: Implement prefix parsing**

```go
// internal/access/policy/prefix.go
package policy

import (
    "strings"

    "github.com/samber/oops"
)

// Subject prefix constants.
const (
    SubjectCharacter = "character:"
    SubjectPlugin    = "plugin:"
    SubjectSystem    = "system"
    SubjectSession   = "session:"
)

// Resource prefix constants.
const (
    ResourceLocation = "location:"
    ResourceObject   = "object:"
    ResourceCommand  = "command:"
    ResourceProperty = "property:"
    ResourceStream   = "stream:"
)

// Error code constants for session resolution.
const (
    ErrCodeSessionInvalid    = "infra:session-invalid"
    ErrCodeSessionStoreError = "infra:session-store-error"
)

// knownPrefixes maps prefixes to their type names.
// Order matters: "system" exact match first, then "stream:" before "location:".
var knownPrefixes = []struct {
    prefix   string
    typeName string
}{
    {SubjectSystem, "system"},
    {ResourceStream, "stream"},
    {SubjectCharacter, "character"},
    {SubjectPlugin, "plugin"},
    {SubjectSession, "session"},
    {ResourceLocation, "location"},
    {ResourceObject, "object"},
    {ResourceCommand, "command"},
    {ResourceProperty, "property"},
}

// ParseEntityRef parses a prefixed entity string into type and ID.
// "system" has no ID. "stream:location:01XYZ" has ID "location:01XYZ".
func ParseEntityRef(ref string) (typeName, id string, err error) {
    if ref == "" {
        return "", "", oops.Code("INVALID_ENTITY_REF").Errorf("empty entity reference")
    }
    if ref == SubjectSystem {
        return "system", "", nil
    }
    for _, p := range knownPrefixes {
        if p.prefix == SubjectSystem {
            continue
        }
        if strings.HasPrefix(ref, p.prefix) {
            return p.typeName, ref[len(p.prefix):], nil
        }
    }
    return "", "", oops.Code("INVALID_ENTITY_REF").
        With("ref", ref).
        Errorf("unknown entity prefix: %q", ref)
}
```

**Step 5: Add system context helpers**

These helpers allow bootstrap and system operations to bypass ABAC by marking context as system-level.

```go
// internal/access/context.go
package access

import "context"

// systemSubjectKey is the context key for system subject marker.
type systemSubjectKey struct{}

// WithSystemSubject returns a new context marked as system-level operation.
// Operations with system context bypass ABAC policy evaluation.
// Used during bootstrap, migrations, and internal system tasks.
func WithSystemSubject(ctx context.Context) context.Context {
    return context.WithValue(ctx, systemSubjectKey{}, true)
}

// IsSystemContext returns true if the context is marked as system-level.
// PolicyStore and other ABAC components use this to bypass policy checks.
func IsSystemContext(ctx context.Context) bool {
    v, ok := ctx.Value(systemSubjectKey{}).(bool)
    return ok && v
}
```

**Step 6: Write tests for system context helpers**

```go
// internal/access/context_test.go
package access_test

import (
    "context"
    "testing"

    "github.com/holomush/holomush/internal/access"
    "github.com/stretchr/testify/assert"
)

func TestSystemContext(t *testing.T) {
    tests := []struct {
        name     string
        ctx      context.Context
        expected bool
    }{
        {
            name:     "regular context returns false",
            ctx:      context.Background(),
            expected: false,
        },
        {
            name:     "system context returns true",
            ctx:      access.WithSystemSubject(context.Background()),
            expected: true,
        },
        {
            name:     "nested system context returns true",
            ctx:      access.WithSystemSubject(access.WithSystemSubject(context.Background())),
            expected: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := access.IsSystemContext(tt.ctx)
            assert.Equal(t, tt.expected, got)
        })
    }
}
```

**Step 7: Run tests to verify they pass**

Run: `task test`
Expected: PASS

**Step 8: Commit**

```bash
git add internal/access/policy/types/ internal/access/policy/prefix.go internal/access/policy/prefix_test.go internal/access/context.go internal/access/context_test.go
git commit -m "feat(access): extend types package, add prefix parser and system context helpers

- Extend AttributeSchema with NamespaceSchema and stub methods
- Add subject/resource prefix constants and parser
- Add WithSystemSubject()/IsSystemContext() for bootstrap operations"
```

---

### Task 7: Policy store interface and PostgreSQL implementation

**Spec References:** Policy Storage > Schema (lines 1973-2114), Policy Storage > Cache Invalidation (lines 2115-2159)

**Acceptance Criteria:**

- [ ] `PolicyStore` interface defines: `Create`, `Get`, `GetByID`, `Update`, `Delete`, `ListEnabled`, `List`
- [ ] `StoredPolicy` struct includes all `access_policies` table columns
- [ ] `StoredPolicy.ID` uses `string` for ID because policy identifiers may be UUIDs generated by PostgreSQL or other string formats. This differs from the world model's ulid.ULID convention because policies are not world entities.
- [ ] `StoredPolicy` includes CreatedAt and UpdatedAt fields populated from DB
- [ ] `PolicyEffect` type defined with `PolicyEffectPermit`/`PolicyEffectForbid` constants
- [ ] `StoredPolicy.Effect` uses `PolicyEffect` (not `policy.Effect`)
- [ ] `PolicyEffect.String()` serializes to DB TEXT values ("permit"/"forbid")
- [ ] Documentation clearly distinguishes `PolicyEffect` (what a policy declares) from `policy.Effect` (what the engine decides)
- [ ] `Create()` generates ULID, inserts row, and calls `pg_notify('policy_changed', policyID)`
- [ ] `Update()` increments version, inserts into `access_policy_versions`, calls `pg_notify`
- [ ] `Delete()` removes row (CASCADE), calls `pg_notify`
- [ ] `ListEnabled()` returns only `enabled = true` rows
- [ ] `ListOptions` supports filtering by `Source` and `Enabled`
- [ ] Constructor accepts `*pgxpool.Pool`; errors use `oops` with context
- [ ] Integration tests (with `//go:build integration`) cover all CRUD operations
- [ ] All tests pass via `task test`

**Design Note:** `Create()` and `Update()` accept pre-compiled AST bytes in `StoredPolicy.CompiledAST`. Compilation happens in the caller (typically the engine or a higher-level service that uses `PolicyCompiler` from Task 12 ([Phase 7.2](./2026-02-06-full-abac-phase-7.2.md))). This approach avoids a circular dependency between Task 7 (PolicyStore) and Task 12 ([Phase 7.2](./2026-02-06-full-abac-phase-7.2.md)) (PolicyCompiler), and differs from the spec wording which suggests the store calls `Compile()` internally.

**Files:**

- Create: `internal/access/policy/store/store.go` (interface)
- Create: `internal/access/policy/store/postgres.go` (implementation)
- Test: `internal/access/policy/store/postgres_test.go`

**Step 1: Write the store interface**

```go
// internal/access/policy/store/store.go
package store

import (
    "context"
    "time"

    "github.com/holomush/holomush/internal/access/policy/types"
)

// StoredPolicy is the persisted form of a policy.
type StoredPolicy struct {
    ID          string
    Name        string
    Description string
    Effect      types.PolicyEffect
    Source      string // "seed", "lock", "admin", "plugin"
    DSLText     string
    CompiledAST []byte // JSONB
    Enabled     bool
    SeedVersion *int
    ChangeNote  string // populated on version upgrades; stored in access_policy_versions
    CreatedBy   string
    Version     int
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

// PolicyStore handles CRUD operations for access policies.
type PolicyStore interface {
    Create(ctx context.Context, p *StoredPolicy) error
    Get(ctx context.Context, name string) (*StoredPolicy, error)
    GetByID(ctx context.Context, id string) (*StoredPolicy, error)
    Update(ctx context.Context, p *StoredPolicy) error
    Delete(ctx context.Context, name string) error
    ListEnabled(ctx context.Context) ([]*StoredPolicy, error)
    List(ctx context.Context, opts ListOptions) ([]*StoredPolicy, error)
}

// ListOptions controls filtering for policy listing.
type ListOptions struct {
    Source  string // filter by source ("seed", "lock", "admin", "plugin", or "" for all)
    Enabled *bool  // filter by enabled state (nil for all)
}
```

**Step 2: Write failing tests for PostgreSQL store**

Write table-driven tests covering:

- Create a policy, verify it round-trips
- Get by name, get by ID
- Update a policy (version increments, version history created)
- Delete a policy
- ListEnabled returns only enabled policies
- NOTIFY is sent on create/update/delete

Test file: `internal/access/policy/store/postgres_test.go`

Use `//go:build integration` tag and testcontainers pattern from existing integration tests in `test/integration/world/world_suite_test.go`.

**Step 3: Implement PostgreSQL store**

Key behaviors:

- `Create()`: generates ULID, inserts row, calls `pg_notify('policy_changed', policyID)`
- `Update()`: increments version, inserts into `access_policy_versions`, calls `pg_notify`
- `Delete()`: deletes row (CASCADE removes versions), calls `pg_notify`
- `ListEnabled()`: returns all rows where `enabled = true`

Follow existing repository patterns from `internal/world/postgres/location_repo.go`:

- Accept `*pgxpool.Pool` in constructor
- Use `oops` for error wrapping with context
- Use helper functions for ULID conversion

**Step 4: Run tests**

Run: `task test`
Expected: Unit tests PASS (integration tests require DB)

**Step 5: Commit**

```bash
git add internal/access/policy/store/
git commit -m "feat(access): add PolicyStore interface and PostgreSQL implementation"
```

---


---

> **[Back to Overview](./2026-02-06-full-abac-implementation.md)** | **[Next: Phase 7.2](./2026-02-06-full-abac-phase-7.2.md)**
