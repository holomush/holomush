# Full ABAC Design Decisions

**Date:** 2026-02-05
**Participants:** Sean (lead), Claude (design assistant)
**Related:** [Full ABAC Architecture Design](2026-02-05-full-abac-design.md)

This document records the design interview that produced the Full ABAC
architecture. Each section captures the question, options considered, and
rationale for the chosen approach.

---

## Table of Contents

1. [Policy Engine Approach](#1-policy-engine-approach)
2. [Policy Definition Format](#2-policy-definition-format)
3. [Attribute Resolution Strategy](#3-attribute-resolution-strategy)
4. [Conflict Resolution](#4-conflict-resolution)
5. ~~[Migration Strategy](#5-migration-strategy)~~ (superseded by #36)
6. [Plugin Attribute Contributions](#6-plugin-attribute-contributions)
7. [Audit Logging Destination](#7-audit-logging-destination)
8. [DSL Expression Language Scope](#8-dsl-expression-language-scope)
9. [Property Model](#9-property-model)
10. [Property Visibility Defaults](#10-property-visibility-defaults)
11. [Cache Invalidation](#11-cache-invalidation)
12. [Player Access Control Layers](#12-player-access-control-layers)
13. [Subject Prefix Normalization](#13-subject-prefix-normalization)
14. [No Database Triggers](#14-no-database-triggers)
15. [Grammar: `in` Operator Extended to Attribute Expressions](#15-grammar-in-operator-extended-to-attribute-expressions)
16. [Entity References Explicitly Deferred](#16-entity-references-explicitly-deferred)
17. [Session Resolution at Engine Entry Point](#17-session-resolution-at-engine-entry-point)
18. [Property Package Ownership](#18-property-package-ownership)
19. [Lock Policies Are Not Versioned](#19-lock-policies-are-not-versioned)
20. ~~[`enter` Action as New ABAC-Only Path](#20-enter-action-as-new-abac-only-path)~~ (superseded by #37)
21. ~~[Shadow Mode Cutover Criteria](#21-shadow-mode-cutover-criteria)~~ (superseded by #37)
22. [Flat Prefixed Strings Over Typed Structs](#22-flat-prefixed-strings-over-typed-structs)
23. [Performance Targets](#23-performance-targets)
24. [Bootstrap Sequence](#24-bootstrap-sequence)
25. [Intentional Builder Permission Expansion](#25-intentional-builder-permission-expansion)
26. [Per-Request Attribute Caching](#26-per-request-attribute-caching)
27. [Unified `AttributeProvider` Interface](#27-unified-attributeprovider-interface)
28. [Cedar-Aligned Missing Attribute Semantics](#28-cedar-aligned-missing-attribute-semantics)
29. [DSL `like` Pattern Validation at Parser Layer](#29-dsl-like-pattern-validation-at-parser-layer)
30. [PolicyCompiler Component](#30-policycompiler-component)
31. [Provider Re-Entrance Prohibition](#31-provider-re-entrance-prohibition)
32. [PropertyProvider Uses SQL JOIN for Parent Location](#32-propertyprovider-uses-sql-join-for-parent-location)
33. [Plugin Lock Tokens MUST Be Namespaced](#33-plugin-lock-tokens-must-be-namespaced)
34. [Time-of-Day Attributes for Environment Provider](#34-time-of-day-attributes-for-environment-provider)
35. [Audit Log Source Column and No Decision Column](#35-audit-log-source-column-and-no-decision-column)
36. [Direct Replacement (No Adapter)](#36-direct-replacement-no-adapter)
37. [No Shadow Mode](#37-no-shadow-mode)
38. [Audit Log Configuration Modes](#38-audit-log-configuration-modes)
39. [`EffectSystemBypass` as Fourth Effect Variant](#39-effectsystembypass-as-fourth-effect-variant)
40. [`has` Operator Supports Dotted Attribute Paths](#40-has-operator-supports-dotted-attribute-paths)
41. [LL(1) Parser Disambiguation for Condition Grammar](#41-ll1-parser-disambiguation-for-condition-grammar)
42. [Sequential Provider Resolution](#42-sequential-provider-resolution)
43. [Property Lifecycle: Go-Level CASCADE Cleanup](#43-property-lifecycle-go-level-cascade-cleanup)
44. [Nested Container Resolution via Recursive CTE](#44-nested-container-resolution-via-recursive-cte)
45. [Bounded List Sizes for `visible_to` / `excluded_from`](#45-bounded-list-sizes-for-visible_to--excluded_from)
46. [`policy validate` and `policy reload` Commands](#46-policy-validate-and-policy-reload-commands)
47. [Fuzz Testing for DSL Parser](#47-fuzz-testing-for-dsl-parser)
48. [Deterministic Seed Policy Names](#48-deterministic-seed-policy-names)
49. [Revised Audit Volume Estimate](#49-revised-audit-volume-estimate)
50. [Plugin Attribute Collision Behavior](#50-plugin-attribute-collision-behavior)
51. [Session Integrity Error Classification](#51-session-integrity-error-classification)
52. [Async Audit Writes](#52-async-audit-writes)
53. [Audit WAL Best-Effort Semantics](#53-audit-wal-best-effort-semantics)
54. [Property Location Resolution Eventual Consistency](#54-property-location-resolution-eventual-consistency)
55. [Session Error Code Simplification](#55-session-error-code-simplification)
56. [Audit Off Mode Includes System Bypasses](#56-audit-off-mode-includes-system-bypasses)
57. [ADR Format Evolution](#57-adr-format-evolution)
58. [Provider Re-Entrance Goroutine Prohibition](#58-provider-re-entrance-goroutine-prohibition)
59. [Fair-Share Provider Timeout Scheduling](#59-fair-share-provider-timeout-scheduling)
60. [Restricted Visibility via Seed Policies Only](#60-restricted-visibility-via-seed-policies-only)
61. [Lock Policy Naming Includes Resource Type](#61-lock-policy-naming-includes-resource-type)
62. [Separate PolicyEffect Type for Stored Policies](#62-separate-policyeffect-type-for-stored-policies)
63. [Remove Session Circuit Breaker Auto-Invalidation](#63-remove-session-circuit-breaker-auto-invalidation)
64. [Remove Admin Bypass of Cache Staleness and Degraded Mode](#64-remove-admin-bypass-of-cache-staleness-and-degraded-mode)

---

## 1. Policy Engine Approach

**Question:** Should HoloMUSH adopt an existing authorization framework or build
a custom engine?

**Options considered:**

| Option | Description                            | Pros                                                                                   | Cons                                                                                                           |
| ------ | -------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| A      | Embed OpenFGA + custom attribute layer | Mature engine, Go-native, PostgreSQL backend, good for relationship graphs             | ReBAC-first model is awkward for attribute comparisons; limited condition language; heavyweight for ~200 users |
| B      | Custom ABAC engine, Cedar-inspired DSL | Full control, tight plugin integration, no impedance mismatch, readable by game admins | More upfront work to build and maintain                                                                        |

**Also evaluated:** Cedar (Rust-only, would need CGO/sidecar), OPA/Rego
(Turing-complete but hard for game admins to read/write).

**Decision:** **Option B — Custom ABAC engine.**

**Rationale:** HoloMUSH's domain is heavily attribute-driven (faction checks,
level gates, property visibility), not relationship-driven. OpenFGA's strength
is graph traversal (org hierarchies, document sharing), which isn't the primary
pattern here. At ~200 concurrent users, we don't need Zanzibar-scale
infrastructure. A custom engine gives full control over the DSL, tight
integration with the plugin system, and a policy language readable by non-engineer
game admins.

**Key insight:** Relationships can be modeled as attributes that get resolved at
evaluation time. The existing `LocationResolver` performs token replacement for
`$here` in glob patterns — a form of dynamic context resolution. The ABAC
engine's `AttributeProvider` generalizes this concept: instead of replacing
tokens in strings, providers resolve full attribute bags for any entity.

---

## 2. Policy Definition Format

**Question:** How should policies be defined and stored?

**Options considered:**

| Option | Description                                | Pros                                                         | Cons                                       |
| ------ | ------------------------------------------ | ------------------------------------------------------------ | ------------------------------------------ |
| 1      | Declarative YAML/JSON with template syntax | Familiar format, easy to store                               | Template syntax (`{{...}}`) gets confusing |
| 2      | Cedar-style policy DSL                     | Reads like English, expressive, well-documented formal model | Requires a parser                          |
| 3      | Structured conditions (pure JSON data)     | Easiest to validate, easy for admin commands to construct    | Verbose, hard to read at a glance          |

**Decision:** **Option 2 — Cedar-style policy DSL.**

**Rationale:** Readable by game admins, expressive enough for complex conditions,
and we can store the text in PostgreSQL while keeping a parsed/validated form.
The parser is straightforward since we're building conditions with attribute
references, not a full programming language. Cedar's formal model is
well-documented to draw from without importing the Rust dependency.

---

## 3. Attribute Resolution Strategy

**Question:** When should attributes be resolved — all up front, or on demand?

**Options considered:**

| Option | Description                   | Pros                                                      | Cons                                         |
| ------ | ----------------------------- | --------------------------------------------------------- | -------------------------------------------- |
| A      | Eager (collect-then-evaluate) | Simple, predictable, complete audit snapshot per decision | May fetch unused attributes                  |
| B      | Lazy (resolve-on-reference)   | More efficient, only fetches what policies need           | Harder to audit, ordering/caching complexity |

**Decision:** **Option A — Eager resolution.**

**Rationale:** At ~200 users and modest policy counts, the performance difference
is negligible. Eager resolution provides a complete attribute snapshot for every
decision, which powers both audit logging and the `policy test` debugging
command. The implementation is simpler and the mental model is clearer: every
check starts with "here's everything we know about the subject and resource."

---

## 4. Conflict Resolution

**Question:** How should conflicting policies be resolved? Should policies have
priority ordering?

**Roadmap predefined:** Deny-overrides (any deny wins, then any allow, then
default deny).

**Additional question:** Should policies have numeric priority for override
scenarios?

**Options considered:**

| Option | Description                     | Pros                                  | Cons                                       |
| ------ | ------------------------------- | ------------------------------------- | ------------------------------------------ |
| A      | Deny-overrides without priority | Simple mental model, Cedar-proven     | Can't say "this allow overrides that deny" |
| B      | Priority-based ordering         | More flexible, supports VIP overrides | "Why was I denied?" debugging nightmares   |

**Decision:** **Deny-overrides without priority, with system subject bypass.**

**Rationale:** Cedar chose no priority and it works well. If an admin needs an
override, they write a more specific `allow` that avoids triggering the `deny`
condition, rather than using priority escalation. The `system` subject bypass
(already existing) handles the "ultimate override" case. Keeps the mental model
simple: deny always wins, period.

---

## 5. Migration Strategy

**Question:** How do we migrate ~30 production call sites from the old
`AccessControl` interface to the new `AccessPolicyEngine`?

**Options considered:**

| Option | Description                                 | Pros                                                  | Cons                                                                  |
| ------ | ------------------------------------------- | ----------------------------------------------------- | --------------------------------------------------------------------- |
| A      | Big-bang interface change                   | Clean, one-time effort                                | Large blast radius, all ~30 callers need error handling added at once |
| B      | New interface + adapter for backward compat | Incremental migration, preserves fail-closed behavior | Two interfaces exist temporarily                                      |

**Decision:** ~~**Option B — New `AccessPolicyEngine` interface with adapter.**~~

**Superseded by [decision #36](#36-direct-replacement-no-adapter).** With no
production releases, the adapter adds unnecessary complexity. All call sites
switch to `AccessPolicyEngine.Evaluate()` directly.

**Naming:** The new interface is called `AccessPolicyEngine` (per Sean's
preference).

---

## 6. Plugin Attribute Contributions

**Question:** How do plugins contribute attributes to the evaluation context?

**Options considered:**

| Option | Description                      | Pros                                                            | Cons                                  |
| ------ | -------------------------------- | --------------------------------------------------------------- | ------------------------------------- |
| A      | Registration-based providers     | Simple interface, synchronous, consistent with eager resolution | Every check calls every provider      |
| B      | Attribute hooks via event system | Lower latency per check, async                                  | Attributes can be stale, more complex |

**Decision:** **Option A — Registration-based providers.**

**Rationale:** Consistent with the eager resolution choice. Plugins implement a
simple `AttributeProvider` interface with `Namespace()` to prevent collisions.
At ~200 users the synchronous resolution cost is trivial. Caching can be added
later if profiling shows it's needed.

---

## 7. Audit Logging Destination

**Question:** Where should access decisions be logged?

**Options considered:**

| Option | Description                    | Pros                                                                | Cons                                                        |
| ------ | ------------------------------ | ------------------------------------------------------------------- | ----------------------------------------------------------- |
| A      | Event store                    | Queryable, replayable, consistent with architecture                 | Enormous volume, mixes auth noise into game streams         |
| B      | Separate PostgreSQL table      | Clean separation, independent retention policy, easy to query/purge | Additional table to manage                                  |
| C      | Structured logging only (slog) | Cheapest, no DB overhead                                            | Harder to query historically, depends on log infrastructure |

**Decision:** **Option B — Separate audit table.**

**Rationale:** Keeps the game event store clean while giving admins a queryable
audit trail. Configurable verbosity: log all denials by default, allow-decisions
only when audit mode is enabled. Denials are always interesting; allows are only
interesting when troubleshooting.

---

## 8. DSL Expression Language Scope

**Initial proposal:** Stripped-down operators (comparisons, AND/OR/NOT, `in`
lists). Excluded hierarchy traversal, `has`, set operations, and if-then-else.

**Sean's feedback:** Include the full set — hierarchy, `has`, set operations, and
if-then-else.

**Decision:** **Full Cedar-compatible expression language.**

**Rationale:** The healer-wound-visibility scenario demonstrated why the full
language is needed. Without `has`, every property would need every possible
attribute defined. Without `containsAny`/`in`, you'd need a separate policy per
healer character. The full expression language pays for itself in real MUSH
scenarios. Operators: `==`, `!=`, `>`, `>=`, `<`, `<=`, `in` (list and
hierarchy), `has`, `containsAll`, `containsAny`, `if-then-else`, `like`, `&&`,
`||`, `!`.

---

## 9. Property Model

**Question:** How should per-property access control work?

**Options considered:**

| Option | Description                        | Pros                                              | Cons                                                               |
| ------ | ---------------------------------- | ------------------------------------------------- | ------------------------------------------------------------------ |
| A      | Properties as sub-resources        | Minimal world model change, path-based addressing | Two concepts (entities vs sub-resources), implicit type derivation |
| B      | Properties as first-class entities | Conceptual uniformity, one mental model           | More DB rows, properties need own table                            |
| C      | Property flags only (no ABAC)      | Simplest                                          | Loses ability to write rich policies on properties                 |

**Discussion:** Sean initially leaned toward B but questioned whether it was
over-engineering. Analysis of the admin experience showed that option A
introduces a second-class citizen (sub-resources) requiring admins to understand
two different resource models. Option B keeps everything uniform: characters,
locations, objects, and properties are all entities evaluated by the same engine.

**Decision:** **Option B — Properties as first-class entities.**

**Rationale:** Properties already need storage (name, value, parent entity).
Adding `owner`, `visibility`, and `flags` columns is minimal overhead. Admins
learn one model: "everything is an entity with attributes, policies control
access to entities." The healer-wound example and faction-backstory scenarios
both validated this approach.

**Key scenario tested:** A property of a character visible only to a group of
other characters (healers) but NOT to the character it belongs to. This works
cleanly with first-class properties:

```text
permit(principal is character, action in ["read"], resource is property)
when { resource.name == "wounds" && principal.flags.containsAny(["healer"]) };

forbid(principal is character, action in ["read"], resource is property)
when { resource.name == "wounds" && resource.parent_id == principal.id };
```

---

## 10. Property Visibility Defaults

**Question:** Should `visible_to`/`excluded_from` always have defaults?

**Sean's input:** Default to always visible to self and empty excluded list when
set to restricted. Prevents footguns.

**Decision:** When visibility is set to `restricted`, auto-populate
`visible_to = [parent_id]` (self) and `excluded_from = []`. For other visibility
levels, both fields are NULL (not applicable). Logic lives in Go property store,
not database triggers.

---

## 11. Cache Invalidation

**Sean's input:** Use PostgreSQL LISTEN/NOTIFY from the start, not polling.

**Decision:** The Go policy store sends `pg_notify('policy_changed', policyID)`
in the same transaction as any CRUD operation. The engine subscribes and reloads
its in-memory cache on notification. No database triggers — the notification
call is explicit Go code.

---

## 12. Player Access Control Layers

**Question:** Can characters manage policies for things they own?

**Decision:** Three-layer model:

1. **Property metadata** (all characters) — Set visibility, visible_to,
   excluded_from on owned properties. No policy authoring needed.
2. **Object locks** (resource owners) — Simplified lock syntax (`faction:X`,
   `flag:X`, `level:>=N`, `me`, `&`, `|`, `!`) that compiles to scoped policies.
   Ownership verified before creation.
3. **Full policies** (admin only) — Full DSL with unrestricted scope.

Admin `forbid` policies always trump player locks via deny-overrides.

---

## 13. Subject Prefix Normalization

**Question:** The static system uses `char:` as the subject prefix for
characters, but resources already use `character:` (e.g., `character:01ABC`).
Should the ABAC system normalize?

**Decision:** Normalize to `character:` everywhere. ~~The adapter MUST accept
both `char:` and `character:` during migration, normalizing to `character:`
internally.~~ The `access` package SHOULD define prefix constants
(`SubjectCharacter`, `SubjectPlugin`, etc.) to prevent typos.

**Rationale:** Asymmetric prefixes (`char:` for subjects, `character:` for
resources) create confusion in policies and audit logs. Normalizing to
`character:` aligns subjects with resources and with Cedar conventions where
the principal type name matches the DSL type name.

_Note: [decision #36](#36-direct-replacement-no-adapter) removed the adapter. All call sites switch directly to
`character:` — the engine **MUST** reject the `char:` prefix with a clear
error rather than normalizing it. The prefix constants (`SubjectCharacter`,
`SubjectPlugin`, etc.) remain the recommended approach._

---

## 14. No Database Triggers

**Sean's hard constraint:** No database triggers or stored procedures. All logic
must live in Go application code. PostgreSQL is storage only.

**Impact:** Visibility defaults, LISTEN/NOTIFY notifications, and version
history management are all handled in Go store implementations.

---

_The following decisions were captured during the architecture review of PR #65.
They record clarifications and refinements made in response to review findings._

---

## 15. Grammar: `in` Operator Extended to Attribute Expressions

**Review finding:** The DSL grammar defined `expr "in" list` and
`expr "in" entity_ref` but example policies used `principal.id in
resource.visible_to` — an attribute-to-attribute membership check that was
unparseable under the original grammar.

**Decision:** Add `expr "in" expr` to the condition production. The right-hand
side MUST resolve to a `[]string` or `[]any` attribute at evaluation time. This
is distinct from `expr "in" list` where the list is a literal.

**Rationale:** Property access control requires checking character IDs against
`visible_to` and `excluded_from` lists, which are attributes, not literals.
Without this, the healer-wound scenario and all `visible_to`/`excluded_from`
policies would be unimplementable.

---

## 16. Entity References Explicitly Deferred

**Review finding:** The grammar included `entity_ref` (`Type::"value"`) syntax
and the operator table listed `in (entity)`, but the spec simultaneously said
this was "reserved for future." This created a confusing situation where admins
could write policies the parser would accept but the evaluator couldn't execute.

**Decision:** Remove `entity_ref` from the grammar entirely. The parser MUST
reject `Type::"value"` syntax with a clear error message directing admins to use
attribute-based group checks (`principal.flags.containsAny(["admin"])`) instead.
Entity references MAY be added in a future phase when group/hierarchy features
are implemented.

**Rationale:** Including unimplemented syntax in the grammar invites runtime
errors. Better to reject at parse time with a helpful message than to accept
syntax that fails silently at evaluation time.

**Updates [decision #8](#8-dsl-expression-language-scope):** The full expression language still includes all
operators from [decision #8](#8-dsl-expression-language-scope). Only `entity_ref` is deferred — `in` works with
lists and attribute expressions.

---

## 17. Session Resolution at Engine Entry Point

**Review finding:** The spec stated sessions are "resolved to their associated
character" but didn't specify where this happens — in the engine, in a provider,
or at the adapter layer. This ambiguity affects the entire provider architecture.

**Decision:** Session resolution happens at the engine entry point, BEFORE
attribute resolution. The engine rewrites `session:web-123` to
`character:01ABC` by querying the session store, then proceeds as if the caller
passed the character subject directly.

**Rationale:** Policies are always evaluated as `principal is character`, never
`principal is session`. Resolving at the entry point keeps the provider layer
clean — `CharacterProvider` only handles characters, not sessions. The
`Session Resolver` in the architecture diagram exists solely for this lookup,
not as an attribute contributor.

---

## 18. Property Package Ownership

**Review finding:** The `entity_properties` table was introduced but the spec
didn't clarify whether properties live in `internal/world` (alongside locations
and objects) or in `internal/access/policy/store`.

**Decision:** Properties are world model entities managed by
`internal/world/PropertyRepository`, consistent with `LocationRepository` and
`ObjectRepository`. The `entity_properties` table is part of the world schema.
The `PropertyProvider` in `internal/access/policy/attribute/` wraps
`PropertyRepository` to resolve attributes for policy evaluation.

**Rationale:** Properties have parent entities (characters, locations, objects)
and represent game-world data, not authorization metadata. Placing them in the
world package maintains the separation of concerns: world model stores data,
access control evaluates policies against it.

**Updates [decision #9](#9-property-model):** Clarifies the implementation location of the property
model from [decision #9](#9-property-model).

---

## 19. Lock Policies Are Not Versioned

**Review finding:** Lock commands compile to "scoped policies" but the spec
didn't specify what happens on modification — is the old policy versioned,
deleted, or updated in place?

**Decision:** Lock-generated policies are NOT versioned.

- `lock X/action = condition` creates a policy via `PolicyStore.Create()`
  with naming convention `lock:{resource-type}:{resource-id}:{action}`.
- `unlock X/action` deletes the policy via `PolicyStore.DeleteByName()`.
- Modifying a lock deletes the old policy and creates a new one in a single
  transaction.

**Rationale:** Lock versioning would explode the audit log for casual player
actions (setting a lock on a chest shouldn't generate version history). Admins
who need version history use the full `policy` command set. Player locks are
ephemeral by design — they exist for in-game convenience, not governance.

---

## 20. `enter` Action as New ABAC-Only Path

**Review finding:** The seed policies introduce an `enter` action for location
entry control, but the static system handles movement through
`write:character:$self` (changing character location). This semantic gap affects
shadow mode validation.

**Decision:** ~~The `enter` action is a new capability introduced by the ABAC
system with no static-system equivalent. Shadow mode validation MUST exclude
`enter` actions when comparing engine results against `StaticAccessControl`.~~

**Superseded by [decision #37](#37-no-shadow-mode).** Shadow mode was removed.
The `enter` action remains a new ABAC capability with no static-system
equivalent, but no shadow mode validation is performed.

**Rationale:** The static system conflates "move yourself" with "enter a
location" under the `write:character` permission. ABAC separates these concerns
so admins can write fine-grained location entry policies (faction gates, level
requirements) independent of character modification permissions.

---

## 21. Shadow Mode Cutover Criteria

**Superseded by [decision #37](#37-no-shadow-mode).** Shadow mode is removed
entirely — there are no production releases to validate against.

~~**Original decision:** Objective cutover criteria (7 days, 10K checks, 100%
agreement). No longer applicable.~~

---

## 22. Flat Prefixed Strings Over Typed Structs

**Review finding:** `AccessRequest` uses flat strings (`Subject:
"character:01ABC"`) parsed at evaluation time, which is inconsistent with the
world model's typed structs (`Location.ID ulid.ULID`).

**Decision:** Keep flat prefixed strings for `AccessRequest`.

**Rationale:** Flat strings simplify serialization for audit logging (no
marshaling needed) and the `policy test` admin command (admins type
`character:01ABC` directly — no struct construction required). The format is
consistent with external API boundaries (telnet/web protocols already exchange
string identifiers). If profiling shows parsing as a bottleneck, introduce a
cached parsed representation without changing the public API.

---

## 23. Performance Targets

**Review finding:** The spec had no stated performance requirements, making it
impossible to detect regressions or know when optimization is needed.

**Decision:** Define performance targets for the policy engine:

| Metric                   | Target |
| ------------------------ | ------ |
| `Evaluate()` p99 latency | <5ms   |
| Attribute resolution     | <2ms   |
| DSL condition evaluation | <1ms   |
| Cache reload             | <50ms  |

All targets assume 200 concurrent users. Implementation SHOULD add
`slog.Debug()` timers for profiling.

**Rationale:** Concrete targets enable CI-based performance regression detection
and give implementers a clear "good enough" threshold. The 5ms target leaves
headroom for the full request path while keeping authorization invisible to
players.

---

## 24. Bootstrap Sequence

**Review finding:** The spec defined seed policies but didn't specify how they
are created. With default-deny and no policies, the first admin would be locked
out (chicken-and-egg problem).

**Decision:** Server startup detects an empty `access_policies` table and seeds
policies as the `system` subject, which bypasses policy evaluation entirely.
Seed policies use deterministic names (`seed:player-self-access`, etc.) for
idempotency.

**Rationale:** The `system` subject bypass already exists in the evaluation
algorithm (step 1). Seeding at startup is consistent with how the static system
initializes default roles. Deterministic naming prevents duplicate seeds on
restart.

**Updates [decision #5](#5-migration-strategy):** Adds bootstrap mechanism to
the migration strategy.

---

## 25. Intentional Builder Permission Expansion

**Review finding:** Seed policies grant builders `delete` on locations, but the
static system only grants `write:location:*` — no `delete:location:*`. This
would cause shadow mode disagreements.

**Decision:** Preserve the expansion as intentional. Builders who can create and
modify locations SHOULD also be able to delete them. The static system's
omission was a gap, not a deliberate restriction.

**Rationale:** Builder workflow requires the ability to clean up test locations.
Without `delete`, builders must ask an admin to remove locations, which is an
unnecessary bottleneck.

_Note: Shadow mode was removed by [decision #37](#37-no-shadow-mode). The
original rationale about shadow mode exclusions no longer applies, but the
permission expansion itself is intentional._

---

## 26. Per-Request Attribute Caching

**Review finding:** Eager resolution without caching means repeated `Evaluate()`
calls within a single user action re-resolve the same attributes. At 200 users
with 5 auth checks per command, this creates unnecessary load.

**Decision:** Implement per-request attribute caching from the start using a
shared `AttributeCache` attached to the request context.

**Rationale:** The cache is scoped to a single request (no stale data risk) and
provides significant savings when a single user action triggers multiple
authorization checks. The implementation cost is low (a map with a mutex) and
avoids a predictable performance problem that would require retrofitting later.

---

## 27. Unified `AttributeProvider` Interface

**Review finding:** The spec defined `AttributeProvider` twice with incompatible
signatures — `ResolveSubject`/`ResolveResource` in the Core Interfaces section
vs. a single `Resolve` with `LockTokens()` in the Lock section.

**Decision:** Unify into a single interface with `ResolveSubject`,
`ResolveResource`, and `LockTokens()`. Providers that contribute no lock
vocabulary return an empty slice from `LockTokens()`.

**Rationale:** The subject/resource distinction matters because providers may
resolve different attributes depending on whether the entity is the principal or
the target. A single `Resolve` method loses this context. Adding `LockTokens()`
to the same interface keeps the provider contract in one place.

---

## 28. Cedar-Aligned Missing Attribute Semantics

**Review finding:** The type system defined `!=` across types as returning
`true`, creating a security hazard. `principal.faction != "enemy"` would return
`true` when `faction` is missing, accidentally granting access to characters
without the attribute. The suggested workaround (`principal.reputation.score
!= 0`) was also broken — it would return `true` when the reputation plugin
was not loaded.

**Decision:** Align with Cedar: ALL comparisons involving a missing attribute
evaluate to `false`, regardless of operator (including `!=`). This matches
Cedar's behavior where a missing attribute produces an error value that causes
the entire condition to be unsatisfied.

**Example:** `principal.faction != "enemy"` with missing `faction` attribute:

1. Comparison evaluates to `false` (missing attribute → all comparisons false)
2. The `when` condition is unsatisfied
3. The `permit` policy does NOT match
4. No other policy matches → default deny
5. Outcome: Access denied (safe, conservative)

This is the desired behavior — missing attributes always fail-closed.

**Rationale:** The original `!=` semantics created a class of policies that
silently granted unintended access. Cedar's behavior is proven safe: missing
attributes are always conservative (deny). The `has` operator provides an
explicit existence check for cases where presence matters. For negation,
the defensive pattern is `principal has faction && principal.faction !=
"enemy"`.

**Updates [decision #8](#8-dsl-expression-language-scope):** Changes the type
system table from [decision #8](#8-dsl-expression-language-scope)'s implied semantics.

---

## 29. DSL `like` Pattern Validation at Parser Layer

**Review finding:** The spec referenced `glob.Compile(pattern, ':',
glob.Simple)` but `gobwas/glob` has no `Simple` option. The library natively
supports character classes (`[abc]`), alternation (`{a,b}`), and `**` — these
cannot be disabled via API.

**Decision:** Move the restriction to the DSL parser layer. The parser MUST
reject `like` patterns containing `[`, `{`, or `**` syntax before passing them
to `glob.Compile(pattern, ':')`. This restricts `like` to simple `*` and `?`
wildcards only.

**Note on backslash escapes:** Backslash characters in patterns are treated as
literal (no escape mechanism). The parser validation MUST be tested against
`gobwas/glob` behavior during implementation. If `gobwas/glob` interprets
backslash as an escape character, the parser MUST reject patterns containing
backslash at parse time to maintain the "no escape" guarantee.

**Rationale:** Parser-level validation gives clear error messages at policy
creation time rather than unexpected matching behavior at evaluation time.
Restricting to simple wildcards keeps the lock syntax approachable for
non-technical game admins.

---

## 30. PolicyCompiler Component

**Review finding:** The spec jumped from "DSL text stored in PostgreSQL" to
"engine evaluates conditions" without defining the compilation pipeline.
Without this, every `Evaluate()` would re-parse DSL text, violating the <5ms
p99 target.

**Decision:** Add a `PolicyCompiler` component responsible for parsing DSL text
to AST, validating attribute references, pre-compiling glob patterns for `like`
expressions, and producing a `CompiledPolicy` struct. The compiled form is
stored alongside DSL text (as JSONB) and used by the in-memory policy cache.

**Rationale:** Compilation at store time (not evaluation time) ensures
`Evaluate()` only works with pre-parsed, pre-validated policies. The compiled
form also enables validation feedback with line/column error information at
`policy create`/`policy edit` time.

---

## 31. Provider Re-Entrance Prohibition

**Review finding:** If a plugin's `ResolveSubject()` called back into the
access control system, it would create a deadlock since the engine is already
mid-evaluation.

**Decision:** Attribute providers MUST NOT invoke `AccessControl.Check()` or
`AccessPolicyEngine.Evaluate()` during attribute resolution. Providers that
need authorization-gated data MUST access repositories directly, consistent
with the `PropertyProvider` pattern.

**Rationale:** The dependency chain `Engine → Provider → Engine` is a deadlock
by design. The existing `PropertyProvider → PropertyRepository` pattern
(bypassing `WorldService`) already demonstrates the correct approach. Making
this an explicit prohibition prevents plugin authors from introducing the
same pattern.

---

## 32. PropertyProvider Uses SQL JOIN for Parent Location

**Review finding:** The `PropertyProvider` needed the parent entity's location
ULID when resolving `parent_location`. The original design used a
`LocationLookup` dependency, creating a `PropertyProvider → LocationLookup →
WorldService` chain that re-introduced the circular dependency the provider
pattern was designed to avoid.

**Decision:** `PropertyRepository` resolves `parent_location` via a recursive
CTE that walks the containment chain (see [Decision #44](#44-nested-container-resolution-via-recursive-cte)
for the full query). For top-level objects and locations, this is equivalent to
a simple JOIN against `objects.location_id`. For nested containers (objects
inside objects), the CTE walks upward until finding an ancestor with a non-NULL
`location_id`. No extra Go-level dependency is required.

**Rationale:** The data is already in PostgreSQL. A single query with a
recursive CTE handles both top-level and nested container cases correctly. This
keeps the provider dependency chain flat: `PropertyProvider →
PropertyRepository → PostgreSQL`.

---

## 33. Plugin Lock Tokens MUST Be Namespaced

**Review finding:** Token conflict resolution was fatal at startup (server
refuses to start on collision), but plugins only SHOULD namespace their tokens.
Without enforcement, a plugin registering `score` would collide with any future
core token or another plugin's `score`, causing server startup failures that are
hard to diagnose.

**Decision:** Plugin lock tokens MUST use a dot-separated prefix that **exactly
matches** their plugin ID (e.g., plugin `reputation` registers
`reputation.score`, plugin `crafting` registers `crafting.type`). Abbreviations
are not allowed — the prefix before the first `.` MUST equal the plugin ID
string. The engine validates this at registration time — plugin tokens without
the correct namespace prefix are rejected.

**Rationale:** Fatal startup errors from token collisions should be preventable,
not just detectable. Requiring namespacing makes collisions structurally
impossible between plugins (each has a unique ID) while core tokens remain
un-namespaced.

**Clarification:** These are separate checks:

1. **Namespace enforcement:** Plugin tokens MUST be prefixed with the plugin's
   own ID. The engine rejects tokens that don't match the registering plugin's
   ID prefix. This prevents cross-plugin conflicts (plugin A cannot register
   `pluginB.score`).

2. **Duplicate plugin detection:** If two plugins with identical IDs are
   loaded, the second plugin's registration MUST fail with a clear error
   ("plugin ID already registered"). This check happens before token
   registration and prevents deployment errors.

These are separate checks: namespace enforcement prevents cross-plugin
conflicts; duplicate plugin ID detection prevents deployment errors.

---

## 34. Time-of-Day Attributes for Environment Provider

**Review finding:** The original spec included only `env.maintenance_mode`
(since renamed to `env.maintenance` in the final spec) and `env.game_state`
as environment attributes. Time-based policy gating (e.g.,
restrict certain areas during night hours) was not possible.

**Decision:** Add `env.hour` (float64, 0-23), `env.minute` (float64, 0-59),
and `env.day_of_week` (string, e.g., `"monday"`) to the EnvironmentProvider.
These are numeric/string attributes resolved from `time.Now()` at evaluation
time, not duration-based.

**Rationale:** Time-of-day gating is the common use case for MUSH environments
(night-only areas, weekend events). Numeric hour/minute with string day_of_week
matches the DSL's existing comparison operators naturally — no new time type
needed.

**Note:** `game_state` was mentioned in the original spec but is not included
in the final EnvironmentProvider schema — HoloMUSH does not currently have a
game state management system. It MAY be added when that system is implemented.

---

## 35. Audit Log Source Column and No Decision Column

**Review finding:** (a) The `access_audit_log` schema had both `decision` and
`effect` columns where `decision` was strictly derivable from `effect`. (b) The
schema had no way to distinguish whether an audit record came from the ABAC
engine, the static adapter, or shadow mode.

**Decision:** (a) Drop the `decision` column. The `effect` column alone
indicates the outcome: `allow` = allowed, `deny`/`default_deny` = denied. (b)
Add a `source TEXT NOT NULL DEFAULT 'admin'` column to `access_policies` to
track where policies originate (`admin`, `lock`, `seed`, `plugin`).

**Validation constraint:** Policies named `seed:*` MUST have `source='seed'`
and vice versa. Policies named `lock:*` MUST have `source='lock'` and vice
versa. Violations are rejected at creation time to prevent naming/source
inconsistencies.

**Rationale:** (a) Redundant columns in append-heavy audit tables waste storage
and create consistency risks. The `effect` column already encodes the decision.
(b) The `source` column enables filtering and lifecycle management — e.g.,
showing only admin-authored policies, or identifying lock-generated policies
for cleanup.

---

## 36. Direct Replacement (No Adapter)

**Review finding:** The adapter pattern ([decision #5](#5-migration-strategy)) and shadow mode
([decision #21](#21-shadow-mode-cutover-criteria)) add significant complexity: normalization helpers, migration
adapters, shadow mode metrics, cutover criteria, exclusion filtering. This
complexity exists solely to support incremental migration from
`StaticAccessControl`.

**Decision:** Replace `StaticAccessControl` directly with `AccessPolicyEngine`.
No backward-compatibility adapter. All call sites switch to `Evaluate()`
directly.

**Rationale:** HoloMUSH has no production releases and no deployed users. The
static access control system has no consumers to preserve compatibility with.
Building adapter and shadow mode infrastructure for a system that has never
been released wastes effort and makes the design harder to understand.

**Impact:**

- Removes `accessControlAdapter`, `migrationAdapter`, `normalizeSubjectPrefix()`,
  `normalizeResource()`, `shadowModeMetrics`
- Removes shadow mode cutover criteria, exclusion filtering, disagreement
  tracking
- All ~30 production call sites update to `AccessPolicyEngine.Evaluate()` in a single
  phase (phase 7.3)
- The `AccessControl` interface and `StaticAccessControl` struct are deleted
  in phase 7.6

**Supersedes:** [decision #5](#5-migration-strategy) (adapter pattern),
[decision #21](#21-shadow-mode-cutover-criteria) (shadow mode)

---

## 37. No Shadow Mode

**Review finding:** Shadow mode validates that seed policies replicate the
static system's behavior. But the static system has known gaps: `$here` resource
patterns that never match actual call site resource strings (dead permissions),
missing `delete:location` for builders, legacy `@`-prefixed command names. The
seed policies intentionally fix these gaps, making 100% agreement impossible
without exclusion filtering — which itself is bug-prone.

**Decision:** Remove shadow mode entirely. The ABAC seed policies define the
correct permission model from scratch, not a replica of the static system.

**Rationale:** Shadow mode is only valuable when migrating a live system with
existing users. HoloMUSH has no releases. The seed policies are validated by
integration tests, not by runtime comparison with a legacy system. This
eliminates an entire class of complexity and removes the risk of exclusion
filtering bugs masking real policy errors.

---

## 38. Audit Log Configuration Modes

**Review finding:** The original spec logged all denials unconditionally with
optional allow logging. There was no way to disable audit logging entirely
(for development/performance) or to control the mode at runtime.

**Decision:** Add three audit modes: `off`, `denials_only`, `all`. Default to
`denials_only` for production.

| Mode           | What is logged            | Use case                    |
| -------------- | ------------------------- | --------------------------- |
| `off`          | Nothing                   | Development, performance    |
| `denials_only` | Deny + default_deny       | Production default          |
| `all`          | All decisions incl. allow | Debugging, compliance audit |

When mode is `all`, system subject bypasses are also logged with
`effect = "system_bypass"` to provide a complete audit trail.

**Rationale:** At 200 users with ~120 checks/sec peak, `all` mode produces
~10M records/day (~35GB at 7-day retention). `denials_only` mode reduces this
to a small fraction (most checks result in allows). `off` mode eliminates
audit overhead entirely for development. The mode is configurable via server
settings and can be changed at runtime without restart.

---

_The following decisions were captured during the second architecture review
of PR #65. They record additional refinements made in response to review
findings._

---

## 39. `EffectSystemBypass` as Fourth Effect Variant

**Review finding (C2):** System subject bypass was handled by early return
before `Evaluate()` reached conflict resolution. This meant bypass decisions
were invisible to the type system and audit logging — callers couldn't
distinguish "no policy matched (default deny)" from "system bypassed all
policies."

**Decision:** Add `EffectSystemBypass` as a fourth variant in the `Effect`
enum:

```go
const (
    EffectDefaultDeny   Effect = iota // No policy matched
    EffectAllow
    EffectDeny
    EffectSystemBypass                // System subject bypass (audit-only)
)
```

**Rationale:** Making bypass explicit in the type system means audit logging,
metrics, and callers can distinguish all four outcomes. The `all` audit mode
logs bypass events, providing a complete trail of system-level operations.

---

## 40. `has` Operator Supports Dotted Attribute Paths

**Review finding (C3):** The `has` operator only accepted simple identifiers
(`principal has faction`), but plugin attributes use dotted namespaces
(`reputation.score`). Without dotted path support, `has` couldn't check for
the existence of plugin-contributed attributes.

**Decision:** Extend the grammar to allow dotted paths after `has`:

```text
| attribute_root "has" identifier { "." identifier }
```

The parser joins segments with `.` and checks the resulting flat key against
the attribute bag. `principal has reputation.score` checks whether
`"reputation.score"` exists in the subject's attribute bag.

**Rationale:** Attribute providers register namespaced keys
(`reputation.score`, not nested maps). The `has` operator must match the same
flat-key model. Without this, admins couldn't write defensive patterns like
`principal has reputation.score && principal.reputation.score >= 50` for
plugin-contributed attributes.

---

## 41. LL(1) Parser Disambiguation for Condition Grammar

**Review finding (C1):** The condition grammar has an ambiguity when the
parser sees an identifier after an expression — it could be the start of a
`has` check or a binary operator. Without a disambiguation rule, the parser
would need unbounded lookahead.

**Decision:** Use one-token lookahead: after parsing a primary expression, if
the next token is `has`, parse a `has`-expression; if the next token is a
comparison or logical operator, parse a binary expression; otherwise, return
the primary expression.

**Rationale:** LL(1) lookahead is sufficient because `has` is a keyword that
cannot appear as an attribute name or operator. This keeps the parser simple
(no backtracking, no GLR) while handling the full grammar.

**Implementation note:** Phase 7.2 recommends participle (PEG-based parser)
which uses ordered-choice semantics rather than explicit lookahead. The LL(1)
specification describes the _logical grammar design_ (what ambiguities exist,
how to resolve them). Participle's PEG ordered-choice achieves the same
disambiguation effect — when multiple alternatives match, the first one in
source order is selected. Implementers MUST verify disambiguation behavior with
test cases covering ambiguous inputs (e.g., `principal.faction` alone vs
`principal.faction == "red"`) regardless of parser implementation choice.

---

## 42. Sequential Provider Resolution

**Review finding (I1):** The spec didn't justify why attribute providers are
resolved sequentially rather than in parallel. With 4+ providers, parallel
resolution could reduce latency.

**Decision:** Keep sequential resolution. Document the rationale explicitly.

**Rationale:** At ~200 concurrent users with providers backed by indexed
PostgreSQL queries, parallel resolution saves <1ms total. Sequential
resolution provides deterministic merge order (later providers can't
silently overwrite earlier attributes), simpler error attribution (which
provider failed is unambiguous), and straightforward debugging
(`slog.Debug` after each provider). Parallel resolution adds goroutine
management, merge synchronization, and non-deterministic log ordering for
negligible latency benefit. If profiling shows provider resolution exceeding
the 2ms target, parallelization can be introduced without API changes.

**Note:** This decision assumes each provider completes in <500μs with indexed
PostgreSQL queries. If profiling during implementation shows attribute
resolution exceeding the 2ms budget (see [Decision #23](#23-performance-targets)),
parallel resolution can be added without API changes — the `AttributeResolver`
interface supports both sequential and parallel strategies.

---

## 43. Property Lifecycle: Go-Level CASCADE Cleanup

**Review finding (I5):** The spec defined `entity_properties` with
`parent_type` and `parent_id` FK columns but didn't address what happens to
properties when their parent entity is deleted. Orphaned rows would accumulate
silently.

**Decision:** Go-level CASCADE in `WorldService` deletion methods:

- `WorldService.DeleteCharacter()` → `PropertyRepository.DeleteByParent("character", charID)`
- `WorldService.DeleteObject()` → `PropertyRepository.DeleteByParent("object", objID)`
- `WorldService.DeleteLocation()` → `PropertyRepository.DeleteByParent("location", locID)`

Both operations happen within the same database transaction. If either fails,
the entire transaction rolls back.

**Rationale:** Database-level `ON DELETE CASCADE` would require a polymorphic
FK (parent_type + parent_id pointing to different tables), which PostgreSQL
doesn't support natively. Go-level cleanup in `WorldService` is explicit,
testable, and consistent with the project's "no database triggers" constraint.
Transactional guarantees prevent orphans without background jobs.

---

## 44. Nested Container Resolution via Recursive CTE

**Review finding (I6):** The spec's `parent_location` resolution for objects
used a simple JOIN against `objects.location_id`, but the world model supports
nested containers (objects inside objects). An object in a chest in a room
would have `location_id = NULL` — the simple JOIN would fail to resolve its
location.

**Decision:** Use a recursive CTE to walk the containment chain:

```sql
WITH RECURSIVE chain AS (
    SELECT id, location_id, contained_in_object_id,
           ARRAY[id] AS path, 1 AS depth
    FROM objects WHERE id = $1
    UNION ALL
    SELECT o.id, o.location_id, o.contained_in_object_id,
           c.path || o.id, c.depth + 1
    FROM objects o
    JOIN chain c ON o.id = c.contained_in_object_id
    WHERE NOT o.id = ANY(c.path)  -- cycle detection
      AND c.depth < 20            -- depth limit
)
SELECT location_id FROM chain WHERE location_id IS NOT NULL LIMIT 1;
```

**Note:** The `path` array column tracks visited IDs to detect cycles
(corrupted containment data). The `depth < 20` limit provides defense-in-depth
against pathological chains. Both guards are REQUIRED in the implementation —
PostgreSQL `WITH RECURSIVE` does not automatically prevent cycles.

**Rationale:** The existing `object_repo.go` already uses recursive CTEs for
containment queries. Reusing this pattern in `PropertyRepository` ensures
`parent_location` resolves correctly regardless of nesting depth. The CTE
terminates when it finds the first ancestor with a `location_id`.

---

## 45. Bounded List Sizes for `visible_to` / `excluded_from`

**Review finding (S3):** The `visible_to` and `excluded_from` TEXT arrays
had no size limit. A character could theoretically add thousands of entries,
degrading `containsAny`/`in` evaluation performance.

**Decision:** Enforce a maximum of 100 entries per list. The property store
rejects updates that would exceed this limit with a clear error message.

**Rationale:** 100 entries balances flexibility (covers 99% of realistic MUSH
use cases — a property visible to 100 specific characters is already unusually
granular) with performance (avoids multi-millisecond scans). At 100 entries,
`in` evaluation requires O(100) string comparisons per check. Adversarial lists
(e.g., 10,000+ entries) would require hash set implementation instead of linear
scan to maintain p99 latency targets.

---

## 46. `policy validate` and `policy reload` Commands

**Review finding (S1, S2):** The command set had no dry-run validation for
policies (admins had to create a policy to discover syntax errors) and no way
to force-refresh the cache when LISTEN/NOTIFY was potentially down.

**Decision:** Add two commands:

1. **`policy validate <dsl-text>`** — Parses and validates DSL without
   creating a policy. Returns success or detailed error with line/column
   information. Available to admins and builders (builders can validate
   hypothetical policies without creating them).

2. **`policy reload`** — Forces an immediate full reload of the in-memory
   policy cache from PostgreSQL. Admin-only. Intended for emergency use when
   LISTEN/NOTIFY may be disconnected.

**Rationale:** `policy validate` closes the feedback loop — admins can iterate
on policy syntax without creating throwaway policies. `policy reload` provides
a manual override for the automatic cache invalidation system, ensuring admins
are never stuck waiting for reconnection during an emergency.

---

## 47. Fuzz Testing for DSL Parser

**Review finding:** The DSL parser accepts untrusted admin input. Without fuzz
testing, edge cases in the parser (malformed Unicode, deeply nested
expressions, pathological patterns) could cause panics or infinite loops.

**Decision:** Add Go-native fuzz tests (`func FuzzParseDSL`) targeting the
DSL parser. The fuzzer exercises `parser.Parse()` with random byte sequences
and validates that it either returns a valid AST or a structured error —
never panics, never hangs.

**Note on lock expression parser:** The lock expression parser SHOULD also have
a fuzz test (`func FuzzParseLock`) since lock expressions accept player input
and must handle malformed input gracefully. Lock parsing is a separate code
path from full DSL policy parsing.

**Rationale:** Go 1.18+ includes built-in fuzz testing. The DSL parser is the
primary attack surface for crafted input. Fuzz testing catches classes of bugs
(buffer overflows in string handling, stack overflow from recursive descent,
infinite loops from ambiguous grammar) that unit tests rarely cover. CI runs
`go test -fuzz=FuzzParseDSL -fuzztime=30s` to catch regressions.

---

## 48. Deterministic Seed Policy Names

**Review finding (I2):** Seed policies used descriptive comments
(`// player-powers: self access`) but had no stable, deterministic name for
idempotent seeding. Without deterministic names, server restart could create
duplicate seeds.

**Decision:** All seed policies use the naming convention `seed:<purpose>`
where the purpose is a kebab-case description of the policy's intent:

- `seed:player-self-access`
- `seed:player-location-read`
- `seed:player-character-colocation`
- `seed:player-object-colocation`
- `seed:player-stream-emit`
- `seed:player-movement`
- `seed:player-basic-commands`
- `seed:builder-location-write`
- `seed:builder-object-write`
- `seed:builder-commands`
- `seed:admin-full-access`
- `seed:property-public-read`
- `seed:property-private-read`
- `seed:property-admin-read`

**Rationale:** Deterministic names enable idempotent seeding (upsert by name)
and allow admins to identify seed policies via `policy list`. The `seed:`
prefix prevents accidental collision with admin-created policies and enables
`policy list --source=seed` filtering.

---

## 49. Revised Audit Volume Estimate

**Review finding (I7):** The original estimate of ~864K records/day assumed
~5 checks/sec. Real MUSH workloads (movement, look, inventory, say, property
reads) produce ~120 checks/sec peak at 200 users.

**Decision:** Revise the estimate: `all` mode produces ~10M records/day (~35GB
at 7-day retention with uncompressed audit rows). `denials_only` mode remains
practical at a fraction of this volume.

**Rationale:** The corrected estimate affects operational guidance (disk
provisioning, retention policy, partition strategy). Admins need accurate
numbers to make informed decisions about audit mode selection.

---

## 50. Plugin Attribute Collision Behavior

**Review finding:** The original spec had a fatal startup error when a plugin
registered an attribute colliding with a core attribute, which was
disproportionate — one bad plugin would brick the entire server.

**Decision:** Reject the plugin's provider registration and continue startup.
Server remains operational with other plugins. Log at ERROR level with plugin
ID and colliding attribute name. Plugin is disabled.

**Rationale:** A single misconfigured plugin should not prevent server startup.
Rejecting the individual plugin preserves availability while core attribute
guarantees remain intact.

**Cross-reference:** Main spec, Provider Registration Order section.

---

## 51. Session Integrity Error Classification

**Review finding:** The spec treated session-character-integrity errors (where
a character is deleted while sessions referencing it still exist) as a normal
operational path.

**Decision:** Classify as a bug/exceptional condition, not a normal path. Log
at CRITICAL level. Operators SHOULD configure alerting. Transactional cleanup
requirement (delete sessions in same transaction as character deletion)
unchanged.

**Rationale:** If the transactional cleanup works correctly, this error should
never occur. Its presence indicates a defect in session invalidation logic or
data corruption — it deserves CRITICAL severity, not INFO.

**Cross-reference:** Main spec, Session Subject Resolution section.

---

## 52. Async Audit Writes

**Review finding:** The spec didn't specify whether audit log writes are
synchronous or asynchronous, which has major performance implications.

**Decision:** Audit log inserts use async writes via a buffered channel.
`Evaluate()` enqueues the audit entry to a channel, and a background goroutine
batch-writes to PostgreSQL. When the channel is full, increment counter metric
`abac_audit_channel_full_total` and drop the entry. Audit logging is
best-effort and MUST NOT block authorization decisions.

**Rationale:** Synchronous audit writes in the authorization hot path would add
latency to every access check. Async writes decouple authorization performance
from audit I/O. The best-effort model accepts that some audit entries may be
lost under extreme load, which is preferable to blocking authorization.

**Cross-reference:** Main spec, Audit Log section.

---

## 53. Audit WAL Best-Effort Semantics

**Review finding:** The spec said denial audit logs "MUST be written
synchronously" with a WAL fallback if the DB write fails, but also specified
graceful degradation (increment counter and drop) when both DB and WAL fail.
These requirements contradict each other — MUST-audit is incompatible with
graceful degradation on dual failure.

**Decision:** Change denial audit writes to SHOULD (best-effort). If both the
database and WAL file are unavailable, log to stderr and increment
`abac_audit_write_failures_total`. Accept that some denial audit entries may be
lost during catastrophic failures. Additionally, standardize the WAL file path
to `$XDG_STATE_HOME/holomush/audit-wal.jsonl` (XDG_STATE_HOME is semantically
correct for transient state) and consolidate the duplicate WAL descriptions in
the spec into a single section.

**Rationale:** A pragmatic approach that preserves audit logging during normal
operation while avoiding the impossible contract of guaranteed writes during
infrastructure failure. The stderr fallback ensures operators can still observe
failures through system-level log aggregation. XDG_STATE_HOME is the correct
XDG directory for state data that is not essential to preserve across reinstalls.

**Cross-reference:** Main spec, Audit Log Configuration section; bead
`holomush-3hdt`.

---

## 54. Property Location Resolution Eventual Consistency

**Review finding:** At READ COMMITTED isolation, the PropertyProvider's
recursive CTE and subject attribute resolution run in separate transactions.
During character movement, the authorization check could see inconsistent
snapshots — e.g., character in Room B but object still in Room A's containment
hierarchy — violating the design's stated "point-in-time snapshot" invariant.

**Decision:** Document this as a known limitation. MUSH movement is
low-frequency (human-speed, not machine-speed) and the practical impact is
negligible. The 100ms timeout and circuit breaker handle operational concerns.
Do not redesign the visibility model or add pessimistic locking.

**Rationale:** The cost of strict consistency (pessimistic locks or
REPEATABLE READ transactions spanning all providers) is disproportionate to the
risk. In practice, a character moving between rooms takes 100-500ms of human
reaction time, during which authorization checks for their objects are
extremely unlikely. Accepting eventual consistency here follows the principle
of not over-engineering for scenarios that cannot cause meaningful harm.

**Cross-reference:** Main spec, Property Model section; bead `holomush-n0k5`.

---

## 55. Session Error Code Simplification

**Review finding:** The spec defined four distinct error cases for session
resolution (SESSION_NOT_FOUND, SESSION_STORE_ERROR, SESSION_NO_CHARACTER,
SESSION_CHARACTER_INTEGRITY) with separate policy IDs, effect codes, and
metrics. This was disproportionate complexity for an edge case.

**Decision:** Simplify to two error codes:

- **SESSION_INVALID** — covers not-found sessions and missing characters.
  Normal operation (expired sessions, logout).
- **SESSION_STORE_ERROR** — covers database unavailability and timeouts.
  Infrastructure failure.

Character deletion integrity (SESSION_CHARACTER_INTEGRITY) is moved to the
world model's responsibility via CASCADE constraints on session deletion when
characters are deleted.

**Rationale:** Both SESSION_NOT_FOUND and SESSION_NO_CHARACTER result in the
same `Decision{Allowed: false, Effect: EffectDefaultDeny}`. The distinction
only matters for forensics, which is rare. The character deletion case is a
world model invariant violation and should be prevented by the world service
(CASCADE delete on sessions), not detected by the authorization layer.

**Cross-reference:** Main spec, Session Subject Resolution section; bead
`holomush-935g`.

---

## 56. Audit Off Mode Includes System Bypasses

**Review finding:** The spec required system_bypass logging in ALL audit modes
including `off`, but `off` mode was defined as "Nothing logged" in the
configuration table. These statements contradicted each other.

**Decision:** Update the `off` mode description from "Nothing" to "System
bypasses only." This preserves the security intent of always logging when the
`system` subject bypasses policy evaluation, while making the table description
accurate.

**Rationale:** System bypass events are security-significant — they indicate
operations running outside normal policy evaluation. Even in development
environments where `off` mode is used for performance, operators should be
aware of bypass activity. The volume is negligible (system operations are
rare), so there is no performance impact.

**Cross-reference:** Main spec, Audit Log Configuration section; bead
`holomush-75um`.

---

## 57. ADR Format Evolution

**Review finding:** New ADRs (0009-0016) use a different section structure than
existing ADRs (0001-0008). Old format: Context > Options Considered > Decision
\> Consequences > References. New format: Context > Decision > Rationale >
Consequences > References (with Options embedded in Context).

**Decision:** This is intentional evolution. The new format is internally
consistent across all 8 new ADRs and makes the Rationale more prominent.
Document the new format in `docs/adr/README.md`. Do not retroactively change
old ADRs. Additionally, move ADR 0010's unique "Testing Requirements" section
into the Consequences section or the main spec's testing section to maintain
ADR structural consistency.

**Rationale:** ADR formats naturally evolve as teams learn what information is
most useful. The new format emphasizes Rationale (why) over Options (what
else), which is more valuable for future readers. Documenting the evolution
prevents confusion without requiring retroactive changes.

**Cross-reference:** All ADRs in `docs/adr/`; bead `holomush-ly15`.

---

## 58. Provider Re-Entrance Goroutine Prohibition

**Review finding:** The context-based re-entrance sentinel only prevents
synchronous re-entrance on the same goroutine. A provider spawning a new
goroutine that calls `Evaluate()` with `context.Background()` bypasses the
guard entirely, potentially causing deadlock or state corruption.

**Decision:** Add an explicit MUST NOT prohibition in the provider contract
stating that providers MUST NOT spawn goroutines that call back into
`Evaluate()`. Detection remains convention-based — no runtime goroutine ID
tracking. Integration tests SHOULD include a scenario verifying that
goroutine-based re-entry is handled (panic or error).

**Rationale:** Runtime goroutine ID tracking (via `sync.Map` or similar) adds
complexity and performance overhead to every `Evaluate()` call. At the current
scale, convention enforcement through code review and clear contract
documentation is sufficient. The MUST NOT clause makes the prohibition explicit
rather than implicit, and the integration test catches violations in CI rather
than production.

**Cross-reference:** Main spec, Attribute Providers section; decision #31
(Provider Re-Entrance Prohibition); bead `holomush-npmk`.

---

## 59. Fair-Share Provider Timeout Scheduling

**Question:** What scheduling approach should be used for distributing the 100ms
evaluation timeout budget across attribute providers during policy evaluation?

**Decision:** Fair-share dynamic allocation is the definitive approach for
provider timeout scheduling. Each provider receives `max(remaining_budget /
remaining_providers, 5ms)` at call time, with unused time automatically
redistributed to subsequent providers. The 100ms total budget serves as a hard
backstop. This is a go-forward commitment—we have evaluated the alternatives and
accept the tradeoffs.

**Rationale:** Fair-share maximizes total budget utilization while maintaining
simplicity. Fast providers (e.g., in-memory lookups returning in <1ms) donate
their unused time to slower providers (e.g., database queries), naturally
balancing the system without operator configuration. The formula is trivial to
implement and reason about, with no tuning parameters beyond the hard 100ms
limit.

**Acknowledged concerns and why they're acceptable:**

1. **Registration order dependency** — A slow early provider can consume budget
   before fast later providers run. This is acceptable because: (a) providers
   register at startup in deterministic order, making behavior predictable; (b)
   the 5ms minimum ensures even late providers get meaningful time; (c) truly
   pathological cases (e.g., a provider consistently timing out) surface quickly
   in monitoring and can be fixed at the provider level.

2. **Dynamic per-provider budgets complicate alerting** — Operators cannot set
   static "provider X should complete in Y ms" alerts because budgets vary based
   on what ran before. This is acceptable because: (a) the total 100ms budget
   provides a clear hard limit for alerting; (b) per-provider P99 latency
   metrics still reveal slow providers; (c) the alternative (fixed equal slices)
   wastes budget when fast providers finish early, making total evaluation
   slower on average.

3. **Alternatives considered but not chosen:**
   - **Fixed equal slices** (e.g., 20ms each for 5 providers) — Simple but
     wastes unused time, making evaluations slower when some providers are fast.
   - **Priority-based allocation** — Adds configuration burden (operators must
     rank provider importance) and complexity (priority queues, starvation
     prevention) with unclear benefit given most providers are ~1-5ms.
   - **Parallel execution** — Would give each provider the full 100ms budget but
     requires thread-safe providers (breaking current single-goroutine contract)
     and adds synchronization complexity. Deferred as future optimization if
     profiling shows evaluation latency is a bottleneck.

Fair-share is the optimal balance of simplicity, utilization, and
predictability. The 100ms hard backstop prevents runaway evaluations, and the
self-balancing property eliminates the need for operator tuning. We move forward
with this approach.

**Cross-reference:** Main spec, Attribute Providers section (lines 1768-1807);
decision #58 (Provider Re-Entrance Goroutine Prohibition).

---

_The following decisions were captured during the implementation plan review
(PR #69). They record decisions made in response to review findings._

---

## 60. Restricted Visibility via Seed Policies Only

**Review finding (PR #69, Critical #2):** The implementation plan's Task 16c
implemented restricted property visibility as pre-engine Go code that
short-circuits before policy evaluation. The spec (lines 1191-1217, 2380-2384)
describes restricted visibility as seed policies evaluated through normal ABAC
evaluation — "the character configures data that existing system-level policies
evaluate." Task 16c created dual enforcement paths and undermined the
"everything is a policy" design principle. Additionally, the pseudocode accessed
`req.Resource.Type` but `AccessRequest.Resource` is a `string`, not a struct.

**Decision:** Remove Task 16c entirely. Handle restricted property visibility
purely through seed policies in Task 22. The `visible_to` permit and
`excluded_from` forbid policies (spec lines 1078-1090) become the sole
enforcement mechanism for restricted visibility.

**Rationale:** A single enforcement path (all visibility decisions flow through
ABAC policy evaluation) is easier to audit, reason about, and debug than dual
paths where some checks bypass the engine. The spec explicitly designed
restricted visibility as policy-evaluated — pre-engine filtering was an
implementation shortcut that violated the architecture. Seed policies also
benefit from the existing `policy test` debugging command and audit logging,
which pre-engine code would not.

**Impact:**

- Task 16c removed from implementation plan
- Task 22 gains two additional seed policies (bringing count to 16):
  `seed:property-visible-to` and `seed:property-excluded-from`
- Dependency graph updated (16c node and edges removed)

**Cross-reference:** Main spec, Property Visibility section (lines 1191-1217);
decision #10 (Property Visibility Defaults); beads `holomush-5k1.191`,
`holomush-5k1.192`.

---

## 61. Lock Policy Naming Includes Resource Type

**Review finding (PR #69, Critical #4):** The implementation plan's Task 25b
uses `lock:<type>:<resource_id>:<action>` (e.g., `lock:object:01ABC:read`).
The spec (lines 2412-2417) uses `lock:<resource_ulid>:<action>` without the
`<type>` segment.

**Decision:** Keep the plan's extended format:
`lock:<type>:<resource_id>:<action>`. This is an intentional deviation from
the spec.

**Rationale:** The extra `<type>` segment makes lock policy names
self-describing for debugging and admin operations. Without it, an admin
seeing `lock:01JKQW3X...:read` must look up the ULID to determine whether
it controls an object, location, or character. With the type segment,
`lock:object:01JKQW3X...:read` is immediately comprehensible. The cost is
slightly longer policy names, which has no performance impact since names are
only used for lookup and display, not hot-path evaluation.

**Impact:**

- Implementation plan's Task 25b format is canonical
- Spec Deviations table in the implementation plan MUST include this entry
- `PolicyStore.DeleteByName()` queries use the extended format
- Lock commands construct names with the type segment

**Cross-reference:** Main spec, Lock Commands section (lines 2412-2417);
decision #19 (Lock Policies Are Not Versioned); bead `holomush-5k1.193`.

---

## 62. Separate PolicyEffect Type for Stored Policies

**Review finding (PR #69, I3):** The implementation plan's Task 6 reused the
decision `Effect` enum (`EffectDefaultDeny`, `EffectAllow`, `EffectDeny`,
`EffectSystemBypass`) for `StoredPolicy.Effect`. But the database stores policy
effect as `TEXT CHECK ('permit', 'forbid')` — a fundamentally different concept.
Policy effect declares what a policy *intends* (permit or forbid); decision
effect records what the engine *decided* (allow, deny, default_deny, or
system_bypass). Conflating them creates type confusion and invalid states
(e.g., a stored policy with `EffectSystemBypass`).

**Decision:** Define a separate `PolicyEffect` type with two constants:
`PolicyEffectPermit` and `PolicyEffectForbid`. This type is used exclusively
for `StoredPolicy.Effect` and `CompiledPolicy.Effect`. The existing `Effect`
enum remains for evaluation decisions only.

**Rationale:** The two enums represent different domains — authoring intent vs.
runtime outcome. Separating them makes invalid states unrepresentable at the
type level. A stored policy can only be `permit` or `forbid`; the
`system_bypass` and `default_deny` variants only exist in evaluation results.

**Cross-reference:** Main spec, Policy Data Model section; bead
`holomush-5k1.53`.

---

## 63. Remove Session Circuit Breaker Auto-Invalidation

**Review finding (PR #65, C6):** The spec's session circuit breaker
auto-invalidated sessions after 3 consecutive errors in 60 seconds. This
created a DoS attack surface: an attacker with a stolen session token could
trigger 3 deliberate errors to permanently kill the legitimate user's session.
The auto-invalidation was also over-engineered for a condition that should
never occur in normal operation (session-character integrity errors).

**Decision:** Remove auto-invalidation entirely. Session integrity errors are
logged at CRITICAL level and a Prometheus counter is incremented, but the
session is NOT killed. The session circuit breaker becomes logging-only.
Providers MUST NOT auto-invalidate sessions based on error counts.

**Rationale:** Session integrity errors indicate bugs (decision #51), not
conditions that should trigger automatic remediation. Auto-invalidation
converts a rare bug into a user-facing DoS vector. Logging at CRITICAL with
alerting gives operators visibility without harming legitimate users. If a
session is truly corrupted, operators can invalidate it manually.

**Cross-reference:** Main spec, Session Subject Resolution section; decision
\#51 (Session Integrity Error Classification); bead `holomush-3kdz`.

---

## 64. Remove Admin Bypass of Cache Staleness and Degraded Mode

**Review finding (PR #65, C5):** The spec allowed admin subjects to bypass
cache staleness thresholds (line 1875) and degraded mode checks (lines
1387-1392). This meant a compromised admin account would operate with zero
constraints during infrastructure failures — exactly when strict access control
is most critical. It also created an asymmetric window where admins used newly
updated policies while non-admins used stale cached policies, potentially
leading to inconsistent authorization state.

**Decision:** Remove admin bypass entirely. All subjects — including admins —
fail-closed during cache staleness and degraded mode. Admins use the
`policy reload` command (decision #46) for manual cache refresh when needed.

**Rationale:** The principle of least privilege applies during degraded mode
more than at any other time. A compromised admin during an infrastructure
outage is a worst-case scenario that should not be made worse by bypassing
access control. The `policy reload` command provides an explicit, auditable
mechanism for admins to refresh their view of policies without creating an
automatic bypass path.

**Cross-reference:** Main spec, Cache Staleness and Degraded Mode sections;
decision #46 (`policy validate` and `policy reload` Commands); bead
`holomush-k5c3`.
