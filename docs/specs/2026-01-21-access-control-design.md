# Core Access Control Design

**Status:** Draft
**Date:** 2026-01-21
**Epic:** holomush-ql5 (Epic 3: Core Access Control)
**Task:** holomush-ql5.1

## Overview

This document defines the core access control architecture for HoloMUSH. The design
provides a minimal permission system that all game systems can build against, with a
clear evolution path from static roles to full Attribute-Based Access Control (ABAC).

### Goals

- Single `AccessControl` interface for all permission checks
- Uniform string-based format for subjects, actions, and resources
- Static role composition model (admin, builder, player)
- Dynamic permission tokens (`$self`, `$here`) for contextual access
- Integration with existing `capability.Enforcer` for plugin permissions
- Event system filtering at delivery time

### Non-Goals

- Full ABAC implementation (deferred to future epic)
- Per-object ACLs (deferred)
- Permission inheritance hierarchies (using composition instead)
- Audit logging (separate concern, not in this design)

## Architecture

```text
┌─────────────────────────────────────────────────────────────────┐
│                      AccessControl                               │
│                                                                   │
│   Check(ctx, subject, action, resource string) bool              │
│                                                                   │
├─────────────────────────────────────────────────────────────────┤
│                                                                   │
│   ┌─────────────────────┐     ┌─────────────────────┐           │
│   │ StaticAccessControl │     │ LocationResolver    │           │
│   │ (MVP Implementation)│────►│ - CurrentLocation   │           │
│   │                     │     │ - CharactersAt      │           │
│   └──────────┬──────────┘     └─────────────────────┘           │
│              │                                                    │
│              │ delegates for plugin subjects                     │
│              ▼                                                    │
│   ┌─────────────────────┐                                        │
│   │ capability.Enforcer │                                        │
│   │ (existing)          │                                        │
│   └─────────────────────┘                                        │
│                                                                   │
└─────────────────────────────────────────────────────────────────┘
```

### Package Structure

```text
internal/access/
  access.go          # AccessControl interface
  static.go          # StaticAccessControl implementation (MVP)
  resolver.go        # LocationResolver interface
  permissions.go     # Permission group definitions

internal/access/accesstest/
  mock.go            # Test helpers
```

## Core Interface

```go
// AccessControl checks permissions for all subjects in HoloMUSH.
// This is the single entry point for all authorization.
type AccessControl interface {
    // Check returns true if subject is allowed to perform action on resource.
    // Returns false for unknown subjects or denied permissions (deny by default).
    //
    // All parameters use prefixed string format for consistency:
    //   - subject: "char:01ABC", "session:01XYZ", "plugin:echo-bot", "system"
    //   - action: "read", "write", "emit", "execute", "grant", etc.
    //   - resource: "location:01ABC", "character:*", "stream:location:*", etc.
    Check(ctx context.Context, subject, action, resource string) bool
}
```

### Design Rationale

| Choice            | Rationale                                                        |
| ----------------- | ---------------------------------------------------------------- |
| All strings       | Uniform format, extensible by plugins/admins, no struct ceremony |
| Boolean return    | Matches existing `capability.Enforcer.Check()`, simple callers   |
| Single interface  | One place for all permission checks, easy to mock/swap           |
| Context parameter | Enables timeout, tracing, future async resolution                |

## Subject Format

Subjects identify who is requesting access using prefixed strings:

| Prefix     | Example            | Description                                |
| ---------- | ------------------ | ------------------------------------------ |
| `char:`    | `char:01ABC123`    | In-game character                          |
| `session:` | `session:01XYZ789` | Connected user session                     |
| `plugin:`  | `plugin:echo-bot`  | Plugin making host call                    |
| `system`   | `system`           | Internal system operation (always allowed) |

## Action Format

Actions are flat strings describing the operation:

| Action    | Description                |
| --------- | -------------------------- |
| `read`    | View data                  |
| `write`   | Modify data                |
| `delete`  | Remove data                |
| `emit`    | Emit events to streams     |
| `execute` | Run commands               |
| `grant`   | Modify others' permissions |

Actions are extensible. Plugins and game admins MAY define custom actions without
code changes.

## Resource Format

Resources are prefixed strings identifying what is being accessed:

| Pattern             | Description            |
| ------------------- | ---------------------- |
| `location:01ABC`    | Specific room          |
| `location:*`        | All rooms (wildcard)   |
| `character:01XYZ`   | Specific character     |
| `object:*`          | All objects            |
| `stream:location:*` | Location event streams |
| `stream:session:*`  | Session event streams  |
| `event:say`         | Specific event type    |
| `command:@dig`      | Specific command       |
| `kv:pluginname:*`   | Plugin KV namespace    |

Resources use `gobwas/glob` for pattern matching (same as `capability.Enforcer`).

## Permission Format

Permissions combine action and resource: `action:resource`

```text
read:location:*           # Read any room
emit:stream:location:*    # Emit to any location stream
execute:command:@dig      # Run @dig command
grant:character:*         # Grant permissions to any character
```

## Dynamic Permission Tokens

For contextual permissions, special tokens are resolved at check time:

| Token     | Resolves To                              |
| --------- | ---------------------------------------- |
| `$self`   | Subject's own character ID               |
| `$here`   | Subject's current location ID            |
| `$here:*` | Objects/characters at subject's location |

**Example usage in permission groups:**

```yaml
player-powers:
  - read:character:$self # Can read own character
  - write:character:$self # Can modify own character
  - read:location:$here # Can read current room
  - emit:stream:location:$here # Can speak in current room
```

**Resolution requires LocationResolver interface:**

```go
type LocationResolver interface {
    // CurrentLocation returns the location ID for a character.
    CurrentLocation(ctx context.Context, charID string) (string, error)

    // CharactersAt returns character IDs present at a location.
    CharactersAt(ctx context.Context, locationID string) ([]string, error)
}
```

## Role Composition Model

Roles compose permission groups. No inheritance - composition is explicit.

### Permission Groups

```go
type PermissionGroup struct {
    Name        string   // e.g., "player-powers"
    Permissions []string // e.g., ["read:location:$here", "emit:stream:location:$here"]
}
```

### Static Role Definitions (MVP)

```yaml
permission_groups:
  player-powers:
    # Self access
    - read:character:$self
    - write:character:$self

    # Current location access
    - read:location:$here
    - read:character:$here:*
    - read:object:$here:*
    - emit:stream:location:$here

    # Basic commands
    - execute:command:say
    - execute:command:pose
    - execute:command:look
    - execute:command:go

  builder-powers:
    # World modification
    - write:location:*
    - write:object:*
    - delete:object:*

    # Builder commands
    - execute:command:@dig
    - execute:command:@create
    - execute:command:@describe
    - execute:command:@link

  admin-powers:
    # Full access
    - read:**
    - write:**
    - delete:**
    - emit:**
    - execute:**
    - grant:**

roles:
  player:
    - player-powers

  builder:
    - player-powers
    - builder-powers

  admin:
    - player-powers
    - builder-powers
    - admin-powers
```

## Implementation

### StaticAccessControl

```go
type StaticAccessControl struct {
    roles     map[string][]string   // roleName → permission patterns
    subjects  map[string]string     // subjectID → roleName
    resolver  LocationResolver      // For $here resolution
    enforcer  *capability.Enforcer  // Delegate for plugin checks
}

func (s *StaticAccessControl) Check(ctx context.Context, subject, action, resource string) bool {
    // System always allowed
    if subject == "system" {
        return true
    }

    // Parse subject prefix
    prefix, id := parseSubject(subject)

    switch prefix {
    case "plugin":
        // Delegate to existing capability.Enforcer
        capability := action + "." + resource
        return s.enforcer.Check(id, capability)

    case "char", "session":
        role := s.subjects[subject]
        if role == "" {
            return false // Unknown subject
        }

        permissions := s.roles[role]
        requested := action + ":" + resource

        // Resolve dynamic tokens ($self, $here)
        resolved := s.resolveTokens(ctx, subject, requested)

        // Match against role permissions using glob
        return matchAny(permissions, resolved)
    }

    return false
}
```

### Role Assignment

```go
// AssignRole sets the role for a subject.
func (s *StaticAccessControl) AssignRole(subject, role string) error

// GetRole returns the current role for a subject.
func (s *StaticAccessControl) GetRole(subject string) string

// RevokeRole removes role assignment (subject becomes unauthorized).
func (s *StaticAccessControl) RevokeRole(subject string) error
```

## Event System Integration

### Event Delivery Filtering

Events are filtered at delivery time to respect dynamic permissions:

```go
func (b *Broadcaster) Deliver(ctx context.Context, event Event) {
    subscribers := b.GetSubscribers(event.Stream)

    for _, sub := range subscribers {
        resource := "stream:" + event.Stream

        if b.accessControl.Check(ctx, sub.Subject, "read", resource) {
            sub.Deliver(event)
        }
        // Silently skip unauthorized subscribers
    }
}
```

### Event Emission Checks

Emission requires explicit permission:

```go
func (h *HostFunctions) emitEvent(L *lua.LState) int {
    stream := L.CheckString(1)

    subject := "plugin:" + h.pluginName
    resource := "stream:" + stream

    if !h.accessControl.Check(ctx, subject, "emit", resource) {
        L.RaiseError("permission denied: cannot emit to %s", stream)
        return 0
    }

    // Proceed with emission...
}
```

### Behavior Summary

| Operation         | Denied Behavior                          |
| ----------------- | ---------------------------------------- |
| Event delivery    | Silent skip (subscriber doesn't receive) |
| Event emission    | Error returned to caller                 |
| Command execution | Error returned to caller                 |

## Plugin Integration

The `AccessControl` interface wraps the existing `capability.Enforcer`:

1. For `plugin:*` subjects, delegate to `enforcer.Check(pluginID, capability)`
2. Convert `action:resource` to capability format: `action.resource`
3. Existing plugin capability patterns continue to work

**Migration path:**

- Plugin host functions call `accessControl.Check()` instead of `enforcer.Check()`
- Enforcer becomes an internal implementation detail
- No changes needed to plugin manifests or capability declarations

## Testing Strategy

### Unit Tests

```go
func TestStaticAccessControl_SelfAccess(t *testing.T) {
    ac := NewStaticAccessControl(...)
    ac.AssignRole("char:01ABC", "player")

    // Can read self
    assert.True(t, ac.Check(ctx, "char:01ABC", "read", "character:01ABC"))

    // Cannot read other character
    assert.False(t, ac.Check(ctx, "char:01ABC", "read", "character:01XYZ"))
}

func TestStaticAccessControl_HereResolution(t *testing.T) {
    resolver := &mockResolver{locations: map[string]string{
        "char:01ABC": "location:room1",
    }}
    ac := NewStaticAccessControl(..., resolver)

    // Can read current location
    assert.True(t, ac.Check(ctx, "char:01ABC", "read", "location:room1"))

    // Cannot read other location
    assert.False(t, ac.Check(ctx, "char:01ABC", "read", "location:room2"))
}

func TestStaticAccessControl_PluginDelegation(t *testing.T) {
    enforcer := capability.NewEnforcer()
    enforcer.SetGrants("echo-bot", []string{"emit.stream.location.*"})

    ac := NewStaticAccessControl(..., enforcer)

    // Delegates to enforcer
    assert.True(t, ac.Check(ctx, "plugin:echo-bot", "emit", "stream:location:01ABC"))
}
```

### Integration Tests

- Event delivery respects permissions
- Command execution checks permissions
- Role assignment persists correctly
- Token resolution works with real LocationResolver

## Acceptance Criteria

- [ ] AccessControl interface defined with `Check(ctx, subject, action, resource) bool`
- [ ] Permission model documented with clear subject/action/resource taxonomy
- [ ] Static role definitions: admin, builder, player with composition
- [ ] Dynamic tokens (`$self`, `$here`) designed for contextual access
- [ ] Plugin capability integration via delegation to Enforcer
- [ ] Event permission flow documented (filter at delivery, check at emission)
- [ ] Design reviewed and approved

## Future Evolution (ABAC)

This design supports evolution to full ABAC:

1. **Add policy engine** - Replace static role lookup with policy evaluation
2. **Add attributes** - Subject, resource, and environment attributes
3. **Add conditions** - Time-based, location-based, relationship-based rules
4. **Add external data** - Query game state for complex decisions

The `AccessControl` interface remains unchanged - only implementation evolves.

## References

- [Plugin System Design](2026-01-18-plugin-system-design.md) - Capability model
- [HoloMUSH Architecture](../plans/2026-01-17-holomush-architecture-design.md)
- [gobwas/glob](https://github.com/gobwas/glob) - Pattern matching library
