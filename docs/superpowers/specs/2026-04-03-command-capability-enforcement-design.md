<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Command Capability Enforcement Design

**Date:** 2026-04-03
**Status:** Draft
**Beads:** holomush-jh4l (original bug), new epic TBD
**Supersedes:** ADR 0007 (command security model), partially supersedes ADR 0014
(direct static access control replacement — command sections only)

## Problem

Command authorization in the ABAC system is broken. There are three
independent failures:

1. **Format mismatch:** Plugin YAMLs define capabilities using dotted
   format (`comms.say`, `admin.boot`) and colon format (`admin:shutdown`)
   inconsistently. Neither format works with the ABAC engine.

2. **Wrong resource passed to engine:** The dispatcher's `CheckCapability`
   function passes the capability string (e.g., `admin:shutdown`) as the
   ABAC resource. The `CommandProvider` expects `command:<name>` format
   and returns nil for anything else. No policies match.

3. **No pre-flight capability model:** Commands declare capabilities but
   they have no effect. A malicious plugin can register a command with
   no capabilities that internally performs privileged operations through
   host functions. The only protection is the service-level ABAC check
   inside the handler — there is no defense-in-depth at dispatch time.

ADR 0014 superseded ADR 0007's capability-namespace model but the
dispatcher code was never updated to match. The seed policies follow
ADR 0014 (using `command:<name>` format), but the dispatcher still
implements ADR 0007.

## Solution

Command authorization becomes a two-layer system enforced at dispatch
time, before the handler runs. Both layers MUST pass for the command
to execute.

### Design Principles

- **Capabilities are declared intent.** A command's manifest declares
  what resource types and actions it will attempt. This serves as both
  a security pre-flight and documentation for operators.
- **Pre-flight can only deny, never grant.** Capability checks are a
  coarse filter. They cannot override instance-level ABAC checks in
  service handlers.
- **Default is least privilege.** Omitted scope defaults to `self` (own
  character only). Commands MUST explicitly declare broader scope.
- **Plugins and core commands use the same model.** No special cases.
  Core commands declare capabilities identically to plugin commands.
- **Service-level ABAC is authoritative.** Property ownership, exit
  locks, zone restrictions, and all other instance-level policies are
  enforced by the service layer, not the capability pre-flight.

## Architecture

### Two-Layer Authorization

```text
Player types command
        │
        ▼
┌─────────────────────────────────────────┐
│ Layer 1: Command Execution Check        │
│                                         │
│ engine.Evaluate(                        │
│   subject,                              │
│   "execute",                            │
│   "command:<name>"                      │
│ )                                       │
│                                         │
│ "Is this character allowed to run       │
│  this command?"                         │
│                                         │
│ Controlled by seed policies:            │
│   resource.command.name in ["say",...]  │
│   "admin" in principal.character.roles  │
└────────────┬────────────────────────────┘
             │ ALLOW
             ▼
┌─────────────────────────────────────────┐
│ Layer 2: Capability Pre-Flight          │
│                                         │
│ For each declared capability:           │
│ engine.CanPerformAction(                │
│   subject,                              │
│   capability.Action,                    │
│   capability.Resource,                  │
│   capability.Scope                      │
│ )                                       │
│                                         │
│ "Does this character have the class     │
│  of permissions this command needs?"    │
│                                         │
│ Type-level check — no specific          │
│ resource ID.                            │
└────────────┬────────────────────────────┘
             │ ALL PASS
             ▼
┌─────────────────────────────────────────┐
│ Handler Executes                        │
│                                         │
│ Handler calls service methods which     │
│ perform instance-level ABAC checks:     │
│                                         │
│ engine.Evaluate(                        │
│   subject,                              │
│   "write",                              │
│   "location:01ABC"                      │
│ )                                       │
│                                         │
│ "Can this character write to THIS       │
│  specific location?"                    │
│                                         │
│ Authoritative. Checks ownership,        │
│ locks, zone policies, etc.              │
└─────────────────────────────────────────┘
```

### Capability Format

Capabilities are structured objects in plugin manifests and Go command
registration. They are NOT strings.

**Plugin YAML:**

```yaml
commands:
  - name: dig
    capabilities:
      - action: write
        resource: location
        scope: local
      - action: write
        resource: exit
        scope: local
    help: "Create a new location and exit"
```

**Go registration:**

```go
mustRegister(command.CommandEntryConfig{
    Name:    "dig",
    Handler: DigHandler,
    Capabilities: []command.Capability{
        {Action: "write", Resource: "location", Scope: command.ScopeLocal},
        {Action: "write", Resource: "exit", Scope: command.ScopeLocal},
    },
})
```

**Go type:**

```go
// Capability declares a resource type and action that a command will
// attempt. Used for pre-flight authorization at dispatch time.
type Capability struct {
    Action   string `yaml:"action" json:"action"`
    Resource string `yaml:"resource" json:"resource"`
    Scope    string `yaml:"scope,omitempty" json:"scope,omitempty"`
}
```

### Scope

Scope defines how broadly a command operates. It controls the spatial
context of the pre-flight check.

| Scope | Meaning | Default? | Example |
| --- | --- | --- | --- |
| `self` | Player's own character only | Yes (implicit) | `setdescription` |
| `local` | Current location and its contents | No | `say`, `look`, `dig` |
| `global` | Server-wide, not tied to location | No | `wall`, `shutdown` |

**Default:** `self` (least privilege). Commands MUST explicitly declare
`local` or `global` scope when needed. Unknown or omitted scope values
are treated as `self`.

**Scope constants:**

```go
const (
    ScopeSelf   = ""       // default — own character only
    ScopeLocal  = "local"  // current location + contents
    ScopeGlobal = "global" // server-wide
)
```

### Validation

Capabilities MUST be validated at manifest load time (plugin
registration) and at command registration time (core commands).

**Validation rules:**

- `Action` MUST be non-empty
- `Action` MUST be a known action (`read`, `write`, `emit`, `enter`,
  `use`, `delete`, `execute`, `admin`)
- `Resource` MUST be non-empty
- `Resource` MUST be a known resource type (`character`, `location`,
  `exit`, `object`, `stream`, `property`, `scene`, `command`, `server`,
  `alias`, `player`)
- `Scope` MUST be `""`, `"local"`, or `"global"` if present
- Invalid capabilities MUST prevent command registration (fail-closed)

### Engine Addition: `CanPerformAction`

New method on `AccessPolicyEngine` interface:

```go
// CanPerformAction checks whether the subject could perform the given
// action on the given resource type, without requiring a specific
// resource instance. This is a type-level pre-flight check.
//
// Scope constrains the spatial context:
//   - "" (self): only the character's own resources
//   - "local": resources at the character's current location
//   - "global": any resource server-wide
//
// Returns true if any policy would potentially permit the action.
// Returns false if an unconditional forbid matches or no policies
// apply for this subject/action/resource type combination.
//
// This is a coarse check — it cannot override instance-level
// Evaluate() calls in service handlers.
CanPerformAction(ctx context.Context, subject, action, resourceType, scope string) (bool, error)
```

**Evaluation logic:**

1. Resolve subject attributes (character roles, player flags, etc.)
2. Iterate compiled policies matching the action and resource type
3. For each matching policy:
   - If it has only subject-attribute conditions (e.g., `"admin" in
     principal.character.roles`), evaluate them
   - If it has resource-specific conditions (e.g.,
     `resource.location.id == principal.character.location`), treat as
     "potentially matches" (optimistic — the handler will do the
     instance-level check)
4. If any `forbid` policy matches unconditionally on subject attributes,
   return `false`
5. If any `permit` policy matches (unconditionally or optimistically),
   return `true`
6. If no policies match, return `false` (default deny)

**Scope enforcement (initial implementation):**

For the initial release, scope is validated and stored but the pre-flight
engine treats all scopes identically — it performs the type-level check
regardless of scope. Scope-aware evaluation (e.g., checking `local`
scope against the player's current location) is a follow-up.

This means the pre-flight is slightly more permissive than declared
scope would allow. The service-level ABAC checks are still authoritative
and enforce spatial constraints.

**Scope enforcement (future):**

When scope enforcement is implemented:

- `self`: pre-flight checks that the subject can perform the action on
  their own character (resource = `character:<subject_id>`)
- `local`: pre-flight checks that policies exist for the subject's
  current location context
- `global`: no spatial constraint — type-level check only

### Command Capability Declarations

Every command MUST declare its capabilities. Core commands and plugin
commands use the same format.

**Core commands (`internal/command/handlers/register.go`):**

| Command | Capabilities |
| --- | --- |
| `say` | `emit:stream` scope `local` |
| `pose` | `emit:stream` scope `local` |
| `look` | `read:location` scope `local`, `read:character` scope `local` |
| `go` | `use:exit` scope `local`, `enter:location` scope `local` |
| `dig` | `write:location` scope `local`, `write:exit` scope `local` |
| `create` | `write:object` scope `local` |
| `describe` | `write:location` scope `local` |
| `link` | `write:exit` scope `local` |
| `home` | `enter:location` scope `local` |
| `teleport` | `enter:location` scope `global` |
| `pemit` | `emit:stream` scope `global` |
| `shutdown` | `admin:server` scope `global` |
| `wall` | `emit:stream` scope `global` |
| `resetpassword` | `write:player` scope `global` |

**Plugin commands (YAML):**

| Command | Plugin | Capabilities |
| --- | --- | --- |
| `page` | core-communication | `emit:stream` scope `local` |
| `whisper` | core-communication | `emit:stream` scope `local` |
| `emit` | core-communication | `emit:stream` scope `local` |
| `sysalias` | core-aliases | `write:alias` scope `global` |
| `sysunsalias` | core-aliases | `write:alias` scope `global` |
| `sysaliases` | core-aliases | `read:alias` scope `global` |
| `alias` | core-aliases | `write:alias` scope `self` |
| `unalias` | core-aliases | `write:alias` scope `self` |
| `aliases` | core-aliases | `read:alias` scope `self` |

### What Changes in the Dispatcher

**Current flow (broken):**

```go
for _, cap := range entry.GetCapabilities() {
    CheckCapability(ctx, d.engine, subject, cap, parsed.Name)
}
```

**New flow:**

```go
// Layer 1: command execution check
req, _ := types.NewAccessRequest(subject, "execute", "command:"+parsed.Name)
decision, _ := d.engine.Evaluate(ctx, req)
if !decision.IsAllowed() {
    return ErrPermissionDenied(parsed.Name, "execute")
}

// Layer 2: capability pre-flight
for _, cap := range entry.GetCapabilities() {
    allowed, err := d.engine.CanPerformAction(
        ctx, subject, cap.Action, cap.Resource, cap.EffectiveScope(),
    )
    if err != nil {
        return oops.Code("CAPABILITY_CHECK_FAILED").Wrap(err)
    }
    if !allowed {
        return ErrInsufficientCapability(parsed.Name, cap)
    }
}
```

### What Gets Removed

- `CheckCapability` function in `internal/command/access.go`
- `capabilities []string` field on `CommandEntryConfig` (replaced by
  `Capabilities []Capability`)
- All dotted capability strings (`admin.boot`, `comms.say`, etc.)
- All colon capability strings (`admin:shutdown`, `admin:password.reset`)
- The `error_test.go` tests that reference old capability format
- ADR 0007 is fully superseded (add note to ADR)

### What Gets Added

- `Capability` struct in `internal/command/types.go`
- `CanPerformAction` method on `AccessPolicyEngine` interface
- `CanPerformAction` implementation in `internal/access/policy/engine.go`
- Capability validation in manifest loader and command registration
- `ErrInsufficientCapability` error type
- Updated seed policies (if needed — existing command-name policies
  should still work for Layer 1)
- ADR update noting ADR 0007 is fully superseded

### Relationship to Existing ABAC

The capability pre-flight does NOT replace or modify any existing ABAC
behavior:

| Check | Can grant? | Can deny? | Knows specific resource? |
| --- | --- | --- | --- |
| Layer 1 (command execution) | Yes | Yes | Yes (`command:<name>`) |
| Layer 2 (capability pre-flight) | No | Yes | No (type-level only) |
| Service handler ABAC | Yes | Yes | Yes (instance-level) |

Layer 2 is strictly additive denial. It can prevent a command from
running but cannot grant access to resources that service-level ABAC
would deny. Property ownership, exit locks, zone restrictions, and all
instance-level policies remain authoritative.

## Testing

### Unit Tests

- `Capability` struct validation: valid/invalid action, resource, scope
- `CanPerformAction`: subject with admin role, subject with no roles,
  subject with forbid policy, no matching policies
- Dispatcher: command with no capabilities passes Layer 1 only,
  command with capabilities checks both layers, capability failure
  prevents handler execution
- Manifest loading: invalid capability format rejects plugin
- Command registration: invalid capability format panics (core commands)

### Integration Tests

- Full flow: register command with capabilities → dispatch as player
  with correct role → handler executes
- Full flow: dispatch as player without correct role → Layer 1 rejects
- Full flow: dispatch as player with role but missing capability type →
  Layer 2 rejects
- Plugin command: register via YAML with capabilities → dispatch →
  verify both layers checked
- Malicious plugin: command declares no capabilities but handler calls
  privileged host function → service-level ABAC blocks

## Migration

1. Add `Capability` struct to `internal/command/types.go`
2. Add `CanPerformAction` to `AccessPolicyEngine` interface
3. Implement `CanPerformAction` in policy engine
4. Update `CommandEntryConfig` to use `[]Capability` instead of
   `[]string`
5. Update dispatcher to use two-layer authorization
6. Update all core command registrations with structured capabilities
7. Update all plugin YAML manifests with structured capabilities
8. Update all test files with new capability format
9. Remove `CheckCapability` function and old capability code
10. Add validation to manifest loader
11. Update ADR 0007 with supersession notice
12. Update documentation (access-control reference, command security)

## Open Questions

None — all design decisions resolved through discussion.
