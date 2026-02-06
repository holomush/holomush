# ADR 0007: Command Security Model

**Date:** 2026-02-02
**Status:** Superseded by [ADR 0014](0014-direct-static-access-control-replacement.md)
**Deciders:** HoloMUSH Contributors

## Context

HoloMUSH commands need authorization before execution. A player typing `@dig east=Library`
should only succeed if they have building privileges. A player typing `@shutdown` should
only succeed if they are an administrator.

The command system (see [ADR 0006](0006-unified-command-registry.md)) uses a unified registry
storing both Go core commands and Lua plugin commands. Each registered command needs security
metadata indicating who can execute it and under what conditions.

Traditional MUSH platforms use role-based permission systems with flags like `WIZARD`,
`BUILDER`, and `STAFF`. These systems are simple but inflexible: either you have the flag
and can do everything it permits, or you do not.

HoloMUSH already has an access control system (`internal/access`) designed for ABAC
(Attribute-Based Access Control). The question is how to integrate command authorization
with this existing infrastructure.

### Requirements

- Commands MUST declare what permissions they need
- The dispatcher MUST check permissions before execution
- Authorization checks MUST be uniform for Go and Lua commands
- The system MUST support fine-grained control (e.g., allow `@dig` but not `@destroy`)
- The system SHOULD integrate with the existing ABAC infrastructure

## Options Considered

### Option A: Role-Based Authorization

Commands require one or more roles. A character must have at least one matching role.

```go
type CommandEntry struct {
    Name     string
    Roles    []string  // e.g., ["builder", "admin"]
    Handler  CommandHandler
}

// Dispatch check
for _, role := range entry.Roles {
    if character.HasRole(role) {
        return entry.Handler(ctx, exec)
    }
}
return ErrPermissionDenied
```

**Pros:**

- Simple mental model (familiar from traditional MUSHes)
- Easy to implement
- Fast to check

**Cons:**

- Coarse-grained: all-or-nothing per role
- Role explosion: need new roles for every permission combination
- Cannot express "builder who can dig but not destroy"
- Plugins must use predefined roles, limiting customization
- Does not leverage existing ABAC infrastructure

### Option B: Capability-Based Authorization

Commands declare specific capabilities. A character must have ALL required capabilities.

```go
type CommandEntry struct {
    Name         string
    Capabilities []string  // e.g., ["build.dig"]
    Handler      CommandHandler
}

// Dispatch check
for _, cap := range entry.Capabilities {
    if !access.HasCapability(ctx, characterID, cap) {
        return ErrPermissionDenied
    }
}
return entry.Handler(ctx, exec)
```

**Pros:**

- Fine-grained: precise control over individual commands
- Composable: grant exactly the capabilities needed
- Extensible: plugins define new capabilities in their namespace
- Hierarchical: `build.*` grants all building capabilities
- Integrates with existing ABAC evaluator

**Cons:**

- More capabilities to manage than roles
- Requires capability grant infrastructure
- Initial setup more complex than simple role assignment

### Option C: Resource-Based Authorization

Commands declare a resource pattern. Authorization depends on what the command targets.

```go
type CommandEntry struct {
    Name      string
    Resource  string  // e.g., "location:$here"
    Action    string  // e.g., "build"
    Handler   CommandHandler
}

// Dispatch check
resource := resolveTokens(entry.Resource, ctx)
if !access.Check(ctx, characterSubject, entry.Action, resource) {
    return ErrPermissionDenied
}
```

**Pros:**

- Context-aware: permission depends on what you are acting on
- Aligns with ABAC resource model
- Supports per-object permissions

**Cons:**

- Complex to understand and debug
- Resource must be known before execution starts
- Many commands do not have a clear target resource at parse time
- Overkill for most MUSH commands

## Decision

We adopt **Option B: Capability-Based Authorization**.

Every command MUST declare its required capabilities. The dispatcher MUST verify the character
has ALL declared capabilities before invoking the handler.

### Capability Namespace

Capabilities use dot-notation hierarchy organized by domain:

| Namespace  | Purpose                             | Examples                                     |
| ---------- | ----------------------------------- | -------------------------------------------- |
| `world.*`  | World navigation and observation    | `world.look`, `world.move`, `world.examine`  |
| `comms.*`  | Communication with other characters | `comms.say`, `comms.pose`, `comms.emit`      |
| `build.*`  | World construction and modification | `build.dig`, `build.create`, `build.link`    |
| `player.*` | Player self-management              | `player.alias`, `player.who`, `player.quit`  |
| `admin.*`  | Server administration               | `admin.boot`, `admin.shutdown`, `admin.wall` |

Plugins MAY define additional namespaces for their commands (e.g., `rpg.combat.*`).

### Default Grants

New characters MUST receive a baseline capability set enabling normal play:

- `world.*` - Look around, move between locations, examine objects
- `comms.*` - Say, pose, emit, and communicate with others
- `player.*` - Manage aliases, see who is online, quit gracefully

Building and administration capabilities MUST require explicit grants by an authorized
character.

### Wildcard Matching

Wildcard capabilities simplify grants using suffix matching:

- `world.*` grants ALL capabilities starting with `world.`
- Wildcards MUST only appear at the end of capability strings
- `*` alone MUST NOT be valid (no "grant everything" shortcut)
- Wildcards match complete segments: `world.*` matches `world.look` but NOT `worldwide.look`

Matching algorithm:

1. Check for exact capability match
2. If no exact match, check for wildcard grants
3. Explicit denials MUST override wildcard grants

### ABAC Integration

Capability checks delegate to the existing `internal/access` evaluator:

```go
func HasCapability(ctx context.Context, charID ulid.ULID, cap string) bool {
    return evaluator.Evaluate(ctx, Subject{CharacterID: charID}, cap, nil)
}
```

This keeps the command system decoupled from ABAC implementation details. Future
enhancements (Epic 7) can add context-aware rules, time-based restrictions, or
location-based permissions without modifying the command dispatcher.

## Consequences

### Positive

- **Fine-grained control**: Operators can grant exactly the permissions needed
- **Plugin flexibility**: Plugins define capabilities in their own namespace
- **Composability**: Combine capabilities to create custom permission sets
- **ABAC alignment**: Leverages existing access control infrastructure
- **Default safety**: Building and admin capabilities require explicit grants
- **Hierarchical grants**: Wildcards simplify common permission patterns

### Negative

- **More granular management**: More capabilities to track than roles
- **Learning curve**: Operators must understand capability model
- **Grant infrastructure needed**: Must implement capability storage and assignment

### Neutral

- **Migration path**: Roles can be implemented as capability bundles if desired
- **Help system**: Must display required capabilities for each command
- **Audit**: Capability checks create natural audit points

### Implementation Notes

- Capability storage: Add `character_capabilities` table with `(character_id, capability)` rows
- Grant command: `@grant <character>=<capability>` (requires `admin.grant` capability)
- Revoke command: `@revoke <character>=<capability>` (requires `admin.revoke` capability)
- List command: `@capabilities <character>` shows granted capabilities

## References

- [Commands & Behaviors Design](../specs/2026-02-02-commands-behaviors-design.md) - Security section
- [Access Control Design](../specs/2026-01-21-access-control-design.md) - ABAC architecture
- [ADR 0006: Unified Command Registry](0006-unified-command-registry.md) - Registry design
- Implementation: `internal/access/evaluator.go`, `internal/command/dispatcher.go` (planned)
