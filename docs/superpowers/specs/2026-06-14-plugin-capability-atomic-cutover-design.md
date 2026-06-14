<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Sub-spec 5: Plugin-capability atomic cutover + o262d settlement

Design bead: `holomush-eykuh.4` (epic `holomush-eykuh`, theme
`plugin-capability-architecture`).

The keywords **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** in
this document are to be interpreted as described in RFC 2119 and RFC 8174.

## Overview

Sub-specs 1–4 of the plugin-capability epic landed the unified resolver, the
host-capability decomposition, the least-privilege/trust layer, and the Lua
parity transport. They left the host in a deliberate **coexistence** state:

- Legacy `hostfunc.Register` (`internal/plugin/hostfunc/functions.go:271`)
  injects **all** host functions (`kv_*`, `query_*`, `create_*`, session, …)
  unconditionally into **every** Lua plugin.
- The host-brokered consumption path (`luabridge.RegisterHostCaps` /
  `RegisterPluginService` + the `hostcap` servers) is wired in production but
  gated behind an **opt-in allowlist** (`internal/plugin/lua/host.go:92`
  `WithHostCapBridge`, default empty → no plugin uses it).

This sub-spec performs the **atomic cutover**: it removes the allowlist, makes
the declaration-gated host-brokered path the sole capability-consumption path
for both runtimes, retires the legacy capability injection, and settles the
`o262d` loader-policy bug. It is the **last** sub-spec; it depends on 1–4.

The fail-fast loader policy (`INV-PLUGIN-43`) is **already live**
(`internal/plugin/manager.go` `resolveLoadOrder`, ~816-842, returns
`PLUGIN_DEPENDENCY_UNSATISFIED`
fail-closed; the WARN+priority-sort fallback that masked `oeb4d` is gone). This
makes the cutover unforgiving by design: after the flip, any capability a plugin
*uses but does not declare* becomes a hard boot failure, not a silent
degradation. Manifest audit completeness is therefore a hard precondition.

## Requirements

### R1 — Retire capability injection only; keep language stdlib

The cutover MUST strip from `hostfunc.Register` only the **capability** host
functions — those backed by a host-capability service in the vocabulary
(`kv`, `world.query`, `world.mutation`, `property`, `session`, `session.admin`,
`focus`, `eval`, `emit`, `settings`). It MUST retain the **language stdlib** as
unconditional, non-gated runtime:

- `holomush.log`, `holomush.new_request_id`
- `holo.fmt` and the `RegisterStdlib` surface
- `register_emit_type` (top-level emit-type registration, `INV-PLUGIN-32`)
- the **handler return-value emit path** (`event_emitter.go::Emit`) — distinct
  from the `emit` host capability (the `EmitService` brokered RPC). The
  return-value path MUST NOT be gated by a capability declaration.

### R2 — Single declaration-gated brokered path (the flip)

After the cutover:

- The `WithHostCapBridge` allowlist MUST be removed; the brokered path is the
  default for all plugins, gated by declared grants (R3).
- Every capability consumption (binary and Lua) MUST route through the one host
  gRPC broker, authorized as `PluginSubject`, gated by the plugin's declared
  grant set (`INV-PLUGIN-44`, already bound).
- No plugin MAY receive a capability or plugin service it did not declare.

### R3 — Consolidate the least-privilege gate (binds INV-PLUGIN-45)

The declaration gate is currently **split per-runtime**: Lua filters injection
in `RegisterHostCaps(L, conn, name, declaredCaps)`
(`internal/plugin/lua/host.go:460`); binary derives wiring from
`manifest.RequiredServiceNames()` / `RequiredCapabilities()`
(`internal/plugin/goplugin/host.go:806,847`). Both read the same manifest
accessors but enforce in two separate implementations — exactly why
`INV-PLUGIN-45` cannot bind.

The resolver MUST become the single grant authority:

- `ResolveDependencyOrder` MUST emit a structured per-plugin **grant set**
  (`Grants[pluginName]` — the granted capabilities and plugin services),
  computed once from validated declarations against the capability vocabulary
  and the set of providers.
- The Lua shim (`RegisterHostCaps`) and the binary broker-wiring loop MUST both
  consume `Grants[pluginName]` instead of independently re-deriving the declared
  set from the manifest.
- `INV-PLUGIN-45` MUST then be bound to the resolver grant computation: a test
  drives a binary plugin and a Lua plugin through the shared grant and asserts an
  **undeclared** dependency is denied **identically** for both runtimes.

This is the broker/registry common-path gate the `plugin-runtime-symmetry` rule
and the foundation spec mandate. A per-call interceptor is **not** introduced as
a second gate (see R4 for the interceptor's narrow, identity-only role).

### R4 — Actor-identity propagation across the Lua bufconn boundary

The Lua bufconn endpoint (`internal/plugin/lua/bufconn_endpoint.go`) does **not**
propagate `core.ActorFromContext` across the gRPC boundary (lesson
`eykuh.2.11`): `NewInProcessConn` carries metadata but not context *values*, and
ADR `holomush-elqw4` bakes only `pluginName` (not the actor) into the per-plugin
server. Token/identity-requiring caps (`emit`, `settings` GAME-scope, `eval`)
are therefore unreachable through the production Lua bridge — `actorFromToken →
LookupActor → core.ActorFromContext` returns nothing.

Because the cutover is **atomic** (R2) and `core-communication`/`core-objects`
use token-requiring caps, this MUST be fixed as a precondition. A per-plugin
server interceptor MUST stamp `core.WithActor` onto the bufconn server context
from the host-established connection identity, before any Lua plugin opts into a
token-requiring cap. This interceptor's role is **identity only** — it is not a
least-privilege gate (that is the resolver, R3).

### R5 — Backing-server preconditions

The following host-cap servers MUST be implemented/wired before the flip,
because the gated path fails closed (`codes.Unimplemented`) without them:

- **`WorldMutationService` + `WorldQueryService.FindLocation`** in `hostcap`
  (`holomush-eykuh.4.1`). Blocks `core-building` (`create_location`,
  `create_exit`, `find_location`) and `core-objects` (`create_object`,
  `create_location`). Behavior-parity with the legacy hostfunc world-write path
  (ABAC subject `plugin:<name>`); error opacity per `grpc-errors.md`.
- **`SessionAdminService` backing** (broadcast/disconnect) for the Lua bufconn
  path (`holomush-eykuh.4.2`). Blocks `core-communication` (`session.broadcast`
  in `wall`).

### R6 — Naming reconciliation

Legacy capability globals whose token differs in spelling from the brokered
token (e.g. legacy `world_ext` vs brokered `world.query`; lesson `eykuh.2.9`)
MUST be reconciled at cutover so a migrated plugin does not receive **both**
surfaces. The retirement of legacy capability injection (R1) removes the legacy
globals; any alias the host keeps for compatibility MUST point at the brokered
surface, not a parallel one.

### R7 — Manifest audit and migration

Every plugin manifest MUST be audited against actual host-function usage and
updated so its declared `requires` exactly covers the capabilities it consumes.
Audit as of 2026-06-14 (capability tokens from
`internal/plugin/capability_vocab.go`):

| Plugin              | Today                              | Capability calls                                                        | Action                          |
| ------------------- | --------------------------------- | ----------------------------------------------------------------------- | ------------------------------- |
| `core-building`     | **none**                          | `query_location`, `find_location`, `create_location`, `create_exit`     | add `world.query`, `world.mutation` |
| `core-objects`      | `property`, `world.query`         | `query_location`, `query_location_characters`, `create_object`, `create_location` | add `world.mutation`            |
| `core-communication`| `session`                         | `session.find_by_name`, `session.broadcast`                             | add `session.admin`             |
| `core-aliases`      | `service: AliasService` (optional)| none                                                                    | none (verify optional degrade)  |
| `core-help`         | none                              | none                                                                    | none                            |
| `echo-bot`          | none                              | none                                                                    | none (test fixture)             |

Binary plugins (e.g. `core-scenes`) MUST be audited the same way; the audit MUST
be re-derived from code at implementation time, not copied from this table.

### R8 — Settle o262d (always-fatal now, policy seam for later)

The loader MUST keep DAG-resolution failures (`CIRCULAR_DEPENDENCY`,
`DUPLICATE_PLUGIN_NAME`, `DUPLICATE_SERVICE_PROVIDER`, `UNSATISFIED_REQUIRES`,
`UNSATISFIED_DEPENDENCY` non-optional) **unconditionally fatal** at boot
(`INV-PLUGIN-43`). To leave room for a future `gracefulDegradation`-gated
per-plugin quarantine policy (the foundation's "policy-layer flip over the same
result" seam), the fatal decision MUST be factored into an explicit **policy
function over the structured resolver result** — not inlined — so the future
policy is a swap at that one point, not a resolver rewrite.

The settlement MUST:

- Add tests mapping each `ResolveDependencyOrder` error class to its expected
  loader behavior (fatal).
- Document that `gracefulDegradation` currently governs only per-plugin **load**
  failures (`manager.go ~575-585`), **not** DAG resolution, and why.
- Close `holomush-o262d`.

### R9 — Test for bridge-layer error opacity

A `luabridge` test (`holomush-eykuh.4.4`) MUST drive a `codes.Internal` gRPC
status carrying secret inner text through a generated binding /
`newPluginMethodInvoker` and assert the Lua-returned error string equals the
opaque `status.Message()` and does **not** contain the inner detail
(`pushBridgeError`, `internal/plugin/luabridge/marshal.go:314-323`). This is
independent and MAY land at
any point in the sub-spec.

## Non-goals

- **Re-opening any sub-spec 1–4 invariant.** `INV-PLUGIN-41/42/43/44` are bound;
  this sub-spec only adds `INV-PLUGIN-45`.
- **Implementing the `gracefulDegradation`-gated quarantine policy.** R8 only
  reserves the seam; the policy itself is future work.
- **In-process gRPC performance tuning** for Lua hot paths (a sub-spec 3
  concern, behind the invariant).
- **External/clustered NATS or any non-plugin subsystem.**

## Invariants

| ID             | Status after this sub-spec | Bound by                                                              |
| -------------- | -------------------------- | -------------------------------------------------------------------- |
| `INV-PLUGIN-45`| `pending` → `bound`        | cross-runtime shared-grant undeclared-dependency denial test (R3)    |

`INV-PLUGIN-41/42/43/44` remain `bound` (unchanged). No new invariant is minted;
`INV-PLUGIN-45` already exists in the registry as `pending`.

## Testing strategy

- **Resolver grant unit tests** (`internal/plugin/dependency_test.go`): the
  structured `Grants` shape; a declared cap/service appears in the grant, an
  undeclared one does not; misdeclared kind still errors (`INV-PLUGIN-42`).
- **o262d error-class table** (`internal/plugin/manager_test.go`): each
  `ResolveDependencyOrder` error class → fatal boot; `gracefulDegradation`
  interaction documented and asserted not to apply to DAG resolution.
- **INV-PLUGIN-45 cross-runtime denial** (integration): one binary + one Lua
  plugin through the shared resolver grant; an undeclared dependency is denied
  identically. Carries `// Verifies: INV-PLUGIN-45`.
- **Actor-identity parity** (R4): a token-requiring cap (e.g. `emit`) succeeds
  through the production Lua bridge once the interceptor stamps the actor;
  extends the `eykuh.2.11` parity test beyond the token-free `kv` case.
- **Bridge opacity** (R9): the `pushBridgeError` strip test.
- **Whole-system load** (`test/integration/wholesystem/`): all migrated
  manifests resolve with no `Unsatisfied` and no fallback; every in-tree plugin
  loads and registers its commands through the brokered path.

## Risks

- **Audit completeness is the dominant risk.** Fail-fast turns any missed
  declaration into a hard boot failure. Mitigation: R7 audit re-derived from code
  - the whole-system load test as the gate.
- **Actor-identity is new surface on the critical path.** A subtle stamping bug
  fails token-requiring caps closed (loud) rather than open — acceptable
  direction, but it blocks `core-communication`/`core-objects` until correct.
- **Return-value emit vs `emit` cap confusion.** Gating the wrong one breaks all
  event emission. R1 fixes the boundary explicitly; tests MUST cover both paths.

## Sequencing

The atomic flip (R2) is the **last** change. Order:

1. R5 backing servers (`eykuh.4.1`, `eykuh.4.2`).
2. R4 actor-identity interceptor (new child `eykuh.4.5` — MUST be created
   during `plan-to-beads` materialization; it does not exist yet).
3. R3 resolver grant set + both shims consuming it.
4. R7 manifest audit + declarations.
5. R8 o262d policy-function refactor + tests; close `o262d`.
6. **R2 atomic flip**: remove allowlist, strip legacy capability injection,
   reconcile names (R6).
7. R3 bind `INV-PLUGIN-45`; R9 opacity test (independent).

## References

- Foundation: `docs/superpowers/specs/2026-06-11-plugin-capability-dependency-foundation-design.md`
- Host-cap decomposition: `docs/superpowers/specs/2026-06-11-plugin-host-capability-decomposition-design.md`
- Least-privilege/trust: `docs/superpowers/specs/2026-06-12-plugin-least-privilege-trust-design.md`
- Declaration enforcement: `docs/superpowers/specs/2026-06-13-plugin-capability-declaration-enforcement-design.md`
- Rules: `.claude/rules/plugin-runtime-symmetry.md`, `.claude/rules/plugin-manifest.md`,
  `.claude/rules/grpc-errors.md`, `.claude/rules/invariants.md`
- `o262d` bug; lessons `eykuh.2.9`, `eykuh.2.10`, `eykuh.2.11`
<!-- adr-capture: sha256=7518d899f25996c0; session=cli; ts=2026-06-14T14:12:13Z; adrs=holomush-40ssh,holomush-vpg8l,holomush-ptf7b,holomush-05f3v -->
