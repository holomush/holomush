# Commands & Behaviors Architecture

**Epic:** holomush-wr9 | **Status:** Design | **Date:** 2026-02-02

This document specifies the architecture for the HoloMUSH command system, covering input
parsing, command dispatch, alias resolution, security, and help integration.

---

## Overview

The Commands & Behaviors system transforms player input into game actions through a unified
dispatch system supporting both Go core commands and Lua plugin commands.

### Command Lifecycle

```text
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

- **Single registry**: All commands (Go and Lua) MUST register in one central registry
- **Manifest-declared**: Lua plugins MUST declare commands in `plugin.yaml`
- **Capability-gated**: Commands MUST declare required capabilities; ABAC MUST check before execution
- **Event-driven outcomes**: Commands that change state MUST emit events as source of truth

### Key Boundaries

- Parser MUST be protocol-agnostic (works for telnet and gRPC)
- Command handlers MUST receive validated, parsed input
- Handlers MUST operate on character context, not connection details

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
// Handlers MUST access services only through exec.Services getters.
type Services struct {
    world       WorldService         // private - use World() getter
    session     core.SessionService  // private - use Session() getter
    access      access.AccessControl // private - use Access() getter
    events      core.EventStore      // private - use Events() getter
    broadcaster EventBroadcaster     // private - use Broadcaster() getter
}

// Getter methods provide read-only access to services.
// This prevents handlers from replacing service implementations.
func (s *Services) World() WorldService              { return s.world }
func (s *Services) Session() core.SessionService     { return s.session }
func (s *Services) Access() access.AccessControl     { return s.access }
func (s *Services) Events() core.EventStore          { return s.events }
func (s *Services) Broadcaster() EventBroadcaster    { return s.broadcaster }

// NOTE: The Services struct uses interfaces rather than concrete types.
// This is intentional and follows Go best practice ("accept interfaces, return structs"):
// - Enables flexible implementation (production vs test implementations)
// - Allows command handlers to be tested with mocks
// - Decouples handlers from concrete service implementations
// - WorldService and EventBroadcaster interfaces define only the methods handlers actually need
```

### Go Command Registration

Core commands register at server startup:

```go
registry.Register(CommandEntry{
    Name:         "look",
    Handler:      handlers.Look,
    Capabilities: nil, // Core navigation commands are intentionally unrestricted
    Help:         "Look at your surroundings",
    Usage:        "look [target]",
    Source:       "core",
})
```

**Core Command Capability Decision**: Basic player commands (`look`, `move`, `quit`, `who`)
are intentionally registered without capability requirements. These are fundamental actions
that all players MUST be able to perform regardless of their granted capabilities:

- `look` - Players MUST always be able to see their surroundings
- `move` - Players MUST always be able to navigate between locations
- `quit` - Players MUST always be able to disconnect their session
- `who` - Players MUST always be able to see who is online

Restricting basic navigation would break the core gameplay loop. Admin commands (`boot`,
`shutdown`, `wall`) still require appropriate `admin.*` capabilities.

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

Aliases MUST be stored in PostgreSQL:

```sql
CREATE TABLE system_aliases (
    alias       TEXT PRIMARY KEY,
    command     TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by  TEXT REFERENCES players(id)  -- ULID stored as TEXT
);

CREATE TABLE player_aliases (
    player_id   TEXT REFERENCES players(id),  -- ULID stored as TEXT
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

### Per-Player Alias Rationale

Aliases are scoped to players (not characters) for the following reasons:

- **Muscle memory**: Players develop command shortcuts over time; these SHOULD persist
  across all their characters
- **Accessibility**: Players with accessibility needs MAY rely on custom aliases;
  forcing per-character setup creates unnecessary friction
- **Simplicity**: One alias namespace per player is easier to manage than per-character
- **MUSH tradition**: Classic MUSHes scope aliases to connections/players, not characters

If per-character aliases are needed in the future, they can be added as a separate
lower-priority layer in the resolution order.

### Validation

**Command names** MUST match: `^[a-zA-Z][a-zA-Z0-9_!?@#$%^+-]{0,19}$`

- MUST start with a letter (not digits or special characters)
- MAY contain special characters after first char: `_!?@#$%^+-`
- MUST NOT exceed 20 characters
- Examples: `look`, `create`, `who`, `say!`, `cmd@test` are valid; `@create`, `+who`, `123go`, `*star` are invalid
- Note: `@` and `+` are allowed after the first character, but disallowed as leading prefixes

**Player aliases** MUST match: `^[a-zA-Z][a-zA-Z0-9_!?@#$%^+-]{0,19}$`

- MUST start with a letter (not digits)
- MAY contain special characters after first char
- MUST NOT exceed 20 characters

**System aliases** MUST follow the same pattern as player aliases.

Aliases follow the same validation as command names for consistency and a simpler mental model.

**Implementation note**: Validation constants SHOULD be defined in `internal/command/validation.go`.

**Circular alias detection**: Alias creation MUST detect and reject circular alias chains.
The resolver MUST track expansion depth and fail if it exceeds 10 expansions (indicating
a cycle or excessively long chain). Circular aliases MUST be rejected with the error:
"Alias rejected: circular reference detected (expansion depth exceeded)".

### Management Commands

| Command                          | Capabilities   | Description         |
| -------------------------------- | -------------- | ------------------- |
| `alias <alias>=<command>`        | `player.alias` | Add player alias    |
| `unalias <alias>`                | `player.alias` | Remove player alias |
| `aliases`                        | `player.alias` | List player aliases |
| `sysalias add <alias>=<command>` | `admin.alias`  | Add system alias    |
| `sysalias remove <alias>`        | `admin.alias`  | Remove system alias |
| `sysalias list`                  | `admin.alias`  | List system aliases |

### Shadow Warnings

When creating an alias, the system SHOULD warn if it shadows:

- **Existing command**: "Warning: 'look' is an existing command. Your alias will override it."
- **System alias** (for player aliases): "Warning: 'l' is a system alias for 'look'. Your alias will take precedence."
- **Own existing alias**: "Warning: Replacing existing alias 'l' (was: 'look')."

Warnings are informational - the operation MUST still succeed. Player has final say over
their own alias namespace.

System alias creation SHOULD warn if shadowing a command but MUST block if shadowing
another system alias (use `sysalias remove` first).

### Alias Caching

Alias resolution MUST use in-memory caching for performance.

**Cache Strategy:**

- System aliases MUST be loaded into memory at server startup
- Player aliases MUST be loaded when a session is established
- Cache MUST be invalidated when aliases are created, updated, or deleted

**Data Structure:**

```go
type AliasCache struct {
    playerAliases map[ulid.ULID]map[string]string // playerID → alias → command
    systemAliases map[string]string               // alias → command
    mu            sync.RWMutex
}
```

**Performance Target:** Alias resolution from cache MUST complete in <1μs.

**Invalidation Rules:**

| Event               | Invalidation Action                        |
| ------------------- | ------------------------------------------ |
| Player alias CRUD   | Invalidate that player's cache entry       |
| System alias CRUD   | Invalidate entire system alias cache       |
| Session termination | Remove player's entry from `playerAliases` |

The cache is authoritative during operation. Database reads occur only at startup
and session establishment; writes always update both cache and database atomically.

---

## Security & Capabilities

### Capability Model

Every command MUST declare required capabilities. The dispatcher MUST check the character
has ALL capabilities before execution.

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

| Category   | Examples                                                    |
| ---------- | ----------------------------------------------------------- |
| `world.*`  | `world.look`, `world.move`, `world.examine`                 |
| `comms.*`  | `comms.say`, `comms.pose`, `comms.emit`                     |
| `build.*`  | `build.dig`, `build.create`, `build.link`, `build.set`      |
| `player.*` | `player.alias`, `player.who`, `player.quit`                 |
| `admin.*`  | `admin.boot`, `admin.shutdown`, `admin.wall`, `admin.alias` |

### Default Grants

New characters MUST receive a default capability set:

- `world.*` - can look, move, examine
- `comms.*` - can say, pose, emit
- `player.*` - can manage own aliases, see who, quit

Building and admin capabilities MUST require explicit grants.

### Wildcard Capability Matching

Wildcard capabilities use suffix matching with the `*` character:

- `world.*` grants ALL capabilities in the `world` namespace (`world.look`, `world.move`, etc.)
- `*` by itself MUST NOT be valid (no "grant everything" wildcard)
- Wildcards MUST only appear at the end of a capability string
- Wildcards MUST match complete segments: `world.*` matches `world.look` but NOT `worldwide.look`
- Wildcards apply to all nested levels: `world.*` matches both `world.look` and `world.admin.boot`
- Explicit capabilities take precedence: if a character has `world.*` but is explicitly denied
  `world.teleport`, the denial MUST win

Matching algorithm:

1. Check for exact capability match first
2. If no exact match, check for wildcard grants (e.g., `world.*` for `world.look`)
3. Wildcards MUST be stored and checked as grants, not expanded at assignment time

### ABAC Integration

The capability check delegates to `internal/access` evaluator:

```go
func HasCapability(ctx context.Context, charID ulid.ULID, cap string) bool {
    return evaluator.Evaluate(ctx, Subject{CharacterID: charID}, cap, nil)
}
```

This keeps the command system decoupled from ABAC implementation. Epic 7 can enhance
the evaluator without changing command dispatch.

### Rate Limiting

Rate limiting prevents command flooding (DoS protection) while allowing legitimate burst usage.

**Algorithm**: Token bucket with per-session tracking.

**Default Limits**:

| Parameter      | Value             | Notes                         |
| -------------- | ----------------- | ----------------------------- |
| Burst capacity | 10 commands       | Maximum burst size            |
| Sustained rate | 2 commands/second | Token refill rate             |
| Configuration  | Server settings   | Operators MAY adjust defaults |

**Limiting Dimensions**:

- **Per-session**: Primary rate limiting MUST be applied per session
- **Per-capability**: Commands MAY specify different limits (e.g., admin commands)

**Bypass**: Characters with `admin.ratelimit.bypass` capability MUST be exempt from rate limiting.

**Error Handling**:

```go
func ErrRateLimited(cooldownMs int64) error {
    return oops.Code("RATE_LIMITED").
        With("cooldown_ms", cooldownMs).
        Errorf("Too many commands. Please slow down.")
}
```

The error SHOULD include cooldown duration in context for client display.

**Observability**:

- Metric: `holomush_command_rate_limited_total` (counter, labels: `command`)

---

## Error Handling

### Error Categories

| Category          | Player Message                                      | Log Level |
| ----------------- | --------------------------------------------------- | --------- |
| Unknown command   | "Unknown command. Try 'help'."                      | Debug     |
| Permission denied | "You don't have permission to do that."             | Info      |
| Invalid arguments | "Usage: {usage}"                                    | Debug     |
| World state error | Descriptive (e.g., "There's no exit to the north.") | Debug     |
| Internal error    | "Something went wrong. Try again."                  | Error     |

### Error Types

Command errors MUST use the `oops` package for consistency with the codebase:

```go
// Error constructors for dispatch failures (oops doesn't have sentinel errors)
func ErrUnknownCommand(cmd string) error {
    return oops.Code("UNKNOWN_COMMAND").With("command", cmd).Errorf("unknown command: %s", cmd)
}

func ErrPermissionDenied(cmd, capability string) error {
    return oops.Code("PERMISSION_DENIED").
        With("command", cmd).
        With("capability", capability).
        Errorf("permission denied for command %s", cmd)
}

func ErrInvalidArgs(cmd, usage string) error {
    return oops.Code("INVALID_ARGS").With("command", cmd).With("usage", usage).Errorf("invalid arguments")
}

// World state errors carry player-facing message and internal cause
func WorldError(message string, cause error) error {
    return oops.Code("WORLD_ERROR").With("message", message).Wrap(cause)
}

// Extract player-facing message from oops error
func PlayerMessage(err error) string {
    if oopsErr, ok := oops.AsOops(err); ok {
        if msg, exists := oopsErr.Context()["message"]; exists {
            if s, ok := msg.(string); ok {
                return s
            }
        }
    }
    return "Something went wrong. Try again."
}
```

### Feedback Patterns

**Success feedback** varies by command:

- `say hello` → "You say, "hello"" (confirmation)
- `look` → Room description (content)
- `move north` → New room description (content)
- `alias l=look` → "Alias added: l → look" (confirmation)

**Error feedback** is contextual:

- Shows usage for argument errors
- Describes world state for game errors
- Generic message for internal failures (no stack traces to players)

### Logging

All command executions MUST be logged with:

- Character ID, command name, success/failure
- Error details (full context) on failure
- Player input MUST be logged at DEBUG level only (privacy protection)

### Observability (OpenTelemetry)

Command execution integrates with the existing OTel infrastructure from Epic 1:

**Tracing**: Each command execution creates a span:

```go
ctx, span := tracer.Start(ctx, "command.execute",
    trace.WithAttributes(
        attribute.String("command.name", cmdName),
        attribute.String("command.source", entry.Source),
        attribute.String("character.id", exec.CharacterID.String()),
    ),
)
defer span.End()
```

**Baggage**: Command context propagates via OTel baggage:

- `command.name` - for downstream correlation
- `player.id` - authenticated player
- `session.id` - links to session traces
- `character.id` - links to world model operations

**Metrics**: Command dispatcher exports:

- `holomush_command_executions_total` (counter) - by command name, source, status
- `holomush_command_duration_seconds` (histogram) - execution latency by command
- `holomush_alias_expansions_total` (counter) - alias usage patterns

Lua plugin command execution inherits the parent span context, allowing traces
to flow through Go → Lua → host function calls.

---

## Help System

### Help Command

The `help` command is implemented as a Lua plugin to prove the plugin model, querying
the Go registry for command metadata via host functions.

```text
help              → List all available commands (that player can use)
help <command>    → Show detailed help for command
help search <term> → Search command name, help, and usage fields (in-memory substring match)
```

### Capability Filtering

Help MUST only show commands the player can execute:

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
// In api/proto/plugin/v1/hostfunc.proto
rpc ListCommands(ListCommandsRequest) returns (ListCommandsResponse);
rpc GetCommandHelp(GetCommandHelpRequest) returns (GetCommandHelpResponse);
```

**Blocking Semantics**: Host functions are synchronous calls executed during event
handling. They MUST NOT block on I/O, network calls, or other plugins. The registry
queries above are in-memory lookups and complete in microseconds. This is distinct
from async event delivery between plugins.

**Error Handling**: Host function errors MUST be handled as follows:

| Error Type        | Host Behavior                                | Lua Behavior                         |
| ----------------- | -------------------------------------------- | ------------------------------------ |
| Invalid arguments | Return error with code `HOST_INVALID_ARGS`   | Lua receives `nil, error_message`    |
| Internal failure  | Return error with code `HOST_INTERNAL_ERROR` | Lua receives `nil, "internal error"` |
| Not found         | Return empty result (not an error)           | Lua receives empty table/nil         |
| Timeout           | N/A (host functions are synchronous)         | N/A                                  |

Lua plugins SHOULD check return values and handle errors gracefully:

```lua
local commands, err = host.list_commands()
if err then
    log.error("failed to list commands: " .. err)
    return {output = "Unable to display help. Try again later."}
end
```

### Rendering

Help content MUST be markdown. Rendering depends on client:

- **Telnet**: MUST strip or convert markdown to plain text
- **Web**: SHOULD render rich markdown

---

## Go vs Lua Command Execution

### Partitioning Strategy

| Implementation | Commands                                                                    | Rationale                                |
| -------------- | --------------------------------------------------------------------------- | ---------------------------------------- |
| **Go**         | `look`, `move`, `quit`, `who`, `boot`, `shutdown`, `wall`, `create`, `set`   | Core engine, admin, performance-critical |
| **Lua**        | `say`, `pose`, `emit`, `dig`, `link`, `help`                                | Proves plugin model, customizable        |

**Capability Requirements**:

- Core navigation (`look`, `move`, `quit`, `who`): **Unrestricted** - no capability check
- Admin commands (`boot`, `shutdown`, `wall`): Require `admin.*` capabilities
- Lua plugin commands: Require capabilities declared in `plugin.yaml`

### Go Command Execution

Direct function call with injected services:

```go
func LookHandler(ctx context.Context, exec *CommandExecution) error {
    room, err := exec.Services.World().GetLocation(ctx, exec.LocationID)
    if err != nil {
        return WorldError("You can't see anything here.", err)
    }
    fmt.Fprintf(exec.Output, "%s\n%s\n", room.Name, room.Description)
    return nil
}
```

Handlers access services via `exec.Services` getters (e.g., `exec.Services.World()`)
rather than storing dependencies. This keeps handlers stateless and testable.

### Lua Command Execution

Registry holds a dispatcher that routes to plugin. The dispatcher creates an internal
`command` event type (not persisted to EventStore) to invoke the plugin handler.

**State Limitations**: Each command invocation creates a fresh Lua VM state. State does
NOT persist between invocations. For v1, commands requiring confirmation SHOULD use
`--confirm` flags or host function KV store for state (see [ADR 0005](../adr/0005-command-state-management.md)).
Multi-turn command wizards are explicitly out of scope for v1.

```go
func LuaDispatcher(pluginName, cmdName string, host plugin.Host) CommandHandler {
    return func(ctx context.Context, exec *CommandExecution) error {
        // Note: ID and Timestamp are generated by the event system
        // EventTypeCommand would need to be added to pkg/plugin/event.go
        event := pluginpkg.Event{
            Type:      pluginpkg.EventType("command"),
            Stream:    fmt.Sprintf("char:%s", exec.CharacterID),
            ActorID:   exec.CharacterID.String(),
            ActorKind: pluginpkg.ActorCharacter,
            Payload:   toJSON(CommandPayload{Name: cmdName, Args: exec.Args}),
        }

        emits, err := host.DeliverEvent(ctx, pluginName, event)
        if err != nil {
            return err
        }

        for _, emit := range emits {
            fmt.Fprint(exec.Output, emit.Payload)
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

```text
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

```text
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

```text
┌──────┐    ┌─────────┐    ┌───────────┐    ┌──────────┐
│Player│    │ Telnet  │    │AliasStore │    │ Registry │
└──┬───┘    └────┬────┘    └─────┬─────┘    └────┬─────┘
   │  "l"        │               │               │
   │────────────>│               │               │
   │             │ Get("l")      │               │
   │             │──────────────────────────────>│
   │             │      not found                │
   │             │<──────────────────────────────│
   │             │ PlayerAlias(playerID, "l")    │
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

| Component           | Package            | Description                                |
| ------------------- | ------------------ | ------------------------------------------ |
| `CommandRegistry`   | `internal/command` | Central dispatch table                     |
| `CommandDispatcher` | `internal/command` | Parse → alias → dispatch → execute         |
| `AliasStore`        | `internal/command` | Repository interface for alias persistence |
| `AliasRepository`   | `internal/store`   | PostgreSQL implementation                  |

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

| ADR                         | Decision                                               | Rationale                                                                                                                                                                                                                                                  |
| --------------------------- | ------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Command Registry Storage    | In-memory only                                         | Small corpus, fast search, avoids DB sync complexity. Revisit for semantic search.                                                                                                                                                                         |
| Unified Command Registry    | Single registry for Go + Lua                           | Uniform error handling, clean help introspection, plugins can override builtins if allowed.                                                                                                                                                                |
| Command Declaration Model   | Manifest-declared at load time                         | Commands visible at startup, clean unload, matches event subscription pattern.                                                                                                                                                                             |
| Command Security Model      | Capability-based (fine-grained)                        | Integrates with ABAC, granular permissions, uniform for Go and Lua.                                                                                                                                                                                        |
| Command Conflict Resolution | Alphabetical load order, last-loaded wins with warning | Plugins MUST load in alphabetical order by plugin name. Within a plugin, commands register in manifest order. Last registration wins. Server MUST log warning on conflict. Plugins SHOULD NOT override core commands without explicit admin configuration. |
| Command State Management    | Deferred (see ADR 0005)                                | v1 uses ephemeral state; multi-turn commands deferred                                                                                                                                                                                                      |

---

## Non-Goals

- Command history/recall (future feature)
- Tab completion (future, client-side for web)
- Semantic search for help (future, requires embeddings)
- Runtime command registration (commands are static per plugin version)
- Multi-turn command wizards (Lua state is ephemeral per command execution)
- Command-scoped state persistence (future: use session-scoped KV store host functions)

---

## References

- [HoloMUSH Roadmap](../plans/2026-01-18-holomush-roadmap-design.md) - Epic 6 definition
- [Plugin System Design](./2026-01-18-plugin-system-design.md) - Lua plugin architecture
- [ADR 0005: Command State Management](../adr/0005-command-state-management.md) - Deferred multi-turn command design
