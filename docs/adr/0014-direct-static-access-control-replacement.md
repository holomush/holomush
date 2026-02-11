# ADR 0014: Direct StaticAccessControl Replacement

**Date:** 2026-02-05
**Status:** Accepted
**Deciders:** HoloMUSH Contributors
**Supersedes:** [ADR 0007: Command Security Model](0007-command-security-model.md)

## Context

HoloMUSH's current authorization system consists of two components:

1. **`StaticAccessControl`** (Epic 3): Role-based permission checking with glob patterns,
   `$self`/`$here` token resolution, and three static roles (player, builder, admin)
2. **`capability.Enforcer`**: Plugin permission checking using manifest-declared capabilities

These are used at ~30 production call sites across the codebase (plus 6 test mocks),
primarily in `internal/world/service.go` and `internal/command/dispatcher.go`. All
call sites use the `AccessControl.Check(ctx, subject, action, resource) bool` interface.

The ABAC system introduces `AccessPolicyEngine.Evaluate(ctx, AccessRequest) (Decision,
error)` with a richer return type (allowed/denied, effect, reason, matched policies,
attribute snapshot). The question is how to migrate existing call sites.

[ADR 0007](0007-command-security-model.md) established a capability-based command security
model with dot-notation namespaces (`build.dig`, `comms.say`), wildcard matching
(`world.*`), and a planned `character_capabilities` table. The ABAC system replaces this
model entirely — commands become `resource is command` with DSL policy conditions.

### Options Considered

**Option A: Backward-compatibility adapter**

Introduce an adapter that wraps `AccessPolicyEngine` behind the existing `AccessControl`
interface. Existing call sites continue using `Check()` unchanged. The adapter normalizes
subjects and resources, translates the `Decision` to a boolean, and logs discrepancies.

| Aspect     | Assessment                                                                                                                                                                                  |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Strengths  | Zero call site changes initially; incremental migration possible                                                                                                                            |
| Weaknesses | Adapter must normalize `char:` ↔ `character:` prefixes, translate resources, handle `Decision` → `bool` lossy conversion; adds an indirection layer; temporary code with no long-term value |

**Option B: Adapter + shadow mode**

Same as Option A, plus run both the old and new systems in parallel, comparing results
to validate equivalence before cutover.

| Aspect     | Assessment                                                                                                                                                                                                                                                                               |
| ---------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Strengths  | High confidence in behavioral equivalence before migration                                                                                                                                                                                                                               |
| Weaknesses | All Option A weaknesses, plus: the static system has known gaps (`$here` patterns that never match, missing `delete:location` for builders, legacy `@`-prefixed command names); exclusion filtering for known differences is itself bug-prone; ~1,000+ lines of temporary infrastructure |

**Option C: Direct replacement**

Replace `AccessControl` with `AccessPolicyEngine` directly. All call sites switch to
`Evaluate()` in a single phase. The `AccessControl` interface, `StaticAccessControl`,
and `capability.Enforcer` are deleted.

| Aspect     | Assessment                                                                                                          |
| ---------- | ------------------------------------------------------------------------------------------------------------------- |
| Strengths  | Clean cutover; no temporary adapter code; callers get rich `Decision` type; no subject/resource normalization layer |
| Weaknesses | All ~30 production call sites must add error handling for `(Decision, error)` return; large single-phase change     |

## Decision

**Option C: Direct replacement.** No backward-compatibility adapter. No shadow mode.

All ~30 production call sites (plus test mocks) update from `AccessControl.Check()` to
`AccessPolicyEngine.Evaluate()` in phase 7.3 of the implementation. The `AccessControl`
interface, `StaticAccessControl`, and `capability.Enforcer` are deleted in phase 7.6.

### What This Supersedes

**ADR 0007 (Command Security Model)** established the following patterns, all of which are
replaced by the ABAC policy system:

| ADR 0007 Pattern                              | ABAC Replacement                                                                     |
| --------------------------------------------- | ------------------------------------------------------------------------------------ |
| Capability namespaces (`build.dig`)           | `resource is command` with `resource.name == "dig"`                                  |
| Wildcard matching (`world.*`)                 | Policy conditions with `resource.name like "..."` or role-based permits              |
| `character_capabilities` table                | `access_policies` table with seed policies                                           |
| Default grants (all characters get `world.*`) | Seed policies: `permit(...) when { resource.name in ["say", "pose", "look", "go"] }` |
| `@grant`/`@revoke` commands                   | `policy create`/`policy delete` commands                                             |
| `capability.Enforcer` for plugins             | Plugin subjects evaluated by same policy engine                                      |

### Subject Prefix Normalization

The static system uses `char:` as the subject prefix for characters. The ABAC system
normalizes to `character:` everywhere. All call sites MUST use `character:` (the access
package defines prefix constants). The `char:` prefix is not supported by the new engine.

### Implementation Phases

1. **Phase 7.3:** Replace `AccessControl` with `AccessPolicyEngine` in dependency injection.
   Update all call sites to use `Evaluate()` directly
2. **Phase 7.6:** Delete `StaticAccessControl`, `AccessControl` interface,
   `capability.Enforcer`, and all related code. Remove legacy `char:` prefix handling

### Call Site Inventory

| Package                                     | Call Sites | Change Required                         |
| ------------------------------------------- | ---------- | --------------------------------------- |
| `internal/world/service.go`                 | 24         | `Check()` → `Evaluate()` + error handle |
| `internal/command/dispatcher.go`            | 1          | Capability loop → single `Evaluate()`   |
| `internal/command/rate_limit_middleware.go` | 1          | `Check()` → `Evaluate()` + error handle |
| `internal/command/handlers/boot.go`         | 1          | `Check()` → `Evaluate()` + error handle |
| `internal/plugin/hostfunc/commands.go`      | 1          | `Check()` → `Evaluate()` + error handle |
| `internal/plugin/hostfunc/functions.go`     | 1          | `Check()` → `Evaluate()` + error handle |
| `internal/access/static.go`                 | 1          | `Check()` → `Evaluate()` + error handle |
| `internal/core/broadcaster` (test only)     | 6          | Update mock injection (no migration)    |

## Rationale

**No production releases:** HoloMUSH has no deployed users. The static access control
system has never served real traffic. Building adapter and shadow mode infrastructure for
a system with zero consumers is wasted effort.

**Static system has known gaps:** The static system has dead permissions (`$here` patterns
that never match actual call site resource strings), missing capabilities (no
`delete:location` for builders), and legacy `@`-prefixed command names. Seed policies
intentionally fix these gaps — they define the correct permission model, not a replica.
Shadow mode would flag these intentional improvements as disagreements.

**Rich return type:** The `Decision` type provides `Effect`, `Reason`, `PolicyID`, matched
policy list, and attribute snapshot. Collapsing this to a boolean via an adapter discards
information that callers can use for error messages, audit trails, and debugging.

**Adapter complexity:** Normalization between `char:` and `character:` prefixes, resource
format translation, and `Decision` → `bool` conversion would add ~500 lines of temporary
code with no long-term value.

## Consequences

**Positive:**

- Clean, permanent codebase — no adapter layer to eventually remove
- Callers get rich `Decision` type with matched policies and attribute snapshots
- No subject/resource normalization layer
- Known static system gaps are fixed by seed policies, not replicated
- Eliminates the entire `capability.Enforcer` subsystem for plugin permissions

**Negative:**

- All ~30 production call sites must be updated in a single phase (large but mechanical change)
- Call sites must handle `(Decision, error)` return instead of simple `bool`
- No gradual rollout — all authorization switches at once

**Neutral:**

- Error handling at call sites follows the existing `oops.Code()` pattern
- Test helpers (`AllowAll`, `DenyAll`, `MockAccessControl`) need ABAC equivalents
- Integration tests validate seed policies provide equivalent or expanded access
- Each seed policy requires integration tests: allowed, denied, edge case (100% coverage before Phase 7.3 cutover)

## References

- [Full ABAC Architecture Design — Replacing Static Roles](../specs/2026-02-05-full-abac-design.md)
- [Design Decision #36: Direct Replacement](../specs/decisions/epic7/phase-7.6/036-direct-replacement-no-adapter.md)
- [Design Decision #37: No Shadow Mode](../specs/decisions/epic7/phase-7.6/037-no-shadow-mode.md)
- [ADR 0007: Command Security Model](0007-command-security-model.md) (superseded)
