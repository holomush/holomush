<!--
SPDX-License-Identifier: Apache-2.0
Copyright 2026 HoloMUSH Contributors
-->

# Plugin Least-Privilege Enforcement & Plugin-Trust Security — Design

**Bead:** `holomush-eykuh.3` (sub-spec 4 of epic `holomush-eykuh`)
**Theme:** `theme:plugin-capability-architecture` (`docs/roadmap.md`)
**Status:** draft for design review
**Date:** 2026-06-12
**Depends on:** sub-specs 1–3 (`holomush-oeb4d`, `holomush-eykuh.1`, `holomush-eykuh.2`)

## Overview

Sub-specs 1–3 landed the **coarse** least-privilege model: a plugin reaches a
host capability or another plugin's service only if its manifest declares it,
both runtimes consume through the one broker/registry common path
(INV-PLUGIN-44/45), and the ABAC subject is host-stamped `plugin:<name>`
(anti-forgery, INV-PLUGIN-22/26). What those sub-specs **deferred to this one**
is everything that makes a grant fine-grained and policy-governed: the per-entry
`access:` and `scope:` parameters' semantics, an actual ABAC decision on
capability/service access (not a static declaration-set check), and the
plugin-trust hardening surfaced by the PR #4430 review.

This sub-spec is organized around **one primitive and four mechanisms**:

- **P0 — the dispatch-context primitive.** A host-vouched acting-character
  subject + attributes stamped onto every brokered call. Everything else
  consumes it; it is what makes the four mechanisms one cohesive change rather
  than four bolt-ons.
- **M1 — the ABAC access gate** (plugin-as-subject; declaration necessary, not
  sufficient).
- **M2 — `access:`** (the operation dimension: read vs write within a
  capability).
- **M3 — `scope:`** (the instance dimension: which resource instances, anchored
  to P0).
- **M4 — the plugin-trust fix** for `CommandRegistryService.ListCommands` /
  `GetCommandHelp` and their Lua twins (the PR #4430 subject-spoofing finding).

It deliberately does **not** migrate production manifests onto the new gating or
gate the legacy unconditional Lua injection — that atomic cutover is sub-spec 5
(`holomush-eykuh.4`). This sub-spec defines and enforces the mechanics and
proves them with fixtures; sub-spec 5 rolls the fleet onto them.

## RFC2119 Keywords

The keywords **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** in
this document are to be interpreted as described in RFC 2119 and RFC 8174 (see
the root `CLAUDE.md` "RFC2119 Keywords" table).

## Goals

- Define the runtime semantics of the `access:` (operation) and `scope:`
  (instance) per-entry least-privilege parameters the foundation reserved.
- Turn capability/service access into a **default-deny ABAC decision** keyed on
  the host-stamped `plugin:<name>` subject, so a declared capability is
  necessary but not sufficient — operators can deny or narrow by policy.
- Establish the **dispatch-context primitive**: a host-vouched acting-character
  subject + attributes that scoped capabilities authorize against and that the
  command-registry RPCs key on instead of wire-supplied `character_id`.
- Close the PR #4430 plugin-trust finding symmetrically across both runtimes.
- Register the new invariants (`INV-PLUGIN-50`…`53`) and bind the relevant
  `pending` consumption-path entries.

## Non-Goals (deferred / out of scope)

| Deferred concern | Sub-spec / bead |
| --- | --- |
| Migrating production manifests onto the new gating; gating the legacy unconditional Lua injection (`hostfunc/functions.go`) | 5 — `holomush-eykuh.4` |
| Adding `requires` declarations to plugins that consume host functions without declaring them today (e.g. `core-building`) | 5 — `holomush-eykuh.4` |
| New capability tokens or service decompositions beyond the 14 already defined | (none — taxonomy is sub-spec 2, frozen) |
| Reflective/descriptor-driven resource extraction in the broker | **rejected by design** (§M3, Alternatives) |

This sub-spec settles the **enforcement mechanics and the trust fix**. It does
not reopen the model, the vocabulary, or the transport — those are sub-specs 1–3
and are frozen.

## Background — current state (grounded)

### The gate is currently a declaration check, not an authorization decision

The foundation's resolver/registry (`ResolveDependencyOrder`,
`manifest.RequiredCapabilities()` / `RequiredServiceNames()`) decides reach by
**set membership**: declared ⇒ reachable. The per-call ABAC core
(`internal/plugin/pluginauthz/evaluate.go`) already exists and is the shared
binary+Lua decision point (INV-PLUGIN-26): it host-derives the subject
(`ActorSubject` → `access.PluginSubject` / `access.CharacterSubject`), enforces
the owned-type entitlement (INV-PLUGIN-24), calls the engine, and emits exactly
one audit event per decision (INV-PLUGIN-25). But it is invoked today only by
the explicit `host.Evaluate` surface — **not** by the capability/service
consumption path. The consumption path has no policy decision at all.

### The per-entry parameters are parsed but inert

`internal/plugin/dependency_type.go` already carries `Dependency.Scope` —
"the foundation parses and round-trips it but does not interpret it." There is
no `Access` field yet. The manifest schema is struct-as-source-of-truth
(`internal/plugin/manifest.go` + `dependency_type.go`, `jsonschema:` tags) with
a generated `schemas/plugin.schema.json` (drift-gated) and the human-facing
`.claude/rules/plugin-manifest.md`.

### The PR #4430 plugin-trust finding (CodeRabbit RC_3400778392 / 3400778419)

`commandRegistryServer.ListCommands` and `GetCommandHelp`
(`internal/plugin/hostcap/servers.go:947-1013`) parse `req.GetCharacterId()`
(wire-supplied) and use it directly as the ABAC subject via
`access.CharacterSubject(charID)` → `q.Available` / `q.Help`. A plugin holding
the `command-registry` capability can therefore enumerate command
visibility/help for **any** character. This is deliberate parity behavior from
sub-spec 2 (it matches the Lua `list_commands` hostfunc, `hostfunc/commands.go`,
which also keys on the request `character_id`). It is the same subject-spoofing
class that ADR `holomush-qeypl` already eliminated for `host.Evaluate` by
deriving the subject host-side. The fix MUST be symmetric — fixing only the
binary side would create a runtime privilege gradient
(`.claude/rules/plugin-runtime-symmetry.md`).

## Design

### P0 — The dispatch-context primitive (the spine)

The host stamps a **`DispatchContext`** onto the `context.Context` of every
brokered capability/service call:

```text
DispatchContext {
    Subject    string            // host-vouched ABAC subject of the acting character (access.CharacterSubject)
    Attributes map[string]string // host-resolved acting-character attributes (location, ...) for scope checks
}
```

Rules:

- It is sourced **only** from the host's own delivery context — the acting
  `core.Actor` already threaded through `DeliverCommand` / `DeliverEvent`. It
  MUST NOT be constructed from any plugin- or wire-supplied field
  (**INV-PLUGIN-51**).
- For invocations with **no** acting character (timer/startup emits, host
  background work), `DispatchContext` is **absent**. Any call that requires a
  character subject (M4) or a `scope:` check (M3) on an absent dispatch context
  **fails closed** (deny) — never falls back to a wire value or an unscoped
  grant.
- The attribute set is host-resolved via the existing ABAC attribute providers
  for the character subject — the same providers that resolve `location` etc.
  for a character today (`internal/access/policy/attribute/`). No plugin input
  participates.
- `DispatchContext` is carried on the `context.Context` via an unexported
  context-key in the `pluginauthz` package (the shared decision point both
  runtimes already depend on), with host-side `WithDispatch(ctx, dc)` /
  `dispatchFrom(ctx)` accessors. It is stamped **once, on the host delivery
  context** in `DeliverCommand` / `DeliverEvent`, before any plugin code runs —
  so every downstream path inherits it: the binary broker call, the sub-spec-3
  Lua bufconn consumption path, **and** the legacy in-VM Lua hostfuncs (which run
  on `ls.Context()` = the delivery context). Because the key is unexported from
  `pluginauthz`, a plugin can neither set nor observe it regardless of path.

P0 is what unifies the sub-spec: M3 resolves `own-location` against
`DispatchContext.Attributes`; M4 uses `DispatchContext.Subject` as the
command-registry subject. The same anti-spoofing guarantee (INV-PLUGIN-51)
covers both.

### M1 — The ABAC access gate (plugin-as-subject)

Capability/service consumption becomes a **default-deny authorization
decision**, not a set-membership check. Subject is the host-stamped
`plugin:<name>` (INV-PLUGIN-22). Declaration remains necessary (the resolver
still gates reach), but is **no longer sufficient**: an operator policy MAY deny
a declared capability (**INV-PLUGIN-50**).

**The capability descriptor (defined here, not pre-existing).** M1/M2/M3 all need
per-method metadata that no current sub-spec established: which ABAC `action` a
method takes, which `resource` type it touches, its operation class
(`read`/`write`), and whether it is scope-eligible (and if so, its
`ScopedResource` extractor). This sub-spec **defines** that host-owned table — a
`CapabilityDescriptor` per host.v1 capability, sited alongside the
token→service registry from sub-spec 2 (host-owned, in-tree):

```text
CapabilityDescriptor {
    Token   string                       // e.g. "world.mutation"
    Methods map[string]MethodDescriptor  // keyed by gRPC method name
}
MethodDescriptor {
    Action    string                     // ABAC action, e.g. "write"
    Resource  string                     // ABAC resource type, e.g. "location"
    Class     OperationClass             // read | write  (M2)
    Scopes    []string                   // supported scope tokens, e.g. ["own-location"]  (M3); empty ⇒ not scope-eligible
    Extract   ScopedResourceFn           // typed accessor; required iff Scopes non-empty  (M3, INV-PLUGIN-52)
}
```

The descriptor is the single host-owned source for all three dimensions; the
sub-spec-2 token→service map and this per-method table are the capability
vocabulary's full definition.

**Why M1 cannot call `pluginauthz.Evaluate` verbatim.** The existing
`Evaluate` enforces an owned-type entitlement —
`EVALUATE_UNENTITLED_TYPE` (`internal/plugin/pluginauthz/evaluate.go:196`):
any resource type not in the plugin's `OwnedTypes` (or the `command`
carve-out) is denied. Host-capability resources (`location`, `kv`, `session`)
are **not** any plugin's owned types, so the owned-type predicate would deny
every capability-access decision. M1 therefore adds a **capability-access
entitlement** as a sibling path in `pluginauthz` that **shares** the
subject-derivation, engine invocation, and single-audit-event machinery
(no second policy engine, no divergent logic — INV-PLUGIN-26) but substitutes
its entitlement predicate: a capability-access decision is entitled when the
manifest **declares** the capability and the resolver **satisfied** it (already
proven at load), not by `OwnedTypes`. The `action`/`resource` fed to the engine
come from the method's `CapabilityDescriptor` entry.

Per the locked topology, the decision is **split across two common paths**, each
shared by both runtimes:

| Check | Where | Inputs | Invariant |
| --- | --- | --- | --- |
| Declaration reach | **load-time resolver / per-plugin server registration** (foundation + sub-spec 3 §4) | declared set | INV-PLUGIN-45 |
| `access:` operation-class + operator policy + `scope:` instance condition | **the one `hostcap` capability interceptor** | method name → descriptor; typed request → concrete resource; `DispatchContext` | INV-PLUGIN-49/50 |

**Realization (single common path, static-before-policy).** Per-call enforcement
is one `grpc.UnaryServerInterceptor` constructed by `hostcap` and installed on
**both** runtimes' host.v1 servers — the binary broker server and the Lua
per-plugin bufconn server both register host.v1 services through
`internal/plugin/hostcap/register.go`, so that interceptor is the genuine common
path (not two transport-specific gates). It runs the cheap static checks first
(declaration re-check + `access:` operation-class from the descriptor;
method-name-only, short-circuit on deny) **before** the policy `Evaluate`
(operator policy + `scope:` condition, which needs the typed request +
`DispatchContext`). This honors the cheap-before-expensive intent while keeping
a single symmetric gate rather than a broker/server split that could diverge by
runtime.

The split is deliberate: each check sits where its inputs already live. The
broker check needs only the method name (no request parsing); the
resource-instance a call touches is known only inside the host.v1 capability
server, which already holds the typed request and is — by INV-PLUGIN-49 — the
single source both runtimes consume. Putting the scope decision there keeps it
typed and symmetric without a reflective broker seam (see Alternatives).

`action` and `resource` for the server-side `Evaluate` come from the called
method's `CapabilityDescriptor.Methods[<method>]` entry (`Action`, `Resource`)
defined above — the host-owned per-method table this sub-spec introduces.

### M2 — `access:` (the operation dimension)

A new optional `Dependency.Access` field narrows a capability grant to an
operation class:

```yaml
requires:
  - capability: kv
    access: read        # KVGet permitted; KVSet / KVDelete denied
```

- Values are a closed enum: `read`, `write`. Absent ⇒ no operation narrowing
  (the capability's full method set, subject to M1/M3).
- The operation class of each method is `CapabilityDescriptor.Methods[<method>].Class`
  (the host-owned per-method table defined in §M1). Each host.v1 method is
  classified `read` or `write`.
- Enforcement is at the **broker/registry common path** (method name is
  sufficient): if the declared `access:` does not cover the called method's
  class, the call is denied before forwarding. `access: read` + a `write` method
  ⇒ deny.
- `access:` is valid **only on `capability:` entries**. On a `service:` entry it
  is a **hard manifest error** (same boundary and rationale as `scope:`,
  INV-PLUGIN-53): the host does not own the provider's method classification, so
  it cannot enforce the promise, and an inert-but-declared keyword is exactly the
  "declared but not enforced" smell this design refuses. A provider that wants
  operation-class narrowing of its own service expresses it in its own policy.
  (A future sub-spec MAY define provider-declared service operation classes; out
  of scope here.)

### M3 — `scope:` (the instance dimension)

`scope:` narrows a capability grant to a subset of resource instances, anchored
to the dispatch context.

```yaml
requires:
  - capability: world.mutation
    scope: own-location   # may mutate only the dispatch character's current location
```

- The supported scope tokens for a capability are
  `CapabilityDescriptor.Methods[<method>].Scopes` (the host-owned table from
  §M1; e.g. `world.mutation` mutation methods → `{own-location}`). A `scope:`
  token not supported by the capability is a **hard manifest error** at load.
  The vocabulary is host-owned for the same reason the capability vocabulary is
  (sub-spec 2): the host implements the server, so it can both define and
  enforce the scope.
- A scope token compiles to a **policy condition** comparing a resource
  attribute against a dispatch-context attribute — `own-location` ⇒
  `resource.location == DispatchContext.Attributes["location"]`. The condition
  is evaluated via the capability-access path (§M1) at the host.v1 server-side
  interceptor.
- The concrete resource of a call is produced by the descriptor's typed
  `Extract` (`ScopedResource(req)`) accessor co-located with each scope-eligible
  handler — never by reflective introspection of the request in the broker. A
  scope-eligible method (`Scopes` non-empty) MUST carry an `Extract`; the
  meta-test (INV-PLUGIN-52) fails the build otherwise.
- `scope:` is valid **only on `capability:` entries.** A `scope:` on a
  `service:` entry is a hard manifest error (**INV-PLUGIN-53**). Rationale: the
  host owns neither the provider's request semantics nor its resource model, so
  it cannot honor the promise; the provider plugin governs its own resources
  (INV-PLUGIN-24) and enforces per-dispatch scope itself via `host.Evaluate`
  against the `DispatchContext` the host propagates across the broker. (Growth
  path: a provider surface that genuinely needs host-enforced cross-plugin
  scope is promoted to a host capability, at which point `scope:` works for
  free.)
- With no `DispatchContext` (P0 absent), a scoped call **fails closed**.

**Fail-closed totality (no silent un-enforcement).** A scope-eligible capability
method that lacks a wired `ScopedResource` extractor MUST deny at runtime — it
MUST NOT forward the call unscoped. This is enforced three ways
(**INV-PLUGIN-52**):

1. **Runtime:** the shared interceptor denies a scoped call whose method has no
   registered extractor (fail-closed default).
2. **Meta-test:** a completeness gate enumerates every method of every
   scope-eligible capability and fails the build if any lacks an extractor —
   catching the gap at CI, in our own (in-tree, host-owned) servers.
3. **Invariant:** INV-PLUGIN-52 binds the totality guarantee to that meta-test +
   a runtime test so a future refactor cannot quietly delete the interceptor.

Because every host capability server is host-owned and in-tree, all three
guards hold regardless of whether the *consumer* is an out-of-tree plugin: the
consumer's origin never changes where enforcement runs.

### M4 — The plugin-trust fix (command-registry)

`commandRegistryServer.ListCommands` / `GetCommandHelp`
(`internal/plugin/hostcap/servers.go`) and the Lua `list_commands` /
`get_command_help` hostfuncs (`internal/plugin/hostfunc/commands.go`) stop
deriving the ABAC subject from `req.GetCharacterId()`. Both runtimes use
**`DispatchContext.Subject`** (P0) — the host-vouched acting character —
exactly as `host.Evaluate` derives its subject host-side (ADR `holomush-qeypl`).

- The `character_id` request field is **removed from the authorization path**.
  (The wire field MAY remain for protocol compatibility but MUST NOT be read as
  the subject; the spec prefers removing it to eliminate the spoofing surface
  entirely — final disposition is a proto-compat decision at implementation.)
- A command-registry call on an **absent** `DispatchContext` fails closed (deny)
  — a plugin cannot enumerate any character's commands outside a dispatch.
- The change is made at the **shared path** for both runtimes
  (plugin-runtime-symmetry): the binary server and the Lua hostfunc derive the
  subject from the same host-stamped context; neither reads a wire subject.
- **INV-PLUGIN-51** binds the host-vouched-subject guarantee, covering both this
  fix and the P0/M3 anchoring.

## Invariants (proposed)

To be registered in `docs/architecture/invariants.yaml` on finalization, each
`binding: pending` until a test asserts it. Highest existing PLUGIN id is
`INV-PLUGIN-49`; these allocate `50`–`53`.

- **INV-PLUGIN-50 (capability access is a default-deny ABAC decision).** A
  plugin's consumption of a host capability or plugin service MUST be authorized
  by a default-deny ABAC decision keyed on the host-stamped `plugin:<name>`
  subject. Manifest declaration is necessary but not sufficient; an operator
  policy MAY deny a declared capability.
- **INV-PLUGIN-51 (host-vouched dispatch subject).** Any character subject or
  dispatch attribute used in a plugin-mediated authorization decision MUST be
  host-vouched (derived from the host delivery context) and MUST NOT originate
  from plugin- or wire-supplied data. Covers the command-registry RPCs and
  `scope:` anchoring.
- **INV-PLUGIN-52 (scope enforcement is total).** Every scope-eligible
  capability method MUST resolve its scoped resource through a wired extractor;
  a method missing its extractor MUST fail closed (deny), never forward
  unscoped. No silent fail-open.
- **INV-PLUGIN-53 (least-privilege parameters are capability-only).** The
  per-entry least-privilege parameters (`access:`, `scope:`) are valid only on a
  `capability:` `requires` entry; either on a `service:` entry MUST be a hard
  manifest error at load. The host owns neither the provider's method
  classification nor its resource model, so it cannot enforce them on a service.

This sub-spec also **binds** consumption-path entries left `pending` by the
foundation where the enforcement decision now genuinely asserts them
(INV-PLUGIN-44/45 gain policy-decision coverage; final binding list is settled
at implementation against the tests written).

## Testing strategy

- **Manifest validation** (`internal/plugin/manifest_test.go` /
  `dependency_type_test.go`): `access:` enum validation; `scope:` token validated
  against the per-capability vocabulary; `access:` **or** `scope:` on a
  `service:` entry is a hard error (binds INV-PLUGIN-53); generated
  `schemas/plugin.schema.json` regen + drift gate.
- **M1 gate** (`pluginauthz` + interceptor tests): a declared capability denied
  by operator policy is unreachable despite declaration (binds INV-PLUGIN-50); a
  declared+permitted capability is reachable; the same policy denies Lua and
  binary identically (parity).
- **M2 `access:`**: `access: read` permits a read method and denies a write
  method at the broker path; absent `access:` permits both (subject to M1/M3).
- **M3 `scope:`**: `scope: own-location` permits a mutation whose resource
  location equals the dispatch location and denies one that differs; an absent
  `DispatchContext` fails the scoped call closed; the **completeness meta-test**
  fails the build when a scope-eligible method lacks an extractor (binds
  INV-PLUGIN-52); a runtime test asserts fail-closed on a missing extractor.
- **M4 trust fix**: `ListCommands` / `GetCommandHelp` (binary) and
  `list_commands` / `get_command_help` (Lua) derive the subject from the
  host-vouched dispatch context and **ignore** any wire `character_id`; a
  spoofed `character_id` cannot enumerate another character's commands; absent
  dispatch fails closed (binds INV-PLUGIN-51); a cross-runtime test asserts both
  surfaces resolve the identical host-derived subject.
- **Audit**: each gate decision emits exactly one host-stamped audit event
  (reuses INV-PLUGIN-25 via `pluginauthz.Evaluate`).
- Per CLAUDE.md, run `task test:int` after any `plugin.yaml` / `requires`
  fixture change — the unit resolver test stays green while only the integration
  suite exercises injection.

## Alternatives considered

- **Unified reflective broker interceptor (one gate for everything).** Rejected:
  it relocates per-method resource-extraction logic into a reflective broker
  seam plus an out-of-band field-mapping registry that drifts from the handler
  and fails **open** on a missing entry — the worst failure mode for a
  least-privilege feature. The split topology keeps extraction typed and
  co-located with the handler.
- **Two-gate plugin-then-character ceiling** (plugin may do no more than the
  acting character). Rejected as the primary model: too restrictive when a
  plugin legitimately exceeds the character (e.g. writing scene logs the
  character cannot directly write). The plugin-subject-with-dispatch-attributes
  model (P0 + M1) expresses scope without capping the plugin at the character's
  own grants.
- **`scope:` enforced by operator policy only, no manifest keyword (YAGNI).**
  Rejected on **discoverability / least-surprise** grounds: the manifest
  `requires` list (surfaced by `plugin info`) is the plugin's declared,
  auditable blast-radius contract. Pushing the instance ceiling into
  operator-authored policy files removes it from the plugin's own contract,
  where an auditor of a third-party plugin would look. The host owns the scope
  vocabulary, so the keyword is host-definable, host-enforceable, and
  manifest-discoverable — three coincident reasons it belongs on `capability:`
  entries.
- **`scope:` legal on `service:` entries with provider-side enforcement.**
  Rejected: it makes one keyword mean two different strengths
  (host-guaranteed vs advisory) — the privilege gradient the runtime-symmetry
  mandate exists to abolish. Provider-side scope is still fully supported, but
  via the provider's own policies, not a consumer manifest promise.

## Risks

| Risk | Mitigation |
| --- | --- |
| A scope-eligible method ships without its extractor → silent unscoped access. | Three-way fail-closed (runtime deny + CI meta-test + INV-PLUGIN-52). All host servers are in-tree, so the meta-test sees them all. |
| Per-call `Evaluate` on the consumption hot path adds latency. | Capability calls already touch DB/bus (session/property/world); the mandate discounts pre-measured perf. The broker static checks (declaration + `access:`) short-circuit before the policy `Evaluate`. |
| `DispatchContext` absence handling differs between runtimes → asymmetry. | Absence ⇒ fail-closed is enforced at the shared interceptor / shared hostfunc path, not per-runtime; parity tests assert identical denial. |
| Removing `character_id` from the wire breaks an out-of-tree caller. | The authorization path stops reading it regardless; the wire field's final disposition (keep-but-ignore vs remove) is a bounded proto-compat decision at implementation. |
| Operator policy now able to deny a declared capability could brick a plugin silently. | Default policy permits declared capabilities (declaration remains the floor unless an operator writes a deny); denials are audited (INV-PLUGIN-25), so the decision trail is explicit. |

## References

- Epic `holomush-eykuh`; this bead `holomush-eykuh.3`; review follow-on
  `holomush-eykuh.8`; migration `holomush-eykuh.4`.
- Sub-spec 1 (foundation):
  `docs/superpowers/specs/2026-06-11-plugin-capability-dependency-foundation-design.md`
  (INV-PLUGIN-41–45; `Dependency.Scope` reserved).
- Sub-spec 2 (decomposition):
  `docs/superpowers/specs/2026-06-11-plugin-host-capability-decomposition-design.md`
  (host-owned capability vocabulary; the 14 host.v1 services).
- Sub-spec 3 (Lua parity):
  `docs/superpowers/specs/2026-06-12-lua-parity-host-brokered-consumption-design.md`
  (INV-PLUGIN-44/45/49 bound; shared declaration gate).
- Per-action plugin authz core: `internal/plugin/pluginauthz/evaluate.go`
  (INV-PLUGIN-22/24/25/26); host-derived subject ADR `holomush-qeypl`; host
  `Evaluate` RPC ADR `holomush-dttdj`.
- Finding site: `internal/plugin/hostcap/servers.go:947-1013`; Lua twins
  `internal/plugin/hostfunc/commands.go`.
- Manifest schema: `internal/plugin/manifest.go`,
  `internal/plugin/dependency_type.go`, `schemas/plugin.schema.json`,
  `.claude/rules/plugin-manifest.md`.
- Runtime symmetry: `.claude/rules/plugin-runtime-symmetry.md`.
- ABAC providers / fail-safe semantics: `.claude/rules/abac-providers.md`,
  ADR `holomush-iv43`.
<!-- adr-capture: sha256=6ed15cd9ff5f6bf3; session=cli; ts=2026-06-13T01:33:03Z; adrs=holomush-wvrtc,holomush-syhc2,holomush-u1sdq,holomush-afyzh -->
