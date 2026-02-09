<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

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
    schema *AttributeSchema // MUST be provided at engine init
}

// Compile parses DSL text, validates it, and returns a compiled policy.
// Returns a descriptive error with line/column information on parse failure.
// Validation warnings are returned for:
// - Unknown attributes (schema evolves over time)
// - Unreachable conditions (e.g., `false && ...`)
// - Always-true conditions (e.g., `principal.level >= 0`)
// - Redundant sub-conditions
// Warnings do not block creation.
// Compile errors are returned for:
// - Bare boolean attribute references (use explicit `== true` instead)
func (c *PolicyCompiler) Compile(dslText string) (*CompiledPolicy, []ValidationWarning, error)

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

// CompiledPolicy is the parsed, validated, and optimized form of a policy.
// The engine evaluates CompiledPolicy instances, never raw DSL text.
type CompiledPolicy struct {
    Effect     PolicyEffect
    Target     CompiledTarget     // Parsed principal/action/resource clauses
    Conditions []CompiledCondition // Pre-parsed AST nodes
    GlobCache  map[string]glob.Glob // Pre-compiled globs for `like` expressions
}
```

**`compiled_ast` JSONB schema sketch:**

```json
{
  "grammar_version": 1,
  "effect": "permit",
  "target": {
    "principal_type": "character",
    "action_list": ["read", "write"],
    "resource_type": "property",
    "resource_exact": null // Target-level pinning: if non-null, policy applies only to this exact resource string
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
the <5ms p99 latency target for cached requests is achievable.

### AccessRequest

```go
// AccessRequest contains all information needed for an access decision.
type AccessRequest struct {
    Subject  string // "character:01ABC", "plugin:echo-bot", "system"
    Action   string // Common: "read", "write", "delete", "enter", "execute", "emit", "list_characters". Open set — plugins MAY introduce additional actions.
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
| `exit:`      | `exit`      | `exit:01MNO`            |
| `scene:`     | `scene`     | `scene:01PQR`           |
| `command:`   | `command`   | `command:say`           |
| `property:`  | `property`  | `property:01GHI`        |
| `stream:`    | `stream`    | `stream:location:01XYZ` |

**Note:** The `session:` prefix is never a valid DSL type in policies. Sessions
are always resolved to `character:` at the engine entry point before evaluation
(see [Session Subject Resolution](#session-subject-resolution) below). The `(resolved)`
marker indicates this prefix does not appear in `principal is T` clauses.

**Subject prefix constants:** The `access` package MUST define these prefixes
as constants (e.g., `SubjectCharacter = "character:"`, `SubjectPlugin =
"plugin:"`) to prevent typos and enable compile-time references. All call sites
MUST use the full `character:` prefix (not the legacy `char:` abbreviation).
The engine MUST reject unknown prefixes with a clear error.

**Security requirement (S1):** The system subject string `"system"` and
`WithSystemSubject(ctx)` context marker MUST be restricted to internal-only
callers. API ingress layers MUST validate that external requests cannot use
the system subject or system context marker to bypass ABAC checks. Tests MUST
verify that API boundaries reject requests attempting to use system subject
bypass mechanisms.

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

Session resolution defines 2 error codes, distinguishing normal operation
(invalid/expired sessions) from infrastructure failures:

- **SESSION_INVALID:** Session not found, expired, or has no associated
  character. Return `Decision{Allowed: false, Effect: EffectDefaultDeny,
  PolicyID: "infra:session-invalid"}` with descriptive error (e.g., `"session
  not found: web-123"` or `"session has no associated character"`). This
  covers normal operation — expired sessions, unauthenticated sessions that
  have not yet selected a character, or sessions referencing deleted
  characters. Using `EffectDefaultDeny` (not `EffectDeny`) distinguishes
  infrastructure errors from explicit policy denials in audit queries. The
  `infra:` prefix on the PolicyID further disambiguates infrastructure errors
  from "no policy matched" (which has an empty PolicyID).

- **SESSION_STORE_ERROR:** Database failure, timeout, or connectivity issue.
  Return `Decision{Allowed: false, Effect: EffectDefaultDeny, PolicyID:
  "infra:session-store-error"}` with the wrapped store error. Fail-closed — a
  session lookup failure MUST NOT grant access. This represents
  infrastructure-level failures that operators need to monitor and alert on.

**Character deletion handling:** Character deletion is handled by the world
model layer, NOT the authorization layer. `WorldService.DeleteCharacter()`
**MUST** use `ON DELETE CASCADE` constraints to invalidate sessions
referencing the deleted character. Session resolution encountering a
deleted character returns `SESSION_INVALID` (normal operation — the session
is now invalid) rather than treating it as an integrity error.

**Error code constants:** Session resolution error codes **MUST** be defined
as constants in the `internal/access` package. The following error code
constants **MUST** be exported:

- `ErrCodeSessionInvalid` — `"infra:session-invalid"`
- `ErrCodeSessionStoreError` — `"infra:session-store-error"`

These constants ensure consistent error handling and filterable audit queries
(e.g., `WHERE policy_id LIKE 'infra:%'` returns only infrastructure failures).

**Audit distinguishability:** Session resolution errors use
`EffectDefaultDeny` with `infra:*` policy IDs, making them filterable in
audit queries. `WHERE effect = 'deny'` returns only explicit policy denials.
`WHERE policy_id LIKE 'infra:%'` returns only infrastructure failures.
`WHERE effect = 'default_deny' AND policy_id = ''` returns "no policy
matched" cases.

Session resolution errors SHOULD use oops error codes for structured handling:
`SESSION_INVALID` and `SESSION_STORE_ERROR`. `SESSION_INVALID` covers all
normal operation cases (expired sessions, missing characters) while
`SESSION_STORE_ERROR` indicates infrastructure failures requiring operational
intervention.

### Decision

```go
// Decision represents the outcome of a policy evaluation.
//
// Invariant: allowed is true if and only if Effect is EffectAllow or EffectSystemBypass.
// The allowed field is unexported to prevent direct field mutation that would violate the invariant.
// Use NewDecision() to construct Decision instances with enforced invariants.
// Use IsAllowed() to access the authorization result.
//
// TWO-PHASE CONSTRUCTION:
// Decisions are constructed in two phases:
// 1. NewDecision(effect, reason, policyID) creates the base decision with the core fields
// 2. The engine sets Policies and Attributes fields after evaluation completes
// Callers MUST NOT use a Decision that has nil Policies or Attributes fields, as these
// are required by the audit logger and for debugging output.
//
// Value Semantics Safety:
// The mixed visibility pattern (allowed unexported, Effect exported) is intentional and safe
// due to Go's value semantics. Decision is always passed by value, never by pointer (see NewDecision
// return type). This means callers receive a copy of the struct. The invariant is enforced at
// creation via NewDecision(), and value copying prevents any mutation of the original Decision
// that might violate the invariant. No API changes are required — the exported Effect field
// remains accessible for all use cases, while the unexported allowed field is protected by
// virtue of struct value copying. Callers must use IsAllowed() to access the authorization result,
// but the Effect field is publicly readable for debugging and logging scenarios.
type Decision struct {
    allowed    bool            // unexported to enforce invariant via constructor
    Effect     Effect          // Allow, Deny, DefaultDeny (no policy matched), or SystemBypass
    Reason     string          // Human-readable explanation
    PolicyID   string          // ID of the determining policy ("" if default deny)
    Policies   []PolicyMatch   // All policies that matched (for debugging) — set in phase 2
    Attributes *AttributeBags  // Snapshot of all resolved attributes — set in phase 2
}

// IsAllowed returns whether the request is authorized.
// This is the only way to access the allowed field from outside the package.
func (d Decision) IsAllowed() bool {
    return d.allowed
}

// NewDecision constructs a Decision with the Allowed/Effect invariant enforced.
// Allowed is set to true if and only if effect is EffectAllow or EffectSystemBypass.
//
// TWO-PHASE CONSTRUCTION PATTERN:
// Phase 1 (construction): NewDecision() creates the Decision with effect, reason, and policyID.
// Phase 2 (evaluation): The engine MUST populate the Policies and Attributes fields after
// construction, before passing the Decision to the audit logger or returning to callers.
//
// This two-phase pattern exists because:
// - The decision effect is determined early (during policy matching)
// - Policies field requires collection of all matching policies (computed during evaluation)
// - Attributes field is the resolved attribute snapshot (computed by the attribute resolver)
//
// The engine MUST always set both Policies and Attributes before returning a Decision
// or passing it to the audit logger. Callers rely on these fields for debugging and
// audit trail generation.
//
// IMPLEMENTATION CONSIDERATION:
// Future refactoring may introduce WithPolicies() and WithAttributes() builder methods
// to enforce the two-phase pattern at compile time:
//   decision := NewDecision(effect, reason, policyID).
//     WithPolicies(matches).
//     WithAttributes(bags)
// However, the current implementation relies on convention and engine discipline.
func NewDecision(effect Effect, reason string, policyID string) Decision {
    allowed := effect == EffectAllow || effect == EffectSystemBypass
    return Decision{
        allowed:  allowed,
        Effect:   effect,
        Reason:   reason,
        PolicyID: policyID,
    }
}

// Validate checks the Decision invariant at the engine return boundary.
// Returns an error if the Allowed/Effect invariant is violated.
// The engine MUST call this before returning any Decision.
func (d Decision) Validate() error {
    expectedAllowed := d.Effect == EffectAllow || d.Effect == EffectSystemBypass
    if d.allowed != expectedAllowed {
        return fmt.Errorf("Decision invariant violated: allowed=%v but effect=%v", d.allowed, d.Effect)
    }
    return nil
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

**Note:** The DSL compiler enforces explicit boolean comparisons
(`principal.admin == true`, never bare `principal.admin`) to prevent fragile
policies where type evolution silently breaks conditions. See **Bare boolean
restriction** in the grammar section.

Missing attributes cause all comparisons to evaluate to `false`
(Cedar-aligned behavior), so `principal.reputation.score >= 50` safely returns
`false` when the reputation plugin is not loaded. The `has` check is only
needed when using negation (`!=`) or `if-then-else` patterns where the
distinction between "missing" and "present but non-matching" matters.

**Bag merge semantics:** The `AttributeResolver` assembles bags by merging all
non-nil provider results. If multiple providers return the same key for the same
entity, the **last-registered provider's value wins** for scalar attributes. For
**list-valued attributes**, if multiple providers contribute the same key, the
values are **concatenated** into a single list, preserving all values from all
providers in registration order (core providers first, then plugins in load
order). This allows multiple plugins to contribute to shared list attributes
(e.g., multiple faction plugins adding faction memberships to a character).
Integration tests MUST verify list concatenation behavior with multiple
providers contributing to the same list attribute key. Example: Provider A
returns `{"factions": ["rebels"]}`, Provider B returns `{"factions": ["traders"]}`.
The merged bag contains `{"factions": ["rebels", "traders"]}`.

Key collision handling for scalar attributes depends on the collision type:

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

**Action bag purpose and MVP deferrability:** The action bag exists for **plugin
extensibility** — plugins MAY define custom actions with metadata (e.g.,
`action.type`, `action.severity`) in future versions. However, the MVP
implementation MAY skip action bag population if no policies reference `action.*`
attributes. The key insight is that **target-clause action matching**
(`action in ["read", "write"]`) covers all built-in use cases and requires no
action attributes. Plugins that need action metadata are deferred to future
phases. The `PolicyCompiler` MUST reject policies referencing unregistered
`action.*` attributes at compile time — the attribute schema registry defines
the complete set of valid attributes. The runtime fail-safe behavior (conditions
on missing attributes evaluate to `false`) serves as defense-in-depth but is
not expected to trigger for `action.*` attributes in production. Future
attributes MAY be added by registering them in the action schema when use cases
emerge.

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

**Goroutine prohibition:** Attribute providers MUST NOT spawn goroutines that
call `Evaluate()` or perform any engine operations. The context-based re-entrance
guard only prevents synchronous re-entrance on the same goroutine. A buggy
provider spawning a goroutine that calls `Evaluate()` bypasses the sentinel
entirely, creating undetected re-entrance. Providers MUST complete all attribute
resolution synchronously within the calling goroutine.

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
on the same call stack only. It does NOT detect cross-goroutine re-entrance.
A provider spawning a goroutine that calls `Evaluate()` bypasses the guard
entirely because the sentinel is stored in the calling goroutine's context,
not in goroutine-global state. Cross-goroutine re-entrance is prevented by
the MUST NOT prohibition in the provider contract (see above), enforced
through convention and code review, not runtime checks. Integration tests
MUST verify that same-goroutine re-entry attempts are detected (via panic
or error) to prevent regression in guard implementation.

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

**ExitProvider** (`exit` namespace) — **stub only** ([Decision #88](../decisions/epic7/phase-7.3/088-exit-scene-provider-stubs.md)):

Stub provider returns only type and ID — sufficient for target matching
(`resource is exit`). Full attribute schema deferred to backlog.

| Attribute | Type   | Requirement | Description      |
| --------- | ------ | ----------- | ---------------- |
| `type`    | string | MUST        | Always `"exit"`  |
| `id`      | string | MUST        | ULID of the exit |

<!-- TODO: Implement full ExitProvider with exit attributes (direction, destination,
     lock status, bidirectionality, source/target locations). See backlog bead
     holomush-5k1.422. Full implementation requires:
     - direction: string (e.g., "north", "out", "portal")
     - destination: string (ULID of target location)
     - source: string (ULID of source location)
     - locked: bool (whether exit is locked by lock expression)
     - bidirectional: bool (whether exit has a return path)
     Provider should query world.ExitRepository for these attributes. -->

**SceneProvider** (`scene` namespace) — **stub only** ([Decision #88](../decisions/epic7/phase-7.3/088-exit-scene-provider-stubs.md)):

Stub provider returns only type and ID — sufficient for target matching
(`resource is scene`). Full attribute schema deferred to backlog.

| Attribute | Type   | Requirement | Description       |
| --------- | ------ | ----------- | ----------------- |
| `type`    | string | MUST        | Always `"scene"`  |
| `id`      | string | MUST        | ULID of the scene |

<!-- TODO: Implement full SceneProvider with scene attributes (privacy, participants,
     creator, active status). See backlog bead holomush-5k1.424. Full implementation
     requires:
     - privacy: string (e.g., "public", "private", "invite-only")
     - participants: []string (ULIDs of characters in scene)
     - creator: string (ULID of scene creator)
     - active: bool (whether scene is currently active)
     - location: string (ULID of location where scene takes place, if any)
     Provider should query world.SceneRepository for these attributes. -->

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
