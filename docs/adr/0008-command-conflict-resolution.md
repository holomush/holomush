# ADR 0008: Command Conflict Resolution

**Date:** 2026-02-02
**Status:** Accepted
**Deciders:** HoloMUSH Contributors

## Context

HoloMUSH supports commands from multiple sources:

- **Core commands**: Built-in Go commands (e.g., `look`, `move`, `quit`) registered at server startup
- **Plugin commands**: Lua commands declared in plugin manifests (e.g., `say`, `pose`, custom RPG commands)

Multiple plugins, or a plugin and core, may attempt to register the same command name. For example:

- Two plugins both define a `roll` command for dice rolling
- A plugin defines `look` which conflicts with the core `look` command
- A theme pack plugin intentionally overrides `say` to add custom formatting

The unified command registry ([ADR 0006](0006-unified-command-registry.md)) needs a deterministic
strategy for handling these conflicts. Without clear rules, the active command would depend on
arbitrary factors like filesystem ordering or goroutine scheduling, making behavior unpredictable
across server restarts.

### Requirements

- Command resolution MUST be deterministic across server restarts
- Conflicts MUST be visible to server operators (not silently ignored)
- Operators SHOULD be able to identify which plugin provides each command
- The system SHOULD allow intentional overrides (plugin replacing core commands)
- The system SHOULD NOT require explicit conflict configuration for every overlap

## Options Considered

### Option A: First-Registered Wins

The first command registration locks the name. Subsequent registrations for the same name fail
with an error.

```text
Load order: core → plugin-a → plugin-b
If both plugins register "roll":
  - plugin-a's "roll" succeeds
  - plugin-b's "roll" fails with error
```

**Pros:**

- Core commands are protected by default
- Conflicts cause explicit errors, forcing resolution
- No ambiguity about which command is active

**Cons:**

- Prevents intentional plugin overrides without special configuration
- Plugin load order still matters but failures are unpredictable
- Operators must maintain conflict resolution configuration
- Unfriendly: installing a new plugin can break the server

### Option B: Last-Registered Wins (Deterministic Order)

Commands register in a well-defined order. Later registrations silently overwrite earlier ones.

```text
Load order: core → plugin-a → plugin-b (alphabetical by name)
If both plugins register "roll":
  - plugin-a's "roll" registers
  - plugin-b's "roll" overwrites (plugin-b wins)
  - No error, server starts normally
```

**Pros:**

- Simple to understand: alphabetical order, last wins
- Plugins can override core commands by design
- No configuration needed for common cases
- Server always starts (no conflict errors)

**Cons:**

- Silent overwrites could surprise operators
- Accidental conflicts may go unnoticed
- Plugin naming affects precedence (a-plugin beats z-plugin)

### Option C: Last-Registered Wins with Warning

Same as Option B, but log a warning on conflict so operators are informed.

```text
Load order: core → plugin-a → plugin-b (alphabetical by name)
If both plugins register "roll":
  - plugin-a's "roll" registers
  - plugin-b's "roll" overwrites
  - Warning logged: "Command 'roll' from 'plugin-b' overrides 'plugin-a'"
  - Server starts normally
```

**Pros:**

- Deterministic behavior (alphabetical order is reproducible)
- Operators see conflicts in logs without server failure
- Allows intentional overrides while flagging accidental ones
- Server always starts successfully
- Easy to audit command sources via logs

**Cons:**

- Plugin naming affects precedence
- Operators must monitor logs to catch conflicts
- Cannot prevent unintended overrides without policy enforcement

### Option D: Explicit Priority Configuration

Require operators to configure priority for conflicting commands in server configuration.

```yaml
command_priorities:
  roll:
    - plugin-b # first choice
    - plugin-a # fallback
  look: core # core always wins
```

**Pros:**

- Operators have full control over resolution
- No surprises from plugin load order
- Can express complex priority rules

**Cons:**

- Significant configuration burden
- Must be updated when adding/removing plugins
- Server fails to start on unconfigured conflicts
- Overkill for typical deployments

## Decision

We adopt **Option C: Last-Registered Wins with Warning**.

### Load Order Specification

Commands load in this deterministic sequence:

1. **Core Go commands** register at server startup (alphabetically by command name)
2. **Plugin commands** register as each plugin loads, where:
   - Plugins load in **alphabetical order by plugin directory name**
   - Within each plugin, commands register in **manifest declaration order**

This ordering is filesystem-independent and reproducible across restarts.

### Conflict Behavior

When a command name is already registered:

1. The new registration **overwrites** the existing entry (last-loaded wins)
2. The server **MUST log a warning** with structured fields:
   - `command`: the conflicting command name
   - `new_source`: plugin or "core" that is taking over
   - `old_source`: plugin or "core" being replaced

Example log output:

```text
WARN command conflict detected command=roll new_source=plugin-b old_source=plugin-a
```

### Core Command Override Policy

Plugins SHOULD NOT override core commands without explicit admin awareness. Operators can:

- Review startup logs for core command overrides
- Rename/remove offending plugins if override was unintended
- Accept the override if it was intentional (e.g., customization plugin)

Future enhancement: A server configuration flag could prevent plugins from overriding core
commands entirely (`allow_core_override: false`).

### Implementation Notes

The `CommandRegistry` tracks the source of each command:

```go
type CommandEntry struct {
    Name         string
    Handler      CommandHandler
    Capabilities []string
    Help         string
    Source       string  // "core" or plugin directory name
}

func (r *Registry) Register(entry CommandEntry) {
    if existing, ok := r.commands[entry.Name]; ok {
        r.logger.Warn("command conflict detected",
            "command", entry.Name,
            "new_source", entry.Source,
            "old_source", existing.Source,
        )
    }
    r.commands[entry.Name] = entry
}
```

## Consequences

### Positive

- **Deterministic**: Same plugins always produce same command layout
- **Observable**: Conflicts are logged for operator awareness
- **Flexible**: Allows intentional plugin customization of core commands
- **Robust**: Server starts despite conflicts (no configuration deadlocks)
- **Auditable**: Startup logs document complete command resolution

### Negative

- **Name-dependent precedence**: Plugin directory naming affects which wins
- **Log monitoring required**: Operators must check logs to catch conflicts
- **No prevention**: Cannot block specific overrides without future enhancement

### Neutral

- **Help system impact**: `help <cmd>` shows the winning command's help text
- **Capability inheritance**: Winning command uses its own capability requirements
- **Hot reload**: Plugin reload follows same rules (warning on re-override)

## References

- [Commands & Behaviors Design](../specs/2026-02-02-commands-behaviors-design.md) - Parent spec
- [ADR 0006: Unified Command Registry](0006-unified-command-registry.md) - Registry architecture
- Implementation: `internal/command/registry.go` (planned)
