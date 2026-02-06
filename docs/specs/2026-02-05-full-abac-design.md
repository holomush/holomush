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
│   │   compiled form  │  │                  │  │ - Attr snapshot  │   │
│   └────────┬────────┘  └──────┬───────────┘  └─────────────────┘   │
│            │                  │                                      │
│   ┌────────┴────────┐        │                                      │
│   │ Policy Compiler  │        │                                      │
│   │ - Parse DSL → AST│        │                                      │
│   │ - Validate attrs │        │                                      │
│   │ - Compile globs  │        │                                      │
│   │ - Store compiled │        │                                      │
│   │   form (JSONB)   │        │                                      │
│   └─────────────────┘        │                                      │
│                    ┌──────────┴───────────┐                          │
│                    │  Attribute Providers  │                          │
│                    ├──────────────────────┤                          │
│                    │ CharacterProvider     │ ← World model            │
│                    │ LocationProvider      │ ← World model            │
│                    │ PropertyProvider      │ ← Property store         │
│                    │ ObjectProvider        │ ← World model            │
│                    │ StreamResolver        │ ← Derived from stream ID │
│                    │ CommandResolver       │ ← Command registry       │
│                    │ Session Resolver      │ ← Session store (not a   │
│                    │                       │   provider; see Session  │
│                    │                       │   Subject Resolution)    │
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
  compiler.go                  # PolicyCompiler: parse, validate, compile
  dsl/                         # DSL parser and AST
    parser.go
    ast.go
    evaluator.go
  attribute/                   # Attribute resolution
    resolver.go                # AttributeResolver orchestrates providers
    provider.go                # AttributeProvider interface
    character.go               # Core: character attributes
    location.go                # Core: location attributes
    object.go                  # Core: object attributes
    property.go                # Core: property attributes
    stream.go                  # Core: stream attributes (derived from ID)
    command.go                 # Core: command attributes
    environment.go             # Core: env attributes (time, game state)
  lock/                        # Lock expression system
    parser.go                  # Lock expression parser
    compiler.go                # Compiles lock AST to DSL policy text
    registry.go                # LockTokenRegistry
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

### PolicyCompiler

The `PolicyCompiler` is responsible for parsing DSL text into an AST,
validating attribute references, pre-compiling glob patterns, and producing a
compiled form suitable for caching and fast evaluation. The compiled form is
stored alongside the DSL text in the policy store (as JSONB).

```go
// PolicyCompiler parses and validates DSL policy text.
type PolicyCompiler interface {
    // Compile parses DSL text, validates it, and returns a compiled policy.
    // Returns a descriptive error with line/column information on parse failure.
    Compile(dslText string) (*CompiledPolicy, error)
}

// CompiledPolicy is the parsed, validated, and optimized form of a policy.
// The engine evaluates CompiledPolicy instances, never raw DSL text.
type CompiledPolicy struct {
    Effect     Effect
    Target     CompiledTarget     // Parsed principal/action/resource clauses
    Conditions []CompiledCondition // Pre-parsed AST nodes
    GlobCache  map[string]glob.Glob // Pre-compiled globs for `like` expressions
}
```

The policy store calls `Compile()` on every `Create` and `Edit` operation,
rejecting invalid DSL before persisting. The in-memory policy cache holds
`CompiledPolicy` instances — `Evaluate()` never re-parses DSL text, ensuring
the <5ms p99 latency target is achievable.

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
`principal is session`. The `Session Resolver` in the architecture diagram exists
only to perform this lookup — it is NOT an `AttributeProvider` and does not
contribute attributes to the bags.

**Session resolution error handling:**

- **Session not found:** Return `Decision{Allowed: false, Effect:
  EffectDefaultDeny}` with error `"session not found: web-123"`. The audit log
  records the error in the `error_message` field.
- **Session store failure:** Return `Decision{Allowed: false, Effect:
  EffectDefaultDeny}` with the wrapped store error. Fail-closed — a session
  lookup failure MUST NOT grant access.
- **No associated character:** Return `Decision{Allowed: false, Effect:
  EffectDefaultDeny}` with error `"session has no associated character"`. This
  handles unauthenticated sessions that have not yet selected a character.

Session resolution errors SHOULD use oops error codes for structured handling:
`SESSION_NOT_FOUND`, `SESSION_STORE_ERROR`, `SESSION_NO_CHARACTER`.

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

**Attribute namespace storage:** Plugin attributes use **flat dot-delimited
keys** in the attribute bags, not nested maps. For example, the reputation
plugin contributes `"reputation.score": 85` as a single key in the `Subject`
map. The DSL expression `principal.reputation.score` is parsed as an attribute
reference with path `["reputation", "score"]`, which the evaluator resolves by
looking up the flat key `"reputation.score"` in the subject bag. The `has`
operator checks top-level key existence only: `principal has faction` checks
whether `"faction"` exists in the subject bag. `principal has reputation.score`
is NOT valid grammar (grammar limits `has` to a single `identifier`).

To check plugin attribute existence, use the attribute in a condition directly
— since missing attributes cause all comparisons to evaluate to `false`
(Cedar-aligned behavior), `principal.reputation.score >= 50` safely returns
`false` when the reputation plugin is not loaded. No explicit existence check
is needed for gating on plugin attributes.

**Action bag construction:** The engine constructs `AttributeBags.Action`
directly from the `AccessRequest` — no provider is needed:

```go
AttributeBags.Action = map[string]any{
    "name": request.Action, // Always present
}
```

### Attribute Providers

```go
// AttributeProvider resolves attributes for a specific namespace.
// Providers that also contribute lock tokens implement LockTokens() to return
// a non-empty slice; providers with no lock vocabulary return an empty slice.
type AttributeProvider interface {
    Namespace() string
    ResolveSubject(ctx context.Context, subjectType, subjectID string) (map[string]any, error)
    ResolveResource(ctx context.Context, resourceType, resourceID string) (map[string]any, error)
    LockTokens() []LockTokenDef // Returns empty slice if no lock tokens
}

// EnvironmentProvider resolves environment-level attributes (no entity context).
type EnvironmentProvider interface {
    Namespace() string
    Resolve(ctx context.Context) (map[string]any, error)
}
```

**Re-entrance prohibition:** Attribute providers MUST NOT invoke
`AccessControl.Check()` or `AccessPolicyEngine.Evaluate()` during attribute
resolution. Attribute resolution happens DURING authorization evaluation —
calling back into the engine creates a deadlock. Providers that need
authorization-gated data MUST use pre-resolved attributes from the bags or
access repositories directly (bypassing authorization, as `PropertyProvider`
does).

**No-op methods:** Providers that only resolve one side (e.g.,
`CommandResolver` only resolves resources, `CharacterProvider` primarily
resolves subjects) SHOULD return `(nil, nil)` from the inapplicable method.
The resolver skips nil results during bag assembly.

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
| `level`    | float64  | MUST        | Character level (>= 0)                     |
| `flags`    | []string | MUST        | Arbitrary flags (empty array if none)      |
| `location` | string   | MUST        | ULID of current location                   |

`CharacterProvider.ResolveSubject` MUST query the world model (or equivalent)
to populate the `location` attribute, similar to how `StaticAccessControl` uses
`LocationResolver.CurrentLocation`. This dependency SHOULD be explicit in the
provider's constructor.

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
| `parent_location` | string | MUST      | ULID of parent entity's location          |

**EnvironmentProvider** (`env` namespace):

| Attribute     | Type   | Requirement | Description                           |
| ------------- | ------ | ----------- | ------------------------------------- |
| `time`        | string | MUST        | Current time (RFC 3339)               |
| `maintenance` | bool   | MUST        | Whether server is in maintenance mode |

**ObjectProvider** (`object` namespace):

| Attribute  | Type   | Requirement | Description                               |
| ---------- | ------ | ----------- | ----------------------------------------- |
| `type`     | string | MUST        | Always `"object"`                         |
| `id`       | string | MUST        | ULID of the object                        |
| `name`     | string | MUST        | Object display name                       |
| `location` | string | MUST        | ULID of containing location               |
| `owner`    | string | MAY         | Subject who owns this object              |

**StreamResolver** (`stream` namespace):

Streams do not have a dedicated database table — their attributes are derived
from the stream ID format (`location:01XYZ`, `character:01ABC`). The resolver
parses the stream ID and extracts relevant fields.

| Attribute  | Type   | Requirement | Description                                 |
| ---------- | ------ | ----------- | ------------------------------------------- |
| `type`     | string | MUST        | Always `"stream"`                           |
| `name`     | string | MUST        | Full stream path (e.g., `"location:01XYZ"`) |
| `location` | string | MAY         | Extracted location ULID (if location stream) |

**CommandResolver** (`command` namespace):

Commands are resolved from the command registry (Epic 6). The resolver looks up
the command definition by name.

| Attribute | Type   | Requirement | Description                     |
| --------- | ------ | ----------- | ------------------------------- |
| `type`    | string | MUST        | Always `"command"`              |
| `name`    | string | MUST        | Command name (e.g., `"say"`)   |

**Action bag** is constructed by the engine directly from the `AccessRequest` —
see [Action bag construction](#attributebags) above.

**Plugin providers** contribute attributes under their own namespace (e.g.,
`reputation.score`). Plugin attributes are always MAY — the engine tolerates
their absence.

**Cross-plugin attribute references:** Plugins can reference each other's
attributes in DSL conditions via the shared attribute bags. For example, a
reputation-gated guild policy can reference both namespaces:
`principal.reputation.score >= 50 && principal.guilds.primary == "merchants"`.
Each plugin resolves its own namespace; the DSL evaluator reads from the
merged bag. No direct plugin-to-plugin dependency is needed.

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
        Subject:  normalizeSubjectPrefix(subject),
        Action:   action,
        Resource: normalizeResource(resource),
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
resource_clause  = "resource" [ "is" type_name | "==" string_literal ]

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
             | expr                                  (* bare boolean: true, false, or boolean attribute *)

expr       = attribute_ref | literal
attribute_ref = ("principal" | "resource" | "action" | "env") "." identifier { "." identifier }
             (* "containsAll" and "containsAny" are reserved words that MUST NOT
                appear as attribute names. Parser disambiguation: when the parser
                encounters one of these tokens after ".", it uses one-token
                lookahead — if the NEXT token is "(", treat it as a method call;
                otherwise, it is a parse error (these names are reserved and
                cannot be attribute segments). This avoids ambiguity between
                method calls and attribute paths. *)
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

**Operator precedence** (highest to lowest):

| Precedence | Operator(s)                    | Associativity |
| ---------- | ------------------------------ | ------------- |
| 1          | `.` (attribute access)         | Left          |
| 2          | `!` (boolean NOT)              | Right (unary) |
| 3          | `has`, `in`, `like`            | Non-assoc     |
| 4          | `==`, `!=`, `>`, `>=`, `<`, `<=` | Non-assoc  |
| 5          | `containsAll`, `containsAny`  | Non-assoc     |
| 6          | `&&` (boolean AND)             | Left          |
| 7          | `\|\|` (boolean OR)            | Left          |
| 8          | `if-then-else`                 | Right         |

**Grammar notes:**

- `&&` binds tighter than `||` (conjunction before disjunction), matching
  standard boolean logic and Cedar semantics.
- `like` uses `gobwas/glob` syntax (already in the project), NOT SQL `LIKE`
  semantics. Supported wildcards: `*` matches any sequence of characters
  (excluding the `:` separator), `?` matches exactly one character (excluding
  `:`). Character classes (`[abc]`) and alternation (`{a,b}`) are NOT
  supported — the DSL parser MUST reject `like` patterns containing `[`, `{`,
  or `**` syntax before passing them to `glob.Compile(pattern, ':')`. This
  restricts `like` to simple `*` and `?` wildcards only. To match a literal `*`
  or `?`, there is no escape mechanism; use `==` for exact matches instead. The `:` separator
  provides natural namespace isolation: `*` does NOT match across `:`
  boundaries. For example, `"location:*"` matches `"location:01ABC"` but not
  `"location:sub:01ABC"`. The `:` character is passed as the separator argument
  to `glob.Compile(pattern, ':')`, which prevents `*` from matching across `:`
  boundaries. This is consistent with the existing `StaticAccessControl`
  permission matching. The DSL evaluator tests MUST verify this separator
  behavior explicitly.
- `action` is a valid attribute root in conditions, providing access to the
  `AttributeBags.Action` map (e.g., `action.name`). Action matching in the
  target clause covers most use cases, but conditions MAY reference action
  attributes when needed.
- `resource == string_literal` in the target clause pins a policy to a specific
  resource instance (e.g., `resource == "object:01ABC"`). This is used by
  lock-generated policies that target a single owned resource. Manually authored
  policies SHOULD prefer `resource is type_name` with conditions for flexibility.
  The `principal_clause` and `action_clause` intentionally lack `==` forms.
  For principal-specific matching, use conditions instead: e.g.,
  `when { principal.id == "character:01ABC" }` rather than a hypothetical
  `principal == "character:01ABC"` target clause.
- `expr "in" list` performs scalar-in-set membership: the left-hand value is
  checked for equality against each element of the literal list. For example,
  `principal.role in ["builder", "admin"]` returns true if `principal.role`
  equals `"builder"` or `"admin"`.
- `expr "in" expr` performs value-in-attribute-array membership: the left-hand
  value is checked for presence in the right-hand attribute, which MUST resolve
  to a `[]string` or `[]any` at evaluation time. For example,
  `principal.id in resource.visible_to` checks whether the principal's ID
  appears in the resource's `visible_to` list.
- **Empty lists** are not valid in the grammar. `list` requires at least one
  element. A policy matching no actions SHOULD be disabled via `policy disable`.
  Disabled policies remain visible in `policy list --disabled` but do not
  participate in evaluation. Alternatively, use an impossible condition (e.g.,
  `when { false }`) to keep a policy visible but inactive.
- **Bare boolean expressions:** The `| expr` alternative in `condition` allows
  bare `true`, `false`, or boolean attribute references as conditions. This is
  required for the `else true` pattern in `if-then-else` expressions. Bare
  boolean attributes (e.g., `resource.restricted`) are equivalent to
  `resource.restricted == true` — both forms are valid. If a bare expression
  resolves to a non-boolean value, the condition evaluates to `false`
  (fail-safe).
- **Deferred: entity references.** Cedar defines `entity_ref` syntax
  (`Type::"value"`) for hierarchy membership checks (e.g.,
  `principal in Group::"admins"`). This is NOT included in the initial grammar.
  The parser MUST reject `Type::"value"` syntax with a clear error message
  directing admins to use attribute-based group checks
  (`principal.flags.containsAny(["admin"])`) instead. Entity references MAY be
  added in a future phase when group/hierarchy features are implemented.

### Type System

The DSL uses dynamic typing with fail-safe behavior on type mismatches:

| Scenario                              | Behavior                       |
| ------------------------------------- | ------------------------------ |
| Attribute missing (any operator)      | Condition evaluates to `false` |
| Type mismatch (e.g., string > int)    | Condition evaluates to `false` |
| `>`, `>=`, `<`, `<=` on non-number   | Condition evaluates to `false` |
| `containsAll`/`containsAny` on non-array | Condition evaluates to `false` |
| `has` on non-existent attribute       | Returns `false`                |
| `==`/`!=` across types               | Condition evaluates to `false` |

**Cedar alignment:** When an attribute is missing, ALL comparisons — including
`!=` — evaluate to `false`. This matches Cedar's behavior where a missing
attribute produces an error value that causes the entire condition to be
unsatisfied. This prevents a class of policies that accidentally grant access
when attributes are absent. For example, `principal.faction != "enemy"` returns
`false` (not `true`) when `faction` is missing, ensuring characters without a
faction are NOT accidentally permitted.

**Defensive pattern for negation:** To write "allow anyone who is not an enemy":

```text
// CORRECT: explicitly check existence first
when { principal has faction && principal.faction != "enemy" };

// ALSO CORRECT: use if-then-else with safe default
when { if principal has faction then principal.faction != "enemy" else false };
```

**Number coercion rules:**

- DSL number literals are parsed as `float64`
- `AttributeProvider` implementations MUST return numeric attributes as
  `float64`. Integer database columns (e.g., `level`) are cast to `float64`
  during attribute resolution, not at comparison time
- All numeric comparisons operate on `float64` values
- This enables plugin attributes to use fractional values (e.g.,
  `reputation.score >= 75.5`) without grammar changes

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

**Dependency layering:** `PropertyRepository` owns data access AND data
invariants. Specifically, `PropertyRepository.Create()` and
`PropertyRepository.Update()` MUST enforce visibility defaults in Go code: when
visibility is `restricted`, auto-populate `visible_to` with `[parent_id]` and
`excluded_from` with `[]` if they are nil. `WorldService` (or the command
handler) owns business rules (e.g., "only the owner can set restricted
visibility") and calls the repository after validation.

During attribute resolution, `PropertyProvider` MUST call
`PropertyRepository` methods directly, bypassing `WorldService`. The engine
resolves property attributes unconditionally (no authorization check during
attribute resolution); authorization happens AFTER attributes are resolved.
This prevents a circular dependency:
`Engine → PropertyProvider → PropertyRepository` (no callback to Engine).
`PropertyProvider` MUST NOT depend on `WorldService` or `AccessPolicyEngine`.

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

### Visibility Seed Policies

Each visibility level is enforced by system-level seed policies. These are
created during bootstrap alongside the role-based seed policies:

```text
// Public properties: readable by characters in the same location as the parent
permit(principal is character, action in ["read"], resource is property)
when { resource.visibility == "public"
    && principal.location == resource.parent_location };

// Private properties: readable only by owner
permit(principal is character, action in ["read"], resource is property)
when { resource.visibility == "private"
    && resource.owner == principal.id };

// Restricted properties: visible_to/excluded_from policies handle these
// (already defined in Example Policies section)

// System properties: only accessible by system subject (handled by system bypass)

// Admin properties: readable only by admins
permit(principal is character, action in ["read"], resource is property)
when { resource.visibility == "admin" && principal.role == "admin" };
```

**Note:** The `PropertyProvider` MUST also expose a `parent_location` attribute
— the ULID of the parent entity's location. For character properties, this
is the character's current location. For location properties, this is the
location itself. For object properties, this is the object's containing
location.

**Dependency chain:** Resolving `parent_location` requires the
`PropertyProvider` to query the parent entity's location. This creates an
additional dependency beyond `PropertyRepository`: the provider needs a
location-resolution mechanism. Implementation SHOULD accept a
`LocationLookup func(entityType, entityID string) (string, error)` in the
constructor, backed by a direct database query (NOT via `WorldService`, to
maintain the no-circular-dependency invariant).

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

**Note:** This example is illustrative, not exhaustive. Only providers matching
the parsed entity types are called. When the resource is `object:01DEF`,
`ObjectProvider.ResolveResource()` is called instead of `LocationProvider`.
All registered plugin providers are called regardless of entity type — they
return `(nil, nil)` for entity types they don't handle.

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

**Plugin provider errors:** The engine logs an error via slog and continues
evaluation with the remaining providers. Missing plugin attributes cause
conditions referencing them to evaluate to `false` (fail-safe). The audit log
records plugin provider errors in a `provider_errors` JSONB field (structured
as `[{"namespace": "reputation", "error": "connection refused"}]`) to aid
debugging "why was I denied?" investigations. Logging is rate-limited to 1
error per minute per provider namespace to control spam.

```text
slog.Error("plugin attribute provider failed",
    "namespace", provider.Namespace(), "error", err)
```

**Error classification:** All errors result in fail-closed (deny) behavior.
No errors are retryable within a single `Evaluate()` call.

| Error Type              | Fail Mode           | Caller Action                      |
| ----------------------- | ------------------- | ---------------------------------- |
| Core provider failure   | Deny + return error | Log and deny (adapter) or inspect  |
| Plugin provider failure | Deny (conditions fail) | Automatic — plugin attrs missing |
| Policy compilation error | Deny + return error | Should not occur (compiled at store time) |
| Session not found       | Deny + return error | Log, deny, session likely expired  |
| Context cancelled       | Deny + return error | Request was cancelled upstream     |

Direct callers of `AccessPolicyEngine.Evaluate()` can distinguish "denied by
policy" (`err == nil, Decision.Effect == EffectDeny`) from "system failure"
(`err != nil, Decision.Effect == EffectDefaultDeny`). The adapter collapses
both to `false`.

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
│         ├─ principal: "principal is T" matches when parsed subject
│         │   prefix equals T (e.g., "character:", "plugin:").
│         │   Valid types: character, plugin. "session" is never
│         │   valid (resolved before this step). Bare "principal"
│         │   matches all subject types.
│         ├─ action: "action in [...]" matches when request action
│         │   is in the list. Bare "action" matches all actions.
│         └─ resource: "resource is T" matches when parsed resource
│             prefix equals T. "resource == X" matches exact string.
│             Bare "resource" matches all resource types.
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

**Benchmark scenario:** Targets assume 50 active policies (25 permit, 25
forbid), average condition complexity of 3 operators per policy, 10 attributes
per entity (subject + resource), and warm per-request attribute cache (not the
first `Evaluate()` call in a request).

**Worst-case bounds:**

| Scenario                             | Bound   | Handling                                  |
| ------------------------------------ | ------- | ----------------------------------------- |
| All 50 policies match (pathological) | <10ms   | Linear scan is acceptable at this scale   |
| Provider timeout                     | 100ms   | Context deadline; return deny + error     |
| Cache miss storm (post-NOTIFY flood) | <100ms  | Lock during reload; stale reads tolerable |
| Plugin provider slow                 | 50ms    | Per-provider context deadline             |

The `Evaluate()` context SHOULD carry a 100ms deadline. If attribute resolution
exceeds this, the engine returns `EffectDefaultDeny` with a timeout error.
Individual plugin providers inherit this deadline — a slow plugin cannot block
the entire evaluation pipeline beyond the request timeout.

**Measurement strategy:**

- Export a Prometheus histogram metric for `Evaluate()` latency
  (e.g., `abac_evaluate_duration_seconds`)
- Add `BenchmarkEvaluate_*` tests with targets as failure thresholds (CI
  fails if benchmarks regress >20% from baseline)
- Staging monitoring alerts on p99 > 10ms (2x target)
- Implementation SHOULD add `slog.Debug()` timers in `engine.Evaluate()` for
  attribute resolution, policy filtering, condition evaluation, and audit
  logging to enable performance profiling during development

### Attribute Caching

The `AttributeResolver` SHOULD implement per-request caching from the start.
When a single user action triggers multiple `Evaluate()` calls (e.g., check
command permission, then check location entry, then check property read), the
same subject attributes are resolved repeatedly.

**Caching strategy:**

- **Scope:** Per `context.Context`. A shared `AttributeCache` is attached to
  the context by the request handler. Multiple `Evaluate()` calls within the
  same request context share the cache.
- **Key:** `{entityType, entityID}` tuple (e.g., `{"character", "01ABC"}`).
- **Invalidation:** Cache is garbage-collected when the context is cancelled
  (end of request). No cross-request caching for MVP.
- **Provider isolation:** Each provider's results are cached independently. A
  plugin provider failure does not invalidate cached results from core providers.

```go
// AttributeCache provides per-request attribute caching.
// Attach to context via WithAttributeCache(ctx) at the request boundary.
type AttributeCache struct {
    mu    sync.RWMutex
    items map[string]map[string]any // "character:01ABC" → attributes
}

// WithAttributeCache attaches a new cache to the context.
// Call this at the request boundary (e.g., command handler entry point).
func WithAttributeCache(ctx context.Context) context.Context

// GetAttributeCache retrieves the cache from context, or nil if none attached.
func GetAttributeCache(ctx context.Context) *AttributeCache
```

The cache assumes **read-only world state** during request processing. If a
command modifies character location, subsequent authorization checks in the
same request use the pre-modification snapshot. This is consistent with the
eager resolution model — attributes are a point-in-time snapshot.

**Future optimization:** If profiling shows cache misses dominate, consider a
short-TTL cache (100ms) for read-only attributes like character roles. This
requires careful invalidation and is deferred until profiling demonstrates the
need.

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
    attributes      JSONB,
    error_message   TEXT,
    provider_errors JSONB                    -- e.g., ["reputation: connection refused"]
);

CREATE INDEX idx_audit_log_timestamp ON access_audit_log(timestamp DESC);
CREATE INDEX idx_audit_log_subject ON access_audit_log(subject, timestamp DESC);
CREATE INDEX idx_audit_log_resource ON access_audit_log(resource, timestamp DESC);
CREATE INDEX idx_audit_log_decision ON access_audit_log(decision, timestamp DESC);
CREATE INDEX idx_audit_log_policy_denied ON access_audit_log(policy_id, timestamp DESC)
    WHERE decision = 'denied';
CREATE INDEX idx_audit_log_resource_action ON access_audit_log(resource, action, timestamp DESC);

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
    UNIQUE(parent_type, parent_id, name),
    CONSTRAINT visibility_restricted_requires_lists
        CHECK (visibility != 'restricted'
            OR (visible_to IS NOT NULL AND excluded_from IS NOT NULL)),
    CONSTRAINT visibility_non_restricted_nulls_lists
        CHECK (visibility = 'restricted'
            OR (visible_to IS NULL AND excluded_from IS NULL))
);

CREATE INDEX idx_properties_parent ON entity_properties(parent_type, parent_id);
```

**Implementation note:** The `updated_at` column has no database trigger. The
Go property store MUST explicitly set `updated_at = now()` in all UPDATE
queries.

### Cache Invalidation

The Go policy store sends `pg_notify('policy_changed', policyID)` in the same
transaction as any policy CRUD operation. The engine subscribes to this channel
and reloads its in-memory policy cache on notification. No database triggers are
used — all notification logic lives in Go application code.

The engine uses `pgx.Conn.Listen()` which requires a dedicated persistent
connection outside the connection pool. On connection loss, the engine MUST:

1. Reconnect with exponential backoff (initial: 100ms, multiplier: 2x, max:
   30s, indefinite retries)
2. Re-subscribe to the `policy_changed` channel
3. Perform a full policy reload before serving the next `Evaluate()` call
   (missed notifications cannot be recovered)

During reconnection, `Evaluate()` uses the stale in-memory cache and logs a
warning: `slog.Warn("policy cache may be stale, LISTEN/NOTIFY reconnecting")`.
This is acceptable for MUSH workloads where a brief stale window (<30s) is
tolerable.

### Audit Log Serialization

The `effect` column in `access_audit_log` maps to the Go `Effect` enum:

| Go Constant         | DB Value         |
| ------------------- | ---------------- |
| `EffectAllow`       | `"allow"`        |
| `EffectDeny`        | `"deny"`         |
| `EffectDefaultDeny` | `"default_deny"` |

The `decision` column is derived: `"allowed"` when `Effect == EffectAllow`,
`"denied"` otherwise.

The `Effect` type MUST serialize to the string values in the mapping table
above, not to `iota` integer values. Implementation SHOULD define a
`String() string` method on `Effect` that returns the DB-compatible string.

### Policy Version Records

A version record is created in `access_policy_versions` only when `dsl_text`
changes (via `policy edit`). Toggling `enabled` via `policy enable`/`disable` or
updating `description` modifies the main `access_policies` row directly without
creating a version record. The `version` column on `access_policies` increments
only on DSL changes.

### Audit Log Retention

Audit records older than 90 days SHOULD be purged by a periodic Go background
job. The purge interval and retention period SHOULD be configurable via server
settings. If the audit table exceeds 10M rows, PostgreSQL table partitioning by
month MAY be introduced.

### Visibility Defaults

See [Visibility Levels](#visibility-levels) and [Dependency layering](#property-model)
for the definitive visibility default rules. The Go property store
(`PropertyRepository`) enforces these defaults — not database triggers.

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

Providers register tokens at startup alongside their attributes. The
`LockTokens()` method is part of the unified `AttributeProvider` interface
(defined in the [Attribute Providers](#attribute-providers) section). Providers
that contribute no lock vocabulary return an empty slice.

**Token name conflict resolution:** The engine MUST reject duplicate token
registrations at startup. Duplicate tokens are a fatal error — the server MUST
NOT start if any token name conflicts are detected. This fail-fast behavior
ensures naming collisions are caught immediately rather than causing subtle
policy evaluation bugs at runtime. Core providers (CharacterProvider) register
tokens first. Plugins SHOULD namespace their tokens to avoid collisions (e.g.,
`rep.score` instead of `score`, `craft` instead of `type`).
The `policy create` command MUST also reject names starting with `seed:` to
reserve that prefix for system use.

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
    action in ["read"],
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
policy test <subject> <action> <resource> [--verbose] [--json]
policy history <name> [--limit=N]
policy audit [--subject=X] [--action=Y] [--decision=denied] [--last=1h] [--limit=N]
```

**Naming conventions:** The `seed:` prefix is reserved for system use. The
`lock:` prefix is reserved for lock-generated policies. Admin-created policies
SHOULD use descriptive names following the pattern
`{effect}-{scope}-{description}` (e.g., `permit-faction-hq-access`,
`forbid-maintenance-all`) to improve `policy list` readability.

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

**Output format:** Human-readable text by default. The `--json` flag outputs
structured JSON for programmatic consumption. Attribute values exceeding 80
characters are truncated with `... (truncated)` in text mode.

**Visibility:** Builders see only policies whose target matches their test
query, not the full policy set. Admins see all matching policies.

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
  level-gate           forbid  CONDITIONS FAILED (principal.level < 5: false, level=7)

Decision: DENIED (default deny — no policies matched)
```

### Command Permissions

```text
permit(principal is character, action in ["execute"], resource is command)
when { principal.role == "admin" && resource.name like "policy*" };

permit(principal is character, action in ["execute"], resource is command)
when { principal.role == "builder" && resource.name == "policy test" };
```

### policy audit

The `--last` flag uses wall-clock time (e.g., `--last=1h` returns events from
the last 60 minutes of real time). The `--limit` flag controls maximum result
count (default 100, max 1000). When both flags are present, `--last` filters
first, then `--limit` caps the result set.

### Policy Order Irrelevance

Policy evaluation order is undefined. Deny-overrides means policy creation
order does not matter — a `forbid` added last still blocks a `permit` added
first. If this causes confusion, admins SHOULD write more specific conditions
rather than relying on ordering.

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

**Command name normalization:** The static system uses `@`-prefixed command
names (`@dig`, `@create`, etc.) in permission strings, while the ABAC seed
policies use plain names (`dig`, `create`) matching Epic 6 conventions. The
`accessControlAdapter` MUST normalize `@`-prefixed command names at its entry
point, stripping the `@` prefix from resource strings matching
`command:@*` before passing them to the engine. This protects all callers from
the legacy naming convention rather than requiring each shadow mode comparison
to handle it.

**Intentional permission expansion:** The seed policies below intentionally
grant builders `delete` on locations, which the static system does not. The
static system's omission of `delete:location:*` for builders was a gap — builders
who can `write` (create/modify) locations SHOULD also be able to delete them.
This expansion is validated during shadow mode by excluding `delete` on
`location` resources from the agreement comparison, alongside `enter` actions
and `@`-prefixed command names.

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

### Bootstrap Sequence

On first startup (or when the `access_policies` table is empty), the server
MUST seed policies automatically:

1. Server startup detects empty `access_policies` table
2. Server inserts all seed policies (role-based + visibility) as `system` subject
3. The `system` subject bypasses policy evaluation entirely (step 1 of the
   evaluation algorithm), so no chicken-and-egg problem exists
4. Subsequent policy changes require `policy.manage` permission via normal
   ABAC evaluation

The seed process is idempotent — policies are inserted with deterministic names
(e.g., `seed:player-self-access`). If a policy with that name already exists,
the seed is skipped for that policy. The `seed:` prefix is reserved for system
use — the `policy create` command MUST reject names starting with `seed:` to
prevent admins from accidentally colliding with seed policies.

**Seed policy upgrades:** Seed policies are immutable after first creation.
Server upgrades that ship updated seed text do NOT overwrite existing seeds.
This ensures admin customizations to seed policies (via `policy edit`) survive
upgrades. If a server version needs to fix a seed policy bug, it MUST use a
migration that explicitly updates the affected policy, logged as a version
change with a `change_note` explaining the upgrade.

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
   `Decision.Allowed` matches `StaticAccessControl.Check()` for all checks,
   excluding known semantic differences:
   - `enter` actions (new ABAC-only path, no static equivalent)
   - `delete` on `location` resources (intentional permission expansion)
   - `@`-prefixed command names (normalized to plain names in ABAC)

   Any other disagreement blocks cutover and triggers immediate investigation.
   After meeting criteria, submit PR to remove `StaticAccessControl`. Rollback:
   revert the removal PR if post-cutover bugs are found.

   **Shadow mode adapter:** During shadow mode, the adapter MUST preserve engine
   errors for comparison rather than swallowing them:

   ```go
   // migrationAdapter wraps AccessPolicyEngine for shadow mode validation.
   // Unlike accessControlAdapter, it preserves errors for disagreement analysis.
   type migrationAdapter struct {
       engine  AccessPolicyEngine
       static  *StaticAccessControl
       logger  *slog.Logger
       metrics *shadowModeMetrics
   }

   // shadowModeMetrics tracks shadow mode validation statistics.
   // Export to Prometheus for visibility into cutover readiness.
   type shadowModeMetrics struct {
       totalChecks   atomic.Int64
       agreements    atomic.Int64
       disagreements atomic.Int64
       engineErrors  atomic.Int64
   }

   func (a *migrationAdapter) Check(ctx context.Context, subject, action, resource string) bool {
       a.metrics.totalChecks.Add(1)
       staticResult := a.static.Check(ctx, subject, action, resource)

       // Normalize for engine: strip @ prefix from command names, normalize char: → character:
       engineSubject := normalizeSubjectPrefix(subject)
       engineResource := normalizeResource(resource)
       decision, err := a.engine.Evaluate(ctx, AccessRequest{
           Subject: engineSubject, Action: action, Resource: engineResource,
       })
       if err != nil {
           a.metrics.engineErrors.Add(1)
           a.logger.Warn("shadow mode: engine error (not a disagreement)",
               "error", err, "static_result", staticResult)
           return staticResult // Static system is authoritative during shadow mode
       }
       if decision.Allowed != staticResult {
           a.metrics.disagreements.Add(1)
           a.logger.Error("shadow mode: DISAGREEMENT",
               "subject", subject, "action", action, "resource", resource,
               "static", staticResult, "engine", decision.Allowed,
               "policy", decision.PolicyID)
       } else {
           a.metrics.agreements.Add(1)
       }
       return staticResult // Static system is authoritative during shadow mode
   }
   ```

   **Normalization helpers** (used by both `accessControlAdapter` and
   `migrationAdapter`):

   ```go
   func normalizeSubjectPrefix(subject string) string {
       if strings.HasPrefix(subject, "char:") {
           return "character:" + strings.TrimPrefix(subject, "char:")
       }
       return subject
   }

   func normalizeResource(resource string) string {
       if strings.HasPrefix(resource, "command:@") {
           return "command:" + strings.TrimPrefix(resource, "command:@")
       }
       return resource
   }
   ```

   Shadow mode comparison operates per authorization check (subject+action+
   resource tuple), not per policy match. Semantic difference exclusions apply
   to individual checks, not to entire policies.

4. **Caller migration (holomush-c6qch):** Callers incrementally migrate from
   `AccessControl.Check()` to `AccessPolicyEngine.Evaluate()` for richer error
   handling.

   **Call site inventory** (packages depending on `access.AccessControl`):

   | Package                              | Usage                               |
   | ------------------------------------ | ----------------------------------- |
   | `internal/command/dispatcher`         | Command execution authorization     |
   | `internal/command/rate_limit_middleware` | Rate limit bypass for admins     |
   | `internal/command/handlers/boot`      | Boot command permission check       |
   | `internal/world/service`              | World model operation authorization |
   | `internal/plugin/hostfunc/commands`   | Plugin command execution auth       |
   | `internal/plugin/hostfunc/functions`  | Plugin host function auth           |
   | `internal/core/broadcaster` (test)    | Test mock injection                 |

   This list is derived from the current codebase. Additional call sites may
   exist by the time phase 7.3 begins — run `grep -r "AccessControl"
   internal/ --include="*.go"` to get the current inventory.

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
