# ADR 0015: Three-Layer Player Access Control

**Date:** 2026-02-05
**Status:** Accepted
**Deciders:** HoloMUSH Contributors

## Context

The ABAC policy engine provides powerful, admin-authored policies using a Cedar-inspired
DSL. However, regular players also need to control access to resources they own — locking
a chest, hiding a property from specific characters, or restricting entry to a personal
room.

Exposing the full DSL to all players would be overwhelming and error-prone. Conversely,
limiting players to only setting visibility flags would be too restrictive for the rich
social dynamics of a MUSH environment. The system needs to balance expressiveness with
usability across different user roles.

### Options Considered

**Option A: Full DSL for everyone**

All characters can author policies using the full Cedar-inspired DSL.

| Aspect     | Assessment                                                                                                                                                                                  |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Strengths  | Maximum expressiveness for all users                                                                                                                                                        |
| Weaknesses | Overwhelming for non-technical players; policies could interact in unexpected ways; difficult to provide good error messages for syntax errors; security risk from poorly authored policies |

**Option B: Metadata-only (flags and lists)**

Players configure visibility and access lists on their properties. No policy authoring.
All logic lives in system-level policies that evaluate these metadata attributes.

| Aspect     | Assessment                                                                                                           |
| ---------- | -------------------------------------------------------------------------------------------------------------------- |
| Strengths  | Simple for players; all policy logic centralized in system policies                                                  |
| Weaknesses | Cannot express conditional access (e.g., "faction rebels AND level >= 5"); limited to the predefined metadata fields |

**Option C: Three-layer progressive model**

Three layers of increasing complexity: metadata configuration, simplified lock syntax,
and full DSL. Each layer targets a different user role.

| Aspect     | Assessment                                                                                                                       |
| ---------- | -------------------------------------------------------------------------------------------------------------------------------- |
| Strengths  | Progressive disclosure — players use what they need; locks provide conditional access without full DSL; admins retain full power |
| Weaknesses | Lock syntax is a third "language" to learn (alongside metadata and DSL); lock compilation adds a translation layer               |

## Decision

**Option C: Three-layer progressive model.**

### Layer 1: Property Metadata (All Characters)

Characters set visibility and access lists on properties they own. No policy authoring
required. The character configures data; existing system-level seed policies evaluate it.

```text
> set me/secret_background.visibility = private
> set me/wounds.visibility = restricted
> set me/wounds.visible_to += character:01AAA
```

This layer requires no understanding of policies or conditions. Players interact with
structured fields that seed policies already know how to evaluate.

### Layer 2: Object Locks (Resource Owners)

Owners set conditions on their resources using a simplified lock syntax. Locks compile
to scoped `permit` policies behind the scenes.

```text
> lock my-chest/read = faction:rebels & level:>=5
> lock me/backstory/read = me | flag:storyteller
> unlock my-chest/read
```

The lock syntax uses registered token predicates — not hard-coded vocabulary. Each
`AttributeProvider` registers lock tokens that expose its attributes to the lock parser.

**Core tokens** (shipped with `CharacterProvider`):

| Token     | Type       | Example            | Compiles To                        |
| --------- | ---------- | ------------------ | ---------------------------------- |
| `faction` | equality   | `faction:rebels`   | `principal.faction == "rebels"`    |
| `flag`    | membership | `flag:storyteller` | `"storyteller" in principal.flags` |
| `level`   | numeric    | `level:>=5`        | `principal.level >= 5`             |

**Plugin tokens** (registered at plugin load, namespaced by plugin ID):

| Token              | Type     | Example                  | Compiles To                               |
| ------------------ | -------- | ------------------------ | ----------------------------------------- |
| `reputation.score` | numeric  | `reputation.score:>=50`  | `principal.reputation.score >= 50`        |
| `guilds.primary`   | equality | `guilds.primary:merchants` | `principal.guilds.primary == "merchants"` |

Lock compilation generates a `permit` policy scoped to the specific resource and action:

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

**Access constraint:** The `lock` command verifies write permission via `Evaluate()` before
creating the policy. Characters can only lock resources they have write access to.

### Layer 3: Full Policies (Admin Only)

The complete Cedar-inspired DSL with unrestricted scope, managed via the `policy` command
set. Only characters with the `admin` role can create, edit, and delete full policies.

### Layer Interaction

Admin `forbid` policies always trump player locks via the deny-overrides conflict
resolution model ([ADR 0011](0011-deny-overrides-without-priority.md)). Players operate
within the boundaries admins set.

| Layer             | Who             | Scope          | Deny-overrides? |
| ----------------- | --------------- | -------------- | --------------- |
| Property metadata | All characters  | Own properties | Yes             |
| Object locks      | Resource owners | Own resources  | Yes             |
| Full policies     | Admins          | Everything     | Top authority   |

## Rationale

**Progressive disclosure:** Most players only need Layer 1 (set a property to private).
Builders and engaged players use Layer 2 (lock a room to faction members). Only admins
use Layer 3. Each layer adds complexity only for users who need it.

**Lock tokens are extensible:** The token registry means the lock vocabulary grows with
the plugin ecosystem. A reputation plugin adds `reputation.score:>=50` to locks
automatically. Players discover available tokens via `lock tokens`.

**Locks compile to policies:** Lock-generated policies are evaluated by the same engine
as admin policies. No separate access control path exists. This ensures deny-overrides
work uniformly — an admin `forbid` blocks a player lock `permit` without special-casing.

**Lock policies are ephemeral:** Lock-generated policies are NOT versioned. Each
`lock`/`unlock` creates or deletes a policy directly. This matches the casual gameplay
context — locking a chest is a quick action, not a governance event.

### Token Namespace Enforcement

Plugin lock tokens MUST use a dot-separated prefix that **exactly matches** their
plugin ID (e.g., plugin `reputation` registers `reputation.score`, not `rep.score`).
Abbreviations are not allowed — the prefix before the first `.` MUST equal the plugin
ID string. The engine validates this at registration and rejects non-namespaced or
incorrectly-prefixed plugin tokens. Core tokens (`faction`, `flag`, `level`) are
un-namespaced because they ship with the engine.

Duplicate token names are a **fatal startup error**. The server MUST NOT start if any
token name collides. This fail-fast behavior catches naming conflicts at deploy time.

## Consequences

**Positive:**

- Players get access control without learning a policy language
- Lock syntax is concise and readable (`faction:rebels & level:>=5`)
- Extensible via token registry — plugins add lock vocabulary automatically
- All layers evaluated by the same engine — uniform deny-overrides behavior
- `lock tokens` command provides discoverability

**Negative:**

- Lock syntax is a third "language" alongside property metadata and the full DSL
- Lock compilation adds a translation layer (parser → AST → DSL text → compiled policy)
- Plugin token namespace enforcement adds a constraint on plugin authors

**Neutral:**

- Lock-generated policies use the naming convention `lock:{type}:{id}:{action}` for
  identification and cleanup
- Token conflicts are caught at server startup, not at lock authoring time
- The `lock tokens` command reads from the registry at runtime — output changes as
  plugins load and unload

## References

- [Full ABAC Architecture Design — Access Control Layers](../specs/2026-02-05-full-abac-design.md)
- [Full ABAC Architecture Design — Lock Token Registry](../specs/2026-02-05-full-abac-design.md)
- [Design Decision #12: Player Access Control Layers](../specs/2026-02-05-full-abac-design-decisions.md)
- [Design Decision #19: Lock Policies Are Not Versioned](../specs/2026-02-05-full-abac-design-decisions.md)
- [Design Decision #33: Plugin Lock Tokens MUST Be Namespaced](../specs/2026-02-05-full-abac-design-decisions.md)
- [ADR 0011: Deny-Overrides Without Priority](0011-deny-overrides-without-priority.md)
