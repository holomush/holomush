# Full ABAC Architecture Design

**Status:** Draft
**Date:** 2026-02-05
**Epic:** holomush-5k1 (Epic 7: Full ABAC)
**Task:** holomush-5k1.1

## Overview

This document defines the full Attribute-Based Access Control (ABAC) architecture
for HoloMUSH, replacing the static role-based system from Epic 3 with a
policy-driven engine. Game administrators define policies using a Cedar-inspired
DSL that references dynamic attributes of subjects, resources, and the environment.
Players control access to their own properties through a simplified lock system.

### Goals

- Dynamic, admin-editable authorization policies stored in PostgreSQL
- Cedar-inspired DSL with rich expression language (comparisons, set operations,
  hierarchy traversal, if-then-else)
- Extensible attribute system with plugin contributions via registration-based
  providers
- Properties as first-class entities with per-property access control
- Player-authored locks for owned resources (simplified policy syntax)
- In-game admin commands for policy CRUD and debugging (`policy` command set)
- Full audit trail of access decisions in a dedicated PostgreSQL table
- Backward-compatible migration from `AccessControl` to `AccessPolicyEngine`
  (~28 production call sites)
- Default-deny posture with deny-overrides conflict resolution

### Non-Goals

- Graph-based relationship traversal (OpenFGA/Zanzibar-style) — relationships
  are modeled as attributes
- Priority-based policy ordering — deny always wins, no escalation
- Real-time policy synchronization across multiple server instances
  (single-server for now)
- Web-based policy editor (admin commands cover MVP, web UI deferred)
- Database triggers or stored procedures — all logic lives in Go

### Key Design Decisions

| Decision              | Choice                                  | Rationale                                                         |
| --------------------- | --------------------------------------- | ----------------------------------------------------------------- |
| Engine                | Custom Go-native ABAC                   | Full control, no impedance mismatch, tight plugin integration     |
| Policy language       | Cedar-inspired DSL                      | Readable by game admins, expressive, well-documented formal model |
| Attribute resolution  | Eager (collect-then-evaluate)           | Simple, predictable, better audit story                           |
| Conflict resolution   | Deny-overrides, no priority             | Simple mental model, Cedar-proven approach                        |
| Property model        | First-class entities                    | Conceptual uniformity — everything is an entity                   |
| Plugin attributes     | Registration-based providers            | Synchronous, consistent with eager resolution                     |
| Audit logging         | Separate PostgreSQL table               | Clean separation from game events, independent retention          |
| Migration             | New interface + adapter                 | Incremental caller migration, no big-bang change                  |
| Cache invalidation    | PostgreSQL LISTEN/NOTIFY (in Go code)   | Push-based, no polling overhead                                   |
| Player access control | Layered: metadata + locks + full policy | Progressive complexity for different user roles                   |

## Architecture

```text
┌──────────────────────────────────────────────────────────────────────┐
│                        AccessPolicyEngine                            │
│                                                                      │
│   Evaluate(ctx, AccessRequest) (Decision, error)                    │
│                                                                      │
├──────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   ┌─────────────────┐  ┌──────────────────┐  ┌─────────────────┐   │
│   │ Policy Store     │  │ Attribute        │  │ Audit Logger    │   │
│   │ (PostgreSQL)     │  │ Resolver         │  │ (PostgreSQL)    │   │
│   │                  │  │                  │  │                 │   │
│   │ - CRUD policies  │  │ - Core providers │  │ - Log denials   │   │
│   │ - Version history│  │ - Plugin provs   │  │ - Optional      │   │
│   │ - DSL text +     │  │ - Environment    │  │   allow logging │   │
│   │   parsed form    │  │                  │  │ - Attr snapshot  │   │
│   └─────────────────┘  └──────┬───────────┘  └─────────────────┘   │
│                               │                                      │
│                    ┌──────────┴───────────┐                          │
│                    │  Attribute Providers  │                          │
│                    ├──────────────────────┤                          │
│                    │ CharacterProvider     │ ← World model            │
│                    │ LocationProvider      │ ← World model            │
│                    │ PropertyProvider      │ ← Property store         │
│                    │ SessionProvider       │ ← Session store          │
│                    │ EnvironmentProvider   │ ← Clock, game state      │
│                    │ PluginProvider(s)     │ ← Registered by plugins  │
│                    └──────────────────────┘                          │
│                                                                      │
├──────────────────────────────────────────────────────────────────────┤
│   ┌──────────────────────────────────────┐                           │
│   │ accessControlAdapter                  │                           │
│   │ Wraps AccessPolicyEngine → old        │                           │
│   │ AccessControl interface               │                           │
│   │ (for incremental migration)           │                           │
│   └──────────────────────────────────────┘                           │
└──────────────────────────────────────────────────────────────────────┘
```

### Request Flow

1. Caller invokes `Evaluate(ctx, AccessRequest)` (or `Check()` via adapter)
2. Engine resolves all attributes eagerly — calls core providers + registered
   plugin providers
3. Engine loads matching policies from the in-memory cache
4. Engine evaluates each policy's conditions against the attribute bags
5. Deny-overrides: any deny → deny, any allow → allow, default → deny
6. Audit logger records the decision, matched policies, and attribute snapshot
7. Returns `Decision` with allowed/denied, reason, and matched policy ID

### Package Structure

```text
internal/access/               # Existing — keeps AccessControl interface + adapter
internal/access/policy/        # NEW — AccessPolicyEngine, evaluation
  engine.go                    # AccessPolicyEngine implementation
  policy.go                    # Policy type, parsing, validation
  dsl/                         # DSL parser and AST
    parser.go
    ast.go
    evaluator.go
  attribute/                   # Attribute resolution
    resolver.go                # AttributeResolver orchestrates providers
    provider.go                # AttributeProvider interface
    character.go               # Core: character attributes
    location.go                # Core: location attributes
    property.go                # Core: property attributes
    environment.go             # Core: env attributes (time, game state)
  store/                       # Policy persistence
    postgres.go                # Policy CRUD + versioning + LISTEN/NOTIFY
  audit/                       # Audit logging
    logger.go                  # Audit decision logger
    postgres.go                # Audit table writes
```

## Core Interfaces

### AccessPolicyEngine

```go
// AccessPolicyEngine is the main entry point for policy-based authorization.
type AccessPolicyEngine interface {
    Evaluate(ctx context.Context, request AccessRequest) (Decision, error)
}
```

### AccessRequest

```go
// AccessRequest contains all information needed for an access decision.
type AccessRequest struct {
    Subject  string // "character:01ABC", "plugin:echo-bot", "system"
    Action   string // "read", "write", "enter", "execute"
    Resource string // "location:01XYZ", "command:dig", "property:01DEF"
}
```

The engine parses the prefixed string format to extract type and ID. The prefix
mapping is:

| Prefix       | DSL Type    | Example                 |
| ------------ | ----------- | ----------------------- |
| `character:` | `character` | `character:01ABC`       |
| `plugin:`    | `plugin`    | `plugin:echo-bot`       |
| `system`     | (bypass)    | `system` (no ID)        |
| `session:`   | `session`   | `session:web-123`       |
| `location:`  | `location`  | `location:01XYZ`        |
| `object:`    | `object`    | `object:01DEF`          |
| `command:`   | `command`   | `command:say`           |
| `property:`  | `property`  | `property:01GHI`        |
| `stream:`    | `stream`    | `stream:location:01XYZ` |

**Subject prefix constants:** The `access` package SHOULD define these prefixes
as constants (e.g., `SubjectCharacter = "character:"`, `SubjectPlugin =
"plugin:"`) to prevent typos and enable compile-time references. The existing
`ParseSubject()` function already handles prefix parsing. The static system uses
`char:` as a legacy abbreviation; the adapter MUST accept both `char:` and
`character:` during migration, normalizing to `character:` internally.

**Design note:** Subject and resource use flat prefixed strings rather than typed
structs to simplify serialization for audit logging and cross-process
communication. Parsing overhead is negligible at <200 concurrent users. If
profiling shows parsing as a bottleneck, introduce a cached parsed
representation.

### Session Subject Resolution

Session subjects (`session:web-123`) are resolved to their associated character
at the engine entry point, BEFORE attribute resolution:

1. Engine receives `AccessRequest{Subject: "session:web-123", ...}`
2. Engine queries the session store for the session's character ID
3. Engine rewrites the request as `AccessRequest{Subject: "character:01ABC", ...}`
4. Attribute resolution proceeds using the character subject

This ensures policies are always evaluated as `principal is character`, never
`principal is session`. The `SessionProvider` in the architecture diagram exists
only to perform this lookup — it does not contribute attributes to the bags.

### Decision

```go
// Decision represents the outcome of a policy evaluation.
// Invariant: Allowed is true if and only if Effect == EffectAllow.
type Decision struct {
    Allowed    bool
    Effect     Effect          // Allow, Deny, or DefaultDeny (no policy matched)
    Reason     string          // Human-readable explanation
    PolicyID   string          // ID of the determining policy ("" if default deny)
    Policies   []PolicyMatch   // All policies that matched (for debugging)
    Attributes *AttributeBags  // Snapshot of all resolved attributes
}

// Effect represents the type of decision.
type Effect int

const (
    EffectDefaultDeny Effect = iota // No policy matched
    EffectAllow
    EffectDeny
)

// PolicyMatch records a single policy's evaluation result.
type PolicyMatch struct {
    PolicyID       string
    PolicyName     string
    Effect         Effect
    ConditionsMet  bool
}
```

### AttributeBags

```go
// AttributeBags holds the resolved attributes for a request.
type AttributeBags struct {
    Subject     map[string]any // e.g., {"type": "character", "faction": "rebels", "level": 7}
    Resource    map[string]any // e.g., {"type": "location", "faction": "rebels", "restricted": true}
    Action      map[string]any // e.g., {"name": "enter"}
    Environment map[string]any // e.g., {"time": "2026-02-05T14:30:00Z"}
}
```

### Attribute Providers

```go
// AttributeProvider resolves attributes for a specific namespace.
type AttributeProvider interface {
    Namespace() string
    ResolveSubject(ctx context.Context, subjectType, subjectID string) (map[string]any, error)
    ResolveResource(ctx context.Context, resourceType, resourceID string) (map[string]any, error)
}

// EnvironmentProvider resolves environment-level attributes (no entity context).
type EnvironmentProvider interface {
    Namespace() string
    Resolve(ctx context.Context) (map[string]any, error)
}
```

### Core Attribute Schema

Each attribute provider contributes a defined set of attributes. Attributes
marked MUST always exist when the entity is valid; MAY attributes may be nil.

**CharacterProvider** (`character` namespace):

| Attribute  | Type     | Requirement | Description                                |
| ---------- | -------- | ----------- | ------------------------------------------ |
| `type`     | string   | MUST        | Always `"character"`                       |
| `id`       | string   | MUST        | ULID of the character                      |
| `name`     | string   | MUST        | Character display name                     |
| `role`     | string   | MUST        | One of: `"player"`, `"builder"`, `"admin"` |
| `faction`  | string   | MAY         | Faction affiliation (nil if unaffiliated)  |
| `level`    | int      | MUST        | Character level (>= 0)                     |
| `flags`    | []string | MUST        | Arbitrary flags (empty array if none)      |
| `location` | string   | MUST        | ULID of current location                   |

**LocationProvider** (`location` namespace):

| Attribute    | Type   | Requirement | Description                               |
| ------------ | ------ | ----------- | ----------------------------------------- |
| `type`       | string | MUST        | Always `"location"`                       |
| `id`         | string | MUST        | ULID of the location                      |
| `name`       | string | MUST        | Location display name                     |
| `faction`    | string | MAY         | Faction that controls this location       |
| `restricted` | bool   | MUST        | Whether entry requires special permission |

**PropertyProvider** (`property` namespace):

| Attribute       | Type     | Requirement | Description                               |
| --------------- | -------- | ----------- | ----------------------------------------- |
| `type`          | string   | MUST        | Always `"property"`                       |
| `id`            | string   | MUST        | ULID of the property                      |
| `name`          | string   | MUST        | Property name                             |
| `parent_type`   | string   | MUST        | Parent entity type                        |
| `parent_id`     | string   | MUST        | Parent entity ULID                        |
| `owner`         | string   | MAY         | Subject who created/set this property     |
| `visibility`    | string   | MUST        | One of: public, private, restricted, etc. |
| `flags`         | []string | MUST        | Arbitrary flags (empty array if none)     |
| `visible_to`    | []string | MAY         | Character IDs (only when restricted)      |
| `excluded_from` | []string | MAY         | Character IDs (only when restricted)      |

**EnvironmentProvider** (`env` namespace):

| Attribute     | Type   | Requirement | Description                           |
| ------------- | ------ | ----------- | ------------------------------------- |
| `time`        | string | MUST        | Current time (RFC 3339)               |
| `maintenance` | bool   | MUST        | Whether server is in maintenance mode |

**Action bag** (constructed by the engine, not a provider):

| Attribute | Type   | Requirement | Description                        |
| --------- | ------ | ----------- | ---------------------------------- |
| `name`    | string | MUST        | The action string from the request |

The engine constructs `AttributeBags.Action` directly from the `AccessRequest`
— no provider is needed. `action.name` equals the `Action` field of the request.

**Plugin providers** contribute attributes under their own namespace (e.g.,
`reputation.score`). Plugin attributes are always MAY — the engine tolerates
their absence.

### Backward-Compatible Adapter

```go
// accessControlAdapter bridges AccessPolicyEngine to the legacy AccessControl interface.
// Callers that need richer error handling SHOULD migrate to AccessPolicyEngine directly.
type accessControlAdapter struct {
    engine AccessPolicyEngine
    logger *slog.Logger
}

func (a *accessControlAdapter) Check(ctx context.Context, subject, action, resource string) bool {
    decision, err := a.engine.Evaluate(ctx, AccessRequest{
        Subject: subject, Action: action, Resource: resource,
    })
    if err != nil {
        a.logger.Error("access policy engine error", "error", err,
            "subject", subject, "action", action, "resource", resource)
        return false // fail-closed
    }
    return decision.Allowed
}
```

## Policy DSL

The DSL is Cedar-inspired with a full expression language. Policies have a
**target** (what they apply to) and optional **conditions** (when they apply).

### Grammar

```text
policy     = effect "(" target ")" [ "when" "{" conditions "}" ] ";"
effect     = "permit" | "forbid"
target     = principal_clause "," action_clause "," resource_clause
principal_clause = "principal" [ "is" type_name ]
action_clause    = "action" [ "in" list ]
resource_clause  = "resource" [ "is" type_name ]

conditions   = disjunction
disjunction  = conjunction { "||" conjunction }
conjunction  = condition { "&&" condition }
condition    = expr comparator expr
             | expr "like" string_literal
             | expr "in" list
             | expr "in" expr
             | expr "." "containsAll" "(" list ")"
             | expr "." "containsAny" "(" list ")"
             | expr "has" identifier
             | "!" condition
             | "(" conditions ")"
             | "if" condition "then" condition "else" condition

expr       = attribute_ref | literal
attribute_ref = ("principal" | "resource" | "action" | "env") "." identifier { "." identifier }
literal    = string_literal | number | boolean
list       = "[" literal { "," literal } "]"
comparator = "==" | "!=" | ">" | ">=" | "<" | "<="
type_name  = identifier

(* Terminals *)
identifier     = letter { letter | digit | "_" | "-" }
string_literal = '"' { character } '"'
number         = [ "-" ] digit { digit } [ "." digit { digit } ]
boolean        = "true" | "false"

(* Whitespace, including newlines, is insignificant within policy text.
   The `policy create` command collects multi-line input until "." on a
   line by itself. *)
```

**Grammar notes:**

- `&&` binds tighter than `||` (conjunction before disjunction), matching
  standard boolean logic and Cedar semantics.
- `like` uses glob syntax (consistent with `gobwas/glob` already in the
  project), NOT SQL `LIKE` semantics. Wildcards: `*` matches any sequence,
  `?` matches one character. The glob is compiled with `:` as separator
  (`glob.Compile(pattern, ':')`) for consistency with the existing
  `StaticAccessControl` permission matching.
- `action` is a valid attribute root in conditions, providing access to the
  `AttributeBags.Action` map (e.g., `action.name`). Action matching in the
  target clause covers most use cases, but conditions MAY reference action
  attributes when needed.
- `expr "in" expr` supports membership checks against attribute values that
  are arrays (e.g., `principal.id in resource.visible_to`). The right-hand side
  MUST resolve to a `[]string` or `[]any` attribute. This is distinct from
  `expr "in" list` where the list is a literal.
- **Deferred: entity references.** Cedar defines `entity_ref` syntax
  (`Type::"value"`) for hierarchy membership checks (e.g.,
  `principal in Group::"admins"`). This is NOT included in the initial grammar.
  The parser MUST reject `Type::"value"` syntax with a clear error message
  directing admins to use attribute-based group checks
  (`principal.flags.containsAny(["admin"])`) instead. Entity references MAY be
  added in a future phase when group/hierarchy features are implemented.

### Supported Operators

| Operator       | Types            | Example                                                      |
| -------------- | ---------------- | ------------------------------------------------------------ |
| `==`, `!=`     | Any              | `principal.faction == resource.faction`                      |
| `>`, `>=`      | Numbers          | `principal.level >= 5`                                       |
| `<`, `<=`      | Numbers          | `principal.level < 10`                                       |
| `in` (list)    | Value in list    | `action in ["read", "write"]`                                |
| `in` (expr)    | Value in attr    | `principal.id in resource.visible_to`                        |
| `has`          | Attribute exists | `principal has faction`                                      |
| `containsAll`  | Set: all present | `principal.flags.containsAll(["approved", "active"])`        |
| `containsAny`  | Set: any present | `principal.flags.containsAny(["admin", "builder"])`          |
| `if-then-else` | Conditional      | `if resource.restricted then principal.level >= 5 else true` |
| `like`         | Glob match       | `resource.name like "faction-hq-*"`                          |
| `&&`           | Boolean AND      | Conditions joined with AND                                   |
| `\|\|`         | Boolean OR       | Grouped with parentheses                                     |
| `!`            | Boolean NOT      | `!(principal.banned == true)`                                |

### Example Policies

```text
// Players can read their own character
permit(principal is character, action in ["read"], resource is character)
when { principal.id == resource.id };

// Characters can enter locations matching their faction
permit(principal is character, action in ["enter"], resource is location)
when { principal.faction == resource.faction };

// Block entry to restricted locations for characters under level 5
forbid(principal is character, action in ["enter"], resource is location)
when { resource.restricted == true && principal.level < 5 };

// Admins can do anything
permit(principal is character, action, resource)
when { principal.role == "admin" };

// Block all access during maintenance
forbid(principal, action, resource)
when { env.maintenance == true };

// Healers can view wound properties on any character
permit(principal is character, action in ["read"], resource is property)
when {
    resource.name == "wounds"
    && principal.flags.containsAny(["healer"])
};

// Characters can read their own properties except system/admin ones
permit(principal is character, action in ["read"], resource is property)
when { resource.parent_type == "character" && resource.parent_id == principal.id };

forbid(principal is character, action in ["read"], resource is property)
when { resource.visibility in ["system", "admin"] && principal.role != "admin" };

// Properties with visible_to lists: only listed characters can read
permit(principal is character, action in ["read"], resource is property)
when {
    resource has visible_to
    && principal.id in resource.visible_to
};

// Exclude specific characters from seeing a property
forbid(principal is character, action in ["read"], resource is property)
when {
    resource has excluded_from
    && principal.id in resource.excluded_from
};

// Plugin: echo-bot can emit to location streams
permit(principal is plugin, action in ["emit"], resource is stream)
when { principal.name == "echo-bot" && resource.name like "location:*" };
```

## Property Model

Properties are first-class entities with their own identity, ownership, and
access control attributes. This provides conceptual uniformity — characters,
locations, objects, and properties are all entities that the policy engine
evaluates against using the same interface.

**Package ownership:** Properties are world model entities managed by
`internal/world/PropertyRepository`, consistent with `LocationRepository` and
`ObjectRepository`. The `entity_properties` table is part of the world schema.
The `PropertyProvider` (in `internal/access/policy/attribute/`) wraps
`PropertyRepository` to resolve property attributes for policy evaluation.

### Property Attributes

| Attribute       | Type     | Description                                              |
| --------------- | -------- | -------------------------------------------------------- |
| `id`            | ULID     | Unique property identifier                               |
| `parent_type`   | string   | Parent entity type: character, location, object          |
| `parent_id`     | ULID     | Parent entity ID                                         |
| `name`          | string   | Property name (unique per parent)                        |
| `value`         | string   | Property value                                           |
| `owner`         | string   | Subject who created/set this property                    |
| `visibility`    | string   | Access level: public, private, restricted, system, admin |
| `flags`         | []string | Arbitrary flags (JSON array)                             |
| `visible_to`    | []string | Character IDs allowed to read (restricted visibility)    |
| `excluded_from` | []string | Character IDs denied from reading                        |

### Visibility Levels

| Visibility   | Who can see?        | visible_to/excluded_from |
| ------------ | ------------------- | ------------------------ |
| `public`     | Anyone in same room | Not applicable (NULL)    |
| `private`    | Owner only          | Not applicable (NULL)    |
| `restricted` | Explicit list       | Defaults: [self], []     |
| `system`     | System only         | Not applicable (NULL)    |
| `admin`      | Admins only         | Not applicable (NULL)    |

When visibility is set to `restricted`, the Go property store MUST auto-populate
`visible_to` with `[parent_id]` and `excluded_from` with `[]` if they are nil.
This prevents the "nobody can see it" footgun.

## Attribute Resolution

The engine uses eager resolution: all attributes are collected before any policy
is evaluated. This provides a complete attribute snapshot for every decision,
which powers audit logging and the `policy test` debugging command.

### Resolution Flow

```text
Evaluate(ctx, AccessRequest{Subject: "character:01ABC", Action: "enter", Resource: "location:01XYZ"})

1. Parse subject → type="character", id="01ABC"
2. Parse resource → type="location", id="01XYZ"
3. Resolve subject attributes:
   CharacterProvider.ResolveSubject("character", "01ABC")
     → {type: "character", id: "01ABC", faction: "rebels", level: 7, role: "player"}
   PluginProvider("reputation").ResolveSubject("character", "01ABC")
     → {reputation.score: 85}
4. Resolve resource attributes:
   LocationProvider.ResolveResource("location", "01XYZ")
     → {type: "location", id: "01XYZ", faction: "rebels", restricted: true}
5. Resolve environment attributes:
   EnvironmentProvider.Resolve()
     → {time: "2026-02-05T14:30:00Z", maintenance: false}
6. Assemble AttributeBags and proceed to policy evaluation
```

### Provider Registration

Plugins register attribute providers at startup. The engine calls all registered
providers during eager resolution. Provider namespaces MUST be unique to prevent
collisions.

```go
engine.RegisterAttributeProvider(reputationProvider)
engine.RegisterEnvironmentProvider(weatherProvider)
```

### Error Handling

**Core provider errors:** The engine returns the error alongside a default-deny
decision. Direct callers can distinguish "denied by policy" from "system
failure":

```go
return Decision{Allowed: false, Effect: EffectDefaultDeny}, err
```

The adapter translates this to `false` with an error log (fail-closed). The
audit log records the `error_message` field for these cases.

**Plugin provider errors:** The engine logs a warning via slog and continues
evaluation with the remaining providers. Missing plugin attributes cause
conditions referencing them to evaluate to `false` (fail-safe). The audit log
does NOT record plugin provider errors to prevent noise.

```text
slog.Warn("plugin attribute provider failed",
    "namespace", provider.Namespace(), "error", err)
```

## Evaluation Algorithm

```text
Evaluate(ctx, AccessRequest{Subject, Action, Resource})
│
├─ 1. System bypass
│    subject == "system" → return Decision{Allowed: true, Effect: Allow}
│
├─ 2. Resolve attributes (eager)
│    ├─ Parse subject type/ID from subject string
│    ├─ Parse resource type/ID from resource string
│    ├─ Call all registered AttributeProviders
│    └─ Assemble AttributeBags{Subject, Resource, Action, Environment}
│
├─ 3. Find applicable policies
│    ├─ Load from in-memory cache
│    └─ Filter: policy target matches request
│         ├─ principal type matches subject type (or unscoped)
│         ├─ action matches (or unscoped)
│         └─ resource type matches resource type (or unscoped)
│
├─ 4. Evaluate conditions
│    For each candidate policy:
│    ├─ Evaluate DSL conditions against AttributeBags
│    ├─ If all conditions true → policy is "satisfied"
│    └─ If any condition false or attribute missing → policy does not apply
│
├─ 5. Combine decisions (deny-overrides)
│    ├─ Any satisfied forbid → Decision{Allowed: false, Effect: Deny}
│    ├─ Any satisfied permit → Decision{Allowed: true, Effect: Allow}
│    └─ No policies satisfied → Decision{Allowed: false, Effect: DefaultDeny}
│
└─ 6. Audit
     ├─ Always log denials (forbid + default deny)
     ├─ Log allows when audit mode is enabled
     └─ Include: decision, matched policies, attribute snapshot
```

### Key Behaviors

- **Missing attributes:** If a condition references an attribute that does not
  exist, the condition evaluates to `false`. A missing attribute can never grant
  access (fail-safe).
- **No short-circuit:** The engine evaluates all candidate policies so the
  `Decision` records all matches. This powers `policy test` debugging.
- **Cache invalidation:** The engine subscribes to PostgreSQL LISTEN/NOTIFY on
  the `policy_changed` channel. The Go policy store calls `pg_notify` after any
  Create/Update/Delete operation. On notification, the engine reloads all enabled
  policies before the next evaluation. On reconnect after a connection drop, the
  engine MUST perform a full policy reload to account for any missed
  notifications.
- **Concurrency:** Policy evaluations use a snapshot of the in-memory policy
  cache at the start of `Evaluate()`. If a policy changes during evaluation, the
  decision reflects the pre-change policy. This is acceptable for MUSH workloads
  where the stale window is <100ms.

### Performance Targets

| Metric                   | Target | Notes                            |
| ------------------------ | ------ | -------------------------------- |
| `Evaluate()` p99 latency | <5ms   | At 200 concurrent users          |
| Attribute resolution     | <2ms   | All providers combined           |
| DSL condition evaluation | <1ms   | Per policy                       |
| Cache reload             | <50ms  | Full policy set reload on NOTIFY |

Implementation SHOULD add `slog.Debug()` timers in `engine.Evaluate()` for
attribute resolution, policy filtering, condition evaluation, and audit logging
to enable performance profiling.

### Future: Attribute Caching

If profiling shows attribute resolution as a bottleneck, introduce per-request
caching in `AttributeResolver`. The cache is scoped to a single `Evaluate()`
call and cleared afterward — no stale data risk across separate checks. This is
deferred until profiling demonstrates the need.

## Policy Storage

### Schema

All `id` columns use ULID format, consistent with project conventions.

```sql
CREATE TABLE access_policies (
    id          TEXT PRIMARY KEY,           -- ULID
    name        TEXT NOT NULL UNIQUE,
    description TEXT,
    effect      TEXT NOT NULL CHECK (effect IN ('permit', 'forbid')),
    dsl_text    TEXT NOT NULL,
    enabled     BOOLEAN NOT NULL DEFAULT true,
    created_by  TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    version     INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX idx_policies_enabled ON access_policies(enabled) WHERE enabled = true;

CREATE TABLE access_policy_versions (
    id          TEXT PRIMARY KEY,           -- ULID
    policy_id   TEXT NOT NULL REFERENCES access_policies(id) ON DELETE CASCADE,
    version     INTEGER NOT NULL,
    dsl_text    TEXT NOT NULL,
    changed_by  TEXT NOT NULL,
    changed_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    change_note TEXT,
    UNIQUE(policy_id, version)
);

CREATE TABLE access_audit_log (
    id            TEXT PRIMARY KEY,         -- ULID
    timestamp     TIMESTAMPTZ NOT NULL DEFAULT now(),
    subject       TEXT NOT NULL,
    action        TEXT NOT NULL,
    resource      TEXT NOT NULL,
    decision      TEXT NOT NULL CHECK (decision IN ('allowed', 'denied')),
    effect        TEXT NOT NULL CHECK (effect IN ('allow', 'deny', 'default_deny')),
    policy_id     TEXT,
    policy_name   TEXT,
    attributes    JSONB,
    error_message TEXT
);

CREATE INDEX idx_audit_log_timestamp ON access_audit_log(timestamp DESC);
CREATE INDEX idx_audit_log_subject ON access_audit_log(subject, timestamp DESC);
CREATE INDEX idx_audit_log_resource ON access_audit_log(resource, timestamp DESC);
CREATE INDEX idx_audit_log_decision ON access_audit_log(decision, timestamp DESC);

CREATE TABLE entity_properties (
    id            TEXT PRIMARY KEY,         -- ULID
    parent_type   TEXT NOT NULL,
    parent_id     TEXT NOT NULL,
    name          TEXT NOT NULL,
    value         TEXT,                     -- NULL permitted for flag-style properties (name-only)
    owner         TEXT,
    visibility    TEXT NOT NULL DEFAULT 'public'
                  CHECK (visibility IN ('public', 'private', 'restricted', 'system', 'admin')),
    flags         JSONB DEFAULT '[]',
    visible_to    JSONB DEFAULT NULL,
    excluded_from JSONB DEFAULT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(parent_type, parent_id, name)
);

CREATE INDEX idx_properties_parent ON entity_properties(parent_type, parent_id);
```

### Cache Invalidation

The Go policy store sends `pg_notify('policy_changed', policyID)` in the same
transaction as any policy CRUD operation. The engine subscribes to this channel
and reloads its in-memory policy cache on notification. No database triggers are
used — all notification logic lives in Go application code.

The engine uses `pgx.Conn.Listen()` which requires a dedicated persistent
connection outside the connection pool. On connection loss, the engine MUST:

1. Reconnect with exponential backoff
2. Re-subscribe to the `policy_changed` channel
3. Perform a full policy reload (missed notifications cannot be recovered)

### Audit Log Serialization

The `effect` column in `access_audit_log` maps to the Go `Effect` enum:

| Go Constant         | DB Value         |
| ------------------- | ---------------- |
| `EffectAllow`       | `"allow"`        |
| `EffectDeny`        | `"deny"`         |
| `EffectDefaultDeny` | `"default_deny"` |

The `decision` column is derived: `"allowed"` when `Effect == EffectAllow`,
`"denied"` otherwise.

### Visibility Defaults

When the Go property store sets visibility to `restricted`, it MUST populate
`visible_to` with `[parent_id]` and `excluded_from` with `[]` if they are nil.
When visibility is changed away from `restricted`, it MUST set both fields to
NULL. This logic lives in the Go store layer, not in database triggers.

## Access Control Layers

HoloMUSH provides three layers of access control, offering progressive
complexity for different user roles.

### Layer 1: Property Metadata (All Characters)

Characters set visibility and access lists on properties they own. No policy
authoring required — the character configures data that existing system-level
policies evaluate.

```text
> set me/secret_background.visibility = private
> set me/wounds.visibility = restricted
> set me/wounds.visible_to += character:01AAA
> set me/wounds.excluded_from += me
```

### Layer 2: Object Locks (Owners)

Owners can set conditions on their owned objects and properties using a
simplified lock syntax. Locks compile to scoped policies behind the scenes.

```text
> lock my-chest/read = faction:rebels
> lock me/backstory/read = me | flag:storyteller
> lock here/enter = level:>=5 & !flag:banned
> lock armory/enter = (faction:rebels | faction:alliance) & rank:>=3
> unlock my-chest/read
```

#### Lock Syntax

The lock expression language has two kinds of primitives: **core primitives**
built into the parser, and **token predicates** registered by attribute
providers.

**Core primitives** (built into the lock parser):

| Primitive    | Meaning                                         |
| ------------ | ----------------------------------------------- |
| `me`         | The owning character (compiles to owner's ULID) |
| Char name/ID | Specific character reference (resolved to ULID) |
| `&`          | AND                                             |
| `\|`         | OR                                              |
| `!`          | NOT                                             |
| `(` `)`      | Grouping for precedence                         |

Core primitives are game-agnostic. The parser resolves character names via the
world model at compile time, embedding the resolved ULID in the generated
policy. If a character name cannot be resolved, the `lock` command MUST return
an error.

**Operator precedence** (highest to lowest): `!`, `&`, `|`. Parentheses
override precedence. `a | b & !c` parses as `a | (b & (!c))`.

#### Lock Token Registry

All other lock vocabulary comes from **registered token predicates**. Each
attribute provider MAY register tokens that expose its attributes to the lock
syntax. Token registration defines how lock expressions compile to full DSL
conditions.

**Token types:**

| Type       | Lock Syntax  | Compiles To                 | Example Lock       |
| ---------- | ------------ | --------------------------- | ------------------ |
| equality   | `name:value` | `principal.attr == "value"` | `faction:rebels`   |
| membership | `name:value` | `"value" in principal.attr` | `flag:storyteller` |
| numeric    | `name:op N`  | `principal.attr op N`       | `level:>=5`        |

Numeric tokens support the full comparison set: `>=`, `>`, `<=`, `<`, `==`.
When no operator is specified (e.g., `level:5`), the default is `==`.

**Token registration interface:**

```go
// LockTokenDef defines how a lock token compiles to a DSL condition.
type LockTokenDef struct {
    // Name is the token identifier used in lock expressions (e.g., "faction").
    Name string

    // AttributePath is the full attribute reference in the DSL
    // (e.g., "principal.faction").
    AttributePath string

    // Type determines parsing and compilation behavior.
    Type LockTokenType // equality | membership | numeric
}

type LockTokenType int

const (
    LockTokenEquality   LockTokenType = iota // name:value → attr == "value"
    LockTokenMembership                       // name:value → "value" in attr
    LockTokenNumeric                          // name:opN  → attr op N
)
```

Providers register tokens at startup alongside their attributes:

```go
// AttributeProvider (extended) — providers optionally declare lock tokens.
type AttributeProvider interface {
    Namespace() string
    Resolve(ctx context.Context, entityID string) (map[string]any, error)
    LockTokens() []LockTokenDef // Returns empty slice if no lock tokens
}
```

**Core-shipped tokens** (registered by CharacterProvider, not hard-coded in
parser):

| Token     | Type       | Attribute Path      | Example            |
| --------- | ---------- | ------------------- | ------------------ |
| `faction` | equality   | `principal.faction` | `faction:rebels`   |
| `flag`    | membership | `principal.flags`   | `flag:storyteller` |
| `level`   | numeric    | `principal.level`   | `level:>=5`        |

These ship with the engine because CharacterProvider is a core provider, but
they are registered through the same mechanism as plugin tokens. The lock parser
has no special knowledge of `faction`, `flag`, or `level`.

**Plugin token examples:**

| Token       | Type       | Attribute Path     | Example               |
| ----------- | ---------- | ------------------ | --------------------- |
| `rep.score` | numeric    | `reputation.score` | `rep.score:>=50`      |
| `craft`     | equality   | `crafting.primary` | `craft:blacksmithing` |
| `guild`     | equality   | `guilds.primary`   | `guild:merchants`     |
| `cert`      | membership | `crafting.certs`   | `cert:master-smith`   |

#### Lock Compilation

The lock command compiles a lock expression into a full DSL policy. The
compilation process:

1. **Tokenize** the lock expression into core primitives and token references
2. **Resolve** each token reference against the token registry
3. **Validate** that all referenced tokens are registered (unknown token →
   error)
4. **Generate** a `permit` policy scoped to the specific resource and action
5. **Store** the policy via `PolicyStore.Create()`

Compilation example:

```text
Input:  lock my-chest/read = (faction:rebels | flag:ally) & level:>=3
Output:
  permit(
    principal,
    action == "read",
    resource == "object:01ABC..."
  ) when {
    (principal.faction == "rebels" || "ally" in principal.flags)
    && principal.level >= 3
  };
```

**Validation rules:**

- Unknown token names MUST produce a clear error: `unknown lock token "foo"
  — available tokens: faction, flag, level, rep.score, ...`
- Type mismatches MUST produce an error: `token "faction" expects a name, not a
  number`
- Numeric tokens with missing operators default to `==`
- Empty values MUST be rejected: `faction:` is invalid

#### Lock Token Discovery

Characters can discover available lock tokens via the `lock tokens` command:

```text
> lock tokens
Available lock tokens:
  faction:X     — Character faction equals X
  flag:X        — Character has flag X
  level:OP N    — Character level (>=, >, <=, <, == N)
  rep.score:OP N — Reputation score (plugin: reputation)
  guild:X       — Primary guild membership (plugin: guilds)
```

The `lock tokens` command reads from the token registry at runtime. Plugin
tokens appear automatically when the plugin is loaded.

**Ownership constraint:** Lock-generated policies can ONLY target resources the
character owns. The lock command MUST verify ownership before creating the
policy. A character can never write a lock that affects another player's
resources.

**Lock policy lifecycle:**

- Lock policies are NOT versioned. Each `lock`/`unlock` creates or deletes a
  policy directly — no version history is maintained.
- `lock X/action = condition` calls `PolicyStore.Create()` with generated DSL
  text and the naming convention `lock:{resource-type}:{resource-id}:{action}`.
- `unlock X/action` calls `PolicyStore.DeleteByName()` to remove the lock
  policy.
- Modifying a lock (re-running `lock X/action` with new conditions) deletes the
  existing lock policy and creates a new one in a single transaction.
- Ownership verification occurs in the lock command handler before any policy
  store operation.
- Token registry is consulted at compile time only — changing a plugin's
  registered tokens does not invalidate existing lock policies (they are already
  compiled to DSL).

### Layer 3: Full Policies (Admin Only)

The full DSL with unrestricted scope, managed via the `policy` command set.

### Layer Interaction

| Layer             | Who             | Scope          | Deny-overrides? |
| ----------------- | --------------- | -------------- | --------------- |
| Property metadata | All characters  | Own properties | Yes             |
| Object locks      | Resource owners | Own resources  | Yes             |
| Full policies     | Admins          | Everything     | Top authority   |

Admin `forbid` policies always trump player locks. Players operate within the
boundaries admins set.

## Admin Commands

### Policy Management

```text
policy list [--enabled|--disabled] [--effect=permit|forbid]
policy show <name>
policy create <name>
policy edit <name>
policy delete <name>
policy enable <name>
policy disable <name>
policy test <subject> <action> <resource> [--verbose]
policy history <name> [--limit=N]
policy audit [--subject=X] [--action=Y] [--decision=denied] [--last=1h]
```

### policy create

Accepts multiline DSL input terminated by `.` on a line by itself. Validates DSL
syntax before saving — rejects errors with line/column information.

```text
> policy create faction-hq-access
Enter policy (end with '.' on a line by itself):
permit(principal is character, action in ["enter", "look"], resource is location)
when { principal.faction == resource.faction && resource.restricted == true };
.
Policy 'faction-hq-access' created (version 1).
```

### policy test

Dry-run evaluation showing resolved attributes, matching policies, and the
final decision. Available to admins and builders (for debugging builds).

```text
> policy test character:01ABC enter location:01XYZ --verbose
Subject attributes:
  type=character, id=01ABC, faction=rebels, level=7, role=player
Resource attributes:
  type=location, id=01XYZ, faction=empire, restricted=true
Environment:
  time=2026-02-05T14:30:00Z, maintenance=false

Evaluating 3 matching policies:
  faction-hq-access    permit  CONDITIONS FAILED (rebels != empire)
  maintenance-lockout  forbid  CONDITIONS FAILED (maintenance=false)
  level-gate           forbid  CONDITIONS FAILED (level 7 >= 5)

Decision: DENIED (default deny — no policies matched)
```

### Command Permissions

```text
permit(principal is character, action in ["execute"], resource is command)
when { principal.role == "admin" && resource.name like "policy*" };

permit(principal is character, action in ["execute"], resource is command)
when { principal.role == "builder" && resource.name == "policy test" };
```

## Migration from Static Roles

### Seed Policies

The current `StaticAccessControl` permissions translate to seed policies. Two
naming corrections are applied during this translation:

- **Subject prefix:** The static system uses `char:` as an abbreviation; the
  ABAC system normalizes to `character:` for consistency with the resource prefix
  format. The adapter accepts both during migration.
- **Command names:** The static system uses legacy `@`-prefixed builder commands
  (`@dig`, `@create`, etc.) inherited from traditional MU\* conventions.
  HoloMUSH's command system (Epic 6) uses plain names without prefixes. The seed
  policies use the correct plain names.

**New actions:** The `enter` action is introduced by the ABAC system for
location entry control. The static system handles movement through
`write:character:$self` (changing the character's location). Shadow mode
validation MUST account for this semantic difference — `enter` checks are new
policy-only paths with no static-system equivalent.

```text
// player-powers: self access
permit(principal is character, action in ["read", "write"], resource is character)
when { resource.id == principal.id };

// player-powers: current location access
permit(principal is character, action in ["read"], resource is location)
when { resource.id == principal.location };

permit(principal is character, action in ["read"], resource is character)
when { resource.location == principal.location };

permit(principal is character, action in ["read"], resource is object)
when { resource.location == principal.location };

permit(principal is character, action in ["emit"], resource is stream)
when { resource.name like "location:*" && resource.location == principal.location };

// player-powers: basic commands
permit(principal is character, action in ["execute"], resource is command)
when { resource.name in ["say", "pose", "look", "go"] };

// builder-powers
permit(principal is character, action in ["write", "delete"], resource is location)
when { principal.role in ["builder", "admin"] };

permit(principal is character, action in ["write", "delete"], resource is object)
when { principal.role in ["builder", "admin"] };

permit(principal is character, action in ["execute"], resource is command)
when { principal.role in ["builder", "admin"]
    && resource.name in ["dig", "create", "describe", "link"] };

// admin-powers: full access
permit(principal is character, action, resource)
when { principal.role == "admin" };
```

### Migration Strategy

1. **Phase 7.1 (Policy Schema):** Create DB tables and policy store. Seed with
   the translated policies above. `StaticAccessControl` continues to serve all
   checks.

2. **Phase 7.3 (Policy Engine):** Build `AccessPolicyEngine` and wrap with
   adapter. Swap the adapter into dependency injection where
   `StaticAccessControl` was. Both implementations exist — the adapter now serves
   checks backed by seed policies.

3. **Shadow mode validation:** Run both engines in parallel during testing.
   Evaluate with both, log disagreements, fix policy gaps.

   **Cutover criteria:** Shadow mode runs in staging for at least 7 days with
   at least 10,000 authorization checks collected. 100% agreement means
   `Decision.Allowed` matches `StaticAccessControl.Check()` for all checks
   (excluding `enter` actions which have no static equivalent). Any disagreement
   blocks cutover and triggers immediate investigation. After meeting criteria,
   submit PR to remove `StaticAccessControl`. Rollback: revert the removal PR
   if post-cutover bugs are found.

4. **Caller migration (holomush-c6qch):** Callers incrementally migrate from
   `AccessControl.Check()` to `AccessPolicyEngine.Evaluate()` for richer error
   handling.

### Plugin Capability Migration

The current `capability.Enforcer` handles plugin permissions separately. Under
ABAC, plugin manifests become seed policies. The Enforcer becomes unnecessary
once all plugin capabilities are expressed as policies and can be removed
alongside `StaticAccessControl`.

## Testing Strategy

### Unit Tests

```text
internal/access/policy/dsl/
  parser_test.go        — Parse valid/invalid DSL, verify AST
  evaluator_test.go     — Evaluate conditions against attribute bags
                          Table-driven: each operator, edge cases,
                          missing attributes, type mismatches

internal/access/policy/
  engine_test.go        — Full evaluation flow with mock providers
                          System bypass, deny-overrides, default deny,
                          provider errors, cache invalidation

internal/access/policy/attribute/
  resolver_test.go      — Orchestrates multiple providers
  character_test.go     — Resolves character attrs from mock world service
  location_test.go      — Resolves location attrs from mock world service
  property_test.go      — Resolves property attrs including visibility/lists
  environment_test.go   — Time, maintenance mode, game state

internal/access/policy/store/
  postgres_test.go      — CRUD, versioning, LISTEN/NOTIFY dispatch

internal/access/policy/audit/
  logger_test.go        — Logs denials, optional allows, attribute snapshots

internal/access/
  adapter_test.go       — Adapter wraps engine, fail-closed on error
```

### DSL Evaluator Coverage

Table-driven tests MUST cover every operator with valid inputs, invalid inputs,
missing attributes, and type mismatches.

### Integration Tests (Ginkgo/Gomega)

```go
Describe("AccessPolicyEngine", func() {
    Describe("Policy evaluation with real PostgreSQL", func() {
        It("denies by default when no policies exist", func() { ... })
        It("allows when a permit policy matches", func() { ... })
        It("deny overrides permit", func() { ... })
        It("resolves character attributes from world model", func() { ... })
        It("handles property visibility with visible_to lists", func() { ... })
        It("plugin attribute providers contribute to evaluation", func() { ... })
    })

    Describe("Lock-generated policies", func() {
        It("creates a scoped policy from lock syntax", func() { ... })
        It("rejects locks on unowned resources", func() { ... })
        It("admin forbid overrides player lock permit", func() { ... })
    })

    Describe("Shadow mode migration", func() {
        It("StaticAccessControl and AccessPolicyEngine agree on seed policies", func() { ... })
    })

    Describe("Cache invalidation via LISTEN/NOTIFY", func() {
        It("reloads policies when notification received", func() { ... })
    })
})
```

### Shadow Mode Validation

During migration, a test harness runs both `StaticAccessControl` and
`AccessPolicyEngine` against the same requests and asserts identical results.
This confirms the seed policies faithfully reproduce the static role behavior.

## Acceptance Criteria

- [ ] ABAC policy data model documented (subjects, resources, actions, conditions)
- [ ] Attribute schema defined for subjects (players, plugins, connections)
- [ ] Attribute schema defined for resources (objects, rooms, commands, properties)
- [ ] Environment attributes defined (time, location, game state)
- [ ] Policy DSL grammar specified with full expression language
- [ ] Policy storage format designed (PostgreSQL schema with versioning)
- [ ] Policy evaluation algorithm documented (deny-overrides, no priority)
- [ ] Audit event schema defined for access decisions
- [ ] Plugin attribute contribution interface designed (registration-based)
- [ ] Admin commands documented for policy management
- [ ] Player lock system designed for owned resource access control
- [ ] Lock syntax compiles to scoped policies with ownership verification
- [ ] Property model designed as first-class entities
- [ ] Migration path documented from static permissions to full ABAC
- [ ] Shadow mode validates seed policies match static role behavior
- [ ] Cache invalidation via LISTEN/NOTIFY reloads policies on change
- [ ] System subject bypass returns allow without policy evaluation
- [ ] Subject type prefix-to-DSL-type mapping documented

## References

- [Core Access Control Design](2026-01-21-access-control-design.md) — Current
  static role implementation (Epic 3)
- [HoloMUSH Roadmap](../plans/2026-01-18-holomush-roadmap-design.md) — Epic 7
  definition
- [Cedar Language Specification](https://docs.cedarpolicy.com/) — DSL inspiration
- [Commands & Behaviors Design](2026-02-02-commands-behaviors-design.md) —
  Command system integration
