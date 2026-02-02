# ADR 0005: Command State Management for Multi-Turn Interactions

**Date:** 2026-02-02
**Status:** Proposed (Deferred to post-v1)
**Deciders:** HoloMUSH Contributors

## Context

Many MUSH commands benefit from multi-turn interactions:

- **Confirmation dialogs**: "Are you sure you want to delete this room? (yes/no)"
- **Wizards**: Step-by-step object creation with prompts for name, description, attributes
- **Editors**: In-line text editing sessions for descriptions
- **Menus**: Numbered option selection for complex commands

HoloMUSH uses gopher-lua for Lua plugin commands. Each command invocation creates a
fresh Lua VM state that is discarded after execution. This design choice provides:

- Isolation between command invocations (no accidental state leakage)
- Predictable behavior (same input always produces same output)
- Simple concurrency model (no shared mutable state)
- Fast startup (~50μs per VM vs ~1.5s for WASM)

However, it means Lua commands cannot maintain state between invocations. A "wizard"
command cannot remember which step the user is on.

## Options Considered

### Option A: Continuation Tokens

Commands return a continuation token in their response. The next command invocation
includes this token, allowing the handler to reconstruct state.

```lua
function handle_command(event)
    local state = decode_continuation(event.continuation_token)
    if state.step == 1 then
        return {
            output = "Enter room name:",
            continuation = encode_continuation({step = 2})
        }
    end
end
```

**Pros:**

- Stateless server, all state in token
- Works with load balancing

**Cons:**

- Tokens can be replayed, forged, or tampered with
- Requires cryptographic signing and validation
- Large tokens for complex state
- Client must return token with next input

### Option B: Session-Scoped State Store

Host functions provide a key-value store scoped to the player's session:

```lua
function handle_command(event)
    local step = host.session_get("create_room_step") or 1
    if step == 1 then
        host.session_set("create_room_step", 2)
        return {output = "Enter room name:"}
    end
end
```

**Pros:**

- Simple API for plugin authors
- State persists across commands within session
- No client cooperation required

**Cons:**

- Requires session state storage (in-memory or database)
- State cleanup on session end
- Potential for orphaned state if command flow is interrupted

### Option C: Go State Machines

Multi-turn commands are implemented in Go with explicit state machines:

```go
type CreateRoomWizard struct {
    step    int
    name    string
    desc    string
}

func (w *CreateRoomWizard) HandleInput(input string) (output string, done bool) {
    // State machine logic
}
```

**Pros:**

- Full control over state management
- Type-safe state transitions
- Can use Go's concurrency primitives

**Cons:**

- Defeats purpose of Lua plugins for customization
- More boilerplate for simple interactions

### Option D: Lua State Pooling

Maintain a pool of Lua VMs per plugin, reusing them for the same session:

**Pros:**

- Lua state naturally persists within VM
- Familiar programming model

**Cons:**

- Complex lifecycle management
- Memory pressure from many concurrent sessions
- State leakage between different commands in same session
- Defeats isolation benefits of fresh VMs

## Decision

**Deferred to post-v1.** For the initial release, multi-turn command wizards are
explicitly out of scope. Commands MUST be designed as single-turn interactions.

For v1, workarounds include:

1. **Confirmation flags**: Use `--confirm` or `--force` flags for destructive actions
   instead of interactive confirmation dialogs.

2. **Single-command completion**: Design commands to gather all input at once:
   `@create room "The Library"="A dusty room filled with books."`

3. **Host function KV store** (if implemented): Use session-scoped storage for
   commands that absolutely need state between invocations.

Post-v1, the recommended approach is **Option B: Session-Scoped State Store** because:

- It provides the cleanest API for plugin authors
- It aligns with existing session management infrastructure
- It avoids client-side complexity of continuation tokens
- It maintains the isolation benefits of fresh Lua VMs

## Consequences

### v1 Implementations MUST NOT

- Assume Lua state persists between command invocations
- Implement multi-turn wizards that depend on Lua-side state
- Store command progress in global Lua variables
- Use Lua coroutines expecting they survive command completion

### v1 Implementations SHOULD

- Use confirmation flags (`--confirm`, `--force`) for destructive operations
- Accept all required input in a single command invocation
- Document single-turn limitations in plugin authoring guides

### Future Work

When implementing session-scoped state (post-v1):

- Define host function API: `session_get(key)`, `session_set(key, value)`, `session_delete(key)`
- Implement state cleanup on session termination
- Add TTL support for automatic state expiration
- Consider state size limits to prevent abuse

## References

- [Commands & Behaviors Design](../specs/2026-02-02-commands-behaviors-design.md) - Parent spec
- [Plugin System Design](../specs/2026-01-18-plugin-system-design.md) - Lua VM architecture
- Implementation: `internal/plugin/lua_host.go`
