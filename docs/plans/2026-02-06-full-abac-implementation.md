# Full ABAC Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the static role-based `AccessControl` system with a full policy-driven `AccessPolicyEngine` supporting a Cedar-inspired DSL, extensible attribute providers, audit logging, and admin commands.

**Architecture:** Custom Go-native ABAC engine with eager attribute resolution, in-memory policy cache invalidated via PostgreSQL LISTEN/NOTIFY, deny-overrides conflict resolution, and per-request attribute caching. No adapter layer — direct replacement of all ~28 production call sites (plus test files and generated mocks).

**Tech Stack:** Go 1.23+, [participle](https://github.com/alecthomas/participle) (struct-tag parser generator), pgx/pgxpool, oops (structured errors), prometheus/client_golang, testify + Ginkgo/Gomega, mockery

---

## Task Execution Protocol

Every task in this plan MUST follow these requirements:

### TDD (Test-Driven Development)

| Step | Description                                                         |
| ---- | ------------------------------------------------------------------- |
| 1    | Write failing test(s) first — tests MUST fail before implementation |
| 2    | Verify the test fails (run `task test`)                             |
| 3    | Write minimal implementation to make the test pass                  |
| 4    | Verify the test passes (run `task test`)                            |
| 5    | Refactor if needed (tests still pass)                               |
| 6    | Commit                                                              |

SQL migration tasks (Tasks 1-3) are exempt from red-green-refactor but MUST have integration test coverage before the phase is considered complete.

### Spec & ADR Traceability

Each task MUST denote which spec sections and ADRs it implements. This is tracked via the **Spec References** field on each task. The implementer MUST verify their work aligns with the referenced spec sections before requesting review.

**Design spec:** `docs/specs/2026-02-05-full-abac-design.md`

Applicable ADRs (from spec §18):

| ADR      | Title                              | Applies To      |
| -------- | ---------------------------------- | --------------- |
| ADR 0011 | Deny-overrides conflict resolution | Tasks 16, 31    |
| ADR 0012 | Eager attribute resolution         | Tasks 13, 16    |
| ADR 0013 | Properties as first-class entities | Tasks 3, 15, 25 |
| ADR 0014 | Direct replacement (no adapter)    | Tasks 28-30     |
| ADR 0015 | Three-Layer Player Access Control  | Tasks 15, 25    |
| ADR 0016 | LISTEN/NOTIFY cache invalidation   | Task 17         |

### Acceptance Criteria

Every task includes an **Acceptance Criteria** section with specific, verifiable conditions. A task is NOT complete until ALL acceptance criteria are met.

### Review Gate

Every task MUST pass review before being marked complete:

1. **Code review** — Run `pr-review-toolkit:review-pr` or equivalent specialized reviewer
2. **Spec alignment review** — Verify implementation matches referenced spec sections
3. **ADR compliance** — If task references an ADR, verify the decision is correctly implemented
4. **All findings addressed** — Fix issues or document why not applicable

A task is complete ONLY when: tests pass, acceptance criteria met, AND review passed.

---

## Phase 7.1: Policy Schema (Database Tables + Policy Store)

> **Note:** Migration numbers in this phase (000015, 000016, 000017) are relative to the current latest migration `000014_aliases`. If other migrations merge before this work, these numbers MUST be updated to avoid collisions.

### Task 1: Create access\_policies migration

**Spec References:** §8.1 (Policy Storage Schema, lines 1971-2050)

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

**Spec References:** §8.1 (Policy Storage Schema, lines 1971-2050), §8.2 (Audit Log Schema)

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
    -- Spec §8.2 line 2015 defines "id TEXT PRIMARY KEY" which is technically
    -- incorrect for partitioned tables. This needs to be corrected in the spec.
    PRIMARY KEY (id, timestamp)
) PARTITION BY RANGE (timestamp);

-- Create initial partitions (current month + 2 future months, per spec §8.2 line 2306)
CREATE TABLE access_audit_log_2026_02 PARTITION OF access_audit_log
    FOR VALUES FROM ('2026-02-01') TO ('2026-03-01');

CREATE TABLE access_audit_log_2026_03 PARTITION OF access_audit_log
    FOR VALUES FROM ('2026-03-01') TO ('2026-04-01');

CREATE TABLE access_audit_log_2026_04 PARTITION OF access_audit_log
    FOR VALUES FROM ('2026-04-01') TO ('2026-05-01');

CREATE INDEX idx_audit_log_timestamp ON access_audit_log USING BRIN (timestamp)
    WITH (pages_per_range = 128);
CREATE INDEX idx_audit_log_subject ON access_audit_log(subject, timestamp DESC);
CREATE INDEX idx_audit_log_denied ON access_audit_log(effect, timestamp DESC)
    WHERE effect IN ('deny', 'default_deny');
```

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

**NOTE:** The spec (§8.2 line 2015) defines `id TEXT PRIMARY KEY`, but PostgreSQL partitioned tables require the partition key (`timestamp`) to be included in the primary key. The implementation correctly uses `PRIMARY KEY (id, timestamp)`. **Action required:** Update spec to reflect this PostgreSQL constraint.

---

### Task 3: Create entity\_properties migration

**Spec References:** §8.1 (Policy Storage Schema), ADR 0013 (Properties as first-class entities)

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

### Task 4: Define core types (AccessRequest, Decision, Effect, PolicyMatch, AttributeBags)

**Spec References:** §3 (Core Interfaces, lines 195-335) — AccessRequest, Decision, Effect, PolicyMatch, AttributeBags

**Acceptance Criteria:**

- [ ] `Effect` enum has exactly 4 values: `DefaultDeny`, `Allow`, `Deny`, `SystemBypass`
- [ ] `Effect.String()` returns spec-mandated strings: `default_deny`, `allow`, `deny`, `system_bypass`
- [ ] `NewDecision()` enforces Allowed invariant: `Allow` and `SystemBypass` → true, all others → false
- [ ] `AccessRequest` has `Subject`, `Action`, `Resource` string fields
- [ ] `Decision` includes `Allowed`, `Effect`, `Reason`, `PolicyID`, `Policies`, `Attributes`
- [ ] `AttributeBags` has `Subject`, `Resource`, `Action`, `Environment` maps
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/types.go`
- Test: `internal/access/policy/types_test.go`

**Step 1: Write failing tests for Effect.String() and Decision invariants**

```go
// internal/access/policy/types_test.go
package policy_test

import (
    "testing"

    "github.com/holomush/holomush/internal/access/policy"
    "github.com/stretchr/testify/assert"
)

func TestEffect_String(t *testing.T) {
    tests := []struct {
        name     string
        effect   policy.Effect
        expected string
    }{
        {"default deny", policy.EffectDefaultDeny, "default_deny"},
        {"allow", policy.EffectAllow, "allow"},
        {"deny", policy.EffectDeny, "deny"},
        {"system bypass", policy.EffectSystemBypass, "system_bypass"},
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
        effect  policy.Effect
        allowed bool
    }{
        {"allow is allowed", policy.EffectAllow, true},
        {"deny is not allowed", policy.EffectDeny, false},
        {"default deny is not allowed", policy.EffectDefaultDeny, false},
        {"system bypass is allowed", policy.EffectSystemBypass, true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            d := policy.NewDecision(tt.effect, "", "")
            assert.Equal(t, tt.allowed, d.Allowed)
        })
    }
}
```

**Step 2: Run tests to verify they fail**

Run: `task test`
Expected: FAIL — package and types don't exist

**Step 3: Implement types**

```go
// internal/access/policy/types.go
package policy

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
func NewDecision(effect Effect, policyID, reason string) Decision {
    return Decision{
        Allowed:  effect == EffectAllow || effect == EffectSystemBypass,
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
```

**Step 4: Run tests to verify they pass**

Run: `task test`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/access/policy/
git commit -m "feat(access): add core ABAC types (AccessRequest, Decision, Effect, AttributeBags)"
```

---

### Task 5: Define subject/resource prefix constants and parser

**Spec References:** §3.3 (Subject/Resource Prefix Format, lines 335-392)

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
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/prefix.go`
- Test: `internal/access/policy/prefix_test.go`

**Step 1: Write failing tests for prefix parsing**

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

**Step 2: Run tests to verify they fail**

Run: `task test`
Expected: FAIL

**Step 3: Implement prefix parsing**

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

**Step 4: Run tests to verify they pass**

Run: `task test`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/access/policy/prefix.go internal/access/policy/prefix_test.go
git commit -m "feat(access): add subject/resource prefix constants and parser"
```

---

### Task 6: Policy store interface and PostgreSQL implementation

**Spec References:** §8.1 (Policy Storage Schema, lines 1971-2050), §6.5 (LISTEN/NOTIFY, lines 1327-1345)

**Acceptance Criteria:**

- [ ] `PolicyStore` interface defines: `Create`, `Get`, `GetByID`, `Update`, `Delete`, `ListEnabled`, `List`
- [ ] `StoredPolicy` struct includes all `access_policies` table columns
- [ ] `Create()` generates ULID, inserts row, and calls `pg_notify('policy_changed', name)`
- [ ] `Update()` increments version, inserts into `access_policy_versions`, calls `pg_notify`
- [ ] `Delete()` removes row (CASCADE), calls `pg_notify`
- [ ] `ListEnabled()` returns only `enabled = true` rows
- [ ] `ListOptions` supports filtering by `Source` and `Enabled`
- [ ] Constructor accepts `*pgxpool.Pool`; errors use `oops` with context
- [ ] Integration tests (with `//go:build integration`) cover all CRUD operations
- [ ] All tests pass via `task test`

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

    "github.com/holomush/holomush/internal/access/policy"
)

// StoredPolicy is the persisted form of a policy.
type StoredPolicy struct {
    ID          string
    Name        string
    Description string
    Effect      policy.Effect
    Source      string // "seed", "lock", "admin", "plugin"
    DSLText     string
    CompiledAST []byte // JSONB
    Enabled     bool
    SeedVersion *int
    CreatedBy   string
    Version     int
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

- `Create()`: generates ULID, inserts row, calls `pg_notify('policy_changed', name)`
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

## Phase 7.2: DSL & Compiler

### Task 7: Define AST node types

**Spec References:** §4 (DSL Grammar, EBNF lines 735-810), §4.1 (Reserved Words)

**Acceptance Criteria:**

- [ ] AST nodes defined for: `Policy`, `Target`, `PrincipalClause`, `ActionClause`, `ResourceClause`, `ConditionBlock`, `Disjunction`, `Conjunction`, `Condition`, `Expr`, `AttrRef`, `Literal`, `ListExpr`
- [ ] All nodes use participle struct tag annotations matching the EBNF grammar
- [ ] `String()` methods render AST back to readable DSL text
- [ ] Reserved words enforced: `permit`, `forbid`, `when`, `principal`, `resource`, `action`, `env`, `is`, `in`, `has`, `like`, `true`, `false`, `if`, `then`, `else`, `containsAll`, `containsAny`
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/dsl/ast.go`
- Test: `internal/access/policy/dsl/ast_test.go`

**Step 1: Write tests for AST node String() methods**

Test that AST nodes render back to readable DSL text (useful for debugging and `policy show`).

**Step 2: Implement AST types using participle struct tags**

Map the EBNF grammar from the spec (lines 739-810) to participle annotations. Key AST nodes:

- `Policy` — top-level: effect + target + optional conditions + semicolon
- `Target` — principal clause + action clause + resource clause
- `PrincipalClause` — `"principal"` optional `"is" type_name`
- `ActionClause` — `"action"` optional `"in" list`
- `ResourceClause` — `"resource"` optional `"is" type_name` or `"==" string_literal`
- `ConditionBlock` — disjunction (top-level `||` chain)
- `Disjunction` — conjunction chain with `||`
- `Conjunction` — condition chain with `&&`
- `Condition` — comparison, `like`, `in`, `has`, `containsAll`/`containsAny`, negation, parenthesized, `if-then-else`, bare boolean literal
- `Expr` — attribute reference or literal
- `AttrRef` — root (`principal`/`resource`/`action`/`env`) + dotted path
- `Literal` — string, number, or boolean
- `ListExpr` — `[` comma-separated literals `]`

Enforce reserved word restrictions: `permit`, `forbid`, `when`, `principal`, `resource`, `action`, `env`, `is`, `in`, `has`, `like`, `true`, `false`, `if`, `then`, `else`, `containsAll`, `containsAny` MUST NOT appear as attribute names.

**Step 3: Commit**

```bash
git add internal/access/policy/dsl/
git commit -m "feat(access): add DSL AST node types with participle annotations"
```

---

### Task 8: Build DSL parser

**Spec References:** §4 (DSL Grammar, EBNF lines 735-810), §4.2 (Operator Semantics), §12.1 (Seed Policy DSL text, lines 2935-2999)

**Acceptance Criteria:**

- [ ] All 14 seed policy DSL strings parse successfully
- [ ] All operators parse correctly: `==`, `!=`, `>`, `>=`, `<`, `<=`, `in`, `like`, `has`, `containsAll`, `containsAny`, `!`, `&&`, `||`, `if-then-else`
- [ ] `resource == "location:01XYZ"` (exact match) parses correctly
- [ ] Missing semicolon → descriptive error with position info
- [ ] Unknown effect → descriptive error
- [ ] Reserved word as attribute name → error
- [ ] Nesting depth >32 → error
- [ ] Table-driven tests cover both valid and invalid policies
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/dsl/parser.go`
- Test: `internal/access/policy/dsl/parser_test.go`

**Step 1: Write failing parser tests**

Table-driven tests MUST cover:

**Valid policies (14 seed policies plus catch-all forbid test case):**

```text
permit(principal is character, action in ["read", "write"], resource is character) when { resource.id == principal.id };
permit(principal is character, action in ["read"], resource is location) when { resource.id == principal.location };
permit(principal is character, action in ["read"], resource is character) when { resource.location == principal.location };
permit(principal is character, action in ["read"], resource is object) when { resource.location == principal.location };
permit(principal is character, action in ["emit"], resource is stream) when { resource.name like "location:*" && resource.location == principal.location };
permit(principal is character, action in ["enter"], resource is location);
permit(principal is character, action in ["execute"], resource is command) when { resource.name in ["say", "pose", "look", "go"] };
permit(principal is character, action in ["write", "delete"], resource is location) when { principal.role in ["builder", "admin"] };
permit(principal is character, action in ["write", "delete"], resource is object) when { principal.role in ["builder", "admin"] };
permit(principal is character, action in ["execute"], resource is command) when { principal.role in ["builder", "admin"] && resource.name in ["dig", "create", "describe", "link"] };
permit(principal is character, action, resource) when { principal.role == "admin" };
permit(principal is character, action in ["read"], resource is property) when { resource.visibility == "public" && principal.location == resource.parent_location };
permit(principal is character, action in ["read"], resource is property) when { resource.visibility == "private" && resource.owner == principal.id };
permit(principal is character, action in ["read"], resource is property) when { resource.visibility == "admin" && principal.role == "admin" };
forbid(principal, action, resource);
```

**Operator coverage:**

- `==`, `!=`, `>`, `>=`, `<`, `<=` — comparisons
- `in` — list membership and attribute list membership
- `like` — glob pattern matching
- `has` — attribute existence (simple and dotted paths)
- `containsAll(list)`, `containsAny(list)` — list methods
- `!` — negation
- `&&`, `||` — boolean logic
- `if-then-else` — conditional expression
- `resource == "location:01XYZ"` — resource exact match

**Invalid policies (expected errors):**

- Missing semicolon
- Unknown effect (not permit/forbid)
- Bare boolean attribute (`principal.admin` without `== true`) → compile error
- Reserved word as attribute name
- Nesting depth >32 → error
- Malformed conditions

**Step 2: Implement parser using participle**

```go
// internal/access/policy/dsl/parser.go
package dsl

import (
    "github.com/alecthomas/participle/v2"
    "github.com/alecthomas/participle/v2/lexer"
)

// Define lexer rules for the DSL.
var policyLexer = lexer.MustSimple([]lexer.SimpleRule{
    // Define tokens: strings, numbers, identifiers, operators, punctuation
})

var policyParser *participle.Parser[Policy]

func init() {
    policyParser = participle.MustBuild[Policy](
        participle.Lexer(policyLexer),
        participle.UseLookahead(2),
    )
}

// Parse parses DSL text into an AST. Returns descriptive errors with position info.
func Parse(dslText string) (*Policy, error) {
    return policyParser.ParseString("", dslText)
}
```

**Step 3: Run tests**

Run: `task test`
Expected: PASS for all valid policies, descriptive errors for invalid ones

**Step 4: Commit**

```bash
git add internal/access/policy/dsl/
git commit -m "feat(access): add participle-based DSL parser"
```

---

### Task 9: Add DSL fuzz tests

**Spec References:** §16 (Testing Strategy — Fuzz Testing, lines 3272-3314), Policy DSL Grammar (lines 737-825)

**Acceptance Criteria:**

- [ ] `FuzzParse` function defined with seed corpus containing all valid policy forms
- [ ] Fuzz test runs for 30s without any panics: `go test -fuzz=FuzzParse -fuzztime=30s`
- [ ] Parser never panics on arbitrary input (returns error instead)
- [ ] Seed corpus includes at least: permit, forbid, all operator types, if-then-else

**Files:**

- Create: `internal/access/policy/dsl/parser_fuzz_test.go`

**Step 1: Write fuzz tests**

```go
// internal/access/policy/dsl/parser_fuzz_test.go
package dsl_test

import (
    "testing"

    "github.com/holomush/holomush/internal/access/policy/dsl"
)

func FuzzParse(f *testing.F) {
    // Seed corpus with all valid policy forms
    f.Add(`permit(principal is character, action in ["read"], resource is location) when { resource.id == principal.location };`)
    f.Add(`forbid(principal, action, resource);`)
    f.Add(`permit(principal is character, action in ["execute"], resource is command) when { resource.name in ["say", "pose", "look", "go"] };`)
    f.Add(`permit(principal is character, action, resource) when { principal.role == "admin" };`)
    f.Add(`permit(principal is character, action in ["read"], resource is property) when { resource.visibility == "public" && principal.location == resource.parent_location };`)
    f.Add(`permit(principal is character, action in ["emit"], resource is stream) when { resource.name like "location:*" && resource.location == principal.location };`)
    f.Add(`permit(principal, action, resource) when { if principal has faction then principal.faction == resource.faction else true };`)

    f.Fuzz(func(t *testing.T, input string) {
        // Parser must not panic on any input.
        _, _ = dsl.Parse(input)
    })
}
```

**Step 2: Run fuzz tests to verify they work**

Run: `go test -fuzz=FuzzParse -fuzztime=30s ./internal/access/policy/dsl/`
Expected: No panics

**Step 3: Commit**

```bash
git add internal/access/policy/dsl/parser_fuzz_test.go
git commit -m "test(access): add fuzz tests for DSL parser"
```

---

### Task 10: Build DSL condition evaluator

**Spec References:** §4.2 (Operator Semantics), §6.3 (Fail-Safe Attribute Handling), §6.4 (Nesting Depth Limit)

**Acceptance Criteria:**

- [ ] Every operator from the spec has table-driven tests covering: valid inputs, missing attributes, type mismatches
- [ ] Missing attribute → evaluates to `false` for ALL comparisons (Cedar-aligned fail-safe)
- [ ] Type mismatch → evaluates to `false` (fail-safe)
- [ ] Depth limit enforced at 32 levels; exceeding returns `false`
- [ ] `like` operator uses glob matching (e.g., `location:*` matches `location:01XYZ`)
- [ ] `if-then-else` evaluates correctly when `has` condition is true/false
- [ ] `containsAll` and `containsAny` work with list attributes
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/dsl/evaluator.go`
- Test: `internal/access/policy/dsl/evaluator_test.go`

**Step 1: Write failing evaluator tests**

Table-driven tests covering EVERY operator (spec requirement). Each operator needs test cases for:

1. Valid inputs (happy path)
2. Missing attributes → evaluates to `false` (fail-safe)
3. Type mismatch → evaluates to `false` (fail-safe)

Operators to cover:

| Operator             | Example                                                            |
| -------------------- | ------------------------------------------------------------------ |
| `==`                 | `principal.role == "admin"`                                        |
| `!=`                 | `principal.role != "guest"`                                        |
| `>`, `>=`, `<`, `<=` | `principal.level > 5`                                              |
| `in` (list)          | `resource.name in ["say", "pose"]`                                 |
| `in` (attr)          | `principal.role in resource.allowed_roles`                         |
| `like`               | `resource.name like "location:*"`                                  |
| `has`                | `principal has faction`, `principal has reputation.score`          |
| `containsAll`        | `principal.flags.containsAll(["vip", "beta"])`                     |
| `containsAny`        | `principal.flags.containsAny(["vip", "beta"])`                     |
| `!`                  | `!(principal.role == "banned")`                                    |
| `&&`                 | `a && b`                                                           |
| `\|\|`               | `a \|\| b`                                                         |
| `if-then-else`       | `if principal has faction then principal.faction == "x" else true` |

**Step 2: Implement evaluator**

```go
// internal/access/policy/dsl/evaluator.go
package dsl

import "github.com/holomush/holomush/internal/access/policy"

// EvalContext provides attribute bags and configuration for evaluation.
type EvalContext struct {
    Bags     *policy.AttributeBags
    MaxDepth int // default 32
}

// EvaluateConditions evaluates the condition block against the attribute bags.
// Returns true if all conditions are satisfied.
func EvaluateConditions(ctx *EvalContext, cond *ConditionBlock) bool
```

Key behaviors:

- **Attribute resolution:** `principal.faction` → lookup `"faction"` in `ctx.Bags.Subject`
- **Dotted paths:** `principal.reputation.score` → lookup `"reputation.score"` (flat dot-delimited key)
- **Missing attribute → `false`** for ALL comparisons (Cedar-aligned fail-safe)
- **Depth limit:** enforce `MaxDepth` (default 32), return `false` if exceeded
- **Glob matching:** use `github.com/gobwas/glob` for `like` operator, pre-compiled in `GlobCache`

**Step 3: Run tests**

Run: `task test`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/access/policy/dsl/evaluator.go internal/access/policy/dsl/evaluator_test.go
git commit -m "feat(access): add DSL condition evaluator with fail-safe semantics"
```

---

### Task 11: Build PolicyCompiler

**Spec References:** §5 (Compilation Pipeline, lines 845-930), §5.1 (Validation Warnings), §5.2 (Glob Pre-compilation)

**Acceptance Criteria:**

- [ ] `Compile()` parses DSL text, validates against schema, returns `CompiledPolicy`
- [ ] Valid DSL → `CompiledPolicy` with correct Effect, Target, Conditions
- [ ] Invalid DSL → error with line/column info
- [ ] Bare boolean attribute (`when { principal.admin }`) → compile error (not warning)
- [ ] Unregistered `action.*` attribute → compile error
- [ ] Unknown attribute → validation warning (not error)
- [ ] Unreachable condition (`false && ...`) → warning
- [ ] Always-true condition → warning
- [ ] Glob patterns pre-compiled in `GlobCache`
- [ ] `compiled_ast` JSONB serialization round-trips correctly
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/compiler.go`
- Test: `internal/access/policy/compiler_test.go`

**Step 1: Write failing tests**

- Compile valid DSL → returns CompiledPolicy with correct Effect, Target, Conditions
- Compile invalid DSL → returns error with line/column info
- Compile bare boolean attribute (`when { principal.admin }`) → returns compile error (not warning)
- Compile unknown attribute → returns validation warning (not error)
- Compile unreachable condition (`false && ...`) → returns warning
- Compile always-true condition → returns warning
- Compile unregistered `action.*` attribute → returns compile error
- Verify glob patterns are pre-compiled in `GlobCache`
- Verify `compiled_ast` JSONB serialization round-trips correctly

**Step 2: Implement PolicyCompiler**

```go
// internal/access/policy/compiler.go
package policy

import (
    "github.com/holomush/holomush/internal/access/policy/dsl"
)

// PolicyCompiler parses and validates DSL policy text.
type PolicyCompiler struct {
    schema *AttributeSchema
}

// NewPolicyCompiler creates a PolicyCompiler with the given schema.
func NewPolicyCompiler(schema *AttributeSchema) *PolicyCompiler

// ValidationWarning is a non-blocking issue found during compilation.
type ValidationWarning struct {
    Line    int
    Column  int
    Message string
}

// CompiledPolicy is the parsed, validated, and optimized form of a policy.
type CompiledPolicy struct {
    GrammarVersion int
    Effect         Effect
    Target         CompiledTarget
    Conditions     *dsl.ConditionBlock
    GlobCache      map[string]glob.Glob
}

// CompiledTarget is the parsed target clause.
type CompiledTarget struct {
    PrincipalType *string  // nil = matches all subjects
    ActionList    []string // nil/empty = matches all actions
    ResourceType  *string  // nil = matches all resources (if ResourceExact also nil)
    ResourceExact *string  // non-nil = exact string match
}

// Compile parses DSL text, validates it, and returns a compiled policy.
func (c *PolicyCompiler) Compile(dslText string) (*CompiledPolicy, []ValidationWarning, error)
```

**Step 3: Run tests**

Run: `task test`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/access/policy/compiler.go internal/access/policy/compiler_test.go
git commit -m "feat(access): add PolicyCompiler with validation and glob pre-compilation"
```

---

## Phase 7.3: Policy Engine & Attribute Providers

### Task 12: Attribute provider interface and schema registry

**Spec References:** §3.4 (AttributeProvider interface, lines 393-512), §5.3 (Schema Registration)

**Acceptance Criteria:**

- [ ] `AttributeProvider` interface: `Namespace()`, `ResolveSubject()`, `ResolveResource()`, `LockTokens()`
- [ ] `EnvironmentProvider` interface: `Namespace()`, `Resolve()`
- [ ] `AttributeSchema` supports: `Register()`, `IsRegistered()`
- [ ] Duplicate namespace registration → error
- [ ] Empty namespace → error
- [ ] Duplicate attribute key within namespace → error
- [ ] Invalid attribute type → error
- [ ] `AttrType` enum: `String`, `Int`, `Float`, `Bool`, `StringList`
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/attribute/provider.go`
- Create: `internal/access/policy/attribute/schema.go`
- Test: `internal/access/policy/attribute/schema_test.go`

**Step 1: Write failing tests for schema registration**

Test cases: register namespace, duplicate namespace → error, empty namespace → error, duplicate attribute key → error, invalid type → error, `IsRegistered()` returns correct results.

**Step 2: Implement**

```go
// internal/access/policy/attribute/provider.go
package attribute

import "context"

// AttributeProvider resolves attributes for a specific namespace.
type AttributeProvider interface {
    Namespace() string
    ResolveSubject(ctx context.Context, subjectType, subjectID string) (map[string]any, error)
    ResolveResource(ctx context.Context, resourceType, resourceID string) (map[string]any, error)
    LockTokens() []LockTokenDef
}

// EnvironmentProvider resolves environment attributes (no entity context).
type EnvironmentProvider interface {
    Namespace() string
    Resolve(ctx context.Context) (map[string]any, error)
}

// LockTokenDef defines a lock token contributed by a provider.
type LockTokenDef struct {
    Token       string
    Description string
    AttrPath    string
}
```

```go
// internal/access/policy/attribute/schema.go
package attribute

// AttrType identifies the type of an attribute value.
type AttrType int

const (
    AttrTypeString     AttrType = iota
    AttrTypeInt
    AttrTypeFloat
    AttrTypeBool
    AttrTypeStringList
)

// NamespaceSchema defines the attributes in a namespace.
type NamespaceSchema struct {
    Attributes map[string]AttrType
}

// AttributeSchema validates attribute references during policy compilation.
type AttributeSchema struct {
    namespaces map[string]*NamespaceSchema
}

func NewAttributeSchema() *AttributeSchema
func (s *AttributeSchema) Register(namespace string, schema *NamespaceSchema) error
func (s *AttributeSchema) IsRegistered(namespace, key string) bool
```

**Step 3: Run tests, commit**

```bash
git add internal/access/policy/attribute/
git commit -m "feat(access): add AttributeProvider interface and schema registry"
```

---

### Task 13: Attribute resolver with per-request caching

**Spec References:** §6.1 (Eager Attribute Resolution), §6.2 (Fair-Share Timeout), §6.6 (Per-Request Caching), ADR 0012 (Eager attribute resolution)

**Acceptance Criteria:**

- [ ] Single provider → correct attribute bags returned
- [ ] Multiple providers → merge semantics (last-registered wins for scalars, concatenate for lists)
- [ ] Core-to-plugin key collision → reject plugin registration at startup
- [ ] Plugin-to-plugin key collision → warn, last registered wins
- [ ] Provider error → skip provider, continue, record `ProviderError`
- [ ] Per-request cache → second `Resolve()` with same entity reuses cached result
- [ ] Fair-share budget: `max(remainingBudget / remainingProviders, 5ms)`
- [ ] Provider exceeding fair-share timeout → cancelled
- [ ] Re-entrance detection → provider calling `Evaluate()` on same context → panic
- [ ] `AttributeCache` is LRU with max 100 entries, attached to context
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/attribute/resolver.go`
- Create: `internal/access/policy/attribute/cache.go`
- Test: `internal/access/policy/attribute/resolver_test.go`

**Step 1: Write failing tests**

- Resolve with single provider → correct bags
- Resolve with multiple providers → merge semantics (last-registered wins for scalars, concatenate for lists)
- Core-to-plugin key collision → reject plugin registration at startup
- Plugin-to-plugin key collision → warn, last registered wins
- Provider error → skip provider, continue, record `ProviderError`
- Per-request cache → second `Resolve()` with same entity reuses cached result
- Timeout enforcement → provider exceeding fair-share timeout is cancelled
- Fair-share budget: 4 providers with 100ms total → ~25ms each initially
- Re-entrance detection → provider calling `Evaluate()` on same context → panic

**Step 2: Implement resolver**

```go
// internal/access/policy/attribute/resolver.go
package attribute

import (
    "context"
    "time"

    "github.com/holomush/holomush/internal/access/policy"
)

// ProviderError records a provider failure during resolution.
type ProviderError struct {
    Namespace  string
    Error      string
    DurationUS int
}

// AttributeResolver orchestrates attribute providers with caching and timeouts.
type AttributeResolver struct {
    providers    []AttributeProvider
    envProviders []EnvironmentProvider
    schema       *AttributeSchema
    totalBudget  time.Duration // default 100ms
}

func NewAttributeResolver(budget time.Duration) *AttributeResolver
func (r *AttributeResolver) RegisterProvider(p AttributeProvider) error
func (r *AttributeResolver) RegisterEnvironmentProvider(p EnvironmentProvider)
func (r *AttributeResolver) Resolve(ctx context.Context, req policy.AccessRequest) (*policy.AttributeBags, []ProviderError, error)
```

Key: fair-share timeout is `max(remainingBudget / remainingProviders, 5ms)`.

```go
// internal/access/policy/attribute/cache.go
package attribute

// AttributeCache is a per-request LRU cache for resolved attributes.
type AttributeCache struct {
    entries map[string]map[string]any // key: "subject:character:01ABC" → attrs
    maxSize int                       // default 100
}

type cacheContextKey struct{}

// WithAttributeCache attaches a cache to the context.
func WithAttributeCache(ctx context.Context) context.Context
// FromContext retrieves the cache from context (nil if absent).
func FromContext(ctx context.Context) *AttributeCache
```

**Step 3: Run tests, commit**

```bash
git add internal/access/policy/attribute/
git commit -m "feat(access): add AttributeResolver with fair-share timeouts and per-request caching"
```

---

### Task 14: Core attribute providers (character, location, object)

**Spec References:** §7.1 (Character Attributes), §7.2 (Location Attributes), §7.3 (Object Attributes)

**Acceptance Criteria:**

- [ ] CharacterProvider resolves: `type`, `id`, `name`, `role`, `faction`, `level`, `flags`, `location`
- [ ] CharacterProvider only resolves `character` type subjects/resources (returns nil for others)
- [ ] LocationProvider resolves: `type`, `id`, `name`, `faction`, `restricted`
- [ ] LocationProvider only resolves `location` type resources
- [ ] ObjectProvider resolves: `type`, `id`, `name`, `location`, `owner`, `flags`
- [ ] ObjectProvider only resolves `object` type resources
- [ ] Each provider uses mockery-generated mocks for world model repositories
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/attribute/character.go`
- Create: `internal/access/policy/attribute/location.go`
- Create: `internal/access/policy/attribute/object.go`
- Test: `internal/access/policy/attribute/character_test.go`
- Test: `internal/access/policy/attribute/location_test.go`
- Test: `internal/access/policy/attribute/object_test.go`

**Step 1: Write failing tests for each provider**

CharacterProvider:

- `ResolveSubject("character", "01ABC")` → `{"type": "character", "id": "01ABC", "name": "...", "role": "player", "faction": "rebels", "level": 5, "flags": ["vip"], "location": "01XYZ"}`
- `ResolveSubject("plugin", "...")` → `(nil, nil)` — character provider only resolves characters
- `ResolveResource("character", "01ABC")` → same attrs (for `resource is character` policies)
- `ResolveResource("location", "...")` → `(nil, nil)` — wrong resource type

LocationProvider:

- `ResolveResource("location", "01XYZ")` → `{"type": "location", "id": "01XYZ", "name": "Town Square", "faction": "neutral", "restricted": false}`
- `ResolveSubject(...)` → `(nil, nil)` — resource-only provider

ObjectProvider:

- `ResolveResource("object", "01DEF")` → `{"type": "object", "id": "01DEF", "name": "Sword", "location": "01XYZ", "owner": "01ABC", "flags": ["weapon"]}`

Each provider takes world model repositories as constructor dependencies. Use mockery-generated mocks for tests.

**Step 2: Implement providers**

**Step 3: Run tests, commit**

```bash
git add internal/access/policy/attribute/character.go internal/access/policy/attribute/location.go
git add internal/access/policy/attribute/object.go
git add internal/access/policy/attribute/*_test.go
git commit -m "feat(access): add core attribute providers (character, location, object)"
```

---

### Task 15: Remaining core providers (environment, command, stream, property)

**Spec References:** §7.4 (Environment Attributes), §7.5 (Command Attributes), §7.6 (Stream Attributes), §7.7 (Property Attributes), ADR 0013 (Properties as first-class entities)

**Acceptance Criteria:**

- [ ] EnvironmentProvider implements `EnvironmentProvider` interface; resolves `time`, `hour`, `minute`, `day_of_week`, `maintenance`
- [ ] CommandProvider resolves `type`, `name` for `command` resources only
- [ ] StreamProvider resolves `type`, `name`, `location` for `stream` resources only
- [ ] PropertyProvider resolves all property attributes including `parent_location`
- [ ] `parent_location` uses recursive CTE with depth limit of 20
- [ ] `parent_location` resolution timeout: 100ms
- [ ] Circuit breaker: 3 timeout errors in 60s → skip queries for 60s
- [ ] Nested containment test: object-in-object-in-room resolves `parent_location` to room
- [ ] Cycle detection → error before depth limit
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/attribute/environment.go`
- Create: `internal/access/policy/attribute/command.go`
- Create: `internal/access/policy/attribute/stream.go`
- Create: `internal/access/policy/attribute/property.go`
- Test files for each

**Step 1: Write failing tests**

EnvironmentProvider (implements `EnvironmentProvider` interface):

- `Resolve()` → `{"time": "2026-02-06T14:30:00Z", "hour": 14, "minute": 30, "day_of_week": "friday", "maintenance": false}`

CommandProvider:

- `ResolveResource("command", "say")` → `{"type": "command", "name": "say"}`
- `ResolveResource("location", "...")` → `(nil, nil)` — wrong type

StreamProvider:

- `ResolveResource("stream", "location:01XYZ")` → `{"type": "stream", "name": "location:01XYZ", "location": "01XYZ"}`

PropertyProvider:

- `ResolveResource("property", "01GHI")` → all property attributes including `parent_location`
- Nested containment: object in object in room → resolves `parent_location` to room
- `parent_location` resolution timeout (100ms) → error, circuit breaker trips after 3 timeouts in 60s
- Cycle detection → error before depth limit (20 levels)

**Step 2: Implement providers**

PropertyProvider's `parent_location` uses recursive CTE:

```sql
WITH RECURSIVE containment AS (
    SELECT parent_type, parent_id, 0 AS depth
    FROM entity_properties WHERE id = $1
    UNION ALL
    SELECT 'location', o.location_id, c.depth + 1
    FROM containment c
    JOIN objects o ON c.parent_type = 'object' AND c.parent_id = o.id::text
    WHERE c.depth < 20
)
SELECT parent_id FROM containment
WHERE parent_type = 'location'
ORDER BY depth DESC LIMIT 1;
```

Circuit breaker: 3 timeout errors in 60s → skip queries for 60s.

**Step 3: Run tests, commit**

```bash
git add internal/access/policy/attribute/environment.go internal/access/policy/attribute/command.go
git add internal/access/policy/attribute/stream.go internal/access/policy/attribute/property.go
git add internal/access/policy/attribute/*_test.go
git commit -m "feat(access): add environment, command, stream, and property providers"
```

---

### Task 16: Build AccessPolicyEngine

**Spec References:** §6 (Evaluation Algorithm, 7-step flow, lines 1642-1690), §3.3 (Session Resolution), ADR 0011 (Deny-overrides), ADR 0012 (Eager attribute resolution)

**Acceptance Criteria:**

- [ ] Implements the 9-step evaluation algorithm from the spec exactly
- [ ] Step 1: Caller invokes `Evaluate(ctx, AccessRequest)`
- [ ] Step 2: System bypass — subject `"system"` → `Decision{Allowed: true, Effect: SystemBypass}`
- [ ] Step 3: Session resolution — subject `"session:web-123"` → resolved to `"character:01ABC"` via SessionResolver
  - [ ] Invalid session → `Decision{Allowed: false, PolicyID: "infra:session-invalid"}`
  - [ ] Session store error → `Decision{Allowed: false, PolicyID: "infra:session-store-error"}`
- [ ] Step 4: Eager attribute resolution (all attributes collected before evaluation)
- [ ] Step 5: Engine loads matching policies from the in-memory cache
- [ ] Step 6: Engine evaluates each policy's conditions against the attribute bags
- [ ] Step 7: Deny-overrides — forbid + permit both match → forbid wins (ADR 0011)
  - [ ] No policies match → `Decision{Allowed: false, Effect: DefaultDeny}`
- [ ] Step 8: Audit logger records the decision, matched policies, and attribute snapshot per configured mode
- [ ] Step 9: Returns `Decision` with allowed/denied, reason, and matched policy ID
- [ ] Provider error → evaluation continues, error recorded in decision
- [ ] Per-request cache → second call reuses cached attributes
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/engine.go`
- Test: `internal/access/policy/engine_test.go`

**Step 1: Write failing tests**

Table-driven tests covering the 9-step evaluation algorithm (spec lines 148-160):

1. **System bypass:** Subject `"system"` → `Decision{Allowed: true, Effect: SystemBypass}`
2. **Session resolution:** Subject `"session:web-123"` → resolved to `"character:01ABC"`, then evaluated
3. **Session invalid:** Subject `"session:expired"` → `Decision{Allowed: false, Effect: DefaultDeny, PolicyID: "infra:session-invalid"}`
4. **Session store error:** DB failure → `Decision{Allowed: false, Effect: DefaultDeny, PolicyID: "infra:session-store-error"}`
5. **Eager attribute resolution:** All providers called before evaluation
6. **Policy matching:** Target filtering — principal type, action list, resource type/exact
7. **Condition evaluation:** Policies with satisfied conditions
8. **Deny-overrides:** Both permit and forbid match → forbid wins
9. **Default deny:** No policies match → `Decision{Allowed: false, Effect: DefaultDeny, PolicyID: ""}`
10. **Audit logging:** Audit entry logged per configured mode
11. **Provider error:** Provider fails → evaluation continues, error recorded in decision
12. **Cache warmth:** Second call in same request reuses per-request attribute cache

**Step 2: Implement engine**

```go
// internal/access/policy/engine.go
package policy

import "context"

// AccessPolicyEngine is the main entry point for policy-based authorization.
type AccessPolicyEngine interface {
    Evaluate(ctx context.Context, request AccessRequest) (Decision, error)
}

// SessionResolver resolves session: subjects to character: subjects.
type SessionResolver interface {
    ResolveSession(ctx context.Context, sessionID string) (characterID string, err error)
}

// AuditLogger logs access decisions.
type AuditLogger interface {
    Log(entry AuditEntry)
}

// Engine implements AccessPolicyEngine.
type Engine struct {
    resolver     *attribute.AttributeResolver
    policyCache  *PolicyCache
    sessions     SessionResolver
    auditLogger  AuditLogger
}

func NewEngine(resolver *attribute.AttributeResolver, cache *PolicyCache, sessions SessionResolver, audit AuditLogger) *Engine

func (e *Engine) Evaluate(ctx context.Context, req AccessRequest) (Decision, error) {
    // Step 1: Invocation entry point
    // Step 2: System bypass
    // Step 3: Session resolution
    // Step 4: Resolve attributes (eager)
    // Step 5: Load applicable policies (from cache snapshot)
    // Step 6: Evaluate conditions per policy
    // Step 7: Combine decisions (deny-overrides)
    // Step 8: Audit
    // Step 9: Return decision
}
```

**Step 3: Run tests**

Run: `task test`
Expected: PASS

**Step 4: Commit**

```bash
git add internal/access/policy/engine.go internal/access/policy/engine_test.go
git commit -m "feat(access): add AccessPolicyEngine with deny-overrides evaluation"
```

---

### Task 17: Policy cache with LISTEN/NOTIFY invalidation

**Spec References:** §6.5 (Cache Invalidation, lines 1327-1345), ADR 0016 (LISTEN/NOTIFY cache invalidation)

**Acceptance Criteria:**

- [ ] `Snapshot()` returns read-only copy safe for concurrent use
- [ ] `Reload()` fetches all enabled policies from store, recompiles, swaps snapshot atomically
- [ ] `Listen()` subscribes to PostgreSQL `NOTIFY` on `policy_changed` channel
- [ ] NOTIFY event → cache reloads before next evaluation
- [ ] Concurrent reads during reload → stale reads tolerable (snapshot semantics)
- [ ] Connection drop + reconnect → full reload
- [ ] Reload latency <50ms (benchmark test)
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/cache.go`
- Test: `internal/access/policy/cache_test.go`

**Step 1: Write failing tests**

- Load policies from store → cache snapshot available
- NOTIFY event on `policy_changed` → cache reloads before next evaluation
- Concurrent reads during reload → use snapshot semantics (stale reads tolerable)
- Connection drop + reconnect → full reload
- Reload latency benchmark: <50ms target

**Step 2: Implement**

```go
// internal/access/policy/cache.go
package policy

import "sync"

// PolicyCache holds compiled policies in memory with LISTEN/NOTIFY refresh.
type PolicyCache struct {
    mu       sync.RWMutex
    policies []*CachedPolicy
    store    store.PolicyStore
    compiler *PolicyCompiler
}

type CachedPolicy struct {
    Stored   store.StoredPolicy
    Compiled *CompiledPolicy
}

// Snapshot returns a read-only copy of current policies (safe for concurrent use).
func (c *PolicyCache) Snapshot() []*CachedPolicy

// Reload fetches all enabled policies from store, recompiles, and swaps snapshot.
func (c *PolicyCache) Reload(ctx context.Context) error

// Listen subscribes to PostgreSQL NOTIFY on 'policy_changed' channel.
// On notification, triggers Reload(). On reconnect after drop, triggers full Reload().
func (c *PolicyCache) Listen(ctx context.Context, pool *pgxpool.Pool) error
```

**Step 3: Run tests, commit**

```bash
git add internal/access/policy/cache.go internal/access/policy/cache_test.go
git commit -m "feat(access): add policy cache with LISTEN/NOTIFY invalidation"
```

---

### Task 18: Audit logger

**Spec References:** Audit Log Serialization (lines 2161-2192), Audit Log Configuration (lines 2193-2269), Audit Log Retention (lines 2271-2310)

**Acceptance Criteria:**

- [ ] Three audit modes: `off` (system bypasses only), `denials_only`, `all`
- [ ] Mode `off`: only system bypasses logged
- [ ] Mode `denials_only`: denials + default deny + system bypass logged, allows skipped
- [ ] Mode `all`: everything logged
- [ ] **Sync write for denials:** `deny` and `default_deny` events written synchronously to PostgreSQL before `Evaluate()` returns
- [ ] **Async write for allows:** `allow` and system bypass events written asynchronously via buffered channel
- [ ] Channel full → entry dropped, `abac_audit_channel_full_total` metric incremented
- [ ] **WAL fallback:** If sync write fails, denial entry written to `$XDG_STATE_HOME/holomush/audit-wal.jsonl` (append-only, O_SYNC)
- [ ] **ReplayWAL():** Method reads WAL entries, batch-inserts to PostgreSQL, truncates file on success
- [ ] Catastrophic failure (DB + WAL fail) → log to stderr at ERROR, increment `abac_audit_failures_total{reason="wal_failed"}`, drop entry
- [ ] Entry includes: subject, action, resource, effect, policy\_id, policy\_name, attributes snapshot, duration\_us
- [ ] `audit/postgres.go` batch-inserts from channel (async) and handles sync writes (denials)
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/audit/logger.go`
- Create: `internal/access/policy/audit/postgres.go`
- Test: `internal/access/policy/audit/logger_test.go`

**Step 1: Write failing tests**

- Mode `off`: only system bypasses logged
- Mode `denials_only`: denials + default deny + system bypass logged, allows skipped
- Mode `all`: everything logged
- **Sync write for denials:** `deny` and `default_deny` events written synchronously, `Evaluate()` blocks until write completes
- **Async write for allows:** `allow` and system bypass events submitted via buffered channel, doesn't block `Evaluate()`
- Channel full: entry dropped, `abac_audit_channel_full_total` metric incremented
- **WAL fallback:** If sync write fails, denial entry written to `$XDG_STATE_HOME/holomush/audit-wal.jsonl`
- **ReplayWAL():** Reads WAL file, batch-inserts to PostgreSQL, truncates on success
- Catastrophic failure: DB + WAL fail → stderr log, `abac_audit_failures_total{reason="wal_failed"}` incremented, entry dropped
- Verify entry contains: subject, action, resource, effect, policy_id, policy_name, attributes snapshot, duration_us

**Step 2: Implement**

```go
// internal/access/policy/audit/logger.go
package audit

// AuditMode controls what decisions are logged.
type AuditMode int

const (
    AuditModeOff         AuditMode = iota // system bypasses only
    AuditModeDenialsOnly                   // denials + default deny + system bypass
    AuditModeAll                           // everything
)

// Logger writes audit entries with sync (denials) or async (allows) paths.
// Denials are written synchronously to prevent evidence erasure.
type Logger struct {
    mode      AuditMode
    entryCh   chan Entry         // async channel for allow/system bypass
    writer    Writer             // PostgreSQL writer
    walPath   string             // Write-Ahead Log path for denial fallback
    walFile   *os.File           // WAL file handle (opened with O_APPEND | O_SYNC)
}

// Writer persists audit entries (PostgreSQL implementation in postgres.go).
type Writer interface {
    Write(ctx context.Context, entries []Entry) error       // batch writes (async)
    WriteSync(ctx context.Context, entry Entry) error       // single sync write (denials)
}

// ReplayWAL reads entries from the WAL file, batch-inserts to PostgreSQL,
// and truncates the file on success. Called on startup and periodically
// during recovery from transient database failures.
func (l *Logger) ReplayWAL(ctx context.Context) error

// Entry is a single audit log record.
type Entry struct {
    ID             string
    Timestamp      time.Time
    Subject        string
    Action         string
    Resource       string
    Effect         string
    PolicyID       string
    PolicyName     string
    Attributes     *policy.AttributeBags
    ErrorMessage   string
    ProviderErrors []attribute.ProviderError
    DurationUS     int
}
```

`audit/postgres.go` implements both async batch-inserts from the channel and synchronous single writes for denials. The logger distinguishes between effects:

- **`deny` and `default_deny`:** Call `writer.WriteSync()` synchronously before returning from `Log()`. If the write fails, append the entry to `$XDG_STATE_HOME/holomush/audit-wal.jsonl` (opened with `O_APPEND | O_SYNC`). If both DB and WAL fail, log to stderr, increment `abac_audit_failures_total{reason="wal_failed"}`, and drop the entry.
- **`allow` and system bypass:** Send to `entryCh` buffered channel for async batch writes. If channel is full, drop entry and increment `abac_audit_channel_full_total`.

`ReplayWAL()` reads JSON-encoded entries from the WAL file, batch-inserts them to PostgreSQL, and truncates the file on success. The server calls this on startup and MAY call it periodically (e.g., every 5 minutes) during recovery.

**Step 3: Run tests, commit**

```bash
git add internal/access/policy/audit/
git commit -m "feat(access): add async audit logger with mode control"
```

---

### Task 19: Prometheus metrics for ABAC

**Spec References:** §9 (Observability, lines 1415-1465), §9.1 (Metric Names and Labels)

**Acceptance Criteria:**

- [ ] `abac_evaluate_duration_seconds` histogram recorded after each `Evaluate()`
- [ ] `abac_policy_evaluations_total` counter with `name` and `effect` labels
- [ ] `abac_audit_channel_full_total` counter for dropped audit entries
- [ ] `abac_provider_circuit_breaker_trips_total` counter with `provider` label
- [ ] `abac_provider_errors_total` counter with `namespace` and `error_type` labels
- [ ] `abac_policy_cache_last_update` gauge with Unix timestamp
- [ ] `abac_unregistered_attributes_total` counter (schema drift indicator)
- [ ] `RegisterMetrics()` follows existing pattern from `internal/observability/server.go`
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/metrics.go`
- Test: `internal/access/policy/metrics_test.go`

**Step 1: Write tests verifying metrics are recorded**

After `Evaluate()`, verify the relevant counters/histograms are updated.

**Step 2: Implement**

Follow existing observability pattern from `internal/observability/server.go`:

```go
// internal/access/policy/metrics.go
package policy

import "github.com/prometheus/client_golang/prometheus"

var (
    evaluateDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
        Name:    "abac_evaluate_duration_seconds",
        Help:    "Time spent in AccessPolicyEngine.Evaluate()",
        Buckets: prometheus.DefBuckets,
    })
    policyEvaluations = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "abac_policy_evaluations_total",
        Help: "Total policy evaluations by name and effect",
    }, []string{"name", "effect"})
    auditChannelFull = prometheus.NewCounter(prometheus.CounterOpts{
        Name: "abac_audit_channel_full_total",
        Help: "Audit entries dropped due to full channel",
    })
    providerCircuitBreakerTrips = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "abac_provider_circuit_breaker_trips_total",
        Help: "Circuit breaker trips by provider namespace",
    }, []string{"provider"})
    providerErrorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "abac_provider_errors_total",
        Help: "Provider errors by namespace and type",
    }, []string{"namespace", "error_type"})
    policyCacheLastUpdate = prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "abac_policy_cache_last_update",
        Help: "Unix timestamp of last policy cache update",
    })
    unregisteredAttributes = prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "abac_unregistered_attributes_total",
        Help: "References to unregistered attributes (schema drift indicator)",
    }, []string{"namespace", "key"})
)

// RegisterMetrics registers all ABAC metrics with the given registerer.
func RegisterMetrics(reg prometheus.Registerer) {
    reg.MustRegister(evaluateDuration, policyEvaluations, auditChannelFull,
        providerCircuitBreakerTrips, providerErrorsTotal,
        policyCacheLastUpdate, unregisteredAttributes)
}
```

**Step 3: Run tests, commit**

```bash
git add internal/access/policy/metrics.go internal/access/policy/metrics_test.go
git commit -m "feat(access): add Prometheus metrics for ABAC engine"
```

---

### Task 20: Performance benchmarks

**Spec References:** §6.7 (Performance Targets, lines 1715-1741)

**Acceptance Criteria:**

- [ ] `BenchmarkEvaluate_ColdCache` — p99 <5ms
- [ ] `BenchmarkEvaluate_WarmCache` — p99 <3ms
- [ ] `BenchmarkAttributeResolution_Cold` — <2ms
- [ ] `BenchmarkAttributeResolution_Warm` — <100μs
- [ ] `BenchmarkConditionEvaluation` — <1ms per policy
- [ ] `BenchmarkCacheReload` — <50ms
- [ ] `BenchmarkWorstCase_NestedIf` — 32-level nesting <5ms
- [ ] `BenchmarkWorstCase_AllPoliciesMatch` — 50 policies <10ms
- [ ] Setup: 50 active policies, 3 operators per condition avg, 10 attributes per entity
- [ ] All benchmarks run without errors

**Files:**

- Create: `internal/access/policy/engine_bench_test.go`

**Step 1: Write benchmarks per spec performance targets (lines 1715-1741)**

```go
func BenchmarkEvaluate_ColdCache(b *testing.B)         // target: <5ms p99
func BenchmarkEvaluate_WarmCache(b *testing.B)          // target: <3ms p99
func BenchmarkAttributeResolution_Cold(b *testing.B)    // target: <2ms
func BenchmarkAttributeResolution_Warm(b *testing.B)    // target: <100μs
func BenchmarkConditionEvaluation(b *testing.B)         // target: <1ms per policy
func BenchmarkCacheReload(b *testing.B)                 // target: <50ms
func BenchmarkWorstCase_NestedIf(b *testing.B)          // 32-level nesting <5ms
func BenchmarkWorstCase_AllPoliciesMatch(b *testing.B)  // 50 policies <10ms
```

Setup: 50 active policies (25 permit, 25 forbid), 3 operators per condition average, 10 attributes per entity.

**Step 2: Run benchmarks**

Run: `go test -bench=. -benchmem ./internal/access/policy/`
Expected: All within spec targets

**Step 3: Commit**

```bash
git add internal/access/policy/engine_bench_test.go
git commit -m "test(access): add ABAC engine benchmarks for performance targets"
```

---

## Phase 7.4: Seed Policies & Bootstrap

### Task 21: Define seed policy constants

**Spec References:** §12.1 (Seed Policies, lines 2935-2999), §12.2 (Seed Naming Convention)

**Acceptance Criteria:**

- [ ] All 14 seed policies defined as `SeedPolicy` structs (verify count against spec)
- [ ] All seed policies compile without error via `PolicyCompiler`
- [ ] Each seed policy name starts with `seed:`
- [ ] Each seed policy has `SeedVersion: 1` field for upgrade tracking
- [ ] No duplicate seed names
- [ ] DSL text matches spec exactly (lines 2935-2999)
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/seed.go`
- Test: `internal/access/policy/seed_test.go`

**Step 1: Write failing tests**

- All 14 seed policies compile without error via `PolicyCompiler`
- Each seed policy name starts with `seed:`
- Each seed policy source is `"seed"`
- No duplicate seed names
- DSL text matches spec exactly (lines 2935-2999)

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

// SeedPolicies returns the complete set of 14 seed policies.
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
    }
}
```

(Note: 14 seed policies listed above. The spec shows these 14 distinct named policies across lines 2935-2999. Verify count against spec during implementation.)

**Step 3: Run tests, commit**

```bash
git add internal/access/policy/seed.go internal/access/policy/seed_test.go
git commit -m "feat(access): define seed policies"
```

---

### Task 22: Bootstrap sequence

**Spec References:** §12.3 (Bootstrap Sequence, lines 3007-3074)

**Acceptance Criteria:**

- [ ] Uses `access.WithSystemSubject(context.Background())` to bypass ABAC for seed operations
- [ ] Per-seed name-based idempotency check via `policyStore.GetByName(ctx, seed.Name)`
- [ ] Skips seed if policy exists with same name and `source="seed"` (already seeded)
- [ ] Logs warning and skips if policy exists with same name but `source!="seed"` (admin collision)
- [ ] New seeds inserted with `source="seed"`, `seed_version=1`, `created_by="system"`
- [ ] Seed version upgrade: if shipped `seed_version > stored.seed_version`, update `dsl_text`, `compiled_ast`, and `seed_version`
- [ ] Upgrade populates `change_note` with `"Auto-upgraded from seed v{N} to v{N+1} on server upgrade"`
- [ ] Respects `--skip-seed-migrations` flag to disable automatic upgrades
- [ ] All tests pass via `task test`

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
    "github.com/holomush/holomush/internal/store"
    "github.com/samsarahq/go/oops"
)

// BootstrapOptions controls bootstrap behavior.
type BootstrapOptions struct {
    SkipSeedMigrations bool // Disable automatic seed version upgrades
}

// Bootstrap seeds the policy table with system policies.
// Called at server startup with system subject context (bypasses ABAC).
// Idempotent: checks each seed policy by name before insertion.
// Supports seed version upgrades unless opts.SkipSeedMigrations is true.
func Bootstrap(ctx context.Context, policyStore store.PolicyStore, compiler *PolicyCompiler, logger *slog.Logger, opts BootstrapOptions) error {
    // Use system subject context to bypass ABAC during bootstrap
    ctx = access.WithSystemSubject(ctx)

    for _, seed := range SeedPolicies() {
        // Per-seed idempotency check: query by name
        existing, err := policyStore.GetByName(ctx, seed.Name)
        if err != nil && !store.IsNotFound(err) {
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

        err = policyStore.Create(ctx, &store.StoredPolicy{
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
- `store.PolicyStore.GetByName(ctx, name)` retrieves policy by name, returns `IsNotFound` error if absent
- `access.WithSystemSubject(ctx)` marks context as system-level operation
- `PolicyStore.Create/Update` checks `access.IsSystemContext(ctx)` and bypasses `Evaluate()` when true
- Upgrade logic compares shipped `seed.SeedVersion` against stored `existing.SeedVersion`
- `--skip-seed-migrations` server flag sets `opts.SkipSeedMigrations=true`
- Legacy policies without `SeedVersion` (nil) will not be upgraded; future enhancement may treat nil as version 0
- `--force-seed-version=N` flag enables rollback (future enhancement, see spec lines 3066-3074)

**Step 3: Run tests, commit**

```bash
git add internal/access/policy/bootstrap.go internal/access/policy/bootstrap_test.go
git commit -m "feat(access): add seed policy bootstrap with version upgrades"
```

---

## Phase 7.5: Locks & Admin

### Task 23: Lock token registry

**Spec References:** §10.1 (Lock Tokens), §10.2 (Lock Token Registration)

**Acceptance Criteria:**

- [ ] Core lock tokens registered: `faction`, `flag`, `level`
- [ ] Plugin lock tokens require namespace prefix (e.g., `myplugin:custom_token`)
- [ ] Duplicate token → error
- [ ] `Lookup()` returns definition with DSL expansion info
- [ ] `All()` returns complete token list
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/lock/registry.go`
- Test: `internal/access/policy/lock/registry_test.go`

**Step 1: Write failing tests**

- Register core lock tokens (faction, flag, level)
- Register plugin lock tokens (must be namespace-prefixed)
- Duplicate token → error
- Lookup token → returns definition with DSL expansion info

**Step 2: Implement**

```go
// internal/access/policy/lock/registry.go
package lock

// LockTokenRegistry maps token names to their DSL expansion templates.
type LockTokenRegistry struct {
    tokens map[string]TokenDef
}

type TokenDef struct {
    Token       string
    Namespace   string
    Description string
    AttrPath    string // attribute path this token maps to
    ValueType   string // "string", "int", "bool"
}

func NewLockTokenRegistry() *LockTokenRegistry
func (r *LockTokenRegistry) Register(def TokenDef) error
func (r *LockTokenRegistry) Lookup(token string) (TokenDef, bool)
func (r *LockTokenRegistry) All() []TokenDef
```

**Step 3: Run tests, commit**

```bash
git add internal/access/policy/lock/
git commit -m "feat(access): add lock token registry"
```

---

### Task 24: Lock expression parser and compiler

**Spec References:** §10.3 (Lock Expression Syntax), §10.4 (Lock-to-DSL Compilation)

**Acceptance Criteria:**

- [ ] `faction:rebels` → generates `forbid` with faction check
- [ ] `flag:storyteller` → generates `forbid` with flag membership check
- [ ] `level>5` → generates `forbid` with level comparison (inverted)
- [ ] `faction:rebels & flag:storyteller` → compound (multiple forbids)
- [ ] `!faction:rebels` → negates faction check
- [ ] Compiler output → valid DSL that `PolicyCompiler` accepts
- [ ] Invalid lock expression → descriptive error
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/access/policy/lock/parser.go`
- Create: `internal/access/policy/lock/compiler.go`
- Test: `internal/access/policy/lock/parser_test.go`
- Test: `internal/access/policy/lock/compiler_test.go`

**Step 1: Write failing tests**

Lock syntax from spec:

- `faction:rebels` → `forbid(principal is character, action, resource == "<target>") when { principal.faction != "rebels" };`
- `flag:storyteller` → `forbid(principal is character, action, resource == "<target>") when { !("storyteller" in principal.flags) };`
- `level>5` → `forbid(principal is character, action, resource == "<target>") when { principal.level <= 5 };`
- `faction:rebels & flag:storyteller` → compound (both conditions as separate forbids or combined)
- `!faction:rebels` → negates faction check

Compiler takes parsed lock expression + target resource string → DSL policy text. Then PolicyCompiler validates the generated DSL.

**Step 2: Implement parser and compiler**

**Step 3: Run tests, commit**

```bash
git add internal/access/policy/lock/
git commit -m "feat(access): add lock expression parser and DSL compiler"
```

---

### Task 25: Property model (EntityProperty type and repository)

**Spec References:** §11 (Property Model), ADR 0013 (Properties as first-class entities)

**Acceptance Criteria:**

- [ ] `EntityProperty` struct: ID, ParentType, ParentID, Name, Value, Owner, Visibility, Flags, VisibleTo, ExcludedFrom, timestamps
- [ ] `PropertyRepository` interface: `Create`, `Get`, `ListByParent`, `Update`, `Delete`
- [ ] CRUD operations round-trip all fields correctly
- [ ] Visibility defaults: `restricted` → auto-set `visible_to=[owner]`, `excluded_from=[]`
- [ ] `visible_to` max 100 entries; `excluded_from` max 100 entries → error if exceeded
- [ ] No overlap between `visible_to` and `excluded_from` → error
- [ ] Parent name uniqueness → error on duplicate `(parent_type, parent_id, name)`
- [ ] Follows existing repository pattern from `internal/world/postgres/location_repo.go`
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/world/property.go` (EntityProperty type + PropertyRepository interface)
- Create: `internal/world/postgres/property_repo.go` (PostgreSQL implementation)
- Test: `internal/world/postgres/property_repo_test.go`

**Step 1: Write failing tests**

- Create property → round-trips all fields
- Get by ID
- List by parent (type + ID)
- Update property (value, visibility, flags)
- Delete property
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
    ParentID     string
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

### Task 26: Admin commands — policy create/list/show/edit/delete

**Spec References:** §13 (Admin Commands, lines 2060-2130)

**Acceptance Criteria:**

- [ ] `policy create <name> <dsl>` → validates DSL, stores policy, triggers NOTIFY
- [ ] `policy list` → shows all policies (filterable by `--source`, `--enabled`/`--disabled`)
- [ ] `policy show <name>` → displays full policy details
- [ ] `policy edit <name> <new_dsl>` → validates new DSL, increments version
- [ ] `policy delete <name>` → removes policy; seed policies cannot be deleted → error
- [ ] Admin-only permission check on create/edit/delete
- [ ] Invalid DSL input → helpful error message with line/column
- [ ] Commands registered in command registry following existing handler patterns
- [ ] All tests pass via `task test`

**Files:**

- Create: `internal/command/handlers/policy.go`
- Test: `internal/command/handlers/policy_test.go`

**Step 1: Write failing tests**

For each command, test:

- Valid invocation produces expected output
- Permission check (admin-only for create/edit/delete)
- Invalid input produces helpful error message
- `policy create <name> <dsl>` → validates DSL, stores policy, triggers NOTIFY
- `policy list` → shows all policies (filterable by `--source`, `--enabled`/`--disabled`)
- `policy show <name>` → displays full policy details
- `policy edit <name> <new_dsl>` → validates new DSL, increments version
- `policy delete <name>` → removes policy (seed policies cannot be deleted)

**Step 2: Implement**

Register commands in the command registry following existing handler patterns in `internal/command/handlers/`.

**Step 3: Run tests, commit**

```bash
git add internal/command/handlers/policy.go internal/command/handlers/policy_test.go
git commit -m "feat(command): add policy CRUD admin commands"
```

---

### Task 27: Admin commands — policy test/validate/reload/attributes/audit

**Spec References:** §13.1 (policy test), §13.2 (policy validate), §13.3 (policy reload), §13.4 (policy attributes), §13.5 (policy audit)

**Acceptance Criteria:**

- [ ] `policy test <subject> <action> <resource>` → returns decision and matched policies
- [ ] `policy test --verbose` → shows all candidate policies with match/no-match reasons
- [ ] `policy validate <dsl>` → success or error with line/column
- [ ] `policy reload` → forces cache reload from DB
- [ ] `policy attributes` → lists all registered attribute namespaces and keys
- [ ] `policy attributes --namespace reputation` → filters to specific namespace
- [ ] `policy audit --since 1h --subject character:01ABC` → queries audit log with filters
- [ ] All tests pass via `task test`

**Files:**

- Modify: `internal/command/handlers/policy.go`
- Test: `internal/command/handlers/policy_test.go`

**Step 1: Write failing tests**

- `policy test <subject> <action> <resource>` → returns decision, matched policies
- `policy test --verbose` → shows all candidate policies with why each did/didn't match
- `policy validate <dsl>` → success or error with line/column
- `policy reload` → forces cache reload from DB
- `policy attributes` → lists all registered attribute namespaces and keys
- `policy attributes --namespace reputation` → filters to specific namespace
- `policy audit --since 1h --subject character:01ABC` → queries audit log with filters

**Step 2: Implement**

**Step 3: Run tests, commit**

```bash
git add internal/command/handlers/policy.go internal/command/handlers/policy_test.go
git commit -m "feat(command): add policy test/validate/reload/attributes/audit commands"
```

---

## Phase 7.6: Call Site Migration & Cleanup

### Task 28: Replace AccessControl with AccessPolicyEngine in dependency injection

**Spec References:** §14 (Call Site Migration, lines 3175-3236), ADR 0014 (Direct replacement, no adapter)

**Acceptance Criteria:**

- [ ] `AccessControl` replaced with `*policy.Engine` in dependency graph
- [ ] `AttributeResolver` wired with all registered providers
- [ ] `PolicyCache` wired and `Listen()` called for NOTIFY subscription
- [ ] `SessionResolver` wired
- [ ] `AuditLogger` wired
- [ ] `Bootstrap()` called at startup to seed policies
- [ ] Build compiles (compilation errors at call sites expected — fixed in Task 29)

**Files:**

- Modify: `cmd/holomush/main.go` (or server bootstrap file)
- Modify: DI/wiring to provide `Engine` (implements `AccessPolicyEngine`) instead of `AccessControl`

**Step 1: Update DI wiring**

Replace `AccessControl` in the dependency graph with `*policy.Engine`. Wire in all dependencies: `AttributeResolver` (with registered providers), `PolicyCache`, `SessionResolver`, `AuditLogger`.

Add `Bootstrap()` call at startup to seed policies.
Add `PolicyCache.Listen()` for NOTIFY subscription.

**Step 2: Run build to identify compilation errors**

Run: `task build`
Expected: Compilation errors at call sites (fixed in Task 29)

**Step 3: Commit**

```bash
git commit -m "refactor(access): wire AccessPolicyEngine in dependency injection"
```

---

### Task 29: Update all call sites (iterative)

**Spec References:** §14.1 (Call Site Inventory, lines 3190-3236), ADR 0014 (Direct replacement, no adapter)

**Acceptance Criteria:**

- [ ] ALL ~28 production call sites (plus test files and generated mocks) migrated from `AccessControl.Check()` to `engine.Evaluate()`
- [ ] Each call site uses `policy.AccessRequest{Subject, Action, Resource}` struct
- [ ] Error handling: `Evaluate()` error → fail-closed (deny), logged via slog
- [ ] All subject strings use `character:` prefix (not legacy `char:`)
- [ ] Tests updated to mock `AccessPolicyEngine` instead of `AccessControl`
- [ ] Tests pass incrementally after each file/package update
- [ ] Committed per package (dispatcher, world, plugin)
- [ ] `task test` passes after all migrations

**Key files include (non-exhaustive)** — run `grep -r "AccessControl" internal/ --include="*.go" -l` for the authoritative list:

- `internal/command/dispatcher.go` — Command execution authorization
- `internal/command/rate_limit_middleware.go` — Rate limit bypass for admins
- `internal/command/handlers/boot.go` — Boot command permission check
- `internal/world/service.go` — World model operation authorization
- `internal/plugin/hostfunc/commands.go` — Plugin command execution auth
- `internal/plugin/hostfunc/functions.go` — Plugin host function auth
- `internal/core/broadcaster_test.go` — Test mock injection

For each file:

1. Change `AccessControl` parameter type to `AccessPolicyEngine`
2. Replace `ac.Check(ctx, subject, action, resource)` with:

   ```go
   decision, err := engine.Evaluate(ctx, policy.AccessRequest{
       Subject:  subject,
       Action:   action,
       Resource: resource,
   })
   if err != nil {
       slog.Error("access evaluation failed", "error", err)
       // Fail-closed: deny on error
   }
   if !decision.Allowed {
       // existing denial handling
   }
   ```

3. Update tests to mock `AccessPolicyEngine` instead of `AccessControl`
4. Ensure all subject strings use `character:` prefix (not legacy `char:`)

**Step 1: Update each file or package group**

**Step 2: Run tests after each update**

Run: `task test`
Expected: PASS incrementally

**Step 3: Commit per package**

```bash
git commit -m "refactor(command): migrate dispatcher to AccessPolicyEngine"
git commit -m "refactor(world): migrate WorldService to AccessPolicyEngine"
git commit -m "refactor(plugin): migrate host functions to AccessPolicyEngine"
```

---

### Task 30: Remove StaticAccessControl, AccessControl interface, and capability.Enforcer

**Spec References:** §14.2 (Cleanup), ADR 0014 (Direct replacement, no adapter)

**Acceptance Criteria:**

- [ ] `internal/access/static.go` and `static_test.go` deleted
- [ ] `internal/access/permissions.go` and `permissions_test.go` deleted (if static-only)
- [ ] `AccessControl` interface removed from `access.go`
- [ ] `capability.Enforcer` removed (capabilities now seed policies)
- [ ] Zero references to `AccessControl` in codebase (`grep` clean)
- [ ] Zero references to `StaticAccessControl` in codebase
- [ ] Zero `char:` prefix usage (all migrated to `character:`)
- [ ] Zero `@`-prefixed command names
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
- Search and remove: all `char:` prefix usage (replace with `character:`)
- Search and remove: all `@`-prefixed command name handling
- Run: `mockery` to regenerate mocks for new `AccessPolicyEngine` interface

**Step 1: Delete static access control files**

**Step 2: Remove AccessControl interface from access.go**

Keep `ParseSubject()` or migrate it to `policy.ParseEntityRef()`. Keep any utility functions still referenced.

**Step 3: Remove capability.Enforcer**

Plugin manifests are now handled by seed policies. Remove enforcer and all references.

**Step 4: Remove legacy prefixes**

- `char:` → `character:` (search all `.go` files)
- `@dig` → `dig` (command name without `@` prefix)

**Step 5: Run tests**

Run: `task test`
Expected: PASS

**Step 6: Commit**

```bash
git add -A
git commit -m "refactor(access): remove StaticAccessControl, AccessControl interface, and capability.Enforcer"
```

---

## Phase 7.7: Integration Tests

### Task 31: Integration tests for full ABAC flow

**Spec References:** §15 (Integration Test Requirements), ADR 0011 (Deny-overrides), ADR 0013 (Properties)

**Acceptance Criteria:**

- [ ] Ginkgo/Gomega BDD-style tests with `//go:build integration` tag
- [ ] testcontainers for PostgreSQL (pattern from `test/integration/world/`)
- [ ] Seed policy behavior: self-access, location read, co-location, admin full access, deny-overrides, default deny
- [ ] Property visibility: public co-located, private owner-only, admin-only, restricted with visible\_to
- [ ] Cache invalidation: NOTIFY after create, NOTIFY after delete → cache reloads
- [ ] Audit logging: denials\_only mode, all mode, off mode
- [ ] Lock system: apply lock → forbid, remove lock → allow
- [ ] All integration tests pass: `go test -race -v -tags=integration ./test/integration/access/...`

**Files:**

- Create: `test/integration/access/access_suite_test.go`
- Create: `test/integration/access/evaluation_test.go`
- Create: `test/integration/access/seed_policies_test.go`
- Create: `test/integration/access/property_visibility_test.go`

**Step 1: Write Ginkgo/Gomega integration tests**

Use testcontainers for PostgreSQL (pattern from `test/integration/world/world_suite_test.go`):

```go
//go:build integration

var _ = Describe("Access Policy Engine", func() {
    Describe("Seed policy behavior", func() {
        It("allows players to read their own character", func() { })
        It("allows players to read their current location", func() { })
        It("denies players reading other locations", func() { })
        It("allows admins full access", func() { })
        It("denies when forbid and permit both match (deny-overrides)", func() { })
        It("denies when no policy matches (default deny)", func() { })
    })

    Describe("Property visibility", func() {
        It("allows public property read by co-located character", func() { })
        It("denies public property read by distant character", func() { })
        It("allows private property read by owner only", func() { })
        It("allows admin property read by admin only", func() { })
        It("handles restricted visibility with visible_to list", func() { })
    })

    Describe("Cache invalidation", func() {
        It("reloads policies when NOTIFY fires after create", func() { })
        It("reloads policies when NOTIFY fires after delete", func() { })
    })

    Describe("Audit logging", func() {
        It("logs denials in denials_only mode", func() { })
        It("logs everything in all mode", func() { })
        It("skips allows in off mode", func() { })
    })

    Describe("Lock system", func() {
        It("applies lock to resource via forbid policy", func() { })
        It("removes lock via unlock command", func() { })
    })
})
```

**Step 2: Run integration tests**

Run: `go test -race -v -tags=integration ./test/integration/access/...`
Expected: PASS

**Step 3: Commit**

```bash
git add test/integration/access/
git commit -m "test(access): add ABAC integration tests with seed policies and property visibility"
```

---

## Post-Implementation Checklist

- [ ] All unit tests pass: `task test`
- [ ] All integration tests pass: `go test -tags=integration ./test/integration/...`
- [ ] All linters pass: `task lint`
- [ ] Fuzz tests run 30s without panics: `go test -fuzz=FuzzParse -fuzztime=30s ./internal/access/policy/dsl/`
- [ ] Benchmarks within spec targets
- [ ] No references to `AccessControl` interface remain
- [ ] No references to `StaticAccessControl` remain
- [ ] No references to `capability.Enforcer` remain
- [ ] No `char:` prefix usage remains (all migrated to `character:`)
- [ ] No `@`-prefixed command names remain
- [ ] All seed policies compile and pass integration tests
- [ ] Audit logging works in all three modes
- [ ] `policy test` command matches actual `Evaluate()` results
- [ ] Metrics exported correctly on `/metrics` endpoint
- [ ] Code coverage >80% per package
