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
- Configurable audit logging with mode control (off, denials-only, all)
- Direct replacement of `AccessControl` with `AccessPolicyEngine` across all
  call sites (greenfield deployment — no backward-compatibility adapter)
- Default-deny posture with deny-overrides conflict resolution

### Non-Goals

- Graph-based relationship traversal (OpenFGA/Zanzibar-style) — relationships
  are modeled as attributes
- Priority-based policy ordering — deny always wins, no escalation
- Real-time policy synchronization across multiple server instances
  (single-server for now)
- Web-based policy editor (admin commands cover MVP, web UI deferred)
- Database triggers or stored procedures — all logic lives in Go

### Glossary

| Term            | Definition                                                                                                    |
| --------------- | ------------------------------------------------------------------------------------------------------------- |
| **Subject**     | The entity in `AccessRequest.Subject` — the Go-side identity string (e.g., `"character:01ABC"`)               |
| **Principal**   | The DSL keyword referring to the subject — `principal is character` matches subjects with `character:` prefix |
| **Resource**    | The target of the access request — entity string in `AccessRequest.Resource`                                  |
| **Action**      | The operation being performed — string in `AccessRequest.Action` (e.g., `"read"`, `"execute"`)                |
| **Environment** | Server-wide context attributes (time, maintenance mode) — the `env` prefix in DSL                             |
| **Policy**      | A permit or forbid rule with target matching and conditions, stored in `access_policies`                      |
| **Seed policy** | A system-installed default policy (prefixed `seed:`) created at first startup                                 |
| **Lock**        | A player-authored simplified policy using token syntax, compiled to a scoped `lock:` policy                   |
| **Decision**    | The outcome of `Evaluate()` — includes effect, reason, matched policies, and attribute snapshot               |

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
| Migration             | Direct replacement (no adapter)         | Greenfield — no releases, clean cutover via seed policies         |
| Cache invalidation    | PostgreSQL LISTEN/NOTIFY (in Go code)   | Push-based, no polling overhead                                   |
| Player access control | Layered: metadata + locks + full policy | Progressive complexity for different user roles                   |

**Seed policies** are the default permission policies installed automatically
at first server startup, replacing the static role definitions from Epic 3.
They define baseline access (movement, self-access, builder/admin privileges)
using the ABAC policy language. See [Seed Policies](#seed-policies) for the
full set.

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
│   │   compiled form  │  │                  │  │ - Attr snapshot │   │
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
│                    │ StreamProvider        │ ← Derived from stream ID │
│                    │ CommandProvider       │ ← Command registry       │
│                    │ Session Resolver      │ ← Session store (not a   │
│                    │                       │   provider; see Session  │
│                    │                       │   Subject Resolution)    │
│                    │ EnvironmentProvider   │ ← Clock, game state      │
│                    │ PluginProvider(s)     │ ← Registered by plugins  │
│                    └──────────────────────┘                          │
│                                                                      │
└──────────────────────────────────────────────────────────────────────┘
```

### Request Flow

1. Caller invokes `Evaluate(ctx, AccessRequest)`
2. Engine resolves all attributes eagerly — calls core providers + registered
   plugin providers
3. Engine loads matching policies from the in-memory cache
4. Engine evaluates each policy's conditions against the attribute bags
5. Deny-overrides: any forbid → deny (wins over permits); else any permit →
   allow; else default deny
6. Audit logger records the decision, matched policies, and attribute snapshot
7. Returns `Decision` with allowed/denied, reason, and matched policy ID

### Package Structure

```text
internal/access/               # Existing — AccessPolicyEngine replaces AccessControl
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
    stream.go                  # Core: StreamProvider — stream attributes (derived from ID)
    command.go                 # Core: CommandProvider — command attributes
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
// This is a concrete struct (not an interface) because there is only one
// implementation. The policy store accepts it as a constructor dependency.
type PolicyCompiler struct {
    schema *AttributeSchema // Optional: for validation warnings
}

// Compile parses DSL text, validates it, and returns a compiled policy.
// Returns a descriptive error with line/column information on parse failure.
// If schema is configured, also returns validation warnings for:
// - Unknown attributes (schema evolves over time)
// - Bare boolean expressions (recommend explicit `== true`)
// - Unreachable conditions (e.g., `false && ...`)
// - Always-true conditions (e.g., `principal.level >= 0`)
// - Redundant sub-conditions
// Warnings do not block creation.
func (c *PolicyCompiler) Compile(dslText string) (*CompiledPolicy, []ValidationWarning, error)

// CompiledPolicy is the parsed, validated, and optimized form of a policy.
// The engine evaluates CompiledPolicy instances, never raw DSL text.
type CompiledPolicy struct {
    Effect     Effect
    Target     CompiledTarget     // Parsed principal/action/resource clauses
    Conditions []CompiledCondition // Pre-parsed AST nodes
    GlobCache  map[string]glob.Glob // Pre-compiled globs for `like` expressions
}
```

**`compiled_ast` JSONB schema sketch:**

```json
{
  "effect": "permit",
  "target": {
    "principal_type": "character",
    "action_list": ["read", "write"],
    "resource_type": "property",
    "resource_exact": null
  },
  "conditions": [
    {
      "op": "==",
      "left": { "type": "attr_ref", "root": "resource", "path": ["parent_type"] },
      "right": { "type": "literal", "value": "character" }
    },
    {
      "op": "==",
      "left": { "type": "attr_ref", "root": "resource", "path": ["parent_id"] },
      "right": { "type": "attr_ref", "root": "principal", "path": ["id"] }
    }
  ],
  "globs": {
    "resource.name like \"wounds*\"": "<pre-compiled glob index>"
  }
}
```

Node types: `attr_ref` (root + path), `literal` (string/number/bool),
`list` (array of literals), `binary_op` (op + left + right),
`unary_op` (op + operand), `has` (root + path), `method_call`
(receiver + method + args), `if_then_else` (cond + then + else).

The policy store calls `Compile()` on every `Create` and `Edit` operation,
rejecting invalid DSL before persisting. The in-memory policy cache holds
`CompiledPolicy` instances — `Evaluate()` never re-parses DSL text, ensuring
the <5ms p99 latency target is achievable.

### AccessRequest

```go
// AccessRequest contains all information needed for an access decision.
type AccessRequest struct {
    Subject  string // "character:01ABC", "plugin:echo-bot", "system"
    Action   string // Common: "read", "write", "delete", "enter", "execute", "emit". Open set — plugins MAY introduce additional actions.
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
| `session:`   | (resolved)  | `session:web-123`       |
| `location:`  | `location`  | `location:01XYZ`        |
| `object:`    | `object`    | `object:01DEF`          |
| `command:`   | `command`   | `command:say`           |
| `property:`  | `property`  | `property:01GHI`        |
| `stream:`    | `stream`    | `stream:location:01XYZ` |

**Note:** The `session:` prefix is never a valid DSL type in policies. Sessions
are always resolved to `character:` at the engine entry point before evaluation
(see [Session Subject Resolution](#session-subject-resolution)). The `(resolved)`
marker indicates this prefix does not appear in `principal is T` clauses.

**Subject prefix constants:** The `access` package MUST define these prefixes
as constants (e.g., `SubjectCharacter = "character:"`, `SubjectPlugin =
"plugin:"`) to prevent typos and enable compile-time references. All call sites
MUST use the full `character:` prefix (not the legacy `char:` abbreviation).
The engine MUST reject unknown prefixes with a clear error.

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
  EffectDefaultDeny, PolicyID: "infra:session-not-found"}` with error
  `"session not found: web-123"`. Using `EffectDefaultDeny` (not
  `EffectDeny`) distinguishes infrastructure failures from explicit policy
  denials in audit queries. The `infra:` prefix on the PolicyID further
  disambiguates infrastructure errors from "no policy matched" (which has
  an empty PolicyID).
- **Session store failure:** Return `Decision{Allowed: false, Effect:
  EffectDefaultDeny, PolicyID: "infra:session-store-error"}` with the
  wrapped store error. Fail-closed — a session lookup failure MUST NOT
  grant access.
- **No associated character:** Return `Decision{Allowed: false, Effect:
  EffectDefaultDeny, PolicyID: "infra:session-no-character"}` with error
  `"session has no associated character"`. This handles unauthenticated
  sessions that have not yet selected a character.
- **Character deleted after session creation:** Return `Decision{Allowed:
  false, Effect: EffectDefaultDeny, PolicyID:
  "infra:session-character-integrity"}` with error `"session character has
  been deleted: 01ABC"`. This error **SHOULD** never occur in correct
  operation — it indicates a defect in the session invalidation logic or data
  corruption. The server **MUST** log at CRITICAL level and operators
  **SHOULD** configure alerting on this error. Session cleanup **MUST**
  invalidate sessions on character deletion: `WorldService.DeleteCharacter()`
  **MUST** query the session store and delete all sessions for the character
  within the same transaction.

**Session integrity circuit breaker:** To prevent systematic
`SESSION_CHARACTER_INTEGRITY` errors from generating excessive logs (up to
7200 CRITICAL logs per minute in worst-case scenarios), the engine **MUST**
implement a per-session circuit breaker. If a session generates **3 or more**
`SESSION_CHARACTER_INTEGRITY` errors within a **60-second window**, the
session **MUST** be automatically invalidated and subsequent evaluation
requests for that session **MUST** return
`infra:session-invalidated-by-circuit-breaker` without re-logging the
integrity error. The first 3 failures are logged at CRITICAL level. The
circuit breaker trip is logged once at ERROR level with message `"session
circuit breaker tripped for session 01XYZ after 3 integrity failures —
session invalidated"`. A Prometheus counter metric
`abac_session_circuit_breaker_trips_total` **MUST** be incremented when the
circuit breaker trips. The 60-second window **SHOULD** be configurable via
server config for testing and tuning.

**Audit distinguishability:** Session resolution errors use
`EffectDefaultDeny` with `infra:*` policy IDs, making them filterable in
audit queries. `WHERE effect = 'deny'` returns only explicit policy denials.
`WHERE policy_id LIKE 'infra:%'` returns only infrastructure failures.
`WHERE effect = 'default_deny' AND policy_id = ''` returns "no policy
matched" cases.

Session resolution errors SHOULD use oops error codes for structured handling:
`SESSION_NOT_FOUND`, `SESSION_STORE_ERROR`, `SESSION_NO_CHARACTER`,
`SESSION_CHARACTER_INTEGRITY`. The `SESSION_CHARACTER_INTEGRITY` code
reflects that this is a world model integrity violation (the character was
deleted while sessions still referenced it), not a normal session lifecycle
event. Session cleanup **MUST** invalidate sessions on character deletion
(see above) to prevent this from occurring.

### Decision

```go
// Decision represents the outcome of a policy evaluation.
// Invariant: Allowed is true if and only if Effect is EffectAllow or EffectSystemBypass.
type Decision struct {
    Allowed    bool
    Effect     Effect          // Allow, Deny, DefaultDeny (no policy matched), or SystemBypass
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
    EffectSystemBypass              // System subject bypass (audit-only)
)

// PolicyMatch records a single policy's evaluation result.
// ConditionsMet is primarily for `policy test --verbose` debugging: it
// distinguishes "matched target but conditions failed" from "matched target
// and conditions passed." This enables the verbose output to show why a
// policy didn't fire, even though it was a candidate.
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
operator supports both simple and dotted paths: `principal has faction` checks
whether `"faction"` exists in the subject bag; `principal has reputation.score`
checks whether `"reputation.score"` exists. For dotted paths, the parser joins
the segments with `.` and checks the resulting flat key. This allows defensive
patterns for plugin-contributed attributes:
`principal has reputation.score && principal.reputation.score >= 50`.

Missing attributes cause all comparisons to evaluate to `false`
(Cedar-aligned behavior), so `principal.reputation.score >= 50` safely returns
`false` when the reputation plugin is not loaded. The `has` check is only
needed when using negation (`!=`) or `if-then-else` patterns where the
distinction between "missing" and "present but non-matching" matters.

**Bag merge semantics:** The `AttributeResolver` assembles bags by merging all
non-nil provider results. If multiple providers return the same key for the same
entity, the **last-registered provider's value wins**. Core providers register
before plugin providers (registration order: core first, then plugins in load
order). Key collision handling depends on the collision type:

- **Core-to-plugin collision** (a plugin overwrites a core attribute like
  `faction`): The server MUST reject the plugin's provider registration and
  continue startup without that plugin. The engine MUST log at ERROR level
  with the plugin ID and colliding attribute name. The plugin is disabled and
  operators are notified. The server remains operational with other plugins.
  Core attributes have semantic guarantees the engine depends on. Plugin
  providers MUST use namespaced keys (e.g., `guilds.faction`, not `faction`)
  to avoid overwriting core attributes.
- **Plugin-to-plugin collision** (two plugins write the same namespaced key):
  The resolver SHOULD warn at startup: `slog.Warn("attribute key collision",
  "key", "guilds.faction", "providers", []string{"guilds-v1", "guilds-v2"})`.
  The last-registered provider wins. This is a deployment configuration issue,
  not a security issue.

**Action bag construction:** The engine constructs `AttributeBags.Action`
directly from the `AccessRequest` — no provider is needed:

```go
AttributeBags.Action = map[string]any{
    "name": request.Action, // Always present
}
```

**Note:** The action bag currently contains only `name`. Conditions
referencing any other `action.*` attribute will evaluate to `false` (missing
attribute, fail-safe). Future attributes (e.g., `action.type`,
`action.scope`) MAY be added when use cases emerge.

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

**Enforcement:** The engine MUST use a context-value sentinel to detect
re-entrance on a per-goroutine basis. `Evaluate()` sets a sentinel key in
the context at entry and checks for its presence at the start of every
call. If the sentinel is already present, the call is re-entrant and
`Evaluate()` panics with a message identifying the re-entrance. This
approach supports concurrent `Evaluate()` calls from different goroutines
(required for the 120 checks/sec target at 200 users) while still
detecting true re-entrance (a provider calling back into the engine within
the same goroutine's call stack).

**Sentinel scope limitation:** The sentinel prevents synchronous re-entrance
on the same call stack only. Providers MUST NOT spawn goroutines during
attribute resolution. Spawning goroutines that perform I/O or call other
engine methods is explicitly prohibited. Cross-goroutine re-entrance is
prevented by convention and code review, not runtime enforcement.

**Provider context contract:** Providers **MUST** propagate the parent
context when creating sub-contexts for timeout isolation. Specifically,
providers **MUST** use `context.WithTimeout(ctx, ...)` (preserving parent
values) rather than `context.WithTimeout(context.Background(), ...)`
(which discards parent values and would bypass the re-entrance guard).
Violating this contract is a provider bug — the re-entrance guard will
not detect callbacks through a disconnected context, but such callbacks
also lose cancellation propagation and request-scoped logging, so they
are incorrect regardless.

```go
type evaluatingKey struct{}

func (e *engine) Evaluate(ctx context.Context, req AccessRequest) (Decision, error) {
    if ctx.Value(evaluatingKey{}) != nil {
        panic("re-entrant Evaluate() call detected — " +
            "an AttributeProvider is calling back into the engine")
    }
    ctx = context.WithValue(ctx, evaluatingKey{}, true)
    // ... proceed with resolution
}
```

**Provider design principles:**

1. Providers MUST only access repositories, never services
2. Providers MUST NOT perform authorization checks
3. If a provider needs authorization-gated data, the data model must be
   restructured to separate authorization metadata from business data
4. Providers resolve attributes unconditionally — the engine performs
   authorization AFTER resolution

**No-op methods:** Providers that only resolve one side (e.g.,
`CommandProvider` only resolves resources, `CharacterProvider` primarily
resolves subjects) SHOULD return `(nil, nil)` from the inapplicable method.
The resolver skips nil results during bag assembly.

### Core Attribute Schema

Each attribute provider contributes a defined set of attributes. Attributes
marked MUST always exist when the entity is valid; MAY attributes may be nil.

**CharacterProvider** (`character` namespace):

| Attribute  | Type     | Requirement | Description                                   |
| ---------- | -------- | ----------- | --------------------------------------------- |
| `type`     | string   | MUST        | Always `"character"`                          |
| `id`       | string   | MUST        | ULID of the character                         |
| `name`     | string   | MUST        | Character display name                        |
| `role`     | string   | MUST        | One of: `"player"`, `"builder"`, `"admin"`    |
| `faction`  | string   | MAY         | Faction affiliation (nil if unaffiliated)     |
| `level`    | float64  | MUST        | Character level (>= 0)                        |
| `flags`    | []string | MUST        | Arbitrary flags (empty array if none)         |
| `location` | string   | MUST        | ULID of current location (not a display name) |

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

| Attribute         | Type     | Requirement | Description                                                  |
| ----------------- | -------- | ----------- | ------------------------------------------------------------ |
| `type`            | string   | MUST        | Always `"property"`                                          |
| `id`              | string   | MUST        | ULID of the property                                         |
| `name`            | string   | MUST        | Property name                                                |
| `parent_type`     | string   | MUST        | Parent entity type                                           |
| `parent_id`       | string   | MUST        | Parent entity ULID                                           |
| `owner`           | string   | MAY         | Subject who created/set this property                        |
| `visibility`      | string   | MUST        | One of: "public", "private", "restricted", "system", "admin" |
| `flags`           | []string | MUST        | Arbitrary flags (empty array if none)                        |
| `visible_to`      | []string | MAY         | Character IDs (only when restricted)                         |
| `excluded_from`   | []string | MAY         | Character IDs (only when restricted)                         |
| `parent_location` | string   | MAY         | ULID of parent entity's location                             |

**EnvironmentProvider** (`env` namespace):

| Attribute     | Type    | Requirement | Description                            |
| ------------- | ------- | ----------- | -------------------------------------- |
| `time`        | string  | MUST        | Current time (RFC 3339)                |
| `hour`        | float64 | MUST        | Current hour (0-23, always UTC)        |
| `minute`      | float64 | MUST        | Current minute (0-59, always UTC)      |
| `day_of_week` | string  | MUST        | Day name (e.g., `"monday"`, lowercase) |
| `maintenance` | bool    | MUST        | Whether server is in maintenance mode  |

**Timezone:** All time attributes (`hour`, `minute`, `day_of_week`) are always
UTC. Game-world time zones and player-local time are explicitly out of scope
for this phase. MUSH games are inherently social environments where "night"
means different things to players in different timezones — the UTC convention
avoids embedding timezone assumptions into policies. If a future phase adds
in-game time (e.g., a day/night cycle), it SHOULD use a separate attribute
(e.g., `env.game_hour`) rather than overloading these values. Policies that
need local-time semantics MUST account for the UTC offset explicitly (e.g.,
`env.hour >= 22` for 10 PM UTC, not local time).

**Note:** `game_state` was considered but is not included — HoloMUSH does not
currently have a game state management system. This attribute MAY be added in a
future phase when game state is implemented.

**ObjectProvider** (`object` namespace):

| Attribute  | Type     | Requirement | Description                           |
| ---------- | -------- | ----------- | ------------------------------------- |
| `type`     | string   | MUST        | Always `"object"`                     |
| `id`       | string   | MUST        | ULID of the object                    |
| `name`     | string   | MUST        | Object display name                   |
| `location` | string   | MUST        | ULID of containing location           |
| `owner`    | string   | MAY         | Subject who owns this object          |
| `flags`    | []string | MUST        | Arbitrary flags (empty array if none) |

**StreamProvider** (`stream` namespace):

Streams do not have a dedicated database table — their attributes are derived
from the stream ID format (`location:01XYZ`, `character:01ABC`). The provider
parses the stream ID and extracts relevant fields.

| Attribute  | Type   | Requirement | Description                                  |
| ---------- | ------ | ----------- | -------------------------------------------- |
| `type`     | string | MUST        | Always `"stream"`                            |
| `name`     | string | MUST        | Full stream path (e.g., `"location:01XYZ"`)  |
| `location` | string | MAY         | Extracted location ULID (if location stream) |

**CommandProvider** (`command` namespace):

Commands are resolved from the command registry (Epic 6). The provider looks up
the command definition by name.

**Multi-word command names:** Commands with subcommands (e.g., `policy test`,
`policy create`) are registered in the command registry as the full multi-word
name. The resource string uses this full name: `command:policy test`. The
`CommandProvider` resolves `resource.name` as the full registered name. Policies
SHOULD use `resource.name like "policy*"` to match all subcommands, or
`resource.name == "policy test"` for specific subcommands.

| Attribute | Type   | Requirement | Description                  |
| --------- | ------ | ----------- | ---------------------------- |
| `type`    | string | MUST        | Always `"command"`           |
| `name`    | string | MUST        | Command name (e.g., `"say"`) |

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
             | attribute_root "has" identifier { "." identifier }
             | "!" condition
             | "(" conditions ")"
             | "if" condition "then" condition "else" condition
             | expr                                  (* bare boolean: true, false, or boolean attribute *)

expr       = attribute_ref | literal
attribute_ref = ("principal" | "resource" | "action" | "env") "." identifier { "." identifier }

attribute_root = "principal" | "resource" | "action" | "env"

(* Note: The `has` production uses `attribute_root` as the left operand,
   restricting it to entity references. Expressions like `5 has foo` are
   rejected at parse time. The `attribute_root` non-terminal is defined
   separately from `attribute_ref` to emphasize the semantic difference:
   `has` tests for attribute existence, which applies to entity roots
   (`principal`, `resource`, `action`, `env`) and their nested paths.
   Both simple (`principal has role`) and dotted paths
   (`resource has metadata.tags`) are valid. `has` expressions return a
   boolean value and participate in `&&`/`||` chains like any other
   condition. Parenthesized forms are valid:
   `(principal has faction) && (resource has metadata.restricted)`. *)

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

(* Reserved words — MUST NOT appear as attribute names or path segments:
   permit, forbid, when, principal, resource, action, env, is, in, has,
   like, true, false, if, then, else, containsAll, containsAny.
   The parser SHOULD produce a clear error: "reserved word X cannot be
   used as an attribute name." *)

(* Whitespace, including newlines, is insignificant within policy text.
   The `policy create` command collects multi-line input until "." on a
   line by itself. *)

(* The parser SHOULD enforce a maximum nesting depth of 32 levels for
   conditions, rejecting deeply nested policies with a clear error. This
   prevents stack overflow during evaluation from naive or malicious input. *)
```

**Parser disambiguation:** The `condition` production is ambiguous at the `expr`
alternative — when the parser encounters `principal.faction`, it cannot know
from the grammar alone whether this is a bare boolean expression or the start of
`expr comparator expr`. The parser MUST use one-token lookahead to disambiguate:
after parsing an `expr`, if the next token is a comparator (`==`, `!=`, `>`,
`>=`, `<`, `<=`), `in`, `like`, `has`, or `.` followed by `containsAll`/
`containsAny`, treat it as the corresponding compound condition; otherwise treat
it as a bare boolean. This makes the grammar LL(1) at the implementation level.

**Bare boolean deprecation:** While the grammar allows bare boolean
expressions (e.g., `when { principal.restricted }` without `== true`), the
implementation SHOULD emit a `ValidationWarning` when a bare boolean is
used, recommending the explicit form `principal.restricted == true`. This
avoids two ways to express the same condition and prevents future reserved
word collisions (if `restricted` became a keyword, `principal.restricted`
as a bare expression would be ambiguous). The explicit form is always
unambiguous.

**Operator precedence** (highest to lowest):

| Precedence | Operator(s)                      | Associativity |
| ---------- | -------------------------------- | ------------- |
| 1          | `.` (attribute access)           | Left          |
| 2          | `!` (boolean NOT)                | Right (unary) |
| 3          | `has`, `in`, `like`              | Non-assoc     |
| 4          | `==`, `!=`, `>`, `>=`, `<`, `<=` | Non-assoc     |
| 5          | `containsAll`, `containsAny`     | Non-assoc     |
| 6          | `&&` (boolean AND)               | Left          |
| 7          | `\|\|` (boolean OR)              | Left          |
| 8          | `if-then-else`                   | Right         |

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
  boundaries. The `:` character is passed as the separator argument
  to `glob.Compile(pattern, ':')`, which prevents `*` from matching across `:`
  boundaries. This is consistent with the existing `StaticAccessControl`
  permission matching. Current resource strings use a single `:` separator
  (`location:01ABC`, `character:01XYZ`, `object:01DEF`), but the separator
  semantics support future multi-segment resource names if needed. Examples:
  - `"location:*"` matches `"location:01ABC"` — single-segment wildcard
  - `"location:*"` does NOT match `"location:sub:01ABC"` — `*` stops at `:`
  - `"*:01ABC"` matches `"location:01ABC"` — prefix wildcard
  - `"*:01ABC"` does NOT match `"character:01ABC"` if the resource string
    has additional segments (it does match here because there is no second `:`)
    The DSL evaluator tests **MUST** verify this separator behavior explicitly,
    including edge cases with current single-segment and potential future
    multi-segment resource formats.
- `action` is a valid attribute root in conditions, providing access to the
  `AttributeBags.Action` map (e.g., `action.name`). Action matching in the
  target clause covers most use cases, but conditions MAY reference action
  attributes when needed.
- `resource == string_literal` in the target clause pins a policy to a specific
  resource instance (e.g., `resource == "object:01ABC"`). This is **early
  filtering** — policies with target-level pinning are excluded from the
  candidate set unless the resource matches. In contrast, `resource.id ==
  "object:01ABC"` in a **condition** is late filtering — the policy enters the
  candidate set, then the condition is evaluated against the attribute bags.
  Prefer target-level pinning for fixed-resource policies (better performance,
  clearer intent). Lock-generated policies use target-level pinning.
  Manually authored policies SHOULD prefer `resource is type_name` with
  conditions for flexibility. The `principal_clause` and `action_clause`
  intentionally lack `==` forms. For principal-specific matching, use conditions
  instead: e.g., `when { principal.id == "character:01ABC" }` rather than a
  hypothetical `principal == "character:01ABC"` target clause.
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
- **Future: target-level parent type matching.** Property policies frequently
  filter by `resource.parent_type` in conditions (e.g.,
  `when { resource.parent_type == "character" }`). A future grammar extension
  MAY add `resource is property(character)` or similar syntax for target-level
  filtering by parent type, improving policy filtering performance and intent
  clarity. For MVP, use conditions.
- **Deferred: entity references.** Cedar defines `entity_ref` syntax
  (`Type::"value"`) for hierarchy membership checks (e.g.,
  `principal in Group::"admins"`). This is NOT included in the initial grammar.
  The parser MUST reject `Type::"value"` syntax with a clear error message
  directing admins to use attribute-based group checks
  (`principal.flags.containsAny(["admin"])`) instead. Entity references MAY be
  added in a future phase when group/hierarchy features are implemented.

### Grammar Versioning

The `compiled_ast` JSONB stored in `access_policies` MUST include a
`grammar_version` field (initially `1`). This enables non-breaking grammar
evolution:

- **Forward compatibility:** The engine evaluates policies using the grammar
  version recorded in their AST. During a migration window, the engine
  supports both version N and N+1 simultaneously.
- **Migration:** The `policy recompile-all` admin command recompiles every
  policy's `dsl_text` with the current grammar version, updating the stored
  AST. Policies that fail recompilation are logged and left at their
  original version.
- **Audit preservation:** Because `dsl_text` (source of truth) and
  `compiled_ast` are both stored, historical audit log entries remain valid
  — the DSL text is human-readable regardless of grammar version, and the
  AST records the version used at evaluation time.
- **Version bump criteria:** A grammar version increment is required when a
  change alters parsing behavior for existing valid input (new operators,
  changed precedence, new reserved words). Additive changes that do not
  affect existing policies (e.g., new built-in functions) do NOT require a
  version bump.

### Type System

The DSL uses dynamic typing with fail-safe behavior on type mismatches:

| Scenario                                 | Behavior                       |
| ---------------------------------------- | ------------------------------ |
| Attribute missing (any operator)         | Condition evaluates to `false` |
| Type mismatch (e.g., string > int)       | Condition evaluates to `false` |
| `>`, `>=`, `<`, `<=` on non-number       | Condition evaluates to `false` |
| `containsAll`/`containsAny` on non-array | Condition evaluates to `false` |
| `has` on non-existent attribute          | Returns `false`                |
| `==`/`!=` across types                   | Condition evaluates to `false` |

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
| `has`          | Attribute exists | `principal has faction`, `principal has reputation.score`    |
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

// NOTE: principal.role != "admin" evaluates to false when role is missing
// (Cedar-aligned semantics). Characters without a role are NOT denied by this
// forbid but are denied by default-deny (no permit matches them either).
// The security outcome is the same; this forbid targets non-admin roles.
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

**Intentional coupling:** Properties embed access control metadata (`owner`,
`visibility`, `visible_to`, `excluded_from`) directly in the world model struct.
This is an intentional architectural tradeoff — properties are the ONLY world
entity with first-class access control fields. Other entities (locations,
objects) rely on external policies. The coupling exists because property
visibility is a core gameplay feature (players configure it directly), not just
an admin concern.

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
| `visible_to`    | []string | Character IDs allowed to read (restricted, max 100)      |
| `excluded_from` | []string | Character IDs denied from reading (max 100)              |

### Visibility Levels

| Visibility   | Who can see?        | visible_to/excluded_from |
| ------------ | ------------------- | ------------------------ |
| `public`     | Anyone in same room | Not applicable (NULL)    |
| `private`    | Owner only          | Not applicable (NULL)    |
| `restricted` | Explicit list       | Defaults: [self], []     |
| `system`     | System only         | Not applicable (NULL)    |
| `admin`      | Admins only         | Not applicable (NULL)    |

**Public visibility and movement:** Public visibility on character properties
means the property is visible to characters in the same location as the owning
character. As the owning character moves, the set of characters who can see
their public properties changes accordingly. The `parent_location` attribute
is resolved at evaluation time from the character's current location. If the
parent entity has no valid location (e.g., a character in the lobby before
entering a room), `parent_location` is nil and location-based visibility
policies fail-safe (deny).

When visibility is set to `restricted`, the Go property store MUST auto-populate
`visible_to` with `[parent_id]` and `excluded_from` with `[]` if they are nil.
This prevents the "nobody can see it" footgun.

**List size limits:** `visible_to` and `excluded_from` are capped at 100 entries
each. The property store MUST reject updates that would exceed this limit. For
access control involving larger groups, admins SHOULD use flag-based policies
(e.g., `principal.flags.containsAny(["guild-members"])`) rather than listing
individual character IDs. This prevents linear-scan performance degradation
during `principal.id in resource.visible_to` evaluation.

**List overlap prohibition:** A character ID MUST NOT appear in both
`visible_to` and `excluded_from` for the same property. The property store
MUST reject updates that would create this overlap. Without this constraint,
adding a character to `visible_to` has no observable effect if they are already
in `excluded_from` (deny-overrides means the `forbid` policy on
`excluded_from` always wins), creating confusing UX for property owners.

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

**Dependency chain:** Resolving `parent_location` requires knowing the parent
entity's ultimate containing location. To avoid a second code path for
world-model queries, `PropertyRepository.GetByID()` MUST resolve
`parent_location` in the same query. The `PropertyProvider` reads
`parent_location` from the repository result — no separate `LocationLookup`
function or direct DB query is needed. This keeps a single code path for all
location data: `PropertyProvider → PropertyRepository → DB`.

The repository determines the resolution strategy based on `parent_type`:

- `character` → JOIN `characters` table for current `location_id`
- `location` → use `parent_id` directly (the location IS the parent)
- `object` → resolution depends on the object's placement column. The
  `objects` table has `location_id`, `held_by_character_id`, and
  `contained_in_object_id` columns — exactly one is non-NULL (a world model
  invariant enforced by `WorldService`). Resolution strategy:
  - **Direct location:** If `location_id` is non-NULL, use it directly.
  - **Held by character:** If `held_by_character_id` is non-NULL, JOIN
    through the `characters` table to get the character's current
    `location_id`. The object's location is its holder's location.
  - **Contained in object:** If `contained_in_object_id` is non-NULL,
    recursive CTE traversal of the containment hierarchy. Objects support
    nested containment (chest inside a room, gem inside the chest). The
    repository MUST walk up the `contained_in_object_id` chain until reaching
    an object with a non-NULL `location_id` or `held_by_character_id`. The
    existing `checkCircularContainmentTx` already uses recursive CTEs for
    this pattern.
  - **Orphaned objects:** If no placement column is non-NULL (data
    corruption), `parent_location` is nil and location-based visibility
    policies fail-safe (deny).

  The recursive CTE MUST include both a depth limit (e.g., 20 iterations) and
  cycle detection (tracking visited IDs via an array path column, rejecting
  IDs already in the path) as defense-in-depth against data corruption.
  PostgreSQL `WITH RECURSIVE` does not automatically prevent cycles.
  If cycles are detected or the depth limit is exceeded, `parent_location` is
  nil and location-based visibility policies fail-safe (deny).

  If the recursive CTE encounters a PostgreSQL error (timeout, resource
  exhaustion), the PropertyProvider **MUST** propagate this as a core provider
  error (return `(nil, err)`), triggering EffectDefaultDeny with error
  propagation. Operators **SHOULD** monitor for CTE timeout errors and
  investigate data model integrity.

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

Callers SHOULD log the error and treat the response as denied (fail-closed).
The audit log records the `error_message` field for these cases.

**Plugin provider errors:** The engine logs an error via slog and continues
evaluation with the remaining providers. Missing plugin attributes cause
conditions referencing them to evaluate to `false` (fail-safe). The audit log
records plugin provider errors in a `provider_errors` JSONB field to aid
debugging "why was I denied?" investigations. The field is an array of error
objects with schema: `[{"namespace": "string", "error": "string", "timestamp":
"RFC3339", "duration_us": "int"}]`. For example:
`[{"namespace": "reputation", "error": "connection refused", "timestamp":
"2026-02-06T12:00:00Z", "duration_us": 1500}]`. Logging is rate-limited to 1
error per minute per `(namespace, error_hash)` tuple to control spam while
preserving visibility of distinct failure modes. If a provider has two
different error types (e.g., DB timeout and network error), both are logged
independently. The rate limiter uses a bounded LRU cache keyed by namespace
and error message hash.

```text
slog.Error("plugin attribute provider failed",
    "namespace", provider.Namespace(), "error", err)
```

**Provider health monitoring:** In addition to per-error rate-limited
logging, the engine **SHOULD** export Prometheus counter metrics per
provider:
`abac_provider_errors_total{namespace="reputation",error_type="timeout"}`.
This provides aggregate visibility into chronic provider failures that
individual log entries cannot. Implementation **SHOULD** include a circuit
breaker per provider with the following parameters:

| Parameter          | Default | Description                              |
| ------------------ | ------- | ---------------------------------------- |
| Failure threshold  | 10      | Consecutive errors to open the circuit   |
| Open duration      | 30s     | Time to skip provider while circuit open |
| Half-open attempts | 1       | Single probe request to test recovery    |

When the circuit opens, the engine logs at WARN level with the provider
namespace and failure count. During the open period, the provider is
skipped (attributes missing, conditions fail-safe). After the open
duration, a single "half-open" probe request tests whether the provider
has recovered. On success, the circuit closes (INFO log); on failure, it
re-opens for another cycle.

**Error classification:** All errors result in fail-closed (deny) behavior.
No errors are retryable within a single `Evaluate()` call.

| Error Type                | Fail Mode              | Caller Action                                             |
| ------------------------- | ---------------------- | --------------------------------------------------------- |
| Core provider failure     | Deny + return error    | Log and deny; callers inspect `err`                       |
| Plugin provider failure   | Deny (conditions fail) | Automatic — plugin attrs missing                          |
| Policy compilation error  | Deny + return error    | Should not occur (compiled at store time)                 |
| Corrupted compiled policy | Deny + skip policy     | Log CRITICAL, disable in cache, continue eval (see below) |
| Session not found         | Deny + return error    | Log, deny, session likely expired                         |
| Character deleted         | Deny + return error    | Log, deny, invalidate session                             |
| Context cancelled         | Deny + return error    | Request was cancelled upstream                            |

**Corrupted compiled policy** means the `CompiledPolicy` JSONB fails to
unmarshal into the AST struct, or the unmarshaled AST violates structural
invariants (e.g., a binary operator node missing a required operand). This
should not occur in normal operation because policies are compiled at
`PolicyStore.Create()` time. Corruption indicates data-level issues
(direct DB edits, storage failures). When detected, the engine:

1. Logs at CRITICAL level with the policy name and corruption details
2. Sets `enabled = false` on the policy row in the database (persisted,
   not just in-memory) to prevent the corrupted policy from being
   reloaded on subsequent cache refreshes
3. Continues evaluating remaining policies

**Recovery:** The `policy repair <name>` admin command (part of the
policy management CLI) re-compiles the policy from its `dsl_text` column,
overwrites the `compiled_ast` JSONB, and re-enables the policy. If
`dsl_text` is also corrupted, the operator must `policy edit <name>` to
provide corrected DSL text. After repairing or disabling the corrupted
policy, use `policy clear-degraded-mode` to restore normal evaluation.
The `policy test --suite` validation workflow **SHOULD** include a
corruption detection pass that unmarshals every `compiled_ast` and
verifies structural invariants.

**Security note (Degraded Mode):** If a corrupted policy has effect
`forbid` or `deny`, silently skipping it creates a security gap. When such
a policy is detected as corrupted, the engine **MUST** enter **degraded
mode** by setting a global flag (`abac_degraded_mode` boolean) that
persists until administratively cleared. In degraded mode:

- All access evaluation requests where the subject is **not** an admin
  receive `EffectDefaultDeny` without evaluating any policies
- Admin subjects (characters with `admin` role or equivalent) bypass the
  degraded mode check and undergo normal policy evaluation
- The CRITICAL log entry **MUST** include the policy name, effect, and
  degraded mode activation message
- A Prometheus gauge `abac_degraded_mode` (0=normal, 1=degraded) **MUST**
  be exposed for alerting

**Recovery:** The `policy clear-degraded-mode` admin command clears the
degraded mode flag and allows normal evaluation to resume. Operators
**SHOULD** configure alerting on CRITICAL-level ABAC log entries and the
`abac_degraded_mode` gauge. Policies with effect `permit` do **not**
trigger degraded mode when corrupted, as skipping a permit policy defaults
to deny, which is fail-safe.

Callers of `AccessPolicyEngine.Evaluate()` can distinguish "denied by
policy" (`err == nil, Decision.Effect == EffectDeny`) from "system failure"
(`err != nil, Decision.Effect == EffectDefaultDeny`). Both result in access
denied, but callers MAY handle system failures differently (e.g., retry,
alert).

## Evaluation Algorithm

```text
Evaluate(ctx, AccessRequest{Subject, Action, Resource})
│
├─ 1. System bypass
│    subject == "system" → return Decision{Allowed: true, Effect: SystemBypass}
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
└─ 6. Audit (when mode != off)
     ├─ Log denials (forbid + default deny) in denials_only and all modes
     ├─ Log allows only in all mode
     └─ Include: decision, matched policies, attribute snapshot
```

### Key Behaviors

- **Missing attributes:** If a condition references an attribute that does not
  exist, the condition evaluates to `false`. A missing attribute can never grant
  access (fail-safe).
- **No short-circuit (default):** The engine evaluates all candidate policies
  so the `Decision` records all matches. This powers `policy test` debugging.
  Implementations MAY optimize by short-circuiting after the first satisfied
  `forbid` policy when audit mode is `denials_only` and no `policy test`
  command is active, provided the triggering forbid is still recorded in
  `Decision.Policies`. Full evaluation MUST be used when `policy test` is
  active or audit mode is `all`.
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

| Metric                              | Target | Notes                             |
| ----------------------------------- | ------ | --------------------------------- |
| `Evaluate()` p99 latency (cold)     | <5ms   | First call in request             |
| `Evaluate()` p99 latency (warm)     | <3ms   | Cached attributes from prior call |
| Attribute resolution (cold)         | <2ms   | All providers combined            |
| Attribute resolution (warm, cached) | <100μs | Map lookup only                   |
| DSL condition evaluation            | <1ms   | Per policy                        |
| Cache reload                        | <50ms  | Full policy set reload on NOTIFY  |

**Benchmark scenario:** Targets assume 50 active policies (25 permit, 25
forbid), average condition complexity of 3 operators per policy, 10 attributes
per entity (subject + resource). "Cold" means first `Evaluate()` call in a
request (no per-request cache). "Warm" means subsequent calls reusing the
per-request `AttributeCache`. Implementation MUST include both
`BenchmarkEvaluate_ColdCache` and `BenchmarkEvaluate_WarmCache` tests.

**Worst-case bounds:**

| Scenario                             | Bound  | Handling                                  |
| ------------------------------------ | ------ | ----------------------------------------- |
| All 50 policies match (pathological) | <10ms  | Linear scan is acceptable at this scale   |
| Provider timeout                     | 100ms  | Context deadline; return deny + error     |
| Cache miss storm (post-NOTIFY flood) | <100ms | Lock during reload; stale reads tolerable |
| Plugin provider slow                 | 50ms   | Per-provider context deadline             |
| 32-level nested if-then-else         | <5ms   | Recursive evaluator with depth limit      |
| 20-level containment CTE             | <10ms  | Recursive SQL with depth limit            |
| Provider starvation (80ms + 80ms)    | 100ms  | Second provider gets cancelled context    |

Implementation MUST include benchmark tests for these pathological cases.
The 32-level nesting and 20-level containment scenarios MUST be included in
`BenchmarkEvaluate_WorstCase` as acceptance criteria. Provider starvation
(one slow provider consuming most of the 100ms budget) MUST be tested to
verify subsequent providers receive cancelled contexts and return promptly.

The `Evaluate()` context MUST carry a 100ms deadline. If attribute resolution
exceeds this, the engine returns `EffectDefaultDeny` with a timeout error.

**Provider resolution is sequential.** Core providers are called in registration
order, then plugin providers. This is a deliberate choice: sequential resolution
enables deterministic merge semantics (last-registered provider wins on key
collisions), simpler error attribution (the failing provider is immediately
identifiable), and straightforward debugging (provider order in audit logs
matches registration order). At ~200 concurrent users with providers each
completing in <1ms, parallel resolution would save negligible latency while
complicating the merge strategy and error handling. If profiling shows provider
resolution as a bottleneck (unlikely at this scale), parallel resolution MAY
be introduced — this would require changing the merge semantics from
"last-registered wins" to a priority-based merge, since goroutine completion
order is non-deterministic.

The 100ms deadline is the total budget for all providers combined.
To prevent priority inversion (where early providers starve later ones),
the engine **MUST** enforce per-provider timeouts within the total budget.
Each provider receives an **equal-share timeout** calculated as
`total_budget / provider_count`, distributed at evaluation start. For
example, with 5 providers and a 100ms total budget, each provider receives
20ms regardless of order or prior provider execution time. Unused time
from a fast provider is **not** redistributed to later providers.

**Tradeoff:** Equal-share prevents priority inversion but may underutilize
the time budget. For instance, if providers A, B, C each complete in 5ms
(15ms total), the remaining 85ms is wasted. This is acceptable because:

1. Core providers (registered first during server startup) no longer
   squeeze plugin providers that register later
2. Predictable per-provider budgets simplify debugging and monitoring
3. The 100ms total budget is conservative for typical attribute lookups

**Example calculation:**

- Total budget: 100ms, 4 providers (core, plugin A, plugin B, plugin C)
- Each provider timeout: `100ms / 4 = 25ms`
- Provider timings: core=5ms, plugin A=10ms, plugin B=25ms (timeout),
  plugin C=15ms
- Total evaluation time: 5+10+25+15 = 55ms (45ms unused)

If the parent context is cancelled during evaluation (e.g., client
disconnect), all remaining providers receive the cancelled context
immediately and the evaluation terminates with `EffectDefaultDeny`.
Equal-share timeout allocation applies only when the parent context is
active.

The engine wraps each provider call with
`context.WithTimeout(ctx, perProviderTimeout)` before calling
`ResolveSubject` or `ResolveResource`. If a provider exceeds its
per-provider timeout, it is cancelled individually and treated as a
plugin provider failure (logged, attributes missing, conditions
fail-safe). The overall 100ms deadline on `Evaluate()` still applies as
a hard backstop. A slow or misbehaving plugin cannot block the entire
evaluation pipeline because both the per-provider and overall deadlines
expire regardless of what the current provider is doing.

**Operational limits:**

| Limit                        | Value | Rationale                       |
| ---------------------------- | ----- | ------------------------------- |
| Maximum registered providers | 20    | 8 core + 12 plugins (headroom)  |
| Maximum active policies      | 500   | Linear scan acceptable at scale |
| Maximum condition nesting    | 32    | Prevents stack overflow         |
| Provider timeout (total)     | 100ms | Hard deadline on `Evaluate()`   |
| Provider timeout (per)       | fair  | `remaining_budget / remaining`  |

**Measurement strategy:**

- Export a Prometheus histogram metric for `Evaluate()` latency
  (e.g., `abac_evaluate_duration_seconds`)
- Add `BenchmarkEvaluate_*` tests with targets as failure thresholds (CI
  fails if benchmarks regress >10% from baseline)
- Staging monitoring alerts on p99 > 10ms (2x target)
- Implementation SHOULD add `slog.Debug()` timers in `engine.Evaluate()` for
  attribute resolution, policy filtering, condition evaluation, and audit
  logging to enable performance profiling during development
- Monitoring SHOULD export per-provider latency metrics (not just aggregate
  `Evaluate()` latency) to identify slow providers independently
- Monitoring SHOULD export `policy_cache_last_update` gauge (Unix timestamp) to
  verify the LISTEN/NOTIFY connection is alive and detect cache staleness
- Monitoring SHOULD export per-policy evaluation counts
  (`abac_policy_evaluations_total{name, effect}`) to identify hot policies
- Operators SHOULD configure alerting when
  `time.Now() - policy_cache_last_update > cache_staleness_threshold` to detect
  prolonged LISTEN/NOTIFY disconnections

**`Decision.Policies` allocation note:** The `Policies []PolicyMatch` slice is
populated for every `Evaluate()` call to support `policy test` debugging. At 50
policies per evaluation and 120 evaluations/sec, this produces ~6,000
`PolicyMatch` allocations/sec. If benchmarking shows allocation pressure,
consider lazy population: only populate `Decision.Policies` when audit mode is
`all` or the caller explicitly requests it (e.g., via a context flag set by
`policy test`). For `denials_only` mode, `Decision.PolicyID` and
`Decision.Reason` are sufficient.

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
- **Fault tolerance:** The cache stores the merged attribute bag per entity. A
  plugin provider failure during a first `Evaluate()` call produces a partial
  bag (missing that plugin's attributes). Subsequent `Evaluate()` calls in the
  same request reuse the cached (partial) bag — the failing provider is NOT
  retried. This is correct because provider failures are not transient within a
  single request, and the fail-safe missing-attribute semantics ensure conditions
  referencing the absent plugin attributes evaluate to `false`.

**Request duration guidance:** The per-request cache assumes request
processing completes within milliseconds. Request handlers SHOULD complete
within 1 second. For long-running operations (batch processing, multi-step
commands exceeding 1s), callers SHOULD create a fresh context with a new
`AttributeCache` rather than reusing a stale cache. The cache is designed
for single-user command execution latencies (~10ms), not batch workloads.

```go
// AttributeCache provides per-request attribute caching.
// Attach to context via WithAttributeCache(ctx) at the request boundary.
type AttributeCache struct {
    mu    sync.RWMutex
    items map[cacheKey]map[string]any
}

type cacheKey struct {
    entityType string // "character", "location", etc.
    entityID   string // "01ABC"
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

**Multi-step commands:** For commands that modify an entity AND then check
access to the modified state (e.g., a builder command that moves a character
and checks their access to the new location), callers SHOULD call
`WithAttributeCache(ctx)` again to create a fresh cache for the post-
modification checks. This pattern is documented here rather than enforced
by the cache itself, since most commands do not modify and re-check.

**Future optimization:** If profiling shows cache misses dominate, consider a
short-TTL cache (100ms) for read-only attributes like character roles. This
requires careful invalidation and is deferred until profiling demonstrates the
need.

## Policy Storage

### Schema

All `id` columns use ULID format, consistent with project conventions.

```sql
CREATE TABLE access_policies (
    id           TEXT PRIMARY KEY,           -- ULID
    name         TEXT NOT NULL UNIQUE,
    description  TEXT,
    effect       TEXT NOT NULL CHECK (effect IN ('permit', 'forbid')),
    source       TEXT NOT NULL DEFAULT 'admin'
                 CHECK (source IN ('seed', 'lock', 'admin', 'plugin')),
    dsl_text     TEXT NOT NULL,
    compiled_ast JSONB NOT NULL,             -- Pre-parsed AST from PolicyCompiler
    enabled      BOOLEAN NOT NULL DEFAULT true,
    created_by   TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    version      INTEGER NOT NULL DEFAULT 1
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

-- Phase 7.1 MUST create this table with monthly range partitioning.
-- Retrofitting partitioning onto an existing table requires exclusive locks
-- and full table rewrites — at 10M rows/day in `all` mode, this becomes
-- impractical within days. Partition-drop purging is also far more efficient
-- than row-by-row DELETE. See "Audit Log Retention" section for partition
-- management (creation, detachment, and purging).
CREATE TABLE access_audit_log (
    id            TEXT PRIMARY KEY,         -- ULID
    timestamp     TIMESTAMPTZ NOT NULL DEFAULT now(),
    subject       TEXT NOT NULL,
    action        TEXT NOT NULL,
    resource      TEXT NOT NULL,
    effect        TEXT NOT NULL CHECK (effect IN ('allow', 'deny', 'default_deny', 'system_bypass')),
    policy_id     TEXT,
    policy_name   TEXT,
    attributes      JSONB,
    error_message   TEXT,
    provider_errors JSONB,                   -- e.g., [{"provider": "reputation", "error": "connection refused"}]
    duration_us     INTEGER                  -- evaluation duration in microseconds (for performance debugging)
);

-- Essential indexes only. The effect column doubles as the decision indicator:
-- allow = allowed, deny/default_deny = denied. No separate decision column needed.
CREATE INDEX idx_audit_log_timestamp ON access_audit_log(timestamp DESC);
CREATE INDEX idx_audit_log_subject ON access_audit_log(subject, timestamp DESC);
CREATE INDEX idx_audit_log_denied ON access_audit_log(effect, timestamp DESC)
    WHERE effect IN ('deny', 'default_deny');

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
CREATE INDEX idx_properties_owner ON entity_properties(owner) WHERE owner IS NOT NULL;
```

**Implementation note:** The `updated_at` column has no database trigger. The
Go property store MUST explicitly set `updated_at = now()` in all UPDATE
queries.

**Property lifecycle on parent deletion:** The `entity_properties` table uses
polymorphic references (`parent_type`, `parent_id`) without foreign key
constraints. To prevent orphaned properties, `WorldService` MUST delete child
properties when deleting parent entities:

- `WorldService.DeleteCharacter()` → `PropertyRepository.DeleteByParent("character", charID)`
- `WorldService.DeleteObject()` → `PropertyRepository.DeleteByParent("object", objID)`
- `WorldService.DeleteLocation()` → `PropertyRepository.DeleteByParent("location", locID)`

These deletions MUST occur in the same database transaction as the parent
entity deletion. `PropertyRepository.DeleteByParent(parentType, parentID)`
performs `DELETE FROM entity_properties WHERE parent_type = $1 AND parent_id = $2`.

**Orphan detection and cleanup:** Because `entity_properties` uses
polymorphic parent references without FK constraints, orphaned properties
can accumulate if a deletion path is added without calling
`DeleteByParent()`. Implementation **MUST** include the following
defense-in-depth measures:

1. **Background cleanup goroutine:** A goroutine running on a configurable
   timer (default: daily) **MUST** detect orphaned properties — rows where
   `parent_id` does not match any existing entity of the declared
   `parent_type`. Detected orphans are logged at WARN level on first
   discovery. After a grace period (default: 24 hours), orphans that
   persist across two consecutive runs **MUST** be actively deleted with
   a batch `DELETE` and logged at INFO level with the count of removed
   rows. The grace period prevents deleting properties whose parent is
   being recreated (e.g., during a migration or restore).

2. **Startup integrity check:** On server startup, the engine **MUST**
   count orphaned properties. If the count exceeds a configurable
   threshold (default: 100), the server **MUST** log at ERROR level but
   continue starting (not fail-fast) to avoid blocking recovery. The
   threshold alerts operators to systematic deletion bugs before
   orphans accumulate to problematic levels.

3. **Integration test coverage:** Each entity deletion path **MUST** have
   a corresponding integration test that verifies child properties are
   deleted in the same transaction.

This is Go-level cascading — no database triggers or FK constraints are
used, consistent with the project's "all logic in Go" constraint.

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

**Cache staleness threshold:** To limit the risk of serving stale policy
decisions during prolonged reconnection windows, the engine **MUST** support a
configurable `cache_staleness_threshold` (default: 5s). When the time since
the last successful cache update exceeds this threshold, the engine **MUST**
fail-closed for all non-admin subjects by returning `EffectDefaultDeny` without
evaluating policies. Admin subjects (characters with `admin` role or equivalent)
bypass the staleness check and undergo normal policy evaluation, ensuring
operators can still access the system during degraded conditions. The engine
**MUST** expose a Prometheus gauge `policy_cache_last_update` (Unix timestamp)
that is updated on every successful cache reload. Operators **SHOULD** configure
alerting when `time.Now() - policy_cache_last_update > cache_staleness_threshold`
to detect prolonged LISTEN/NOTIFY disconnections before non-admin access is
denied. Once the LISTEN/NOTIFY connection is restored and a full reload completes,
normal evaluation resumes automatically.

### Audit Log Serialization

The `effect` column in `access_audit_log` maps to the Go `Effect` enum:

| Go Constant          | DB Value          |
| -------------------- | ----------------- |
| `EffectAllow`        | `"allow"`         |
| `EffectDeny`         | `"deny"`          |
| `EffectDefaultDeny`  | `"default_deny"`  |
| `EffectSystemBypass` | `"system_bypass"` |

The `effect` column is the sole decision indicator — there is no separate
`decision` column. `allow` means the request was allowed; `deny` or
`default_deny` means it was denied.

The `Effect` type MUST serialize to the string values in the mapping table
above, not to `iota` integer values. Implementation SHOULD define a
`String() string` method on `Effect` that returns the DB-compatible string.

### Policy Version Records

A version record is created in `access_policy_versions` only when `dsl_text`
changes (via `policy edit`). Toggling `enabled` via `policy enable`/`disable` or
updating `description` modifies the main `access_policies` row directly without
creating a version record. The `version` column on `access_policies` increments
only on DSL changes.

**Policy rollback:** Implementation SHOULD include a `policy rollback <name>
<version>` admin command that restores a previous version's DSL text and
creates a new version record for the rollback. This avoids requiring admins
to manually reconstruct old policy text from the history output.

### Audit Log Configuration

The audit logger supports three modes, configurable via server settings:

```go
type AuditMode string

const (
    AuditOff        AuditMode = "off"          // No audit logging
    AuditDenialsOnly AuditMode = "denials_only" // Log deny + default_deny only
    AuditAll        AuditMode = "all"           // Log all decisions
)

type AuditConfig struct {
    Mode           AuditMode     // Default: AuditDenialsOnly
    RetainDenials  time.Duration // Default: 90 days
    RetainAllows   time.Duration // Default: 7 days (only relevant when Mode=all)
    PurgeInterval  time.Duration // Default: 24 hours
}
```

| Mode           | What is logged            | Typical use case            |
| -------------- | ------------------------- | --------------------------- |
| `off`          | Nothing                   | Development, performance    |
| `denials_only` | Deny + default_deny       | Production default          |
| `all`          | All decisions incl. allow | Debugging, compliance audit |

The default mode is `denials_only` — this balances operational visibility with
storage efficiency.

**Volume estimates:** At 200 concurrent users, typical MUSH activity generates
~0.15 commands/sec/user with ~4 `Evaluate()` calls per command (command
permission, location check, property reads). This produces ~120 checks/sec
total, or ~10M audit records/day in `all` mode. Each record includes a JSONB
`attributes` snapshot (~500 bytes). At 7-day `RetainAllows` retention, `all`
mode accumulates ~70M rows (~35GB). `denials_only` mode produces a small
fraction of this (most checks result in allows). The 7-day allow retention
and 24-hour purge interval are important to enforce in `all` mode to prevent
unbounded growth.

**System bypass auditing:** When audit mode is `all`, system subject bypasses
SHOULD also be logged with `effect = "system_bypass"` to provide a complete
audit trail. In `denials_only` mode, system bypasses are not logged.

**Async audit writes:** Audit log inserts use async writes via a buffered
channel. `Evaluate()` enqueues the audit entry to a channel, and a
background goroutine batch-writes to PostgreSQL. The channel has a
configurable buffer size (default TBD during implementation). When the
channel is full, the audit logger MUST increment the counter metric
`abac_audit_channel_full_total` and drop the entry. Audit logging is
best-effort and MUST NOT block authorization decisions.

If an audit log insert fails (missing partition, disk full, connection error),
the audit logger **MUST** log the failure at ERROR level but **MUST NOT**
propagate the error to the caller. The `Evaluate()` decision is returned
successfully — audit logging is best-effort and does not block authorization.
A counter metric `abac_audit_failures_total{reason}` **SHOULD** be incremented
to alert operators.

### Audit Log Retention

Audit records MUST be purged by a periodic Go background job. The purge
interval and retention periods are configurable via `AuditConfig`:

- **Denials:** Retained for 90 days (default)
- **Allows:** Retained for 7 days (default, only relevant in `all` mode)
- **Purge interval:** Every 24 hours (default)

**Partitioning:** In `all` mode, the audit table reaches 10M rows on day one
(~120 checks/sec × 86,400s). Even in `denials_only` mode, at a 5% denial
rate, 10M rows is reached in ~20 days. Phase 7.1 MUST create the
`access_audit_log` table with monthly range partitioning on the `timestamp`
column from the start. Retrofitting partitioning onto a populated table
requires exclusive locks and full table rewrites — at multi-million row
scale, this locks the table for hours. Partition-drop purging is also orders
of magnitude faster than row-by-row `DELETE`.

```sql
CREATE TABLE access_audit_log (
    -- columns as defined above
) PARTITION BY RANGE (timestamp);

-- Create initial partitions (3 months ahead)
CREATE TABLE access_audit_log_2026_02 PARTITION OF access_audit_log
    FOR VALUES FROM ('2026-02-01') TO ('2026-03-01');
CREATE TABLE access_audit_log_2026_03 PARTITION OF access_audit_log
    FOR VALUES FROM ('2026-03-01') TO ('2026-04-01');
CREATE TABLE access_audit_log_2026_04 PARTITION OF access_audit_log
    FOR VALUES FROM ('2026-04-01') TO ('2026-05-01');
```

**Partition management:** A background goroutine **MUST** manage partition
lifecycle on a configurable schedule (default: daily):

1. **Create future partitions:** Ensure at least 2 months of future
   partitions exist. If creation fails (e.g., disk full, permissions),
   log at ERROR level — inserts to a missing partition cause PostgreSQL
   errors that the audit logger handles gracefully (logged, not fatal).
2. **Drop expired partitions:** Use `DETACH PARTITION` followed by `DROP
   TABLE` for partitions older than the retention period. `DETACH` first
   allows backup before permanent deletion if needed.
3. **Health check integration:** The health endpoint **SHOULD** report
   the number of available future partitions. Alert if fewer than 2
   future partitions exist.

The purge job **MUST** create future partitions and detach/drop expired
partitions rather than issuing row-by-row `DELETE` statements.

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
> lock armory/enter = (faction:rebels | faction:alliance) & level:>=3
> unlock my-chest/read
```

**Resource target resolution:** The lock command resolves the resource target
(the part before `/`) using the same name resolution as other world commands:
`me` resolves to the character issuing the command, `here` resolves to their
current location, and object names (e.g., `my-chest`, `armory`) are looked up
in the world model using the standard object matching rules (exact name match,
then disambiguation if multiple objects share the name in the same location).
All targets are resolved to ULIDs at lock compile time — the generated policy
uses the resolved ULID, not the display name.

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

**Common lock expression patterns** (explicit parentheses recommended for
clarity):

```text
GOOD:  (faction:rebels | flag:ally) & level:>=3
AVOID: faction:rebels | flag:ally & level:>=3  # Equivalent but harder to read
GOOD:  !flag:banned & (faction:rebels | faction:neutrals)
AVOID: !flag:banned & faction:rebels | faction:neutrals  # NOT equivalent!
```

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

**Token name conflict resolution:** Duplicate token registrations between
plugins MUST cause a startup error, not just a warning. Non-deterministic
behavior from load-order-dependent last-registered-wins is operationally
dangerous — restarting the server could silently change lock semantics. The
error message MUST identify both plugins and the conflicting token name,
directing the operator to disable one plugin. Core-to-plugin collisions are
structurally prevented by the namespacing requirement below (core tokens are
un-namespaced; plugin tokens require a dot prefix). Core providers
(CharacterProvider) register tokens first. Plugin tokens MUST contain at
least one dot separator.
The segment before the first `.` MUST exactly match the plugin ID (e.g.,
plugin `reputation` registers `reputation.score`, plugin `crafting` registers
`crafting.type`). Abbreviations are not allowed. Dotless tokens (e.g.,
`reputation` without a dot) are rejected. Multi-segment tokens (e.g.,
`reputation.score.detailed`) are valid — only the first segment is checked.
The engine validates this at registration — plugin tokens without the correct
prefix or missing a dot separator are rejected with a startup error indicating
the expected namespace. To shift validation left to development time, plugin
test suites **SHOULD** include a test that loads the plugin manifest,
instantiates the `AttributeProvider`, calls `LockTokens()`, and verifies
that all returned tokens have the correct namespace prefix. This catches
naming violations in CI rather than at server startup in production.
The `policy create` command MUST reject names starting with reserved prefixes:
`seed:` (system seed policies) and `lock:` (lock-generated policies). Both
prefixes are reserved for system use. See [Naming conventions](#admin-commands)
for the complete list of reserved prefixes.

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

| Token              | Type       | Plugin ID    | Attribute Path     | Example                          |
| ------------------ | ---------- | ------------ | ------------------ | -------------------------------- |
| `reputation.score` | numeric    | `reputation` | `reputation.score` | `reputation.score:>=50`          |
| `crafting.primary` | equality   | `crafting`   | `crafting.primary` | `crafting.primary:blacksmithing` |
| `guilds.primary`   | equality   | `guilds`     | `guilds.primary`   | `guilds.primary:merchants`       |
| `crafting.certs`   | membership | `crafting`   | `crafting.certs`   | `crafting.certs:master-smith`    |

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
    principal is character,
    action in ["read"],
    resource == "object:01ABC..."
  ) when {
    resource.owner == principal.id
    && (principal.faction == "rebels" || "ally" in principal.flags)
    && principal.level >= 3
  };
```

**Ownership check requirement:** All lock-generated policies **MUST** include
`resource.owner == principal.id` in the condition block to prevent lock
policies from surviving ownership transfer. Without this check, a lock set by
the original owner would grant access to the lock's conditions even after the
resource is transferred to a new owner, creating a backdoor permit.

**System attribute immutability:** The attributes `level`, `role`, and
`faction` are **non-writable system attributes** managed by the core engine.
These cannot be modified by players or builders through attribute commands.
Plugin-provided attributes (e.g., `reputation.score`, `guilds.primary`) follow
plugin-specific write rules and **MAY** be mutable depending on plugin design.

**Validation rules:**

- Unknown token names MUST produce a clear error: `unknown lock token "foo"
  — available tokens: faction, flag, level, reputation.score, ...`
- Type mismatches MUST produce an error: `token "faction" expects a name, not a
  number`
- Numeric tokens with missing operators default to `==`
- Empty values MUST be rejected: `faction:` is invalid

#### Lock Token Discovery

Characters can discover available lock tokens via the `lock tokens` command:

```text
> lock tokens
Available lock tokens:
  faction:X             (equality)   — Character faction equals X
  flag:X                (membership) — Character has flag X
  level:OP N            (numeric)    — Character level (>=, >, <=, <, == N)
  reputation.score:OP N (numeric)    — Reputation score (plugin: reputation)
  guilds.primary:X      (equality)   — Primary guild membership (plugin: guilds)
```

The `lock tokens` command reads from the token registry at runtime. Plugin
tokens appear automatically when the plugin is loaded. A `--verbose` flag
SHOULD show the underlying DSL attribute path for debugging:

```text
> lock tokens --verbose
Available lock tokens:
  faction:X             — Character faction equals X
                          (maps to: principal.faction)
  reputation.score:OP N — Reputation score (plugin: reputation)
                          (maps to: principal.reputation.score)
```

**Access constraint:** Lock-generated policies can ONLY target resources the
character has write access to. The lock command MUST verify write permission
(via `Evaluate()`) before creating the policy. This means:

- Characters can lock their own properties and objects (they have write access)
- Builders can lock locations they have write access to (no ownership concept
  for locations — write access is sufficient)
- Commands and streams are NOT lockable — no character has write access to
  command or stream resources. Command permissions are admin-only policy targets
- A character can never write a lock that affects resources they cannot modify

**Lock policy lifecycle:**

- Lock policies are NOT versioned. Each `lock`/`unlock` creates or deletes a
  policy directly — no version history is maintained.
- `lock X/action = condition` calls `PolicyStore.Create()` with generated DSL
  text and the naming convention `lock:{type}:{id}:{action}` where `{type}` is
  the bare resource type without a trailing colon (e.g.,
  `lock:object:01ABC:read`). For property locks, the `{type}` is `property`
  and the `{id}` is the property's own ULID (e.g.,
  `lock:property:01DEF:read` for `lock me/backstory/read`). This naming
  format is safe because lockable resources (objects, properties, locations)
  all use ULID identifiers which contain no colons or spaces.
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
policy list [--enabled|--disabled] [--effect=permit|forbid] [--source=seed|lock|admin|plugin]
policy show <name>
policy create <name>
policy edit <name>
policy delete <name>
policy enable <name>
policy disable <name>
policy validate                                          (interactive multiline input)
policy test <subject> <action> <resource> [--verbose] [--json]
policy reload
policy history <name> [--limit=N]
policy audit [--subject=X] [--action=Y] [--effect=denied] [--last=1h] [--limit=N]
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

### policy validate

Validates DSL text without persisting a policy. Accepts multiline input like
`policy create`, runs the `PolicyCompiler`, and reports success or errors with
line/column information. Useful for iterating on policy text before committing
to a name.

```text
> policy validate
Enter policy (end with '.' on a line by itself):
permit(principal is character, action in ["read"], resource is location)
when { principal.level >= };
.
Error at line 2, column 27: expected expression after '>='
```

### policy test

Dry-run evaluation showing resolved attributes, matching policies, and the
final decision. Available to admins and builders (for debugging builds).

**Output format:** Human-readable text by default. The `--json` flag outputs
structured JSON for programmatic consumption. Attribute values exceeding 80
characters are truncated with `... (truncated)` in text mode.

**Visibility:** Builders see only policies whose target matches their test
query, not the full policy set. Admins see all matching policies.

**Attribute redaction for builders:** When a builder uses `policy test`,
attributes for entities the builder does not own **MUST** be redacted to
prevent information disclosure. Redacted attributes are displayed as
`<redacted>` in both text and JSON output. Only the entity type and ID are
shown. Admins see all attributes without redaction.

**Redaction rules:**

- **Subject redaction:** If the subject is a character the builder does not
  own, all attributes except `type` and `id` are redacted
- **Resource redaction:** If the resource is an object/location/scene the
  builder does not own, all attributes except `type` and `id` are redacted
- **Ownership determination:** The engine resolves `resource.owner` and
  `principal.id` from the attribute providers to determine ownership
- **Audit logging:** All `policy test` invocations **MUST** be logged to the
  audit log with `action=policy.test`, `subject=<invoking-character>`,
  `resource=<test-target>`, and metadata including `test_subject`,
  `test_action`, `test_resource`. This enables operators to detect abuse of
  `policy test` for reconnaissance.

**Redacted output example (builder testing a character they don't own):**

```text
> policy test character:01ADMIN enter location:01XYZ
Subject attributes:
  type=character, id=01ADMIN, <redacted>
Resource attributes:
  type=location, id=01XYZ, faction=rebels, restricted=true

Evaluating 2 matching policies:
  faction-hq-access    permit  CONDITIONS FAILED (unknown)
  level-gate           forbid  CONDITIONS FAILED (unknown)

Decision: DENIED (default deny — no policies matched)
```

**Full output example (admin or testing own character):**

```text
> policy test character:01ABC enter location:01XYZ
Subject attributes:
  type=character, id=01ABC, faction=rebels, level=7, role=player
Resource attributes:
  type=location, id=01XYZ, faction=rebels, restricted=true

Evaluating 2 matching policies:
  faction-hq-access    permit  MATCHED
  level-gate           forbid  CONDITIONS FAILED (principal.level < 5: false, level=7)

Decision: ALLOWED (faction-hq-access)
```

**Condition failure detail:** When `--verbose` is set, the output shows **all
failing sub-conditions** for each policy (not just the first failure). This
provides full diagnostic information. Each sub-condition shows the operator,
the resolved values, and the evaluation result. Without `--verbose`, only the
policy name, effect, and final result are shown.

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

**Scenario test files:** For comprehensive seed policy verification,
implementation SHOULD support a `policy test --suite <file>` mode that
reads test scenarios from a YAML file:

```yaml
scenarios:
  - name: "Player self-access"
    subject: "character:01PLAYER"
    action: "read"
    resource: "character:01PLAYER"
    expected: allow
  - name: "Player cannot read admin property"
    subject: "character:01PLAYER"
    action: "read"
    resource: "property:01ADMIN_SECRET"
    expected: deny
```

This enables batch verification of all seed policies in integration tests
and simplifies regression testing after policy changes.

### Command Permissions

```text
permit(principal is character, action in ["execute"], resource is command)
when { principal.role == "admin" && resource.name like "policy*" };

permit(principal is character, action in ["execute"], resource is command)
when { principal.role == "builder" && resource.name == "policy test" };
```

### policy reload

Forces an immediate full reload of the in-memory policy cache from PostgreSQL.
Available to admins only. Use this when the LISTEN/NOTIFY connection may be
down and an emergency policy change needs to take effect immediately, without
waiting for reconnection.

```text
> policy reload
Policy cache reloaded (42 active policies).
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

## Replacing Static Roles

**Design decision:** HoloMUSH has no production releases. The static
`AccessControl` system from Epic 3 is replaced entirely by `AccessPolicyEngine`.
There is no backward-compatibility adapter, no shadow mode, and no incremental
migration. All call sites switch to `Evaluate()` directly. This simplifies the
design and eliminates an entire class of complexity (normalization helpers,
migration adapters, shadow mode metrics, cutover criteria). See decisions #36
and #37 in the decisions log.

### Seed Policies

The seed policies define the default permission model. They use the ABAC
engine's full capabilities (attribute-based conditions, `enter` action for
location control) rather than replicating the static system's limitations.

```text
// seed:player-self-access
permit(principal is character, action in ["read", "write"], resource is character)
when { resource.id == principal.id };

// seed:player-location-read
permit(principal is character, action in ["read"], resource is location)
when { resource.id == principal.location };

// seed:player-character-colocation
permit(principal is character, action in ["read"], resource is character)
when { resource.location == principal.location };

// seed:player-object-colocation
permit(principal is character, action in ["read"], resource is object)
when { resource.location == principal.location };

// seed:player-stream-emit
permit(principal is character, action in ["emit"], resource is stream)
when { resource.name like "location:*" && resource.location == principal.location };

// seed:player-movement
// Intentionally unconditional — movement is allowed by default for all
// characters. Admins restrict specific locations via forbid policies
// (deny-overrides ensures forbid always wins over this permit).
permit(principal is character, action in ["enter"], resource is location);

// seed:player-basic-commands
permit(principal is character, action in ["execute"], resource is command)
when { resource.name in ["say", "pose", "look", "go"] };

// seed:builder-location-write
permit(principal is character, action in ["write", "delete"], resource is location)
when { principal.role in ["builder", "admin"] };

// seed:builder-object-write
permit(principal is character, action in ["write", "delete"], resource is object)
when { principal.role in ["builder", "admin"] };

// seed:builder-commands
permit(principal is character, action in ["execute"], resource is command)
when { principal.role in ["builder", "admin"]
    && resource.name in ["dig", "create", "describe", "link"] };

// seed:admin-full-access
permit(principal is character, action, resource)
when { principal.role == "admin" };
```

The comment preceding each policy IS the deterministic name used during
bootstrap (e.g., `seed:player-self-access`). Each name is prefixed with
`seed:` to prevent collision with admin-created policies.

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

**Seed policy upgrades:** Seed policies are immutable after first creation.
Server upgrades that ship updated seed text do NOT overwrite existing seeds.
This ensures admin customizations to seed policies (via `policy edit`) survive
upgrades. If a server version needs to fix a seed policy bug, it MUST use a
migration that explicitly updates the affected policy, logged as a version
change with a `change_note` explaining the upgrade.

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

### Implementation Sequence

1. **Phase 7.1 (Policy Schema):** Create DB tables and policy store.

2. **Phase 7.2 (DSL & Compiler):** Build DSL parser using
   [participle](https://github.com/alecthomas/participle) (struct-tag parser
   generator), evaluator, and `PolicyCompiler`. Participle is preferred over
   `goyacc` (requires separate `.y` grammar file and manual AST mapping) and
   hand-rolled recursive descent (more code, harder to maintain) because its
   struct-tag approach generates Go AST structs directly from grammar
   annotations, eliminating the mapping layer. Mandate fuzz testing for all
   parser entry points. Unit test with table-driven tests.

3. **Phase 7.3 (Policy Engine):** Build `AccessPolicyEngine`, attribute
   providers, and audit logger. Replace `AccessControl` with
   `AccessPolicyEngine` in dependency injection. Update all call sites to use
   `Evaluate()` directly.

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
| `internal/plugin/hostfunc/functions`     | Plugin host function auth           |
| `internal/core/broadcaster` (test)       | Test mock injection                 |

This list is derived from the current codebase. Run `grep -r "AccessControl"
internal/ --include="*.go"` to get the current inventory before starting
phase 7.3.

### Plugin Capability Migration

The current `capability.Enforcer` handles plugin permissions separately. Under
ABAC, plugin manifests become seed policies. The Enforcer is removed alongside
`StaticAccessControl` in phase 7.6.

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
  resolver_test.go      — Orchestrates multiple providers, timeout enforcement
  character_test.go     — Resolves character attrs from mock world service
  location_test.go      — Resolves location attrs from mock world service
  property_test.go      — Resolves property attrs including visibility/lists
  environment_test.go   — Time, maintenance mode

internal/access/policy/store/
  postgres_test.go      — CRUD, versioning, LISTEN/NOTIFY dispatch

internal/access/policy/audit/
  logger_test.go        — Mode control (off/denials/all), attribute snapshots
```

### DSL Evaluator Coverage

Table-driven tests MUST cover every operator with valid inputs, invalid inputs,
missing attributes, and type mismatches.

### Fuzz Testing

The DSL parser MUST include fuzz tests using Go's native fuzzing (`go test
-fuzz`). Parser bugs are security bugs — a malformed input that causes a panic,
infinite loop, or incorrect parse result could grant or deny access incorrectly.

```go
func FuzzParseDSL(f *testing.F) {
    // Seed with valid and near-valid policies
    f.Add(`permit(principal, action, resource);`)
    f.Add(`forbid(principal is character, action in ["read"], resource is location)
        when { principal.level >= 5 };`)
    f.Add(`permit(principal, action, resource) when { if principal has faction
        then principal.faction != "enemy" else false };`)
    f.Fuzz(func(t *testing.T, input string) {
        // Must not panic, must terminate within timeout
        _, _, _ = compiler.Compile(input)
    })
}
```

Fuzz targets SHOULD also cover the evaluator (random ASTs against random
attribute bags) and the lock expression parser. CI SHOULD run a short fuzz
cycle (30 seconds) on every build; extended fuzzing (hours) SHOULD run as a
scheduled nightly job. Crash-inducing inputs discovered by the scheduled
fuzzer MUST be added as regression test cases in the unit test suite.

**Fuzz corpus strategy:**

- **Seeds:** All example policies from this spec + all seed policies + known
  edge cases (empty input, max nesting, Unicode identifiers, reserved words
  as attribute names)
- **Structured mutation:** Valid DSL with randomized attribute paths, random
  operators, random nesting depths (up to 32), and random literal values
- **Crash storage:** Crash-inducing inputs MUST be stored in
  `testdata/fuzz_crashes/` and added as deterministic regression tests
- **Coverage target:** Fuzzing SHOULD achieve >80% code coverage of the
  parser and evaluator packages before being considered sufficient

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
        It("rejects locks on resources without write access", func() { ... })
        It("admin forbid overrides player lock permit", func() { ... })
    })

    Describe("Audit logging", func() {
        It("logs denials in denials_only mode", func() { ... })
        It("logs all decisions in all mode", func() { ... })
        It("logs nothing in off mode", func() { ... })
    })

    Describe("Cache invalidation via LISTEN/NOTIFY", func() {
        It("reloads policies when notification received", func() { ... })
    })
})
```

## Acceptance Criteria

- [ ] ABAC policy data model documented (subjects, resources, actions, conditions)
- [ ] Attribute schema defined for subjects (players, plugins, connections)
- [ ] Attribute schema defined for resources (objects, rooms, commands, properties)
- [ ] Environment attributes defined (time, maintenance mode)
- [ ] Policy DSL grammar specified with full expression language
- [ ] Policy storage format designed (PostgreSQL schema with versioning)
- [ ] Policy evaluation algorithm documented (deny-overrides, no priority)
- [ ] Audit log with configurable modes (off, denials-only, all)
- [ ] Plugin attribute contribution interface designed (registration-based)
- [ ] Admin commands documented for policy management
- [ ] Player lock system designed with write-access verification
- [ ] Lock syntax compiles to scoped policies
- [ ] Property model designed as first-class entities
- [ ] Direct replacement of StaticAccessControl documented (no adapter)
- [ ] Microbenchmarks (pure evaluator, no I/O): single-policy <10μs,
      50-policy set <100μs, attribute resolution <50μs (via `go test -bench`)
- [ ] Integration benchmarks (with real providers): `Evaluate()` p99 <5ms cold,
      <3ms warm, matching the performance targets in the spec
- [ ] Cache invalidation via LISTEN/NOTIFY reloads policies on change
- [ ] System subject bypass returns allow without policy evaluation
- [ ] Subject type prefix-to-DSL-type mapping documented
- [ ] Provider timeout and operational limits defined

## Future Commands (Deferred)

The following commands are not part of the MVP but are natural extensions of the
policy management system:

- **`policy diff <name> [<version>]`** — Show what changed between policy
  versions. Useful for audit investigation. Reads from `access_policy_versions`.
- **`policy export [--source=X]`** — Export policies as DSL text files for
  backup and environment promotion (dev → staging). The deterministic naming
  convention already supports round-trip export/import.
- **`policy import <file>`** — Import policies from a DSL text file. Validates
  each policy via `PolicyCompiler` before persisting. Existing policies with the
  same name are skipped unless `--overwrite` is specified.
- **`policy lint [<name>]`** — Semantic analysis of policies for common
  mistakes. Unlike `policy validate` (syntax only), lint checks for: negation
  without `has` guard (`principal.faction != "enemy"` without
  `principal has faction`), overly broad `forbid` policies without conditions,
  and `permit` policies logically subsumed by existing policies. Available to
  admins and builders.

## References

- [Design Decision Log](2026-02-05-full-abac-design-decisions.md) — Rationale
  for key design choices made during review
- [Core Access Control Design](2026-01-21-access-control-design.md) — Current
  static role implementation (Epic 3)
- [HoloMUSH Roadmap](../plans/2026-01-18-holomush-roadmap-design.md) — Epic 7
  definition
- [Cedar Language Specification](https://docs.cedarpolicy.com/) — DSL inspiration
- [Commands & Behaviors Design](2026-02-02-commands-behaviors-design.md) —
  Command system integration

### Related ADRs

- [ADR 0009: Custom Go-Native ABAC Engine](../adr/0009-custom-go-native-abac-engine.md)
- [ADR 0010: Cedar-Aligned Fail-Safe Type Semantics](../adr/0010-cedar-aligned-fail-safe-type-semantics.md)
- [ADR 0011: Deny-Overrides Without Priority](../adr/0011-deny-overrides-without-priority.md)
- [ADR 0012: Eager Attribute Resolution](../adr/0012-eager-attribute-resolution.md)
- [ADR 0013: Properties as First-Class Entities](../adr/0013-properties-as-first-class-entities.md)
- [ADR 0014: Direct Static Access Control Replacement](../adr/0014-direct-static-access-control-replacement.md)
- [ADR 0015: Three-Layer Player Access Control](../adr/0015-three-layer-player-access-control.md)
- [ADR 0016: LISTEN/NOTIFY Policy Cache Invalidation](../adr/0016-listen-notify-policy-cache-invalidation.md)
