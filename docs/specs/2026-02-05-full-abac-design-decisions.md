# Full ABAC Design Decisions

**Date:** 2026-02-05
**Participants:** Sean (lead), Claude (design assistant)
**Related:** [Full ABAC Architecture Design](2026-02-05-full-abac-design.md)

This document records the design interview that produced the Full ABAC
architecture. Each section captures the question, options considered, and
rationale for the chosen approach.

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

**Question:** How do we migrate ~28 production call sites from the old
`AccessControl` interface to the new `AccessPolicyEngine`?

**Options considered:**

| Option | Description                                 | Pros                                                  | Cons                                                                  |
| ------ | ------------------------------------------- | ----------------------------------------------------- | --------------------------------------------------------------------- |
| A      | Big-bang interface change                   | Clean, one-time effort                                | Large blast radius, all ~28 callers need error handling added at once |
| B      | New interface + adapter for backward compat | Incremental migration, preserves fail-closed behavior | Two interfaces exist temporarily                                      |

**Decision:** **Option B — New `AccessPolicyEngine` interface with adapter.**

**Rationale:** The adapter wraps the new engine to satisfy the old
`AccessControl` interface (logging errors internally, returning `false`). Callers
migrate incrementally — those needing richer error info (like the command
dispatcher) move to `AccessPolicyEngine.Evaluate()` directly, while others stay
on the adapter. The new interface also accepts structured `AccessRequest` and
returns structured `Decision` with reason, matched policy, and attributes.

**Tracking:** Migration tracked as `holomush-c6qch` (P3, depends on phase 7.3).

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
   `flag:X`, `level>=N`, `me`, `&`, `|`, `!`) that compiles to scoped policies.
   Ownership verified before creation.
3. **Full policies** (admin only) — Full DSL with unrestricted scope.

Admin `forbid` policies always trump player locks via deny-overrides.

---

## 13. No Database Triggers

**Sean's hard constraint:** No database triggers or stored procedures. All logic
must live in Go application code. PostgreSQL is storage only.

**Impact:** Visibility defaults, LISTEN/NOTIFY notifications, and version
history management are all handled in Go store implementations.
