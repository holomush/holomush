<!--
SPDX-License-Identifier: Apache-2.0
Copyright 2026 HoloMUSH Contributors
-->

# Lua Parity Layer — Host-Brokered Consumption of Capabilities and Plugin Services — Design

> Sub-spec 3 of epic `holomush-eykuh` (Plugin capability & dependency architecture).
> Bead: `holomush-eykuh.2`. Depends on sub-spec 1 (`holomush-oeb4d`, foundation —
> typed deps + unified resolver) and sub-spec 2 (`holomush-eykuh.1`, the
> `holomush.plugin.host.v1` capability decomposition).

## Overview

Binary plugins consume **both** host capabilities and other plugins' services through
**one** mechanism: the hashicorp/go-plugin grpcbroker. For each declared dependency the
host resolves a provider and serves it on a broker channel the plugin dials back over
real gRPC (`internal/plugin/goplugin/host.go`, the broker-proxy loop ~666–694). Host
capabilities (the
`holomush.plugin.host.v1` services landed in sub-spec 2) and plugin-provided services are
uniform — both are just broker IDs.

Lua plugins have **no such uniform mechanism**. Host capabilities reach Lua as
hand-written Go shims (`internal/plugin/hostfunc/cap_*.go`) injected as Lua globals, and
**Lua has no way at all to consume a plugin-provided service**. Worse, each shim is a
*parallel, divergent* contract: `cap_session.go` defines its own `SessionInfo` struct, its
own narrow `SessionAccess` interface, and Lua names (`session.find_by_name`) that do not
correspond to any `host.v1` RPC. That is precisely the runtime-specific capability surface
INV-PLUGIN-49 forbids.

This sub-spec gives Lua **one host-brokered consumption mechanism** for both host
capabilities and plugin services, built on the same `host.v1` contracts and the same
`BrokerProxy` binary already uses — so the proto contract becomes the single source both
runtimes consume. It **binds INV-PLUGIN-44, INV-PLUGIN-45, and INV-PLUGIN-49** and closes
bug `holomush-eykuh.6`.

It does **not** migrate existing Lua plugins onto the new path or delete the legacy shims —
that is sub-spec 5 (`holomush-eykuh.4`). The two sub-specs meet at a clean seam: **3 builds
and proves the mechanism; 5 migrates production plugins onto it and removes the old
surface.**

## RFC2119 Keywords

The keywords **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are to be
interpreted as described in RFC 2119 and RFC 8174 (see the root `CLAUDE.md` table).

## Goals

1. Lua plugins **MUST** consume host capabilities (`holomush.plugin.host.v1`) through the
   same gRPC contracts binary consumes — not a parallel hand-written surface.
2. The `host.v1` server impls **MUST** be made runtime-neutral (§0): relocated to
   `internal/plugin/hostcap/` behind a narrow `HostCapabilities` port so a single handler
   body backs both runtimes (INV-PLUGIN-49 at the server level, not just the contract). The
   three impls Lua is the first consumer of — Session, Property, World (proto contracts from
   sub-spec 2, **no server impl yet**; see Background) — **MUST** be implemented here,
   wrapping the same host-capability logic the legacy
   `cap_session.go`/`cap_property.go`/`cap_world_query.go` shims call today.
3. Lua plugins **MUST** be able to consume a plugin-provided service (the plugin→host→plugin
   direction that has no Lua path today), through the same `BrokerProxy` binary uses.
4. Plugin identity for both paths **MUST** be host-established and unforgeable — never
   wire-supplied — preserving INV-PLUGIN-22 / the `PluginSubject` ABAC contract.
5. The declaration gate (least privilege) **MUST** live at the shared broker/registry path,
   not in per-runtime code (INV-PLUGIN-45).
6. The work **MUST** bind INV-PLUGIN-44, INV-PLUGIN-45, INV-PLUGIN-49 with genuine
   cross-runtime assertions, closing `holomush-eykuh.6`.
7. Plugin-author developer experience **MUST NOT** regress: Lua's "drop a script, no build
   step" value proposition is preserved, and the call surface carries no magic strings.

## Non-Goals (deferred)

| Deferred | Owner |
| --- | --- |
| Migrating existing Lua plugins onto the new path; deleting `cap_*.go`; gating the unconditional injection | Sub-spec 5 (`holomush-eykuh.4`) |
| Adding `requires` declarations to plugins that consume host functions without declaring them today | Sub-spec 5 |
| Generated `.lua` doc/type-stub files for editor/LSP discoverability | `holomush-eykuh.9` (dev-ex follow-on; plugin-SDK scope) |
| Lua **consuming a server-streaming** plugin RPC | Out of scope — no current consumer; all `host.v1` is unary; `BrokerProxy` already proxies streams generically when one is needed |
| Least-privilege *enforcement hardening* and plugin-trust security | Sub-spec 4 (`holomush-eykuh.3`) |

## Background — current state (grounded)

### Binary consumption — one broker mechanism

`goplugin/host.go:686` serves the host-capability server (`newPluginHostServiceServer`)
and, for each `manifest.RequiredServiceNames()`, resolves the provider from `h.registry`,
wraps it in a `BrokerProxy`, and serves it on a fresh broker ID. The plugin receives
`requiredServices[svcName] = "broker:N"` in its `InitRequest` and dials back. Host caps and
plugin services are the same shape.

`BrokerProxy` (`goplugin/broker_proxy.go`) is a **generic, codegen-free** forwarder: a
`grpc.UnknownServiceHandler` + `ProxyStreams` that forwards any method by name to the
provider's `ClientConn`. It already handles unary and streaming transparently.

`newPluginHostServiceServer(host, pluginName)` (`goplugin/host_service.go`) builds a
**dedicated `*grpc.Server` per binary plugin**, registering every `host.v1` capability
server with `hostCapabilityBase{host, pluginName}` baked in. **Identity is which server
instance the plugin dials — host-wired per plugin, never read from a request.**

### Lua consumption — divergent shims, no plugin-service path

`hostfunc/capability.go` `InjectRequired(L, requires, pluginName)` registers `Capability`
modules (`cap_session.go`, `cap_property.go`, …) as Lua globals. Each is a hand-written Go
translation against a bespoke struct/interface, not the `host.v1` proto. The ABAC subject
is derived host-side from the captured `pluginName` (`adapter.go:68`
`access.PluginSubject(a.pluginName)`) — correct and unforgeable, but a *second* contract
parallel to the proto. There is **no Lua mechanism to call a plugin-provided service.**

### What sub-spec 2 already locked

The `holomush.plugin.host.v1` package exists: **11 proto files declaring 13 services**
(`session.proto` → `SessionService` + `SessionAdminService`; `stream.proto` →
`StreamHistoryService` + `StreamSubscriptionService`; the other 9 files one service each:
`property`, `world`, `emit`, `eval`, `focus`, `kv`, `audit`, `settings`,
`command_registry`). **Every RPC is unary** (verified: zero `rpc … stream`;
`QueryStreamHistory` returns a batched response; `AddSessionStream`/`RemoveSessionStream`
are unary subscription-control).

**Not every service has a Go server impl yet.** `host_capability_servers.go` implements
**9** server types (`focusServer`, `emitServer`, `evalServer`, `settingsServer`,
`streamHistoryServer`, `streamSubscriptionServer`, `auditServer`, `commandRegistryServer`,
`kvServer`), and `newPluginHostServiceServer` (`host_service.go:31`) registers exactly
those 9 — with an explicit comment that **Session, Property, and World are deliberately NOT
registered because they have no binary consumer in sub-spec 2.** Lua *is* their consumer
(today via `cap_session.go`/`cap_property.go`/`cap_world_query.go`), so **implementing the
`sessionServer`/`propertyServer`/`worldServer` types is this sub-spec's job** (see Goal 2,
§0, §2). They wrap the same host capability logic the legacy shims call. All 12 server
impls (the 9 existing + these 3) relocate to the runtime-neutral `internal/plugin/hostcap/`
package behind the `HostCapabilities` port (§0).

The existing servers derive the ABAC subject + emit actor-vouching (the `holomush-ec22.1`
token gate) from `hostCapabilityBase.pluginName`; the new three MUST do the same.

## Design

The mechanism is a foundational refactor (§0) plus three components and a codegen step.
§0 makes single-source true at the *server* level; components 1 and 3 mirror binary
exactly; component 2 is the only new runtime machinery.

### §0 — Runtime-neutral host-capability servers (the `HostCapabilities` port)

INV-PLUGIN-49 requires both runtimes to consume the **same server implementations**, not
merely the same proto contract. Today they cannot: the 9 `host.v1` server impls in
`internal/plugin/goplugin/host_capability_servers.go` embed
`hostCapabilityBase{ host *goplugin.Host }` — welded to the concrete binary-plugin `Host`
(reaching `host.engine`, `host.tokenStore`, `host.auditor`, `host.sessionAccess`, the
settings stores, focus coordinator, history reader, …). The Lua runtime does **not** import
`goplugin` (clean sibling layering) and holds a *parallel* backing on `hostfunc.Functions`
(`engine`, `auditor`, `sessionAccess`, `propertyRegistry`, `worldMutator`, `kvStore`,
`settingsOps`, `historyReader`, …) — the same logical dependencies held twice. A second
server impl wrapping `hostfunc.Functions` is exactly the runtime-specific surface
INV-PLUGIN-49 forbids; having `lua` import `goplugin` is the wrong dependency direction.

The resolution is **ports & adapters**:

1. Define a narrow `HostCapabilities` port — the set of host operations the 9 servers (plus
   the 3 new ones in §2) actually call (access engine, auditor, dispatch-token store,
   session access, property registry, world query/mutator, settings stores, focus
   coordinator, history reader, stream registry, command querier, KV store, event emitter).
   The interface MUST be **method-narrow** (only what the servers call), not a mirror of the
   whole `goplugin.Host` struct.
2. Move the 9 server impls + `hostCapabilityBase` into a **runtime-neutral package**,
   `internal/plugin/hostcap/`, depending only on the port (not on `goplugin`).
3. Provide two adapters: `goplugin.Host` already exposes the needed operations (a thin
   adapter or direct interface-satisfaction), and a **new `hostfunc.Functions`-backed
   adapter** for the Lua runtime. Both construct the same `hostcap` servers.

Result: **one** set of `host.v1` server impls, two thin adapters; INV-PLUGIN-49 holds at the
server level by construction. The dispatch-token concern stays adapter-specific — the binary
adapter wires the `emitTokenStore` forgery defense; the Lua adapter supplies the
connection-scoped identity equivalent (the token surface exists only where a forgery surface
does; §4 / `.claude/rules/plugin-runtime-symmetry.md`).

This refactor is **behavior-preserving for binary**: the binary path keeps the same servers
reaching the same host operations, now through the port. It is verified by the existing
`goplugin` host-capability tests continuing to pass unchanged (plus a relocation smoke test).

### §1 — Per-plugin bufconn endpoint (mirror of binary)

For each Lua plugin VM, the host stands up a per-plugin `*grpc.Server` registering the
`host.v1` capability servers, wrapped on an in-memory bufconn via the **existing**
`internal/plugin.NewInProcessConn(srv)` helper (`internal/plugin/inprocess_conn.go` — it
already serves any `*grpc.Server` on a bufconn and returns a `grpc.ClientConnInterface`;
the spec reuses it rather than building parallel bufconn infrastructure). The plugin's Lua
code is handed only the resulting in-process `ClientConn`.

The server registers the **`hostcap` servers** (§0), constructed with the Lua adapter as
their `HostCapabilities` port and `pluginName` baked into `hostCapabilityBase`. Its
**registration set differs** from binary's: binary's omits Session/Property/World (no binary
consumer); the Lua server registers the capabilities a Lua plugin actually consumes,
**including the `sessionServer`/`propertyServer`/`worldServer` added in §2**. The handler
bodies are the single-source `hostcap` impls; only the registration set and the adapter are
per-runtime. (Whether registration becomes manifest-gated for the *binary* server too is a
§4 / sub-spec 4 concern, not changed here.) A shared
`hostcap.RegisterCapabilities(server, base, set)` helper factors the common registration so
the two runtimes cannot drift; `goplugin`'s `newPluginHostServiceServer` is refactored to
call it. Final file layout is a plan-stage decision within `internal/plugin/hostcap/`.

- Identity is **connection-scoped and host-established at wiring time**. The plugin cannot
  enumerate or forge another plugin's connection (in-process; it holds only its own
  `ClientConn`) — a stronger boundary than binary's integer broker IDs.
- Because Lua now lands on the **same** `hostCapabilityBase` servers, the ABAC subject
  (`access.PluginSubject(name)`) and emit actor-vouching are inherited with **no new
  subject/token code**.
- `host.v1` services that have no consumer in this sub-spec remain registered as today;
  registering them is harmless and keeps the per-plugin server identical to binary's.

The bufconn `Listener` + `*grpc.Server` per plugin is the deliberate choice over a single
shared server, precisely because a shared listener could not bind per-plugin identity
host-side without a forgeable metadata header. Per-plugin servers are cheap and reproduce
binary's identity model verbatim. If a profile ever shows the in-memory pipe copy matters,
bufconn is replaceable by a custom in-memory `grpc.ClientConnInterface` behind the same
stubs — a transparent later optimization, out of scope here.

### §2 — The Lua↔proto bridge (the only new machinery)

The bridge turns a Lua call into a proto request, invokes the unary RPC over the per-plugin
`ClientConn`, and turns the proto response back into Lua values. It is **unary-only**
(all `host.v1` is unary). It splits by **proto ownership**:

**Host capabilities — build-time codegen, strongly typed.** A `go:generate` generator reads
the `host.v1` service descriptors and emits typed Go marshalers: each constructs the
concrete `*hostv1.<Method>Request` field-by-field (no map keys, no magic strings) and calls
the already-generated typed client stub (`hostv1.New<Svc>ServiceClient(conn).<Method>`),
then marshals the typed response into a Lua table. The emitted code registers
`namespace.Method{…}`-shaped Lua tables (e.g. `session.FindByName{…}`). Strong typing on the
Go side, drift-proof (regenerated, never hand-synced), and genuinely the **single source**:
binary and Lua both consume the same generated stubs from the same descriptors.

**Plugin services — load-time descriptor-driven, typed-shaped surface.** A provider's proto
is third-party and not imported by the host, so there is no Go struct to marshal into and
(Lua being non-compiled) no consumer-side type enforcement to gain. The host **does** hold
the provider's `FileDescriptor` at load time (the provider must register it for the broker
to route). When a Lua plugin declares `requires: service: <Name>` and the provider is
resolved, the bridge builds `namespace.Method{…}`-shaped Lua tables for that service from
the descriptors, marshaling Lua tables ↔ `dynamicpb.Message`. Call sites carry **no magic
strings**; the reflective marshaling is invisible to the author. Binary consumers keep full
static typing (they import the provider's `.pb.go`); this is the achievable ceiling for a
non-compiled consumer of a contract it does not own.

Both surfaces present the **identical** `namespace.Method{…}` ergonomic shape, so a plugin
author cannot tell which is codegen'd and which is descriptor-driven — uniform dev-ex, no
build step on the Lua side.

**Error mapping.** gRPC `status` errors map to the established Lua convention
(`hostfunc/helpers.go` `pushError` → `nil, "<message>"`, the 2-return pattern). Internal
error text is not leaked past the boundary (per `.claude/rules/grpc-errors.md`); the bridge
surfaces the status code/message the host servers already shape.

**Load-time validation (fail early).** Tables are built (and validated against descriptors)
at load. An unknown method or a malformed/unsatisfiable `requires: service` fails at **load
time**, not at first call — recovering most of static typing's safety at the boundary that
matters. This is consistent with the foundation's fail-fast resolver (`holomush-oeb4d`).

### §3 — Plugin→plugin transport (reuse `BrokerProxy`)

A Lua plugin's call to a required plugin service dials a loopback `ClientConn` whose server
end is the **same** `BrokerProxy` binary uses to reach that provider. One generic
byte-forwarder, both runtimes, both directions. No new proxy code; the §2 plugin-service
binding produces the request bytes, `BrokerProxy` forwards them to the provider.

### §4 — The declaration gate (shared path, least privilege)

The gate that decides whether a plugin may reach a capability or service is the **resolver +
registry** the foundation already established (`ResolveDependencyOrder`,
`manifest.RequiredServiceNames()` / `RequiredCapabilities()`), shared by both runtimes
(INV-PLUGIN-45). Lua's per-plugin server (§1) registers/serves **only** what the manifest
declares and the resolver satisfied; an undeclared capability or service is never reachable
from Lua, exactly as for binary (INV-PLUGIN-44). No per-runtime gating logic is introduced.

### §5 — Coexistence with the legacy shims (the 3↔5 seam)

The legacy `cap_*.go` injection is **left intact and untouched.** The new bridge is
exercised in this sub-spec through a **test fixture**, not by rerouting production plugins —
because the old shim and the new bridge cannot both own the same Lua global, so rerouting a
real plugin necessarily means deleting its old injection, which is sub-spec 5's gating job.
In sub-spec 3 the two surfaces coexist; sub-spec 5 migrates and removes the old one.

## Invariants

Sub-spec 3 **binds** three invariants left `pending` by the foundation/decomposition:

| Invariant | Summary (registry) | How this sub-spec binds it |
| --- | --- | --- |
| INV-PLUGIN-44 | Both runtimes obtain every declared dependency through the one host gRPC broker, gated by declaration, authorized as `PluginSubject`; neither receives an undeclared capability/service. | Lua now consumes via the per-plugin bufconn server + `BrokerProxy` (§1, §3); fixture asserts a declared cap and a declared service are reachable and an **undeclared** one is not. |
| INV-PLUGIN-45 | The declaration gate MUST live at the broker/registry common path shared by both runtimes; per-runtime gating is forbidden. | §4 — Lua reuses the foundation resolver/registry gate; fixture asserts the same gate denies Lua and binary identically. |
| INV-PLUGIN-49 | A capability's RPC contract MUST be the single source both runtimes consume; no runtime-specific capability surface. | §2 host-cap codegen makes the proto the literal shared source; cross-runtime test asserts a Lua consumer and a binary consumer reach the **same** `host.v1` handler. Closes `holomush-eykuh.6`. |

No **new** invariant ids are minted by this sub-spec. (If the per-plugin-identity property of
§1 warrants its own registry entry rather than resting on INV-PLUGIN-22, that is a
reviewer-time decision; default is to bind the three above.)

## Testing strategy

- **Fixture Lua plugin** that consumes (a) a unary host capability and (b) a fixture
  plugin-provided service, over the new path. Build-tag-gated where it needs the integration
  harness; bridge marshaling is unit-testable in isolation.
- **New-server tests:** the `sessionServer`/`propertyServer`/`worldServer` implemented here
  are exercised by a Lua consumer (these three have no binary consumer, so their first
  consumption is the Lua path) — asserting parity with the behavior the legacy
  `cap_session.go`/`cap_property.go`/`cap_world_query.go` shims produce.
- **Cross-runtime parity test (binds INV-PLUGIN-49):** uses a capability that **already has a
  binary consumer/server** so both runtimes genuinely reach the *same* handler — `eval`
  (`Evaluate`), `settings` (`GetSetting`), or `kv` are good candidates (read-only, simple).
  The same `host.v1` handler is reached by a Lua consumer (bufconn) and a binary consumer
  (broker); both observe identical behavior and the host-derived subject. Carries
  `// Verifies: INV-PLUGIN-49`. (Session/Property/World cannot serve this test until a binary
  consumer exists — they prove the single-source *implementation* via the new-server tests
  above instead.)
- **Gate tests (bind INV-PLUGIN-44/45):** a declared dependency is reachable; an
  **undeclared** capability/service is not reachable from Lua; the shared gate denies Lua and
  binary identically.
- **Identity/anti-forgery:** assert the ABAC subject seen by the host server is
  `plugin:<fixtureName>` regardless of any value the Lua code attempts to supply.
- **Bridge marshaling units:** round-trip Lua table ↔ typed `host.v1` message (codegen path)
  and Lua table ↔ `dynamicpb.Message` (descriptor path); gRPC `status` → `pushError` mapping;
  load-time rejection of an unknown method / unsatisfiable `requires`.
- **Coexistence:** the legacy `cap_*.go` injection still works unchanged (no regression).
- Per CLAUDE.md, run `task test:int` after any `plugin.yaml`/`requires` fixture change —
  the unit resolver test stays green while only the integration suite exercises injection
  (per the `core-aliases` lesson, memory `5eead5f0`).

## Risks

| Risk | Mitigation |
| --- | --- |
| Per-plugin bufconn server adds N in-process servers/goroutines. | Servers are cheap; this reproduces binary's per-plugin model exactly. Custom in-memory `ClientConn` is a documented later optimization behind the same interface. |
| New codegen step (host-cap bridge) is build machinery that can drift. | Mirrors the existing generated-stub discipline; a `go:generate` + drift gate (as with `schemas/plugin.schema.json`, memory `12d49499`) keeps it honest. Generator output is the only artifact; never hand-edit. |
| Descriptor-driven plugin→plugin marshaling is one reflective seam. | Honest and unavoidable (third-party proto, non-compiled consumer); load-time validation recovers fail-early; binary keeps full static typing. |
| In-process gRPC marshaling overhead on capability calls. | Capability calls are not inner-loop (session/property/emit already touch DB/bus); the mandate explicitly discounts pre-measured perf; optimization escape hatch preserved. |
| Rerouting a production plugin would collide with its legacy injection. | Out of scope by the §5 seam — sub-spec 3 proves via fixture only; sub-spec 5 owns migration. |
| Implementing `sessionServer`/`propertyServer`/`worldServer` adds server-impl scope sub-spec 2 deferred. | Bounded: the host capability logic already exists (the legacy shims' backing interfaces — `SessionAccess`, property/world adapters); the new servers wrap it behind the `host.v1` contract. The proto contracts already exist; this is impl, not redesign. |
| §0 relocation of the 9 existing servers + `hostCapabilityBase` into `hostcap` behind a port is a non-trivial refactor of working binary code. | Behavior-preserving by construction (same servers, same host ops, now via a method-narrow interface `goplugin.Host` already satisfies); guarded by the existing `goplugin` host-capability tests passing unchanged. The interface is extracted from actual call sites, not speculative. Land §0 as its own commit/bead before the Lua-consumer work so a regression is isolatable. |

## References

- Epic: `holomush-eykuh` · this bead: `holomush-eykuh.2` · closes `holomush-eykuh.6` ·
  follow-on `holomush-eykuh.9`
- Sub-spec 1 (foundation): `docs/superpowers/specs/2026-06-11-plugin-capability-dependency-foundation-design.md`
- Sub-spec 2 (decomposition): `docs/superpowers/specs/2026-06-11-plugin-host-capability-decomposition-design.md`
- Runtime symmetry: `.claude/rules/plugin-runtime-symmetry.md`
- gRPC error handling: `.claude/rules/grpc-errors.md`
- Grounding: `goplugin/host.go:686`, `goplugin/broker_proxy.go`, `goplugin/host_service.go`,
  `hostfunc/capability.go`, `hostfunc/cap_session.go`, `hostfunc/adapter.go`,
  `api/proto/holomush/plugin/host/v1/*.proto`
<!-- adr-capture: sha256=d2adb37e8e840a22; session=cli; ts=2026-06-12T13:12:55Z; adrs=holomush-elqw4,holomush-ws2mi,holomush-l5bqb -->
