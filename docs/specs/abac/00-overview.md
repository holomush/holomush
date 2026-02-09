<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Full ABAC Architecture Design

**Status:** Draft
**Date:** 2026-02-05
**Epic:** holomush-5k1 (Epic 7: Full ABAC)
**Task:** holomush-5k1.1

## Overview

This document defines the full Attribute-Based Access Control (ABAC) architecture
for HoloMUSH, replacing the static role-based system from Epic 3 with a
policy-driven engine. Game administrators define policies using a Cedar-inspired
DSL that references dynamic attributes of subjects, resources, and the environment.
Players control access to their own properties through a simplified lock system.

### Goals

- Dynamic, admin-editable authorization policies stored in PostgreSQL
- Cedar-inspired DSL with rich expression language (comparisons, set operations,
  hierarchy traversal, if-then-else)
- Extensible attribute system with plugin contributions via registration-based
  providers
- Properties as first-class entities with per-property access control
- Player-authored locks for owned resources (simplified policy syntax)
- In-game admin commands for policy CRUD and debugging (`policy` command set)
- Configurable audit logging with mode control (off, denials-only, all)
- Direct replacement of `AccessControl` with `AccessPolicyEngine` across ~30
  call sites (greenfield deployment — no backward-compatibility adapter)
- Default-deny posture with deny-overrides conflict resolution

### Non-Goals

- Graph-based relationship traversal (OpenFGA/Zanzibar-style) — relationships
  are modeled as attributes
- Priority-based policy ordering — deny always wins, no escalation
- Real-time policy synchronization across multiple server instances
  (single-server for now)
- Web-based policy editor (admin commands cover MVP, web UI deferred)
- Database triggers or stored procedures — all logic lives in Go

### Glossary

| Term            | Definition                                                                                                                                                                                  |
| --------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Subject**     | The entity in `AccessRequest.Subject` — the Go-side identity string (e.g., `"character:01ABC"`)                                                                                             |
| **Principal**   | The DSL keyword referring to the subject — `principal is character` matches subjects with `character:` prefix                                                                               |
| **Resource**    | The target of the access request — entity string in `AccessRequest.Resource`                                                                                                                |
| **Action**      | The operation being performed — string in `AccessRequest.Action` (e.g., `"read"`, `"execute"`)                                                                                              |
| **Environment** | Server-wide context attributes (time, maintenance mode) — the `env` prefix in DSL                                                                                                           |
| **Policy**      | A permit or forbid rule with target matching and conditions, stored in `access_policies`                                                                                                    |
| **Seed policy** | A system-installed default policy (prefixed `seed:`) created at first startup that defines baseline access (movement, self-access, builder/admin privileges) using the ABAC policy language |
| **Lock**        | A player-authored simplified policy using token syntax, compiled to a scoped `lock:` policy                                                                                                 |
| **Decision**    | The outcome of `Evaluate()` — includes effect, reason, matched policies, and attribute snapshot                                                                                             |

### Key Design Decisions

| Decision              | Choice                                  | Rationale                                                         |
| --------------------- | --------------------------------------- | ----------------------------------------------------------------- |
| Engine                | Custom Go-native ABAC                   | Full control, no impedance mismatch, tight plugin integration     |
| Policy language       | Cedar-inspired DSL                      | Readable by game admins, expressive, well-documented formal model |
| Attribute resolution  | Eager (collect-then-evaluate)           | Simple, predictable, better audit story                           |
| Conflict resolution   | Deny-overrides, no priority             | Simple mental model, Cedar-proven approach                        |
| Property model        | First-class entities                    | Conceptual uniformity — everything is an entity                   |
| Plugin attributes     | Registration-based providers            | Synchronous, consistent with eager resolution                     |
| Audit logging         | Separate PostgreSQL table               | Clean separation from game events, independent retention          |
| Migration             | Direct replacement (no adapter)         | Greenfield — no releases, clean cutover via seed policies         |
| Cache invalidation    | PostgreSQL LISTEN/NOTIFY (in Go code)   | Push-based, no polling overhead                                   |
| Player access control | Layered: metadata + locks + full policy | Progressive complexity for different user roles                   |

**Seed policies** are the default permission policies installed automatically
at first server startup, replacing the static role definitions from Epic 3.
They define baseline access (movement, self-access, builder/admin privileges)
using the ABAC policy language. See [Seed Policies](07-migration-seeds.md#seed-policies) for the
full set.

### Reserved Prefixes

All string identifiers in the ABAC system use reserved prefixes for validation and routing:

| Category         | Prefix       | Purpose                                | Example                       |
| ---------------- | ------------ | -------------------------------------- | ----------------------------- |
| Subject Strings  | `character:` | Character entity reference             | `character:01ABC`             |
|                  | `plugin:`    | Plugin identifier                      | `plugin:echo-bot`             |
|                  | `system`     | System bypass (no ID, immediate allow) | `system`                      |
|                  | `session:`   | Session ID (resolved to `character:`)  | `session:01XYZ`               |
| Resource Strings | `location:`  | Location/room reference                | `location:01XYZ`              |
|                  | `object:`    | Object entity reference                | `object:01DEF`                |
|                  | `exit:`      | Exit entity reference                  | `exit:01MNO`                  |
|                  | `scene:`     | Scene entity reference                 | `scene:01PQR`                 |
|                  | `character:` | Character as resource (dual-use)       | `character:01ABC`             |
|                  | `command:`   | Command name                           | `command:say`                 |
|                  | `property:`  | Property entity reference              | `property:01GHI`              |
|                  | `stream:`    | Event stream reference                 | `stream:location:01XYZ`       |
| Policy Names     | `seed:`      | System seed policies                   | `seed:player-self-access`     |
|                  | `lock:`      | Lock-generated policies                | `lock:object:01ABC:read`      |
| Policy IDs       | `infra:`     | Infrastructure error disambiguation    | `infra:attr-resolution-error` |

**Dual-use prefix:** `character:` appears in both Subject Strings and Resource Strings because characters can be both the subject performing an action (e.g., `character:01ABC` reading a location) and the resource being acted upon (e.g., another character reading `character:01ABC`'s attributes). The engine distinguishes these roles by the field in `AccessRequest`: `Subject` vs. `Resource`. Existing world providers resolve character attributes in both subject and resource contexts.

See [AccessRequest](01-core-types.md#accessrequest) for subject/resource format details and [Seed Policies](07-migration-seeds.md#seed-policies) for policy name conventions.

## Architecture

```text
┌──────────────────────────────────────────────────────────────────────┐
│                        AccessPolicyEngine                            │
│                                                                      │
│   Evaluate(ctx, AccessRequest) (Decision, error)                    │
│                                                                      │
├──────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   ┌─────────────────┐  ┌──────────────────┐  ┌─────────────────┐   │
│   │ Policy Store     │  │ Attribute        │  │ Audit Logger    │   │
│   │ (PostgreSQL)     │  │ Resolver         │  │ (PostgreSQL)    │   │
│   │                  │  │                  │  │                 │   │
│   │ - CRUD policies  │  │ - Core providers │  │ - Log denials   │   │
│   │ - Version history│  │ - Plugin provs   │  │ - Optional      │   │
│   │ - DSL text +     │  │ - Environment    │  │   allow logging │   │
│   │   compiled form  │  │                  │  │ - Attr snapshot │   │
│   └────────┬────────┘  └──────┬───────────┘  └─────────────────┘   │
│            │                  │                                      │
│   ┌────────┴────────┐        │                                      │
│   │ Policy Compiler  │        │                                      │
│   │ - Parse DSL → AST│        │                                      │
│   │ - Validate attrs │        │                                      │
│   │ - Compile globs  │        │                                      │
│   │ - Store compiled │        │                                      │
│   │   form (JSONB)   │        │                                      │
│   └─────────────────┘        │                                      │
│                    ┌──────────┴───────────┐                          │
│                    │  Attribute Providers  │                          │
│                    ├──────────────────────┤                          │
│                    │ CharacterProvider     │ ← World model            │
│                    │ LocationProvider      │ ← World model            │
│                    │ PropertyProvider      │ ← Property store         │
│                    │ ObjectProvider        │ ← World model            │
│                    │ StreamProvider        │ ← Derived from stream ID │
│                    │ CommandProvider       │ ← Command registry       │
│                    │ ExitProvider (stub)   │ ← World model (type/id)  │
│                    │ SceneProvider (stub)  │ ← World model (type/id)  │
│                    │ Session Resolver      │ ← Session store (not a   │
│                    │                       │   provider; see Session  │
│                    │                       │   Subject Resolution)    │
│                    │ EnvironmentProvider   │ ← Clock, game state      │
│                    │ PluginProvider(s)     │ ← Registered by plugins  │
│                    └──────────────────────┘                          │
│                                                                      │
└──────────────────────────────────────────────────────────────────────┘
```

### Request Flow

This section provides a high-level overview. See [Evaluation Algorithm](04-resolution-evaluation.md#evaluation-algorithm) for the authoritative step-by-step specification.

1. Caller invokes `Evaluate(ctx, AccessRequest)`
2. System bypass: if subject is `"system"`, immediately allow with
   `SystemBypass` effect
3. Session resolution: if subject is `"session:..."`, resolve to character ID
   before attribute resolution
4. Engine resolves all attributes eagerly — calls core providers + registered
   plugin providers
5. Engine loads matching policies from the in-memory cache
6. Engine evaluates each policy's conditions against the attribute bags
7. Deny-overrides: any forbid → deny (wins over permits); else any permit →
   allow; else default deny
8. Audit logger records the decision, matched policies, and attribute snapshot
9. Returns `Decision` with allowed/denied, reason, and matched policy ID

### Package Structure

```text
internal/access/               # Existing — AccessPolicyEngine replaces AccessControl
internal/access/policy/        # NEW — AccessPolicyEngine, evaluation
  engine.go                    # AccessPolicyEngine implementation
  policy.go                    # Policy type, parsing, validation
  compiler.go                  # PolicyCompiler: parse, validate, compile
  dsl/                         # DSL parser and AST
    parser.go
    ast.go
    evaluator.go
  attribute/                   # Attribute resolution
    resolver.go                # AttributeResolver orchestrates providers
    provider.go                # AttributeProvider interface
    character.go               # Core: character attributes
    location.go                # Core: location attributes
    object.go                  # Core: object attributes
    property.go                # Core: property attributes
    stream.go                  # Core: StreamProvider — stream attributes (derived from ID)
    command.go                 # Core: CommandProvider — command attributes
    exit.go                    # Stub: ExitProvider — type/id only (TODO: full attrs, see holomush-5k1.422)
    scene.go                   # Stub: SceneProvider — type/id only (TODO: full attrs, see holomush-5k1.424)
    environment.go             # Core: env attributes (time, game state)
  lock/                        # Lock expression system
    parser.go                  # Lock expression parser
    compiler.go                # Compiles lock AST to DSL policy text
    registry.go                # LockTokenRegistry
  store/                       # Policy persistence
    postgres.go                # Policy CRUD + versioning + LISTEN/NOTIFY
  audit/                       # Audit logging
    logger.go                  # Audit decision logger
    postgres.go                # Audit table writes
```
