# Commands & Behaviors Architecture

**Epic:** holomush-wr9 | **Status:** Design | **Date:** 2026-02-02

This document specifies the architecture for the HoloMUSH command system, covering input
parsing, command dispatch, alias resolution, security, and help integration.

---

## Overview

The Commands & Behaviors system transforms player input into game actions through a unified
dispatch system supporting both Go core commands and Lua plugin commands.

### Command Lifecycle

```
Input → Parse → Alias Resolution → Dispatch → Execute → Event Emission → Output
```

1. **Input**: Raw text from telnet or gRPC (e.g., `say hello world`)
2. **Parse**: Split into command name and arguments (`cmd=say`, `args=hello world`)
3. **Alias Resolution**: Expand aliases: exact match → player alias → system alias
4. **Dispatch**: Look up handler in unified command registry
5. **Execute**: Run handler with context (character, location, args)
6. **Event Emission**: Successful state-changing commands emit domain events
7. **Output**: Feedback to player (confirmation, error, or game state description)

### Design Principles

- **Single registry**: All commands (Go and Lua) register in one central registry
- **Manifest-declared**: Lua plugins declare commands in `plugin.yaml`
- **Capability-gated**: Commands declare required capabilities; ABAC checks before execution
- **Event-driven outcomes**: Commands that change state emit events as source of truth

### Key Boundaries

- Parser is protocol-agnostic (works for telnet and gRPC)
- Command handlers receive validated, parsed input
- Handlers operate on character context, not connection details

### Execution Context

Commands execute in the context of an authenticated player controlling a character:

- **PlayerID**: The authenticated player (from Epic 5 auth). Used for session management,
  rate limiting, and player-level settings like aliases.
- **CharacterID**: The character being controlled. Used for world interactions,
  capability checks, and game state.
- **SessionID**: The active session. Used for `quit` command and output buffering.

A player may have multiple characters but controls one at a time per session.

---

## Command Registry

### Data Structures

```go
type CommandRegistry struct {
    commands map[string]CommandEntry
    mu       sync.RWMutex
}

type CommandEntry struct {
    Name         string           // canonical name (e.g., "say")
    Handler      CommandHandler   // Go handler or Lua dispatcher
    Capabilities []string         // ALL required capabilities (AND logic)
    Help         string           // short description (one line)
    Usage        string           // usage pattern (e.g., "say <message>")
    HelpText     string           // detailed markdown (loaded from helpFile if specified)
    Source       string           // "core" or plugin name
}

type CommandHandler func(ctx context.Context, exec *CommandExecution) error

type CommandExecution struct {
    CharacterID   ulid.ULID
    LocationID    ulid.ULID
    CharacterName string
    PlayerID      ulid.ULID   // authenticated player (for session/auth context)
    SessionID     ulid.ULID   // active session (for quit, output buffering)
    Args          string      // unparsed argument string
    Output        io.Writer   // session-wrapped writer with buffering
    Services      *Services   // injected dependencies for handlers
}

// Services provides access to core services for command handlers.
// Handlers MUST NOT store references to services beyond execution.
type Services struct {
    World   world.Service     // world model queries and mutations
    Session core.SessionManager
    Access  access.Evaluator
    Events  core.EventStore
}
```

### Go Command Registration

Core commands register at server startup:

```go
registry.Register(CommandEntry{
    Name:         "look",
    Handler:      handlers.Look,
    Capabilities: []string{"world.look"},
    Help:         "Look at your surroundings",
    Usage:        "look [target]",
    Source:       "core",
})
```

### Lua Command Registration

Plugins declare commands in `plugin.yaml`:

```yaml
name: communication
commands:
  - name: say
    capabilities:
      - comms.say
    help: "Send a message to the room"
    usage: "say <message>"
    helpText: |
      ## Say

      Speaks a message aloud to everyone in your current location.

      ### Examples

      - `say Hello everyone!` - Says "Hello everyone!" to the room
```

Alternative using external file:

```yaml
commands:
  - name: say
    capabilities:
      - comms.say
    help: "Send a message to the room"
    usage: "say <message>"
    helpFile: "help/say.md"
```

At plugin load:
1. Read manifest `helpText` → store directly in `HelpText`
2. If `helpFile` specified → read file contents → store in `HelpText`
3. Missing file = plugin load error (fail fast)

Manifest validation MUST reject commands with both `helpText` AND `helpFile` set.
Commands with neither get a default help message: "No detailed help available. Usage: {usage}"

---

## Alias System

### Storage

Aliases are stored in PostgreSQL:

```sql
CREATE TABLE system_aliases (
    alias       TEXT PRIMARY KEY,
    command     TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by  UUID REFERENCES players(id)
);

CREATE TABLE player_aliases (
    player_id   UUID REFERENCES players(id),
    alias       TEXT NOT NULL,
    command     TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (player_id, alias)
);
```

### Resolution Order

1. **Exact match**: Input matches a registered command name → use directly
2. **Player alias**: Look up in `player_aliases` for current player
3. **System alias**: Look up in `system_aliases`
4. **No match**: Pass original input to dispatcher (may be unknown command)

Aliases expand to full command strings, then re-parse. Example: `l` → `look` → parsed
as command `look` with no args.

### Alias Validation

Aliases MUST match the pattern `^[a-z][a-z0-9]{0,31}$`:

- Lowercase letters and digits only
- Must start with a letter
- Maximum 32 characters

Alias creation MUST detect and reject circular alias chains. The resolver tracks
expansion depth and fails if it exceeds 10 expansions (indicating a cycle or
excessively long chain).

### Management Commands

| Command | Capabilities | Description |
|---------|--------------|-------------|
| `alias add <alias>=<command>` | `player.alias` | Add player alias |
| `alias remove <alias>` | `player.alias` | Remove player alias |
| `alias list` | `player.alias` | List player aliases |
| `sysalias add <alias>=<command>` | `admin.alias` | Add system alias |
| `sysalias remove <alias>` | `admin.alias` | Remove system alias |
| `sysalias list` | `admin.alias` | List system aliases |

### Shadow Warnings

When creating an alias, warn if it shadows:

- **Existing command**: "Warning: 'look' is an existing command. Your alias will override it."
- **System alias** (for player aliases): "Warning: 'l' is a system alias for 'look'. Your alias will take precedence."
- **Own existing alias**: "Warning: Replacing existing alias 'l' (was: 'look')."

Warnings are informational - the operation still succeeds. Player has final say over
their own alias namespace.

System alias creation warns if shadowing a command but MUST block if shadowing another
system alias (use `sysalias remove` first).

---

## Security & Capabilities

### Capability Model

Every command declares required capabilities. The dispatcher checks the character has
ALL capabilities before execution.

```go
// In command dispatch
entry, ok := registry.Get(cmdName)
if !ok {
    return ErrUnknownCommand
}

for _, cap := range entry.Capabilities {
    if !access.HasCapability(ctx, characterID, cap) {
        return ErrPermissionDenied
    }
}

return entry.Handler(ctx, execution)
```

### Capability Namespace

Capabilities use dot-notation hierarchy:

| Category | Examples |
|----------|----------|
| `world.*` | `world.look`, `world.move`, `world.examine` |
| `comms.*` | `comms.say`, `comms.pose`, `comms.emit` |
| `build.*` | `build.dig`, `build.create`, `build.link`, `build.describe` |
| `player.*` | `player.alias`, `player.who`, `player.quit` |
| `admin.*` | `admin.boot`, `admin.shutdown`, `admin.wall`, `admin.alias` |

### Default Grants

New characters receive a default capability set:

- `world.*` - can look, move, examine
- `comms.*` - can say, pose, emit
- `player.*` - can manage own aliases, see who, quit

Building and admin capabilities require explicit grants.

### ABAC Integration

The capability check delegates to `internal/access` evaluator:

```go
func HasCapability(ctx context.Context, charID ulid.ULID, cap string) bool {
    return evaluator.Evaluate(ctx, Subject{CharacterID: charID}, cap, nil)
}
```

This keeps the command system decoupled from ABAC implementation. Epic 7 can enhance
the evaluator without changing command dispatch.

---

## Error Handling

### Error Categories

| Category | Player Message | Log Level |
|----------|----------------|-----------|
| Unknown command | "Unknown command. Try 'help'." | Debug |
| Permission denied | "You don't have permission to do that." | Info |
| Invalid arguments | "Usage: {usage}" | Debug |
| World state error | Descriptive (e.g., "There's no exit to the north.") | Debug |
| Internal error | "Something went wrong. Try again." | Error |

### Error Types

Command errors use the `oops` package for consistency with the codebase:

```go
// Sentinel errors for dispatch failures
var (
    ErrUnknownCommand   = oops.Define(oops.Code("UNKNOWN_COMMAND"))
    ErrPermissionDenied = oops.Define(oops.Code("PERMISSION_DENIED"))
    ErrInvalidArgs      = oops.Define(oops.Code("INVALID_ARGS"))
)

// World state errors carry player-facing message and internal cause
// Example: oops.Code("WORLD_ERROR").With("message", "There's no exit to the north.").Wrap(cause)
func WorldError(message string, cause error) error {
    return oops.Code("WORLD_ERROR").With("message", message).Wrap(cause)
}

// Extract player-facing message from oops error
func PlayerMessage(err error) string {
    if msg, ok := oops.Get(err, "message").(string); ok {
        return msg
    }
    return "Something went wrong. Try again."
}
```

### Feedback Patterns

**Success feedback** varies by command:

- `say hello` → "You say, "hello"" (confirmation)
- `look` → Room description (content)
- `move north` → New room description (content)
- `alias add l=look` → "Alias added: l → look" (confirmation)

**Error feedback** is contextual:

- Shows usage for argument errors
- Describes world state for game errors
- Generic message for internal failures (no stack traces to players)

### Logging

All command executions logged with:

- Character ID, command name, success/failure
- Error details (full context) on failure
- Player input at DEBUG level only (privacy)

---

## Help System

### Help Command

The `help` command is implemented as a Lua plugin to prove the plugin model, querying
the Go registry for command metadata via host functions.

```
help              → List all available commands (that player can use)
help <command>    → Show detailed help for command
help search <term> → Search help text for term (in-memory substring match)
```

### Capability Filtering

Help only shows commands the player can execute:

```go
func AvailableCommands(ctx context.Context, charID ulid.ULID) []CommandEntry {
    var available []CommandEntry
    for _, entry := range registry.All() {
        if hasAllCapabilities(ctx, charID, entry.Capabilities) {
            available = append(available, entry)
        }
    }
    return available
}
```

### Host Functions

Help plugin needs to query registry from Lua:

```protobuf
// In hostfunc.proto
rpc ListCommands(ListCommandsRequest) returns (ListCommandsResponse);
rpc GetCommandHelp(GetCommandHelpRequest) returns (GetCommandHelpResponse);
```

**Blocking Semantics**: Host functions are synchronous calls executed during event
handling. They MUST NOT block on I/O, network calls, or other plugins. The registry
queries above are in-memory lookups and complete in microseconds. This is distinct
from async event delivery between plugins.

### Rendering

Help content is markdown. Rendering depends on client:

- **Telnet**: Strip or convert markdown to plain text
- **Web**: Render rich markdown

---

## Go vs Lua Command Execution

### Partitioning Strategy

| Implementation | Commands | Rationale |
|----------------|----------|-----------|
| **Go** | `look`, `move`, `quit`, `who`, `boot`, `shutdown`, `wall` | Core engine, admin, performance-critical |
| **Lua** | `say`, `pose`, `emit`, `dig`, `create`, `describe`, `link`, `help` | Proves plugin model, customizable |

### Go Command Execution

Direct function call with injected services:

```go
func LookHandler(ctx context.Context, exec *CommandExecution) error {
    room, err := exec.Services.World.GetLocation(ctx, exec.LocationID)
    if err != nil {
        return WorldError("You can't see anything here.", err)
    }
    fmt.Fprintf(exec.Output, "%s\n%s\n", room.Name, room.Description)
    return nil
}
```

Handlers access services via `exec.Services` rather than storing dependencies.
This keeps handlers stateless and testable.

### Lua Command Execution

Registry holds a dispatcher that routes to plugin. The dispatcher creates an internal
`command` event type (not persisted to EventStore) to invoke the plugin handler:

```go
func LuaDispatcher(pluginName, cmdName string, host plugin.Host) CommandHandler {
    return func(ctx context.Context, exec *CommandExecution) error {
        event := pluginpkg.Event{
            Type:      "command",
            Stream:    fmt.Sprintf("char:%s", exec.CharacterID),
            ActorID:   exec.CharacterID.String(),
            ActorKind: pluginpkg.ActorKindCharacter,
            Payload:   toJSON(CommandPayload{Name: cmdName, Args: exec.Args}),
        }

        responses, err := host.DeliverEvent(ctx, pluginName, event)
        if err != nil {
            return err
        }

        for _, resp := range responses {
            fmt.Fprint(exec.Output, resp.Payload)
        }
        return nil
    }
}
```

### Lua Plugin Handler

```lua
function handle_command(event)
    local cmd = json.decode(event.payload)
    if cmd.name == "say" then
        host.emit_event({
            stream = "location:" .. event.location_id,
            type = "say",
            payload = json.encode({message = cmd.args})
        })
        return {output = string.format('You say, "%s"', cmd.args)}
    end
end
```

---

## Sequence Diagrams

### Basic Command Flow (Go)

```
┌──────┐    ┌─────────┐    ┌──────────┐    ┌────────┐    ┌───────┐
│Player│    │ Telnet  │    │ Registry │    │ Access │    │Handler│
└──┬───┘    └────┬────┘    └────┬─────┘    └───┬────┘    └───┬───┘
   │  "look"     │              │              │             │
   │────────────>│              │              │             │
   │             │ Get("look")  │              │             │
   │             │─────────────>│              │             │
   │             │   entry      │              │             │
   │             │<─────────────│              │             │
   │             │ HasCapability(world.look)   │             │
   │             │────────────────────────────>│             │
   │             │              true           │             │
   │             │<────────────────────────────│             │
   │             │ Execute(ctx, exec)                        │
   │             │──────────────────────────────────────────>│
   │             │              room description             │
   │             │<──────────────────────────────────────────│
   │ output      │              │              │             │
   │<────────────│              │              │             │
```

### Lua Command Flow

```
┌──────┐    ┌─────────┐    ┌──────────┐    ┌──────┐    ┌──────────┐
│Player│    │ Telnet  │    │ Registry │    │ Host │    │Lua Plugin│
└──┬───┘    └────┬────┘    └────┬─────┘    └──┬───┘    └────┬─────┘
   │  "say hi"   │              │             │              │
   │────────────>│              │             │              │
   │             │ Get("say")   │             │              │
   │             │─────────────>│             │              │
   │             │ LuaDispatcher│             │              │
   │             │<─────────────│             │              │
   │             │ (capability check...)      │              │
   │             │ DeliverEvent(command)      │              │
   │             │───────────────────────────>│              │
   │             │              │             │ handle_cmd   │
   │             │              │             │─────────────>│
   │             │              │             │ emit_event   │
   │             │              │             │<─────────────│
   │             │              │             │   output     │
   │             │              │             │<─────────────│
   │             │      response              │              │
   │             │<───────────────────────────│              │
   │ output      │              │             │              │
   │<────────────│              │             │              │
```

### Alias Resolution Flow

```
┌──────┐    ┌─────────┐    ┌───────────┐    ┌──────────┐
│Player│    │ Telnet  │    │AliasStore │    │ Registry │
└──┬───┘    └────┬────┘    └─────┬─────┘    └────┬─────┘
   │  "l"        │               │               │
   │────────────>│               │               │
   │             │ Get("l")      │               │
   │             │──────────────────────────────>│
   │             │      not found                │
   │             │<──────────────────────────────│
   │             │ PlayerAlias(charID, "l")      │
   │             │──────────────>│               │
   │             │    not found  │               │
   │             │<──────────────│               │
   │             │ SystemAlias("l")              │
   │             │──────────────>│               │
   │             │    "look"     │               │
   │             │<──────────────│               │
   │             │ Get("look")   │               │
   │             │──────────────────────────────>│
   │             │      entry                    │
   │             │<──────────────────────────────│
   │             │ (continue with look...)       │
```

---

## Deliverables

### New Components

| Component | Package | Description |
|-----------|---------|-------------|
| `CommandRegistry` | `internal/command` | Central dispatch table |
| `CommandDispatcher` | `internal/command` | Parse → alias → dispatch → execute |
| `AliasStore` | `internal/command` | Repository interface for alias persistence |
| `AliasRepository` | `internal/store` | PostgreSQL implementation |

### Schema Changes

- `system_aliases` table
- `player_aliases` table

### Proto Changes

- `ListCommands`, `GetCommandHelp` host functions for Lua help plugin

### Manifest Schema Changes

```yaml
# New fields in plugin.yaml
commands:
  - name: string
    capabilities: [string]
    help: string
    usage: string
    helpText: string   # OR
    helpFile: string   # (mutually exclusive)
```

---

## ADRs Required

| ADR | Decision | Rationale |
|-----|----------|-----------|
| Command Registry Storage | In-memory only | Small corpus, fast search, avoids DB sync complexity. Revisit for semantic search. |
| Unified Command Registry | Single registry for Go + Lua | Uniform error handling, clean help introspection, plugins can override builtins if allowed. |
| Command Declaration Model | Manifest-declared at load time | Commands visible at startup, clean unload, matches event subscription pattern. |
| Command Security Model | Capability-based (fine-grained) | Integrates with ABAC, granular permissions, uniform for Go and Lua. |
| Command Conflict Resolution | Last-loaded wins with warning | Plugins loaded after core can override commands. Server logs warning on startup. Plugins SHOULD NOT override core commands without explicit admin configuration. |

---

## Out of Scope

- Command history/recall (future feature)
- Tab completion (future, client-side for web)
- Semantic search for help (future, requires embeddings)
- Runtime command registration (commands are static per plugin version)

---

## References

- [HoloMUSH Roadmap](../plans/2026-01-18-holomush-roadmap-design.md) - Epic 6 definition
- [Plugin System Design](./2026-01-18-plugin-system-design.md) - Lua plugin architecture
