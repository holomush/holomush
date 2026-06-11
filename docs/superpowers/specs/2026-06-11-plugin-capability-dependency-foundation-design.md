<!--
SPDX-License-Identifier: Apache-2.0
Copyright 2026 HoloMUSH Contributors
-->

# Plugin Capability & Dependency Foundation — Design

**Bead:** `holomush-oeb4d` (foundation sub-spec of epic `holomush-eykuh`)
**Theme:** `theme:plugin-capability-architecture` (`docs/roadmap.md`)
**Status:** draft for design review
**Date:** 2026-06-11

## Overview

This is the **foundation** sub-spec of a do-it-right redesign of the plugin
capability & dependency model. It establishes the declaration vocabulary, the
unified resolver, and the parity / least-privilege / fail-fast **invariants**
that the remaining sub-specs build on. It deliberately does **not** define the
full capability taxonomy, decompose `PluginHostService`, specify the Lua
transport wiring, or migrate the manifests — those are sub-specs 2–5
(`holomush-eykuh.1`–`.4`).

It was triggered by a small bug (`holomush-oeb4d`): `core-aliases` declares
`requires: holomush.alias.v1.AliasService`, which nothing provides, so the DAG
dependency resolver fails `UNSATISFIED_REQUIRES` and the loader silently falls
back to a global priority sort for the **entire** plugin set on every boot
(`internal/plugin/manager.go:809-830`). Root-causing that bug surfaced a deeper
architectural conflation, captured below.

## RFC2119 Keywords

The keywords **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** in
this document are to be interpreted as described in RFC 2119 and RFC 8174.

## Goals

- Define a single manifest vocabulary for everything a plugin depends on: host
  capabilities **and** other plugins' services.
- Make the dependency resolver validate **and** order the unified graph, with a
  structured result and a fail-fast default policy.
- Establish the runtime-parity, least-privilege, and fail-fast **invariants** as
  registry entries (`INV-PLUGIN-41`…`45`).
- Fix the boot bug (`holomush-oeb4d`) as a byproduct of the corrected model.

## Non-Goals (deferred to later sub-specs)

| Deferred concern | Sub-spec / bead |
| --- | --- |
| The concrete capability taxonomy + `PluginHostService` decomposition into capability-scoped proto contracts | 2 — `holomush-eykuh.1` |
| The Lua transport **wiring** (in-process gRPC over bufconn/loopback, hot-path optimization) | 3 — `holomush-eykuh.2` |
| Least-privilege **enforcement** mechanics + ABAC policy on capability/service access | 4 — `holomush-eykuh.3` |
| The **full** least-privilege migration: gate Lua injection + declare capabilities for plugins that consume host functions without declaring them today (e.g. `core-building`) | 5 — `holomush-eykuh.4` |

The foundation **does** reclassify the four existing phantom `requires` (§4) —
that minimal manifest edit is in scope here because fail-fast (`INV-PLUGIN-43`)
cannot land safely while those four entries remain unsatisfiable. Sub-spec 5 is
the *remaining* migration (injection gating + full declaration audit).

This spec settles the **model and invariants**; the sub-specs cannot reopen
them, only implement them.

## Background — current state (grounded)

Three accreted problems, all confirmed against `main`:

1. **The `requires` field is overloaded.** It drives both DAG load-order
   resolution (`internal/plugin/dependency.go:30` `ResolveDependencyOrder`,
   satisfied only by host `serverServices` + plugin `Provides`) **and** Lua
   capability injection (`internal/plugin/lua/host.go:344`
   `h.hostFuncs.Register(L, name, requires...)` in `DeliverEvent`, and the
   identical call in `DeliverCommand` — both gate on `requires` →
   `internal/plugin/hostfunc/capability.go:42` `InjectRequired`). The resolver
   is blind to the capability registry.

2. **Four `requires` name services that exist in no registry.** On `main`, the
   service registry registers only `holomush.world.v1.WorldService`
   (`internal/plugin/setup/subsystem.go:238`). `core-aliases`→`AliasService`,
   `core-communication`→`SessionService`, and `core-objects`→`PropertyService`
   - `WorldQueryService` are unsatisfiable by the resolver; it returns on the
   **first** miss, so the boot WARN names only one — masking the breadth. None
   of those four proto services exist under `api/proto/`.

3. **Capability delivery is asymmetric and not least-privilege.** Lua plugins
   receive most host functions **unconditionally**: `query_*`, `set_property`,
   `create_*` are set on the per-VM `mod` table
   (`internal/plugin/hostfunc/functions.go:253-265`) for every plugin **before**
   the `requires`-gated `InjectRequired` guard (`functions.go:308`), so they
   exist in every VM regardless of declarations (only the `capability.go`
   registry path is `requires`-gated, and it is inert for the default set).
   Binary plugins, by contrast, consume host capabilities as gRPC services via
   the grpcbroker
   (`internal/plugin/goplugin/host.go:658-705`, design
   `docs/superpowers/specs/2026-04-06-grpcbroker-service-injection-design.md`).
   `PluginHostService` is a 25-RPC god-service
   (`api/proto/holomush/plugin/v1/plugin.proto`), so service-granularity
   declarations cannot express least privilege. There is **no `plugin → host →
   plugin` path for Lua** at all: binary plugins reach another plugin's provided
   service through the broker; Lua plugins have no mechanism to call a
   plugin-provided service.

The binary half of the target model already exists and is **reused, not
redesigned**: the grpcbroker authenticates each plugin over mTLS by its
per-plugin cert CN (the plugin name; `tlscerts.GenerateServerCert`) and
guarantees providers load before consumers
(`2026-04-06-grpcbroker-service-injection-design.md` §1.2, §2.2). That transport
identity is the same string value as the plugin's ABAC `PluginSubject`
(`internal/access`-side, e.g. `pluginauthz/evaluate.go:81`); the two are
distinct concepts (transport CN vs. authorization subject) that happen to share
the plugin name. INV-PLUGIN-44's "authorized as `PluginSubject`" refers to the
ABAC subject, which the Lua in-process proxy MUST stamp identically.

## Design

### §1 — Manifest model: typed dependency entries

A plugin declares dependencies as a single `requires:` list of **typed
entries**, each carrying a kind tag plus optional attributes. `provides:`
(services this plugin implements) is unchanged.

```yaml
requires:
  - capability: world.query                       # host capability (short vocab)
  - capability: world.mutation
    scope: own-location                           # least-privilege parameter (future)
  - capability: kv
  - service: holomush.scene.v1.SceneService       # another plugin's service (proto path)
    version: ">=1.0.0"                            # version constraint
    optional: true                                # per-dependency graceful-degrade
provides:
  - holomush.scene.v1.SceneService
```

Rules:

- An entry MUST be exactly one of `capability:` or `service:`.
- `capability:` values are drawn from a **controlled short vocabulary**
  (`world.query`, `kv`, `session`, …) — host-provided, decoupled from proto
  naming. The vocabulary itself is defined in sub-spec 2; the foundation only
  requires that it **exists and is validated**.
- `service:` values are **full proto service paths** — a contract provided by
  some plugin (or the host).
- Each entry MAY carry attributes: `version` (semver constraint, services
  only), `optional` (boolean, default `false`), and capability-scoping
  parameters (e.g. `scope`, `access`) whose semantics are defined in sub-spec 4.
- The pre-existing `dependencies:` version-constraint map is **folded into**
  `requires` `service` entries' `version` attribute — one place to declare
  every dependency.

**Why typed entries (not two flat fields, not implicit-kind):** the object form
is the only one that carries per-entry attributes, which least-privilege
(`scope`/`access`), versioning, and optionality all require; choosing flat
strings now would force a manifest-wide migration to objects later. The explicit
kind tag is an author **assertion** the resolver **validates** (declare
`capability: X` but `X` is plugin-provided → hard error, never a silent
reclassification).

### §2 — Unified resolver

`ResolveDependencyOrder` is generalized to validate and order the unified graph,
and to return a **structured result** rather than `(order, error)`:

```text
ResolveResult {
    Ordered     []*DiscoveredPlugin   // topological load order
    Unsatisfied []UnsatisfiedDep      // {plugin, entry, reason}
    Cycles      [][]string            // plugin-name cycles, if any
}
```

Resolution:

1. **Provider set** = `{registered host capabilities}` (the controlled
   vocabulary, no DAG edge — they are always host-available) ∪ `{plugin
   provides}` (each creates a provider→consumer edge).
2. **Per-entry validation:**
   - `capability: X` — `X` MUST be a registered host capability. If `X` is
     instead plugin-provided, that is a `MISDECLARED_DEPENDENCY` error.
   - `service: Y` — `Y` MUST be provided by some plugin/host; the `version`
     constraint MUST be satisfied; an edge is added.
   - A non-optional entry that is unsatisfiable is recorded in `Unsatisfied`.
3. **Ordering:** Kahn's algorithm over service edges only (capabilities add
   none). A cycle is recorded in `Cycles`.
4. **Policy (loader, not resolver):** the default policy turns any non-empty
   `Unsatisfied` (excluding `optional` entries, which are simply skipped) or
   `Cycles` into a **fatal boot error** — fail-fast, fail-closed, mirroring the
   crypto KEK-mandatory-boot posture (`INV-CRYPTO-119`). Because the resolver
   returns structured data, a future **per-plugin quarantine** policy is a
   policy-layer flip over the same result — not a resolver rewrite.

This deletes the error-masking that hid the bug (`o262d`): the loader MUST NOT
silently downgrade an unsatisfied non-optional dependency to a WARN + priority
sort.

### §3 — The parity / consumption contract

The core symmetry invariant the foundation establishes:

> For any declared dependency `D` (a capability **or** a plugin service), both
> the binary and Lua runtimes obtain access to `D` through the **one host gRPC
> broker**, **gated by the declaration** (a plugin gets only what it declared —
> least-privilege), and **authorized as `PluginSubject`**. The delivery shim
> differs by runtime — binary receives a grpcbroker `ClientConn`, Lua receives
> an injected in-process gRPC proxy — but the **contract, gating, and
> authorization are identical**.

This commits the foundation to **one brokered gRPC consumption path for both
runtimes**, covering host capabilities and plugin services alike, with the
least-privilege gate at the **single broker/registry common path**.

**Why the Lua transport is forced to in-process gRPC (not a Go shim), at the
foundation level:**

- The `plugin → host → plugin` mandate requires a Lua plugin to reach another
  plugin's gRPC service. No host Go shim can exist for an arbitrary plugin's
  service, so Lua MUST have a brokered gRPC consumption path. Once it exists for
  plugin services, host capabilities use the same path.
- It makes "every capability is a gRPC contract" the **real** consumption path
  for both runtimes, not a nominal parallel definition.
- It yields a **single** least-privilege gate at the broker/registry common
  path, satisfying the `plugin-runtime-symmetry` rule's "gate at the common
  path." A Go-shim transport would split the gate (broker for binary, injection
  for Lua) — the drift hazard that rule exists to prevent.

The unconditional Lua injection
(`internal/plugin/hostfunc/functions.go:253-265`) is replaced by
declaration-gated consumption. This is the breaking change that the manifest
migration (sub-spec 5) exists to absorb — e.g. `core-building` declares zero
`requires` today yet calls `create_location`/`query_location`, so it MUST gain
explicit capability declarations.

**Deferred to sub-spec 3 (cannot reopen this invariant):** only the in-process
gRPC transport **wiring and performance** — bufconn/loopback, batching, whether
hot paths get a fast lane behind the contract.

### §4 — The boot bug as a byproduct

The foundation reclassifies the four existing phantom `requires` so the resolver
no longer fails `UNSATISFIED_REQUIRES`:

| Plugin | Today (phantom `service`) | Foundation |
| --- | --- | --- |
| `core-communication` | `holomush.session.v1.SessionService` | `capability: session` |
| `core-objects` | `holomush.property.v1.PropertyService` | `capability: property` |
| `core-objects` | `holomush.world.v1.WorldQueryService` | `capability: world.query` |
| `core-aliases` | `holomush.alias.v1.AliasService` | **dropped** — aliases are delivered at the command layer (`command.AliasCache`), never wired as a capability in production; `core-aliases` has no capability dependency |

The foundation registers the **minimal** vocabulary these reclassifications need
(`session`, `property`, `world.query`); the full taxonomy + contracts are
sub-spec 2. With these edits the DAG order is honored and the every-boot WARN +
fallback disappear — the boot bug is fixed end-to-end, and fail-fast
(`INV-PLUGIN-43`) is safe to enable.

## Invariants (proposed)

To be registered in `docs/architecture/invariants.yaml` on spec finalization
(per `.claude/rules/invariants.md`). Each will be `binding: pending` until a
test asserts it.

- **INV-PLUGIN-41 (unified dependency graph).** The plugin dependency resolver
  MUST validate and order a single graph spanning host capabilities (satisfied
  without an ordering edge) and plugin-provided services (provider-before-
  consumer edge). A declared dependency unsatisfiable by either provider source
  MUST be reported, never silently dropped or reclassified.
- **INV-PLUGIN-42 (declaration kind is validated).** A `capability:` entry MUST
  resolve to a registered host capability and a `service:` entry to a provided
  proto service; a kind/provider mismatch MUST be a hard `MISDECLARED_DEPENDENCY`
  error, not a silent reclassification.
- **INV-PLUGIN-43 (fail-fast, fail-closed).** An unsatisfied non-optional
  dependency or a dependency cycle MUST fail the boot; the loader MUST NOT
  downgrade it to a WARN + priority-sort fallback. `optional: true` entries MAY
  be skipped.
- **INV-PLUGIN-44 (runtime-parity consumption).** Binary and Lua plugins MUST
  obtain every declared dependency through the one host gRPC broker, gated by
  the declaration and authorized as `PluginSubject`; neither runtime MAY receive
  an undeclared capability or service.
- **INV-PLUGIN-45 (single least-privilege gate).** The declaration gate that
  enforces least privilege MUST live at the broker/registry common path shared
  by both runtimes; per-runtime gating that could diverge is forbidden.

INV-PLUGIN-44/45 do **not** duplicate `INV-CRYPTO-34` (runtime symmetry for the
emit/fence/audit capability path): they govern the dependency-**consumption**
path (broker access to declared capabilities/services), a distinct surface.
Both are facets of the broader `plugin-runtime-symmetry` mandate.

## Testing strategy

- **Resolver unit tests** (`internal/plugin/dependency_test.go`): typed-entry
  validation (capability vs service), `MISDECLARED_DEPENDENCY`, version
  constraints, `optional` skip, structured-result shape, cycle reporting.
- **Default-set regression** (the original `oeb4d` acceptance): load the real
  `plugins/*` manifests and assert resolution completes with no `Unsatisfied`
  and no fallback — the test that pins INV-PLUGIN-41/43.
- **Parity tests** land with sub-spec 3 (consumption path) — the foundation
  registers INV-PLUGIN-44/45 as `pending` and the binding follows the
  implementation.

## Risks

- **Atomic cutover.** Declaration-gated consumption breaks any plugin using an
  undeclared host function; every manifest MUST be audited and updated in
  lockstep with the enforcement flip (sub-spec 5). The foundation's fail-fast
  default makes such a gap loud rather than silent — by design.
- **Capability-vocabulary governance.** A controlled vocabulary needs an owner
  and a validation point (sub-spec 2). Until it exists, the resolver cannot
  distinguish a typo from an unregistered capability.
- **In-process gRPC overhead** for Lua hot paths (`query_*` per delivery) — a
  sub-spec 3 performance concern, behind the invariant.

## References

- Bug + grounding trail: `holomush-oeb4d` (`bd show`).
- grpcbroker service injection (binary half, reused):
  `docs/superpowers/specs/2026-04-06-grpcbroker-service-injection-design.md`.
- Plugin-first command architecture:
  `docs/specs/2026-03-28-plugin-first-command-architecture-design.md`.
- Runtime symmetry rule: `.claude/rules/plugin-runtime-symmetry.md`.
- Resolver: `internal/plugin/dependency.go`, `internal/plugin/manager.go:806-830`.
- Capability injection: `internal/plugin/hostfunc/functions.go:228-308`,
  `internal/plugin/hostfunc/capability.go`.
<!-- adr-capture: sha256=fc0e5fe059a6762f; session=cli; ts=2026-06-11T15:44:43Z; adrs=holomush-mg4x6,holomush-dfkca,holomush-gtkzy -->
