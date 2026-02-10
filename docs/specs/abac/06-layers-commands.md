<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

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
> unlock my-chest
```

**Unlock semantics:** The `unlock` command removes lock-generated policies.
Two forms are supported:

1. **Action-specific unlock:** `unlock <resource>/<action>` removes the lock
   policy for the specified action on the resource. Example:
   `unlock my-chest/read` deletes the policy named
   `lock:object:<resource_ulid>:read`.

2. **Resource-wide unlock:** `unlock <resource>` removes **all** lock policies
   for the resource across all actions. Example: `unlock my-chest` deletes all
   policies matching the pattern `lock:object:<resource_ulid>:*`. The
   implementation **MUST** query `PolicyStore` with
   `WHERE name LIKE 'lock:object:<resource_ulid>:%'` (or equivalent pattern for
   the resolved resource type) and delete all matching policies. This ensures
   bulk unlock for resources with multiple action locks.

**Resource target resolution:** The lock command resolves the resource target
(the part before `/`) using the same name resolution as other world commands:
`me` resolves to the character issuing the command, `here` resolves to their
current location, and object names (e.g., `my-chest`, `armory`) are looked up
in the world model using the standard object matching rules (exact name match,
then disambiguation if multiple objects share the name in the same location).
All targets are resolved to ULIDs at lock compile time — the generated policy
uses the resolved ULID, not the display name.

#### Lock Syntax

> **DEFERRED (Phase 7.5):** The lock system documented in this subsection is
> part of Phase 7.5 (Locks & Admin), which is deferred. Implementation should
> not begin until Phase 7.5 is scheduled.

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
(defined in the [Attribute Providers](01-core-types.md#attribute-providers) section). Providers
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
prefixes are reserved for system use. See [Naming conventions](#policy-management)
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

**Compilation examples:**

**Object lock (includes ownership check):**

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

**Location lock (omits ownership check):**

```text
Input:  lock armory/enter = faction:rebels & level:>=3
Output:
  permit(
    principal is character,
    action in ["enter"],
    resource == "location:01XYZ..."
  ) when {
    principal.faction == "rebels"
    && principal.level >= 3
  };
```

**Ownership check requirement:** Lock-generated policies **MUST** include
`resource.owner == principal.id` in the condition block for resources that
have an owner attribute (objects, properties). This prevents lock policies
from surviving ownership transfer. Without this check, a lock set by the
original owner would grant access to the lock's conditions even after the
resource is transferred to a new owner, creating a backdoor permit.

For locations (which have no owner attribute), the ownership check is
**omitted** — locations use role-based write access authorization instead.
Location locks rely on the builder having write permission to the location at
lock creation time, and the generated policy conditions apply to all
principals without an ownership constraint. This allows location builders to
set access conditions (e.g., `level:>=5`) that apply regardless of which
builder created the lock.

**Security requirement (S4 - holomush-5k1.344):** Lock operations MUST
re-verify ownership on each operation, not just at creation. Creator identity
MUST be persisted with the lock record. Periodic reconciliation SHOULD detect
orphaned or ownership-transferred locks.

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
  faction:X             (equality, string)   — Character faction equals X
  flag:X                (membership, string) — Character has flag X
  level:OP N            (numeric, int)       — Character level (>=, >, <=, <, == N)
  reputation.score:OP N (numeric, float64)   — Reputation score (plugin: reputation)
  guilds.primary:X      (equality, string)   — Primary guild membership (plugin: guilds)
```

The `lock tokens` command reads from the token registry at runtime. Plugin
tokens appear automatically when the plugin is loaded. A `--verbose` flag
SHOULD show the underlying DSL attribute path for debugging:

```text
> lock tokens --verbose
Available lock tokens:
  faction:X             — Character faction equals X
                          (maps to: principal.faction, type: string)
  reputation.score:OP N — Reputation score (plugin: reputation)
                          (maps to: principal.reputation.score, type: float64)
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
- **Rate limit:** Characters are limited to a maximum of 50 lock policies
  (configurable via server YAML `access.max_lock_policies_per_character`,
  default: 50). The lock command MUST check the current count of lock policies
  authored by the character before creating a new lock. If the limit is
  exceeded, the command MUST return an error: `"Cannot create lock: you have
  reached the maximum of {N} lock policies. Use 'unlock' to remove unused
  locks first."` This prevents abuse and unbounded policy set growth.
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
policy diff <name> [<version>]                           (deferred)
policy export [--source=X]                               (deferred)
policy import <file>                                     (deferred)
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

**Usage:**

```text
policy test <subject> <action> <resource> [--verbose] [--json]
```

**Parameters:**

- `<subject>` — Subject entity reference (e.g., `character:01ABC`)
- `<action>` — Action to test (e.g., `enter`, `read`, `write`)
- `<resource>` — Resource entity reference (e.g., `location:01XYZ`)
- `--verbose` — Show detailed condition evaluation for all policies
- `--json` — Output structured JSON instead of human-readable text

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
Available to admins only. Use this when:

- The LISTEN/NOTIFY connection is down and an emergency policy change needs to
  take effect immediately, without waiting for reconnection
- Cache staleness has triggered fail-closed behavior and manual refresh is
  needed to restore access
- Verifying that a recent policy change has been applied to the cache

**Note:** This command does NOT bypass cache staleness or degraded mode
restrictions. During degraded mode or prolonged staleness, administrators
**MUST** use direct CLI or database access to resolve system issues, as the
policy engine fails closed for all subjects including admins.

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
